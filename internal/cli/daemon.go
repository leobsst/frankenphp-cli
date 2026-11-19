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
	return &cobra.Command{
		Use:   "daemon",
		Short: i18n.T("Run the API of the dashboard (foreground)."),
		Long: i18n.T(`Run the API of the dashboard, in the foreground until interrupted.

It is served on a Unix socket of the data directory (daemon.sock, for this
user only) and on 127.0.0.1:DASHBOARD_PORT, where the access token of
daemon.token is required. The actions run one at a time.`),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return a.runDaemon(ctx)
		},
	}
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
	local, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	socketPath := filepath.Join(dir, daemon.SocketFile)
	socket, err := listenSocket(socketPath)
	if err != nil {
		_ = local.Close()
		return err
	}
	defer func() { _ = os.Remove(socketPath) }()

	a.log.Success(i18n.Tf("Dashboard API listening on http://%s (token: %s)", local.Addr(), filepath.Join(dir, daemon.TokenFile)))
	a.log.Info(i18n.Tf("CLI socket: %s", socketPath))
	srv := daemon.New(func(log events.Log) (*daemon.Workspace, error) { return a.workspace(dir, log) },
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

// workspace opens the configuration, the database and the services of dir
// for the daemon, reporting to log, with the dependencies of the commands.
func (a *app) workspace(dir string, log events.Log) (*daemon.Workspace, error) {
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
	h := a.deps.Hosts(log)
	certs := func(certsDir string) (stack.Certs, error) {
		m, err := ssl.New(certsDir, log)
		if err != nil {
			return nil, err
		}
		m.Run = a.deps.Command
		return m, nil
	}
	st := &stack.Service{Dir: dir, Env: e, Store: s, Docker: d, Hosts: h, Certs: certs, Watcher: a.deps.Watcher, Log: log}
	caddyDir := e.ResolveDir("CADDY_DIR", "./caddy", dir)
	return &daemon.Workspace{
		Dir: dir, Env: e, Store: s, Stack: st,
		Domains: func() *domains.Service {
			return &domains.Service{Dir: dir, CaddyDir: caddyDir, Env: e, Store: s, Docker: d, Hosts: h,
				Certs: func() (domains.Certs, error) { return certs(e.CertsDir(dir)) }, Log: log}
		},
		Close: s.Close,
	}, nil
}
