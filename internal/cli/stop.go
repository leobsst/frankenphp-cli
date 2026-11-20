package cli

import (
	"context"
	"net/http"

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

// stop stops the stack and the LAN DNS server, and removes the hosts
// entries. The daemon goes on.
func (a *app) stop(ctx context.Context) error {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	if err := a.runOp(ctx, dir, http.MethodPost, "/api/v1/stack/stop", nil, nil); err != nil {
		return reported(err)
	}
	a.con.Println()
	a.log.Success(i18n.T("Web server stopped!"))
	return nil
}
