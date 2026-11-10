package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/caddyfile"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/core/resources"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

// without returns the versions of before missing from after, sorted.
func without(before, after []string) []string {
	var out []string
	for _, v := range before {
		if !slices.Contains(after, v) {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}

// stopVersions stops and removes the containers of versions left without
// domains, then regenerates the compose file without them.
func (a *app) stopVersions(ctx context.Context, r *server, orphaned, remaining []string) error {
	for _, v := range orphaned {
		name := phpversions.ContainerName(v)
		a.log.Info(fmt.Sprintf("Stopping %s (no more domains)...", name))
		r.d.StopContainer(ctx, name)
	}
	if len(orphaned) == 0 {
		return nil
	}
	listen, err := dns.ListenAddress(r.e)
	if err != nil {
		return err
	}
	return stack.GenerateCompose(r.dir, r.e, remaining, listen)
}

// --- remove-host -------------------------------------------------------------

func newRemoveHostCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "remove-host DOMAINS",
		Short: "Remove host(s) from the running server.",
		Long: `Remove host(s) from the running server.

This command allows you to remove domains while the server is running,
without having to stop and restart the entire environment.

The Caddyfile configurations will be archived (not deleted) in case
you need to restore them later.

Examples:
    frankenmanager remove-host "oldapp.test"
    frankenmanager remove-host "app1.test app2.test"`,
		Args: rangeArgs(1, "DOMAINS"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.removeHost(cmd.Context(), strings.Fields(args[0]))
		},
	}
}

// removeHost removes domains, aliases and proxy hosts, archiving the site
// files of the domains.
func (a *app) removeHost(ctx context.Context, names []string) (err error) {
	r, err := a.openRunning(ctx, "The server is not running.")
	if err != nil {
		return err
	}
	defer closeStore(r.s, &err)

	existing, err := r.s.Domains()
	if err != nil {
		return err
	}
	versionOf := map[string]string{}
	for _, d := range existing {
		versionOf[d.Name] = d.PHPVersion
	}
	aliasList, err := r.s.Aliases()
	if err != nil {
		return err
	}
	proxyList, err := r.s.Proxies()
	if err != nil {
		return err
	}
	isAlias := map[string]bool{}
	for _, al := range aliasList {
		isAlias[al.Alias] = true
	}
	isProxy := map[string]bool{}
	for _, p := range proxyList {
		isProxy[p.Domain] = true
	}

	var domains, aliases, proxies []string
	pick := func(list *[]string, name string) {
		if slices.Contains(*list, name) {
			a.log.Info(fmt.Sprintf("Domain '%s' is duplicated in input, skipping.", name))
		} else {
			*list = append(*list, name)
		}
	}
	for _, name := range names {
		switch {
		case versionOf[name] != "":
			pick(&domains, name)
		case isAlias[name]:
			pick(&aliases, name)
		case isProxy[name]:
			pick(&proxies, name)
		default:
			a.log.Warning(fmt.Sprintf("Domain '%s' is not configured, skipping.", name))
		}
	}
	if len(domains)+len(aliases)+len(proxies) == 0 {
		a.log.Info("No domains to remove.")
		return nil
	}

	h := a.deps.Hosts(a.log)
	removeEntry := func(name string) {
		if _, err := h.RemoveEntry(ctx, "127.0.0.1", name); err != nil {
			a.log.Warning(fmt.Sprintf("Failed to remove hosts entry for %s: %v", name, err))
		}
	}
	g := r.generator(a.log)
	before, err := r.s.ActivePHPVersions()
	if err != nil {
		return err
	}
	var touched []string // versions losing a domain
	for _, d := range domains {
		if !slices.Contains(touched, versionOf[d]) {
			touched = append(touched, versionOf[d])
		}
	}

	var removedAliases []string
	err = func() error {
		a.log.Info(fmt.Sprintf("Removing %d domain(s), %d alternate host(s), and %d proxy host(s)...",
			len(domains), len(aliases), len(proxies)))
		a.log.Info("Removing hosts entries...")
		for _, d := range domains {
			removeEntry(d)
		}
		if len(domains) > 0 {
			a.log.Info("Archiving Caddyfiles...")
			if err := g.Archive(domains); err != nil {
				return err
			}
		}
		if err := r.s.RemoveDomains(domains); err != nil {
			return err
		}
		// The requested aliases, then those of the removed domains
		if err := r.s.RemoveAliases(aliases); err != nil {
			return err
		}
		cascaded, err := r.s.RemoveAliasesForTargets(domains)
		if err != nil {
			return err
		}
		removedAliases = append(slices.Clone(aliases), cascaded...)
		if len(cascaded) > 0 {
			a.log.Info("Also removing alternate host(s) pointing to removed domain(s): " + strings.Join(cascaded, ", "))
		}
		for _, al := range removedAliases {
			removeEntry(al)
		}
		if err := r.s.RemoveProxies(proxies); err != nil {
			return err
		}
		for _, p := range proxies {
			removeEntry(p)
		}

		after, err := r.s.ActivePHPVersions()
		if err != nil {
			return err
		}
		orphaned := without(before, after)
		for _, v := range orphaned {
			name := phpversions.ContainerName(v)
			a.log.Info(fmt.Sprintf("Stopping %s (no more domains)...", name))
			r.d.StopContainer(ctx, name)
		}
		slices.Sort(touched)
		for _, v := range touched {
			if slices.Contains(after, v) {
				a.restartVersion(ctx, r.d, v)
			}
		}
		if len(orphaned) > 0 {
			listen, err := dns.ListenAddress(r.e)
			if err != nil {
				return err
			}
			if err := stack.GenerateCompose(r.dir, r.e, after, listen); err != nil {
				return err
			}
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
		a.log.Error(fmt.Sprintf("An error occurred: %v", err))
		return exitError(1)
	}

	a.con.Println()
	a.log.Success("Host(s) removed successfully!")
	if len(domains) > 0 {
		a.log.Info("Removed domains:")
		for _, d := range domains {
			a.log.Info(fmt.Sprintf("  - %s (PHP %s)", d, versionOf[d]))
		}
		a.log.Info("\nCaddyfiles archived to: " + g.ArchiveDir())
	}
	if len(removedAliases) > 0 {
		a.log.Info("Removed alternate hosts:")
		for _, al := range removedAliases {
			a.log.Info("  - " + al)
		}
	}
	if len(proxies) > 0 {
		a.log.Info("Removed proxy hosts:")
		for _, p := range proxies {
			a.log.Info("  - " + p)
		}
	}
	return nil
}

// --- restore-host ------------------------------------------------------------

func newRestoreHostCmd(a *app) *cobra.Command {
	var list, forceSSL bool
	var php string
	cmd := &cobra.Command{
		Use:   "restore-host [DOMAINS]",
		Short: "Restore archived host(s) to the running server.",
		Long: `Restore archived host(s) to the running server.

This command allows you to restore previously removed domains from the archive.
Use --list to see all available archived hosts.

Examples:
    frankenmanager restore-host --list
    frankenmanager restore-host "myapp.test"
    frankenmanager restore-host "app1.test" --php 8.5`,
		Args: maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if list {
				return a.listArchived()
			}
			if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
				a.con.Println(a.con.Color(ui.Yellow, "Specify domains to restore or use --list to see archived hosts."))
				return exitError(1)
			}
			return a.restoreHost(cmd.Context(), strings.Fields(args[0]), forceSSL, php)
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&list, "list", "l", false, "List all archived hosts")
	f.BoolVar(&forceSSL, "force-ssl", false, "Force SSL certificate regeneration")
	f.StringVar(&php, "php", "", phpVersionHelp)
	return cmd
}

// listArchived lists the archived site files (list_archived_hosts).
func (a *app) listArchived() error {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	g := caddyfile.New(e.ResolveDir("CADDY_DIR", "./caddy", dir), dir, a.log)
	archived, err := g.ListArchived()
	if err != nil {
		return err
	}
	if len(archived) == 0 {
		a.log.Info("No archived hosts found.")
		a.log.Info("Archive directory: " + g.ArchiveDir())
		return nil
	}
	t := ui.Table{Title: "Archived Hosts", Columns: []ui.Column{
		{Header: "Domain", Color: ui.Cyan}, {Header: "Caddyfile", Faint: true},
	}}
	for _, item := range archived {
		t.Rows = append(t.Rows, []string{item.FullDomain, item.SimpleDomain + "_Caddyfile"})
	}
	a.con.PrintTable(t)
	a.log.Info("\nArchive directory: " + g.ArchiveDir())
	a.log.Info(`Use 'frankenmanager restore-host "domain.test"' to restore a host.`)
	return nil
}

// restoreHost brings archived domains back, with --php or the default
// version, not their original one.
func (a *app) restoreHost(ctx context.Context, names []string, forceSSL bool, php string) (err error) {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	// Checked before the server state
	if php, err = a.phpVersion(php, e); err != nil {
		return err
	}
	r, err := a.openRunning(ctx, "The server is not running. Use 'start' command instead.")
	if err != nil {
		return err
	}
	defer closeStore(r.s, &err)

	existing, err := r.s.DomainNames()
	if err != nil {
		return err
	}
	// The names are compared without their TLD: app.test is active when
	// app.dev is registered
	short := func(d string) string {
		if i := strings.LastIndex(d, "."); i >= 0 {
			return d[:i]
		}
		return d
	}
	var restore []string
	for _, name := range names {
		active := slices.ContainsFunc(existing, func(d string) bool { return short(d) == short(name) })
		switch {
		case active:
			a.log.Info(fmt.Sprintf("Domain '%s' is already active, skipping.", name))
		case slices.Contains(restore, name):
			a.log.Info(fmt.Sprintf("Domain '%s' is duplicated in input, skipping.", name))
		default:
			restore = append(restore, name)
		}
	}
	if len(restore) == 0 {
		a.log.Info("No domains to restore.")
		return nil
	}

	active, err := r.s.ActivePHPVersions()
	if err != nil {
		return err
	}
	if err := resources.New(r.dir, a.log).EnsurePHPVersionConfig(php); err != nil {
		return err
	}
	certs, err := a.ssl(r.e.CertsDir(r.dir))
	if err != nil {
		return err
	}
	c := &cleanup{a: a, h: a.deps.Hosts(a.log)}
	var restored []string
	err = func() error {
		a.log.Info(fmt.Sprintf("Restoring %d domain(s) with PHP %s...", len(restore), php))
		a.log.Info("Restoring Caddyfiles...")
		var err error
		if restored, err = r.generator(a.log).RestoreToVersion(restore, php); err != nil {
			return err
		}
		if len(restored) == 0 {
			return nil
		}
		a.log.Info("Generating SSL certificates...")
		if err := certs.GenerateAll(ctx, restored, forceSSL, r.e.IsProduction()); err != nil {
			return err
		}
		a.log.Info("Adding hosts entries...")
		for _, domain := range restored {
			if err := c.add(ctx, domain); err != nil {
				return err
			}
		}
		if err := r.s.AddDomains(restored, php); err != nil {
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
		if err := r.writeMain(); err != nil {
			return err
		}
		a.restartProxy(ctx, r.d)
		return nil
	}()
	if err != nil {
		// The domains leave the database and their site files go back
		// to the archive, ready for another restore-host
		return c.fail(ctx, err, "The host(s) were not restored.", func() {
			_ = r.s.RemoveDomains(restored)
			_ = r.generator(quietLogger{}).Archive(restored)
		})
	}
	if len(restored) == 0 {
		a.log.Info("No Caddyfiles were restored.")
		return nil
	}

	a.con.Println()
	a.log.Success("Host(s) restored successfully!")
	a.log.Info("Restored domains:")
	for _, domain := range restored {
		a.log.Info(fmt.Sprintf("  - https://%s (PHP %s)", domain, php))
	}
	return nil
}

// --- switch-php --------------------------------------------------------------

func newSwitchPHPCmd(a *app) *cobra.Command {
	var php string
	cmd := &cobra.Command{
		Use:   "switch-php DOMAIN --php VERSION",
		Short: "Switch the PHP version for a domain without restarting unrelated containers.",
		Long: `Switch the PHP version for a domain without restarting unrelated containers.

Examples:
    frankenmanager switch-php "myapp.test" --php 8.5
    frankenmanager switch-php "legacy.test" --php 8.2`,
		Args: rangeArgs(1, "DOMAIN"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("php") {
				return usageError{errors.New("Missing option '--php'.")} //nolint:staticcheck // user-facing message
			}
			return a.switchPHP(cmd.Context(), args[0], php)
		},
	}
	cmd.Flags().StringVar(&php, "php", "", phpVersionHelp)
	return cmd
}

// switchPHP moves a domain to another PHP version.
func (a *app) switchPHP(ctx context.Context, domain, php string) (err error) {
	if err := phpversions.Validate(php); err != nil {
		return err
	}
	if w := phpversions.DeprecationWarning(php); w != "" {
		a.log.Warning(w)
	}
	r, err := a.openRunning(ctx, "The server is not running.")
	if err != nil {
		return err
	}
	defer closeStore(r.s, &err)

	current, found, err := r.s.DomainPHPVersion(domain)
	if err != nil {
		return err
	}
	if !found {
		// An unknown domain is an error: exit code 1
		return fmt.Errorf("Domain '%s' is not configured.", domain) //nolint:staticcheck // user-facing message
	}
	if current == php {
		a.log.Info(fmt.Sprintf("Domain '%s' is already using PHP %s.", domain, php))
		return nil
	}
	a.log.Info(fmt.Sprintf("Switching '%s' from PHP %s to PHP %s...", domain, current, php))
	if err := resources.New(r.dir, a.log).EnsurePHPVersionConfig(php); err != nil {
		return err
	}
	g := r.generator(a.log)
	before, err := r.s.ActivePHPVersions()
	if err != nil {
		return err
	}
	if err := g.MoveToVersion(domain, current, php); err != nil {
		return err
	}
	if _, err := r.s.UpdateDomainPHPVersion(domain, php); err != nil {
		return err
	}
	after, err := r.s.ActivePHPVersions()
	if err != nil {
		return err
	}

	err = func() error {
		if !slices.Contains(before, php) {
			if err := a.startVersion(ctx, r, php, after); err != nil {
				return err
			}
		} else {
			a.restartVersion(ctx, r.d, php)
		}
		// The old version lost a site
		if slices.Contains(after, current) {
			a.restartVersion(ctx, r.d, current)
		}
		if err := a.stopVersions(ctx, r, without(before, after), after); err != nil {
			return err
		}
		if err := r.writeMain(); err != nil {
			return err
		}
		a.restartProxy(ctx, r.d)
		return nil
	}()
	if err != nil {
		a.log.Error(fmt.Sprintf("An error occurred: %v", err))
		_ = g.MoveToVersion(domain, php, current)
		_, _ = r.s.UpdateDomainPHPVersion(domain, current)
		a.log.Error("Rolled back version change.")
		return exitError(1)
	}

	a.con.Println()
	a.log.Success(fmt.Sprintf("Switched '%s' to PHP %s!", domain, php))
	return nil
}
