package cli

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// phpVersionUsage returns the help of the --php options, translated.
func phpVersionUsage() string {
	return i18n.Tf("PHP version (%s; %s deprecated). Defaults to DEFAULT_PHP_VERSION",
		strings.Join(phpversions.Supported, ", "), strings.Join(phpversions.Deprecated, ", "))
}

// maxArgs accepts up to n arguments, extra ones being a usage error.
func maxArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) <= n {
			return nil
		}
		return noArgs(cmd, args[n:])
	}
}

type startOptions struct {
	path, php       string
	forceSSL, build bool
}

func newStartCmd(a *app) *cobra.Command {
	var o startOptions
	cmd := &cobra.Command{
		Use:   "start [DOMAINS]",
		Short: i18n.T("Start the FrankenPHP development server."),
		Long: i18n.T(`Start the FrankenPHP development server.

If domains are not provided, uses domains registered in the database.
With no domain at all, starts the reverse proxy and databases only
(add hosts later with add-host).
If --path is not specified, uses the DEFAULT_PROJECT_PATH configuration.
PHP images are only built when missing or outdated, unless --build is set.

Examples:
    frankenmanager start "myapp.test"
    frankenmanager start "myapp.test api.test" --path /home/projects
    frankenmanager start "myapp.test" --php 8.4
    frankenmanager start  # Uses registered domains from database
    frankenmanager start --build  # Force a rebuild of the PHP images`),
		Args: maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var domains []string
			if len(args) == 1 && args[0] != "" {
				domains = strings.Fields(args[0])
			}
			return a.start(cmd.Context(), domains, o)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.path, "path", "p", "", i18n.T("Project root path (uses the DEFAULT_PROJECT_PATH configuration if not set)"))
	f.BoolVar(&o.forceSSL, "force-ssl", false, i18n.T("Force SSL certificate regeneration"))
	f.StringVar(&o.php, "php", "", phpVersionUsage())
	f.BoolVar(&o.build, "build", false, i18n.T("Rebuild the PHP images even if they are up to date"))
	return cmd
}

// isRunning reports whether the stack of dir runs.
func (a *app) isRunning(ctx context.Context, d *docker.Manager, s *store.Store) (bool, error) {
	return stack.IsRunning(ctx, d, s)
}

// certs returns the certificate generator of certsDir, for the services.
func (a *app) certs(certsDir string) (stack.Certs, error) {
	m, err := a.ssl(certsDir)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// stackService returns the stack service of dir, reporting on the console.
// e may be nil for Stop.
func (a *app) stackService(dir string, e *env.Env, d *docker.Manager, s *store.Store) *stack.Service {
	log := a.events()
	return &stack.Service{
		Dir: dir, Env: e, Store: s, Docker: d, Hosts: a.deps.Hosts(log),
		Certs: a.certs, DNS: a.dns(dir), Log: log,
	}
}

// start starts the stack. It starts even without any domain: the reverse
// proxy and databases come up alone.
func (a *app) start(ctx context.Context, domains []string, o startOptions) error {
	dir, e, err := a.prepare()
	if err != nil {
		return err
	}
	// The daemon has another working directory
	path := o.path
	if path != "" {
		if path, err = filepath.Abs(path); err != nil {
			return err
		}
	}
	var res stack.StartResult
	err = a.runOp(ctx, dir, http.MethodPost, "/api/stack/start", stack.StartOptions{
		Domains: domains, Path: path, PHP: o.php, ForceSSL: o.forceSSL, Build: o.build,
	}, &res)
	if err != nil {
		return reported(err)
	}
	// The daemon of the dashboard, handed to the service manager: it then
	// runs at login (DAEMON_AUTOSTART=false: on demand only)
	dashboard := ""
	if e.Get(env.DaemonAutostartKey) != "false" {
		if _, _, err := a.ensureDaemon(ctx, dir); err != nil {
			a.log.Warning(i18n.Tf("The dashboard daemon did not start: %v", err))
		} else {
			dashboard = "http://127.0.0.1:" + e.Get(env.DashboardPortKey)
		}
	}

	a.con.Println()
	a.log.Success(i18n.T("Web server started!"))
	for _, v := range res.Versions {
		var vd []string
		for _, m := range res.Domains {
			if m.PHPVersion == v {
				vd = append(vd, m.Name)
			}
		}
		a.log.Info(fmt.Sprintf("  PHP %s: %s", v, strings.Join(vd, ", ")))
	}
	if len(res.Statics) > 0 {
		var names []string
		for _, st := range res.Statics {
			names = append(names, st.Domain)
		}
		a.log.Info(i18n.Tf("  Static: %s", strings.Join(names, ", ")))
	}
	if soketi.Enabled(e) {
		a.log.Info(i18n.Tf("Soketi WebSocket server: wss://%s (apps: 127.0.0.1:%s)", soketi.Domain, soketi.Port(e)))
	}
	if s3.Enabled(e) {
		a.logS3Endpoints(e)
	}
	if dashboard != "" {
		a.log.Info(i18n.Tf("Dashboard: %s", dashboard))
	}
	if res.DNSListen != "" {
		ip, err := dns.ResolveIP(e)
		if err != nil {
			return err
		}
		a.log.Info(i18n.Tf("LAN DNS server listening on %s", res.DNSListen))
		addrs, err := dns.ServerAddresses(e)
		if err != nil {
			return err
		}
		for _, line := range dns.DescribeServerAddresses(addrs, ip) {
			a.log.Info(line)
		}
		if res.Watched {
			a.log.Info(i18n.T("  The domains follow this machine's LAN IP when it changes."))
		}
		if strings.HasPrefix(ip, "127.") {
			a.log.Warning(i18n.T("No LAN IP detected: the domains resolve to this machine only. " +
				"Set DNS_IP to this machine's LAN IP: frankenmanager configuration set DNS_IP <ip>"))
		}
	}
	return nil
}
