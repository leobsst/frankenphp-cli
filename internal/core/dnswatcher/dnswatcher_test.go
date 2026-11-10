package dnswatcher

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/command/commandtest"
)

const (
	dataDir = "/Users/me/Library/Application Support/frankenmanager & <co>"
	exe     = "/usr/local/lib/frankenmanager/frankenmanager"
)

func golden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The testdata files are the plist and unit written by earlier releases:
// they must stay identical so that both versions stop each other's service.
func TestServiceFilesMatchReference(t *testing.T) {
	cmd := []string{exe, "--no-update-check", Command}
	if got := string(LaunchdPlist(dataDir, cmd, "")); got != golden(t, "plist") {
		t.Errorf("plist differs from the reference:\n%s", got)
	}
	app := "/usr/local/lib/frankenmanager/FrankenManager.app"
	if got := string(LaunchdPlist(dataDir, cmd, app)); got != golden(t, "plist_app") {
		t.Errorf("plist with app differs from the reference:\n%s", got)
	}
	unitDir := `/home/me/.local/share/frank "en" \manager`
	unitCmd := []string{unitDir + "/frankenmanager", "--no-update-check", Command}
	if got := SystemdUnitContent(unitDir, unitCmd); got != golden(t, "unit") {
		t.Errorf("unit differs from the reference:\n%s", got)
	}
}

type memConfig map[string]string

func (m memConfig) Get(k string) string { return m[k] }

func TestNeeded(t *testing.T) {
	for c, want := range map[*memConfig]bool{
		{}:                     false,
		{"DNS_SERVER": "true"}: true,
		{"DNS_SERVER": "true", "DNS_IP": "1.2.3.4"}: false,
		{"DNS_SERVER": "TRUE"}:                      false,
	} {
		if got := Needed(*c); got != want {
			t.Errorf("Needed(%v) = %v", *c, got)
		}
	}
}

// newWatcher returns a Watcher whose files live in a temporary directory
// and whose detached process is faked.
// fakePID is the pid of the fake detached watcher: alive for Alive, and
// recorded by Kill instead of being signalled.
const fakePID = 424242

func newWatcher(t *testing.T, goos string, paths map[string]string) (*Watcher, *commandtest.Runner, *int) {
	t.Helper()
	dir := t.TempDir()
	r := &commandtest.Runner{Paths: paths}
	spawned := 0
	w := &Watcher{
		DataDir:   filepath.Join(dir, "data"),
		GOOS:      goos,
		Exe:       filepath.Join(dir, "lib", "frankenmanager"),
		PlistPath: filepath.Join(dir, "LaunchAgents", LaunchdLabel+".plist"),
		UnitPath:  filepath.Join(dir, "systemd", "user", SystemdUnit),
		UID:       501,
		Run:       r,
		Sleep:     func(time.Duration) {},
		Spawn: func(*Watcher) (int, error) {
			spawned++
			return fakePID, nil
		},
		Alive: func(pid int) bool { return pid == fakePID },
		Kill:  func(int) error { return nil },
	}
	if err := os.MkdirAll(w.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return w, r, &spawned
}

func TestStartLaunchdWithApp(t *testing.T) {
	w, r, spawned := newWatcher(t, "darwin", map[string]string{"launchctl": "/bin/launchctl"})
	agent := filepath.Join(filepath.Dir(w.Exe), MacOSAppName, "Contents", "MacOS", MacOSAppExecutable)
	if err := os.MkdirAll(filepath.Dir(agent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	w.Lsregister = agent // any existing file
	attempts := 0
	r.Handle = func(c command.Cmd) (command.Result, error) {
		if c.Name == "launchctl" && c.Args[0] == "bootstrap" {
			attempts++
			if attempts < 3 { // launchd still tearing the old job down
				return command.Result{ExitCode: 5}, nil
			}
		}
		return command.Result{}, nil
	}

	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(filepath.Dir(w.Exe), MacOSAppName)
	data, _ := os.ReadFile(w.PlistPath)
	if string(data) != string(LaunchdPlist(w.DataDir, []string{w.Exe, "--no-update-check", Command}, app)) {
		t.Errorf("plist:\n%s", data)
	}
	want := []string{
		agent + " -f " + app,
		"launchctl bootstrap gui/501 " + w.PlistPath,
		"launchctl bootstrap gui/501 " + w.PlistPath,
		"launchctl bootstrap gui/501 " + w.PlistPath,
	}
	if !slices.Equal(r.Lines(), want) || *spawned != 0 {
		t.Errorf("calls = %v, spawned %d", r.Lines(), *spawned)
	}

	r.Calls = nil
	if !w.Stop(context.Background()) {
		t.Error("Stop found nothing")
	}
	if _, err := os.Stat(w.PlistPath); err == nil {
		t.Error("plist left")
	}
	if want := []string{"launchctl bootout gui/501/" + LaunchdLabel}; !slices.Equal(r.Lines(), want) {
		t.Errorf("stop calls = %v", r.Lines())
	}
}

func TestStartLaunchdFailureFallsBackToDetached(t *testing.T) {
	w, r, spawned := newWatcher(t, "darwin", map[string]string{"launchctl": "/bin/launchctl"})
	r.Handle = func(command.Cmd) (command.Result, error) { return command.Result{ExitCode: 1}, nil }
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.Calls) != 5 || *spawned != 1 {
		t.Errorf("calls %v, spawned %d", r.Lines(), *spawned)
	}
	if _, err := os.Stat(w.PlistPath); err == nil {
		t.Error("plist left after failure")
	}
	if pid := w.RunningPID(); pid != fakePID {
		t.Errorf("pid = %d", pid)
	}
}

func TestStartSystemd(t *testing.T) {
	w, r, spawned := newWatcher(t, "linux", map[string]string{"systemctl": "/usr/bin/systemctl"})
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(w.UnitPath)
	if string(data) != SystemdUnitContent(w.DataDir, []string{w.Exe, "--no-update-check", Command}) {
		t.Errorf("unit:\n%s", data)
	}
	want := []string{"systemctl --user daemon-reload", "systemctl --user enable --now " + SystemdUnit}
	if !slices.Equal(r.Lines(), want) || *spawned != 0 {
		t.Errorf("calls = %v", r.Lines())
	}

	// Restart: the old unit is removed first
	r.Calls = nil
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	want = []string{
		"systemctl --user disable --now " + SystemdUnit, "systemctl --user daemon-reload",
		"systemctl --user daemon-reload", "systemctl --user enable --now " + SystemdUnit,
	}
	if !slices.Equal(r.Lines(), want) {
		t.Errorf("restart calls = %v", r.Lines())
	}
}

func TestStartSystemdWithoutUserInstance(t *testing.T) {
	w, r, spawned := newWatcher(t, "linux", map[string]string{"systemctl": "/usr/bin/systemctl"})
	r.Handle = func(c command.Cmd) (command.Result, error) {
		if slices.Contains(c.Args, "enable") {
			return command.Result{ExitCode: 1}, nil
		}
		return command.Result{}, nil
	}
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.UnitPath); err == nil || *spawned != 1 {
		t.Errorf("unit left or not detached (spawned %d)", *spawned)
	}
}

// Windows (or no service manager): detached. Stop removes services left
// by another OS run too, and the PID file.
func TestDetachedAndStop(t *testing.T) {
	w, r, spawned := newWatcher(t, "windows", nil)
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(w.DataDir, pidName))
	if *spawned != 1 || string(data) != strconv.Itoa(fakePID) || len(r.Calls) != 0 {
		t.Errorf("spawned %d, pid file %q, calls %v", *spawned, data, r.Lines())
	}

	// A stale PID is cleaned up
	if err := os.WriteFile(filepath.Join(w.DataDir, pidName), []byte("999999999"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w.RunningPID() != 0 {
		t.Error("stale pid reported")
	}
	if _, err := os.Stat(filepath.Join(w.DataDir, pidName)); err == nil {
		t.Error("stale pid file kept")
	}
	if w.Stop(context.Background()) {
		t.Error("Stop reported a running watcher")
	}

	// A running detached watcher is terminated
	var killed []int
	w.Kill = func(pid int) error { killed = append(killed, pid); return nil }
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !w.Stop(context.Background()) || !slices.Equal(killed, []int{fakePID}) {
		t.Errorf("killed = %v", killed)
	}
	if _, err := os.Stat(filepath.Join(w.DataDir, pidName)); err == nil {
		t.Error("pid file kept after Stop")
	}
}

func TestUpdateStopsWhenNotNeeded(t *testing.T) {
	w, _, spawned := newWatcher(t, "windows", nil)
	running, err := w.Update(context.Background(), memConfig{"DNS_SERVER": "true", "DNS_IP": "10.0.0.1"})
	if err != nil || running || *spawned != 0 {
		t.Errorf("running=%v err=%v spawned=%d", running, err, *spawned)
	}
	running, err = w.Update(context.Background(), memConfig{"DNS_SERVER": "true"})
	if err != nil || !running || *spawned != 1 {
		t.Errorf("running=%v err=%v spawned=%d", running, err, *spawned)
	}
}
