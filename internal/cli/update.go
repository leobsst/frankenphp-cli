package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/client"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/updater"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

func newUpdateCmd(a *app) *cobra.Command {
	var force, check bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: i18n.T("Update FrankenManager to the latest version."),
		Long: i18n.T(`Update FrankenManager to the latest version.

This command downloads and installs the latest release from GitHub.
A running server is stopped before the install and started again on the
new version right after; a stopped one stays stopped. A running dashboard
daemon is restarted on the new version.
Since the binary lives in a system directory (e.g. /usr/local/lib),
installing it usually requires admin privileges: run with sudo.

Examples:
    frankenmanager update --check  # Check for updates without installing
    sudo frankenmanager update     # Update to latest version
    sudo frankenmanager update --force  # Force reinstall latest version`),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := platform.DataDir()
			if err != nil {
				return err
			}
			u := a.deps.Updater(dir, a.log)
			if !check {
				// A running dashboard daemon is restarted on the new version
				u.DaemonRunning = client.New(dir).Running
				a.grantBindPortOnUpdate(cmd.Context(), u, dir)
				if !u.Update(cmd.Context(), force) {
					return exitError(1)
				}
				// Earlier releases had no alias
				a.ensureAlias()
				return nil
			}
			a.log.Info(i18n.Tf("Current version: %s", u.Version))
			latest, err := u.Check(cmd.Context())
			if err != nil {
				return err
			}
			if latest == nil {
				a.log.Success(i18n.T("You are on the latest version!"))
				return nil
			}
			a.log.Info(i18n.Tf("New version available: %s", latest.Version))
			a.log.Info(i18n.Tf("Release: %s", latest.Name))
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&force, "force", "f", false, i18n.T("Force update even if on latest version"))
	f.BoolVarP(&check, "check", "c", false, i18n.T("Only check for updates, don't install"))
	return cmd
}

// grantBindPortOnUpdate has the update give the new binary the port 53
// permission (Linux) when the current one has it or the LAN DNS server is
// on: replacing the file drops it.
func (a *app) grantBindPortOnUpdate(ctx context.Context, u *updater.Updater, dir string) {
	p := a.deps.Privilege(a.log)
	if !p.BindPortNeeded() {
		return
	}
	needed := u.Exe != "" && p.CanBindPort(ctx, u.Exe)
	if !needed {
		if e, err := a.loadEnv(dir); err == nil {
			needed = dns.Enabled(e)
		}
	}
	if needed {
		u.GrantBindPort = p.GrantBindPort
	}
}
