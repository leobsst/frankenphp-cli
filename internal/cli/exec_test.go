package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

func TestContainerPath(t *testing.T) {
	for in, want := range map[string]string{
		`/home/me/app`:      "/home/me/app",
		`C:\Users\me\app`:   "/c/Users/me/app",
		`d:\projects\`:      "/d/projects",
		`D:\`:               "/d",
		`/Users/me/My Apps`: "/Users/me/My Apps",
	} {
		if got := containerPath(in); got != want {
			t.Errorf("containerPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExecMounts(t *testing.T) {
	// Resolved, like the cwd exec passes (macOS: /var is /private/var)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "real")
	app := filepath.Join(real, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks not available:", err) // Windows without the privilege
	}
	other := filepath.Join(root, "other")

	for _, tc := range []struct {
		cwd, projects string
		want          []string
	}{
		{app, real, []string{real}},          // inside the projects root
		{real, real, []string{real}},         // the root itself
		{other, real, []string{real, other}}, // outside
		{app, link, []string{link}},          // root given through a symlink
		{app, filepath.Join(root, "gone"), []string{app}},
		{app, "", []string{app}},
		{filepath.Join(root, "realx"), real, []string{real, filepath.Join(root, "realx")}}, // prefix, not parent
	} {
		if got := execMounts(tc.cwd, tc.projects); !slices.Equal(got, tc.want) {
			t.Errorf("execMounts(%q, %q) = %v, want %v", tc.cwd, tc.projects, got, tc.want)
		}
	}
}

// execEnv records the docker run line instead of running it.
func execEnv(t *testing.T, code int) (*testEnv, *[]string) {
	t.Helper()
	e := newEnv(t)
	e.initEnv("")
	var ran []string
	e.interactive = func(_ context.Context, name string, args []string) (int, error) {
		ran = append([]string{name}, args...)
		return code, nil
	}
	return e, &ran
}

func TestExec(t *testing.T) {
	e, ran := execEnv(t, 3)
	cwd, _ := os.Getwd()
	cwd, _ = filepath.EvalSymlinks(cwd)

	// The command's exit code; -v and --help belong to the command
	code, out, errOut := e.run("exec", "8.4", "php", "-v", "--help")
	if code != 3 || out != "" || errOut != "" {
		t.Errorf("code %d, out %q, err %q", code, out, errOut)
	}
	want := []string{
		"docker", "run", "--rm", "-i", "--network", "host",
		"--user", execUser(runtime.GOOS, e.loadEnv()), "--entrypoint", "",
		"-e", "HOME=/tmp", "-e", "COMPOSER_CACHE_DIR=/tmp/composer-cache",
		"-e", "GIT_CONFIG_COUNT=1", "-e", "GIT_CONFIG_KEY_0=safe.directory", "-e", "GIT_CONFIG_VALUE_0=*",
		"-v", cwd + ":" + containerPath(cwd),
		"-v", filepath.Join(e.dir, "php", "8.4", "php.ini") + ":/usr/local/etc/php/php.ini:ro",
		"-w", containerPath(cwd), "custom-frankenphp:8.4", "php", "-v", "--help",
	}
	if !slices.Equal(*ran, want) {
		t.Errorf("docker run:\n%s\nwant:\n%s", strings.Join(*ran, " "), strings.Join(want, " "))
	}
	if last := e.dockerLines()[len(e.dockerLines())-1]; last != "image inspect --format {{.Id}} custom-frankenphp:8.4" {
		t.Errorf("image check = %q", last)
	}
}

func TestExecDomainAliasAndShell(t *testing.T) {
	e, ran := execEnv(t, 0)
	e.initEnv("APP_ENV=prod\n")
	s := e.store()
	if err := s.AddDomains([]string{"app.test"}, "8.3"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAliases([]string{"alt.test"}, "app.test"); err != nil {
		t.Fatal(err)
	}
	projects := e.projects()
	if err := s.SetSetting(store.SettingProjectPath, projects); err != nil {
		t.Fatal(err)
	}
	e.tty = true

	code, _, _ := e.run("exec", "alt.test", "php artisan migrate && php artisan cache:clear")
	got := strings.Join(*ran, " ")
	for _, part := range []string{
		"docker run --rm -i -t --network host",
		"-v " + projects + ":" + containerPath(projects) + " -v ",
		":/usr/local/etc/php/conf.d/99-production.ini:ro",
		"custom-frankenphp:8.3 sh -c php artisan migrate && php artisan cache:clear",
	} {
		if !strings.Contains(got, part) {
			t.Errorf("missing %q in %s", part, got)
		}
	}
	if code != 0 {
		t.Errorf("code %d", code)
	}
}

func TestExecErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		args  []string
		setup func(e *testEnv)
		code  int
		out   string
	}{
		"no command":  {[]string{"exec", "8.4", "  "}, nil, 1, "ERROR: No command given.\n"},
		"unknown":     {[]string{"exec", "ghost.test", "php"}, nil, 1, "ERROR: Domain 'ghost.test' is not configured.\n"},
		"proxy":       {[]string{"exec", "vite.test", "php"}, func(e *testEnv) { _ = e.store().AddProxies([]string{"vite.test"}, "127.0.0.1:5173") }, 1, "ERROR: 'vite.test' is a proxy host: it has no PHP version.\n"},
		"no image":    {[]string{"exec", "8.5", "php"}, func(e *testEnv) { e.docker.handle = func([]string) docker.Result { return docker.Result{ExitCode: 1} } }, 1, "ERROR: Image 'custom-frankenphp:8.5' not found: start the server (or add a host) with PHP 8.5 to build it.\n"},
		"missing all": {[]string{"exec"}, nil, ExitUsage, ""},
		"missing cmd": {[]string{"exec", "8.4"}, nil, ExitUsage, ""},
		"help":        {[]string{"exec", "--help"}, nil, 0, "Run a command with a PHP version"},
		"double dash": {[]string{"exec", "--", "8.4", "php"}, nil, 0, ""},
	} {
		t.Run(name, func(t *testing.T) {
			e, ran := execEnv(t, 0)
			if tc.setup != nil {
				tc.setup(e)
			}
			code, out, errOut := e.run(tc.args...)
			if code != tc.code || !strings.Contains(out, tc.out) {
				t.Errorf("code %d, out %q, err %q", code, out, errOut)
			}
			ranOK := name == "double dash"
			if (len(*ran) > 0) != ranOK {
				t.Errorf("docker run = %v", *ran)
			}
			switch name {
			case "missing all":
				if !strings.Contains(errOut, "Error: Missing argument 'DOMAIN|VERSION'.") {
					t.Errorf("err %q", errOut)
				}
			case "missing cmd":
				if !strings.Contains(errOut, "Error: Missing argument 'COMMAND...'.") {
					t.Errorf("err %q", errOut)
				}
			}
		})
	}
}

// A domain runs with its own PHP version's image.
func TestExecDomain(t *testing.T) {
	e, ran := execEnv(t, 0)
	if err := e.store().AddDomains([]string{"app.test"}, "8.5"); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := e.run("exec", "app.test", "php", "-v"); code != 0 {
		t.Errorf("code %d", code)
	}
	if got := strings.Join(*ran, " "); !strings.Contains(got, " custom-frankenphp:8.5 php -v") ||
		!strings.Contains(got, filepath.Join("php", "8.5", "php.ini")+":/usr/local/etc/php/php.ini:ro") {
		t.Errorf("docker run: %s", got)
	}
}

// The root option before exec, as in the spawned processes and scripts,
// is not part of the command, and still skips the update notice. After
// DOMAIN, it belongs to the command.
func TestExecAfterRootOption(t *testing.T) {
	e, ran := execEnv(t, 0)
	if code, out, errOut := e.run("--no-update-check", "exec", "8.4", "php", "--no-update-check"); code != 0 {
		t.Fatalf("code %d, out %q, err %q", code, out, errOut)
	}
	if got := strings.Join(*ran, " "); !strings.HasSuffix(got, " custom-frankenphp:8.4 php --no-update-check") {
		t.Errorf("docker run: %s", got)
	}
	if len(e.notified) != 0 {
		t.Errorf("notified %v", e.notified)
	}
	// Without it, exec announces updates like any command
	e.run("exec", "8.4", "php")
	if len(e.notified) != 1 {
		t.Errorf("notified %v", e.notified)
	}
}

// loadEnv reads the configuration of the test data directory.
func (e *testEnv) loadEnv() *env.Env {
	e.t.Helper()
	v := env.New(store.ConfigDB{Path: filepath.Join(e.dir, "db.sqlite")})
	if err := v.Load(); err != nil {
		e.t.Fatal(err)
	}
	return v
}
