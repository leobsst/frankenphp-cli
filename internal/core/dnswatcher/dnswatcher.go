// Package dnswatcher keeps the LAN DNS records pointed at this host's
// current LAN IP.
//
// The DNS container cannot see the host's IP (on macOS and Windows, Docker
// runs in a VM): a small `_watch-dns-internal` process polls the LAN IP
// and rewrites the records when it changes. It only runs with
// DNS_SERVER=true and DNS_IP unset.
//
// It is handed to the OS service manager when there is one (LaunchAgent on
// macOS, systemd user unit on Linux), otherwise it runs detached. The
// label, unit name, plist, unit and PID file are those of earlier releases,
// so that the current and earlier versions stop each other's watcher.
package dnswatcher

import (
	"context"
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
	"syscall"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

const (
	// Command is the hidden command running the watcher.
	Command = "_watch-dns-internal"
	// PollInterval is how often the LAN IP is checked.
	PollInterval = 5 * time.Second

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

// Config reads the .env settings.
type Config interface {
	Get(key string) string
}

// Needed reports whether the records follow the detected LAN IP.
func Needed(c Config) bool { return dns.Enabled(c) && c.Get("DNS_IP") == "" }

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

// Update restarts the watcher when the settings need it, stops it
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
// .env and the database are read again on each change, to include the
// domains added since the watcher started.
func CheckOnce(dataDir, lastIP string, stderr io.Writer) (string, error) {
	ip := dns.LANIP()
	if ip == lastIP || strings.HasPrefix(ip, "127.") {
		return lastIP, nil
	}
	e := env.New(filepath.Join(dataDir, ".env"), filepath.Join(dataDir, ".env.example"))
	if err := e.Load(); err != nil {
		return lastIP, err
	}
	if !Needed(e) {
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

// Run is the watcher process: it writes its PID (launchd and systemd start
// it without telling anyone), polls until SIGTERM, then removes the PID
// file if still its own. Errors are logged and the watch goes on.
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

	lastIP := ""
	for {
		ip, err := CheckOnce(dataDir, lastIP, stderr)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "DNS watcher error: %v\n", err)
		}
		lastIP = ip
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(PollInterval):
		}
	}
}
