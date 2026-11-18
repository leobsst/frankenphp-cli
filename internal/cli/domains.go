package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
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
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
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

// errNoProjectPath is raised when a new PHP container needs the projects
// root and none is known.
var errNoProjectPath = i18n.NewError("No project path known: set DEFAULT_PROJECT_PATH (frankenmanager configuration set) " +
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
	statics, err := r.s.StaticHosts()
	if err != nil {
		return err
	}
	return caddyfile.WriteMain(r.caddyDir, caddyfile.Main{
		Domains: domains, Aliases: aliases, Proxies: proxies, Statics: statics,
		DBAdminTarget: r.e.DBAdminTarget(), WebsocketTarget: soketi.Target(r.e),
		S3Target: s3.ProxyTarget(r.e),
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
	a.log.Info(i18n.Tf("Starting PHP %s container...", phpVersion))
	versions := active
	if !slices.Contains(versions, phpVersion) {
		versions = append(slices.Clone(active), phpVersion)
	}
	return stack.ComposeUp(ctx, r.d, r.e, versions, customPath)
}

// restartVersion restarts the container of a PHP version.
func (a *app) restartVersion(ctx context.Context, d *docker.Manager, phpVersion string) {
	name := phpversions.ContainerName(phpVersion)
	a.log.Info(i18n.Tf("Restarting %s...", name))
	d.RestartContainer(ctx, name)
}

// restartProxy restarts the reverse proxy, to pick up new routes.
func (a *app) restartProxy(ctx context.Context, d *docker.Manager) {
	a.log.Info(i18n.T("Restarting reverse proxy..."))
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
// exit code 1 with the final message, translated ("The host(s) were not
// added.").
func (c *cleanup) fail(ctx context.Context, err error, done string, undo func()) error {
	c.a.log.Error(i18n.Tf("An error occurred: %v", err))
	c.a.log.Error(i18n.T("Cleaning up..."))
	for _, domain := range c.added {
		_, _ = c.h.RemoveEntry(ctx, "127.0.0.1", domain)
	}
	if undo != nil {
		undo()
	}
	c.a.log.Error(i18n.Tf("Cleanup complete. %s", done))
	return exitError(1)
}

// --- add-host ----------------------------------------------------------------

// staticOptions are the add-host options of static hosts.
type staticOptions struct {
	enabled bool
	root    string // relative to the projects root, "" for <domain without TLD>
	spa     bool
}

// cleanRoot normalizes a valid --root: slashes, no trailing one.
func cleanRoot(root string) string {
	return path.Clean(strings.ReplaceAll(root, `\`, "/"))
}

// checkProjectDir checks that the project directory of a --root, its first
// segment, exists in the projects root: the deeper directories may not be
// built yet (dist/).
func checkProjectDir(customPath, root string) error {
	first, _, _ := strings.Cut(cleanRoot(root), "/")
	if first == "." {
		return nil
	}
	dir := filepath.Join(customPath, first)
	if !isDir(dir) {
		return &validate.Error{Msg: i18n.Tf("Project directory does not exist: %s (first directory of --root, in the project path %s)",
			dir, customPath)}
	}
	return nil
}

// projectPath returns the projects root of a running server, an error when
// none is known.
func projectPath(r *server) (string, error) {
	customPath, err := stack.ProjectPath(r.e, r.s)
	if err == nil && customPath == "" {
		err = errNoProjectPath
	}
	return customPath, err
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

// takenHosts returns every registered host name: domains, aliases, proxy
// and static hosts.
func takenHosts(s *store.Store) ([]string, error) {
	taken, err := s.DomainNames()
	if err != nil {
		return nil, err
	}
	aliases, err := s.Aliases()
	if err != nil {
		return nil, err
	}
	for _, al := range aliases {
		taken = append(taken, al.Alias)
	}
	proxies, err := s.Proxies()
	if err != nil {
		return nil, err
	}
	for _, p := range proxies {
		taken = append(taken, p.Domain)
	}
	statics, err := s.StaticHostNames()
	if err != nil {
		return nil, err
	}
	return append(taken, statics...), nil
}

// newHosts validates domains and returns those not registered yet,
// without duplicates.
func (a *app) newHosts(s *store.Store, domains []string) ([]string, error) {
	taken, err := takenHosts(s)
	if err != nil {
		return nil, err
	}
	var added []string
	for _, domain := range domains {
		if err := validate.Domain(domain); err != nil {
			return nil, err
		}
		switch {
		case slices.Contains(taken, domain):
			a.log.Info(i18n.Tf("Domain '%s' is already configured, skipping.", domain))
		case slices.Contains(added, domain):
			a.log.Info(i18n.Tf("Domain '%s' is duplicated in input, skipping.", domain))
		default:
			added = append(added, domain)
		}
	}
	return added, nil
}

// addHost adds PHP domains, served from root when set, or proxy hosts
// forwarding to target.
func (a *app) addHost(ctx context.Context, domains []string, forceSSL bool, php, target string, proxy bool,
	root string) (err error) {
	r, err := a.openRunning(ctx, i18n.T("The server is not running. Use 'start' command instead."))
	if err != nil {
		return err
	}
	defer closeStore(r.s, &err)

	added, err := a.newHosts(r.s, domains)
	if err != nil {
		return err
	}
	if len(added) == 0 {
		a.log.Info(i18n.T("No new domains to add."))
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
	if root != "" {
		customPath, err := projectPath(r)
		if err != nil {
			return err
		}
		if err := checkProjectDir(customPath, root); err != nil {
			return err
		}
		root = cleanRoot(root)
	}
	active, err := r.s.ActivePHPVersions()
	if err != nil {
		return err
	}
	if err := resources.New(r.dir, a.log).EnsurePHPVersionConfig(php); err != nil {
		return err
	}

	err = func() error {
		a.log.Info(i18n.Tf("Adding %d new domain(s) with PHP %s...", len(added), php))
		a.log.Info(i18n.T("Generating SSL certificates..."))
		if err := certs.GenerateAll(ctx, added, forceSSL, r.e.IsProduction()); err != nil {
			return err
		}
		a.log.Info(i18n.T("Adding hosts entries..."))
		for _, domain := range added {
			if err := c.add(ctx, domain); err != nil {
				return err
			}
		}
		a.log.Info(i18n.T("Generating Caddyfiles..."))
		if err := r.generator(a.log).GenerateForVersionRoot(added, php, root); err != nil {
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
		return c.fail(ctx, err, i18n.T("The host(s) were not added."), func() { _ = r.s.RemoveDomains(added) })
	}

	a.con.Println()
	a.log.Success(i18n.T("New host(s) added successfully!"))
	a.log.Info(i18n.T("Added domains:"))
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
		a.log.Info(i18n.Tf("Adding %d new proxy host(s) -> %s...", len(added), target))
		a.log.Info(i18n.T("Generating SSL certificates..."))
		if err := generate(ctx, added, forceSSL, r.e.IsProduction()); err != nil {
			return err
		}
		a.log.Info(i18n.T("Adding hosts entries..."))
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
		return c.fail(ctx, err, i18n.T("The proxy host(s) were not added."), func() { _ = r.s.RemoveProxies(added) })
	}

	a.con.Println()
	a.log.Success(i18n.T("New proxy host(s) added successfully!"))
	a.log.Info(i18n.T("Added domains:"))
	for _, domain := range added {
		a.log.Info(fmt.Sprintf("  - https://%s -> %s", domain, target))
	}
	return nil
}

// addStaticHosts adds domains whose files the reverse proxy serves:
// certificate, hosts entry, site file and main Caddyfile, no PHP container.
func (a *app) addStaticHosts(ctx context.Context, domains []string, o staticOptions, forceSSL bool) (err error) {
	r, err := a.openRunning(ctx, i18n.T("The server is not running. Use 'start' command instead."))
	if err != nil {
		return err
	}
	defer closeStore(r.s, &err)

	added, err := a.newHosts(r.s, domains)
	if err != nil {
		return err
	}
	if len(added) == 0 {
		a.log.Info(i18n.T("No new domains to add."))
		return nil
	}
	// The reverse proxy mounts the projects root to serve the files
	customPath, err := projectPath(r)
	if err != nil {
		return err
	}
	if o.root != "" {
		if err := checkProjectDir(customPath, o.root); err != nil {
			return err
		}
	}
	hosts := make([]store.StaticHost, 0, len(added))
	for _, domain := range added {
		root := o.root
		if root == "" {
			root = caddyfile.SimpleDomain(domain)
		}
		hosts = append(hosts, store.StaticHost{Domain: domain, Root: cleanRoot(root), SPA: o.spa})
	}
	warned := map[string]bool{}
	for _, h := range hosts {
		if dir := filepath.Join(customPath, filepath.FromSlash(h.Root)); !warned[dir] && !isDir(dir) {
			warned[dir] = true
			a.log.Warning(i18n.Tf("Directory %s does not exist yet: build or create it before browsing.", dir))
		}
	}

	certs, err := a.ssl(r.e.CertsDir(r.dir))
	if err != nil {
		return err
	}
	active, err := r.s.ActivePHPVersions()
	if err != nil {
		return err
	}
	// Mounted by the reverse proxy since an earlier static host
	mounted := caddyfile.HasStaticSites(r.caddyDir)
	c := &cleanup{a: a, h: a.deps.Hosts(a.log)}
	g := r.generator(a.log)
	err = func() error {
		a.log.Info(i18n.Tf("Adding %d new static host(s)...", len(added)))
		a.log.Info(i18n.T("Generating SSL certificates..."))
		if err := certs.GenerateAll(ctx, added, forceSSL, r.e.IsProduction()); err != nil {
			return err
		}
		a.log.Info(i18n.T("Adding hosts entries..."))
		for _, domain := range added {
			if err := c.add(ctx, domain); err != nil {
				return err
			}
		}
		a.log.Info(i18n.T("Generating Caddyfiles..."))
		if err := g.GenerateStatic(hosts); err != nil {
			return err
		}
		if err := r.s.AddStaticHosts(hosts); err != nil {
			return err
		}
		if err := r.writeMain(); err != nil {
			return err
		}
		if err := dns.Sync(r.dir, r.e, r.s); err != nil {
			return err
		}
		if !mounted {
			// The reverse proxy is recreated with the static mounts
			a.log.Info(i18n.T("Mounting the static sites in the reverse proxy..."))
			if err := stack.ComposeUp(ctx, r.d, r.e, active, customPath); err != nil {
				return err
			}
		}
		a.restartProxy(ctx, r.d)
		return nil
	}()
	if err != nil {
		return c.fail(ctx, err, i18n.T("The static host(s) were not added."), func() {
			_ = r.s.RemoveStaticHosts(added)
			_ = r.generator(quietLogger{}).ArchiveStatic(added)
			_ = r.writeMain()
		})
	}

	a.con.Println()
	a.log.Success(i18n.T("New static host(s) added successfully!"))
	a.log.Info(i18n.T("Added domains:"))
	for _, h := range hosts {
		spa := ""
		if h.SPA {
			spa = ", SPA"
		}
		a.log.Info(fmt.Sprintf("  - https://%s -> %s%s", h.Domain, filepath.Join(customPath, filepath.FromSlash(h.Root)), spa))
	}
	a.log.Info(i18n.Tf("\nSite files (editable): %s", g.StaticDir()))
	return nil
}

// isDir reports whether p is an existing directory.
func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
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

	domains, err := r.s.DomainNames()
	if err != nil {
		return err
	}
	proxies, err := r.s.Proxies()
	if err != nil {
		return err
	}
	isProxy := func(d string) bool {
		return slices.ContainsFunc(proxies, func(p store.Proxy) bool { return p.Domain == d })
	}
	statics, err := r.s.StaticHostNames()
	if err != nil {
		return err
	}
	if slices.Contains(statics, target) {
		return &validate.Error{Msg: i18n.Tf("'%s' is a static host: it cannot have alternate hosts. "+
			"Add another static host with the same --root instead.", target)}
	}
	// serves is what the aliases forward to, for the summary
	var serves string
	if slices.Contains(domains, target) {
		php, _, err := r.s.DomainPHPVersion(target)
		if err != nil {
			return err
		}
		serves = "PHP " + php
	} else if upstream, ok, err := r.s.ProxyTarget(target); err != nil {
		return err
	} else if ok {
		serves = "proxy " + upstream
	} else {
		return &validate.Error{Msg: i18n.Tf("Domain '%s' is not configured. Add it first with 'add-host'.", target)}
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
			a.log.Info(i18n.Tf("'%s' is the target domain itself, skipping.", domain))
		case slices.Contains(domains, domain), isProxy(domain), slices.Contains(statics, domain):
			a.log.Info(i18n.Tf("Domain '%s' is already a registered host, skipping.", domain))
		case isAlias(domain):
			a.log.Info(i18n.Tf("'%s' is already configured as an alias, skipping.", domain))
		case slices.Contains(added, domain):
			a.log.Info(i18n.Tf("'%s' is duplicated in input, skipping.", domain))
		default:
			added = append(added, domain)
		}
	}
	if len(added) == 0 {
		a.log.Info(i18n.T("No new alternate hosts to add."))
		return nil
	}

	certs, err := a.ssl(r.e.CertsDir(r.dir))
	if err != nil {
		return err
	}
	c := &cleanup{a: a, h: a.deps.Hosts(a.log)}
	err = func() error {
		a.log.Info(i18n.Tf("Adding %d alternate host(s) for '%s'...", len(added), target))
		a.log.Info(i18n.T("Generating SSL certificates..."))
		if err := certs.GenerateAll(ctx, added, forceSSL, r.e.IsProduction()); err != nil {
			return err
		}
		a.log.Info(i18n.T("Adding hosts entries..."))
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
		return c.fail(ctx, err, i18n.T("The alternate host(s) were not added."), func() { _ = r.s.RemoveAliases(added) })
	}

	a.con.Println()
	a.log.Success(i18n.T("Alternate host(s) added successfully!"))
	a.log.Info(i18n.T("Added aliases:"))
	for _, domain := range added {
		a.log.Info(fmt.Sprintf("  - https://%s -> %s (%s)", domain, target, serves))
	}
	return nil
}
