package cli

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/client"
	"github.com/leobsst/frankenphp-cli/internal/core/background"
	"github.com/leobsst/frankenphp-cli/internal/core/caserver"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/dnswatcher"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/daemon"
	"github.com/leobsst/frankenphp-cli/internal/platform"
	"github.com/leobsst/frankenphp-cli/internal/version"
)

// The daemon serves the LAN DNS server and shares the root CA. The CLI
// reaches them through it; earlier releases ran them in processes of their
// own, stopped on the way.

// dns returns the LAN DNS server of dir as the commands see it.
func (a *app) dns(dir string) stack.DNS {
	if a.deps.DNS != nil {
		return a.deps.DNS(dir)
	}
	return &daemonDNS{a: a, dir: dir}
}

// daemonDNS is the LAN DNS server served by the daemon, started when needed.
type daemonDNS struct {
	a   *app
	dir string
}

// stopWatcher stops the LAN DNS process of earlier releases: the daemon
// serves now, on the same port.
func (x *daemonDNS) stopWatcher(ctx context.Context) {
	if w, err := x.a.deps.Watcher(x.dir); err == nil {
		w.Stop(ctx)
	}
}

func (x *daemonDNS) Apply(ctx context.Context) (bool, error) {
	x.stopWatcher(ctx)
	e, _, err := env.Open(x.dir)
	if err != nil {
		return false, err
	}
	c := client.New(x.dir)
	if !dns.Enabled(e) && !c.Running(ctx) {
		return false, nil
	}
	if c, _, err = x.a.ensureDaemon(ctx, x.dir); err != nil {
		return false, err
	}
	var st daemon.DNSState
	if err := c.Run(ctx, http.MethodPost, "/api/v1/dns/apply", nil, &st); err != nil {
		return false, err
	}
	return st.Running, nil
}

func (x *daemonDNS) Stop(ctx context.Context) {
	x.stopWatcher(ctx)
	if c := client.New(x.dir); c.Running(ctx) {
		_ = c.Run(ctx, http.MethodPost, "/api/v1/dns/stop", nil, nil)
	}
}

func (x *daemonDNS) Running(ctx context.Context) bool {
	var st daemon.DNSState
	return client.New(x.dir).Do(ctx, http.MethodGet, "/api/v1/dns", nil, &st) == nil && st.Running
}

// inProcessDNS is the LAN DNS server of the daemon itself.
type inProcessDNS struct {
	host *dnswatcher.Host
	dir  string
}

func (x *inProcessDNS) Apply(context.Context) (bool, error) {
	e, _, err := env.Open(x.dir)
	if err != nil {
		return false, err
	}
	return x.host.Apply(e), nil
}

func (x *inProcessDNS) Stop(context.Context) { x.host.Stop() }

func (x *inProcessDNS) Running(context.Context) bool { return x.host.Running() }

// --- Operations --------------------------------------------------------------

// opClient returns the client the commands that change the shared state
// submit their operations to, and the function that ends it. They go
// through the daemon, the only owner of that state; but the daemon has no
// terminal to ask for a password: without the passwordless setup, they run
// in this process (same API and services), where sudo can ask.
func (a *app) opClient(ctx context.Context, dir string) (*client.Client, func(), error) {
	if a.deps.DaemonInProcess || !a.helperReady(ctx) {
		ctx, cancel := context.WithCancel(ctx)
		srv := daemon.New(func(log events.Log) (*daemon.Workspace, error) {
			return a.workspace(dir, a.dns(dir), &caserver.Host{}, log)
		}, "", 0, version.Version)
		go srv.Queue.Run(ctx)
		return client.NewInProcess(srv.Handler()), cancel, nil
	}
	c, _, err := a.ensureDaemon(ctx, dir)
	return c, func() {}, err
}

// helperReady reports whether the hosts file can be edited without a
// terminal: the passwordless helper of setup.
func (a *app) helperReady(ctx context.Context) bool {
	return a.deps.Privilege(events.NewLog(nil)).CanUseHelper(ctx)
}

// runOp runs an operation of the API (method, path, body as JSON), printing
// its events as they come like the commands always did, and decodes its
// result into out. With --verbose, the image builds stream their output.
func (a *app) runOp(ctx context.Context, dir, method, path string, body, out any) error {
	c, done, err := a.opClient(ctx, dir)
	if err != nil {
		return err
	}
	defer done()
	if a.verbose {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		path += sep + "verbose=true"
	}
	return c.RunWithEvents(ctx, method, path, body, out, func(e daemon.Event) { a.log.Emit(e.Core()) })
}

// --- Daemon service ----------------------------------------------------------

// daemonService returns the background service of the daemon of dir.
// It runs with the PATH of the command that installs it: the service
// manager's own lacks mkcert and docker (Homebrew, Docker Desktop).
func daemonService(dir string) (*background.Service, error) {
	spec := daemon.ServiceSpec
	spec.Env = map[string]string{"PATH": platform.ToolPath(os.Getenv("PATH"))}
	return background.New(dir, spec)
}

// serviceStarter starts the daemon of a data directory: handed to the OS
// service manager (started at login) unless DAEMON_AUTOSTART=false, then
// detached.
func serviceStarter(service func(string) (*background.Service, error), detached client.Starter) client.Starter {
	return func(dir string) (<-chan struct{}, error) {
		if e, _, err := env.Open(dir); err == nil && e.Get(env.DaemonAutostartKey) == "false" {
			return detached(dir)
		}
		s, err := service(dir)
		if err != nil {
			return nil, err
		}
		// The service manager relaunches it: no exit to wait for
		return nil, s.Start(context.Background())
	}
}

// --- Root CA sharing ---------------------------------------------------------

// The CA sharing of the commands: through the daemon, after stopping the
// detached server of earlier releases.

func shareCAVia(start client.Starter) func(dataDir, certFile string, port int) error {
	return func(dataDir, _ string, port int) error {
		caserver.StopSharing(dataDir)
		c, _, err := client.Ensure(context.Background(), dataDir, version.Version, start)
		if err != nil {
			return err
		}
		return c.Run(context.Background(), http.MethodPut, "/api/v1/ca", map[string]any{"enabled": true, "port": port}, nil)
	}
}

func stopCAVia(dataDir string) bool {
	stopped := caserver.StopSharing(dataDir)
	c := client.New(dataDir)
	if !c.Running(context.Background()) {
		return stopped
	}
	err := c.Run(context.Background(), http.MethodPut, "/api/v1/ca", map[string]any{"enabled": false}, nil)
	return stopped || err == nil
}

func caStateVia(dataDir string) (pid, port int, ok bool) {
	if pid, port, ok := caserver.RunningState(dataDir); ok {
		return pid, port, true
	}
	var st struct {
		Sharing bool `json:"sharing"`
		Port    int  `json:"port"`
	}
	if err := client.New(dataDir).Do(context.Background(), http.MethodGet, "/api/v1/ca", nil, &st); err != nil || !st.Sharing {
		return 0, 0, false
	}
	return 0, st.Port, true
}
