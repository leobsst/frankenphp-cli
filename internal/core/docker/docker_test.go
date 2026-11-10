package docker

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// call is one docker invocation seen by fakeRunner.
type call struct {
	dir  string
	env  map[string]string
	args []string
}

// fakeRunner records the calls and answers with respond (exit 0 when nil).
type fakeRunner struct {
	calls   []call
	respond func(args []string) (Result, error)
}

func (f *fakeRunner) Run(_ context.Context, dir string, env map[string]string, args ...string) (Result, error) {
	f.calls = append(f.calls, call{dir, maps.Clone(env), args})
	if f.respond == nil {
		return Result{}, nil
	}
	return f.respond(args)
}

func (f *fakeRunner) argLines() []string {
	var lines []string
	for _, c := range f.calls {
		lines = append(lines, strings.Join(c.args, " "))
	}
	return lines
}

type memLog struct{ lines []string }

func (l *memLog) Info(msg string) { l.lines = append(l.lines, msg) }

func newManager(t *testing.T, respond func([]string) (Result, error)) (*Manager, *fakeRunner, *memLog) {
	t.Helper()
	r := &fakeRunner{respond: respond}
	log := &memLog{}
	return &Manager{Dir: t.TempDir(), Runner: r, Log: log}, r, log
}

// The testdata fingerprints are those of earlier releases, with non-ASCII,
// control and quote characters in the build arguments: they must stay
// identical so that existing images are not rebuilt.
func TestBuildHashMatchesReference(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "build_hash.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Dockerfile *string           `json:"dockerfile"`
		Entrypoint *string           `json:"entrypoint"`
		Args       map[string]string `json:"args"`
		Hash       string            `json:"hash"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		m := &Manager{Dir: t.TempDir()}
		if c.Dockerfile != nil {
			writeFile(t, filepath.Join(m.Dir, "Dockerfile"), *c.Dockerfile)
		}
		if c.Entrypoint != nil {
			writeFile(t, filepath.Join(m.Dir, "docker", "entrypoint.sh"), *c.Entrypoint)
		}
		got, err := m.BuildHash(c.Args)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.Hash {
			t.Errorf("case %d: hash %s, reference %s (json %s)", i, got, c.Hash, buildHashJSON(c.Args))
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildHashJSON(t *testing.T) {
	if got := buildHashJSON(map[string]string{}); got != "{}" {
		t.Errorf("empty = %s", got)
	}
	got := buildHashJSON(map[string]string{"b": "é\"🐘", "a": "x"})
	if want := `{"a": "x", "b": "\u00e9\"\ud83d\udc18"}`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestAllContainers(t *testing.T) {
	got := AllContainers(Containers{
		PHPVersions: []string{"8.5", "8.3"},
		DBEngines:   []string{"postgresql", "mysql"},
		DNS:         true, Redis: true, Websocket: true,
	})
	want := []string{
		"frankenphp-8.3", "frankenphp-8.5", "franken-caddy-proxy", "franken_redis", "franken_soketi",
		"franken_postgresql", "franken_mysql", "franken_phpmyadmin", "franken_pgadmin", "franken_dns",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v", got)
	}
	got = AllContainers(Containers{Production: true})
	if want := []string{"franken-caddy-proxy", "franken_mariadb"}; !slices.Equal(got, want) {
		t.Errorf("production default = %v", got)
	}
}

func TestBuildImageSkipsCurrentImage(t *testing.T) {
	m, r, log := newManager(t, nil)
	b := Build{CustomPath: "/sites", PHPVersion: "8.3"}
	args, _ := b.Args()
	hash, _ := m.BuildHash(args)
	r.respond = func(a []string) (Result, error) {
		return Result{Stdout: []byte(`{"` + BuildHashLabel + `":"` + hash + `"}` + "\n")}, nil
	}

	built, err := m.BuildImage(context.Background(), b)
	if err != nil || built {
		t.Fatalf("built=%v err=%v", built, err)
	}
	if want := []string{"image inspect --format {{json .Config.Labels}} custom-frankenphp:8.3"}; !slices.Equal(r.argLines(), want) {
		t.Errorf("calls = %v", r.argLines())
	}
	if !slices.Equal(log.lines, []string{"PHP 8.3 image is up to date, skipping build"}) {
		t.Errorf("log = %v", log.lines)
	}
}

func TestBuildImageBuildsOutdatedImage(t *testing.T) {
	for name, inspect := range map[string]Result{
		"other hash": {Stdout: []byte(`{"` + BuildHashLabel + `":"old"}`)},
		"no labels":  {Stdout: []byte("null\n")},
		"missing":    {ExitCode: 1},
	} {
		t.Run(name, func(t *testing.T) {
			m, r, log := newManager(t, func(a []string) (Result, error) {
				if a[0] == "image" {
					return inspect, nil
				}
				return Result{}, nil
			})
			b := Build{CustomPath: "/sites", PHPVersion: "8.4", Distro: "trixie", WWWGroup: "20", InstallPdftk: true}
			built, err := m.BuildImage(context.Background(), b)
			if err != nil || !built {
				t.Fatalf("built=%v err=%v", built, err)
			}
			args, _ := b.Args()
			hash, _ := m.BuildHash(args)
			want := "build --quiet --pull --rm --tag custom-frankenphp:8.4 --label " + BuildHashLabel + "=" + hash +
				" --build-arg CUSTOM_PATH=/sites --build-arg INSTALL_PDFTK=true" +
				" --build-arg PHP_IMAGE_TAG=1-php8.4-trixie --build-arg WWWGROUP=20 " + m.Dir
			if got := r.argLines()[1]; got != want {
				t.Errorf("build call:\n got %s\nwant %s", got, want)
			}
			if log.lines[0] != "Building Docker image for PHP 8.4..." {
				t.Errorf("log = %v", log.lines)
			}
		})
	}
}

func TestBuildImageForceAndFailure(t *testing.T) {
	m, r, _ := newManager(t, func([]string) (Result, error) {
		return Result{ExitCode: 1, Stderr: []byte("ERROR: pull access denied\n")}, nil
	})
	_, err := m.BuildImage(context.Background(), Build{PHPVersion: "8.3", Force: true})
	var dErr *Error
	if !errors.As(err, &dErr) || dErr.Msg != "Failed to build image for PHP 8.3: ERROR: pull access denied" {
		t.Errorf("err = %v", err)
	}
	if len(r.calls) != 1 || r.calls[0].args[0] != "build" {
		t.Errorf("force must skip the inspection: %v", r.argLines())
	}

	if _, err := m.BuildImage(context.Background(), Build{PHPVersion: "7.4"}); err == nil || len(r.calls) != 1 {
		t.Errorf("unsupported version: err=%v calls=%v", err, r.argLines())
	}
}

func TestComposeUpAndDown(t *testing.T) {
	m, r, _ := newManager(t, nil)
	ctx := context.Background()
	vars := map[string]string{"UID": "501"}
	if err := m.ComposeUp(ctx, vars); err != nil {
		t.Fatal(err)
	}
	if err := m.ComposeUp(ctx, nil, "dns"); err != nil {
		t.Fatal(err)
	}
	m.ComposeDown(ctx)
	want := []string{
		"--log-level error compose -f docker-compose-generated.yml up -d --remove-orphans",
		"--log-level error compose -f docker-compose-generated.yml up -d --no-deps dns",
		"--log-level error compose -f docker-compose-generated.yml down",
	}
	if !slices.Equal(r.argLines(), want) {
		t.Errorf("calls = %v", r.argLines())
	}
	if r.calls[0].dir != m.Dir || r.calls[0].env["UID"] != "501" {
		t.Errorf("first call = %+v", r.calls[0])
	}

	r.respond = func([]string) (Result, error) { return Result{ExitCode: 1, Stderr: []byte("port taken\n")}, nil }
	if err := m.ComposeUp(ctx, nil); err == nil || err.Error() != "Failed to start containers: port taken\n" {
		t.Errorf("err = %v", err)
	}
	m.ComposeDown(ctx) // failures ignored
}

func TestContainerCommands(t *testing.T) {
	m, r, _ := newManager(t, func(a []string) (Result, error) {
		switch {
		case slices.Contains(a, "missing"):
			return Result{ExitCode: 1, Stderr: []byte("No such container")}, nil
		case a[0] == "exec":
			return Result{ExitCode: 3, Stdout: []byte("out\n"), Stderr: []byte("err\n")}, nil
		}
		return Result{}, nil
	})
	ctx := context.Background()
	if !m.RestartContainer(ctx, "franken_redis") || m.RestartContainer(ctx, "missing") {
		t.Error("RestartContainer")
	}
	if !m.StopContainer(ctx, "frankenphp-8.2") || m.StopContainer(ctx, "missing") {
		t.Error("StopContainer")
	}
	if code, out := m.Exec(ctx, "franken_mariadb", "mariadb", "-e", "SELECT 1"); code != 3 || out != "out\nerr\n" {
		t.Errorf("Exec = %d %q", code, out)
	}
	want := []string{
		"restart franken_redis", "restart missing",
		"stop frankenphp-8.2", "rm frankenphp-8.2", "stop missing",
		"exec franken_mariadb mariadb -e SELECT 1",
	}
	if !slices.Equal(r.argLines(), want) {
		t.Errorf("calls = %v", r.argLines())
	}

	m.Runner = &fakeRunner{respond: func([]string) (Result, error) {
		return Result{ExitCode: -1}, errors.New("docker not found")
	}}
	if code, out := m.Exec(ctx, "x", "true"); code != -1 || out != "docker not found" {
		t.Errorf("Exec without docker = %d %q", code, out)
	}
}

func TestContainerStatuses(t *testing.T) {
	m, r, _ := newManager(t, func([]string) (Result, error) {
		return Result{Stdout: []byte(`{"Names":"franken-caddy-proxy","State":"running","Status":"Up 27 hours"}
{"Names":"frankenphp-8.3,other","State":"running","Status":"Up 2 minutes (healthy)"}
not json
{"Names":"franken_mariadb","State":"exited","Status":"Exited (0) 3 days ago"}
{"Names":"franken_redis","State":"","Status":"Up 5 seconds (health: starting)"}
{"Names":"franken_mysql","State":"running","Status":"Up 3 minutes (unhealthy)"}
`)}, nil
	})
	s := m.ContainerStatuses(context.Background())
	want := Statuses{
		"franken-caddy-proxy": {"running", "N/A"},
		"frankenphp-8.3":      {"running", "healthy"},
		"other":               {"running", "healthy"},
		"franken_mariadb":     {"exited", "N/A"},
		"franken_redis":       {"unknown", "starting"},
		"franken_mysql":       {"running", "unhealthy"}, // not "healthy", its substring
	}
	if !maps.Equal(s, want) {
		t.Errorf("statuses = %v", s)
	}
	if s.Get("nope") != NotFound {
		t.Error("missing container is not NotFound")
	}
	if want := []string{"ps --all --no-trunc --format json"}; !slices.Equal(r.argLines(), want) {
		t.Errorf("calls = %v", r.argLines())
	}

	m.Runner = &fakeRunner{respond: func([]string) (Result, error) { return Result{ExitCode: 1}, nil }}
	if s := m.ContainerStatuses(context.Background()); len(s) != 0 {
		t.Errorf("unreachable Docker = %v", s)
	}
}

func TestIsRunning(t *testing.T) {
	up := Status{State: "running"}
	proxyOnly := Statuses{"franken-caddy-proxy": up}
	withPHP := Statuses{"franken-caddy-proxy": up, "frankenphp-8.4": up}
	for _, tc := range []struct {
		s        Statuses
		versions []string
		want     bool
	}{
		{Statuses{}, nil, false},
		{proxyOnly, nil, true},
		{proxyOnly, []string{"8.3"}, false},
		{withPHP, []string{"8.3", "8.4"}, true},
		{Statuses{"frankenphp-8.4": up}, []string{"8.4"}, false},
	} {
		if got := IsRunning(tc.s, tc.versions); got != tc.want {
			t.Errorf("IsRunning(%v, %v) = %v", tc.s, tc.versions, got)
		}
	}
}
