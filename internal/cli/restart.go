package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

func newRestartCmd(a *app) *cobra.Command {
	var forceSSL, build bool
	selected := map[string]*bool{}
	for _, t := range stack.RestartTargets {
		selected[t] = new(bool)
	}
	cmd := &cobra.Command{
		Use:   "restart",
		Short: i18n.T("Restart the FrankenPHP development server or specific containers."),
		Long: i18n.T(`Restart the FrankenPHP development server or specific containers.

Examples:
    frankenmanager restart                    # Restart all containers
    frankenmanager restart --caddy            # Restart only web server/Caddy
    frankenmanager restart --database         # Restart only database
    frankenmanager restart --cache --pma      # Restart Redis and phpMyAdmin
    frankenmanager restart --dns              # Restart the LAN DNS server
    frankenmanager restart --soketi           # Restart the Soketi WebSocket server
    frankenmanager restart --s3               # Restart the S3 server
    frankenmanager restart --build            # Rebuild PHP images, then restart`),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var targets []string
			for _, t := range stack.RestartTargets {
				if *selected[t] {
					targets = append(targets, t)
				}
			}
			return a.restart(cmd.Context(), forceSSL, targets, build)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&forceSSL, "force-ssl", false, i18n.T("Force SSL certificate regeneration"))
	f.BoolVar(selected["caddy"], "caddy", false, i18n.T("Restart only the web server / Caddy container"))
	// Options with two long names: the second one is a hidden alias
	alias := func(name, short, help string) {
		f.BoolVar(selected[name], name, false, help)
		f.BoolVar(selected[name], short, false, help)
		_ = f.MarkHidden(short)
	}
	alias("database", "db", i18n.T("Restart only the database container (alias: --db)"))
	f.BoolVar(selected["cache"], "cache", false, i18n.T("Restart only the Redis cache container (REDIS_ENABLED=true)"))
	alias("phpmyadmin", "pma", i18n.T("Restart only the phpMyAdmin container (alias: --pma)"))
	f.BoolVar(selected["dns"], "dns", false, i18n.T("Restart only the LAN DNS server (DNS_SERVER=true)"))
	alias("soketi", "ws", i18n.T("Restart only the Soketi WebSocket server (SOKETI_ENABLED=true) (alias: --ws)"))
	f.BoolVar(selected["s3"], "s3", false, i18n.T("Restart only the S3 server (S3_ENABLED=true)"))
	f.BoolVar(&build, "build", false, i18n.T("Rebuild the PHP images and recreate their containers"))
	return cmd
}

// restart restarts the stack or some of its containers. targets is empty to
// restart everything.
func (a *app) restart(ctx context.Context, forceSSL bool, targets []string, build bool) (err error) {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	d := a.docker(dir)
	s, err := a.openStore(dir)
	if err != nil {
		return err
	}
	defer closeStore(s, &err)
	if err := a.stackService(dir, e, d, s).Restart(ctx, targets, forceSSL, build); err != nil {
		return err
	}
	a.con.Println()
	if len(targets) == 0 {
		a.log.Success(i18n.T("Web server restarted!"))
	} else {
		a.log.Success(i18n.Tf("Container(s) restarted: %s", strings.Join(targets, ", ")))
	}
	return nil
}
