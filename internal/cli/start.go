package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/caddyfile"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/password"
	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/core/resources"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/core/validate"
)

// phpVersionHelp is the help of the --php options.
var phpVersionHelp = fmt.Sprintf("PHP version (%s; %s deprecated). Defaults to DEFAULT_PHP_VERSION",
	strings.Join(phpversions.Supported, ", "), strings.Join(phpversions.Deprecated, ", "))

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
		Short: "Start the FrankenPHP development server.",
		Long: `Start the FrankenPHP development server.

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
    frankenmanager start --build  # Force a rebuild of the PHP images`,
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
	f.StringVarP(&o.path, "path", "p", "", "Project root path (uses the DEFAULT_PROJECT_PATH configuration if not set)")
	f.BoolVar(&o.forceSSL, "force-ssl", false, "Force SSL certificate regeneration")
	f.StringVar(&o.php, "php", "", phpVersionHelp)
	f.BoolVar(&o.build, "build", false, "Rebuild the PHP images even if they are up to date")
	return cmd
}

// isRunning reports whether the stack of dir runs (DatabaseManager.is_running).
func (a *app) isRunning(ctx context.Context, d *docker.Manager, s *store.Store) (bool, error) {
	active, err := s.ActivePHPVersions()
	if err != nil {
		return false, err
	}
	return docker.IsRunning(d.ContainerStatuses(ctx), active), nil
}

// dbPasswords returns the configured root passwords of the MariaDB and MySQL
// servers to sync.
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

// start starts the stack. It starts even without any domain: the reverse
// proxy and databases come up alone.
func (a *app) start(ctx context.Context, domains []string, o startOptions) (err error) {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}

	php := o.php
	if php == "" {
		php = phpversions.ResolveDefault(e.Get("DEFAULT_PHP_VERSION"))
	}
	// An invalid --php is an error message
	if err := phpversions.Validate(php); err != nil {
		return err
	}
	if w := phpversions.DeprecationWarning(php); w != "" {
		a.log.Warning(w)
	}
	for _, key := range []string{"UID", "GID"} {
		if _, err := e.Require(key); err != nil {
			a.log.Error(err.Error())
			a.log.Error("Please set UID and GID: frankenmanager configuration set UID <id>")
			return exitError(1)
		}
	}
	customPath := o.path
	if customPath == "" {
		customPath = e.Get("DEFAULT_PROJECT_PATH")
	}

	d := a.docker(dir)
	s, err := a.openStore(dir)
	if err != nil {
		return err
	}
	defer closeStore(s, &err)

	// Checked before anything is written to the configuration
	if running, err := a.isRunning(ctx, d, s); err != nil {
		return err
	} else if running {
		return errors.New("The server is already running.") //nolint:staticcheck // user-facing message
	}

	engines := e.DBEngines()
	for _, g := range []struct{ engine, key, name string }{
		{"mariadb", "MARIADB_ROOT_PASSWORD", "MariaDB"},
		{"mysql", "MYSQL_ROOT_PASSWORD", "MySQL"},
		{"postgresql", "POSTGRES_PASSWORD", "PostgreSQL"},
	} {
		if slices.Contains(engines, g.engine) && e.Get(g.key) == "" {
			if err := e.Set(g.key, env.GeneratePassword()); err != nil {
				return err
			}
			a.log.Info(g.name + " password generated and saved to the configuration")
		}
	}
	generated, err := soketi.EnsureCredentials(e)
	if err != nil {
		return err
	}
	if len(generated) > 0 {
		a.log.Info(fmt.Sprintf("Soketi %s generated and saved to the configuration", strings.Join(generated, ", ")))
	}

	caddyDir := e.ResolveDir("CADDY_DIR", "./caddy", dir)
	databaseDir := e.ResolveDir("DATABASE_DIR", "./database", dir)

	// Registered domains keep their version, new ones take --php
	registered, err := s.Domains()
	if err != nil {
		return err
	}
	var names []string
	for _, r := range registered {
		names = append(names, r.Name)
	}
	merged := slices.Clone(registered)
	if domains == nil {
		if len(registered) > 0 {
			a.log.Info("Using registered domains: " + strings.Join(names, ", "))
		} else {
			a.log.Info("No PHP domain registered: starting without PHP container.")
			a.log.Info(`Add hosts later with: frankenmanager add-host "myapp.test"`)
		}
	} else {
		var added []string
		for _, name := range domains {
			if !slices.Contains(names, name) {
				added = append(added, name)
				merged = append(merged, store.Domain{Name: name, PHPVersion: php})
			}
		}
		if len(registered) > 0 {
			a.log.Info("Registered domains: " + strings.Join(names, ", "))
			if len(added) > 0 {
				a.log.Info(fmt.Sprintf("New domains (PHP %s): %s", php, strings.Join(added, ", ")))
			}
		}
	}

	// The project path is only needed by PHP containers
	if len(merged) > 0 {
		if customPath == "" {
			a.log.Error("No project path specified and DEFAULT_PROJECT_PATH is not set " +
				"(frankenmanager configuration set DEFAULT_PROJECT_PATH <path>)")
			return exitError(1)
		}
		if err := validate.Directory(customPath); err != nil {
			return err
		}
	}
	for _, m := range merged {
		if err := validate.Domain(m.Name); err != nil {
			return err
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

	res := resources.New(dir, a.log)
	for _, v := range versions {
		if err := res.EnsurePHPVersionConfig(v); err != nil {
			return err
		}
	}

	h := a.deps.Hosts(a.log)
	var hostsAdded []string
	fail := func(err error) error {
		a.log.Error(fmt.Sprintf("An error occurred: %v", err))
		a.log.Error("Cleaning up...")
		for _, domain := range hostsAdded {
			_, _ = h.RemoveEntry(ctx, "127.0.0.1", domain)
		}
		// Registered domains are unrelated to this failure: put them back
		_ = s.SetDomains(registered)
		a.log.Error("Cleanup complete. The server did not start.")
		return exitError(1)
	}

	aliasEntries, err := s.AliasEntries()
	if err != nil {
		return err
	}
	proxies, err := s.Proxies()
	if err != nil {
		return err
	}
	served := slices.Clone(all)
	for _, al := range aliasEntries {
		served = append(served, al.Alias)
	}
	for _, p := range proxies {
		served = append(served, p.Domain)
	}
	for _, ws := range soketi.Domains(e) {
		if !slices.Contains(all, ws) {
			served = append(served, ws)
		}
	}

	certs, err := a.ssl(filepath.Join(caddyDir, "certs"))
	if err != nil {
		return fail(err)
	}
	if err := certs.GenerateAll(ctx, served, o.forceSSL, e.IsProduction()); err != nil {
		return fail(err)
	}
	for _, domain := range served {
		if _, err := h.AddEntry(ctx, "127.0.0.1", domain); err != nil {
			return fail(err)
		}
		hostsAdded = append(hostsAdded, domain)
	}

	g := caddyfile.New(caddyDir, dir, a.log)
	if err := g.GenerateAllForVersions(merged); err != nil {
		return fail(err)
	}
	if err := caddyfile.WriteMain(caddyDir, caddyfile.Main{
		Domains: merged, Aliases: aliasEntries, Proxies: proxies,
		DBAdminTarget: e.DBAdminTarget(), WebsocketTarget: soketi.Target(e),
	}); err != nil {
		return fail(err)
	}

	if err := s.SetDomains(merged); err != nil {
		return fail(err)
	}
	if customPath != "" {
		if err := s.SetSetting(store.SettingProjectPath, customPath); err != nil {
			return fail(err)
		}
	}

	if err := dns.Sync(dir, e, s); err != nil {
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

	if err := stack.BuildImages(ctx, d, e, a.log, customPath, versions, o.build); err != nil {
		return fail(err)
	}
	d.ComposeDown(ctx)
	if err := stack.ComposeUp(ctx, d, e, versions, listen, customPath); err != nil {
		return fail(err)
	}

	passwords, err := dbPasswords(e, engines)
	if err != nil {
		return fail(err)
	}
	if err := password.New(dir, d, a.log).SyncAll(ctx, engines, passwords); err != nil {
		return fail(err)
	}

	w, err := a.deps.Watcher(dir)
	if err != nil {
		return fail(err)
	}
	watched, err := w.Update(ctx, e)
	if err != nil {
		return fail(err)
	}

	a.con.Println()
	a.log.Success("Web server started!")
	for _, v := range versions {
		var vd []string
		for _, m := range merged {
			if m.PHPVersion == v {
				vd = append(vd, m.Name)
			}
		}
		a.log.Info(fmt.Sprintf("  PHP %s: %s", v, strings.Join(vd, ", ")))
	}
	if soketi.Enabled(e) {
		a.log.Info(fmt.Sprintf("Soketi WebSocket server: wss://%s (apps: 127.0.0.1:%s)", soketi.Domain, soketi.Port(e)))
	}
	if listen != "" {
		ip, err := dns.ResolveIP(e)
		if err != nil {
			return err
		}
		a.log.Info("LAN DNS server listening on " + listen)
		addrs, err := dns.ServerAddresses(e)
		if err != nil {
			return err
		}
		for _, line := range dns.DescribeServerAddresses(addrs, ip) {
			a.log.Info(line)
		}
		if watched {
			a.log.Info("  The domains follow this machine's LAN IP when it changes.")
		}
		if strings.HasPrefix(ip, "127.") {
			a.log.Warning("No LAN IP detected: the domains resolve to this machine only. " +
				"Set DNS_IP to this machine's LAN IP: frankenmanager configuration set DNS_IP <ip>")
		}
	}
	return nil
}
