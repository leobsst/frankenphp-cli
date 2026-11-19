package stack

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/caddyfile"
	"github.com/leobsst/frankenphp-cli/internal/core/compose"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/dnswatcher"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/core/hosts"
	"github.com/leobsst/frankenphp-cli/internal/core/password"
	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/core/resources"
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/core/validate"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// s3ReadyTimeout is how long start waits for the S3 server to answer.
const s3ReadyTimeout = 90 * time.Second

// Certs generates the site certificates.
type Certs interface {
	GenerateAll(ctx context.Context, domains []string, force, production bool) error
}

// Service starts, stops and restarts the stack of a data directory.
type Service struct {
	Dir    string // data directory
	Env    *env.Env
	Store  *store.Store
	Docker *docker.Manager
	Hosts  *hosts.Manager
	// Certs returns the certificate generator of a certs directory. It is
	// called only when certificates are needed: creating it creates the
	// directory.
	Certs func(certsDir string) (Certs, error)
	// Watcher returns the LAN DNS process of the data directory.
	Watcher func(dataDir string) (*dnswatcher.Watcher, error)
	Log     events.Log
}

// IsRunning reports whether the stack runs: its containers are up.
func IsRunning(ctx context.Context, d *docker.Manager, s *store.Store) (bool, error) {
	active, err := s.ActivePHPVersions()
	if err != nil {
		return false, err
	}
	return docker.IsRunning(d.ContainerStatuses(ctx), active), nil
}

// ResolvePHP resolves a requested PHP version ("" for
// DEFAULT_PHP_VERSION), validates it and warns when it is deprecated.
func ResolvePHP(requested string, e *env.Env, log events.Log) (string, error) {
	v := requested
	if v == "" {
		v = phpversions.ResolveDefault(e.Get("DEFAULT_PHP_VERSION"))
	}
	if err := phpversions.Validate(v); err != nil {
		return "", err
	}
	if w := phpversions.DeprecationWarning(v); w != "" {
		log.Warning(w)
	}
	return v, nil
}

// RemoveLegacyDNS removes the DNS container of earlier releases, if any: it
// would hold port 53.
func RemoveLegacyDNS(ctx context.Context, d *docker.Manager) {
	d.RemoveContainer(ctx, compose.DNSContainer)
}

// dbPasswords returns the configured root passwords of the MariaDB and
// MySQL servers to sync.
func dbPasswords(e *env.Env, engines []string) (map[string]string, error) {
	keys := map[string]string{"mariadb": "MARIADB_ROOT_PASSWORD", "mysql": "MYSQL_ROOT_PASSWORD"}
	passwords := map[string]string{}
	for _, engine := range engines {
		if key, ok := keys[engine]; ok {
			p, err := e.Require(key)
			if err != nil {
				return nil, err
			}
			passwords[engine] = p
		}
	}
	return passwords, nil
}

// SyncPasswords applies the configured root passwords to the running
// MariaDB and MySQL servers.
func (s *Service) SyncPasswords(ctx context.Context) error {
	engines := s.Env.DBEngines()
	passwords, err := dbPasswords(s.Env, engines)
	if err != nil {
		return err
	}
	return password.New(s.Dir, s.Docker, s.Log).SyncAll(ctx, engines, passwords)
}

// versionsOf returns the distinct PHP versions of domains, sorted.
func versionsOf(domains []store.Domain) []string {
	var versions []string
	for _, d := range domains {
		if !slices.Contains(versions, d.PHPVersion) {
			versions = append(versions, d.PHPVersion)
		}
	}
	slices.Sort(versions)
	return versions
}

// reportedError reports the messages of a failure, then returns err as
// already reported.
func (s *Service) reportedError(err error, messages ...string) error {
	for _, m := range messages {
		s.Log.Error(m)
	}
	return &events.ReportedError{Err: err}
}

// --- S3 ----------------------------------------------------------------------

// SyncS3 prepares the S3 server once its container runs: the node layout
// on first start, the global key allowed on every bucket, then the hosts
// entries and DNS records of the bucket hosts.
func (s *Service) SyncS3(ctx context.Context) error {
	s.Log.Step(i18n.T("Waiting for the S3 server..."))
	g := s3.NewGarage(s.Env)
	if err := g.WaitReady(ctx, s3ReadyTimeout); err != nil {
		return err
	}
	if err := g.EnsureLayout(ctx); err != nil {
		return err
	}
	id, err := s.Env.Require(s3.AccessKeyKey)
	if err != nil {
		return err
	}
	secret, err := s.Env.Require(s3.SecretKeyKey)
	if err != nil {
		return err
	}
	if err := g.EnsureGlobalKey(ctx, id, secret); err != nil {
		return err
	}
	return SyncS3Hosts(ctx, s.Dir, s.Env, s.Store, s.Hosts, g)
}

// SyncS3Hosts brings the hosts entries of the bucket hosts in line with
// the buckets of the server, caches the bucket names, and refreshes the
// DNS records.
func SyncS3Hosts(ctx context.Context, dir string, e *env.Env, s *store.Store, h *hosts.Manager, g *s3.Garage) error {
	names, err := g.BucketNames(ctx)
	if err != nil {
		return err
	}
	old, err := s.S3Buckets()
	if err != nil {
		return err
	}
	for _, b := range old {
		if !slices.Contains(names, b) {
			if _, err := h.RemoveEntry(ctx, "127.0.0.1", s3.BucketHost(b)); err != nil {
				return err
			}
		}
	}
	for _, b := range names {
		if _, err := h.AddEntry(ctx, "127.0.0.1", s3.BucketHost(b)); err != nil {
			return err
		}
	}
	if err := s.SetS3Buckets(names); err != nil {
		return err
	}
	if dns.Enabled(e) {
		return dns.Sync(dir, e, s)
	}
	return nil
}

// RemoveS3Hosts removes the hosts entries of the S3 host and of the cached
// bucket hosts, errors ignored. The cache is kept for the next start.
func RemoveS3Hosts(ctx context.Context, h *hosts.Manager, s *store.Store) {
	names := []string{s3.Domain}
	buckets, _ := s.S3Buckets()
	for _, b := range buckets {
		names = append(names, s3.BucketHost(b))
	}
	for _, name := range names {
		if found, err := h.HasEntry("127.0.0.1", name); err == nil && found {
			_, _ = h.RemoveEntry(ctx, "127.0.0.1", name)
		}
	}
}

// --- start -------------------------------------------------------------------

// StartOptions are the options of Start.
type StartOptions struct {
	// Domains are added to the registered ones with PHP; nil starts the
	// registered domains only
	Domains []string
	// Path is the projects root, "" for DEFAULT_PROJECT_PATH
	Path string
	// PHP is the version of the new domains, "" for DEFAULT_PHP_VERSION
	PHP      string
	ForceSSL bool
	// Build rebuilds the PHP images even when up to date
	Build bool
}

// StartResult is what Start brought up.
type StartResult struct {
	Domains  []store.Domain // the PHP domains served
	Versions []string       // their PHP versions, sorted
	Statics  []store.StaticHost
	// DNSListen is the address of the LAN DNS server, "" when disabled
	DNSListen string
	// Watched reports that the domains follow the LAN IP of the machine
	Watched bool
}

// Start starts the stack. It starts even without any domain: the reverse
// proxy and databases come up alone.
func (s *Service) Start(ctx context.Context, o StartOptions) (StartResult, error) {
	e, st, d := s.Env, s.Store, s.Docker
	php, err := ResolvePHP(o.PHP, e, s.Log)
	if err != nil {
		return StartResult{}, err
	}
	for _, key := range []string{"UID", "GID"} {
		if _, err := e.Require(key); err != nil {
			return StartResult{}, s.reportedError(err, err.Error(),
				i18n.T("Please set UID and GID: frankenmanager configuration set UID <id>"))
		}
	}
	customPath := o.Path
	if customPath == "" {
		customPath = e.Get("DEFAULT_PROJECT_PATH")
	}

	// Checked before anything is written to the configuration
	if running, err := IsRunning(ctx, d, st); err != nil {
		return StartResult{}, err
	} else if running {
		return StartResult{}, i18n.NewError("The server is already running.")
	}

	engines := e.DBEngines()
	for _, g := range []struct{ engine, key, name string }{
		{"mariadb", "MARIADB_ROOT_PASSWORD", "MariaDB"},
		{"mysql", "MYSQL_ROOT_PASSWORD", "MySQL"},
		{"postgresql", "POSTGRES_PASSWORD", "PostgreSQL"},
	} {
		if slices.Contains(engines, g.engine) && e.Get(g.key) == "" {
			if err := e.Set(g.key, env.GeneratePassword()); err != nil {
				return StartResult{}, err
			}
			s.Log.Info(i18n.Tf("%s password generated and saved to the configuration", g.name))
		}
	}
	generated, err := soketi.EnsureCredentials(e)
	if err != nil {
		return StartResult{}, err
	}
	if len(generated) > 0 {
		s.Log.Info(i18n.Tf("Soketi %s generated and saved to the configuration", strings.Join(generated, ", ")))
	}
	if generated, err = s3.EnsureCredentials(e); err != nil {
		return StartResult{}, err
	}
	if len(generated) > 0 {
		s.Log.Info(i18n.Tf("S3 %s generated and saved to the configuration", strings.Join(generated, ", ")))
	}

	caddyDir := e.ResolveDir("CADDY_DIR", "./caddy", s.Dir)
	databaseDir := e.ResolveDir("DATABASE_DIR", "./database", s.Dir)

	// Registered domains keep their version, new ones take PHP
	registered, err := st.Domains()
	if err != nil {
		return StartResult{}, err
	}
	var names []string
	for _, r := range registered {
		names = append(names, r.Name)
	}
	merged := slices.Clone(registered)
	if o.Domains == nil {
		if len(registered) > 0 {
			s.Log.Info(i18n.Tf("Using registered domains: %s", strings.Join(names, ", ")))
		} else {
			s.Log.Info(i18n.T("No PHP domain registered: starting without PHP container."))
			s.Log.Info(i18n.T(`Add hosts later with: frankenmanager add-host "myapp.test"`))
		}
	} else {
		var added []string
		for _, name := range o.Domains {
			if !slices.Contains(names, name) {
				added = append(added, name)
				merged = append(merged, store.Domain{Name: name, PHPVersion: php})
			}
		}
		if len(registered) > 0 {
			s.Log.Info(i18n.Tf("Registered domains: %s", strings.Join(names, ", ")))
			if len(added) > 0 {
				s.Log.Info(i18n.Tf("New domains (PHP %s): %s", php, strings.Join(added, ", ")))
			}
		}
	}

	statics, err := st.StaticHosts()
	if err != nil {
		return StartResult{}, err
	}
	// The project path is only needed by PHP containers and static hosts
	if len(merged) > 0 || len(statics) > 0 {
		if customPath == "" {
			err := i18n.NewError("No project path specified and DEFAULT_PROJECT_PATH is not set " +
				"(frankenmanager configuration set DEFAULT_PROJECT_PATH <path>)")
			return StartResult{}, s.reportedError(err, err.Error())
		}
		if err := validate.Directory(customPath); err != nil {
			if o.Path == "" && !e.IsSet("DEFAULT_PROJECT_PATH") {
				// The OS default, never chosen: tell how to choose
				err := i18n.Errorf("The default projects directory does not exist: %s "+
					"(create it or run frankenmanager configuration set DEFAULT_PROJECT_PATH <path>)", customPath)
				return StartResult{}, s.reportedError(err, err.Error())
			}
			return StartResult{}, err
		}
	}
	for _, m := range merged {
		if err := validate.Domain(m.Name); err != nil {
			return StartResult{}, err
		}
	}
	// Duplicates removed, order kept
	var unique []store.Domain
	var all []string
	for _, m := range merged {
		if !slices.Contains(all, m.Name) {
			unique = append(unique, m)
			all = append(all, m.Name)
		}
	}
	merged = unique
	versions := versionsOf(merged)

	res := resources.New(s.Dir, s.Log)
	for _, v := range versions {
		if err := res.EnsurePHPVersionConfig(v); err != nil {
			return StartResult{}, err
		}
	}

	h := s.Hosts
	var hostsAdded []string
	fail := func(err error) (StartResult, error) {
		s.Log.Error(i18n.Tf("An error occurred: %v", err))
		s.Log.Error(i18n.T("Cleaning up..."))
		for _, domain := range hostsAdded {
			_, _ = h.RemoveEntry(ctx, "127.0.0.1", domain)
		}
		// Registered domains are unrelated to this failure: put them back
		_ = st.SetDomains(registered)
		return StartResult{}, s.reportedError(err, i18n.T("Cleanup complete. The server did not start."))
	}

	aliasEntries, err := st.AliasEntries()
	if err != nil {
		return StartResult{}, err
	}
	proxies, err := st.Proxies()
	if err != nil {
		return StartResult{}, err
	}
	served := slices.Clone(all)
	for _, al := range aliasEntries {
		served = append(served, al.Alias)
	}
	for _, p := range proxies {
		served = append(served, p.Domain)
	}
	for _, sh := range statics {
		served = append(served, sh.Domain)
	}
	for _, ws := range soketi.Domains(e) {
		if !slices.Contains(all, ws) {
			served = append(served, ws)
		}
	}
	// The bucket hosts get their entries once the S3 server answers
	if s3.Enabled(e) && !slices.Contains(all, s3.Domain) {
		served = append(served, s3.Domain)
	}

	certs, err := s.Certs(filepath.Join(caddyDir, "certs"))
	if err != nil {
		return fail(err)
	}
	if err := certs.GenerateAll(ctx, served, o.ForceSSL, e.IsProduction()); err != nil {
		return fail(err)
	}
	for _, domain := range served {
		if _, err := h.AddEntry(ctx, "127.0.0.1", domain); err != nil {
			return fail(err)
		}
		hostsAdded = append(hostsAdded, domain)
	}

	g := caddyfile.New(caddyDir, s.Dir, s.Log)
	if err := g.GenerateAllForVersions(merged); err != nil {
		return fail(err)
	}
	if err := g.GenerateStatic(statics); err != nil {
		return fail(err)
	}
	if err := caddyfile.WriteMain(caddyDir, caddyfile.Main{
		Domains: merged, Aliases: aliasEntries, Proxies: proxies, Statics: statics,
		DBAdminTarget: e.DBAdminTarget(), WebsocketTarget: soketi.Target(e),
		S3Target: s3.ProxyTarget(e),
	}); err != nil {
		return fail(err)
	}

	if err := st.SetDomains(merged); err != nil {
		return fail(err)
	}
	if customPath != "" {
		if err := st.SetSetting(store.SettingProjectPath, customPath); err != nil {
			return fail(err)
		}
	}

	if err := dns.Sync(s.Dir, e, st); err != nil {
		return fail(err)
	}
	listen, err := dns.ListenAddress(e)
	if err != nil {
		return fail(err)
	}

	// MySQL ignores a world-writable option file
	cnf := filepath.Join(databaseDir, ".my-healthcheck.cnf")
	if info, err := os.Stat(cnf); err == nil && info.Mode().Perm()&0o022 != 0 {
		if err := os.Chmod(cnf, 0o640); err != nil {
			return fail(err)
		}
	}

	if err := BuildImages(ctx, d, e, s.Log, customPath, versions, o.Build); err != nil {
		return fail(err)
	}
	d.ComposeDown(ctx)
	if listen != "" {
		RemoveLegacyDNS(ctx, d)
	}
	if err := ComposeUp(ctx, d, e, versions, customPath); err != nil {
		return fail(err)
	}
	if err := s.SyncPasswords(ctx); err != nil {
		return fail(err)
	}
	if s3.Enabled(e) {
		if err := s.SyncS3(ctx); err != nil {
			return fail(err)
		}
	}

	w, err := s.Watcher(s.Dir)
	if err != nil {
		return fail(err)
	}
	watched, err := w.Update(ctx, e)
	if err != nil {
		return fail(err)
	}
	return StartResult{Domains: merged, Versions: versions, Statics: statics, DNSListen: listen, Watched: watched}, nil
}

// --- stop --------------------------------------------------------------------

// Stop stops the stack and the LAN DNS process, and removes the hosts
// entries.
func (s *Service) Stop(ctx context.Context) error {
	st, d := s.Store, s.Docker
	if running, err := IsRunning(ctx, d, st); err != nil {
		return err
	} else if !running {
		return i18n.Errorf("The server is already stopped.")
	}

	s.Log.Step(i18n.T("Stopping web server..."))
	if w, err := s.Watcher(s.Dir); err == nil {
		w.Stop(ctx)
	}
	d.ComposeDown(ctx)

	names, err := st.DomainNames()
	if err != nil {
		return err
	}
	aliases, err := st.Aliases()
	if err != nil {
		return err
	}
	for _, al := range aliases {
		names = append(names, al.Alias)
	}
	proxies, err := st.Proxies()
	if err != nil {
		return err
	}
	for _, p := range proxies {
		names = append(names, p.Domain)
	}
	statics, err := st.StaticHostNames()
	if err != nil {
		return err
	}
	names = append(names, statics...)
	h := s.Hosts
	for _, name := range names {
		_, _ = h.RemoveEntry(ctx, "127.0.0.1", name) // errors ignored
	}
	// The Soketi host, also when SOKETI_ENABLED was turned off while running
	if found, err := h.HasEntry("127.0.0.1", soketi.Domain); err == nil && found {
		_, _ = h.RemoveEntry(ctx, "127.0.0.1", soketi.Domain)
	}
	// The S3 hosts, also when S3_ENABLED was turned off while running
	RemoveS3Hosts(ctx, h, st)
	return nil
}

// --- restart -----------------------------------------------------------------

// RestartTargets are the parts of the stack Restart can restart alone, in
// the order they are processed.
var RestartTargets = []string{"caddy", "database", "cache", "phpmyadmin", "dns", "soketi", "s3"}

// restartContainers maps the single-container targets to their container.
var restartContainers = map[string]string{
	"cache":      compose.RedisContainer,
	"phpmyadmin": compose.PHPMyAdminContainer,
	"soketi":     soketi.ContainerName,
}

// errBuildWithoutProjectPath is raised when restart --build cannot find
// the projects root.
var errBuildWithoutProjectPath = i18n.NewError("No project path known: set DEFAULT_PROJECT_PATH (frankenmanager configuration set) " +
	"or use 'stop' then 'start --build --path <path>'.")

// Restart restarts the stack, or only targets (see RestartTargets) when
// not empty. build rebuilds the PHP images first.
func (s *Service) Restart(ctx context.Context, targets []string, forceSSL, build bool) error {
	e, st, d := s.Env, s.Store, s.Docker
	if running, err := IsRunning(ctx, d, st); err != nil {
		return err
	} else if !running {
		return i18n.NewError("The server is not running.")
	}
	active, err := st.ActivePHPVersions()
	if err != nil {
		return err
	}

	all := len(targets) == 0
	has := func(t string) bool { return slices.Contains(targets, t) }
	if all {
		s.Log.Info(i18n.T("Restarting web server..."))
	} else {
		s.Log.Info(i18n.Tf("Restarting %s...", strings.Join(targets, ", ")))
	}

	if all || has("caddy") {
		domains, err := st.DomainNames()
		if err != nil {
			return err
		}
		domains = append(domains, soketi.Domains(e)...)
		if s3.Enabled(e) {
			domains = append(domains, s3.Domain)
		}
		s.Log.Break()
		s.Log.Step(i18n.T("Generating SSL certificates..."))
		// <CADDY_DIR>/certs, like the other commands, not CERTS_DIR
		certs, err := s.Certs(e.CertsDir(s.Dir))
		if err != nil {
			return err
		}
		if err := certs.GenerateAll(ctx, domains, forceSSL, e.IsProduction()); err != nil {
			return err
		}
	}

	engines := e.DBEngines()
	redis, websocket, dnsOn := e.RedisEnabled(), soketi.Enabled(e), dns.Enabled(e)

	if build && len(active) > 0 {
		customPath, err := ProjectPath(e, st)
		if err != nil {
			return err
		}
		if customPath == "" {
			return errBuildWithoutProjectPath
		}
		if err := BuildImages(ctx, d, e, s.Log, customPath, active, true); err != nil {
			return err
		}
		// Recreate the PHP containers on the new images
		if err := ComposeUp(ctx, d, e, active, customPath); err != nil {
			return err
		}
	} else if build {
		s.Log.Info(i18n.T("No PHP domain registered: no image to build."))
	}

	if all || has("dns") {
		if dnsOn {
			if err := s.refreshDNS(ctx); err != nil {
				return err
			}
		}
		// Restarts the server process, picking up the DNS_* changes made
		// since the last start; stops it when DNS_SERVER was turned off
		w, err := s.Watcher(s.Dir)
		if err != nil {
			return err
		}
		if _, err := w.Update(ctx, e); err != nil {
			return err
		}
	}

	if all {
		d.RestartAll(ctx, docker.Containers{
			PHPVersions: active, DBEngines: engines, Production: e.IsProduction(),
			Redis: redis, Websocket: websocket, S3: s3.Enabled(e),
		})
	} else {
		for _, t := range targets {
			switch {
			case t == "caddy":
				// Every FrankenPHP container, then the reverse proxy
				for _, v := range active {
					d.RestartContainer(ctx, phpversions.ContainerName(v))
				}
				d.RestartContainer(ctx, compose.ReverseProxyContainer)
			case t == "database":
				for _, engine := range engines {
					if name, ok := compose.DBContainers[engine]; ok {
						d.RestartContainer(ctx, name)
					}
				}
			case t == "cache" && !redis:
				s.Log.Warning(i18n.T("Redis is disabled: run 'frankenmanager service enable redis' to use it."))
			case t == "soketi" && !websocket:
				s.Log.Warning(i18n.T("Soketi is disabled: run 'frankenmanager service enable soketi' to use it."))
			case t == "s3" && !s3.Enabled(e):
				s.Log.Warning(i18n.T("S3 is disabled: run 'frankenmanager service enable s3' to use it."))
			case t == "s3":
				// Garage, then the ACL gateway in front of it
				d.RestartContainer(ctx, s3.ContainerName)
				d.RestartContainer(ctx, s3.GatewayContainer)
			case t == "dns" && !dnsOn:
				s.Log.Warning(i18n.T("The LAN DNS server is disabled: run 'frankenmanager service enable dns'."))
			case t == "dns":
				// The background process, restarted above
			default:
				d.RestartContainer(ctx, restartContainers[t])
			}
		}
	}

	if all || has("database") {
		if err := s.SyncPasswords(ctx); err != nil {
			return err
		}
	}
	return nil
}

// refreshDNS rewrites the records with the current LAN IP and removes the
// DNS container of earlier releases, which would hold port 53.
func (s *Service) refreshDNS(ctx context.Context) error {
	if err := dns.Sync(s.Dir, s.Env, s.Store); err != nil {
		return err
	}
	RemoveLegacyDNS(ctx, s.Docker)
	ip, err := dns.ResolveIP(s.Env)
	if err != nil {
		return err
	}
	s.Log.Info(i18n.Tf("LAN DNS server: domains resolve to %s", ip))
	return nil
}
