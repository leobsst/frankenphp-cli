package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/caserver"
	"github.com/leobsst/frankenphp-cli/internal/core/dnswatcher"
	"github.com/leobsst/frankenphp-cli/internal/core/resources"
	"github.com/leobsst/frankenphp-cli/internal/core/updater"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

// The hidden commands the CLI runs itself, in detached processes or from
// `update`. Their names, arguments and exit codes must not change: the
// services and processes started by earlier releases, and the `update`
// command of the previous updater, run them on the new binary.

// untilTerminated returns a context ended by SIGTERM (launchd, systemd,
// `trust-ca off`) or Ctrl-C.
func untilTerminated(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}

// hiddenError writes err to stderr, the log file of a detached process,
// and ends with exit code 1.
func (a *app) hiddenError(err error) error {
	fmt.Fprintln(a.errOut, err)
	return exitError(1)
}

func newInternalCmds(a *app) []*cobra.Command {
	return []*cobra.Command{
		{
			Use:    caserver.Command + " CERT_FILE PORT",
			Short:  i18n.T("Internal: blocking single-file HTTP server, spawned by `trust-ca on`."),
			Hidden: true,
			Args:   rangeArgs(2, "CERT_FILE", "PORT"),
			RunE: func(cmd *cobra.Command, args []string) error {
				port, err := strconv.Atoi(args[1])
				if err != nil {
					return usageError{i18n.Errorf("Invalid value for 'PORT': '%s' is not a valid integer.", args[1])}
				}
				ctx, stop := untilTerminated(cmd.Context())
				defer stop()
				if err := a.deps.ServeCA(ctx, args[0], port); err != nil {
					return a.hiddenError(err)
				}
				return nil
			},
		},
		{
			Use:    dnswatcher.Command,
			Short:  i18n.T("Internal: keep the LAN DNS records on the current LAN IP, spawned by `start`."),
			Hidden: true,
			Args:   noArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				dir, err := a.projectDir()
				if err != nil {
					return a.hiddenError(err)
				}
				ctx, stop := untilTerminated(cmd.Context())
				defer stop()
				if err := a.deps.WatchDNS(ctx, dir, a.errOut); err != nil {
					return a.hiddenError(err)
				}
				return nil
			},
		},
		{
			Use:    updater.SyncResourcesCommand,
			Short:  i18n.T("Internal: resync the static resources, run by `update` with the new binary."),
			Hidden: true,
			Args:   noArgs,
			RunE: func(*cobra.Command, []string) error {
				dir, err := platform.DataDir()
				if err != nil {
					return err
				}
				synced, err := resources.New(dir, a.log).SyncInstalled()
				if err != nil {
					return err
				}
				if !synced {
					// Never initialized: its first command extracts everything
					return exitError(updater.SyncResourcesNothingToDo)
				}
				return nil
			},
		},
		newInstallAliasCmd(a),
		{
			Use:    updater.ServerRunningCommand,
			Short:  i18n.T("Internal: exit 0 when the server runs, 1 otherwise, run by `update` as the real user."),
			Hidden: true,
			Args:   noArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				dir, err := a.projectDir()
				if err != nil {
					return exitError(1)
				}
				if running, err := a.serverRunning(cmd.Context(), dir); err != nil || !running {
					return exitError(1)
				}
				return nil
			},
		},
		{
			Use:    updater.RefreshCommand,
			Short:  i18n.T("Internal: refresh the cached GitHub release info, spawned at startup."),
			Hidden: true,
			Args:   noArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				dir, err := platform.DataDir()
				if err != nil {
					return exitError(1)
				}
				if err := a.deps.Refresh(cmd.Context(), dir, a.log); err != nil {
					return exitError(1) // silent: retried later
				}
				return nil
			},
		},
	}
}

// refresh refreshes the update cache from GitHub.
func refresh(ctx context.Context, dataDir string, log updater.Logger) error {
	_, err := updater.New(dataDir, log).Refresh(ctx)
	return err
}

// newInstallAliasCmd installs the configured CLI alias, for the install
// scripts: --bin-dir is the directory of the frankenmanager command, which
// PATH may not reach under sudo.
func newInstallAliasCmd(a *app) *cobra.Command {
	var binDir string
	cmd := &cobra.Command{
		Use:    "_install-alias-internal",
		Short:  i18n.T("Internal: install the CLI alias next to the command, run by the install scripts."),
		Hidden: true,
		Args:   noArgs,
		RunE: func(*cobra.Command, []string) error {
			a.ensureAliasIn(binDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&binDir, "bin-dir", "", i18n.T("Directory of the frankenmanager command"))
	return cmd
}
