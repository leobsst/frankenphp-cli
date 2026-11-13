package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/caddyfile"
	"github.com/leobsst/frankenphp-cli/internal/core/compose"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/hosts"
	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/core/resources"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/core/validate"
)

// rangeArgs accepts len(names) positional arguments, the first required of
// them mandatory, with usage errors (exit code 2).
func rangeArgs(required int, names ...string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < required {
			return usageError{fmt.Errorf("Missing argument '%s'.", names[len(args)])} //nolint:staticcheck // user-facing message
		}
		if len(args) > len(names) {
			return noArgs(cmd, args[len(names):])
		}
		return nil
	}
}

// errNoProjectPath is raised when a new PHP container needs the projects
// root and none is known.
var errNoProjectPath = errors.New("No project path known: set DEFAULT_PROJECT_PATH (frankenmanager configuration set) " + //nolint:staticcheck // user-facing message
	"or restart the server with --path.")

// server is the context of a command run on a started server.
type server struct {
	dir      string
	e        *env.Env
	d        *docker.Manager
	s        *store.Store
	caddyDir string
}

// openRunning prepares a command that needs the server started; notRunning
// is the error otherwise. The caller closes r.s.
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

func (r *server) generator(log caddyfile.Logger) *caddyfile.Generator {
	return caddyfile.New(r.caddyDir, r.dir, log)
}

// writeMain regenerates the main Caddyfile from the database.
func (r *server) writeMain() error {
	domains, err := r.s.Domains()
	if err != nil {
		return err
	}
	aliases, err := r.s.AliasEntries()
	if err != nil {
		return err
	}
	proxies, err := r.s.Proxies()
	if err != nil {
		return err
	}
	return caddyfile.WriteMain(r.caddyDir, caddyfile.Main{
		Domains: domains, Aliases: aliases, Proxies: proxies,
		DBAdminTarget: r.e.DBAdminTarget(), WebsocketTarget: soketi.Target(r.e),
	})
}

// startVersion builds the image of a PHP version not running yet, then
// brings it up with the others (active plus phpVersion).
func (a *app) startVersion(ctx context.Context, r *server, phpVersion string, active []string) error {
	customPath, err := stack.ProjectPath(r.e, r.s)
	if err != nil {
		return err
	}
	if customPath == "" {
		return errNoProjectPath
	}
	if _, err := stack.BuildImage(ctx, r.d, r.e, a.log, customPath, phpVersion, false); err != nil {
		return err
	}
	a.log.Info(fmt.Sprintf("Starting PHP %s container...", phpVersion))
	listen, err := dns.ListenAddress(r.e)
	if err != nil {
		return err
	}
	versions := active
	if !slices.Contains(versions, phpVersion) {
		versions = append(slices.Clone(active), phpVersion)
	}
	return stack.ComposeUp(ctx, r.d, r.e, versions, listen, customPath)
}

// restartVersion restarts the container of a PHP version.
func (a *app) restartVersion(ctx context.Context, d *docker.Manager, phpVersion string) {
	name := phpversions.ContainerName(phpVersion)
	a.log.Info(fmt.Sprintf("Restarting %s...", name))
	d.RestartContainer(ctx, name)
}

// restartProxy restarts the reverse proxy, to pick up new routes.
func (a *app) restartProxy(ctx context.Context, d *docker.Manager) {
	a.log.Info("Restarting reverse proxy...")
	d.RestartContainer(ctx, compose.ReverseProxyContainer)
}

// phpVersion resolves --php (flag, then DEFAULT_PHP_VERSION), validates it
// and warns when deprecated.
func (a *app) phpVersion(flag string, e *env.Env) (string, error) {
	v := flag
	if v == "" {
		v = phpversions.ResolveDefault(e.Get("DEFAULT_PHP_VERSION"))
	}
	if err := phpversions.Validate(v); err != nil {
		return "", err
	}
	if w := phpversions.DeprecationWarning(v); w != "" {
		a.log.Warning(w)
	}
	return v, nil
}

// cleanup undoes the hosts entries of a failed command. Its failure report
// is the error, then the cleanup messages, then exit code 1: the error is
// printed only once.
type cleanup struct {
	a     *app
	h     *hosts.Manager
	added []string
}

func (c *cleanup) add(ctx context.Context, domain string) error {
	if _, err := c.h.AddEntry(ctx, "127.0.0.1", domain); err != nil {
		return err
	}
	c.added = append(c.added, domain)
	return nil
}

// fail logs err, removes the hosts entries added, runs undo, and returns
// exit code 1 with the final message ("The host(s) were not added.").
func (c *cleanup) fail(ctx context.Context, err error, done string, undo func()) error {
	c.a.log.Error(fmt.Sprintf("An error occurred: %v", err))
	c.a.log.Error("Cleaning up...")
	for _, domain := range c.added {
		_, _ = c.h.RemoveEntry(ctx, "127.0.0.1", domain)
	}
	if undo != nil {
		undo()
	}
	c.a.log.Error("Cleanup complete. " + done)
	return exitError(1)
}

// --- add-host ----------------------------------------------------------------

func newAddHostCmd(a *app) *cobra.Command {
	var forceSSL bool
	var php string
	cmd := &cobra.Command{
		Use:   "add-host DOMAINS [TARGET]",
		Short: "Add new host(s) to the running server.",
		Long: `Add new host(s) to the running server.

This command allows you to add new domains while the server is running,
without having to stop and restart the entire environment.

If a target is given, the domain(s) are registered as proxy hosts that
forward straight to that raw upstream address instead of getting a PHP
container of their own.

Examples:
    frankenmanager add-host "newapp.test"
    frankenmanager add-host "app1.test app2.test" --php 8.4
    frankenmanager add-host "legacy.test" --php 8.2 --force-ssl
    frankenmanager add-host "domain.test" http://127.0.0.1:8006
    frankenmanager add-host "domain.test" 127.0.0.1:8006`,
		Args: rangeArgs(1, "DOMAINS", "TARGET"),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, proxy := "", len(args) == 2
			if proxy {
				target = args[1]
			}
			return a.addHost(cmd.Context(), strings.Fields(args[0]), forceSSL, php, target, proxy)
		},
	}
	cmd.Flags().BoolVar(&forceSSL, "force-ssl", false, "Force SSL certificate regeneration")
	cmd.Flags().StringVar(&php, "php", "", phpVersionHelp)
	return cmd
}

// addHost adds PHP domains, or proxy hosts forwarding to target.
func (a *app) addHost(ctx context.Context, domains []string, forceSSL bool, php, target string, proxy bool) (err error) {
	r, err := a.openRunning(ctx, "The server is not running. Use 'start' command instead.")
	if err != nil {
		return err
	}
	defer closeStore(r.s, &err)

	taken, err := r.s.DomainNames()
	if err != nil {
		return err
	}
	aliases, err := r.s.Aliases()
	if err != nil {
		return err
	}
	for _, al := range aliases {
		taken = append(taken, al.Alias)
	}
	proxies, err := r.s.Proxies()
	if err != nil {
		return err
	}
	for _, p := range proxies {
		taken = append(taken, p.Domain)
	}

	var added []string
	for _, domain := range domains {
		if err := validate.Domain(domain); err != nil {
			return err
		}
		switch {
		case slices.Contains(taken, domain):
			a.log.Info(fmt.Sprintf("Domain '%s' is already configured, skipping.", domain))
		case slices.Contains(added, domain):
			a.log.Info(fmt.Sprintf("Domain '%s' is duplicated in input, skipping.", domain))
		default:
			added = append(added, domain)
		}
	}
	if len(added) == 0 {
		a.log.Info("No new domains to add.")
		return nil
	}

	certs, err := a.ssl(r.e.CertsDir(r.dir))
	if err != nil {
		return err
	}
	c := &cleanup{a: a, h: a.deps.Hosts(a.log)}
	if proxy {
		return a.addProxyHosts(ctx, r, c, certs.GenerateAll, added, target, forceSSL)
	}

	php, err = a.phpVersion(php, r.e)
	if err != nil {
		return err
	}
	active, err := r.s.ActivePHPVersions()
	if err != nil {
		return err
	}
	if err := resources.New(r.dir, a.log).EnsurePHPVersionConfig(php); err != nil {
		return err
	}

	err = func() error {
		a.log.Info(fmt.Sprintf("Adding %d new domain(s) with PHP %s...", len(added), php))
		a.log.Info("Generating SSL certificates...")
		if err := certs.GenerateAll(ctx, added, forceSSL, r.e.IsProduction()); err != nil {
			return err
		}
		a.log.Info("Adding hosts entries...")
		for _, domain := range added {
			if err := c.add(ctx, domain); err != nil {
				return err
			}
		}
		a.log.Info("Generating Caddyfiles...")
		if err := r.generator(a.log).GenerateForVersion(added, php); err != nil {
			return err
		}
		if err := r.s.AddDomains(added, php); err != nil {
			return err
		}
		if err := r.writeMain(); err != nil {
			return err
		}
		if err := dns.Sync(r.dir, r.e, r.s); err != nil {
			return err
		}
		if !slices.Contains(active, php) {
			if err := a.startVersion(ctx, r, php, active); err != nil {
				return err
			}
		} else {
			a.restartVersion(ctx, r.d, php)
		}
		a.restartProxy(ctx, r.d)
		return nil
	}()
	if err != nil {
		// The domains are removed from the database too
		return c.fail(ctx, err, "The host(s) were not added.", func() { _ = r.s.RemoveDomains(added) })
	}

	a.con.Println()
	a.log.Success("New host(s) added successfully!")
	a.log.Info("Added domains:")
	for _, domain := range added {
		a.log.Info(fmt.Sprintf("  - https://%s (PHP %s)", domain, php))
	}
	return nil
}

// addProxyHosts adds domains forwarding to a raw upstream: certificate,
// hosts entry and main Caddyfile only, no PHP container.
func (a *app) addProxyHosts(ctx context.Context, r *server, c *cleanup,
	generate func(context.Context, []string, bool, bool) error, added []string, target string, forceSSL bool) error {
	if err := validate.ProxyTarget(target); err != nil {
		return err
	}
	err := func() error {
		a.log.Info(fmt.Sprintf("Adding %d new proxy host(s) -> %s...", len(added), target))
		a.log.Info("Generating SSL certificates...")
		if err := generate(ctx, added, forceSSL, r.e.IsProduction()); err != nil {
			return err
		}
		a.log.Info("Adding hosts entries...")
		for _, domain := range added {
			if err := c.add(ctx, domain); err != nil {
				return err
			}
		}
		if err := r.s.AddProxies(added, target); err != nil {
			return err
		}
		if err := r.writeMain(); err != nil {
			return err
		}
		if err := dns.Sync(r.dir, r.e, r.s); err != nil {
			return err
		}
		a.restartProxy(ctx, r.d)
		return nil
	}()
	if err != nil {
		return c.fail(ctx, err, "The proxy host(s) were not added.", func() { _ = r.s.RemoveProxies(added) })
	}

	a.con.Println()
	a.log.Success("New proxy host(s) added successfully!")
	a.log.Info("Added domains:")
	for _, domain := range added {
		a.log.Info(fmt.Sprintf("  - https://%s -> %s", domain, target))
	}
	return nil
}

// --- add-alias ---------------------------------------------------------------

func newAddAliasCmd(a *app) *cobra.Command {
	var forceSSL bool
	cmd := &cobra.Command{
		Use:   "add-alias TARGET ALIASES",
		Short: "Add alternate host(s) that reverse-proxy to an already-configured domain.",
		Long: `Add alternate host(s) that reverse-proxy to an already-configured domain.

This is like 'add-host', but instead of creating a new site with its own
Caddyfile and PHP container, each alternate host gets its own SSL
certificate and hosts entry and is added to the reverse proxy's Caddyfile
only, forwarding to the target domain's existing site.

Examples:
    frankenmanager add-alias "myapp.test" "myapp-alt.test"
    frankenmanager add-alias "myapp.test" "alt1.test alt2.test" --force-ssl`,
		Args: rangeArgs(2, "TARGET", "ALIASES"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.addAlias(cmd.Context(), args[0], strings.Fields(args[1]), forceSSL)
		},
	}
	cmd.Flags().BoolVar(&forceSSL, "force-ssl", false, "Force SSL certificate regeneration")
	return cmd
}

// addAlias adds alternate hosts forwarding to an existing domain.
func (a *app) addAlias(ctx context.Context, target string, aliases []string, forceSSL bool) (err error) {
	r, err := a.openRunning(ctx, "The server is not running. Use 'start' command instead.")
	if err != nil {
		return err
	}
	defer closeStore(r.s, &err)

	domains, err := r.s.DomainNames()
	if err != nil {
		return err
	}
	if !slices.Contains(domains, target) {
		return &validate.Error{Msg: fmt.Sprintf("Domain '%s' is not configured. Add it first with 'add-host'.", target)}
	}
	php, _, err := r.s.DomainPHPVersion(target)
	if err != nil {
		return err
	}
	existing, err := r.s.Aliases()
	if err != nil {
		return err
	}
	isAlias := func(d string) bool {
		return slices.ContainsFunc(existing, func(al store.Alias) bool { return al.Alias == d })
	}

	var added []string
	for _, domain := range aliases {
		if err := validate.Domain(domain); err != nil {
			return err
		}
		switch {
		case domain == target:
			a.log.Info(fmt.Sprintf("'%s' is the target domain itself, skipping.", domain))
		case slices.Contains(domains, domain):
			a.log.Info(fmt.Sprintf("Domain '%s' is already a registered host, skipping.", domain))
		case isAlias(domain):
			a.log.Info(fmt.Sprintf("'%s' is already configured as an alias, skipping.", domain))
		case slices.Contains(added, domain):
			a.log.Info(fmt.Sprintf("'%s' is duplicated in input, skipping.", domain))
		default:
			added = append(added, domain)
		}
	}
	if len(added) == 0 {
		a.log.Info("No new alternate hosts to add.")
		return nil
	}

	certs, err := a.ssl(r.e.CertsDir(r.dir))
	if err != nil {
		return err
	}
	c := &cleanup{a: a, h: a.deps.Hosts(a.log)}
	err = func() error {
		a.log.Info(fmt.Sprintf("Adding %d alternate host(s) for '%s'...", len(added), target))
		a.log.Info("Generating SSL certificates...")
		if err := certs.GenerateAll(ctx, added, forceSSL, r.e.IsProduction()); err != nil {
			return err
		}
		a.log.Info("Adding hosts entries...")
		for _, domain := range added {
			if err := c.add(ctx, domain); err != nil {
				return err
			}
		}
		if err := r.s.AddAliases(added, target); err != nil {
			return err
		}
		if err := r.writeMain(); err != nil {
			return err
		}
		if err := dns.Sync(r.dir, r.e, r.s); err != nil {
			return err
		}
		a.restartProxy(ctx, r.d)
		return nil
	}()
	if err != nil {
		return c.fail(ctx, err, "The alternate host(s) were not added.", func() { _ = r.s.RemoveAliases(added) })
	}

	a.con.Println()
	a.log.Success("Alternate host(s) added successfully!")
	a.log.Info("Added aliases:")
	for _, domain := range added {
		a.log.Info(fmt.Sprintf("  - https://%s -> %s (PHP %s)", domain, target, php))
	}
	return nil
}
