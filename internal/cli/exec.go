package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/core/resources"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/core/validate"
)

// Fallback user when the host has no Unix IDs (Windows) and .env sets
// none: the default user of WSL2 distributions and most Linux desktops.
const defaultUID, defaultGID = "1000", "1000"

func newExecCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec DOMAIN|VERSION COMMAND...",
		Short: "Run a command with a PHP version (or a domain's), from the current directory.",
		Long: `Run a command with a PHP version (or a domain's), from the current directory.

The command runs as the current user in a one-off container of the
version's PHP image, with the current directory mounted at the same path.
A quoted command is run through a shell, so pipes and && work.

DOMAIN|VERSION: PHP version to use (` + strings.Join(phpversions.Supported, ", ") + `),
or domain (or alternate host) whose PHP version to use.

Examples:
    frankenmanager exec 8.4 php -v
    frankenmanager exec 8.2 composer install
    frankenmanager exec myapp.test php -v
    frankenmanager exec myapp.test composer install
    frankenmanager exec myapp.test "php artisan migrate --force"
    frankenmanager exec legacy.test "php artisan config:clear && php artisan cache:clear"`,
		// Everything after DOMAIN belongs to the command, -v and --help
		// included
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, args = stripRootOptions(args)
			if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
				return cmd.Help()
			}
			if len(args) > 0 && args[0] == "--" {
				args = args[1:]
			}
			if len(args) == 0 {
				return usageError{errors.New("Missing argument 'DOMAIN|VERSION'.")} //nolint:staticcheck // user-facing message
			}
			if len(args) == 1 {
				return usageError{errors.New("Missing argument 'COMMAND...'.")} //nolint:staticcheck // user-facing message
			}
			return a.exec(cmd.Context(), args[0], args[1:])
		},
	}
	return cmd
}

// resolvePHPVersion returns the version given as-is, or the one serving a
// domain or an alias's target.
func resolvePHPVersion(s *store.Store, domain string) (string, error) {
	if slices.Contains(phpversions.Supported, domain) {
		return domain, nil
	}
	if v, ok, err := s.DomainPHPVersion(domain); err != nil || ok {
		return v, err
	}
	if target, ok, err := s.AliasTarget(domain); err != nil {
		return "", err
	} else if ok {
		if v, ok, err := s.DomainPHPVersion(target); err != nil || ok {
			return v, err
		}
	}
	if _, ok, err := s.ProxyTarget(domain); err != nil {
		return "", err
	} else if ok {
		return "", &validate.Error{Msg: fmt.Sprintf("'%s' is a proxy host: it has no PHP version.", domain)}
	}
	return "", &validate.Error{Msg: fmt.Sprintf("Domain '%s' is not configured.", domain)}
}

// containerPath is where a host directory is mounted in the container:
// POSIX paths as-is, Windows ones as /c/Users/...
func containerPath(p string) string {
	if len(p) >= 2 && p[1] == ':' && ('a' <= p[0]|0x20 && p[0]|0x20 <= 'z') {
		rest := strings.Trim(strings.ReplaceAll(p[2:], `\`, "/"), "/")
		out := "/" + strings.ToLower(p[:1])
		if rest != "" {
			out += "/" + rest
		}
		return out
	}
	return filepath.ToSlash(p)
}

// execUser returns the current user as a Docker uid:gid, from .env (or
// 1000:1000) on Windows.
func execUser(goos string, e *env.Env) string {
	if goos != "windows" {
		return fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	}
	uid, gid := e.Get("UID"), e.Get("GID")
	if uid == "" {
		uid = defaultUID
	}
	if gid == "" {
		gid = defaultGID
	}
	return uid + ":" + gid
}

// isUnder reports whether p is root or inside it, lexically (Path.parents).
func isUnder(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

// execMounts returns the host directories to mount, each at the same path:
// the projects root, so that apps reaching sibling projects keep working,
// plus cwd unless it is inside it.
func execMounts(cwd, projectPath string) []string {
	var mounts []string
	if projectPath != "" {
		root := projectPath
		if root == "~" || strings.HasPrefix(root, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				root = filepath.Join(home, root[1:])
			}
		}
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			mounts = append(mounts, root)
			candidates := []string{root}
			if real, err := filepath.EvalSymlinks(root); err == nil {
				if abs, err := filepath.Abs(real); err == nil {
					candidates = append(candidates, abs)
				}
			}
			for _, c := range candidates {
				if isUnder(cwd, c) {
					return mounts
				}
			}
		}
	}
	return append(mounts, cwd)
}

// iniMount is a PHP config file mounted read-only.
type iniMount struct{ host, container string }

// runArgs is the `docker run` command line. A single argument goes through
// a shell, so that a quoted command line (pipes, &&) works as typed;
// several are run as-is, keeping the user's quoting.
func runArgs(image, user, workdir string, mounts []string, inis []iniMount, command []string, tty bool) []string {
	args := []string{"run", "--rm", "-i"}
	if tty {
		args = append(args, "-t")
	}
	args = append(args,
		// Same network as the PHP containers: databases, Redis and the
		// other domains are reachable as from the served sites
		"--network", "host",
		"--user", user,
		// No entrypoint: it updates the CA store, which needs root
		"--entrypoint", "",
		// The host user has no home in the image
		"-e", "HOME=/tmp",
		"-e", "COMPOSER_CACHE_DIR=/tmp/composer-cache",
		// Mounted files may belong to another UID than the user's
		"-e", "GIT_CONFIG_COUNT=1",
		"-e", "GIT_CONFIG_KEY_0=safe.directory",
		"-e", "GIT_CONFIG_VALUE_0=*",
	)
	for _, m := range mounts {
		args = append(args, "-v", m+":"+containerPath(m))
	}
	for _, ini := range inis {
		args = append(args, "-v", ini.host+":"+ini.container+":ro")
	}
	args = append(args, "-w", containerPath(workdir), image)
	if len(command) == 1 {
		return append(args, "sh", "-c", command[0])
	}
	return append(args, command...)
}

// exec runs command in a one-off container of a PHP version's image, as
// the current user, and exits with its exit code.
// The server does not need to run.
func (a *app) exec(ctx context.Context, target string, command []string) (err error) {
	if !slices.ContainsFunc(command, func(part string) bool { return strings.TrimSpace(part) != "" }) {
		return &validate.Error{Msg: "No command given."}
	}
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	s, err := a.openStore(dir)
	if err != nil {
		return err
	}
	defer closeStore(s, &err)

	php, err := resolvePHPVersion(s, target)
	if err != nil {
		return err
	}
	image := phpversions.ImageName(php)
	if !a.docker(dir).ImageExists(ctx, image) {
		return fmt.Errorf("Image '%s' not found: start the server (or add a host) "+ //nolint:staticcheck // user-facing message
			"with PHP %s to build it.", image, php)
	}

	if err := resources.New(dir, a.log).EnsurePHPVersionConfig(php); err != nil {
		return err
	}
	iniDir := filepath.Join(dir, "php", php)
	candidates := []iniMount{{filepath.Join(iniDir, "php.ini"), "/usr/local/etc/php/php.ini"}}
	if e.IsProduction() {
		candidates = append(candidates, iniMount{filepath.Join(iniDir, "php-prod.ini"), "/usr/local/etc/php/conf.d/99-production.ini"})
	}
	var inis []iniMount
	for _, c := range candidates {
		if info, err := os.Stat(c.host); err == nil && info.Mode().IsRegular() {
			inis = append(inis, c)
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// The real path, with the symlinks resolved
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	projectPath, err := stack.ProjectPath(e, s)
	if err != nil {
		return err
	}
	args := runArgs(image, execUser(runtime.GOOS, e), cwd, execMounts(cwd, projectPath), inis, command, a.deps.TTY())
	code, err := a.deps.Interactive(ctx, "docker", args)
	if err != nil {
		return err
	}
	if code != 0 {
		return exitError(code)
	}
	return nil
}

// runInteractive runs a program on the terminal and returns its exit code.
// Ctrl-C reaches it through the terminal; this process waits for it
// instead of dying first.
func runInteractive(ctx context.Context, name string, args []string) (int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return code, nil
		}
		return 130, nil // killed by a signal
	}
	if err != nil {
		return 0, fmt.Errorf("Failed to run %s: %w", name, err) //nolint:staticcheck // shown as "ERROR: ..."
	}
	return 0, nil
}

// stdioIsTTY reports whether stdin and stdout are terminals.
func stdioIsTTY() bool {
	in, out := os.Stdin.Fd(), os.Stdout.Fd()
	return (isatty.IsTerminal(in) || isatty.IsCygwinTerminal(in)) &&
		(isatty.IsTerminal(out) || isatty.IsCygwinTerminal(out))
}
