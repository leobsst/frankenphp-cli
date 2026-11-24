package cli

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/dashboard"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

func newDashboardCmd(a *app) *cobra.Command {
	var printOnly bool
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: i18n.T("Open the dashboard in the browser."),
		Long: i18n.T(`Open the dashboard in the browser.

The dashboard answers at https://frankenmanager.test, and at
https://<CLI_ALIAS>.test (https://fkm.test by default), on this machine
only: these names are not given to the LAN DNS server, and the page signs in
by calling the daemon on 127.0.0.1, which no other device reaches. Typed in
the browser, they open the dashboard without any password.

Before the stack runs, the dashboard answers at
https://127.0.0.1:DASHBOARD_HTTPS_PORT. From another device, use the remote
access (DASHBOARD_ACCESS), with the password.

Examples:
    frankenmanager dashboard           # Open it
    frankenmanager dashboard --print   # Print its address only`),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return a.openDashboard(cmd.Context(), printOnly) },
	}
	cmd.Flags().BoolVar(&printOnly, "print", false, i18n.T("Print the address without opening the browser"))
	return cmd
}

func (a *app) openDashboard(ctx context.Context, printOnly bool) error {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	// The daemon serves the dashboard: started if it does not run
	if _, _, err := a.ensureDaemon(ctx, dir); err != nil {
		return err
	}
	url := a.dashboardURL(e, dir)
	a.log.Success(i18n.Tf("Dashboard: %s", url))
	if printOnly {
		return nil
	}
	cmd := platform.OpenURLCommand(url)
	platform.AsRealUser(cmd)
	if err := cmd.Start(); err != nil {
		a.log.Warning(i18n.Tf("Could not open the browser (%v): open the address above.", err))
		return nil
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// dashboardURL is the first dashboard name the reverse proxy serves (the
// stack runs, started by a release that made its certificate, and no
// registered host takes it), the HTTPS listener of the daemon otherwise.
func (a *app) dashboardURL(e *env.Env, dir string) string {
	names := dashboard.Domains(e)
	if st, err := a.openStore(dir); err == nil {
		if available, err := dashboard.Available(e, st); err == nil {
			names = available
		}
		_ = st.Close()
	}
	webPort := e.Get("WEB_HTTPS_PORT")
	up := false
	if conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", webPort), 300*time.Millisecond); err == nil {
		_ = conn.Close()
		up = true
	}
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(e.CertsDir(dir), name+".pem")); err != nil || !up {
			continue
		}
		if webPort == "" || webPort == "443" {
			return "https://" + name + "/"
		}
		return "https://" + net.JoinHostPort(name, webPort) + "/"
	}
	return "https://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(dashboard.HTTPSPort(e))) + "/"
}
