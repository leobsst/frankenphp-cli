package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

func newStopCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: i18n.T("Stop the FrankenPHP development server."),
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return a.stop(cmd.Context()) },
	}
}

// stop stops the stack, the DNS watcher, and removes the hosts entries.
func (a *app) stop(ctx context.Context) (err error) {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	d := a.docker(dir)
	s, err := a.openStore(dir)
	if err != nil {
		return err
	}
	defer closeStore(s, &err)
	if err := a.stackService(dir, nil, d, s).Stop(ctx); err != nil {
		return err
	}
	a.con.Println()
	a.log.Success(i18n.T("Web server stopped!"))
	return nil
}
