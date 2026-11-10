package ssl

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/command/commandtest"
)

// setup returns a Manager whose fake mkcert writes the files it is asked
// for, with a CA in caRoot.
func setup(t *testing.T) (*Manager, *commandtest.Runner, *commandtest.Log, string) {
	t.Helper()
	caRoot := t.TempDir()
	for _, f := range []string{"rootCA.pem", "rootCA-key.pem"} {
		if err := os.WriteFile(filepath.Join(caRoot, f), []byte(f), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := &commandtest.Runner{Paths: map[string]string{"mkcert": "mkcert"}}
	r.Handle = func(c command.Cmd) (command.Result, error) {
		if c.Args[0] == "-CAROOT" {
			return command.Result{Stdout: []byte(caRoot + "\n")}, nil
		}
		for _, f := range []string{c.Args[1], c.Args[3]} {
			if err := os.WriteFile(f, []byte("cert"), 0o600); err != nil {
				return command.Result{}, err
			}
		}
		return command.Result{}, nil
	}
	log := &commandtest.Log{}
	m, err := New(filepath.Join(t.TempDir(), "certs"), log)
	if err != nil {
		t.Fatal(err)
	}
	m.Run = r
	return m, r, log, caRoot
}

func TestGenerateAll(t *testing.T) {
	m, r, log, _ := setup(t)
	now := time.Now()
	m.Now = func() time.Time { return now }
	// a.test: 10 days old, kept; b.test: 40 days old, regenerated
	for d, age := range map[string]time.Duration{"a.test": 10 * 24 * time.Hour, "b.test": 40 * 24 * time.Hour} {
		for _, f := range []string{d + ".pem", d + "-key.pem"} {
			p := filepath.Join(m.Dir, f)
			_ = os.WriteFile(p, nil, 0o600)
			_ = os.Chtimes(p, now.Add(-age), now.Add(-age))
		}
	}

	if err := m.GenerateAll(context.Background(), []string{"a.test", "b.test", "c.test"}, false, false); err != nil {
		t.Fatal(err)
	}
	cert := func(d string) string {
		return "mkcert -cert-file " + filepath.Join(m.Dir, d+".pem") + " -key-file " + filepath.Join(m.Dir, d+"-key.pem") + " " + d
	}
	want := []string{"mkcert -CAROOT", cert("localhost"), cert("b.test"), cert("c.test")}
	if !slices.Equal(r.Lines(), want) {
		t.Errorf("calls = %v", r.Lines())
	}
	wantLog := []string{
		"info: Creating SSL certificates for: a.test, b.test, c.test",
		"info: Certificate for a.test still valid (10 days old), skipping",
		"success: SSL certificates generated!",
	}
	if !slices.Equal(log.Lines, wantLog) {
		t.Errorf("log = %v", log.Lines)
	}
	if data, _ := os.ReadFile(filepath.Join(m.Dir, "rootCA.pem")); string(data) != "rootCA.pem" {
		t.Error("CA not copied")
	}
	entries, _ := os.ReadDir(m.Dir)
	for _, e := range entries {
		if info, _ := e.Info(); runtime.GOOS != "windows" && info.Mode().Perm() != 0o750 { // Windows keeps no Unix mode
			t.Errorf("%s mode %v", e.Name(), info.Mode())
		}
	}

	// force regenerates, production skips the domains
	r.Calls = nil
	if err := m.GenerateAll(context.Background(), []string{"a.test"}, true, false); err != nil {
		t.Fatal(err)
	}
	if len(r.Calls) != 3 {
		t.Errorf("force: %v", r.Lines())
	}
	r.Calls = nil
	if err := m.GenerateAll(context.Background(), []string{"a.test"}, true, true); err != nil {
		t.Fatal(err)
	}
	if len(r.Calls) != 2 {
		t.Errorf("production: %v", r.Lines())
	}
}

func TestInstallCAErrors(t *testing.T) {
	m, r, _, caRoot := setup(t)
	ctx := context.Background()

	_ = os.Remove(filepath.Join(caRoot, "rootCA-key.pem"))
	if err := m.InstallCA(ctx); err == nil || !strings.HasPrefix(err.Error(), "No root CA installed yet.") {
		t.Errorf("missing key: %v", err)
	}

	r.Paths = nil
	if err := m.InstallCA(ctx); err == nil || err.Error() != "mkcert not found. Please install mkcert first." {
		t.Errorf("no mkcert: %v", err)
	}

	r.Paths = map[string]string{"mkcert": "mkcert"}
	r.Handle = func(command.Cmd) (command.Result, error) { return command.Result{ExitCode: 1}, nil }
	if err := m.InstallCA(ctx); err == nil || !strings.HasPrefix(err.Error(), "Could not determine mkcert's CA root.") {
		t.Errorf("no CAROOT: %v", err)
	}
	if err := m.GenerateLocalhost(ctx); err == nil || !strings.HasPrefix(err.Error(), "Failed to generate localhost certificate") {
		t.Errorf("localhost: %v", err)
	}
}
