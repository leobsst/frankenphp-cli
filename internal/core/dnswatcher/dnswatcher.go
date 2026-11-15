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
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/dnsclients"
	"github.com/leobsst/frankenphp-cli/internal/core/embeddns"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/platform"
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
	MacOSAppName       = "FrankenManager.app"
	MacOSAppBundleID   = "dev.frankenmanager"
	MacOSAppExecutable = "frankenmanager-agent"
	lsregister         = "/System/Library/Frameworks/CoreServices.framework/Frameworks/" +
		"LaunchServices.framework/Support/lsregister"

	pidName = ".dns_watcher.pid"
	logName = ".dns_watcher.log"
)

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
	exe, err := platform.Executable()
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	return &Watcher{
		DataDir:    dataDir,
		GOOS:       runtime.GOOS,
		Exe:        exe,
		PlistPath:  filepath.Join(home, "Library", "LaunchAgents", LaunchdLabel+".plist"),
		UnitPath:   filepath.Join(configHome, "systemd", "user", SystemdUnit),
		UID:        os.Getuid(),
		Lsregister: lsregister,
		Run:        command.Exec{},
		Sleep:      time.Sleep,
		Spawn:      spawnDetached,
		Alive:      platform.ProcessAlive,
		Kill:       platform.Terminate,
	}, nil
}

func (w *Watcher) pidPath() string { return filepath.Join(w.DataDir, pidName) }
func (w *Watcher) logPath() string { return filepath.Join(w.DataDir, logName) }

// command is the watcher command line.
func (w *Watcher) command() []string { return []string{w.Exe, "--no-update-check", Command} }

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
func (w *Watcher) RunningPID() int {
	data, err := os.ReadFile(w.pidPath())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || !w.Alive(pid) {
		_ = os.Remove(w.pidPath())
		return 0
	}
	return pid
}

// Start (re)starts the watcher: LaunchAgent on macOS, systemd user unit on
// Linux, detached process otherwise or when they fail.
func (w *Watcher) Start(ctx context.Context) error {
	w.Stop(ctx)
	switch {
	case w.GOOS == "darwin" && w.Run.LookPath("launchctl") != "":
		if w.startLaunchd(ctx) {
			return nil
		}
	case w.GOOS == "linux" && w.Run.LookPath("systemctl") != "":
		if w.startSystemd(ctx) {
			return nil
		}
	}
	pid, err := w.Spawn(w)
	if err != nil {
		return err
	}
	// The watcher writes it too: written here so that `status` sees it now
	return os.WriteFile(w.pidPath(), []byte(strconv.Itoa(pid)), 0o644)
}

// Stop stops the watcher and removes both services, whatever the OS, then
// terminates a detached watcher (or one started by a version without
// services). It reports whether something was running.
func (w *Watcher) Stop(ctx context.Context) bool {
	stopped := w.stopLaunchd(ctx)
	if w.stopSystemd(ctx) {
		stopped = true
	}
	if pid := w.RunningPID(); pid != 0 {
		_ = w.Kill(pid)
		stopped = true
	}
	_ = os.Remove(w.pidPath())
	return stopped
}

func spawnDetached(w *Watcher) (int, error) {
	// stderr goes to a file, not a pipe: this launcher exits right away and
	// the watcher would die on its first write to a closed pipe
	log, err := os.Create(w.logPath())
	if err != nil {
		return 0, err
	}
	defer func() { _ = log.Close() }()
	args := w.command()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = w.DataDir
	cmd.Stderr = log
	platform.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

func (w *Watcher) run(ctx context.Context, name string, args ...string) bool {
	res, err := w.Run.Run(ctx, command.Cmd{Name: name, Args: args})
	return err == nil && res.OK()
}

// --- macOS: LaunchAgent ---

// macOSApp returns FrankenManager.app next to the binary, "" when absent.
func (w *Watcher) macOSApp() string {
	app := filepath.Join(filepath.Dir(w.Exe), MacOSAppName)
	info, err := os.Stat(filepath.Join(app, "Contents", "MacOS", MacOSAppExecutable))
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return app
}

func (w *Watcher) launchdDomain() string { return "gui/" + strconv.Itoa(w.UID) }

func (w *Watcher) startLaunchd(ctx context.Context) bool {
	app := w.macOSApp()
	if app != "" {
		// Launch Services must know the app for the Login Items to show its
		// name; best effort
		if _, err := os.Stat(w.Lsregister); err == nil {
			w.run(ctx, w.Lsregister, "-f", app)
		}
	}
	if err := os.MkdirAll(filepath.Dir(w.PlistPath), 0o755); err != nil {
		return false
	}
	if err := os.WriteFile(w.PlistPath, LaunchdPlist(w.DataDir, w.command(), app), 0o644); err != nil {
		return false
	}
	// Right after a bootout, launchd may refuse the bootstrap for a moment
	for range 5 {
		if w.run(ctx, "launchctl", "bootstrap", w.launchdDomain(), w.PlistPath) {
			return true
		}
		w.Sleep(500 * time.Millisecond)
	}
	_ = os.Remove(w.PlistPath)
	return false
}

func (w *Watcher) stopLaunchd(ctx context.Context) bool {
	if _, err := os.Stat(w.PlistPath); err != nil {
		return false
	}
	if w.Run.LookPath("launchctl") != "" {
		w.run(ctx, "launchctl", "bootout", w.launchdDomain()+"/"+LaunchdLabel)
	}
	_ = os.Remove(w.PlistPath)
	return true
}

// LaunchdPlist is the LaunchAgent starting the watcher at login and
// relaunching it when it dies, byte for byte the one written by earlier
// releases (keys sorted). With app, the watcher runs through the app's
// launcher and the agent is tied to its bundle identifier. Its paths are
// joined with slashes: the file only exists on macOS.
func LaunchdPlist(dataDir string, command []string, app string) []byte {
	if app != "" {
		command = append([]string{path.Join(app, "Contents", "MacOS", MacOSAppExecutable)}, command[1:]...)
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	key := func(k string) { b.WriteString("\t<key>" + k + "</key>\n") }
	str := func(indent, s string) { b.WriteString(indent + "<string>" + plistEscape(s) + "</string>\n") }
	if app != "" {
		key("AssociatedBundleIdentifiers")
		b.WriteString("\t<array>\n")
		str("\t\t", MacOSAppBundleID)
		b.WriteString("\t</array>\n")
	}
	key("KeepAlive")
	b.WriteString("\t<true/>\n")
	key("Label")
	str("\t", LaunchdLabel)
	key("ProcessType")
	str("\t", "Background")
	key("ProgramArguments")
	b.WriteString("\t<array>\n")
	for _, arg := range command {
		str("\t\t", arg)
	}
	b.WriteString("\t</array>\n")
	key("RunAtLoad")
	b.WriteString("\t<true/>\n")
	key("StandardErrorPath")
	str("\t", path.Join(dataDir, logName))
	key("StandardOutPath")
	str("\t", "/dev/null")
	key("WorkingDirectory")
	str("\t", dataDir)
	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String())
}

// plistEscape escapes a plist string: line endings to \n, then &, < and >.
func plistEscape(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// --- Linux: systemd user unit ---

func systemdQuote(arg string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(arg) + `"`
}

// SystemdUnitContent is the user unit starting the watcher at login and
// relaunching it when it dies. Its paths are joined with slashes: the file
// only exists on Linux.
func SystemdUnitContent(dataDir string, command []string) string {
	quoted := make([]string, len(command))
	for i, arg := range command {
		quoted[i] = systemdQuote(arg)
	}
	return "[Unit]\n" +
		"Description=FrankenManager LAN DNS watcher\n" +
		"\n" +
		"[Service]\n" +
		"ExecStart=" + strings.Join(quoted, " ") + "\n" +
		"WorkingDirectory=" + dataDir + "\n" +
		"StandardOutput=null\n" +
		"StandardError=append:" + path.Join(dataDir, logName) + "\n" +
		"Restart=always\n" +
		"RestartSec=5\n" +
		"\n" +
		"[Install]\n" +
		"WantedBy=default.target\n"
}

func (w *Watcher) systemctl(ctx context.Context, args ...string) bool {
	return w.run(ctx, "systemctl", append([]string{"--user"}, args...)...)
}

func (w *Watcher) startSystemd(ctx context.Context) bool {
	if err := os.MkdirAll(filepath.Dir(w.UnitPath), 0o755); err != nil {
		return false
	}
	if err := os.WriteFile(w.UnitPath, []byte(SystemdUnitContent(w.DataDir, w.command())), 0o644); err != nil {
		return false
	}
	// Fails without a systemd user instance (WSL, containers...)
	if w.systemctl(ctx, "daemon-reload") && w.systemctl(ctx, "enable", "--now", SystemdUnit) {
		return true
	}
	_ = os.Remove(w.UnitPath)
	w.systemctl(ctx, "daemon-reload")
	return false
}

func (w *Watcher) stopSystemd(ctx context.Context) bool {
	if _, err := os.Stat(w.UnitPath); err != nil {
		return false
	}
	hasSystemctl := w.Run.LookPath("systemctl") != ""
	if hasSystemctl {
		w.systemctl(ctx, "disable", "--now", SystemdUnit)
	}
	_ = os.Remove(w.UnitPath)
	if hasSystemctl {
		w.systemctl(ctx, "daemon-reload")
	}
	return true
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
	_, _ = fmt.Fprintf(stderr, "%s LAN IP changed: %s -> %s, DNS records updated\n",
		time.Now().Format("2006-01-02T15:04:05"), previous, ip)
	return ip, nil
}

// Run is the background process: it writes its PID (launchd and systemd
// start it without telling anyone), writes the DNS files, serves them until
// SIGTERM, then writes the pending devices and removes the PID file if
// still its own. A server that cannot start ends Run with its error: the
// service manager relaunches the process. Other errors are logged and the
// process goes on.
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
		return fmt.Errorf("%w\nThe LAN DNS server may not use this port: run `sudo frankenmanager setup`, "+
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

func (l *loop) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(l.stderr, "%s "+format+"\n", append([]any{time.Now().Format("2006-01-02T15:04:05")}, args...)...)
}

func (l *loop) tick(ctx context.Context) {
	ip, err := CheckOnce(l.dataDir, l.lastIP, l.stderr)
	if err != nil {
		l.logf("DNS watcher error: %v", err)
	}
	l.lastIP = ip
	l.flush(ctx)
	if now := time.Now(); now.Sub(l.lastPrune) >= PruneInterval {
		if err := dnsclients.Prune(l.store, now); err != nil {
			l.logf("DNS clients prune error: %v", err)
		}
		l.lastPrune = now
	}
}

// flush writes the devices seen, then looks up the identity of the new
// ones in the background, one lookup batch at a time.
func (l *loop) flush(ctx context.Context) {
	stale, err := l.tracker.Flush(l.store)
	if err != nil {
		l.logf("DNS clients write error: %v", err)
		return
	}
	if len(stale) == 0 || !l.identifying.TryLock() {
		return
	}
	go func() {
		defer l.identifying.Unlock()
		if err := dnsclients.Identify(ctx, l.store, l.identity, stale, time.Now()); err != nil {
			l.logf("DNS clients identity error: %v", err)
		}
	}()
}
