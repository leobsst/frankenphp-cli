package privilege

import (
	"context"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// bindCapability lets a binary bind ports below 1024 (port 53 of the LAN
// DNS server) without running as root.
const bindCapability = "cap_net_bind_service"

// BindPortNeeded reports whether the OS reserves port 53 to root for a user
// process: Linux only (macOS lets any user bind it, Windows has no
// reserved ports).
func (m *Manager) BindPortNeeded() bool { return m.GOOS == "linux" }

// GrantBindPort lets exe bind port 53 (setcap on its real path). Linux
// only, as root; a no-op elsewhere. The capability is lost when the file
// is replaced: `update` grants it again.
func (m *Manager) GrantBindPort(ctx context.Context, exe string) error {
	if !m.BindPortNeeded() {
		return nil
	}
	if !m.IsAdmin() {
		return &Error{i18n.T("Allowing port 53 requires root privileges. Please run with: sudo frankenmanager setup")}
	}
	if m.Run.LookPath("setcap") == "" {
		// setcap is not installed
		return &Error{i18n.T("setcap not found: install libcap2-bin (Debian, Ubuntu) or libcap (Fedora, Arch), " +
			"then run: sudo frankenmanager setup")}
	}
	res, err := m.Run.Run(ctx, command.Cmd{Name: "setcap", Args: []string{bindCapability + "=+ep", exe}})
	if err != nil {
		return err
	}
	if !res.OK() {
		return &Error{i18n.Tf("setcap failed on %s: %s", exe, strings.TrimSpace(string(res.Stderr)))}
	}
	return nil
}

// RevokeBindPort removes the capabilities of exe (`setup --remove`). Linux
// only, as root; a no-op elsewhere or without setcap.
func (m *Manager) RevokeBindPort(ctx context.Context, exe string) bool {
	if !m.BindPortNeeded() || !m.IsAdmin() || m.Run.LookPath("setcap") == "" || !m.CanBindPort(ctx, exe) {
		return false
	}
	res, err := m.Run.Run(ctx, command.Cmd{Name: "setcap", Args: []string{"-r", exe}})
	return err == nil && res.OK()
}

// CanBindPort reports whether exe may bind port 53: always outside Linux,
// otherwise when getcap shows the capability.
func (m *Manager) CanBindPort(ctx context.Context, exe string) bool {
	if !m.BindPortNeeded() {
		return true
	}
	if m.Run.LookPath("getcap") == "" {
		return false
	}
	res, err := m.Run.Run(ctx, command.Cmd{Name: "getcap", Args: []string{exe}})
	return err == nil && res.OK() && strings.Contains(string(res.Stdout), bindCapability)
}
