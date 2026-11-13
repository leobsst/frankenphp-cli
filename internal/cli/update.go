package cli

import (
	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/platform"
)

func newUpdateCmd(a *app) *cobra.Command {
	var force, check bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update FrankenManager to the latest version.",
		Long: `Update FrankenManager to the latest version.

This command downloads and installs the latest release from GitHub.
Since the binary lives in a system directory (e.g. /usr/local/lib),
installing it usually requires admin privileges: run with sudo.

Examples:
    frankenmanager update --check  # Check for updates without installing
    sudo frankenmanager update     # Update to latest version
    sudo frankenmanager update --force  # Force reinstall latest version`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := platform.DataDir()
			if err != nil {
				return err
			}
			u := a.deps.Updater(dir, a.log)
			if !check {
				if !u.Update(cmd.Context(), force) {
					return exitError(1)
				}
				// Earlier releases had no alias
				a.ensureAlias()
				return nil
			}
			a.log.Info("Current version: " + u.Version)
			latest, err := u.Check(cmd.Context())
			if err != nil {
				return err
			}
			if latest == nil {
				a.log.Success("You are on the latest version!")
				return nil
			}
			a.log.Info("New version available: " + latest.Version)
			a.log.Info("Release: " + latest.Name)
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&force, "force", "f", false, "Force update even if on latest version")
	f.BoolVarP(&check, "check", "c", false, "Only check for updates, don't install")
	return cmd
}
