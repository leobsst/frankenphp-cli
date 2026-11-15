package privilege

import (
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/command"
)

const fmExe = "/usr/local/lib/frankenmanager/frankenmanager"

func TestGrantBindPort(t *testing.T) {
	// macOS and Windows: nothing to do
	m, r, _ := newManager(t, "darwin", false)
	if err := m.GrantBindPort(t.Context(), fmExe); err != nil || len(r.Calls) != 0 || !m.CanBindPort(t.Context(), fmExe) {
		t.Errorf("darwin: %v %v", err, r.Lines())
	}

	m, r, _ = newManager(t, "linux", true)
	r.Paths = map[string]string{"setcap": "/usr/sbin/setcap", "getcap": "/usr/sbin/getcap"}
	if err := m.GrantBindPort(t.Context(), fmExe); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.Lines(), "|"); got != "setcap cap_net_bind_service=+ep "+fmExe {
		t.Errorf("calls = %s", got)
	}

	r.Calls = nil
	r.Handle = func(c command.Cmd) (command.Result, error) {
		if c.Name == "getcap" {
			return command.Result{Stdout: []byte(fmExe + " cap_net_bind_service=ep\n")}, nil
		}
		return command.Result{}, nil
	}
	if !m.CanBindPort(t.Context(), fmExe) || !m.RevokeBindPort(t.Context(), fmExe) {
		t.Error("capability not seen or not revoked")
	}
	if got := strings.Join(r.Lines(), "|"); !strings.HasSuffix(got, "setcap -r "+fmExe) {
		t.Errorf("calls = %s", got)
	}
}

func TestGrantBindPortErrors(t *testing.T) {
	m, _, _ := newManager(t, "linux", false)
	if err := m.GrantBindPort(t.Context(), fmExe); err == nil || !strings.Contains(err.Error(), "sudo frankenmanager setup") {
		t.Errorf("not root: %v", err)
	}

	m, _, _ = newManager(t, "linux", true)
	if err := m.GrantBindPort(t.Context(), fmExe); err == nil || !strings.Contains(err.Error(), "libcap") {
		t.Errorf("no setcap: %v", err)
	}
	if m.CanBindPort(t.Context(), fmExe) {
		t.Error("no getcap: reported allowed")
	}

	m, r, _ := newManager(t, "linux", true)
	r.Paths = map[string]string{"setcap": "/usr/sbin/setcap"}
	r.Handle = func(command.Cmd) (command.Result, error) {
		return command.Result{ExitCode: 1, Stderr: []byte("Operation not supported")}, nil
	}
	if err := m.GrantBindPort(t.Context(), fmExe); err == nil || !strings.Contains(err.Error(), "Operation not supported") {
		t.Errorf("setcap failure: %v", err)
	}
}
