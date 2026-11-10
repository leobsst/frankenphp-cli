package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

// initEnv creates .env from .env.example, as the first start does, then
// appends extra (a later key wins).
func (e *testEnv) initEnv(extra string) {
	e.t.Helper()
	if _, err := env.New(filepath.Join(e.dir, ".env"), filepath.Join(e.dir, ".env.example")).EnsureExists(); err != nil {
		e.t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(e.dir, ".env"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString("\n" + extra); err != nil {
		e.t.Fatal(err)
	}
}

func (e *testEnv) envValue(key string) string {
	e.t.Helper()
	v := env.New(filepath.Join(e.dir, ".env"), "")
	if err := v.Load(); err != nil {
		e.t.Fatal(err)
	}
	return v.Get(key)
}

// projects returns a projects root for --path.
func (e *testEnv) projects() string {
	p := filepath.Join(e.root, "projects")
	if err := os.MkdirAll(p, 0o755); err != nil {
		e.t.Fatal(err)
	}
	return p
}

// dockerLines returns the docker calls, the noisy prefix of compose and
// the build arguments dropped.
func (e *testEnv) dockerLines() []string {
	var lines []string
	for _, c := range e.docker.calls {
		line := strings.Join(c, " ")
		line = strings.TrimPrefix(line, "--log-level error compose -f docker-compose-generated.yml ")
		if c[0] == "build" {
			line = "build " + c[5] // --tag <image>
		}
		lines = append(lines, line)
	}
	return lines
}

const running = `{"Names":"franken-caddy-proxy","State":"running","Status":"Up"}
{"Names":"frankenphp-8.4","State":"running","Status":"Up"}`

func TestStartCreatesEnvFirst(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("start")
	if code != 1 || !strings.Contains(out, "-- Created .env file at "+filepath.Join(e.dir, ".env")+". Please review settings, then try again.") {
		t.Errorf("code %d, out:\n%s", code, out)
	}
	if len(e.docker.calls) != 0 || strings.Contains(out, "ERROR") {
		t.Errorf("docker %v, out %s", e.docker.calls, out)
	}
}

func TestStart(t *testing.T) {
	e := newEnv(t)
	e.initEnv("")
	e.docker.handle = func(args []string) docker.Result {
		if args[0] == "image" { // no image yet
			return docker.Result{ExitCode: 1}
		}
		return docker.Result{}
	}
	code, out, _ := e.run("start", "app.test api.test app.test", "--php", "8.4", "--path", e.projects())
	if code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	for _, line := range []string{
		"-- MariaDB password generated and saved to .env\n",
		"-- Added 127.0.0.1\tapp.test to hosts file\n",
		"-- Building Docker image for PHP 8.4...\n",
		"\n-- Web server started!\n--   PHP 8.4: app.test, api.test\n",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("missing %q in:\n%s", line, out)
		}
	}
	if e.envValue("MARIADB_ROOT_PASSWORD") == "" {
		t.Error("no MariaDB password")
	}
	if !slices.Equal(e.dockerLines(), []string{
		"ps --all --no-trunc --format json",
		"image inspect --format {{json .Config.Labels}} custom-frankenphp:8.4",
		"build custom-frankenphp:8.4",
		"down",
		"up -d --remove-orphans",
		"exec franken_mariadb mariadb -u root -p" + e.envValue("MARIADB_ROOT_PASSWORD") + " -e SELECT 1",
	}) {
		t.Errorf("docker calls:\n%s", strings.Join(e.dockerLines(), "\n"))
	}

	hostsContent := readFile(t, e.hostsFile)
	for _, line := range []string{"127.0.0.1\tapp.test\n", "::1\t\tapi.test\n"} {
		if !strings.Contains(hostsContent, line) {
			t.Errorf("hosts:\n%s", hostsContent)
		}
	}
	s := e.store()
	domains, _ := s.Domains()
	if !slices.Equal(domains, []store.Domain{{Name: "app.test", PHPVersion: "8.4"}, {Name: "api.test", PHPVersion: "8.4"}}) {
		t.Errorf("domains = %v", domains)
	}
	if p, _, _ := s.Setting(store.SettingProjectPath); p != e.projects() {
		t.Errorf("project path = %q", p)
	}
	if main := readFile(t, filepath.Join(e.dir, "caddy", "Caddyfile")); !strings.Contains(main, "app.test {") {
		t.Errorf("Caddyfile:\n%s", main)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "caddy", "sites", "php-8.4", "app_Caddyfile")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "caddy", "certs", "rootCA.pem")); err != nil {
		t.Error("CA not copied into the certs")
	}
}

// Nothing is written to .env when the server already runs.
func TestStartAlreadyRunning(t *testing.T) {
	e := newEnv(t)
	e.initEnv("SOKETI_ENABLED=true\n")
	e.docker.ps = running
	if err := e.store().AddDomains([]string{"app.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	code, out, _ := e.run("start")
	if code != 1 || out != "ERROR: The server is already running.\n" {
		t.Errorf("code %d, out %q", code, out)
	}
	if e.envValue("MARIADB_ROOT_PASSWORD") != "" || e.envValue("SOKETI_DEFAULT_APP_KEY") != "" {
		t.Error(".env written")
	}
}

func TestStartErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		out  string
	}{
		// An error message
		"invalid php": {[]string{"start", "app.test", "--php", "7.4"}, "ERROR: Unsupported PHP version '7.4'"},
		"no path":     {[]string{"start", "app.test"}, "ERROR: No project path specified and DEFAULT_PROJECT_PATH is not set in .env\n"},
		"bad path":    {[]string{"start", "app.test", "-p", "/nonexistent"}, "ERROR: Directory does not exist: /nonexistent\n"},
		"bad domain":  {[]string{"start", "bad_domain.test", "-p", "."}, "ERROR: Invalid domain name: bad_domain.test\n"},
		"extra arg":   {[]string{"start", "a.test", "b.test"}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.initEnv("")
			code, out, errOut := e.run(tc.args...)
			if name == "extra arg" {
				if code != ExitUsage || !strings.Contains(errOut, "Got unexpected extra argument (b.test)") {
					t.Errorf("code %d, %q", code, errOut)
				}
				return
			}
			if code != 1 || !strings.Contains(out, tc.out) {
				t.Errorf("code %d, out %q", code, out)
			}
			if len(e.dockerLines()) > 1 {
				t.Errorf("docker calls %v", e.dockerLines())
			}
		})
	}
}

// A failure puts the hosts file and the registered domains back.
func TestStartFailureCleansUp(t *testing.T) {
	e := newEnv(t)
	e.initEnv("")
	if err := e.store().AddDomains([]string{"old.test"}, "8.3"); err != nil {
		t.Fatal(err)
	}
	e.docker.handle = func(args []string) docker.Result {
		if slices.Contains(args, "up") {
			return docker.Result{ExitCode: 1, Stderr: []byte("port 443 is already allocated")}
		}
		return docker.Result{}
	}
	hostsBefore := readFile(t, e.hostsFile)
	code, out, _ := e.run("start", "new.test", "-p", e.projects())
	if code != 1 || !strings.Contains(out, "ERROR: An error occurred: ") ||
		!strings.Contains(out, "ERROR: Cleaning up...\n-- Removed old.test from hosts file\n") ||
		!strings.HasSuffix(out, "ERROR: Cleanup complete. The server did not start.\n") {
		t.Errorf("code %d, out:\n%s", code, out)
	}
	if got := readFile(t, e.hostsFile); got != hostsBefore {
		t.Errorf("hosts not restored:\n%s", got)
	}
	domains, _ := e.store().Domains()
	if !slices.Equal(domains, []store.Domain{{Name: "old.test", PHPVersion: "8.3"}}) {
		t.Errorf("domains = %v", domains)
	}
}

func TestStop(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("stop")
	if code != 1 || out != "ERROR: The server is already stopped.\n" {
		t.Errorf("code %d, out %q", code, out)
	}

	s := e.store()
	if err := s.AddDomains([]string{"app.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddProxies([]string{"vite.test"}, "127.0.0.1:5173"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, e.hostsFile, "127.0.0.1\tlocalhost\n127.0.0.1\tapp.test\n::1\t\tapp.test\n"+
		"127.0.0.1\tvite.test\n127.0.0.1\tws.frankenmanager.test\n")
	writeFile(t, filepath.Join(e.dir, ".dns_watcher.pid"), "424242")
	e.docker.ps = running

	code, out, _ = e.run("stop")
	if code != 0 || !strings.HasPrefix(out, "-- Stopping web server...\n") || !strings.HasSuffix(out, "\n-- Web server stopped!\n") {
		t.Errorf("code %d, out:\n%s", code, out)
	}
	if got := readFile(t, e.hostsFile); got != "127.0.0.1\tlocalhost\n" {
		t.Errorf("hosts:\n%s", got)
	}
	if !slices.Contains(e.dockerLines(), "down") || !slices.Equal(e.killed, []int{424242}) {
		t.Errorf("docker %v, killed %v", e.dockerLines(), e.killed)
	}
}

func TestRestartSomeContainers(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("restart")
	if code != 1 || out != "ERROR: The server is not running.\n" {
		t.Errorf("code %d, out %q", code, out)
	}

	e.initEnv("MARIADB_ROOT_PASSWORD=secret\n")
	if err := e.store().AddDomains([]string{"app.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	e.docker.ps = running
	e.docker.calls = nil
	// The order of restartTargets whatever the command line order; --db is
	// --database
	code, out, _ = e.run("restart", "--cache", "--db", "--caddy", "--ws")
	if code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	for _, line := range []string{
		"-- Restarting caddy, database, cache, soketi...\n",
		"\n-- Generating SSL certificates...\n",
		"WARNING: Redis is disabled: set REDIS_ENABLED=true in .env to use it.\n",
		"WARNING: Soketi is disabled: set SOKETI_ENABLED=true in .env to use it.\n",
		"\n-- Container(s) restarted: caddy, database, cache, soketi\n",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("missing %q in:\n%s", line, out)
		}
	}
	want := []string{
		"ps --all --no-trunc --format json",
		"restart frankenphp-8.4",
		"restart franken-caddy-proxy",
		"restart franken_mariadb",
		"exec franken_mariadb mariadb -u root -psecret -e SELECT 1",
	}
	if !slices.Equal(e.dockerLines(), want) {
		t.Errorf("docker calls:\n%s", strings.Join(e.dockerLines(), "\n"))
	}
}

func TestRestartBuildWithoutProjectPath(t *testing.T) {
	e := newEnv(t)
	e.initEnv("")
	if err := e.store().AddDomains([]string{"app.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	e.docker.ps = running
	code, out, _ := e.run("restart", "--dns", "--build")
	want := "ERROR: No project path known: set DEFAULT_PROJECT_PATH in .env or use 'stop' then 'start --build --path <path>'.\n"
	if code != 1 || !strings.HasSuffix(out, want) {
		t.Errorf("code %d, out:\n%s", code, out)
	}
}
