package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
)

func newStopCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the FrankenPHP development server.",
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
	if running, err := a.isRunning(ctx, d, s); err != nil {
		return err
	} else if !running {
		return errors.New("The server is already stopped.") //nolint:staticcheck // user-facing message
	}

	a.log.Info("Stopping web server...")
	if w, err := a.deps.Watcher(dir); err == nil {
		w.Stop(ctx)
	}
	d.ComposeDown(ctx)

	names, err := s.DomainNames()
	if err != nil {
		return err
	}
	aliases, err := s.Aliases()
	if err != nil {
		return err
	}
	for _, al := range aliases {
		names = append(names, al.Alias)
	}
	proxies, err := s.Proxies()
	if err != nil {
		return err
	}
	for _, p := range proxies {
		names = append(names, p.Domain)
	}
	h := a.deps.Hosts(a.log)
	for _, name := range names {
		_, _ = h.RemoveEntry(ctx, "127.0.0.1", name) // errors ignored
	}
	// The Soketi host, also when SOKETI_ENABLED was turned off while running
	if found, err := h.HasEntry("127.0.0.1", soketi.Domain); err == nil && found {
		_, _ = h.RemoveEntry(ctx, "127.0.0.1", soketi.Domain)
	}

	a.con.Println()
	a.log.Success("Web server stopped!")
	return nil
}
