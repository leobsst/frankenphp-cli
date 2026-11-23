// Package background keeps a FrankenManager process running in the
// background: handed to the OS service manager when there is one
// (LaunchAgent on macOS, systemd user unit on Linux), which starts it at
// login and relaunches it when it dies, otherwise detached with a PID
// file. A Spec tells the services apart.
package background

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

// FrankenManager.app, shipped next to the binary on macOS.
const (
	MacOSAppName       = "FrankenManager.app"
	MacOSAppBundleID   = "dev.frankenmanager"
	MacOSAppExecutable = "frankenmanager-agent"
	lsregister         = "/System/Library/Frameworks/CoreServices.framework/Frameworks/" +
		"LaunchServices.framework/Support/lsregister"
)

// Spec is what tells a background service apart.
type Spec struct {
	Label       string   // LaunchAgent label
	Unit        string   // systemd user unit
	Description string   // of the unit
	Args        []string // the command line after the binary
	PIDFile     string   // in the data directory
	LogFile     string   // in the data directory: the error output
	// WindowsRun, when set, is the value name of the process in the Run key
	// of the user (HKCU): Windows then runs RunArgs at login. Windows has no
	// user service manager without administrator rights; nothing relaunches
	// the process when it dies
	WindowsRun string
	RunArgs    []string // the command line after the binary, at login
	// Env is the environment of the process under the service manager,
	// whose own is minimal (launchd: PATH=/usr/bin:/bin:/usr/sbin:/sbin)
	Env map[string]string
}

// Service runs a Spec for a data directory.
type Service struct {
	Spec
	DataDir    string
	GOOS       string
	Exe        string // real path of the binary
	PlistPath  string // ~/Library/LaunchAgents/<label>.plist
	UnitPath   string // $XDG_CONFIG_HOME/systemd/user/<unit>
	UID        int
	Lsregister string
	Run        command.Runner
	Sleep      func(time.Duration)
	// Spawn starts the detached process and returns its pid.
	Spawn func(s *Service) (int, error)
	// Alive and Kill check and terminate a process.
	Alive func(pid int) bool
	Kill  func(pid int) error
}

// New returns the Service of spec for dataDir and the running binary.
func New(dataDir string, spec Spec) (*Service, error) {
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
	return &Service{
		Spec:       spec,
		DataDir:    dataDir,
		GOOS:       runtime.GOOS,
		Exe:        exe,
		PlistPath:  filepath.Join(home, "Library", "LaunchAgents", spec.Label+".plist"),
		UnitPath:   filepath.Join(configHome, "systemd", "user", spec.Unit),
		UID:        os.Getuid(),
		Lsregister: lsregister,
		Run:        command.Exec{},
		Sleep:      time.Sleep,
		Spawn:      SpawnDetached,
		Alive:      platform.ProcessAlive,
		Kill:       platform.Terminate,
	}, nil
}

// PIDPath is the PID file of a detached process (or of one that writes its
// own).
func (s *Service) PIDPath() string { return filepath.Join(s.DataDir, s.PIDFile) }

// LogPath is the error output of the process.
func (s *Service) LogPath() string { return filepath.Join(s.DataDir, s.LogFile) }

// Command is the command line of the process.
func (s *Service) Command() []string { return append([]string{s.Exe}, s.Args...) }

// RunningPID returns the pid in the PID file, 0 when none runs. A stale
// PID file is removed.
func (s *Service) RunningPID() int {
	data, err := os.ReadFile(s.PIDPath())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || !s.Alive(pid) {
		_ = os.Remove(s.PIDPath())
		return 0
	}
	return pid
}

// Managed reports whether the OS service manager runs the process: it then
// relaunches it when it exits.
func (s *Service) Managed() bool {
	for _, p := range []string{s.PlistPath, s.UnitPath} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// Start (re)starts the process: LaunchAgent on macOS, systemd user unit on
// Linux, detached process otherwise or when they fail.
func (s *Service) Start(ctx context.Context) error {
	s.Stop(ctx)
	switch {
	case s.GOOS == "darwin" && s.Run.LookPath("launchctl") != "":
		if s.startLaunchd(ctx) {
			return nil
		}
	case s.GOOS == "linux" && s.Run.LookPath("systemctl") != "":
		if s.startSystemd(ctx) {
			return nil
		}
	case s.GOOS == "windows" && s.WindowsRun != "":
		// Best effort: the process starts detached now either way
		s.run(ctx, "reg", "add", windowsRunKey, "/v", s.WindowsRun, "/t", "REG_SZ", "/d", s.windowsRunCommand(), "/f")
	}
	pid, err := s.Spawn(s)
	if err != nil {
		return err
	}
	// The process writes it too: written here so that a status sees it now
	return os.WriteFile(s.PIDPath(), []byte(strconv.Itoa(pid)), 0o644)
}

// Stop stops the process and removes both services, whatever the OS, then
// terminates a detached process. It reports whether something was running.
func (s *Service) Stop(ctx context.Context) bool {
	stopped := s.stopLaunchd(ctx)
	if s.stopSystemd(ctx) {
		stopped = true
	}
	if s.GOOS == "windows" && s.WindowsRun != "" &&
		s.run(ctx, "reg", "delete", windowsRunKey, "/v", s.WindowsRun, "/f") {
		stopped = true
	}
	if pid := s.RunningPID(); pid != 0 {
		_ = s.Kill(pid)
		stopped = true
	}
	_ = os.Remove(s.PIDPath())
	return stopped
}

// SpawnDetached starts the process detached, its error output in LogPath.
func SpawnDetached(s *Service) (int, error) {
	// A file, not a pipe: this launcher exits right away and the process
	// would die on its first write to a closed pipe
	log, err := os.Create(s.LogPath())
	if err != nil {
		return 0, err
	}
	defer func() { _ = log.Close() }()
	args := s.Command()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = s.DataDir
	cmd.Stderr = log
	platform.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

func (s *Service) run(ctx context.Context, name string, args ...string) bool {
	res, err := s.Run.Run(ctx, command.Cmd{Name: name, Args: args})
	return err == nil && res.OK()
}

// --- Windows: Run key ---

// windowsRunKey is the key of the programs Windows runs at the user's login.
const windowsRunKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

// windowsRunCommand is the command line of the Run key value: the binary,
// quoted, then RunArgs.
func (s *Service) windowsRunCommand() string {
	parts := []string{`"` + s.Exe + `"`}
	for _, a := range s.RunArgs {
		if strings.ContainsAny(a, " \t") {
			a = `"` + a + `"`
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

// --- macOS: LaunchAgent ---

// MacOSApp returns FrankenManager.app next to the binary exe, "" when
// absent.
func MacOSApp(exe string) string {
	app := filepath.Join(filepath.Dir(exe), MacOSAppName)
	info, err := os.Stat(filepath.Join(app, "Contents", "MacOS", MacOSAppExecutable))
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return app
}

func (s *Service) launchdDomain() string { return "gui/" + strconv.Itoa(s.UID) }

func (s *Service) startLaunchd(ctx context.Context) bool {
	app := MacOSApp(s.Exe)
	if app != "" {
		// Launch Services must know the app for the Login Items to show its
		// name; best effort
		if _, err := os.Stat(s.Lsregister); err == nil {
			s.run(ctx, s.Lsregister, "-f", app)
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.PlistPath), 0o755); err != nil {
		return false
	}
	if err := os.WriteFile(s.PlistPath, LaunchdPlist(s.Spec, s.DataDir, s.Command(), app), 0o644); err != nil {
		return false
	}
	// Right after a bootout, launchd may refuse the bootstrap for a moment
	for range 5 {
		if s.run(ctx, "launchctl", "bootstrap", s.launchdDomain(), s.PlistPath) {
			return true
		}
		s.Sleep(500 * time.Millisecond)
	}
	_ = os.Remove(s.PlistPath)
	return false
}

// stopLaunchd boots the job out by its label even without its plist: a job
// whose plist was removed while it was still loaded (a failed bootout, an
// earlier release) keeps running and relaunching otherwise, on the same
// port.
func (s *Service) stopLaunchd(ctx context.Context) bool {
	_, err := os.Stat(s.PlistPath)
	stopped := err == nil
	if s.Run.LookPath("launchctl") != "" &&
		s.run(ctx, "launchctl", "bootout", s.launchdDomain()+"/"+s.Label) {
		stopped = true
	}
	_ = os.Remove(s.PlistPath)
	return stopped
}

// LaunchdPlist is the LaunchAgent of spec, starting the process at login
// and relaunching it when it dies (keys sorted). With app, the process
// runs through the app's launcher and the agent is tied to its bundle
// identifier. Its paths are joined with slashes: the file only exists on
// macOS.
func LaunchdPlist(spec Spec, dataDir string, command []string, app string) []byte {
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
	if len(spec.Env) > 0 {
		key("EnvironmentVariables")
		b.WriteString("\t<dict>\n")
		for _, k := range slices.Sorted(maps.Keys(spec.Env)) {
			b.WriteString("\t\t<key>" + plistEscape(k) + "</key>\n")
			str("\t\t", spec.Env[k])
		}
		b.WriteString("\t</dict>\n")
	}
	key("KeepAlive")
	b.WriteString("\t<true/>\n")
	key("Label")
	str("\t", spec.Label)
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
	str("\t", path.Join(dataDir, spec.LogFile))
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

// SystemdUnitContent is the user unit of spec, starting the process at
// login and relaunching it when it dies. Its paths are joined with slashes:
// the file only exists on Linux.
func SystemdUnitContent(spec Spec, dataDir string, command []string) string {
	quoted := make([]string, len(command))
	for i, arg := range command {
		quoted[i] = systemdQuote(arg)
	}
	env := ""
	for _, k := range slices.Sorted(maps.Keys(spec.Env)) {
		env += "Environment=" + systemdQuote(k+"="+spec.Env[k]) + "\n"
	}
	return "[Unit]\n" +
		"Description=" + spec.Description + "\n" +
		"\n" +
		"[Service]\n" +
		env +
		"ExecStart=" + strings.Join(quoted, " ") + "\n" +
		"WorkingDirectory=" + dataDir + "\n" +
		"StandardOutput=null\n" +
		"StandardError=append:" + path.Join(dataDir, spec.LogFile) + "\n" +
		"Restart=always\n" +
		"RestartSec=5\n" +
		"\n" +
		"[Install]\n" +
		"WantedBy=default.target\n"
}

func (s *Service) systemctl(ctx context.Context, args ...string) bool {
	return s.run(ctx, "systemctl", append([]string{"--user"}, args...)...)
}

func (s *Service) startSystemd(ctx context.Context) bool {
	if err := os.MkdirAll(filepath.Dir(s.UnitPath), 0o755); err != nil {
		return false
	}
	if err := os.WriteFile(s.UnitPath, []byte(SystemdUnitContent(s.Spec, s.DataDir, s.Command())), 0o644); err != nil {
		return false
	}
	// Fails without a systemd user instance (WSL, containers...)
	if s.systemctl(ctx, "daemon-reload") && s.systemctl(ctx, "enable", "--now", s.Unit) {
		return true
	}
	_ = os.Remove(s.UnitPath)
	s.systemctl(ctx, "daemon-reload")
	return false
}

// stopSystemd also stops a unit still running without its file, for the
// same reason as stopLaunchd.
func (s *Service) stopSystemd(ctx context.Context) bool {
	hasSystemctl := s.Run.LookPath("systemctl") != ""
	if _, err := os.Stat(s.UnitPath); err != nil {
		return hasSystemctl && s.systemctl(ctx, "stop", s.Unit)
	}
	if hasSystemctl {
		s.systemctl(ctx, "disable", "--now", s.Unit)
	}
	_ = os.Remove(s.UnitPath)
	if hasSystemctl {
		s.systemctl(ctx, "daemon-reload")
	}
	return true
}
