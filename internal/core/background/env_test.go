package background

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/command/commandtest"
)

// The service manager starts the process with a minimal PATH: the spec's
// environment goes into the LaunchAgent and the systemd unit.
func TestServiceEnvironment(t *testing.T) {
	spec := Spec{Label: "dev.test", Unit: "test.service", LogFile: "test.log",
		Env: map[string]string{"PATH": "/opt/homebrew/bin:/usr/bin"}}
	plist := string(LaunchdPlist(spec, "/data", []string{"/bin/fm", "daemon", "run"}, ""))
	want := "\t<key>EnvironmentVariables</key>\n\t<dict>\n\t\t<key>PATH</key>\n\t\t<string>/opt/homebrew/bin:/usr/bin</string>\n\t</dict>\n"
	if !strings.Contains(plist, want) {
		t.Errorf("plist:\n%s", plist)
	}
	unit := SystemdUnitContent(spec, "/data", []string{"/bin/fm", "daemon", "run"})
	if !strings.Contains(unit, "Environment=\"PATH=/opt/homebrew/bin:/usr/bin\"\nExecStart=") {
		t.Errorf("unit:\n%s", unit)
	}
	// Without Env, nothing changes (the DNS watcher's files stay as released)
	spec.Env = nil
	if strings.Contains(string(LaunchdPlist(spec, "/data", []string{"/bin/fm"}, "")), "EnvironmentVariables") ||
		strings.Contains(SystemdUnitContent(spec, "/data", []string{"/bin/fm"}), "Environment=") {
		t.Error("environment written without Env")
	}
}

// On Windows the Run key of the user starts the process at login: written
// on Start, removed on Stop.
func TestWindowsRunKey(t *testing.T) {
	r := &commandtest.Runner{}
	s := &Service{Spec: Spec{Label: "dev.test", PIDFile: ".test.pid", LogFile: "test.log", WindowsRun: "FrankenManager",
		RunArgs: []string{"--no-update-check", "daemon", "start"}},
		DataDir: t.TempDir(), GOOS: "windows", Exe: `C:\Program Files\fm\frankenmanager.exe`, Run: r,
		Spawn: func(*Service) (int, error) { return 42, nil }, Alive: func(int) bool { return false }, Kill: func(int) error { return nil },
		PlistPath: filepath.Join(t.TempDir(), "none.plist"), UnitPath: filepath.Join(t.TempDir(), "none.service")}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := `reg add HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v FrankenManager /t REG_SZ /d "C:\Program Files\fm\frankenmanager.exe" --no-update-check daemon start /f`
	if lines := r.Lines(); !slices.Contains(lines, want) {
		t.Errorf("commands = %q", lines)
	}
	r.Calls = nil
	s.Stop(context.Background())
	if lines := r.Lines(); !slices.Contains(lines, `reg delete HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v FrankenManager /f`) {
		t.Errorf("commands = %q", lines)
	}
	// Without WindowsRun (the DNS watcher of earlier releases): no Run key
	r.Calls, s.WindowsRun = nil, ""
	_ = s.Start(context.Background())
	for _, l := range r.Lines() {
		if strings.HasPrefix(l, "reg ") {
			t.Errorf("unexpected %s", l)
		}
	}
}
