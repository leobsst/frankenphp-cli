// Package domains manages the hosts of a running server: PHP domains,
// proxy hosts, static hosts and their alternate hosts (aliases). It reports
// its progress as events and returns what it did: the CLI prints the
// summary, a daemon can send it to the dashboard.
package domains

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/core/caddyfile"
	"github.com/leobsst/frankenphp-cli/internal/core/compose"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
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

// ErrNoProjectPath is raised when a new PHP container or a static host
// needs the projects root and none is known.
var ErrNoProjectPath = i18n.NewError("No project path known: set DEFAULT_PROJECT_PATH (frankenmanager configuration set) " +
	"or restart the server with --path.")

// Certs generates the site certificates.
type Certs = stack.Certs

// Service works on the hosts of a running server. The caller checks that
// the server runs and owns the store.
type Service struct {
	Dir      string // data directory
	CaddyDir string
	Env      *env.Env
	Store    *store.Store
	Docker   *docker.Manager
	Hosts    *hosts.Manager
	// Certs returns the certificate generator. It is called only when
	// certificates are needed: creating it creates the certs directory.
	Certs func() (Certs, error)
	Log   events.Log
}

func (s *Service) generator(log caddyfile.Logger) *caddyfile.Generator {
	return caddyfile.New(s.CaddyDir, s.Dir, log)
}

// WriteMain regenerates the main Caddyfile from the database.
func WriteMain(caddyDir string, e *env.Env, s *store.Store) error {
	domains, err := s.Domains()
	if err != nil {
		return err
	}
	aliases, err := s.AliasEntries()
	if err != nil {
		return err
	}
	proxies, err := s.Proxies()
	if err != nil {
		return err
	}
	statics, err := s.StaticHosts()
	if err != nil {
		return err
	}
	return caddyfile.WriteMain(caddyDir, caddyfile.Main{
		Domains: domains, Aliases: aliases, Proxies: proxies, Statics: statics,
		DBAdminTarget: e.DBAdminTarget(), WebsocketTarget: soketi.Target(e),
		S3Target: s3.ProxyTarget(e),
	})
}

func (s *Service) writeMain() error { return WriteMain(s.CaddyDir, s.Env, s.Store) }

// projectPath returns the projects root the server runs with,
// ErrNoProjectPath when none is known.
func (s *Service) projectPath() (string, error) {
	customPath, err := stack.ProjectPath(s.Env, s.Store)
	if err == nil && customPath == "" {
		err = ErrNoProjectPath
	}
	return customPath, err
}

// startVersion builds the image of a PHP version not running yet, then
// brings it up with the others (active plus phpVersion).
func (s *Service) startVersion(ctx context.Context, phpVersion string, active []string) error {
	customPath, err := s.projectPath()
	if err != nil {
		return err
	}
	if _, err := stack.BuildImage(ctx, s.Docker, s.Env, s.Log, customPath, phpVersion, false); err != nil {
		return err
	}
	s.Log.Step(i18n.Tf("Starting PHP %s container...", phpVersion))
	versions := active
	if !slices.Contains(versions, phpVersion) {
		versions = append(slices.Clone(active), phpVersion)
	}
	return stack.ComposeUp(ctx, s.Docker, s.Env, versions, customPath)
}

// restartVersion restarts the container of a PHP version.
func (s *Service) restartVersion(ctx context.Context, phpVersion string) {
	name := phpversions.ContainerName(phpVersion)
	s.Log.Step(i18n.Tf("Restarting %s...", name))
	s.Docker.RestartContainer(ctx, name)
}

// RestartProxy restarts the reverse proxy, to pick up new routes.
func RestartProxy(ctx context.Context, d *docker.Manager, log events.Log) {
	log.Step(i18n.T("Restarting reverse proxy..."))
	d.RestartContainer(ctx, compose.ReverseProxyContainer)
}

func (s *Service) restartProxy(ctx context.Context) { RestartProxy(ctx, s.Docker, s.Log) }

// cleanup undoes the hosts entries of a failed operation.
type cleanup struct {
	s     *Service
	added []string
}

func (c *cleanup) add(ctx context.Context, domain string) error {
	if _, err := c.s.Hosts.AddEntry(ctx, "127.0.0.1", domain); err != nil {
		return err
	}
	c.added = append(c.added, domain)
	return nil
}

// fail reports err, removes the hosts entries added, runs undo, reports
// the outcome (translated, "The host(s) were not added.") and returns err
// as already reported.
func (c *cleanup) fail(ctx context.Context, err error, outcome string, undo func()) error {
	c.s.Log.Error(i18n.Tf("An error occurred: %v", err))
	c.s.Log.Error(i18n.T("Cleaning up..."))
	for _, domain := range c.added {
		_, _ = c.s.Hosts.RemoveEntry(ctx, "127.0.0.1", domain)
	}
	if undo != nil {
		undo()
	}
	c.s.Log.Error(i18n.Tf("Cleanup complete. %s", outcome))
	return &events.ReportedError{Err: err}
}

// CleanRoot normalizes a valid root: slashes, no trailing one.
func CleanRoot(root string) string {
	return path.Clean(strings.ReplaceAll(root, `\`, "/"))
}

// checkProjectDir checks that the project directory of a root, its first
// segment, exists in the projects root: the deeper directories may not be
// built yet (dist/).
func checkProjectDir(customPath, root string) error {
	first, _, _ := strings.Cut(CleanRoot(root), "/")
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

// isDir reports whether p is an existing directory.
func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
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
func (s *Service) newHosts(domains []string) ([]string, error) {
	taken, err := takenHosts(s.Store)
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
			s.Log.Info(i18n.Tf("Domain '%s' is already configured, skipping.", domain))
		case slices.Contains(added, domain):
			s.Log.Info(i18n.Tf("Domain '%s' is duplicated in input, skipping.", domain))
		default:
			added = append(added, domain)
		}
	}
	return added, nil
}

// PHPResult is what an operation on PHP domains did.
type PHPResult struct {
	Domains []string // empty when nothing was done
	PHP     string
}

// AddPHP adds PHP domains with a PHP version ("" for DEFAULT_PHP_VERSION),
// served from root (relative to the projects root, "" for
// <domain without TLD>/public).
func (s *Service) AddPHP(ctx context.Context, domains []string, php, root string, forceSSL bool) (PHPResult, error) {
	if root != "" {
		if err := validate.Root(root); err != nil {
			return PHPResult{}, err
		}
	}
	added, err := s.newHosts(domains)
	if err != nil {
		return PHPResult{}, err
	}
	if len(added) == 0 {
		s.Log.Info(i18n.T("No new domains to add."))
		return PHPResult{}, nil
	}
	certs, err := s.Certs()
	if err != nil {
		return PHPResult{}, err
	}
	php, err = stack.ResolvePHP(php, s.Env, s.Log)
	if err != nil {
		return PHPResult{}, err
	}
	if root != "" {
		customPath, err := s.projectPath()
		if err != nil {
			return PHPResult{}, err
		}
		if err := checkProjectDir(customPath, root); err != nil {
			return PHPResult{}, err
		}
		root = CleanRoot(root)
	}
	active, err := s.Store.ActivePHPVersions()
	if err != nil {
		return PHPResult{}, err
	}
	if err := resources.New(s.Dir, s.Log).EnsurePHPVersionConfig(php); err != nil {
		return PHPResult{}, err
	}

	c := &cleanup{s: s}
	err = func() error {
		s.Log.Info(i18n.Tf("Adding %d new domain(s) with PHP %s...", len(added), php))
		s.Log.Step(i18n.T("Generating SSL certificates..."))
		if err := certs.GenerateAll(ctx, added, forceSSL, s.Env.IsProduction()); err != nil {
			return err
		}
		s.Log.Step(i18n.T("Adding hosts entries..."))
		for _, domain := range added {
			if err := c.add(ctx, domain); err != nil {
				return err
			}
		}
		s.Log.Step(i18n.T("Generating Caddyfiles..."))
		if err := s.generator(s.Log).GenerateForVersionRoot(added, php, root); err != nil {
			return err
		}
		if err := s.Store.AddDomains(added, php); err != nil {
			return err
		}
		if err := s.writeMain(); err != nil {
			return err
		}
		if err := dns.Sync(s.Dir, s.Env, s.Store); err != nil {
			return err
		}
		if !slices.Contains(active, php) {
			if err := s.startVersion(ctx, php, active); err != nil {
				return err
			}
		} else {
			s.restartVersion(ctx, php)
		}
		s.restartProxy(ctx)
		return nil
	}()
	if err != nil {
		// The domains are removed from the database too
		return PHPResult{}, c.fail(ctx, err, i18n.T("The host(s) were not added."),
			func() { _ = s.Store.RemoveDomains(added) })
	}
	return PHPResult{Domains: added, PHP: php}, nil
}

// ProxyResult is what AddProxy did.
type ProxyResult struct {
	Domains []string // empty when nothing was done
	Target  string
}

// AddProxy adds domains forwarding to a raw upstream: certificate, hosts
// entry and main Caddyfile only, no PHP container.
func (s *Service) AddProxy(ctx context.Context, domains []string, target string, forceSSL bool) (ProxyResult, error) {
	added, err := s.newHosts(domains)
	if err != nil {
		return ProxyResult{}, err
	}
	if len(added) == 0 {
		s.Log.Info(i18n.T("No new domains to add."))
		return ProxyResult{}, nil
	}
	certs, err := s.Certs()
	if err != nil {
		return ProxyResult{}, err
	}
	if err := validate.ProxyTarget(target); err != nil {
		return ProxyResult{}, err
	}
	c := &cleanup{s: s}
	err = func() error {
		s.Log.Info(i18n.Tf("Adding %d new proxy host(s) -> %s...", len(added), target))
		s.Log.Step(i18n.T("Generating SSL certificates..."))
		if err := certs.GenerateAll(ctx, added, forceSSL, s.Env.IsProduction()); err != nil {
			return err
		}
		s.Log.Step(i18n.T("Adding hosts entries..."))
		for _, domain := range added {
			if err := c.add(ctx, domain); err != nil {
				return err
			}
		}
		if err := s.Store.AddProxies(added, target); err != nil {
			return err
		}
		if err := s.writeMain(); err != nil {
			return err
		}
		if err := dns.Sync(s.Dir, s.Env, s.Store); err != nil {
			return err
		}
		s.restartProxy(ctx)
		return nil
	}()
	if err != nil {
		return ProxyResult{}, c.fail(ctx, err, i18n.T("The proxy host(s) were not added."),
			func() { _ = s.Store.RemoveProxies(added) })
	}
	return ProxyResult{Domains: added, Target: target}, nil
}

// StaticResult is what AddStatic did.
type StaticResult struct {
	Hosts       []store.StaticHost // empty when nothing was done
	ProjectPath string             // projects root the roots are relative to
	SiteDir     string             // directory of the editable site files
}

// AddStatic adds domains whose files the reverse proxy serves from root
// (relative to the projects root, "" for <domain without TLD>), spa
// falling back to /index.html: certificate, hosts entry, site file and
// main Caddyfile, no PHP container.
func (s *Service) AddStatic(ctx context.Context, domains []string, root string, spa, forceSSL bool) (StaticResult, error) {
	if root != "" {
		if err := validate.Root(root); err != nil {
			return StaticResult{}, err
		}
	}
	added, err := s.newHosts(domains)
	if err != nil {
		return StaticResult{}, err
	}
	if len(added) == 0 {
		s.Log.Info(i18n.T("No new domains to add."))
		return StaticResult{}, nil
	}
	// The reverse proxy mounts the projects root to serve the files
	customPath, err := s.projectPath()
	if err != nil {
		return StaticResult{}, err
	}
	if root != "" {
		if err := checkProjectDir(customPath, root); err != nil {
			return StaticResult{}, err
		}
	}
	statics := make([]store.StaticHost, 0, len(added))
	for _, domain := range added {
		r := root
		if r == "" {
			r = caddyfile.SimpleDomain(domain)
		}
		statics = append(statics, store.StaticHost{Domain: domain, Root: CleanRoot(r), SPA: spa})
	}
	warned := map[string]bool{}
	for _, h := range statics {
		if dir := filepath.Join(customPath, filepath.FromSlash(h.Root)); !warned[dir] && !isDir(dir) {
			warned[dir] = true
			s.Log.Warning(i18n.Tf("Directory %s does not exist yet: build or create it before browsing.", dir))
		}
	}

	certs, err := s.Certs()
	if err != nil {
		return StaticResult{}, err
	}
	active, err := s.Store.ActivePHPVersions()
	if err != nil {
		return StaticResult{}, err
	}
	// Mounted by the reverse proxy since an earlier static host
	mounted := caddyfile.HasStaticSites(s.CaddyDir)
	c := &cleanup{s: s}
	g := s.generator(s.Log)
	err = func() error {
		s.Log.Info(i18n.Tf("Adding %d new static host(s)...", len(added)))
		s.Log.Step(i18n.T("Generating SSL certificates..."))
		if err := certs.GenerateAll(ctx, added, forceSSL, s.Env.IsProduction()); err != nil {
			return err
		}
		s.Log.Step(i18n.T("Adding hosts entries..."))
		for _, domain := range added {
			if err := c.add(ctx, domain); err != nil {
				return err
			}
		}
		s.Log.Step(i18n.T("Generating Caddyfiles..."))
		if err := g.GenerateStatic(statics); err != nil {
			return err
		}
		if err := s.Store.AddStaticHosts(statics); err != nil {
			return err
		}
		if err := s.writeMain(); err != nil {
			return err
		}
		if err := dns.Sync(s.Dir, s.Env, s.Store); err != nil {
			return err
		}
		if !mounted {
			// The reverse proxy is recreated with the static mounts
			s.Log.Step(i18n.T("Mounting the static sites in the reverse proxy..."))
			if err := stack.ComposeUp(ctx, s.Docker, s.Env, active, customPath); err != nil {
				return err
			}
		}
		s.restartProxy(ctx)
		return nil
	}()
	if err != nil {
		return StaticResult{}, c.fail(ctx, err, i18n.T("The static host(s) were not added."), func() {
			_ = s.Store.RemoveStaticHosts(added)
			_ = s.generator(events.NewLog(nil)).ArchiveStatic(added)
			_ = s.writeMain()
		})
	}
	return StaticResult{Hosts: statics, ProjectPath: customPath, SiteDir: g.StaticDir()}, nil
}

// AliasResult is what AddAlias did.
type AliasResult struct {
	Aliases []string // empty when nothing was done
	Target  string
	// Serves is what the aliases forward to: "PHP 8.4" for a domain,
	// "proxy <upstream>" for a proxy host
	Serves string
}

// AddAlias adds alternate hosts forwarding to an existing domain or proxy
// host.
func (s *Service) AddAlias(ctx context.Context, target string, aliases []string, forceSSL bool) (AliasResult, error) {
	domains, err := s.Store.DomainNames()
	if err != nil {
		return AliasResult{}, err
	}
	proxies, err := s.Store.Proxies()
	if err != nil {
		return AliasResult{}, err
	}
	isProxy := func(d string) bool {
		return slices.ContainsFunc(proxies, func(p store.Proxy) bool { return p.Domain == d })
	}
	statics, err := s.Store.StaticHostNames()
	if err != nil {
		return AliasResult{}, err
	}
	if slices.Contains(statics, target) {
		return AliasResult{}, &validate.Error{Msg: i18n.Tf("'%s' is a static host: it cannot have alternate hosts. "+
			"Add another static host with the same --root instead.", target)}
	}
	var serves string
	if slices.Contains(domains, target) {
		php, _, err := s.Store.DomainPHPVersion(target)
		if err != nil {
			return AliasResult{}, err
		}
		serves = "PHP " + php
	} else if upstream, ok, err := s.Store.ProxyTarget(target); err != nil {
		return AliasResult{}, err
	} else if ok {
		serves = "proxy " + upstream
	} else {
		return AliasResult{}, &validate.Error{Msg: i18n.Tf("Domain '%s' is not configured. Add it first with 'add-host'.", target)}
	}
	existing, err := s.Store.Aliases()
	if err != nil {
		return AliasResult{}, err
	}
	isAlias := func(d string) bool {
		return slices.ContainsFunc(existing, func(al store.Alias) bool { return al.Alias == d })
	}

	var added []string
	for _, domain := range aliases {
		if err := validate.Domain(domain); err != nil {
			return AliasResult{}, err
		}
		switch {
		case domain == target:
			s.Log.Info(i18n.Tf("'%s' is the target domain itself, skipping.", domain))
		case slices.Contains(domains, domain), isProxy(domain), slices.Contains(statics, domain):
			s.Log.Info(i18n.Tf("Domain '%s' is already a registered host, skipping.", domain))
		case isAlias(domain):
			s.Log.Info(i18n.Tf("'%s' is already configured as an alias, skipping.", domain))
		case slices.Contains(added, domain):
			s.Log.Info(i18n.Tf("'%s' is duplicated in input, skipping.", domain))
		default:
			added = append(added, domain)
		}
	}
	if len(added) == 0 {
		s.Log.Info(i18n.T("No new alternate hosts to add."))
		return AliasResult{}, nil
	}

	certs, err := s.Certs()
	if err != nil {
		return AliasResult{}, err
	}
	c := &cleanup{s: s}
	err = func() error {
		s.Log.Info(i18n.Tf("Adding %d alternate host(s) for '%s'...", len(added), target))
		s.Log.Step(i18n.T("Generating SSL certificates..."))
		if err := certs.GenerateAll(ctx, added, forceSSL, s.Env.IsProduction()); err != nil {
			return err
		}
		s.Log.Step(i18n.T("Adding hosts entries..."))
		for _, domain := range added {
			if err := c.add(ctx, domain); err != nil {
				return err
			}
		}
		if err := s.Store.AddAliases(added, target); err != nil {
			return err
		}
		if err := s.writeMain(); err != nil {
			return err
		}
		if err := dns.Sync(s.Dir, s.Env, s.Store); err != nil {
			return err
		}
		s.restartProxy(ctx)
		return nil
	}()
	if err != nil {
		return AliasResult{}, c.fail(ctx, err, i18n.T("The alternate host(s) were not added."),
			func() { _ = s.Store.RemoveAliases(added) })
	}
	return AliasResult{Aliases: added, Target: target, Serves: serves}, nil
}
