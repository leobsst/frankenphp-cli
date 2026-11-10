package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/version"
)

func run(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = Execute(args, &o, &e)
	return code, o.String(), e.String()
}

func TestVersion(t *testing.T) {
	want := "FrankenManager version " + version.Version + "\n"
	for _, flag := range []string{"--version", "-v"} {
		code, out, _ := run(t, flag)
		if code != ExitOK || out != want {
			t.Errorf("%s: code %d, output %q; want 0, %q", flag, code, out, want)
		}
	}
}

// Processes spawned by the CLI pass the hidden option before the command.
func TestNoUpdateCheckAccepted(t *testing.T) {
	if code, out, _ := run(t, "--no-update-check", "--version"); code != ExitOK || out == "" {
		t.Errorf("code %d, output %q; want 0 and the version", code, out)
	}
}

func TestNoUpdateCheckHidden(t *testing.T) {
	_, out, _ := run(t, "--help")
	if strings.Contains(out, "no-update-check") {
		t.Errorf("--no-update-check shows in the help:\n%s", out)
	}
}

func TestUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing command", nil, "Error: Missing command."},
		{"unknown command", []string{"bogus"}, "Error: No such command 'bogus'."},
		{"unknown option", []string{"--bogus"}, "Error: unknown flag: --bogus"},
	}
	for _, c := range cases {
		code, out, errOut := run(t, c.args...)
		if code != ExitUsage {
			t.Errorf("%s: exit code %d, want %d", c.name, code, ExitUsage)
		}
		if out != "" {
			t.Errorf("%s: unexpected stdout %q", c.name, out)
		}
		if !strings.Contains(errOut, c.want) || !strings.HasPrefix(errOut, "Usage: frankenmanager") {
			t.Errorf("%s: stderr = %q, want usage and %q", c.name, errOut, c.want)
		}
	}
}
