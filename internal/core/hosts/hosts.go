// Package hosts adds and removes entries in the system hosts file.
//
// Each host gets two lines, "127.0.0.1\t<host>" and "::1\t<host>", with no
// block marker, the layout of files written by earlier releases. localhost
// is never touched. On macOS and Linux, the passwordless helper is used when
// configured, otherwise `sudo tee` rewrites the file (sudo may ask for the
// password). On Windows the file is written directly, from an Administrator
// terminal.
package hosts

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/core/privilege"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

const (
	loopbackV4 = "127.0.0.1"
	loopbackV6 = "::1"
)

// Error is a hosts file failure.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// Logger receives the progress messages.
type Logger interface {
	Info(msg string)
	Success(msg string)
}

// Helper runs the passwordless hosts helper.
type Helper interface {
	CanUseHelper(ctx context.Context) bool
	RunHelper(ctx context.Context, action, ip, hostname string) (string, error)
}

// Manager edits a hosts file.
type Manager struct {
	Path    string
	GOOS    string
	Helper  Helper // nil: never use the helper
	Run     command.Runner
	Log     Logger
	IsAdmin func() bool

	helperChecked, helperUsable bool
}

// New returns the Manager of the system hosts file.
func New(log Logger) *Manager {
	return &Manager{
		Path:    platform.HostsFile,
		GOOS:    runtime.GOOS,
		Helper:  privilege.New(nil),
		Run:     command.Exec{},
		Log:     log,
		IsAdmin: platform.IsAdmin,
	}
}

func (m *Manager) windows() bool { return m.GOOS == "windows" }

// canUseHelper checks once whether the helper runs without password.
func (m *Manager) canUseHelper(ctx context.Context) bool {
	if m.Helper == nil || m.windows() {
		return false
	}
	if !m.helperChecked {
		m.helperUsable = m.Helper.CanUseHelper(ctx)
		m.helperChecked = true
	}
	return m.helperUsable
}

// read returns the hosts file with \n line endings.
func (m *Manager) read() (string, error) {
	data, err := os.ReadFile(m.Path)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n"), nil
}

// write replaces the hosts file content (\n line endings).
func (m *Manager) write(ctx context.Context, content string) error {
	if m.windows() {
		// Windows uses \r\n line endings
		content = strings.ReplaceAll(content, "\n", "\r\n")
		if !m.IsAdmin() {
			return &Error{"Failed to write hosts file. Run as Administrator: " +
				"FrankenManager needs an Administrator terminal to modify " + m.Path}
		}
		if err := writeInPlace(m.Path, content); err != nil {
			return &Error{fmt.Sprintf("Failed to write hosts file. Run as Administrator: %v", err)}
		}
		return nil
	}
	res, err := m.Run.Run(ctx, command.Cmd{Name: "sudo", Args: []string{"tee", m.Path}, Stdin: []byte(content)})
	if err != nil {
		return &Error{fmt.Sprintf("Failed to write hosts file: %v", err)}
	}
	if !res.OK() {
		return &Error{"Failed to write hosts file: " + string(res.Stderr)}
	}
	return nil
}

// writeInPlace truncates and rewrites path, keeping its ACLs.
func writeInPlace(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func entryPattern(ip, hostname string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(ip) + `\s+` + regexp.QuoteMeta(hostname) + `\s*$`)
}

// HasEntry reports whether a line maps ip to hostname (trailing spaces
// allowed).
func (m *Manager) HasEntry(ip, hostname string) (bool, error) {
	content, err := m.read()
	if err != nil {
		return false, err
	}
	return hasLine(content, entryPattern(ip, hostname)), nil
}

func hasLine(content string, re *regexp.Regexp) bool {
	for _, line := range strings.Split(content, "\n") {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// AddEntry adds ip and, for 127.0.0.1, ::1 for hostname. It reports
// whether it added them: false for localhost or an existing entry.
func (m *Manager) AddEntry(ctx context.Context, ip, hostname string) (bool, error) {
	if hostname == "localhost" {
		return false, nil
	}
	found, err := m.HasEntry(ip, hostname)
	if err != nil {
		return false, err
	}
	if found {
		m.Log.Info(fmt.Sprintf("%s\t%s already exists", ip, hostname))
		return false, nil
	}

	if m.canUseHelper(ctx) {
		if _, err := m.Helper.RunHelper(ctx, "add", ip, hostname); err != nil {
			return false, nil // the helper failure is silent
		}
		m.Log.Success(fmt.Sprintf("Added %s\t%s to hosts file", ip, hostname))
		if ip == loopbackV4 {
			if _, err := m.Helper.RunHelper(ctx, "add", loopbackV6, hostname); err == nil {
				m.Log.Success(fmt.Sprintf("Added %s\t%s to hosts file", loopbackV6, hostname))
			}
		}
		return true, nil
	}

	content, err := m.read()
	if err != nil {
		return false, err
	}
	entry := ip + "\t" + hostname + "\n"
	if ip == loopbackV4 {
		entry += loopbackV6 + "\t\t" + hostname + "\n"
	}
	if err := m.write(ctx, content+entry); err != nil {
		return false, err
	}
	m.Log.Success(fmt.Sprintf("Added %s\t%s to hosts file", ip, hostname))
	return true, nil
}

// RemoveEntry removes the ip and ::1 lines of hostname. It reports whether
// it removed them: false for localhost or a missing entry.
//
// A helper installed by an earlier release cannot remove entries on Linux:
// when the helper fails, the file is rewritten with `sudo tee`.
func (m *Manager) RemoveEntry(ctx context.Context, ip, hostname string) (bool, error) {
	if hostname == "localhost" {
		return false, nil
	}
	found, err := m.HasEntry(ip, hostname)
	if err != nil {
		return false, err
	}
	if !found {
		m.Log.Info(hostname + " not found in hosts file")
		return false, nil
	}

	if m.canUseHelper(ctx) {
		if _, err := m.Helper.RunHelper(ctx, "remove", ip, hostname); err == nil {
			m.Log.Success(fmt.Sprintf("Removed %s\t%s from hosts file", ip, hostname))
			if ip == loopbackV4 {
				_, _ = m.Helper.RunHelper(ctx, "remove", loopbackV6, hostname)
			}
			return true, nil
		}
	}

	content, err := m.read()
	if err != nil {
		return false, err
	}
	v4, v6 := entryPattern(ip, hostname), entryPattern(loopbackV6, hostname)
	lines := splitLines(content)
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !v4.MatchString(line) && !v6.MatchString(line) {
			kept = append(kept, line)
		}
	}
	if len(kept) == len(lines) {
		return false, nil
	}
	if err := m.write(ctx, strings.Join(kept, "\n")+"\n"); err != nil {
		return false, err
	}
	m.Log.Success("Removed " + hostname + " from hosts file")
	return true, nil
}

// splitLines splits \n-separated content into lines: no trailing empty
// line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
