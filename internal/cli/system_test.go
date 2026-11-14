package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/privilege"
)

// The reference (testdata/qr) is the output of earlier releases. The two
// encoders lay the data modules out differently, so the codes are compared
// on what they share: size, characters, border, finder patterns and format
// information (same error correction level and mask).
func TestQRMatchesReference(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "qr"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := qrASCII("http://192.168.1.20:9080/")
	if err != nil {
		t.Fatal(err)
	}
	want, got := strings.Split(string(data), "\n"), strings.Split(out, "\n")
	if len(got) != len(want) {
		t.Fatalf("%d lines, reference %d:\n%s", len(got), len(want), out)
	}
	for i := range want {
		g, w := []rune(got[i]), []rune(want[i])
		if len(g) != len(w) {
			t.Fatalf("line %d: %d columns, reference %d", i, len(g), len(w))
		}
		for _, r := range g {
			if !strings.ContainsRune("\u00a0▀▄█", r) {
				t.Fatalf("line %d: unexpected %q", i, r)
			}
		}
		// Finder patterns and format information: the first 9 columns of
		// the top 5 lines, and the last 9 of the top 4 lines
		if i < 5 && string(g[:9]) != string(w[:9]) {
			t.Errorf("line %d starts %q, reference %q", i, string(g[:9]), string(w[:9]))
		}
		if i < 4 && string(g[len(g)-9:]) != string(w[len(w)-9:]) {
			t.Errorf("line %d ends %q, reference %q", i, string(g[len(g)-9:]), string(w[len(w)-9:]))
		}
	}
}

// privilegeFiles points the privilege manager at temporary files, as a
// non-root user.
func (e *testEnv) privilegeFiles(helper string) (helperPath, sudoersPath string) {
	e.t.Helper()
	helperPath = filepath.Join(e.root, "bin", "frankenmanager-hosts-helper")
	sudoersPath = filepath.Join(e.root, "sudoers.d", "frankenmanager")
	if helper != "" {
		writeFile(e.t, helperPath, helper)
		writeFile(e.t, sudoersPath, "rules")
	}
	e.privilege = func(log privilege.Logger) *privilege.Manager {
		return &privilege.Manager{
			GOOS: "darwin", HostsPath: e.hostsFile, HelperPath: helperPath, SudoersPath: sudoersPath,
			Run: e.cmd, Log: log, IsAdmin: func() bool { return false },
			Owner: func(string, int, int) error { return nil },
		}
	}
	return helperPath, sudoersPath
}

func TestSetupStatus(t *testing.T) {
	e := newEnv(t)
	e.privilegeFiles("")
	delete(e.cmd.Paths, "mkcert")
	code, out, _ := e.run("setup", "--status")
	if code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	mustContain(t, out,
		"Data Directory", "│ Location    │ "+e.dir+" │", "│ Mode        │ Production", "│ Initialized │ Yes",
		"Privilege Configuration Status", "│ Platform                │ darwin │", "│ Sudoers Configured      │ No     │",
		"Dependencies Status", "│ mkcert     │ Not installed │",
		"-- To complete setup, run:\n--   sudo frankenmanager setup\n",
	)
}

func TestSetup(t *testing.T) {
	e := newEnv(t)
	delete(e.cmd.Paths, "mkcert")

	// Not configured, not root: the error of privilege.Setup
	e.privilegeFiles("")
	code, out, _ := e.run("setup")
	if code != 1 || !strings.HasSuffix(out, "ERROR: Setup requires root privileges. Please run with: sudo frankenmanager setup\n") {
		t.Errorf("code %d, out:\n%s", code, out)
	}

	// A helper installed by an earlier release is replaced
	e.privilegeFiles("#!/bin/bash\n# old helper\n")
	code, out, _ = e.run("setup")
	if code != 1 || !strings.Contains(out, "-- Updating the hosts helper script...\n") {
		t.Errorf("code %d, out:\n%s", code, out)
	}

	// Up to date, without mkcert: the install instructions
	e.privilegeFiles(privilege.HelperScript(e.hostsFile))
	code, out, _ = e.run("setup")
	if code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	mustContain(t, out,
		"-- Privilege escalation is already configured.\n",
		"-- mkcert is not installed.\n-- To install mkcert automatically, run:\n--   sudo frankenmanager setup --install-mkcert\n",
		"-- To install mkcert on macOS:\n",
		"-- Setup complete!\n",
	)
}

func TestSetupRemove(t *testing.T) {
	e := newEnv(t)
	e.privilegeFiles("")
	code, out, _ := e.run("setup", "-r")
	want := "-- Removing privilege escalation configuration...\n-- \n-- Privilege escalation is not configured. Nothing to remove.\n"
	if code != 0 || out != want {
		t.Errorf("code %d, out %q", code, out)
	}
	e.privilegeFiles(privilege.HelperScript(e.hostsFile))
	code, out, _ = e.run("setup", "--remove")
	if code != 1 || !strings.HasSuffix(out, "ERROR: Removal requires root privileges. Please run with: sudo frankenmanager setup --remove\n") {
		t.Errorf("code %d, out %q", code, out)
	}
}

func TestTrustCA(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("trust-ca", "status")
	if code != 0 || out != "-- Root CA sharing is currently OFF.\n" {
		t.Errorf("status: code %d, out %q", code, out)
	}
	code, out, _ = e.run("trust-ca", "off")
	if code != 1 || out != "ERROR: Root CA sharing is not currently running.\n" {
		t.Errorf("off: code %d, out %q", code, out)
	}
	code, out, _ = e.run("trust-ca", "maybe")
	if code != 1 || out != "Unknown action 'maybe'. Use on, off, or status.\n" {
		t.Errorf("unknown: code %d, out %q", code, out)
	}

	code, out, _ = e.run("trust-ca", "on", "-p", "9443")
	if code != 0 {
		t.Fatalf("on: code %d, out:\n%s", code, out)
	}
	if !slices.Equal(e.shared, []string{e.dir, filepath.Join(e.root, "caroot", "rootCA.pem"), "9443"}) {
		t.Errorf("shared %v", e.shared)
	}
	qr, _ := qrASCII("http://192.168.1.20:9443/")
	mustContain(t, out,
		"-- Root CA is now shared at http://192.168.1.20:9443/\n"+qr+"\n-- Scan the QR code",
		"WARNING: Anyone on your network can reach this port while sharing is on.",
	)

	e.caPort = 9443
	code, out, _ = e.run("trust-ca", "status")
	if code != 0 || out != "-- Root CA sharing is ON at http://192.168.1.20:9443/\n" {
		t.Errorf("status on: code %d, out %q", code, out)
	}
	code, out, _ = e.run("trust-ca", "off")
	if code != 0 || out != "-- Root CA sharing stopped.\n" {
		t.Errorf("off: code %d, out %q", code, out)
	}

	delete(e.cmd.Paths, "mkcert")
	code, out, _ = e.run("trust-ca", "on")
	if code != 1 || out != "ERROR: mkcert not found. Run `sudo frankenmanager setup --install-mkcert` first.\n" {
		t.Errorf("no mkcert: code %d, out %q", code, out)
	}
}

func TestReset(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("reset")
	if code != 1 || out != "ERROR: You must specify at least one option: --db or --caddyfiles\n" {
		t.Errorf("code %d, out %q", code, out)
	}

	s := e.store()
	if err := s.AddDomains([]string{"app.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("project_path", "/projects"); err != nil {
		t.Fatal(err)
	}
	site := filepath.Join(e.dir, "caddy", "sites", "php-8.4", "app_Caddyfile")
	other := filepath.Join(e.dir, "caddy", "sites", "php-8.3", "old_Caddyfile")
	archived := filepath.Join(e.dir, "caddy", "sites", "archive", "gone_Caddyfile")
	for _, f := range []string{site, other, archived} {
		writeFile(t, f, "x")
	}

	// Refused: nothing changes
	e.stdin = "n\n"
	code, out, _ = e.run("reset", "--database", "--caddy")
	mustContain(t, out,
		"\n⚠️  Warning: You are about to reset:\n  • FrankenManager configuration (domain list)\n  • custom Caddyfiles\n",
		"\nThis action cannot be undone!\n\nDo you want to proceed? [y/N]: -- Reset cancelled.\n",
	)
	if names, _ := s.DomainNames(); code != 0 || len(names) != 1 || !exists(site) {
		t.Errorf("code %d, domains %v", code, names)
	}

	// End of input: aborted
	e.stdin = ""
	code, _, errOut := e.run("reset", "--db")
	if code != 1 || errOut != "\nAborted!\n" {
		t.Errorf("abort: code %d, err %q", code, errOut)
	}

	// Invalid answer, then yes
	e.stdin = "maybe\nyes\n"
	code, out, _ = e.run("reset", "--db", "--caddyfiles")
	if code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	mustContain(t, out, "Error: invalid input\nDo you want to proceed? [y/N]: ",
		"-- Configuration reset successfully!\n", "-- Deleted 2 Caddyfile(s)\n", "\n-- Reset completed successfully!\n")
	if names, _ := s.DomainNames(); len(names) != 0 {
		t.Errorf("domains %v", names)
	}
	if p, _, _ := s.Setting("project_path"); p != "/projects" {
		t.Error("settings cleared")
	}
	if exists(site) || exists(other) || !exists(archived) {
		t.Error("wrong Caddyfiles deleted")
	}

	// Refused while running
	if err := s.AddDomains([]string{"app.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	e.docker.ps = running
	code, out, _ = e.run("reset", "--db")
	if code != 1 || out != "ERROR: The server is currently running. Please stop the server first with 'frankenmanager stop'.\n" {
		t.Errorf("running: code %d, out %q", code, out)
	}
}

// No site file, or no sites directory at all: nothing to delete, no error.
func TestResetCaddyfilesWithoutSites(t *testing.T) {
	e := newEnv(t)
	for _, prepare := range []func(){
		func() {}, // the extracted, empty directories
		func() { _ = os.RemoveAll(filepath.Join(e.dir, "caddy", "sites")) },
	} {
		prepare()
		e.stdin = "y\n"
		code, out, _ := e.run("reset", "--caddyfiles")
		if code != 0 || !strings.Contains(out, "-- No custom Caddyfiles found to delete\n") {
			t.Errorf("code %d, out:\n%s", code, out)
		}
	}
}
