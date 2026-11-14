package env

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

const (
	// FileName is the legacy configuration file of the data directory.
	FileName = ".env"
	// MigratedFileName is what FileName is renamed to once migrated.
	MigratedFileName = ".env.migrated"
	// exampleFileName is the template earlier releases extracted next to
	// FileName.
	exampleFileName = ".env.example"
)

// MigrationBackend is a Backend that also keeps the migration marker.
type MigrationBackend interface {
	Backend
	Setting(key string) (string, bool, error)
	SetSetting(key, value string) error
}

// Migration is the outcome of Migrate.
type Migration struct {
	// Migrated is set when the .env file was imported by this call.
	Migrated bool
	// Imported are the keys stored (values differing from the defaults).
	Imported []string
	// Unknown are the keys of the file that are not configuration keys:
	// not imported, still in the renamed file.
	Unknown []string
	// Invalid are the keys whose value is rejected (see Definition.Check):
	// not imported, the default applies.
	Invalid []string
	// Backup is the path the file was renamed to, "" when it could not be.
	Backup string
	// Ignored is set when a .env file exists but the configuration was
	// already migrated: it is not read.
	Ignored bool
}

// Migrate imports the .env file of dataDir into the configuration, once:
// the valid non-empty values differing from the defaults are stored, then
// the file is renamed to .env.migrated as a backup. Unknown keys and
// invalid values are left out.
func Migrate(dataDir string, b MigrationBackend) (Migration, error) {
	var m Migration
	path := filepath.Join(dataDir, FileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	if _, done, err := b.Setting(store.SettingEnvMigrated); err != nil {
		return m, err
	} else if done {
		m.Ignored = true
		return m, nil
	}

	values := map[string]string{}
	for key, value := range Parse(string(data)) {
		def, known := Lookup(key)
		if known {
			value = def.canonical(value)
		}
		switch {
		case !known:
			m.Unknown = append(m.Unknown, key)
		case value == "" || value == def.DefaultValue():
			// The default applies anyway, and follows future releases
		case def.Check(value) != nil:
			m.Invalid = append(m.Invalid, def.Key)
		default:
			values[def.Key] = value
			m.Imported = append(m.Imported, def.Key)
		}
	}
	slices.Sort(m.Imported)
	slices.Sort(m.Unknown)
	slices.Sort(m.Invalid)

	if err := b.SetConfig(values); err != nil {
		return m, fmt.Errorf("migrate %s: %w", path, err)
	}
	if err := b.SetSetting(store.SettingEnvMigrated, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return m, fmt.Errorf("migrate %s: %w", path, err)
	}
	m.Migrated = true

	backup := filepath.Join(dataDir, MigratedFileName)
	if _, err := os.Stat(backup); err == nil {
		backup += "-" + time.Now().Format("20060102150405")
	}
	if err := os.Rename(path, backup); err == nil {
		m.Backup = backup
	}
	// The template of the file, extracted by earlier releases
	_ = os.Remove(filepath.Join(dataDir, exampleFileName))
	return m, nil
}

// Open migrates the .env file of dataDir when needed, then loads the
// configuration of its db.sqlite.
func Open(dataDir string) (*Env, Migration, error) {
	b := store.ConfigDB{Path: filepath.Join(dataDir, "db.sqlite")}
	m, err := Migrate(dataDir, b)
	if err != nil {
		return nil, m, err
	}
	e := New(b)
	return e, m, e.Load()
}
