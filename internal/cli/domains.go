package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/domains"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/core/validate"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// rangeArgs accepts len(names) positional arguments, the first required of
// them mandatory, with usage errors (exit code 2).
func rangeArgs(required int, names ...string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < required {
			return usageError{i18n.Errorf("Missing argument '%s'.", names[len(args)])}
		}
		if len(args) > len(names) {
			return noArgs(cmd, args[len(names):])
		}
		return nil
	}
}

// server is the context of a command run on a started server.
type server struct {
	dir      string
	e        *env.Env
	d        *docker.Manager
	s        *store.Store
	caddyDir string
}

// openRunning prepares a command that needs the server started; notRunning
// (translated) is the error otherwise. The caller closes r.s.
func (a *app) openRunning(ctx context.Context, notRunning string) (*server, error) {
	dir, err := a.projectDir()
	if err != nil {
		return nil, err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return nil, err
	}
	d := a.docker(dir)
	s, err := a.openStore(dir)
	if err != nil {
		return nil, err
	}
	if ok, err := a.isRunning(ctx, d, s); err != nil || !ok {
		_ = s.Close()
		if err == nil {
			err = errors.New(notRunning)
		}
		return nil, err
	}
	return &server{dir: dir, e: e, d: d, s: s, caddyDir: e.ResolveDir("CADDY_DIR", "./caddy", dir)}, nil
}

// writeMain regenerates the main Caddyfile from the database.
func (r *server) writeMain() error { return domains.WriteMain(r.caddyDir, r.e, r.s) }

// events returns the console as the event log of the services.
func (a *app) events() events.Log { return events.NewLog(a.log) }

// domainService returns the hosts service of a running server, reporting
// on the console.
func (a *app) domainService(r *server) *domains.Service {
	log := a.events()
	return &domains.Service{
		Dir: r.dir, CaddyDir: r.caddyDir, Env: r.e, Store: r.s, Docker: r.d,
		Hosts: a.deps.Hosts(log),
		Certs: func() (domains.Certs, error) { return a.certs(r.e.CertsDir(r.dir)) },
		Log:   log,
	}
}

// restartProxy restarts the reverse proxy, to pick up new routes.
func (a *app) restartProxy(ctx context.Context, d *docker.Manager) {
	domains.RestartProxy(ctx, d, a.events())
}

// reported turns a failure the services already reported (error and
// cleanup messages) into exit code 1, so that it is not printed twice.
func reported(err error) error {
	var r *events.ReportedError
	if errors.As(err, &r) {
		return exitError(1)
	}
	return err
}

// --- add-host ----------------------------------------------------------------

// staticOptions are the add-host options of static hosts.
type staticOptions struct {
	enabled bool
	root    string // relative to the projects root, "" for <domain without TLD>
	spa     bool
}

func newAddHostCmd(a *app) *cobra.Command {
	var forceSSL bool
	var php string
	var static staticOptions
	cmd := &cobra.Command{
		Use:   "add-host DOMAINS [TARGET]",
		Short: i18n.T("Add new host(s) to the running server."),
		Long: i18n.T(`Add new host(s) to the running server.

This command allows you to add new domains while the server is running,
without having to stop and restart the entire environment.

If a target is given, the domain(s) are registered as proxy hosts that
forward straight to that raw upstream address instead of getting a PHP
container of their own.

--root sets the directory served, relative to the project path: its first
directory (the project) must exist. Default: <domain without TLD>/public
for PHP, <domain without TLD> for a static host.

With --static, the domain(s) are static hosts (HTML site, built front-end
app): the reverse proxy serves their files itself, with no PHP container.
Add --spa for a single-page app, whose unknown paths fall back to
/index.html.

Examples:
    frankenmanager add-host "newapp.test"
    frankenmanager add-host "app1.test app2.test" --php 8.4
    frankenmanager add-host "legacy.test" --php 8.2 --force-ssl
    frankenmanager add-host "api.test" --root monorepo/apps/api/public
    frankenmanager add-host "domain.test" http://127.0.0.1:8006
    frankenmanager add-host "domain.test" 127.0.0.1:8006
    frankenmanager add-host "site.test" --static
    frankenmanager add-host "front.test" --static --root front/dist --spa`),
		Args: rangeArgs(1, "DOMAINS", "TARGET"),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, proxy := "", len(args) == 2
			if proxy {
				target = args[1]
			}
			f := cmd.Flags()
			for _, name := range []string{"static", "root"} {
				if f.Changed(name) && proxy {
					return usageError{i18n.Errorf("Option '%s' cannot be used with a target.", "--"+name)}
				}
			}
			if f.Changed("spa") && !static.enabled {
				return usageError{i18n.Errorf("Option '%s' requires '%s'.", "--spa", "--static")}
			}
			if static.root != "" {
				if err := validate.Root(static.root); err != nil {
					return err
				}
			}
			if static.enabled {
				return a.addStaticHosts(cmd.Context(), strings.Fields(args[0]), static, forceSSL)
			}
			return a.addHost(cmd.Context(), strings.Fields(args[0]), forceSSL, php, target, proxy, static.root)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&forceSSL, "force-ssl", false, i18n.T("Force SSL certificate regeneration"))
	f.StringVar(&php, "php", "", phpVersionUsage())
	f.BoolVar(&static.enabled, "static", false, i18n.T("Serve the domain(s) as static files (HTML), without PHP"))
	f.StringVar(&static.root, "root", "", i18n.T("Directory served, relative to the project path; its first directory must exist"))
	f.BoolVar(&static.spa, "spa", false, i18n.T("Single-page app: unknown paths of a static host fall back to /index.html"))
	return cmd
}

// addHost adds PHP domains, served from root when set, or proxy hosts
// forwarding to target.
func (a *app) addHost(ctx context.Context, names []string, forceSSL bool, php, target string, proxy bool,
	root string) (err error) {
	r, err := a.openRunning(ctx, i18n.T("The server is not running. Use 'start' command instead."))
	if err != nil {
		return err
	}
	defer closeStore(r.s, &err)

	svc := a.domainService(r)
	if proxy {
		res, err := svc.AddProxy(ctx, names, target, forceSSL)
		if err != nil || len(res.Domains) == 0 {
			return reported(err)
		}
		a.con.Println()
		a.log.Success(i18n.T("New proxy host(s) added successfully!"))
		a.log.Info(i18n.T("Added domains:"))
		for _, domain := range res.Domains {
			a.log.Info(fmt.Sprintf("  - https://%s -> %s", domain, res.Target))
		}
		return nil
	}

	res, err := svc.AddPHP(ctx, names, php, root, forceSSL)
	if err != nil || len(res.Domains) == 0 {
		return reported(err)
	}
	a.con.Println()
	a.log.Success(i18n.T("New host(s) added successfully!"))
	a.log.Info(i18n.T("Added domains:"))
	for _, domain := range res.Domains {
		a.log.Info(fmt.Sprintf("  - https://%s (PHP %s)", domain, res.PHP))
	}
	return nil
}

// addStaticHosts adds domains whose files the reverse proxy serves.
func (a *app) addStaticHosts(ctx context.Context, names []string, o staticOptions, forceSSL bool) (err error) {
	r, err := a.openRunning(ctx, i18n.T("The server is not running. Use 'start' command instead."))
	if err != nil {
		return err
	}
	defer closeStore(r.s, &err)

	res, err := a.domainService(r).AddStatic(ctx, names, o.root, o.spa, forceSSL)
	if err != nil || len(res.Hosts) == 0 {
		return reported(err)
	}
	a.con.Println()
	a.log.Success(i18n.T("New static host(s) added successfully!"))
	a.log.Info(i18n.T("Added domains:"))
	for _, h := range res.Hosts {
		spa := ""
		if h.SPA {
			spa = ", SPA"
		}
		a.log.Info(fmt.Sprintf("  - https://%s -> %s%s", h.Domain, filepath.Join(res.ProjectPath, filepath.FromSlash(h.Root)), spa))
	}
	a.log.Info(i18n.Tf("\nSite files (editable): %s", res.SiteDir))
	return nil
}

// --- add-alias ---------------------------------------------------------------

func newAddAliasCmd(a *app) *cobra.Command {
	var forceSSL bool
	cmd := &cobra.Command{
		Use:   "add-alias TARGET ALIASES",
		Short: i18n.T("Add alternate host(s) that reverse-proxy to an already-configured domain or proxy host."),
		Long: i18n.T(`Add alternate host(s) that reverse-proxy to an already-configured domain or proxy host.

This is like 'add-host', but instead of creating a new site with its own
Caddyfile and PHP container, each alternate host gets its own SSL
certificate and hosts entry and is added to the reverse proxy's Caddyfile
only, forwarding to the target's existing site: the PHP site of a domain,
or the upstream of a proxy host.

Examples:
    frankenmanager add-alias "myapp.test" "myapp-alt.test"
    frankenmanager add-alias "myapp.test" "alt1.test alt2.test" --force-ssl
    frankenmanager add-alias "proxied.test" "proxied-alt.test"`),
		Args: rangeArgs(2, "TARGET", "ALIASES"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.addAlias(cmd.Context(), args[0], strings.Fields(args[1]), forceSSL)
		},
	}
	cmd.Flags().BoolVar(&forceSSL, "force-ssl", false, i18n.T("Force SSL certificate regeneration"))
	return cmd
}

// addAlias adds alternate hosts forwarding to an existing domain or proxy
// host.
func (a *app) addAlias(ctx context.Context, target string, aliases []string, forceSSL bool) (err error) {
	r, err := a.openRunning(ctx, i18n.T("The server is not running. Use 'start' command instead."))
	if err != nil {
		return err
	}
	defer closeStore(r.s, &err)

	res, err := a.domainService(r).AddAlias(ctx, target, aliases, forceSSL)
	if err != nil || len(res.Aliases) == 0 {
		return reported(err)
	}
	a.con.Println()
	a.log.Success(i18n.T("Alternate host(s) added successfully!"))
	a.log.Info(i18n.T("Added aliases:"))
	for _, domain := range res.Aliases {
		a.log.Info(fmt.Sprintf("  - https://%s -> %s (%s)", domain, res.Target, res.Serves))
	}
	return nil
}
