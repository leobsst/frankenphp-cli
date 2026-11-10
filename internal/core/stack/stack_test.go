package stack

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/compose"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

func newEnv(t *testing.T, dir, content string) *env.Env {
	t.Helper()
	e := env.New(filepath.Join(dir, ".env"), filepath.Join(dir, ".env.example"))
	if err := os.WriteFile(e.Path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	return e
}

// fakeRunner records the docker calls; Docker is never reached.
type fakeRunner struct {
	args [][]string
	envs []map[string]string
}

func (f *fakeRunner) Run(_ context.Context, _ string, env map[string]string, args ...string) (docker.Result, error) {
	f.args = append(f.args, args)
	f.envs = append(f.envs, env)
	if args[0] == "image" {
		return docker.Result{ExitCode: 1}, nil // no image yet
	}
	return docker.Result{}, nil
}

type memLog struct{ lines []string }

func (l *memLog) Info(msg string)    { l.lines = append(l.lines, "info: "+msg) }
func (l *memLog) Warning(msg string) { l.lines = append(l.lines, "warning: "+msg) }

func TestProjectPath(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	for _, tc := range []struct{ env, setting, want string }{
		{"", "", ""},
		{"DEFAULT_PROJECT_PATH=/default\n", "", "/default"},
		{"DEFAULT_PROJECT_PATH=/default\n", "/stored", "/stored"},
	} {
		if tc.setting != "" {
			if err := s.SetSetting(store.SettingProjectPath, tc.setting); err != nil {
				t.Fatal(err)
			}
		}
		got, err := ProjectPath(newEnv(t, dir, tc.env), s)
		if err != nil || got != tc.want {
			t.Errorf("ProjectPath = %q, %v; want %q", got, err, tc.want)
		}
	}
}

func TestComposeOptionsFromEnv(t *testing.T) {
	e := newEnv(t, t.TempDir(), "APP_ENV=production\nDB_ENGINES=postgresql\nREDIS_ENABLED=true\nSOKETI_ENABLED=true\n")
	got := ComposeOptions(e, []string{"8.4"}, "0.0.0.0:53")
	want := compose.Options{PHPVersions: []string{"8.4"}, Production: true, DBEngines: []string{"postgresql"},
		DNSListen: "0.0.0.0:53", Redis: true, Websocket: true}
	if !slices.Equal(got.DBEngines, want.DBEngines) || got.Production != want.Production ||
		got.DNSListen != want.DNSListen || got.Redis != want.Redis || got.Websocket != want.Websocket ||
		!slices.Equal(got.PHPVersions, want.PHPVersions) {
		t.Errorf("got %+v", got)
	}
}

func TestComposeUpWritesFileThenStarts(t *testing.T) {
	dir := t.TempDir()
	e := newEnv(t, dir, "UID=501\nGID=20\nMARIADB_ROOT_PASSWORD=secret\n")
	r := &fakeRunner{}
	d := &docker.Manager{Dir: dir, Runner: r, Log: &memLog{}}

	if err := ComposeUp(context.Background(), d, e, []string{"8.3"}, "", "/Users/me/Sites"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, compose.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != compose.Build(compose.Options{PHPVersions: []string{"8.3"}}) {
		t.Error("compose file differs from compose.Build")
	}
	if len(r.args) != 1 || !slices.Contains(r.args[0], "--remove-orphans") {
		t.Fatalf("calls = %v", r.args)
	}
	if v := r.envs[0]; v["CUSTOM_PATH"] != "/Users/me/Sites" || v["UID"] != "501" || v["PWD"] != dir {
		t.Errorf("compose env = %v", v)
	}
}

func TestComposeUpMissingUID(t *testing.T) {
	dir := t.TempDir()
	r := &fakeRunner{}
	d := &docker.Manager{Dir: dir, Runner: r, Log: &memLog{}}
	if err := ComposeUp(context.Background(), d, newEnv(t, dir, ""), nil, "", ""); err == nil || len(r.args) != 0 {
		t.Errorf("err=%v calls=%v", err, r.args)
	}
}

func TestBuildImagesFromEnv(t *testing.T) {
	dir := t.TempDir()
	e := newEnv(t, dir, "WWWGROUP=20\nEXTRA_APT_PACKAGES=vim\nEXTRA_APK_PACKAGES_84=git\n"+
		"EXTRA_COMPOSER_PACKAGES=laravel/installer\nINSTALL_PDFTK=true\n")
	r := &fakeRunner{}
	log := &memLog{}
	d := &docker.Manager{Dir: dir, Runner: r, Log: log}

	if err := BuildImages(context.Background(), d, e, log, "/sites", []string{"8.4", "8.2"}, false); err != nil {
		t.Fatal(err)
	}
	var builds []string
	for _, a := range r.args {
		if a[0] == "build" {
			builds = append(builds, strings.Join(a, " "))
		}
	}
	if len(builds) != 2 || !strings.Contains(builds[0], "PHP_IMAGE_TAG=1-php8.2-alpine") {
		t.Fatalf("builds = %v", builds)
	}
	for _, arg := range []string{"WWWGROUP=20", "EXTRA_SYSTEM_PACKAGES=git", "EXTRA_COMPOSER_PACKAGES=laravel/installer",
		"INSTALL_PDFTK=true", "CUSTOM_PATH=/sites"} {
		if !strings.Contains(builds[1], "--build-arg "+arg) {
			t.Errorf("8.4 build misses %s: %s", arg, builds[1])
		}
	}
	if strings.Contains(builds[0], "EXTRA_SYSTEM_PACKAGES") {
		t.Errorf("8.2 build has system packages: %s", builds[0])
	}
	warning := "warning: EXTRA_APT_PACKAGES is ignored on Alpine images: use EXTRA_APK_PACKAGES, " +
		"or set PHP_IMAGE_DISTRO=trixie."
	if log.lines[0] != warning {
		t.Errorf("log = %v", log.lines)
	}

	bad := newEnv(t, dir, "PHP_IMAGE_DISTRO=debian\n")
	if _, err := BuildImage(context.Background(), d, bad, log, "", "8.3", false); err == nil {
		t.Error("no error for an unsupported distro")
	}
}
