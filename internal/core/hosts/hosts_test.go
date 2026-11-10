package hosts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/command/commandtest"
)

const initial = "##\n127.0.0.1\tlocalhost\n::1             localhost\n"

// fakeHelper applies the helper actions to the file, like the script.
type fakeHelper struct {
	usable  bool
	failRm  bool // an earlier release's helper on Linux
	path    string
	actions []string
}

func (h *fakeHelper) CanUseHelper(context.Context) bool { return h.usable }

func (h *fakeHelper) RunHelper(_ context.Context, action, ip, hostname string) (string, error) {
	h.actions = append(h.actions, action+" "+ip+" "+hostname)
	if action == "remove" && h.failRm {
		return "", errors.New("sed: can't read : No such file or directory")
	}
	if action == "add" {
		f, _ := os.OpenFile(h.path, os.O_APPEND|os.O_WRONLY, 0)
		_, _ = f.WriteString(ip + "\t" + hostname + "\n")
		_ = f.Close()
	}
	return "", nil
}

// newManager returns a Manager on a temporary hosts file; `sudo tee`
// writes its stdin there.
func newManager(t *testing.T, goos string, helper *fakeHelper) (*Manager, *commandtest.Runner, *commandtest.Log) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &commandtest.Runner{Handle: func(c command.Cmd) (command.Result, error) {
		return command.Result{}, os.WriteFile(c.Args[1], c.Stdin, 0o644)
	}}
	log := &commandtest.Log{}
	m := &Manager{Path: path, GOOS: goos, Run: r, Log: log, IsAdmin: func() bool { return true }}
	if helper != nil {
		helper.path = path
		m.Helper = helper
	}
	return m, r, log
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAddRemoveWithSudoTee(t *testing.T) {
	m, r, log := newManager(t, "linux", &fakeHelper{usable: false})
	ctx := context.Background()

	if added, err := m.AddEntry(ctx, "127.0.0.1", "app.test"); err != nil || !added {
		t.Fatalf("add = %v, %v", added, err)
	}
	if got := read(t, m.Path); got != initial+"127.0.0.1\tapp.test\n::1\t\tapp.test\n" {
		t.Errorf("hosts after add:\n%q", got)
	}
	if added, _ := m.AddEntry(ctx, "127.0.0.1", "app.test"); added {
		t.Error("added twice")
	}
	if found, _ := m.HasEntry("127.0.0.1", "app"); found {
		t.Error("prefix matched")
	}

	if removed, err := m.RemoveEntry(ctx, "127.0.0.1", "app.test"); err != nil || !removed {
		t.Fatalf("remove = %v, %v", removed, err)
	}
	if got := read(t, m.Path); got != initial {
		t.Errorf("hosts after remove:\n%q", got)
	}
	if removed, _ := m.RemoveEntry(ctx, "127.0.0.1", "app.test"); removed {
		t.Error("removed twice")
	}
	if want := []string{"sudo tee " + m.Path, "sudo tee " + m.Path}; !slices.Equal(r.Lines(), want) {
		t.Errorf("calls = %v", r.Lines())
	}
	want := []string{
		"success: Added 127.0.0.1\tapp.test to hosts file",
		"info: 127.0.0.1\tapp.test already exists",
		"success: Removed app.test from hosts file",
		"info: app.test not found in hosts file",
	}
	if !slices.Equal(log.Lines, want) {
		t.Errorf("log = %q", log.Lines)
	}
}

func TestLocalhostIsNeverTouched(t *testing.T) {
	m, r, _ := newManager(t, "darwin", nil)
	ctx := context.Background()
	if added, _ := m.AddEntry(ctx, "127.0.0.1", "localhost"); added {
		t.Error("localhost added")
	}
	if removed, _ := m.RemoveEntry(ctx, "127.0.0.1", "localhost"); removed {
		t.Error("localhost removed")
	}
	if len(r.Calls) != 0 || read(t, m.Path) != initial {
		t.Error("hosts file changed")
	}
}

func TestHelper(t *testing.T) {
	h := &fakeHelper{usable: true}
	m, r, log := newManager(t, "darwin", h)
	ctx := context.Background()
	if added, err := m.AddEntry(ctx, "127.0.0.1", "app.test"); err != nil || !added {
		t.Fatalf("add = %v, %v", added, err)
	}
	if removed, err := m.RemoveEntry(ctx, "127.0.0.1", "app.test"); err != nil || !removed {
		t.Fatalf("remove = %v, %v", removed, err)
	}
	want := []string{"add 127.0.0.1 app.test", "add ::1 app.test", "remove 127.0.0.1 app.test", "remove ::1 app.test"}
	if !slices.Equal(h.actions, want) || len(r.Calls) != 0 {
		t.Errorf("helper %v, commands %v", h.actions, r.Lines())
	}
	wantLog := []string{
		"success: Added 127.0.0.1\tapp.test to hosts file",
		"success: Added ::1\tapp.test to hosts file",
		"success: Removed 127.0.0.1\tapp.test from hosts file",
	}
	if !slices.Equal(log.Lines, wantLog) {
		t.Errorf("log = %q", log.Lines)
	}
}

// A helper installed by an earlier release cannot remove entries on Linux;
// sudo tee does.
func TestRemoveFallsBackWhenHelperFails(t *testing.T) {
	h := &fakeHelper{usable: true, failRm: true}
	m, r, _ := newManager(t, "linux", h)
	ctx := context.Background()
	if _, err := m.AddEntry(ctx, "127.0.0.1", "app.test"); err != nil {
		t.Fatal(err)
	}
	if removed, err := m.RemoveEntry(ctx, "127.0.0.1", "app.test"); err != nil || !removed {
		t.Fatalf("remove = %v, %v", removed, err)
	}
	if read(t, m.Path) != initial || len(r.Calls) != 1 {
		t.Errorf("hosts:\n%q\ncalls %v", read(t, m.Path), r.Lines())
	}
}

func TestWindowsWritesDirectlyWithCRLF(t *testing.T) {
	m, r, _ := newManager(t, "windows", &fakeHelper{usable: true})
	if err := os.WriteFile(m.Path, []byte("127.0.0.1 localhost\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if added, err := m.AddEntry(ctx, "127.0.0.1", "app.test"); err != nil || !added {
		t.Fatalf("add = %v, %v", added, err)
	}
	if got := read(t, m.Path); got != "127.0.0.1 localhost\r\n127.0.0.1\tapp.test\r\n::1\t\tapp.test\r\n" {
		t.Errorf("hosts = %q", got)
	}
	if removed, err := m.RemoveEntry(ctx, "127.0.0.1", "app.test"); err != nil || !removed {
		t.Fatalf("remove = %v, %v", removed, err)
	}
	if got := read(t, m.Path); got != "127.0.0.1 localhost\r\n" {
		t.Errorf("hosts = %q", got)
	}
	if len(r.Calls) != 0 {
		t.Errorf("commands run on Windows: %v", r.Lines())
	}

	m.IsAdmin = func() bool { return false }
	var hErr *Error
	if _, err := m.AddEntry(ctx, "127.0.0.1", "b.test"); !errors.As(err, &hErr) {
		t.Errorf("non-admin err = %v", err)
	}
}

func TestSudoTeeFailure(t *testing.T) {
	m, r, _ := newManager(t, "linux", nil)
	r.Handle = func(command.Cmd) (command.Result, error) {
		return command.Result{ExitCode: 1, Stderr: []byte("sudo: 3 incorrect password attempts\n")}, nil
	}
	_, err := m.AddEntry(context.Background(), "127.0.0.1", "app.test")
	if err == nil || err.Error() != "Failed to write hosts file: sudo: 3 incorrect password attempts\n" {
		t.Errorf("err = %v", err)
	}
}
