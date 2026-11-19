package cli

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/client"
	"github.com/leobsst/frankenphp-cli/internal/core/buckets"
	"github.com/leobsst/frankenphp-cli/internal/core/caserver"
	"github.com/leobsst/frankenphp-cli/internal/core/dnswatcher"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/domains"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/core/ssl"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/daemon"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/version"
)

func newDaemonCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: i18n.T("Run, start, stop or check the API of the dashboard."),
		Long: i18n.T(`Run, start, stop or check the API of the dashboard.

The daemon serves it on a Unix socket of the data directory (daemon.sock, for
this user only) and on 127.0.0.1:DASHBOARD_PORT, where the access token of
daemon.token is required. The actions run one at a time.

Examples:
    frankenmanager daemon start    # Start it in the background
    frankenmanager daemon status   # Check whether it runs, and its version
    frankenmanager daemon stop
    frankenmanager daemon run      # Run it in the foreground (services)`),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	run := &cobra.Command{
		Use:   "run",
		Short: i18n.T("Run the daemon in the foreground, until interrupted."),
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return a.runDaemon(ctx)
		},
	}
	start := &cobra.Command{
		Use:   "start",
		Short: i18n.T("Start the daemon in the background, or replace one of another version."),
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return a.daemonStart(cmd.Context()) },
	}
	stop := &cobra.Command{
		Use:   "stop",
		Short: i18n.T("Stop the daemon."),
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return a.daemonStop(cmd.Context()) },
	}
	status := &cobra.Command{
		Use:   "status",
		Short: i18n.T("Show whether the daemon runs, and its version."),
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return a.daemonStatus(cmd.Context()) },
	}
	cmd.AddCommand(run, start, stop, status)
	return cmd
}

// ensureDaemon returns the client of the daemon of this release, starting
// or replacing it when needed.
func (a *app) ensureDaemon(ctx context.Context, dir string) (*client.Client, client.Started, error) {
	return client.Ensure(ctx, dir, version.Version, a.deps.StartDaemon)
}

func (a *app) daemonStart(ctx context.Context) error {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	_, what, err := a.ensureDaemon(ctx, dir)
	if err != nil {
		return err
	}
	url := "http://127.0.0.1:" + e.Get(env.DashboardPortKey)
	switch what {
	case client.AlreadyRunning:
		a.log.Info(i18n.Tf("The daemon is already running: %s", url))
	case client.Replaced:
		a.log.Success(i18n.Tf("Daemon replaced by version %s: %s", version.Version, url))
	default:
		a.log.Success(i18n.Tf("Daemon started: %s", url))
	}
	return nil
}

func (a *app) daemonStop(ctx context.Context) error {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	// A daemon handed to the service manager would be relaunched: the
	// service goes, until the next command that needs the daemon
	stopped := false
	if svc, err := a.deps.DaemonService(dir); err == nil {
		stopped = svc.Stop(ctx)
	}
	if err := client.New(dir).Stop(ctx); err != nil && !(stopped && errors.Is(err, client.ErrNotRunning)) {
		return err
	}
	a.log.Success(i18n.T("Daemon stopped."))
	return nil
}

func (a *app) daemonStatus(ctx context.Context) error {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	v, err := client.New(dir).Version(ctx)
	if err != nil {
		a.log.Info(i18n.T("The daemon is not running."))
		return nil
	}
	a.log.Info(i18n.Tf("The daemon is running (version %s).", v))
	if v != version.Version {
		a.log.Warning(i18n.Tf("This CLI is version %s: run 'frankenmanager daemon start' to replace it.", version.Version))
	}
	return nil
}

// runDaemon serves the API until ctx is done.
func (a *app) runDaemon(ctx context.Context) error {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(e.Get(env.DashboardPortKey))
	if err != nil {
		return err
	}
	token, err := daemon.LoadToken(dir)
	if err != nil {
		return err
	}
	// The socket first: it tells a daemon already running
	socketPath := filepath.Join(dir, daemon.SocketFile)
	socket, err := listenSocket(socketPath)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(socketPath) }()
	local, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		_ = socket.Close()
		return err
	}

	a.log.Success(i18n.Tf("Dashboard API listening on http://%s (token: %s)", local.Addr(), filepath.Join(dir, daemon.TokenFile)))
	a.log.Info(i18n.Tf("CLI socket: %s", socketPath))
	// The background processes of earlier releases: the daemon serves the
	// LAN DNS server and shares the root CA itself now
	if w, err := a.deps.Watcher(dir); err == nil {
		w.Stop(ctx)
	}
	caserver.StopSharing(dir)
	h := &daemonHosts{dns: &dnswatcher.Host{DataDir: dir, Log: a.errOut}, ca: &caserver.Host{}}
	defer h.dns.Stop()
	defer h.ca.Stop()
	if h.dns.Apply(e) {
		a.log.Info(i18n.T("LAN DNS server started."))
	}

	dnsServer := &inProcessDNS{host: h.dns, dir: dir}
	srv := daemon.New(func(log events.Log) (*daemon.Workspace, error) { return a.workspace(dir, dnsServer, h.ca, log) },
		token, port, version.Version)
	err = srv.Serve(ctx, socket, local)
	a.log.Info(i18n.T("Dashboard API stopped."))
	return err
}

// listenSocket listens on the Unix socket path, readable by the user only.
// A socket left by a daemon that did not stop cleanly is replaced; one that
// answers means a daemon already runs.
func listenSocket(path string) (net.Listener, error) {
	if conn, err := net.Dial("unix", path); err == nil {
		_ = conn.Close()
		return nil, i18n.Errorf("A daemon is already running (%s).", path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

// daemonHosts are what the daemon runs in its own process.
type daemonHosts struct {
	dns *dnswatcher.Host
	ca  *caserver.Host
}

// workspace opens the configuration, the database and the services of dir
// for the daemon, reporting to log, with the dependencies of the commands:
// dnsServer is its LAN DNS server, ca shares the root CA.
func (a *app) workspace(dir string, dnsServer stack.DNS, ca *caserver.Host, log events.Log) (*daemon.Workspace, error) {
	e, _, err := env.Open(dir)
	if err != nil {
		return nil, err
	}
	s, err := a.openStore(dir)
	if err != nil {
		return nil, err
	}
	d := docker.New(dir, log)
	d.Runner = a.deps.Docker
	hostsFile := a.deps.Hosts(log)
	certs := func(certsDir string) (stack.Certs, error) {
		m, err := ssl.New(certsDir, log)
		if err != nil {
			return nil, err
		}
		m.Run = a.deps.Command
		return m, nil
	}
	st := &stack.Service{Dir: dir, Env: e, Store: s, Docker: d, Hosts: hostsFile, Certs: certs,
		DNS: dnsServer, Log: log}
	caddyDir := e.ResolveDir("CADDY_DIR", "./caddy", dir)
	return &daemon.Workspace{
		Dir: dir, Env: e, Store: s, Stack: st,
		Domains: func() *domains.Service {
			return &domains.Service{Dir: dir, CaddyDir: caddyDir, Env: e, Store: s, Docker: d, Hosts: hostsFile,
				Certs: func() (domains.Certs, error) { return certs(e.CertsDir(dir)) }, Log: log}
		},
		Buckets: func(ctx context.Context) (*buckets.Service, error) {
			g, err := buckets.Open(ctx, e)
			if err != nil {
				return nil, err
			}
			return &buckets.Service{Dir: dir, Env: e, Store: s, Hosts: hostsFile, Garage: g, Log: log}, nil
		},
		CA: ca.Sharing(dir, func(ctx context.Context) (string, error) {
			return ssl.RootCAPath(ctx, a.deps.Command)
		}, a.deps.LANIP),
		Close: s.Close,
	}, nil
}
