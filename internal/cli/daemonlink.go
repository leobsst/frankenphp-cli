package cli

import (
	"context"
	"net/http"

	"github.com/leobsst/frankenphp-cli/internal/client"
	"github.com/leobsst/frankenphp-cli/internal/core/background"
	"github.com/leobsst/frankenphp-cli/internal/core/caserver"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/dnswatcher"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/daemon"
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
	if err := c.Run(ctx, http.MethodPost, "/api/dns/apply", nil, &st); err != nil {
		return false, err
	}
	return st.Running, nil
}

func (x *daemonDNS) Stop(ctx context.Context) {
	x.stopWatcher(ctx)
	if c := client.New(x.dir); c.Running(ctx) {
		_ = c.Run(ctx, http.MethodPost, "/api/dns/stop", nil, nil)
	}
}

func (x *daemonDNS) Running(ctx context.Context) bool {
	var st daemon.DNSState
	return client.New(x.dir).Do(ctx, http.MethodGet, "/api/dns", nil, &st) == nil && st.Running
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

// --- Daemon service ----------------------------------------------------------

// daemonService returns the background service of the daemon of dir.
func daemonService(dir string) (*background.Service, error) {
	return background.New(dir, daemon.ServiceSpec)
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
		return c.Run(context.Background(), http.MethodPut, "/api/ca", map[string]any{"enabled": true, "port": port}, nil)
	}
}

func stopCAVia(dataDir string) bool {
	stopped := caserver.StopSharing(dataDir)
	c := client.New(dataDir)
	if !c.Running(context.Background()) {
		return stopped
	}
	err := c.Run(context.Background(), http.MethodPut, "/api/ca", map[string]any{"enabled": false}, nil)
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
	if err := client.New(dataDir).Do(context.Background(), http.MethodGet, "/api/ca", nil, &st); err != nil || !st.Sharing {
		return 0, 0, false
	}
	return 0, st.Port, true
}
