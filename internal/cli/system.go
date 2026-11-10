package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/core/caserver"
	"github.com/leobsst/frankenphp-cli/internal/core/resources"
	"github.com/leobsst/frankenphp-cli/internal/core/ssl"
	"github.com/leobsst/frankenphp-cli/internal/platform"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

// yesNo is how the status tables show a boolean.
func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

// --- setup -------------------------------------------------------------------

func newSetupCmd(a *app) *cobra.Command {
	var remove, status, installMkcert bool
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure FrankenManager for passwordless operation.",
		Long: `Configure FrankenManager for passwordless operation.

This command sets up the system to allow FrankenManager to:
- Modify /etc/hosts without requiring a password each time
- Optionally install mkcert for SSL certificate generation

Run with sudo for initial setup:
    sudo frankenmanager setup

To also install mkcert:
    sudo frankenmanager setup --install-mkcert

To check current status:
    frankenmanager setup --status

To remove configuration:
    sudo frankenmanager setup --remove`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch {
			case status:
				return a.setupStatus(cmd.Context())
			case remove:
				return a.setupRemove()
			default:
				return a.setup(cmd.Context(), installMkcert)
			}
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&remove, "remove", "r", false, "Remove privilege configuration")
	f.BoolVarP(&status, "status", "s", false, "Show current configuration status")
	f.BoolVarP(&installMkcert, "install-mkcert", "m", false, "Install mkcert for SSL certificates")
	return cmd
}

// setupStatus shows the data directory, privilege and dependency status.
func (a *app) setupStatus(ctx context.Context) error {
	p := a.deps.Privilege(a.log)
	m := a.deps.Mkcert(a.log)
	status := p.Status(ctx)
	dir, err := platform.DataDir()
	if err != nil {
		return err
	}
	_, statErr := os.Stat(filepath.Join(dir, resources.InitializedMarker))

	settings := []ui.Column{{Header: "Setting", Color: ui.Cyan}, {Header: "Value", Color: ui.Green}}
	a.con.PrintTable(ui.Table{Title: "Data Directory", Columns: settings, Rows: [][]string{
		{"Location", dir},
		{"Mode", "Production"},
		{"Initialized", yesNo(statErr == nil)},
	}})
	a.con.Println()

	rows := [][]string{
		{"Platform", status.Platform},
		{"Running as Admin/Root", yesNo(status.IsAdmin)},
		{"Helper Script Installed", yesNo(status.HelperInstalled)},
	}
	if status.Platform != "windows" {
		rows = append(rows, []string{"Sudoers Configured", yesNo(status.SudoersConfigured)})
	}
	rows = append(rows, []string{"Passwordless Available", yesNo(status.PasswordlessAvailable)})
	a.con.PrintTable(ui.Table{Title: "Privilege Configuration Status", Columns: settings, Rows: rows})
	a.con.Println()

	mkcertStatus := "Not installed"
	if m.IsInstalled() {
		mkcertStatus = "Installed"
	}
	a.con.PrintTable(ui.Table{Title: "Dependencies Status", Columns: []ui.Column{
		{Header: "Dependency", Color: ui.Cyan}, {Header: "Status", Color: ui.Green},
	}, Rows: [][]string{{"mkcert", mkcertStatus}}})

	if !status.Configured || !m.IsInstalled() {
		a.log.Info("")
		a.log.Info("To complete setup, run:")
		a.log.Info("  sudo frankenmanager setup")
	}
	return nil
}

// setup installs the privilege configuration (replacing an outdated
// helper script) and mkcert's CA, installing mkcert when asked.
func (a *app) setup(ctx context.Context, installMkcert bool) error {
	p := a.deps.Privilege(a.log)
	m := a.deps.Mkcert(a.log)
	a.log.Info("Setting up FrankenManager...")
	a.log.Info("")

	install := !p.Configured()
	if !install && !p.UpToDate() {
		// A helper installed by an earlier release cannot remove entries on
		// Linux
		a.log.Info("Updating the hosts helper script...")
		install = true
	}
	if install {
		if err := p.Setup(ctx); err != nil {
			a.log.Error(err.Error())
			return exitError(1)
		}
	} else {
		a.log.Info("Privilege escalation is already configured.")
	}
	a.log.Info("")

	switch {
	case m.IsInstalled():
		a.log.Info("mkcert is already installed.")
		m.SetupCA(ctx)
	case installMkcert:
		a.log.Info("Installing mkcert...")
		if m.Install(ctx) {
			m.SetupCA(ctx)
		} else {
			a.log.Warning("mkcert installation failed. You can install it manually:")
			a.log.Info(m.Instructions())
		}
	default:
		a.log.Info("mkcert is not installed.")
		a.log.Info("To install mkcert automatically, run:")
		a.log.Info("  sudo frankenmanager setup --install-mkcert")
		a.log.Info("")
		a.log.Info("Or install manually:")
		a.log.Info(m.Instructions())
	}
	a.log.Info("")
	a.log.Success("Setup complete!")
	return nil
}

// setupRemove removes the privilege configuration.
func (a *app) setupRemove() error {
	p := a.deps.Privilege(a.log)
	a.log.Info("Removing privilege escalation configuration...")
	a.log.Info("")
	if !p.Configured() {
		a.log.Info("Privilege escalation is not configured. Nothing to remove.")
		return nil
	}
	if err := p.Remove(); err != nil {
		a.log.Error(err.Error())
		return exitError(1)
	}
	return nil
}

// --- trust-ca ----------------------------------------------------------------

func newTrustCACmd(a *app) *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "trust-ca ACTION",
		Short: "Serve the local mkcert root CA over the LAN so other devices can trust it.",
		Long: `Serve the local mkcert root CA over the LAN so other devices can trust it.

Off by default. Turn it on temporarily when you need another device on
your network to trust your local .test domains, then turn it off again.
Only the public root CA is served - never its private key.

ACTION: on, off, or status.

Examples:
    frankenmanager trust-ca on              # Start sharing on the default port
    frankenmanager trust-ca on --port 9443   # Start sharing on a custom port
    frankenmanager trust-ca status           # Check whether sharing is active
    frankenmanager trust-ca off              # Stop sharing`,
		Args: rangeArgs(1, "ACTION"),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "on":
				return a.trustCAOn(cmd.Context(), port)
			case "off":
				return a.trustCAOff()
			case "status":
				return a.trustCAStatus()
			}
			a.con.Println(a.con.Color(ui.Red, fmt.Sprintf("Unknown action '%s'. Use on, off, or status.", args[0])))
			return exitError(1)
		},
	}
	cmd.Flags().IntVarP(&port, "port", "p", caserver.DefaultPort, "Port to serve the root CA on (default 9080)")
	return cmd
}

// rootCAPath is mkcert's own rootCA.pem, the CA trusted on this machine,
// not a copy in caddy/certs.
func (a *app) rootCAPath(ctx context.Context) (string, error) {
	mkcert := a.deps.Command.LookPath("mkcert")
	if mkcert == "" {
		return "", &ssl.Error{Msg: "mkcert not found. Run `sudo frankenmanager setup --install-mkcert` first."}
	}
	res, err := a.deps.Command.Run(ctx, command.Cmd{Name: mkcert, Args: []string{"-CAROOT"}})
	if err != nil {
		return "", &ssl.Error{Msg: fmt.Sprintf("Could not determine mkcert's CA root: %v", err)}
	}
	if !res.OK() {
		return "", &ssl.Error{Msg: "Could not determine mkcert's CA root: " + strings.TrimSpace(string(res.Stderr))}
	}
	return filepath.Join(strings.TrimSpace(string(res.Stdout)), "rootCA.pem"), nil
}

// qrASCII renders data as a QR code in cp437 characters (no-break space,
// upper and lower half, full block), with a 1-module border.
func qrASCII(data string) (string, error) {
	q, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		return "", err
	}
	q.DisableBorder = true
	bitmap := q.Bitmap()
	n := len(bitmap)
	const border = 1
	dark := func(r, c int) int {
		if r < 0 || c < 0 || r >= n || c >= n || !bitmap[r][c] {
			return 0
		}
		return 1
	}
	codes := []string{"\u00a0", "▀", "▄", "█"}
	var b strings.Builder
	for r := -border; r < n+border; r += 2 {
		for c := -border; c < n+border; c++ {
			b.WriteString(codes[dark(r, c)+dark(r+1, c)<<1])
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func (a *app) trustCAOn(ctx context.Context, port int) error {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	cert, err := a.rootCAPath(ctx)
	if err != nil {
		return err
	}
	if err := a.deps.ShareCA(dir, cert, port); err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s:%d/", a.deps.LANIP(), port)
	a.log.Success("Root CA is now shared at " + url)
	qr, err := qrASCII(url)
	if err != nil {
		return err
	}
	a.con.Println(qr)
	a.log.Info("Scan the QR code (or open that URL in a browser) on the other device " +
		"to download rootCA.pem.")
	a.log.Info(`iOS: install the profile even though it shows "Not Verified" (normal for any ` +
		"manually-installed CA) - then you MUST also go to Settings > General > About > " +
		`Certificate Trust Settings and enable full trust for "FrankenManager Local ` +
		`Development CA". Without that last toggle, HTTPS warnings keep appearing.`)
	a.log.Info("Android: trust is granted automatically as soon as the certificate is installed.")
	a.log.Warning("Anyone on your network can reach this port while sharing is on. " +
		"Run `frankenmanager trust-ca off` once other devices have installed it.")
	return nil
}

func (a *app) trustCAOff() error {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	if !a.deps.StopCA(dir) {
		return errors.New("Root CA sharing is not currently running.") //nolint:staticcheck // user-facing message
	}
	a.log.Success("Root CA sharing stopped.")
	return nil
}

func (a *app) trustCAStatus() error {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	_, port, ok := a.deps.CAState(dir)
	if !ok {
		a.log.Info("Root CA sharing is currently OFF.")
		return nil
	}
	a.log.Info(fmt.Sprintf("Root CA sharing is ON at http://%s:%d/", a.deps.LANIP(), port))
	return nil
}

// --- reset -------------------------------------------------------------------

func newResetCmd(a *app) *cobra.Command {
	var db, caddyfiles bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset FrankenManager configuration and/or custom Caddyfiles.",
		Long: `Reset FrankenManager configuration and/or custom Caddyfiles.

This command allows you to reset specific components of FrankenManager.
The server must be stopped before running this command.

NOTE: This does NOT affect your MariaDB database data.

Examples:
    frankenmanager reset --db                 # Reset domain list configuration
    frankenmanager reset --caddyfiles         # Reset only custom Caddyfiles
    frankenmanager reset --db --caddyfiles    # Reset both`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return a.reset(cmd.Context(), db, caddyfiles) },
	}
	f := cmd.Flags()
	f.BoolVar(&db, "db", false, "Reset FrankenManager configuration (domain list) (alias: --database)")
	f.BoolVar(&db, "database", false, "Reset FrankenManager configuration (domain list)")
	_ = f.MarkHidden("database")
	f.BoolVar(&caddyfiles, "caddyfiles", false, "Reset custom Caddyfiles (alias: --caddy)")
	f.BoolVar(&caddyfiles, "caddy", false, "Reset custom Caddyfiles")
	_ = f.MarkHidden("caddy")
	return cmd
}

// errAborted reports that the input ended at a prompt.
var errAborted = errors.New("aborted")

// confirm asks a yes/no question, no by default.
func (a *app) confirm(question string) (bool, error) {
	in := bufio.NewReader(a.deps.Stdin)
	for {
		fmt.Fprintf(a.out, "%s [y/N]: ", question)
		line, err := in.ReadString('\n')
		if err != nil && (line == "" || !errors.Is(err, io.EOF)) {
			return false, errAborted
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true, nil
		case "", "n", "no":
			return false, nil
		}
		fmt.Fprintln(a.out, "Error: invalid input")
	}
}

// reset clears the domain list and/or the site files, once confirmed.
// Neither the database servers' data nor the settings table are touched.
func (a *app) reset(ctx context.Context, db, caddyfiles bool) (err error) {
	if !db && !caddyfiles {
		a.log.Error("You must specify at least one option: --db or --caddyfiles")
		return exitError(1)
	}
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
	if running, err := a.isRunning(ctx, d, s); err != nil {
		return err
	} else if running {
		return errors.New("The server is currently running. " + //nolint:staticcheck // user-facing message
			"Please stop the server first with 'frankenmanager stop'.")
	}

	c := a.con
	sites := filepath.Join(e.ResolveDir("CADDY_DIR", "./caddy", dir), "sites")
	c.Println("\n", c.Bold(c.Color(ui.Yellow, "⚠️  Warning: You are about to reset:")))
	if db {
		c.Println("  • FrankenManager configuration (domain list)")
	}
	if caddyfiles {
		c.Println("  • custom Caddyfiles")
	}
	if db {
		c.Println("\n", c.Dim("Configuration reset will clear all registered domains from db.sqlite"))
		c.Println(c.Dim("Note: This does NOT affect your MariaDB database data"))
	}
	if caddyfiles {
		c.Println("\n", c.Dim("Caddyfiles reset will delete all files in: "+filepath.Join(sites, "custom")))
	}
	c.Println("\n", c.Bold(c.Color(ui.Red, "This action cannot be undone!")), "\n")

	ok, err := a.confirm("Do you want to proceed?")
	if errors.Is(err, errAborted) {
		// A newline, then Aborted!, both on stderr
		fmt.Fprintln(a.errOut)
		fmt.Fprintln(a.errOut, "Aborted!")
		return exitError(1)
	}
	if !ok {
		a.log.Info("Reset cancelled.")
		return nil
	}

	err = func() error {
		if db {
			a.log.Info("Resetting FrankenManager configuration...")
			if err := s.Reset(); err != nil {
				return err
			}
			a.log.Success("Configuration reset successfully!")
		}
		if caddyfiles {
			a.log.Info("Resetting custom Caddyfiles...")
			deleted := 0
			// The legacy custom/ directory and every php-<v>/ one
			for _, pattern := range []string{"custom", "php-*"} {
				files, err := filepath.Glob(filepath.Join(sites, pattern, "*_Caddyfile"))
				if err != nil {
					return err
				}
				for _, f := range files {
					if err := os.Remove(f); err != nil {
						return err
					}
					deleted++
				}
			}
			if deleted > 0 {
				a.log.Success(fmt.Sprintf("Deleted %d Caddyfile(s)", deleted))
			} else {
				a.log.Info("No custom Caddyfiles found to delete")
			}
		}
		return nil
	}()
	if err != nil {
		a.log.Error(fmt.Sprintf("An error occurred during reset: %v", err))
		return exitError(1)
	}
	c.Println()
	a.log.Success("Reset completed successfully!")
	return nil
}
