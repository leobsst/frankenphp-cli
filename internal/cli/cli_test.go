package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/command/commandtest"
	"github.com/leobsst/frankenphp-cli/internal/core/dnswatcher"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/hosts"
	"github.com/leobsst/frankenphp-cli/internal/core/mkcert"
	"github.com/leobsst/frankenphp-cli/internal/core/privilege"
	"github.com/leobsst/frankenphp-cli/internal/core/resources"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/core/updater"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

// fakeDocker answers the docker commands: ps returns ps, handle (when
// set) the others, success otherwise.
type fakeDocker struct {
	ps     string
	handle func(args []string) docker.Result
	calls  [][]string
}

func (f *fakeDocker) Run(_ context.Context, _ string, _ map[string]string, args ...string) (docker.Result, error) {
	f.calls = append(f.calls, args)
	if args[0] == "ps" {
		return docker.Result{Stdout: []byte(f.ps)}, nil
	}
	if f.handle != nil {
		return f.handle(args), nil
	}
	return docker.Result{}, nil
}

// Stream records the call like Run, marked "stream", and writes fake
// build logs: the docker runner of --verbose builds.
func (f *fakeDocker) Stream(_ context.Context, _ string, out io.Writer, args ...string) (int, error) {
	f.calls = append(f.calls, append([]string{"stream"}, args...))
	_, _ = io.WriteString(out, "#1 [internal] load build definition from Dockerfile\n")
	return 0, nil
}

type quietLog struct{}

func (quietLog) Info(string)    {}
func (quietLog) Warning(string) {}

// testEnv is a test environment: an initialized data directory, and fakes
// for Docker, the other programs (mkcert, sudo, launchctl), the hosts file,
// the DNS watcher and the update notice. Nothing outside the temporary
// directory is touched.
type testEnv struct {
	t         *testing.T
	root, dir string
	docker    *fakeDocker
	cmd       *commandtest.Runner
	hostsFile string
	notified  []string
	killed    []int
	// interactive replaces the terminal programs (exec); tty is the
	// terminal answer
	interactive func(ctx context.Context, name string, args []string) (int, error)
	tty         bool
	// privilege replaces the privilege manager; shared records ShareCA,
	// caPort is the port of a running CA server (0: none); stdin answers
	// the prompts
	privilege func(log privilege.Logger) *privilege.Manager
	shared    []string
	caPort    int
	stdin     string
	// serveCA, watchDNS and refresh replace the work of the hidden
	// commands: nothing listens, watches or reaches GitHub
	serveCA  func(ctx context.Context, cert string, port int) error
	watchDNS func(ctx context.Context, dir string, stderr io.Writer) error
	refresh  func(ctx context.Context, dir string, log interface{ Info(string) }) error
	// updater replaces the release manager (fake GitHub)
	updater func(dataDir string, log updater.Logger) *updater.Updater
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "data")
	t.Setenv(platform.DataDirEnv, dir)
	if err := resources.New(dir, quietLog{}).Ensure(); err != nil {
		t.Fatal(err)
	}
	e := &testEnv{t: t, root: root, dir: dir, docker: &fakeDocker{}, hostsFile: filepath.Join(root, "hosts")}
	writeFile(t, e.hostsFile, "127.0.0.1\tlocalhost\n")

	// mkcert's CA root, installed by setup
	caRoot := filepath.Join(root, "caroot")
	writeFile(t, filepath.Join(caRoot, "rootCA.pem"), "CA")
	writeFile(t, filepath.Join(caRoot, "rootCA-key.pem"), "KEY")
	e.cmd = &commandtest.Runner{
		Paths: map[string]string{"mkcert": "/bin/mkcert"},
		Handle: func(c command.Cmd) (command.Result, error) {
			switch {
			case c.Name == "/bin/mkcert" && c.Args[0] == "-CAROOT":
				return command.Result{Stdout: []byte(caRoot + "\n")}, nil
			case c.Name == "sudo" && c.Args[0] == "tee":
				writeFile(t, c.Args[1], string(c.Stdin))
			}
			return command.Result{}, nil
		},
	}
	return e
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

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// hosts edits the temporary hosts file through the fake sudo, without the
// privilege helper.
func (e *testEnv) hosts(log hosts.Logger) *hosts.Manager {
	return &hosts.Manager{Path: e.hostsFile, GOOS: "darwin", Run: e.cmd, Log: log, IsAdmin: func() bool { return false }}
}

// watcher runs launchctl through the fake runner (not found: detached)
// and never starts or signals a process.
func (e *testEnv) watcher(dataDir string) (*dnswatcher.Watcher, error) {
	return &dnswatcher.Watcher{
		DataDir:   dataDir,
		GOOS:      "darwin",
		Exe:       "/usr/local/lib/frankenmanager/frankenmanager",
		PlistPath: filepath.Join(e.root, "LaunchAgents", dnswatcher.LaunchdLabel+".plist"),
		UnitPath:  filepath.Join(e.root, "systemd", dnswatcher.SystemdUnit),
		UID:       501,
		Run:       e.cmd,
		Sleep:     func(time.Duration) {},
		Spawn:     func(*dnswatcher.Watcher) (int, error) { return 424242, nil },
		Alive:     func(pid int) bool { return pid == 424242 },
		Kill:      func(pid int) error { e.killed = append(e.killed, pid); return nil },
	}, nil
}

func (e *testEnv) run(args ...string) (code int, out, errOut string) {
	e.t.Helper()
	var o, eo bytes.Buffer
	deps := Deps{
		Docker:  e.docker,
		Command: e.cmd,
		Hosts:   e.hosts,
		Watcher: e.watcher,
		Interactive: func(ctx context.Context, name string, args []string) (int, error) {
			if e.interactive == nil {
				e.t.Errorf("unexpected %s %v", name, args)
				return 1, nil
			}
			return e.interactive(ctx, name, args)
		},
		TTY: func() bool { return e.tty },
		Privilege: func(log privilege.Logger) *privilege.Manager {
			if e.privilege == nil {
				e.t.Error("unexpected privilege manager")
				return &privilege.Manager{}
			}
			return e.privilege(log)
		},
		Mkcert: func(log mkcert.Logger) *mkcert.Installer {
			return &mkcert.Installer{GOOS: "darwin", GOARCH: "arm64", Run: e.cmd, Log: log,
				IsAdmin: func() bool { return false }, BinDir: filepath.Join(e.root, "bin"), Home: e.root}
		},
		ShareCA: func(dataDir, certFile string, port int) error {
			e.shared = []string{dataDir, certFile, strconv.Itoa(port)}
			return nil
		},
		StopCA: func(string) bool {
			running := e.caPort != 0
			e.caPort = 0
			return running
		},
		CAState: func(string) (int, int, bool) { return 424242, e.caPort, e.caPort != 0 },
		LANIP:   func() string { return "192.168.1.20" },
		Stdin:   strings.NewReader(e.stdin),
		ServeCA: func(ctx context.Context, cert string, port int) error {
			if e.serveCA == nil {
				e.t.Error("unexpected CA server")
				return nil
			}
			return e.serveCA(ctx, cert, port)
		},
		WatchDNS: func(ctx context.Context, dir string, stderr io.Writer) error {
			if e.watchDNS == nil {
				e.t.Error("unexpected DNS watcher")
				return nil
			}
			return e.watchDNS(ctx, dir, stderr)
		},
		Updater: func(dataDir string, log updater.Logger) *updater.Updater {
			if e.updater == nil {
				e.t.Error("unexpected updater")
				// Never the real GitHub
				return &updater.Updater{Version: "dev", DataDir: dataDir, Log: log, APIURL: "https://api.test/",
					Client: &http.Client{Transport: fakeGitHub{code: 500}}, Now: time.Now}
			}
			return e.updater(dataDir, log)
		},
		Refresh: func(ctx context.Context, dir string, log updater.Logger) error {
			if e.refresh == nil {
				e.t.Error("unexpected GitHub lookup")
				return nil
			}
			return e.refresh(ctx, dir, log)
		},
		Notify: func(dataDir string, _ updater.Logger) { e.notified = append(e.notified, dataDir) },
	}
	code = ExecuteWith(args, &o, &eo, deps)
	return code, o.String(), eo.String()
}

// store opens the data directory's database for setup.
func (e *testEnv) store() *store.Store {
	e.t.Helper()
	s, err := store.Open(filepath.Join(e.dir, "db.sqlite"))
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = s.Close() })
	return s
}

func (e *testEnv) writeEnv(content string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.dir, ".env"), []byte(content), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

func TestUpdateNotice(t *testing.T) {
	e := newEnv(t)
	e.run("list")
	e.run("--no-update-check", "list")
	e.run("--version")
	e.run()
	if !slices.Equal(e.notified, []string{e.dir}) {
		t.Errorf("notified = %v", e.notified)
	}
}

func TestExtraArgument(t *testing.T) {
	e := newEnv(t)
	code, _, errOut := e.run("list", "a", "b")
	if code != ExitUsage || !strings.Contains(errOut, "Error: Got unexpected extra arguments (a b)") {
		t.Errorf("code %d, %q", code, errOut)
	}
}

func TestListEmpty(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("list")
	want := "\nNo domains registered.\nUse 'frankenmanager start \"myapp.test\"' to add domains.\n\n"
	if code != 0 || out != want {
		t.Errorf("code %d, out %q", code, out)
	}
}

func TestList(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	if err := s.AddDomains([]string{"app.test", "api.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAliases([]string{"www.app.test"}, "app.test"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddProxies([]string{"vite.test"}, "127.0.0.1:5173"); err != nil {
		t.Fatal(err)
	}
	code, out, _ := e.run("list")
	want := `
Registered Domains
╭──────────┬─────╮
│ Domain   │ PHP │
├──────────┼─────┤
│ app.test │ 8.4 │
│ api.test │ 8.4 │
╰──────────┴─────╯

2 domain(s) registered

   Alternate Hosts (Aliases)
╭──────────────┬───────────────╮
│ Alias        │ Target Domain │
├──────────────┼───────────────┤
│ www.app.test │ app.test      │
╰──────────────┴───────────────╯

1 alternate host(s) registered

         Proxy Hosts
╭───────────┬────────────────╮
│ Domain    │ Upstream       │
├───────────┼────────────────┤
│ vite.test │ 127.0.0.1:5173 │
╰───────────┴────────────────╯

1 proxy host(s) registered

`
	if code != 0 || out != want {
		t.Errorf("code %d, out:\n%s", code, out)
	}
}

func TestStatusStopped(t *testing.T) {
	e := newEnv(t)
	code, out, _ := e.run("status")
	want := `
=== FrankenPHP Server Status ===

Status: stopped

Containers:
╭───────────────────────────┬──────────────┬──────────────╮
│ Container                 │ Status       │ Health       │
├───────────────────────────┼──────────────┼──────────────┤
│ franken-caddy-proxy       │ not found    │ N/A          │
│ franken_mariadb           │ not found    │ N/A          │
│ franken_phpmyadmin        │ not found    │ N/A          │
╰───────────────────────────┴──────────────┴──────────────╯

`
	if code != 0 || out != want {
		t.Errorf("code %d, out:\n%s", code, out)
	}
}

func TestStatusRunning(t *testing.T) {
	e := newEnv(t)
	e.writeEnv("DB_ENGINES=postgresql\nREDIS_ENABLED=true\nDNS_SERVER=true\nDNS_IP=192.168.1.20\nDNS_LISTEN_IP=192.168.1.20\n")
	if err := e.store().AddDomains([]string{"app.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	e.docker.ps = `{"Names":"frankenphp-8.4","State":"running","Status":"Up 2 minutes (healthy)"}
{"Names":"franken-caddy-proxy","State":"running","Status":"Up 2 minutes"}
{"Names":"franken_redis","State":"exited","Status":"Exited (1)"}
{"Names":"franken_postgresql","State":"running","Status":"Up (health: starting)"}`

	code, out, _ := e.run("status")
	for _, line := range []string{
		"Status: running\n",
		"LAN DNS server: 192.168.1.20:53 (domains resolve to 192.168.1.20)\n",
		"│ app.test                       │ PHP 8.4      │",
		"│ frankenphp-8.4            │ running      │ healthy      │",
		"│ franken_redis             │ exited       │ N/A          │",
		"│ franken_postgresql        │ running      │ starting     │",
		"│ franken_pgadmin           │ not found    │ N/A          │",
		"│ franken_dns               │ not found    │ N/A          │",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("missing %q in:\n%s", line, out)
		}
	}
	// DNS_IP pinned: no watcher line
	if code != 0 || strings.Contains(out, "Follows LAN IP changes") || strings.Contains(out, "phpmyadmin") {
		t.Errorf("code %d, out:\n%s", code, out)
	}
	if len(e.docker.calls) != 1 || !slices.Equal(e.docker.calls[0], []string{"ps", "--all", "--no-trunc", "--format", "json"}) {
		t.Errorf("docker calls = %v", e.docker.calls)
	}
}
