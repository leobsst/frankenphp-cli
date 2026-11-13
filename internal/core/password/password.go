// Package password syncs the database root passwords with the configuration.
//
// When the configuration holds a new password, the running server still has the old
// one: the old ones are kept in .db_password_history_<engine>, the right
// one is found by trying them, then ALTER USER sets the new one. Only
// MariaDB and MySQL need it: PostgreSQL reads POSTGRES_PASSWORD on start.
package password

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/compose"
)

// DefaultRetries is how many times (one per second) a server is tried
// before giving up, while it starts.
const DefaultRetries = 30

// Execer runs a command in a container (docker.Manager).
type Execer interface {
	Exec(ctx context.Context, container string, command ...string) (int, string)
}

// Logger receives the progress messages.
type Logger interface {
	Info(msg string)
	Success(msg string)
	Error(msg string)
}

// Manager syncs the passwords of a data directory's servers.
type Manager struct {
	Dir     string // data directory, holding the history files
	Docker  Execer
	Log     Logger
	Retries int
	Sleep   func(time.Duration)
}

// New returns a Manager for dataDir.
func New(dataDir string, docker Execer, log Logger) *Manager {
	return &Manager{Dir: dataDir, Docker: docker, Log: log, Retries: DefaultRetries, Sleep: time.Sleep}
}

func (m *Manager) historyFile(engine string) string {
	return filepath.Join(m.Dir, ".db_password_history_"+engine)
}

// History returns the passwords used before, oldest first. A MariaDB
// without history reads the legacy .db_password_history, the single file
// earlier releases wrote before the history was split per engine.
func (m *Manager) History(engine string) []string {
	data, err := os.ReadFile(m.historyFile(engine))
	if err != nil && engine == "mariadb" {
		data, err = os.ReadFile(filepath.Join(m.Dir, ".db_password_history"))
	}
	if err != nil {
		return nil
	}
	var passwords []string
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			passwords = append(passwords, line)
		}
	}
	return passwords
}

// save appends password to the history unless already there.
func (m *Manager) save(engine, password string) error {
	if slices.Contains(m.History(engine), password) {
		return nil
	}
	path := m.historyFile(engine)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o660)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(password + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(path, 0o660)
}

func client(engine string) string {
	if engine == "mariadb" {
		return "mariadb"
	}
	return "mysql"
}

func label(engine string) string {
	if engine == "mariadb" {
		return "MariaDB"
	}
	return "MySQL"
}

func (m *Manager) works(ctx context.Context, engine, container, password string) bool {
	code, _ := m.Docker.Exec(ctx, container, client(engine), "-u", "root", "-p"+password, "-e", "SELECT 1")
	return code == 0
}

// working returns the password the server accepts: the new one first, then
// the history from the most recent; "" when none works.
func (m *Manager) working(ctx context.Context, engine, container, newPassword string) string {
	if m.works(ctx, engine, container, newPassword) {
		return newPassword
	}
	history := m.History(engine)
	for i := len(history) - 1; i >= 0; i-- {
		if m.works(ctx, engine, container, history[i]) {
			return history[i]
		}
	}
	return ""
}

// Sync sets newPassword on a MariaDB or MySQL server. It reports whether
// the server ends up with it.
func (m *Manager) Sync(ctx context.Context, engine, container, newPassword string) (bool, error) {
	current := ""
	for i := 0; i < m.Retries && current == ""; i++ {
		if i > 0 {
			m.Sleep(time.Second)
		}
		current = m.working(ctx, engine, container, newPassword)
	}
	if current == "" {
		m.Log.Info(label(engine) + " password sync skipped (could not connect)")
		return false, nil
	}
	if current == newPassword {
		if err := m.save(engine, newPassword); err != nil {
			return false, err
		}
		m.Log.Info(label(engine) + " password already in sync")
		return true, nil
	}

	sql := fmt.Sprintf("ALTER USER 'root'@'%%' IDENTIFIED BY '%s'; "+
		"ALTER USER 'root'@'localhost' IDENTIFIED BY '%s'; FLUSH PRIVILEGES;", newPassword, newPassword)
	if code, _ := m.Docker.Exec(ctx, container, client(engine), "-u", "root", "-p"+current, "-e", sql); code != 0 {
		m.Log.Error("Failed to sync " + label(engine) + " password")
		return false, nil
	}
	if err := m.save(engine, newPassword); err != nil {
		return false, err
	}
	m.Log.Success(label(engine) + " password updated to match the configuration")
	return true, nil
}

// SyncAll syncs the password of every enabled MariaDB and MySQL server.
// passwords maps an engine to its configured password; engines without one are
// skipped.
func (m *Manager) SyncAll(ctx context.Context, engines []string, passwords map[string]string) error {
	for _, engine := range engines {
		if engine != "mariadb" && engine != "mysql" {
			continue
		}
		if passwords[engine] == "" {
			continue
		}
		if _, err := m.Sync(ctx, engine, compose.DBContainers[engine], passwords[engine]); err != nil {
			return err
		}
	}
	return nil
}
