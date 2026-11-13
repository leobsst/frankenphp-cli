package password

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/command/commandtest"
)

// fakeDB accepts one root password and records the commands.
type fakeDB struct {
	password string
	down     int // attempts refused before the server is up
	calls    []string
}

func (d *fakeDB) Exec(_ context.Context, container string, cmd ...string) (int, string) {
	d.calls = append(d.calls, container+" "+strings.Join(cmd, " "))
	if d.down > 0 {
		d.down--
		return 1, "Can't connect"
	}
	if cmd[3] != "-p"+d.password {
		return 1, "Access denied"
	}
	if strings.HasPrefix(cmd[5], "ALTER USER") {
		d.password = strings.Split(cmd[5], "'")[5]
	}
	return 0, ""
}

func newManager(t *testing.T, db *fakeDB) (*Manager, *commandtest.Log, *int) {
	t.Helper()
	log := &commandtest.Log{}
	m := New(t.TempDir(), db, log)
	sleeps := 0
	m.Sleep = func(time.Duration) { sleeps++ }
	return m, log, &sleeps
}

func TestSyncFromHistory(t *testing.T) {
	db := &fakeDB{password: "old"}
	m, log, _ := newManager(t, db)
	// Legacy MariaDB history, written by earlier releases
	if err := os.WriteFile(filepath.Join(m.Dir, ".db_password_history"), []byte("older\nold\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ok, err := m.Sync(context.Background(), "mariadb", "franken_mariadb", "new")
	if err != nil || !ok {
		t.Fatalf("sync = %v, %v", ok, err)
	}
	if db.password != "new" {
		t.Errorf("server password = %q", db.password)
	}
	want := []string{
		"franken_mariadb mariadb -u root -pnew -e SELECT 1",
		"franken_mariadb mariadb -u root -pold -e SELECT 1",
		"franken_mariadb mariadb -u root -pold -e ALTER USER 'root'@'%' IDENTIFIED BY 'new'; " +
			"ALTER USER 'root'@'localhost' IDENTIFIED BY 'new'; FLUSH PRIVILEGES;",
	}
	if !slices.Equal(db.calls, want) {
		t.Errorf("calls =\n%s", strings.Join(db.calls, "\n"))
	}
	data, _ := os.ReadFile(filepath.Join(m.Dir, ".db_password_history_mariadb"))
	if string(data) != "new\n" {
		t.Errorf("history = %q", data)
	}
	if info, _ := os.Stat(filepath.Join(m.Dir, ".db_password_history_mariadb")); runtime.GOOS != "windows" && info.Mode().Perm() != 0o660 { // Windows keeps no Unix mode
		t.Errorf("history mode %v", info.Mode())
	}
	if !slices.Equal(log.Lines, []string{"success: MariaDB password updated to match the configuration"}) {
		t.Errorf("log = %v", log.Lines)
	}
	if got := m.History("mariadb"); !slices.Equal(got, []string{"new"}) {
		t.Errorf("history = %v", got)
	}
}

func TestSyncAlreadyInSyncAfterRetries(t *testing.T) {
	db := &fakeDB{password: "pw", down: 2}
	m, log, sleeps := newManager(t, db)
	ok, err := m.Sync(context.Background(), "mysql", "franken_mysql", "pw")
	if err != nil || !ok || *sleeps != 2 {
		t.Fatalf("sync = %v, %v, %d sleeps", ok, err, *sleeps)
	}
	if !slices.Equal(log.Lines, []string{"info: MySQL password already in sync"}) {
		t.Errorf("log = %v", log.Lines)
	}
	if got := m.History("mysql"); !slices.Equal(got, []string{"pw"}) {
		t.Errorf("history = %v", got)
	}
}

func TestSyncGivesUp(t *testing.T) {
	db := &fakeDB{password: "unknown"}
	m, log, sleeps := newManager(t, db)
	m.Retries = 3
	ok, err := m.Sync(context.Background(), "mariadb", "franken_mariadb", "new")
	if err != nil || ok || *sleeps != 2 {
		t.Fatalf("sync = %v, %v, %d sleeps", ok, err, *sleeps)
	}
	if !slices.Equal(log.Lines, []string{"info: MariaDB password sync skipped (could not connect)"}) {
		t.Errorf("log = %v", log.Lines)
	}
}

func TestSyncAllSkipsPostgreSQLAndMissingPasswords(t *testing.T) {
	db := &fakeDB{password: "pw"}
	m, _, _ := newManager(t, db)
	err := m.SyncAll(context.Background(), []string{"postgresql", "mysql", "mariadb"},
		map[string]string{"postgresql": "pg", "mysql": "pw"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"franken_mysql mysql -u root -ppw -e SELECT 1"}; !slices.Equal(db.calls, want) {
		t.Errorf("calls = %v", db.calls)
	}
}
