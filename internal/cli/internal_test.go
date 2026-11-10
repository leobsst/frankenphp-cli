package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/resources"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

// The command lines the services and processes of earlier releases run.
func TestHiddenCommands(t *testing.T) {
	e := newEnv(t)
	var served []string
	e.serveCA = func(_ context.Context, cert string, port int) error {
		served = []string{cert, fmt.Sprint(port)}
		if port == 1 {
			return errors.New("listen tcp4 0.0.0.0:1: bind: permission denied")
		}
		return nil
	}
	var watched string
	e.watchDNS = func(_ context.Context, dir string, stderr io.Writer) error {
		watched = dir
		_, _ = fmt.Fprintln(stderr, "watching")
		return nil
	}

	code, out, errOut := e.run("--no-update-check", "_serve-ca-internal", "/ca/rootCA.pem", "9080")
	if code != 0 || out != "" || errOut != "" || strings.Join(served, " ") != "/ca/rootCA.pem 9080" {
		t.Errorf("serve: code %d, out %q, err %q, served %v", code, out, errOut, served)
	}
	// Errors go to stderr: trust-ca on shows the last line of its log
	code, out, errOut = e.run("--no-update-check", "_serve-ca-internal", "/ca/rootCA.pem", "1")
	if code != 1 || out != "" || errOut != "listen tcp4 0.0.0.0:1: bind: permission denied\n" {
		t.Errorf("serve error: code %d, out %q, err %q", code, out, errOut)
	}
	code, _, errOut = e.run("--no-update-check", "_serve-ca-internal", "/ca/rootCA.pem", "x")
	if code != ExitUsage || !strings.Contains(errOut, "Error: Invalid value for 'PORT': 'x' is not a valid integer.") {
		t.Errorf("bad port: code %d, err %q", code, errOut)
	}

	code, _, errOut = e.run("--no-update-check", "_watch-dns-internal")
	if code != 0 || watched != e.dir || errOut != "watching\n" {
		t.Errorf("watch: code %d, dir %q, err %q", code, watched, errOut)
	}

	// Hidden commands never announce updates, even without --no-update-check
	e.run("_watch-dns-internal")
	if len(e.notified) != 0 {
		t.Errorf("notified %v", e.notified)
	}
}

func TestHiddenFromHelp(t *testing.T) {
	e := newEnv(t)
	_, out, _ := e.run("--help")
	for _, name := range []string{"_serve-ca-internal", "_watch-dns-internal", "_sync-resources-internal", "_refresh-update-cache", "completion"} {
		if strings.Contains(out, name) {
			t.Errorf("%s shows in the help", name)
		}
	}
}

// The previous updater's `update` runs this on the new binary and expects
// 0 or 3.
func TestSyncResourcesExitCodes(t *testing.T) {
	e := newEnv(t)
	if code, out, _ := e.run("--no-update-check", "_sync-resources-internal"); code != 0 || out != "" {
		t.Errorf("initialized: code %d, out %q", code, out)
	}

	fresh := filepath.Join(t.TempDir(), "never-initialized")
	t.Setenv(platform.DataDirEnv, fresh)
	if code, out, _ := e.run("--no-update-check", "_sync-resources-internal"); code != 3 || out != "" {
		t.Errorf("not initialized: code %d, out %q", code, out)
	}
	if exists(filepath.Join(fresh, resources.InitializedMarker)) {
		t.Error("initialized the data directory")
	}
}

func TestRefreshUpdateCache(t *testing.T) {
	e := newEnv(t)
	calls := 0
	e.refresh = func(_ context.Context, dir string, _ interface{ Info(string) }) error {
		calls++
		if dir != e.dir {
			t.Errorf("dir %q", dir)
		}
		if calls == 2 {
			return errors.New("Could not reach GitHub (Forbidden).")
		}
		return nil
	}
	if code, out, _ := e.run("--no-update-check", "_refresh-update-cache"); code != 0 || out != "" {
		t.Errorf("code %d, out %q", code, out)
	}
	// A failure is silent: the next command retries
	if code, out, errOut := e.run("--no-update-check", "_refresh-update-cache"); code != 1 || out != "" || errOut != "" {
		t.Errorf("failure: code %d, out %q, err %q", code, out, errOut)
	}
}
