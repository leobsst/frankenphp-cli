package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/caddyfile"
	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

// --- remove-host -------------------------------------------------------------

func newRemoveHostCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "remove-host DOMAINS",
		Short: i18n.T("Remove host(s) from the running server."),
		Long: i18n.T(`Remove host(s) from the running server.

This command allows you to remove domains while the server is running,
without having to stop and restart the entire environment.

The Caddyfile configurations will be archived (not deleted) in case
you need to restore them later.

Examples:
    frankenmanager remove-host "oldapp.test"
    frankenmanager remove-host "app1.test app2.test"`),
		Args: rangeArgs(1, "DOMAINS"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.removeHost(cmd.Context(), strings.Fields(args[0]))
		},
	}
}

// removeHost removes domains, aliases, proxy and static hosts, archiving
// the site files of the domains and static hosts.
func (a *app) removeHost(ctx context.Context, names []string) (err error) {
	r, err := a.openRunning(ctx, i18n.T("The server is not running."))
	if err != nil {
		return err
	}
	defer closeStore(r.s, &err)

	res, err := a.domainService(r).Remove(ctx, names)
	if err != nil || res.Empty() {
		return reported(err)
	}
	a.con.Println()
	a.log.Success(i18n.T("Host(s) removed successfully!"))
	if len(res.Domains) > 0 {
		a.log.Info(i18n.T("Removed domains:"))
		for _, d := range res.Domains {
			a.log.Info(fmt.Sprintf("  - %s (PHP %s)", d.Name, d.PHPVersion))
		}
		a.log.Info(i18n.Tf("\nCaddyfiles archived to: %s", res.ArchiveDir))
	}
	if len(res.Aliases) > 0 {
		a.log.Info(i18n.T("Removed alternate hosts:"))
		for _, al := range res.Aliases {
			a.log.Info("  - " + al)
		}
	}
	if len(res.Proxies) > 0 {
		a.log.Info(i18n.T("Removed proxy hosts:"))
		for _, p := range res.Proxies {
			a.log.Info("  - " + p)
		}
	}
	if len(res.Statics) > 0 {
		a.log.Info(i18n.T("Removed static hosts:"))
		for _, st := range res.Statics {
			a.log.Info("  - " + st)
		}
		a.log.Info(i18n.Tf("\nCaddyfiles archived to: %s", res.StaticArchiveDir))
	}
	return nil
}

// --- restore-host ------------------------------------------------------------

func newRestoreHostCmd(a *app) *cobra.Command {
	var list, forceSSL bool
	var php string
	cmd := &cobra.Command{
		Use:   "restore-host [DOMAINS]",
		Short: i18n.T("Restore archived host(s) to the running server."),
		Long: i18n.T(`Restore archived host(s) to the running server.

This command allows you to restore previously removed domains from the archive.
Use --list to see all available archived hosts.

Examples:
    frankenmanager restore-host --list
    frankenmanager restore-host "myapp.test"
    frankenmanager restore-host "app1.test" --php 8.5`),
		Args: maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if list {
				return a.listArchived()
			}
			if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
				a.con.Println(a.con.Color(ui.Yellow, i18n.T("Specify domains to restore or use --list to see archived hosts.")))
				return exitError(1)
			}
			return a.restoreHost(cmd.Context(), strings.Fields(args[0]), forceSSL, php)
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&list, "list", "l", false, i18n.T("List all archived hosts"))
	f.BoolVar(&forceSSL, "force-ssl", false, i18n.T("Force SSL certificate regeneration"))
	f.StringVar(&php, "php", "", phpVersionUsage())
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
		a.log.Info(i18n.T("No archived hosts found."))
		a.log.Info(i18n.Tf("Archive directory: %s", g.ArchiveDir()))
		return nil
	}
	t := ui.Table{Title: i18n.T("Archived Hosts"), Columns: []ui.Column{
		{Header: i18n.T("Domain"), Color: ui.Cyan}, {Header: "Caddyfile", Faint: true},
	}}
	for _, item := range archived {
		t.Rows = append(t.Rows, []string{item.FullDomain, item.SimpleDomain + "_Caddyfile"})
	}
	a.con.PrintTable(t)
	a.log.Info("\n" + i18n.Tf("Archive directory: %s", g.ArchiveDir()))
	a.log.Info(i18n.T(`Use 'frankenmanager restore-host "domain.test"' to restore a host.`))
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
	if php, err = stack.ResolvePHP(php, e, a.events()); err != nil {
		return err
	}
	r, err := a.openRunning(ctx, i18n.T("The server is not running. Use 'start' command instead."))
	if err != nil {
		return err
	}
	defer closeStore(r.s, &err)

	res, err := a.domainService(r).Restore(ctx, names, php, forceSSL)
	if err != nil || len(res.Domains) == 0 {
		return reported(err)
	}
	a.con.Println()
	a.log.Success(i18n.T("Host(s) restored successfully!"))
	a.log.Info(i18n.T("Restored domains:"))
	for _, domain := range res.Domains {
		a.log.Info(fmt.Sprintf("  - https://%s (PHP %s)", domain, res.PHP))
	}
	return nil
}

// --- switch-php --------------------------------------------------------------

func newSwitchPHPCmd(a *app) *cobra.Command {
	var php string
	cmd := &cobra.Command{
		Use:   "switch-php DOMAIN --php VERSION",
		Short: i18n.T("Switch the PHP version for a domain without restarting unrelated containers."),
		Long: i18n.T(`Switch the PHP version for a domain without restarting unrelated containers.

Examples:
    frankenmanager switch-php "myapp.test" --php 8.5
    frankenmanager switch-php "legacy.test" --php 8.2`),
		Args: rangeArgs(1, "DOMAIN"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("php") {
				return usageError{i18n.Errorf("Missing option '%s'.", "--php")}
			}
			return a.switchPHP(cmd.Context(), args[0], php)
		},
	}
	cmd.Flags().StringVar(&php, "php", "", phpVersionUsage())
	return cmd
}

// switchPHP moves a domain to another PHP version.
func (a *app) switchPHP(ctx context.Context, domain, php string) (err error) {
	// Checked before the server state
	if err := phpversions.Validate(php); err != nil {
		return err
	}
	if w := phpversions.DeprecationWarning(php); w != "" {
		a.log.Warning(w)
	}
	r, err := a.openRunning(ctx, i18n.T("The server is not running."))
	if err != nil {
		return err
	}
	defer closeStore(r.s, &err)

	res, err := a.domainService(r).SwitchPHP(ctx, domain, php)
	if err != nil || !res.Switched {
		return reported(err)
	}
	a.con.Println()
	a.log.Success(i18n.Tf("Switched '%s' to PHP %s!", domain, php))
	return nil
}
