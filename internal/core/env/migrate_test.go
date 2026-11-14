package env

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

func TestMigrate(t *testing.T) {
	dir := t.TempDir()
	content := "APP_ENV=dev\nDEFAULT_PHP_VERSION=8.4\nMARIADB_ROOT_PASSWORD=secret\nDNS_IP=\nUSER=ci\nDB_PORT=3306\n" +
		"REDIS_ENABLED=True\nPHP_IMAGE_DISTRO=ubuntu\nWEB_HTTP_PORT=99999\n"
	for name, data := range map[string]string{".env": content, ".env.example": "UID=\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	e, m, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Empty values and defaults are left out, so they follow future defaults;
	// invalid values too, while a mere case difference is normalized
	want := Migration{
		Migrated: true,
		Imported: []string{"DEFAULT_PHP_VERSION", "MARIADB_ROOT_PASSWORD", "REDIS_ENABLED"},
		Unknown:  []string{"USER"},
		Invalid:  []string{"PHP_IMAGE_DISTRO", "WEB_HTTP_PORT"},
		Backup:   filepath.Join(dir, ".env.migrated"),
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("migration = %+v, want %+v", m, want)
	}
	if !reflect.DeepEqual(e.Vars(), map[string]string{
		"DEFAULT_PHP_VERSION": "8.4", "MARIADB_ROOT_PASSWORD": "secret", "REDIS_ENABLED": "true",
	}) {
		t.Errorf("vars = %v", e.Vars())
	}
	if data, err := os.ReadFile(m.Backup); err != nil || string(data) != content {
		t.Errorf("backup: %q, %v", data, err)
	}
	for _, name := range []string{".env", ".env.example"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s still there", name)
		}
	}

	// Done once: a .env written afterwards is ignored, its values unread
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DEFAULT_PHP_VERSION=8.5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e, m, err = Open(dir)
	if err != nil || !m.Ignored || m.Migrated || e.Get("DEFAULT_PHP_VERSION") != "8.4" {
		t.Errorf("second open: %+v, %v, %q", m, err, e.Get("DEFAULT_PHP_VERSION"))
	}
}

func TestMigrateKeepsAnEarlierBackup(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{".env": "DB_PORT=3310\n", ".env.migrated": "old\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, m, err := Open(dir)
	if err != nil || m.Backup == "" || m.Backup == filepath.Join(dir, ".env.migrated") {
		t.Fatalf("migration = %+v, %v", m, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, ".env.migrated")); string(data) != "old\n" {
		t.Errorf("earlier backup overwritten: %q", data)
	}
}

func TestOpenWithoutEnvFile(t *testing.T) {
	dir := t.TempDir()
	_, m, err := Open(dir)
	if err != nil || !reflect.DeepEqual(m, Migration{}) {
		t.Errorf("%+v, %v", m, err)
	}
	if _, ok, _ := (store.ConfigDB{Path: filepath.Join(dir, "db.sqlite")}).Setting(store.SettingEnvMigrated); ok {
		t.Error("marked as migrated without a .env")
	}
}

func TestCheck(t *testing.T) {
	cases := []struct {
		key, value string
		ok         bool
	}{
		{"REDIS_ENABLED", "true", true},
		{"REDIS_ENABLED", "yes", false},
		{"DEFAULT_PHP_VERSION", "8.5", true},
		{"DEFAULT_PHP_VERSION", "7.4", false},
		{"WEB_HTTPS_PORT", "8443", true},
		{"WEB_HTTPS_PORT", "70000", false},
		{"DB_ENGINES", "mariadb, PostgreSQL", true},
		{"DB_ENGINES", "mariadb,oracle", false},
		{"DNS_IP", "", true}, // empty: auto-detect
		{"DNS_IP", "192.168.1.300", false},
		{"DNS_UPSTREAM", "1.1.1.1 9.9.9.9", true},
		{"CLI_ALIAS", "fk", true},
		{"CLI_ALIAS", "", true}, // no alias
		{"CLI_ALIAS", "frankenmanager", false},
		{"CLI_ALIAS", "rm -rf", false},
	}
	for _, c := range cases {
		d, ok := Lookup(c.key)
		if !ok {
			t.Fatalf("%s: unknown", c.key)
		}
		if err := d.Check(c.value); (err == nil) != c.ok {
			t.Errorf("Check(%s, %q) = %v", c.key, c.value, err)
		}
	}
	if d, ok := Lookup("redis_enabled"); !ok || d.Key != "REDIS_ENABLED" {
		t.Error("lookup is not case-insensitive")
	}
}
