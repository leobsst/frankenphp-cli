package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/core/caserver"
	"github.com/leobsst/frankenphp-cli/internal/core/dnswatcher"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/hosts"
	"github.com/leobsst/frankenphp-cli/internal/core/mkcert"
	"github.com/leobsst/frankenphp-cli/internal/core/privilege"
	"github.com/leobsst/frankenphp-cli/internal/core/resources"
	"github.com/leobsst/frankenphp-cli/internal/core/ssl"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/core/updater"
	"github.com/leobsst/frankenphp-cli/internal/platform"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

// Deps are the outside world of the commands, replaced in tests. A nil
// field takes the real implementation.
type Deps struct {
	// Docker runs the docker command.
	Docker docker.Runner
	// Notify announces a newer release before a command.
	Notify func(dataDir string, log updater.Logger)
	// Command runs the other programs: mkcert, sudo, launchctl, systemctl.
	Command command.Runner
	// Hosts returns the hosts file manager.
	Hosts func(log hosts.Logger) *hosts.Manager
	// Watcher returns the DNS watcher of a data directory.
	Watcher func(dataDir string) (*dnswatcher.Watcher, error)
	// Interactive runs a program on the terminal (exec) and returns its
	// exit code.
	Interactive func(ctx context.Context, name string, args []string) (int, error)
	// TTY reports whether stdin and stdout are terminals.
	TTY func() bool
	// Privilege and Mkcert return the hosts privilege and mkcert managers.
	Privilege func(log privilege.Logger) *privilege.Manager
	Mkcert    func(log mkcert.Logger) *mkcert.Installer
	// ShareCA starts sharing the root CA (detached server), StopCA stops
	// it, CAState reports it.
	ShareCA func(dataDir, certFile string, port int) error
	StopCA  func(dataDir string) bool
	CAState func(dataDir string) (pid, port int, ok bool)
	// LANIP is the address shown to the other devices.
	LANIP func() string
	// Updater checks for and installs releases.
	Updater func(dataDir string, log updater.Logger) *updater.Updater
	// Stdin answers the prompts.
	Stdin io.Reader
	// ServeCA, WatchDNS and Refresh are the work of the hidden commands:
	// serve the root CA, follow the LAN IP, ask GitHub for a release.
	ServeCA  func(ctx context.Context, certFile string, port int) error
	WatchDNS func(ctx context.Context, dataDir string, stderr io.Writer) error
	Refresh  func(ctx context.Context, dataDir string, log updater.Logger) error
}

func (d Deps) withDefaults() Deps {
	if d.Docker == nil {
		d.Docker = docker.ExecRunner{}
	}
	if d.Command == nil {
		d.Command = command.Exec{}
	}
	if d.Hosts == nil {
		d.Hosts = hosts.New
	}
	if d.Interactive == nil {
		d.Interactive = runInteractive
	}
	if d.TTY == nil {
		d.TTY = stdioIsTTY
	}
	if d.Privilege == nil {
		d.Privilege = privilege.New
	}
	if d.Mkcert == nil {
		d.Mkcert = mkcert.New
	}
	if d.ShareCA == nil {
		d.ShareCA = shareCA
	}
	if d.StopCA == nil {
		d.StopCA = caserver.StopSharing
	}
	if d.CAState == nil {
		d.CAState = caserver.RunningState
	}
	if d.LANIP == nil {
		d.LANIP = platform.LANIP
	}
	if d.ServeCA == nil {
		d.ServeCA = caserver.Serve
	}
	if d.WatchDNS == nil {
		d.WatchDNS = dnswatcher.Run
	}
	if d.Refresh == nil {
		d.Refresh = refresh
	}
	if d.Updater == nil {
		d.Updater = updater.New
	}
	if d.Stdin == nil {
		d.Stdin = os.Stdin
	}
	if d.Watcher == nil {
		d.Watcher = dnswatcher.New
	}
	if d.Notify == nil {
		d.Notify = func(dataDir string, log updater.Logger) { updater.New(dataDir, log).Notify() }
	}
	return d
}

// app is what every command shares: output and dependencies.
type app struct {
	deps   Deps
	out    io.Writer
	errOut io.Writer
	log    *ui.Logger
	con    *ui.Console
	// verbose shows the logs of the image builds (--verbose)
	verbose bool
}

// projectDir returns the data directory, prepared for a command: tree
// created, resources extracted or resynced (get_project_dir).
func (a *app) projectDir() (string, error) {
	dir, err := platform.DataDir()
	if err != nil {
		return "", err
	}
	if err := resources.New(dir, a.log).Ensure(); err != nil {
		return "", err
	}
	return dir, nil
}

// loadEnv reads the .env of dir; a missing file yields no variable.
func (a *app) loadEnv(dir string) (*env.Env, error) {
	e := env.New(filepath.Join(dir, ".env"), filepath.Join(dir, ".env.example"))
	return e, e.Load()
}

// openStore opens db.sqlite of dir.
func (a *app) openStore(dir string) (*store.Store, error) {
	return store.Open(filepath.Join(dir, "db.sqlite"))
}

// docker returns the Docker manager of dir.
func (a *app) docker(dir string) *docker.Manager {
	m := docker.New(dir, a.log)
	m.Runner = a.deps.Docker
	if a.verbose {
		m.BuildOutput = a.out
	}
	return m
}

// skipUpdateCheck lists the commands without the startup notice: the
// hidden ones the CLI spawns itself, and `update`, which asks GitHub.
var skipUpdateCheck = map[string]bool{
	"_serve-ca-internal":       true,
	"_watch-dns-internal":      true,
	"_refresh-update-cache":    true,
	"_sync-resources-internal": true,
	"update":                   true,
}

// notify announces a newer release before cmd, unless --no-update-check.
func (a *app) notify(cmd *cobra.Command, args []string) {
	skip, _ := cmd.Flags().GetBool("no-update-check")
	if cmd.DisableFlagParsing {
		// exec: the root options before it reach its arguments unparsed
		skip, _ = stripRootOptions(args)
	}
	if skip || skipUpdateCheck[cmd.Name()] {
		return
	}
	if dir, err := platform.DataDir(); err == nil {
		a.deps.Notify(dir, a.log)
	}
}

// stripRootOptions removes the root options that cobra leaves, unparsed,
// in front of the arguments of a command without flag parsing (exec):
// `frankenmanager --no-update-check exec 8.4 php -v`. It reports whether
// --no-update-check was one of them. exec has no option of its own, so
// whatever precedes DOMAIN can only be a root option.
func stripRootOptions(args []string) (noUpdateCheck bool, rest []string) {
	for len(args) > 0 && (args[0] == "--no-update-check" || args[0] == "--no-update-check=true") {
		noUpdateCheck, args = true, args[1:]
	}
	return noUpdateCheck, args
}

// noArgs rejects any argument with a usage error, exit code 2.
func noArgs(_ *cobra.Command, args []string) error {
	switch len(args) {
	case 0:
		return nil
	case 1:
		return usageError{fmt.Errorf("Got unexpected extra argument (%s)", args[0])} //nolint:staticcheck // user-facing message
	default:
		return usageError{fmt.Errorf("Got unexpected extra arguments (%s)", strings.Join(args, " "))} //nolint:staticcheck // user-facing message
	}
}

// closeStore closes s, keeping the first error.
func closeStore(s *store.Store, err *error) {
	if cerr := s.Close(); *err == nil {
		*err = cerr
	}
}

// exitError ends a command with an exit code, its messages already
// printed.
type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

// ssl returns the certificate manager of certsDir.
func (a *app) ssl(certsDir string) (*ssl.Manager, error) {
	m, err := ssl.New(certsDir, a.log)
	if err != nil {
		return nil, err
	}
	m.Run = a.deps.Command
	return m, nil
}

// quietLogger drops the messages of a cleanup step.
type quietLogger struct{}

func (quietLogger) Info(string)    {}
func (quietLogger) Success(string) {}
func (quietLogger) Warning(string) {}
func (quietLogger) Error(string)   {}

// shareCA starts the detached root CA server, from the running binary.
func shareCA(dataDir, certFile string, port int) error {
	exe, err := platform.Executable()
	if err != nil {
		return err
	}
	_, err = caserver.StartSharing(dataDir, exe, certFile, port)
	return err
}
