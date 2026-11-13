package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

// startedEnv is a running server with app.test on PHP 8.4, projects
// root known.
func startedEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t)
	e.initEnv("MARIADB_ROOT_PASSWORD=secret\n")
	s := e.store()
	if err := s.AddDomains([]string{"app.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(store.SettingProjectPath, e.projects()); err != nil {
		t.Fatal(err)
	}
	e.docker.ps = running
	e.docker.handle = func(args []string) docker.Result {
		if args[0] == "image" {
			return docker.Result{ExitCode: 1}
		}
		return docker.Result{}
	}
	return e
}

func (e *testEnv) site(version, simple string) string {
	return filepath.Join(e.dir, "caddy", "sites", "php-"+version, simple+"_Caddyfile")
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func mustContain(t *testing.T, out string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains(out, line) {
			t.Errorf("missing %q in:\n%s", line, out)
		}
	}
}

func TestUsageErrorsOfDomainCommands(t *testing.T) {
	e := newEnv(t)
	for args, want := range map[string]string{
		"add-host":           "Error: Missing argument 'DOMAINS'.",
		"add-alias app.test": "Error: Missing argument 'ALIASES'.",
		"remove-host":        "Error: Missing argument 'DOMAINS'.",
		"switch-php a.test":  "Error: Missing option '--php'.",
		"add-host a b c":     "Error: Got unexpected extra argument (c)",
	} {
		code, _, errOut := e.run(strings.Fields(args)...)
		if code != ExitUsage || !strings.Contains(errOut, want) {
			t.Errorf("%s: code %d, %q", args, code, errOut)
		}
	}
	code, out, _ := e.run("restore-host")
	if code != 1 || out != "Specify domains to restore or use --list to see archived hosts.\n" {
		t.Errorf("restore-host: code %d, %q", code, out)
	}
}

func TestAddHost(t *testing.T) {
	e := startedEnv(t)
	code, out, _ := e.run("add-host", "new.test app.test new.test")
	if code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	mustContain(t, out,
		"-- Domain 'app.test' is already configured, skipping.\n",
		"-- Domain 'new.test' is duplicated in input, skipping.\n",
		"-- Adding 1 new domain(s) with PHP 8.3...\n",
		"-- Starting PHP 8.3 container...\n",
		"\n-- New host(s) added successfully!\n-- Added domains:\n--   - https://new.test (PHP 8.3)\n",
	)
	if !slices.Equal(e.dockerLines()[1:], []string{
		"image inspect --format {{json .Config.Labels}} custom-frankenphp:8.3",
		"build custom-frankenphp:8.3",
		"up -d --remove-orphans",
		"restart franken-caddy-proxy",
	}) {
		t.Errorf("docker:\n%s", strings.Join(e.dockerLines(), "\n"))
	}
	if !exists(e.site("8.3", "new")) || !strings.Contains(readFile(t, e.hostsFile), "127.0.0.1\tnew.test\n") {
		t.Error("site file or hosts entry missing")
	}
	if v, _, _ := e.store().DomainPHPVersion("new.test"); v != "8.3" {
		t.Errorf("version %q", v)
	}
	compose := readFile(t, filepath.Join(e.dir, "docker-compose-generated.yml"))
	if !strings.Contains(compose, "frankenphp-8.3") || !strings.Contains(compose, "frankenphp-8.4") {
		t.Errorf("compose without both versions:\n%s", compose)
	}

	// Existing version: its container is restarted
	e.docker.calls = nil
	if code, out, _ := e.run("add-host", "other.test", "--php", "8.4"); code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	if !slices.Equal(e.dockerLines()[1:], []string{"restart frankenphp-8.4", "restart franken-caddy-proxy"}) {
		t.Errorf("docker: %v", e.dockerLines())
	}
}

func TestAddHostFailureRemovesHostsEntries(t *testing.T) {
	e := startedEnv(t)
	if err := e.store().SetSetting(store.SettingProjectPath, ""); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, e.hostsFile)
	code, out, _ := e.run("add-host", "new.test", "--php", "8.5")
	mustContain(t, out,
		"ERROR: An error occurred: No project path known: set DEFAULT_PROJECT_PATH (frankenmanager configuration set) or restart the server with --path.\n",
		"ERROR: Cleanup complete. The host(s) were not added.\n",
	)
	if code != 1 || readFile(t, e.hostsFile) != before {
		t.Errorf("code %d, hosts:\n%s", code, readFile(t, e.hostsFile))
	}
	// The domain is not left in the database
	if names, _ := e.store().DomainNames(); !slices.Equal(names, []string{"app.test"}) {
		t.Errorf("domains = %v", names)
	}
}

// A failed restore leaves neither the domain in the database nor its
// site file outside the archive.
func TestRestoreHostFailure(t *testing.T) {
	e := startedEnv(t)
	if err := e.store().SetSetting(store.SettingProjectPath, ""); err != nil {
		t.Fatal(err)
	}
	archived := filepath.Join(e.dir, "caddy", "sites", "archive", "old_Caddyfile")
	writeFile(t, archived, "http://old.test {\n}\n")
	code, out, _ := e.run("restore-host", "old.test", "--php", "8.5")
	if code != 1 || !strings.HasSuffix(out, "ERROR: Cleanup complete. The host(s) were not restored.\n") {
		t.Errorf("code %d, out:\n%s", code, out)
	}
	if names, _ := e.store().DomainNames(); !slices.Equal(names, []string{"app.test"}) {
		t.Errorf("domains = %v", names)
	}
	if !exists(archived) || exists(e.site("8.5", "old")) {
		t.Error("site file not back in the archive")
	}
}

func TestAddProxyHost(t *testing.T) {
	e := startedEnv(t)
	code, out, _ := e.run("add-host", "vite.test", "ftp://x")
	if code != 1 || !strings.HasSuffix(out, "ERROR: Invalid proxy target: ftp://x\n") {
		t.Errorf("code %d, out %q", code, out)
	}
	code, out, _ = e.run("add-host", "vite.test", "127.0.0.1:5173")
	if code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	mustContain(t, out, "-- Adding 1 new proxy host(s) -> 127.0.0.1:5173...\n", "--   - https://vite.test -> 127.0.0.1:5173\n")
	if p, _, _ := e.store().ProxyTarget("vite.test"); p != "127.0.0.1:5173" {
		t.Errorf("proxy %q", p)
	}
	if !strings.Contains(readFile(t, filepath.Join(e.dir, "caddy", "Caddyfile")), "vite.test {") {
		t.Error("proxy missing from the Caddyfile")
	}
}

func TestAddAlias(t *testing.T) {
	e := startedEnv(t)
	code, out, _ := e.run("add-alias", "nope.test", "alt.test")
	if code != 1 || !strings.HasSuffix(out, "ERROR: Domain 'nope.test' is not configured. Add it first with 'add-host'.\n") {
		t.Errorf("code %d, out %q", code, out)
	}
	code, out, _ = e.run("add-alias", "app.test", "app.test alt.test")
	if code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	mustContain(t, out,
		"-- 'app.test' is the target domain itself, skipping.\n",
		"--   - https://alt.test -> app.test (PHP 8.4)\n",
	)
	if target, _, _ := e.store().AliasTarget("alt.test"); target != "app.test" {
		t.Errorf("target %q", target)
	}
}

func TestRemoveHost(t *testing.T) {
	e := startedEnv(t)
	s := e.store()
	if err := s.AddDomains([]string{"keep.test"}, "8.3"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAliases([]string{"alt.test"}, "app.test"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, e.site("8.4", "app"), "http://app.test {\n}\n")

	code, out, _ := e.run("remove-host", "app.test ghost.test app.test")
	if code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	mustContain(t, out,
		"WARNING: Domain 'ghost.test' is not configured, skipping.\n",
		"-- Domain 'app.test' is duplicated in input, skipping.\n",
		"-- Also removing alternate host(s) pointing to removed domain(s): alt.test\n",
		"-- Stopping frankenphp-8.4 (no more domains)...\n",
		"-- \nCaddyfiles archived to: ",
		"-- Removed alternate hosts:\n--   - alt.test\n",
	)
	if !slices.Equal(e.dockerLines()[1:], []string{"stop frankenphp-8.4", "rm frankenphp-8.4", "restart franken-caddy-proxy"}) {
		t.Errorf("docker: %v", e.dockerLines())
	}
	if exists(e.site("8.4", "app")) || !exists(filepath.Join(e.dir, "caddy", "sites", "archive", "app_Caddyfile")) {
		t.Error("site file not archived")
	}
	if names, _ := s.DomainNames(); !slices.Equal(names, []string{"keep.test"}) {
		t.Errorf("domains %v", names)
	}
	if strings.Contains(readFile(t, filepath.Join(e.dir, "docker-compose-generated.yml")), "frankenphp-8.4") {
		t.Error("compose still runs 8.4")
	}
}

func TestRestoreHost(t *testing.T) {
	e := startedEnv(t)
	code, out, _ := e.run("restore-host", "--list")
	if code != 0 || !strings.HasPrefix(out, "-- No archived hosts found.\n-- Archive directory: ") {
		t.Errorf("code %d, out %q", code, out)
	}

	writeFile(t, filepath.Join(e.dir, "caddy", "sites", "archive", "old_Caddyfile"), "http://old.test {\n}\n")
	_, out, _ = e.run("restore-host", "-l")
	mustContain(t, out, "Archived Hosts", "│ old.test │ old_Caddyfile │")

	code, out, _ = e.run("restore-host", "old.test app.dev", "--php", "8.4")
	if code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	mustContain(t, out,
		"-- Domain 'app.dev' is already active, skipping.\n",
		"-- Restored old_Caddyfile (PHP 8.4)\n",
		"--   - https://old.test (PHP 8.4)\n",
	)
	if !exists(e.site("8.4", "old")) {
		t.Error("site file not restored")
	}
}

func TestSwitchPHP(t *testing.T) {
	e := startedEnv(t)
	code, out, _ := e.run("switch-php", "ghost.test", "--php", "8.4")
	// Exit code 1
	if code != 1 || !strings.HasSuffix(out, "ERROR: Domain 'ghost.test' is not configured.\n") {
		t.Errorf("code %d, out %q", code, out)
	}

	if err := e.store().AddDomains([]string{"other.test"}, "8.3"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, e.site("8.4", "app"), "http://app.test {\n}\n")
	e.docker.calls = nil
	code, out, _ = e.run("switch-php", "app.test", "--php", "8.3")
	if code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	mustContain(t, out, "-- Switching 'app.test' from PHP 8.4 to PHP 8.3...\n", "\n-- Switched 'app.test' to PHP 8.3!\n")
	if !slices.Equal(e.dockerLines()[1:], []string{
		"restart frankenphp-8.3", "stop frankenphp-8.4", "rm frankenphp-8.4", "restart franken-caddy-proxy",
	}) {
		t.Errorf("docker: %v", e.dockerLines())
	}
	if !exists(e.site("8.3", "app")) || exists(e.site("8.4", "app")) {
		t.Error("site file not moved")
	}
}

// A failed switch moves the site file and the version back.
func TestSwitchPHPRollback(t *testing.T) {
	e := startedEnv(t)
	if err := e.store().SetSetting(store.SettingProjectPath, ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, e.site("8.4", "app"), "http://app.test {\n}\n")
	code, out, _ := e.run("switch-php", "app.test", "--php", "8.5")
	if code != 1 || !strings.HasSuffix(out, "ERROR: Rolled back version change.\n") {
		t.Errorf("code %d, out:\n%s", code, out)
	}
	if v, _, _ := e.store().DomainPHPVersion("app.test"); v != "8.4" || !exists(e.site("8.4", "app")) {
		t.Errorf("not rolled back: %q", v)
	}
}
