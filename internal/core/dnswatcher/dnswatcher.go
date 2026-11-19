// Package dnswatcher runs the LAN DNS server in the background: CoreDNS
// embedded in a `_watch-dns-internal` process (see embeddns), which also
// records the devices querying it (see dnsclients) and keeps the records
// pointed at this host's current LAN IP when DNS_IP is unset.
//
// It runs on the host rather than in a container so that it sees the real
// address of each device. It runs whenever DNS_SERVER=true.
//
// It is handed to the OS service manager when there is one (LaunchAgent on
// macOS, systemd user unit on Linux), otherwise it runs detached. The
// command, label, unit name, plist, unit and PID file are those of earlier
// releases (where the process only followed the LAN IP), so that the
// current and earlier versions stop each other's process.
package dnswatcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/core/background"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/dnsclients"
	"github.com/leobsst/frankenphp-cli/internal/core/embeddns"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

const (
	// Command is the hidden command running the server.
	Command = "_watch-dns-internal"
	// PollInterval is how often the LAN IP is checked and the devices
	// written to the database.
	PollInterval = 5 * time.Second
	// PruneInterval is how often the devices no longer seen are removed.
	PruneInterval = 24 * time.Hour

	LaunchdLabel = "dev.frankenmanager.dns-watcher"
	SystemdUnit  = "frankenmanager-dns-watcher.service"

	// FrankenManager.app, shipped next to the binary on macOS.
	MacOSAppName       = background.MacOSAppName
	MacOSAppBundleID   = background.MacOSAppBundleID
	MacOSAppExecutable = background.MacOSAppExecutable

	pidName = ".dns_watcher.pid"
	logName = ".dns_watcher.log"
)

// Spec is the background service of the watcher, as earlier releases
// installed it.
var Spec = background.Spec{
	Label: LaunchdLabel, Unit: SystemdUnit, Description: "FrankenManager LAN DNS watcher",
	Args: []string{"--no-update-check", Command}, PIDFile: pidName, LogFile: logName,
}

// Config reads the configuration.
type Config interface {
	Get(key string) string
}

// Needed reports whether the background process runs: whenever the LAN DNS
// server is on.
func Needed(c Config) bool { return dns.Enabled(c) }

// FollowsIP reports whether the records follow the detected LAN IP: DNS_IP
// unset.
func FollowsIP(c Config) bool { return dns.Enabled(c) && c.Get("DNS_IP") == "" }

// Watcher starts and stops the watcher of a data directory.
// Watcher runs the watcher process of a data directory (see
// background.Service).
type Watcher struct {
	DataDir    string
	GOOS       string
	Exe        string // real path of the binary
	PlistPath  string // ~/Library/LaunchAgents/<label>.plist
	UnitPath   string // $XDG_CONFIG_HOME/systemd/user/<unit>
	UID        int
	Lsregister string
	Run        command.Runner
	Sleep      func(time.Duration)
	// Spawn starts the detached watcher and returns its pid.
	Spawn func(w *Watcher) (int, error)
	// Alive and Kill check and terminate a process.
	Alive func(pid int) bool
	Kill  func(pid int) error
}

// New returns the Watcher of dataDir for the running binary.
func New(dataDir string) (*Watcher, error) {
	s, err := background.New(dataDir, Spec)
	if err != nil {
		return nil, err
	}
	return &Watcher{
		DataDir: s.DataDir, GOOS: s.GOOS, Exe: s.Exe, PlistPath: s.PlistPath, UnitPath: s.UnitPath,
		UID: s.UID, Lsregister: s.Lsregister, Run: s.Run, Sleep: s.Sleep,
		Spawn: spawnDetached, Alive: s.Alive, Kill: s.Kill,
	}, nil
}

// service is the background service of w.
func (w *Watcher) service() *background.Service {
	return &background.Service{
		Spec: Spec, DataDir: w.DataDir, GOOS: w.GOOS, Exe: w.Exe, PlistPath: w.PlistPath, UnitPath: w.UnitPath,
		UID: w.UID, Lsregister: w.Lsregister, Run: w.Run, Sleep: w.Sleep,
		Spawn: func(*background.Service) (int, error) { return w.Spawn(w) },
		Alive: w.Alive, Kill: w.Kill,
	}
}

func spawnDetached(w *Watcher) (int, error) { return background.SpawnDetached(w.service()) }

// Update restarts the process when the LAN DNS server is on, stops it
// otherwise, and reports whether it runs.
func (w *Watcher) Update(ctx context.Context, c Config) (bool, error) {
	if !Needed(c) {
		w.Stop(ctx)
		return false, nil
	}
	return true, w.Start(ctx)
}

// RunningPID returns the pid of the running watcher, 0 when none runs. A
// stale PID file is removed.
func (w *Watcher) RunningPID() int { return w.service().RunningPID() }

// Start (re)starts the watcher: LaunchAgent on macOS, systemd user unit on
// Linux, detached process otherwise or when they fail.
func (w *Watcher) Start(ctx context.Context) error { return w.service().Start(ctx) }

// Stop stops the watcher and removes both services, whatever the OS, then
// terminates a detached watcher (or one started by a version without
// services). It reports whether something was running.
func (w *Watcher) Stop(ctx context.Context) bool { return w.service().Stop(ctx) }

// LaunchdPlist is the LaunchAgent of the watcher, byte for byte the one
// written by earlier releases.
func LaunchdPlist(dataDir string, command []string, app string) []byte {
	return background.LaunchdPlist(Spec, dataDir, command, app)
}

// SystemdUnitContent is the user unit of the watcher.
func SystemdUnitContent(dataDir string, command []string) string {
	return background.SystemdUnitContent(Spec, dataDir, command)
}

// --- Watcher process ---

// CheckOnce rewrites the records when the LAN IP changed since lastIP, and
// returns the IP they now point to. A loopback IP means no network for now
// (e.g. switching from Wi-Fi to Ethernet): the records are left alone. The
// configuration and the domains are read again on each change, to include the
// domains added since the watcher started.
func CheckOnce(dataDir, lastIP string, stderr io.Writer) (string, error) {
	ip := dns.LANIP()
	if ip == lastIP || strings.HasPrefix(ip, "127.") {
		return lastIP, nil
	}
	e, _, err := env.Open(dataDir)
	if err != nil {
		return lastIP, err
	}
	if !FollowsIP(e) {
		return lastIP, nil
	}
	s, err := store.Open(filepath.Join(dataDir, "db.sqlite"))
	if err != nil {
		return lastIP, err
	}
	defer func() { _ = s.Close() }()
	if err := dns.Sync(dataDir, e, s); err != nil {
		return lastIP, err
	}
	previous := lastIP
	if previous == "" {
		previous = "None"
	}
	_, _ = fmt.Fprint(stderr, i18n.Tf("%s LAN IP changed: %s -> %s, DNS records updated\n",
		time.Now().Format("2006-01-02T15:04:05"), previous, ip))
	return ip, nil
}

// Run is the background process of earlier releases: it writes its PID
// (launchd and systemd start it without telling anyone), then serves (see
// Serve) until SIGTERM, and removes the PID file if still its own.
func Run(ctx context.Context, dataDir string, stderr io.Writer) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM)
	defer stop()

	pidPath := filepath.Join(dataDir, pidName)
	pid := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(pidPath, []byte(pid), 0o644); err != nil {
		return err
	}
	defer func() {
		if data, err := os.ReadFile(pidPath); err == nil && strings.TrimSpace(string(data)) == pid {
			_ = os.Remove(pidPath)
		}
	}()
	return Serve(ctx, dataDir, stderr)
}

// Serve writes the DNS files, serves them until ctx ends, then writes the
// pending devices. A server that cannot start ends Serve with its error.
// Other errors are logged and the server goes on.
func Serve(ctx context.Context, dataDir string, stderr io.Writer) error {
	s, err := store.Open(filepath.Join(dataDir, "db.sqlite"))
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	// Current files: the Corefile of an earlier release was a container's
	if err := syncFiles(dataDir, s); err != nil {
		return err
	}

	tracker := dnsclients.NewTracker()
	serverCtx, stopServer := context.WithCancel(ctx)
	served := make(chan error, 1)
	go func() {
		served <- embeddns.Run(serverCtx, embeddns.Options{
			Corefile: dns.CorefilePath(dataDir), OnQuery: tracker.Seen, Log: stderr,
		})
	}()
	l := &loop{dataDir: dataDir, store: s, tracker: tracker, stderr: stderr, identity: dnsclients.SystemIdentifier()}
	defer l.flush(context.Background())

	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()
	for {
		select {
		case err := <-served:
			stopServer()
			if err != nil {
				return serverError(err)
			}
			return nil
		case <-ctx.Done():
			stopServer()
			return <-served
		case <-ticker.C:
			l.tick(ctx)
		}
	}
}

// syncFiles writes the Corefile and the records when the server is on.
func syncFiles(dataDir string, s *store.Store) error {
	e, _, err := env.Open(dataDir)
	if err != nil {
		return err
	}
	return dns.Sync(dataDir, e, s)
}

// serverError explains a refused port 53.
func serverError(err error) error {
	if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "permission denied") {
		return i18n.Errorf("%w\nThe LAN DNS server may not use this port: run `sudo frankenmanager setup`, "+
			"or set DNS_PORT above 1023", err)
	}
	return err
}

// loop is the periodic work of Run.
type loop struct {
	dataDir  string
	store    *store.Store
	tracker  *dnsclients.Tracker
	stderr   io.Writer
	identity dnsclients.Identifier

	lastIP      string
	lastPrune   time.Time
	identifying sync.Mutex
}

// logf writes msg, already translated, after the time.
func (l *loop) logf(msg string) {
	_, _ = fmt.Fprintf(l.stderr, "%s %s\n", time.Now().Format("2006-01-02T15:04:05"), msg)
}

func (l *loop) tick(ctx context.Context) {
	ip, err := CheckOnce(l.dataDir, l.lastIP, l.stderr)
	if err != nil {
		l.logf(i18n.Tf("DNS watcher error: %v", err))
	}
	l.lastIP = ip
	l.flush(ctx)
	if now := time.Now(); now.Sub(l.lastPrune) >= PruneInterval {
		if err := dnsclients.Prune(l.store, now); err != nil {
			l.logf(i18n.Tf("DNS clients prune error: %v", err))
		}
		l.lastPrune = now
	}
}

// flush writes the devices seen, then looks up the identity of the new
// ones in the background, one lookup batch at a time.
func (l *loop) flush(ctx context.Context) {
	stale, err := l.tracker.Flush(l.store)
	if err != nil {
		l.logf(i18n.Tf("DNS clients write error: %v", err))
		return
	}
	if len(stale) == 0 || !l.identifying.TryLock() {
		return
	}
	go func() {
		defer l.identifying.Unlock()
		if err := dnsclients.Identify(ctx, l.store, l.identity, stale, time.Now()); err != nil {
			l.logf(i18n.Tf("DNS clients identity error: %v", err))
		}
	}()
}
