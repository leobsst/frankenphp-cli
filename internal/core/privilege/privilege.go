// Package privilege configures passwordless hosts file changes.
//
// On macOS and Linux, `sudo frankenmanager setup` installs a helper script
// that may only add or remove 127.0.0.1 / ::1 entries, and a sudoers rule
// letting the user run it without password. The paths are those earlier
// releases used, so the files of existing installations are recognized and
// reused.
//
// On Windows, no helper is installed: the PowerShell helper of earlier
// releases ran without elevation, so it only worked from an Administrator
// terminal anyway. FrankenManager writes the hosts file itself and requires
// such a terminal; `setup --remove` deletes the old helper.
package privilege

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

const (
	// HelperPath is the hosts helper script (macOS, Linux).
	HelperPath = "/usr/local/bin/frankenmanager-hosts-helper"
	// SudoersPath is the sudoers rule allowing the helper without password.
	SudoersPath = "/etc/sudoers.d/frankenmanager"
	// windowsHelperName is the PowerShell helper of earlier releases, in
	// %LOCALAPPDATA%\FrankenManager.
	windowsHelperName = "FrankenManagerHostsHelper.ps1"

	helperTimeout = 10 * time.Second
	probeTimeout  = 5 * time.Second
)

// Error is a configuration failure.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// Logger receives the progress messages.
type Logger interface {
	Info(msg string)
	Success(msg string)
	Warning(msg string)
	Error(msg string)
}

// Manager installs, checks and removes the privilege configuration.
type Manager struct {
	GOOS        string
	HostsPath   string
	HelperPath  string
	SudoersPath string
	// WindowsHelperPath is the PowerShell helper left by earlier releases.
	WindowsHelperPath string
	Run               command.Runner
	Log               Logger
	IsAdmin           func() bool
	// Owner sets the owner of a file (os.Chown), replaced in tests.
	Owner func(path string, uid, gid int) error
}

// New returns the Manager of the running system.
func New(log Logger) *Manager {
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		appData, _ = os.UserHomeDir()
	}
	return &Manager{
		GOOS:              runtime.GOOS,
		HostsPath:         platform.HostsFile,
		HelperPath:        HelperPath,
		SudoersPath:       SudoersPath,
		WindowsHelperPath: filepath.Join(appData, "FrankenManager", windowsHelperName),
		Run:               command.Exec{},
		Log:               log,
		IsAdmin:           platform.IsAdmin,
		Owner:             os.Chown,
	}
}

func (m *Manager) windows() bool { return m.GOOS == "windows" }

// Configured reports whether the hosts file can be changed: the helper and
// the sudoers rule exist (macOS, Linux), or the hosts file is writable
// (Windows, Administrator terminal).
func (m *Manager) Configured() bool {
	if m.windows() {
		return m.hostsWritable()
	}
	return exists(m.HelperPath) && exists(m.SudoersPath)
}

// UpToDate reports whether the installed helper is the current one: a
// helper installed by an earlier release fails to remove entries on Linux,
// `setup` replaces it.
func (m *Manager) UpToDate() bool {
	if m.windows() {
		return true
	}
	data, err := os.ReadFile(m.HelperPath)
	return err == nil && string(data) == HelperScript(m.HostsPath)
}

func (m *Manager) hostsWritable() bool {
	f, err := os.OpenFile(m.HostsPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return false
	}
	return f.Close() == nil
}

// CanUseHelper reports whether the helper runs without password (macOS,
// Linux), or whether the hosts file is writable (Windows).
func (m *Manager) CanUseHelper(ctx context.Context) bool {
	if !m.Configured() {
		return false
	}
	if m.windows() {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	// -n: sudo fails instead of asking for a password
	res, err := m.Run.Run(ctx, command.Cmd{Name: "sudo", Args: []string{"-n", m.HelperPath, "--help"}})
	return err == nil && !strings.Contains(string(res.Stderr), "password is required")
}

// RunHelper runs `sudo <helper> <action> <ip> <hostname>` and returns its
// output, or an error carrying the helper's message.
func (m *Manager) RunHelper(ctx context.Context, action, ip, hostname string) (string, error) {
	if !exists(m.HelperPath) {
		return "", errors.New("Helper script not installed. Run 'frankenmanager setup' first.") //nolint:staticcheck // user-facing message
	}
	ctx, cancel := context.WithTimeout(ctx, helperTimeout)
	defer cancel()
	res, err := m.Run.Run(ctx, command.Cmd{Name: "sudo", Args: []string{m.HelperPath, action, ip, hostname}})
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "", errors.New("Helper script timed out") //nolint:staticcheck // user-facing message
	case err != nil:
		return "", err
	case !res.OK():
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = strings.TrimSpace(string(res.Stdout))
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// Setup installs the configuration of the running system. It requires root
// (Administrator on Windows).
func (m *Manager) Setup(ctx context.Context) error {
	if m.windows() {
		return m.setupWindows()
	}
	return m.setupUnix(ctx)
}

func (m *Manager) setupUnix(ctx context.Context) error {
	if !m.IsAdmin() {
		return &Error{"Setup requires root privileges. Please run with: sudo frankenmanager setup"}
	}

	m.Log.Info("Creating helper script at " + m.HelperPath)
	if err := os.WriteFile(m.HelperPath, []byte(HelperScript(m.HostsPath)), 0o755); err != nil {
		return err
	}
	if err := os.Chmod(m.HelperPath, 0o755); err != nil { // WriteFile keeps the mode of an existing file
		return err
	}
	if err := m.Owner(m.HelperPath, 0, 0); err != nil {
		return err
	}

	username, err := sudoUser()
	if err != nil {
		return err
	}
	if err := m.installSudoers(ctx, SudoersContent(username, m.HelperPath, m.SudoersPath)); err != nil {
		return &Error{fmt.Sprintf("Failed to configure sudoers: %v", err)}
	}

	m.Log.Success("Privilege configuration complete!")
	m.Log.Info("You can now run frankenmanager start/stop without sudo prompts")
	return nil
}

// installSudoers validates content with visudo, then installs it
// atomically: a broken sudoers file would break sudo itself. The temporary
// file has a dot in its name, which sudo ignores in sudoers.d.
func (m *Manager) installSudoers(ctx context.Context, content string) error {
	tmp := filepath.Join(filepath.Dir(m.SudoersPath), "."+filepath.Base(m.SudoersPath)+".tmp")
	if err := os.WriteFile(tmp, []byte(content), 0o440); err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()

	res, err := m.Run.Run(ctx, command.Cmd{Name: "visudo", Args: []string{"-c", "-f", tmp}})
	if err != nil {
		return err
	}
	if !res.OK() {
		return &Error{"Invalid sudoers syntax: " + string(res.Stderr)}
	}
	m.Log.Info("Installing sudoers configuration at " + m.SudoersPath)
	if err := os.Chmod(tmp, 0o440); err != nil {
		return err
	}
	if err := m.Owner(tmp, 0, 0); err != nil {
		return err
	}
	return os.Rename(tmp, m.SudoersPath)
}

// sudoUser is the user who ran sudo, the current user otherwise.
func sudoUser() (string, error) {
	if name := os.Getenv("SUDO_USER"); name != "" {
		return name, nil
	}
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

func (m *Manager) setupWindows() error {
	if !m.IsAdmin() {
		m.administratorHelp("setup")
		return &Error{"Administrator privileges required"}
	}
	m.removeWindowsHelper()
	m.Log.Success("The hosts file can be modified from this Administrator terminal.")
	m.Log.Info("Run FrankenManager from an Administrator terminal whenever it changes the hosts file")
	m.Log.Info("(start, stop, add-host, add-alias, remove-host, restore-host).")
	return nil
}

func (m *Manager) administratorHelp(cmd string) {
	m.Log.Error("Setup requires Administrator privileges.")
	m.Log.Info("Please run this command in an Administrator terminal:")
	m.Log.Info("  1. Right-click on PowerShell or Command Prompt")
	m.Log.Info("  2. Select 'Run as administrator'")
	m.Log.Info("  3. Run: frankenmanager " + cmd)
}

// removeWindowsHelper deletes the PowerShell helper of earlier releases and
// its directory when empty. It reports whether there was one.
func (m *Manager) removeWindowsHelper() bool {
	if !exists(m.WindowsHelperPath) {
		return false
	}
	m.Log.Info("Removing helper script: " + m.WindowsHelperPath)
	if err := os.Remove(m.WindowsHelperPath); err != nil {
		m.Log.Warning(fmt.Sprintf("Could not remove %s: %v", m.WindowsHelperPath, err))
		return false
	}
	_ = os.Remove(filepath.Dir(m.WindowsHelperPath)) // only when empty
	return true
}

// Remove deletes the configuration (`setup --remove`).
func (m *Manager) Remove() error {
	if m.windows() {
		if m.removeWindowsHelper() {
			m.Log.Success("Helper script removed")
		} else {
			m.Log.Info("No helper script found")
		}
		m.Log.Info("")
		m.Log.Info("To restore default hosts file permissions:")
		m.Log.Info("  1. Right-click on " + m.HostsPath)
		m.Log.Info("  2. Go to Properties -> Security")
		m.Log.Info("  3. Remove any custom user permissions")
		return nil
	}

	if !m.IsAdmin() {
		return &Error{"Removal requires root privileges. Please run with: sudo frankenmanager setup --remove"}
	}
	removed := false
	for _, f := range []struct{ label, path string }{
		{"helper script", m.HelperPath},
		{"sudoers configuration", m.SudoersPath},
	} {
		if !exists(f.path) {
			continue
		}
		m.Log.Info(fmt.Sprintf("Removing %s: %s", f.label, f.path))
		if err := os.Remove(f.path); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		m.Log.Success("Privilege configuration removed")
	} else {
		m.Log.Info("No privilege configuration found")
	}
	return nil
}

// Status is the privilege configuration shown by `setup --status`.
type Status struct {
	Platform              string // "darwin", "linux" or "windows"
	Configured            bool
	IsAdmin               bool
	HelperInstalled       bool // macOS, Linux only
	SudoersConfigured     bool // macOS, Linux only
	PasswordlessAvailable bool
}

// Status returns the current configuration.
func (m *Manager) Status(ctx context.Context) Status {
	s := Status{
		Platform:              m.GOOS,
		Configured:            m.Configured(),
		IsAdmin:               m.IsAdmin(),
		PasswordlessAvailable: m.CanUseHelper(ctx),
	}
	if !m.windows() {
		s.HelperInstalled = exists(m.HelperPath)
		s.SudoersConfigured = exists(m.SudoersPath)
	}
	return s
}

// HelperScript is the hosts helper. It removes entries with grep and cat
// instead of `sed -i ”`, which only BSD sed accepts, so removal also works
// on Linux; cat keeps the inode and mode of the hosts file.
func HelperScript(hostsPath string) string {
	return `#!/bin/bash
# FrankenManager Hosts Helper
# This script is allowed to run with passwordless sudo to modify /etc/hosts
# It only allows adding/removing entries matching 127.0.0.1 or ::1

set -euo pipefail

HOSTS_FILE="` + hostsPath + `"
ACTION="${1:-}"
IP="${2:-}"
HOSTNAME="${3:-}"

# Validate action
if [[ "$ACTION" != "add" && "$ACTION" != "remove" ]]; then
    echo "Error: Action must be 'add' or 'remove'" >&2
    exit 1
fi

# Validate IP (only allow localhost IPs)
if [[ "$IP" != "127.0.0.1" && "$IP" != "::1" ]]; then
    echo "Error: Only 127.0.0.1 and ::1 are allowed" >&2
    exit 1
fi

# Validate hostname (basic validation)
if [[ -z "$HOSTNAME" || "$HOSTNAME" == "localhost" ]]; then
    echo "Error: Invalid hostname" >&2
    exit 1
fi

# Validate hostname format (alphanumeric, dots, hyphens only)
if ! [[ "$HOSTNAME" =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]$ ]]; then
    echo "Error: Hostname contains invalid characters" >&2
    exit 1
fi

PATTERN="^$IP[[:space:]]+$HOSTNAME[[:space:]]*$"

if [[ "$ACTION" == "add" ]]; then
    # Allow trailing whitespace in the pattern
    if grep -Eq "$PATTERN" "$HOSTS_FILE" 2>/dev/null; then
        echo "Entry already exists"
        exit 0
    fi

    # Add entry
    echo -e "$IP\t$HOSTNAME" >> "$HOSTS_FILE"
    echo "Added $IP $HOSTNAME"

elif [[ "$ACTION" == "remove" ]]; then
    if grep -Eq "$PATTERN" "$HOSTS_FILE" 2>/dev/null; then
        # Portable on BSD and GNU, unlike sed -i; cat keeps the inode
        TMP_FILE="$(mktemp)"
        trap 'rm -f "$TMP_FILE"' EXIT
        grep -Ev "$PATTERN" "$HOSTS_FILE" > "$TMP_FILE" || true
        cat "$TMP_FILE" > "$HOSTS_FILE"
        echo "Removed $IP $HOSTNAME"
        exit 0
    else
        echo "Entry not found"
        exit 1
    fi
fi
`
}

// SudoersContent is the sudoers rule letting username run the helper
// without password.
func SudoersContent(username, helperPath, sudoersPath string) string {
	return `# FrankenManager - Allow passwordless sudo for hosts helper
# This file was automatically generated by frankenmanager setup
# To remove: sudo rm ` + sudoersPath + `

` + username + ` ALL=(ALL) NOPASSWD: ` + helperPath + `
`
}

// exists reports whether p exists. A file that cannot be checked counts as
// present: /etc/sudoers.d is often 0750 root on Linux, where a plain
// existence check fails for a non-root user and the helper would never be
// used. `sudo -n` then decides (CanUseHelper).
func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}
