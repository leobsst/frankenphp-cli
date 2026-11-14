// Package env holds the user configuration. It used to live in the .env
// file of the data directory; it is now stored in db.sqlite (table
// configuration) and the .env file is migrated once (see Migrate). The keys
// keep their .env names.
package env

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/randstr"
)

// validDBEngines are the database engines DB_ENGINES may list.
var validDBEngines = []string{"mariadb", "mysql", "postgresql"}

// Backend stores the values set by the user.
type Backend interface {
	Config() (map[string]string, error)
	SetConfig(values map[string]string) error
	DeleteConfig(keys ...string) error
}

// Env is the configuration: the values set by the user, over the defaults
// of the registry.
type Env struct {
	backend Backend
	vars    map[string]string // set by the user
}

// New returns the configuration stored in backend; call Load to read it.
func New(backend Backend) *Env {
	return &Env{backend: backend, vars: map[string]string{}}
}

// FromMap returns a configuration held in memory only (tests, previews).
func FromMap(vars map[string]string) *Env {
	m := MemoryBackend{}
	maps.Copy(m, vars)
	return &Env{backend: m, vars: maps.Clone(m)}
}

// MemoryBackend is a Backend held in memory.
type MemoryBackend map[string]string

// Config returns a copy of the values.
func (m MemoryBackend) Config() (map[string]string, error) { return maps.Clone(m), nil }

// SetConfig stores values.
func (m MemoryBackend) SetConfig(values map[string]string) error {
	maps.Copy(m, values)
	return nil
}

// DeleteConfig removes values.
func (m MemoryBackend) DeleteConfig(keys ...string) error {
	for _, k := range keys {
		delete(m, k)
	}
	return nil
}

// Load reads the stored values, replacing those in memory. A value the
// registry rejects (edited by hand in db.sqlite) is dropped silently: its
// key falls back to the default. A mere case difference ("True") is
// normalized.
func (e *Env) Load() error {
	stored, err := e.backend.Config()
	if err != nil {
		return err
	}
	vars := make(map[string]string, len(stored))
	for key, value := range stored {
		if d, ok := Lookup(key); ok {
			value = d.canonical(value)
			if d.Check(value) != nil {
				continue
			}
		}
		vars[key] = value
	}
	e.vars = vars
	return nil
}

// Parse reads .env content like EnvironmentManager.load: each line is
// trimmed; empty lines, "#" lines and lines without "=" are skipped; the
// line is split on the first "="; the key is trimmed, the value is trimmed
// then stripped of surrounding " and ' characters. A later key wins.
func Parse(content string) map[string]string {
	vars := map[string]string{}
	for _, line := range splitLines(content) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vars[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return vars
}

// splitLines splits s on the line endings a .env file can contain (\n,
// \r\n, \r): no trailing empty line, and no line at all for empty content.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// Get returns a value: the one set by the user, the default of the key
// otherwise, "" for an unknown key.
func (e *Env) Get(key string) string {
	if v, ok := e.vars[key]; ok {
		return v
	}
	return defaultOf(key)
}

// IsSet reports whether the user set key (as opposed to its default).
func (e *Env) IsSet(key string) bool {
	_, ok := e.vars[key]
	return ok
}

// Vars returns a copy of the values set by the user.
func (e *Env) Vars() map[string]string { return maps.Clone(e.vars) }

// Require returns a value, or an error when it is missing or empty.
func (e *Env) Require(key string) (string, error) {
	if v := e.Get(key); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("Required configuration '%s' is not set (frankenmanager configuration set %s <value>)", key, key) //nolint:staticcheck // user-facing message
}

// Set stores a value.
func (e *Env) Set(key, value string) error {
	if err := e.backend.SetConfig(map[string]string{key: value}); err != nil {
		return err
	}
	e.vars[key] = value
	return nil
}

// Reset removes the value set by the user: the key falls back to its
// default.
func (e *Env) Reset(key string) error {
	if err := e.backend.DeleteConfig(key); err != nil {
		return err
	}
	delete(e.vars, key)
	return nil
}

// GeneratePassword returns a random 32-character alphanumeric password.
func GeneratePassword() string { return randstr.New(randstr.Alphanumeric, 32) }

// ExtraPackages merges EXTRA_<type>_PACKAGES (every PHP version) and
// EXTRA_<type>_PACKAGES_<version without dot> (e.g. _84), space-separated,
// without duplicates. packageType is "APT", "APK" or "COMPOSER".
func (e *Env) ExtraPackages(phpVersion, packageType string) string {
	suffix := strings.ReplaceAll(phpVersion, ".", "")
	all := e.Get("EXTRA_"+packageType+"_PACKAGES") + " " + e.Get("EXTRA_"+packageType+"_PACKAGES_"+suffix)

	var packages []string
	for _, pkg := range strings.Fields(all) {
		if !slices.Contains(packages, pkg) {
			packages = append(packages, pkg)
		}
	}
	return strings.Join(packages, " ")
}

// ImageDistro returns PHP_IMAGE_DISTRO ("alpine" by default, or "trixie").
func (e *Env) ImageDistro() (string, error) {
	return phpversions.ResolveDistro(e.Get("PHP_IMAGE_DISTRO"))
}

// ExtraSystemPackages returns the extra packages of the configured
// distribution: EXTRA_APK_PACKAGES* on Alpine, EXTRA_APT_PACKAGES* on
// Trixie. warning is set when APT packages are configured on Alpine, where
// they are ignored.
func (e *Env) ExtraSystemPackages(phpVersion string) (packages, warning string, err error) {
	distro, err := e.ImageDistro()
	if err != nil {
		return "", "", err
	}
	if distro != "alpine" {
		return e.ExtraPackages(phpVersion, "APT"), "", nil
	}
	if e.ExtraPackages(phpVersion, "APT") != "" {
		warning = "EXTRA_APT_PACKAGES is ignored on Alpine images: use EXTRA_APK_PACKAGES, " +
			"or set PHP_IMAGE_DISTRO=trixie."
	}
	return e.ExtraPackages(phpVersion, "APK"), warning, nil
}

// PdftkEnabled reports whether INSTALL_PDFTK is exactly "true".
func (e *Env) PdftkEnabled() bool { return e.Get("INSTALL_PDFTK") == "true" }

// IsProduction reports whether APP_ENV is "prod" or "production".
func (e *Env) IsProduction() bool {
	appEnv := e.Get("APP_ENV")
	return appEnv == "prod" || appEnv == "production"
}

// RedisEnabled reports whether REDIS_ENABLED is "true" (trimmed, any case).
func (e *Env) RedisEnabled() bool {
	return strings.ToLower(strings.TrimSpace(e.Get("REDIS_ENABLED"))) == "true"
}

// DNSServerEnabled reports whether DNS_SERVER is exactly "true".
func (e *Env) DNSServerEnabled() bool { return e.Get("DNS_SERVER") == "true" }

// SoketiEnabled reports whether SOKETI_ENABLED is "true" (trimmed, any case).
func (e *Env) SoketiEnabled() bool { return soketi.Enabled(e) }

// ParseDBEngines parses a comma-separated DB_ENGINES value: trimmed,
// lowercased, unknown engines dropped, order and duplicates kept.
func ParseDBEngines(s string) []string {
	var engines []string
	for _, e := range strings.Split(s, ",") {
		e = strings.ToLower(strings.TrimSpace(e))
		if slices.Contains(validDBEngines, e) {
			engines = append(engines, e)
		}
	}
	return engines
}

// DBEngines returns the enabled database engines, ["mariadb"] when none.
func (e *Env) DBEngines() []string {
	value := e.Get("DB_ENGINES")
	if value == "" {
		value = "mariadb"
	}
	if engines := ParseDBEngines(value); len(engines) > 0 {
		return engines
	}
	return []string{"mariadb"}
}

// DBAdminTarget returns the upstream https://localhost proxies to in
// development: phpMyAdmin whenever MariaDB or MySQL runs, pgAdmin when
// PostgreSQL is the only engine. "" in production or without admin UI.
func (e *Env) DBAdminTarget() string {
	if e.IsProduction() {
		return ""
	}
	engines := e.DBEngines()
	if slices.Contains(engines, "mariadb") || slices.Contains(engines, "mysql") {
		return "127.0.0.1:" + e.getOr("PMA_PORT", "8080")
	}
	if slices.Contains(engines, "postgresql") {
		return "127.0.0.1:" + e.getOr("PGADMIN_PORT", "8081")
	}
	return ""
}

// ResolveDir returns the directory configured by key (default when empty),
// relative to projectDir unless absolute.
func (e *Env) ResolveDir(key, def, projectDir string) string {
	p := e.getOr(key, def)
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(projectDir, p)
}

// CertsDir returns <CADDY_DIR>/certs. Every command uses it: a CERTS_DIR
// variable is ignored.
func (e *Env) CertsDir(projectDir string) string {
	return filepath.Join(e.ResolveDir("CADDY_DIR", "./caddy", projectDir), "certs")
}

// ComposeEnvVars returns every variable the generated compose file expects.
func (e *Env) ComposeEnvVars(projectDir, customPath string, dbEngines []string) (map[string]string, error) {
	localhost := "127.0.0.1:"
	if e.Get("EXPOSE_SERVICES") == "true" {
		localhost = ""
	}
	uid, err := e.Require("UID")
	if err != nil {
		return nil, err
	}
	gid, err := e.Require("GID")
	if err != nil {
		return nil, err
	}

	vars := map[string]string{
		"CUSTOM_PATH":              customPath,
		"UID":                      uid,
		"GID":                      gid,
		"DB_PORT":                  localhost + e.getOr("DB_PORT", "3306") + ":3306",
		"PMA_PORT":                 localhost + e.getOr("PMA_PORT", "8080") + ":80",
		"REDIS_PORT":               localhost + e.getOr("REDIS_PORT", "6379") + ":6379",
		"WEB_HTTP_PORT":            e.getOr("WEB_HTTP_PORT", "80"),
		"WEB_HTTPS_PORT":           e.getOr("WEB_HTTPS_PORT", "443"),
		"MYSQL_MAX_ALLOWED_PACKET": e.getOr("MYSQL_MAX_ALLOWED_PACKET", "512M"),
		"PWD":                      projectDir,
		"CADDY_DIR":                e.ResolveDir("CADDY_DIR", "./caddy", projectDir),
		"DATABASE_DIR":             e.ResolveDir("DATABASE_DIR", "./database", projectDir),
	}
	dbVars, err := e.dbEnvVars(dbEngines, localhost)
	if err != nil {
		return nil, err
	}
	soketiVars, err := soketi.ComposeEnvVars(e, localhost)
	if err != nil {
		return nil, err
	}
	maps.Copy(vars, dbVars)
	maps.Copy(vars, soketiVars)
	return vars, nil
}

// dbEnvVars returns the compose variables of the enabled database engines.
func (e *Env) dbEnvVars(dbEngines []string, localhost string) (map[string]string, error) {
	vars := map[string]string{}
	if slices.Contains(dbEngines, "mariadb") {
		password, err := e.Require("MARIADB_ROOT_PASSWORD")
		if err != nil {
			return nil, err
		}
		vars["MARIADB_ROOT_PASSWORD"] = password
		vars["MARIADB_VERSION"] = e.getOr("MARIADB_VERSION", "10.11.9")
	}
	if slices.Contains(dbEngines, "mysql") {
		password, err := e.Require("MYSQL_ROOT_PASSWORD")
		if err != nil {
			return nil, err
		}
		vars["MYSQL_ROOT_PASSWORD"] = password
		vars["MYSQL_VERSION"] = e.getOr("MYSQL_VERSION", "8.0")
		vars["MYSQL_PORT"] = localhost + e.getOr("MYSQL_PORT", "3307") + ":3306"
	}
	if slices.Contains(dbEngines, "postgresql") {
		password, err := e.Require("POSTGRES_PASSWORD")
		if err != nil {
			return nil, err
		}
		vars["POSTGRES_PASSWORD"] = password
		vars["POSTGRES_USER"] = e.getOr("POSTGRES_USER", "postgres")
		vars["POSTGRES_VERSION"] = e.getOr("POSTGRES_VERSION", "16")
		vars["POSTGRES_PORT"] = localhost + e.getOr("POSTGRES_PORT", "5432") + ":5432"
		vars["PGADMIN_PORT"] = localhost + e.getOr("PGADMIN_PORT", "8081") + ":80"
	}
	return vars, nil
}

// getOr returns a variable, def when missing or empty.
func (e *Env) getOr(key, def string) string {
	if v := e.Get(key); v != "" {
		return v
	}
	return def
}
