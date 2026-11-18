// Package store persists the server state in db.sqlite. The schema is the
// one earlier releases wrote, so databases created by earlier versions are
// read and written as they are, and stay readable by those versions.
package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure Go driver (CGO_ENABLED=0), registered as "sqlite"

	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// Domain is a domain served by a PHP container.
type Domain struct {
	Name       string
	PHPVersion string
}

// Alias is an alternate host reverse-proxying to an existing domain or
// proxy host.
type Alias struct {
	Alias  string
	Target string
}

// AliasEntry is an alias with what its target serves, resolved when read:
// an alias follows its target's PHP version or upstream changes. Exactly
// one of PHPVersion (target is a domain) and Upstream (target is a proxy
// host) is set.
type AliasEntry struct {
	Alias      string
	PHPVersion string
	Upstream   string
	Target     string
}

// Proxy is a domain reverse-proxying to a raw upstream address.
type Proxy struct {
	Domain string
	Target string
}

// StaticHost is a domain whose files the reverse proxy serves itself, with
// no PHP container: an HTML site or a built single-page app.
type StaticHost struct {
	Domain string
	Root   string // relative to the projects root
	SPA    bool   // unknown paths fall back to /index.html
}

// DNSClient is a device that queried the LAN DNS server.
type DNSClient struct {
	IP         string    `json:"ip"`
	Name       string    `json:"name"` // best effort, "" when unknown
	MAC        string    `json:"mac"`  // best effort, "" when unknown
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
	Queries    int64     `json:"queries"`
	IdentityAt time.Time `json:"-"` // last name/MAC lookup, zero when never
}

// DNSSighting is what the server saw of a client since the last write.
type DNSSighting struct {
	Queries int64
	Last    time.Time
}

// SettingProjectPath stores the projects root given to `start`.
const SettingProjectPath = "project_path"

// SettingS3Buckets caches the S3 bucket names, one per line, for the
// hosts entries and DNS records of their virtual-host style hosts.
const SettingS3Buckets = "s3_buckets"

// SettingEnvMigrated marks a configuration migrated from the .env file.
const SettingEnvMigrated = "env_migrated"

// Store is an open db.sqlite.
type Store struct {
	db *sql.DB
}

// schema creates the tables, as earlier releases define them, when missing.
var schema = []string{
	`CREATE TABLE IF NOT EXISTS domains (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT NOT NULL UNIQUE,
		php_version TEXT NOT NULL DEFAULT '8.3',
		added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE TABLE IF NOT EXISTS aliases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		alias_domain TEXT NOT NULL UNIQUE,
		target_domain TEXT NOT NULL,
		added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE TABLE IF NOT EXISTS proxies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT NOT NULL UNIQUE,
		target TEXT NOT NULL,
		added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`,
	// Unknown to earlier releases, which leave it alone
	`CREATE TABLE IF NOT EXISTS static_hosts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT NOT NULL UNIQUE,
		root TEXT NOT NULL,
		spa INTEGER NOT NULL DEFAULT 0,
		added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`,
	// The devices of the LAN DNS server, times in Unix seconds. Unknown to
	// earlier releases, which leave it alone
	`CREATE TABLE IF NOT EXISTS dns_clients (
		ip TEXT PRIMARY KEY,
		name TEXT NOT NULL DEFAULT '',
		mac TEXT NOT NULL DEFAULT '',
		first_seen INTEGER NOT NULL,
		last_seen INTEGER NOT NULL,
		queries INTEGER NOT NULL DEFAULT 0,
		identity_at INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
	// The user configuration (formerly .env): only the values set by the
	// user, the defaults live in the code
	`CREATE TABLE IF NOT EXISTS configuration (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`,
}

// Open opens path, creating the file and the missing tables.
func Open(path string) (*Store, error) {
	// busy_timeout: wait up to 5 s for another process (e.g. the DNS
	// watcher) instead of failing at once
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, i18n.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.createTables(); err != nil {
		_ = db.Close()
		return nil, i18n.Errorf("open %s: %w", path, err)
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) createTables() error {
	for _, stmt := range schema {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// --- Settings ----------------------------------------------------------------

// Setting returns a stored setting and whether it exists.
func (s *Store) Setting(key string) (string, bool, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// S3Buckets returns the cached S3 bucket names.
func (s *Store) S3Buckets() ([]string, error) {
	v, _, err := s.Setting(SettingS3Buckets)
	if err != nil || v == "" {
		return nil, err
	}
	return strings.Split(v, "\n"), nil
}

// SetS3Buckets caches the S3 bucket names.
func (s *Store) SetS3Buckets(names []string) error {
	return s.SetSetting(SettingS3Buckets, strings.Join(names, "\n"))
}

// SetSetting stores a setting, replacing its value.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec("INSERT INTO settings (key, value) VALUES (?, ?) "+
		"ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

// --- Configuration -----------------------------------------------------------

// Config returns every configuration value set by the user.
func (s *Store) Config() (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, value FROM configuration")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	values := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		values[k] = v
	}
	return values, rows.Err()
}

// SetConfig stores configuration values, replacing existing ones, in one
// transaction.
func (s *Store) SetConfig(values map[string]string) error {
	return s.inTx(func(tx *sql.Tx) error {
		for k, v := range values {
			if _, err := tx.Exec("INSERT INTO configuration (key, value) VALUES (?, ?) "+
				"ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP", k, v); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteConfig removes configuration values: they fall back to their
// default.
func (s *Store) DeleteConfig(keys ...string) error {
	return s.execEach("DELETE FROM configuration WHERE key = ?", keys)
}

// ConfigDB is the configuration of the db.sqlite at Path, opened for each
// operation: no connection to close for the commands that only read it.
type ConfigDB struct{ Path string }

// Config returns the values set by the user.
func (c ConfigDB) Config() (map[string]string, error) {
	var values map[string]string
	return values, c.with(func(s *Store) (err error) { values, err = s.Config(); return err })
}

// SetConfig stores values.
func (c ConfigDB) SetConfig(values map[string]string) error {
	return c.with(func(s *Store) error { return s.SetConfig(values) })
}

// DeleteConfig removes values.
func (c ConfigDB) DeleteConfig(keys ...string) error {
	return c.with(func(s *Store) error { return s.DeleteConfig(keys...) })
}

// Setting returns a stored setting and whether it exists.
func (c ConfigDB) Setting(key string) (value string, ok bool, err error) {
	return value, ok, c.with(func(s *Store) error { value, ok, err = s.Setting(key); return err })
}

// SetSetting stores a setting.
func (c ConfigDB) SetSetting(key, value string) error {
	return c.with(func(s *Store) error { return s.SetSetting(key, value) })
}

func (c ConfigDB) with(fn func(*Store) error) error {
	s, err := Open(c.Path)
	if err != nil {
		return err
	}
	if err := fn(s); err != nil {
		_ = s.Close()
		return err
	}
	return s.Close()
}

// ReadConfigValue returns a configuration value of the db.sqlite at path,
// opened read-only: nothing is created, so it is safe under sudo. ok is
// false when the file, the table or the key is missing.
func ReadConfigValue(path, key string) (value string, ok bool, err error) {
	if _, err := os.Stat(path); err != nil {
		return "", false, nil
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return "", false, err
	}
	defer func() { _ = db.Close() }()
	err = db.QueryRow("SELECT value FROM configuration WHERE key = ?", key).Scan(&value)
	if err != nil {
		// No such row, or a database older than the table
		return "", false, nil
	}
	return value, true, nil
}

// --- Domains -----------------------------------------------------------------

// Domains returns the domains with their PHP version, in insertion order.
func (s *Store) Domains() ([]Domain, error) {
	return queryList(s.db, "SELECT domain, php_version FROM domains ORDER BY id", func(r *sql.Rows) (Domain, error) {
		var d Domain
		return d, r.Scan(&d.Name, &d.PHPVersion)
	})
}

// DomainNames returns the domain names, in insertion order.
func (s *Store) DomainNames() ([]string, error) {
	return queryList(s.db, "SELECT domain FROM domains ORDER BY id", scanString)
}

// SetDomains replaces every domain (`start`). The ids change.
func (s *Store) SetDomains(domains []Domain) error {
	return s.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM domains"); err != nil {
			return err
		}
		for _, d := range domains {
			if _, err := tx.Exec("INSERT INTO domains (domain, php_version) VALUES (?, ?)", d.Name, d.PHPVersion); err != nil {
				return err
			}
		}
		return nil
	})
}

// AddDomains adds domains with phpVersion, ignoring those already present.
func (s *Store) AddDomains(names []string, phpVersion string) error {
	return s.execEach("INSERT OR IGNORE INTO domains (domain, php_version) VALUES (?, ?)", names, phpVersion)
}

// RemoveDomains removes domains. Their aliases are left (see
// RemoveAliasesForTargets).
func (s *Store) RemoveDomains(names []string) error {
	return s.execEach("DELETE FROM domains WHERE domain = ?", names)
}

// DomainPHPVersion returns the PHP version of a domain and whether it exists.
func (s *Store) DomainPHPVersion(name string) (string, bool, error) {
	return s.queryValue("SELECT php_version FROM domains WHERE domain = ?", name)
}

// UpdateDomainPHPVersion changes the PHP version of a domain and reports
// whether the domain exists.
func (s *Store) UpdateDomainPHPVersion(name, phpVersion string) (bool, error) {
	res, err := s.db.Exec("UPDATE domains SET php_version = ? WHERE domain = ?", phpVersion, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ActivePHPVersions returns the distinct PHP versions of the domains, sorted.
func (s *Store) ActivePHPVersions() ([]string, error) {
	return queryList(s.db, "SELECT DISTINCT php_version FROM domains ORDER BY php_version", scanString)
}

// --- Aliases -----------------------------------------------------------------

// Aliases returns the aliases, in insertion order.
func (s *Store) Aliases() ([]Alias, error) {
	return queryList(s.db, "SELECT alias_domain, target_domain FROM aliases ORDER BY id", func(r *sql.Rows) (Alias, error) {
		var a Alias
		return a, r.Scan(&a.Alias, &a.Target)
	})
}

// AliasEntries returns the aliases whose target exists, with the PHP
// version of a target domain or the upstream of a target proxy host, in
// insertion order. Aliases of a missing target are skipped.
func (s *Store) AliasEntries() ([]AliasEntry, error) {
	const q = `SELECT a.alias_domain, COALESCE(d.php_version, ''), COALESCE(p.target, ''), a.target_domain
		FROM aliases a
		LEFT JOIN domains d ON d.domain = a.target_domain
		LEFT JOIN proxies p ON p.domain = a.target_domain
		WHERE d.domain IS NOT NULL OR p.domain IS NOT NULL
		ORDER BY a.id`
	return queryList(s.db, q, func(r *sql.Rows) (AliasEntry, error) {
		var a AliasEntry
		return a, r.Scan(&a.Alias, &a.PHPVersion, &a.Upstream, &a.Target)
	})
}

// AliasTarget returns the target of an alias and whether it is one.
func (s *Store) AliasTarget(alias string) (string, bool, error) {
	return s.queryValue("SELECT target_domain FROM aliases WHERE alias_domain = ?", alias)
}

// AddAliases adds aliases of target, ignoring those already present.
func (s *Store) AddAliases(aliases []string, target string) error {
	return s.execEach("INSERT OR IGNORE INTO aliases (alias_domain, target_domain) VALUES (?, ?)", aliases, target)
}

// RemoveAliases removes aliases.
func (s *Store) RemoveAliases(aliases []string) error {
	return s.execEach("DELETE FROM aliases WHERE alias_domain = ?", aliases)
}

// RemoveAliasesForTargets removes the aliases of the given domains or proxy
// hosts and returns them, in insertion order (cascade of `remove-host`).
func (s *Store) RemoveAliasesForTargets(targets []string) ([]string, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(targets)), ",")
	args := make([]any, len(targets))
	for i, t := range targets {
		args[i] = t
	}

	var removed []string
	err := s.inTx(func(tx *sql.Tx) error {
		var err error
		removed, err = queryList(tx, "SELECT alias_domain FROM aliases WHERE target_domain IN ("+placeholders+") ORDER BY id",
			scanString, args...)
		if err != nil {
			return err
		}
		_, err = tx.Exec("DELETE FROM aliases WHERE target_domain IN ("+placeholders+")", args...)
		return err
	})
	return removed, err
}

// --- Proxies -----------------------------------------------------------------

// Proxies returns the proxy hosts, in insertion order.
func (s *Store) Proxies() ([]Proxy, error) {
	return queryList(s.db, "SELECT domain, target FROM proxies ORDER BY id", func(r *sql.Rows) (Proxy, error) {
		var p Proxy
		return p, r.Scan(&p.Domain, &p.Target)
	})
}

// ProxyTarget returns the upstream of a proxy host and whether it is one.
func (s *Store) ProxyTarget(domain string) (string, bool, error) {
	return s.queryValue("SELECT target FROM proxies WHERE domain = ?", domain)
}

// AddProxies adds proxy hosts to target (stored as given), ignoring those
// already present.
func (s *Store) AddProxies(domains []string, target string) error {
	return s.execEach("INSERT OR IGNORE INTO proxies (domain, target) VALUES (?, ?)", domains, target)
}

// RemoveProxies removes proxy hosts. Their aliases are left (see
// RemoveAliasesForTargets).
func (s *Store) RemoveProxies(domains []string) error {
	return s.execEach("DELETE FROM proxies WHERE domain = ?", domains)
}

// --- Static hosts ------------------------------------------------------------

// StaticHosts returns the static hosts, in insertion order.
func (s *Store) StaticHosts() ([]StaticHost, error) {
	return queryList(s.db, "SELECT domain, root, spa FROM static_hosts ORDER BY id", func(r *sql.Rows) (StaticHost, error) {
		var h StaticHost
		return h, r.Scan(&h.Domain, &h.Root, &h.SPA)
	})
}

// StaticHostNames returns the static host domains, in insertion order.
func (s *Store) StaticHostNames() ([]string, error) {
	return queryList(s.db, "SELECT domain FROM static_hosts ORDER BY id", scanString)
}

// AddStaticHosts adds static hosts, ignoring those already present.
func (s *Store) AddStaticHosts(hosts []StaticHost) error {
	return s.inTx(func(tx *sql.Tx) error {
		for _, h := range hosts {
			if _, err := tx.Exec("INSERT OR IGNORE INTO static_hosts (domain, root, spa) VALUES (?, ?, ?)",
				h.Domain, h.Root, h.SPA); err != nil {
				return err
			}
		}
		return nil
	})
}

// RemoveStaticHosts removes static hosts.
func (s *Store) RemoveStaticHosts(domains []string) error {
	return s.execEach("DELETE FROM static_hosts WHERE domain = ?", domains)
}

// --- DNS clients -------------------------------------------------------------

// RecordDNSClients adds the sightings to their clients, creating the new
// ones, in one transaction.
func (s *Store) RecordDNSClients(sightings map[string]DNSSighting) error {
	if len(sightings) == 0 {
		return nil
	}
	return s.inTx(func(tx *sql.Tx) error {
		for ip, v := range sightings {
			last := v.Last.Unix()
			if _, err := tx.Exec(`INSERT INTO dns_clients (ip, first_seen, last_seen, queries) VALUES (?, ?, ?, ?)
				ON CONFLICT(ip) DO UPDATE SET last_seen = MAX(last_seen, excluded.last_seen),
					queries = queries + excluded.queries`, ip, last, last, v.Queries); err != nil {
				return err
			}
		}
		return nil
	})
}

// DNSClients returns the clients, the most recently seen first.
func (s *Store) DNSClients() ([]DNSClient, error) {
	const q = `SELECT ip, name, mac, first_seen, last_seen, queries, identity_at
		FROM dns_clients ORDER BY last_seen DESC, ip`
	return queryList(s.db, q, func(r *sql.Rows) (DNSClient, error) {
		var c DNSClient
		var first, last, identity int64
		err := r.Scan(&c.IP, &c.Name, &c.MAC, &first, &last, &c.Queries, &identity)
		c.FirstSeen, c.LastSeen = time.Unix(first, 0), time.Unix(last, 0)
		if identity != 0 {
			c.IdentityAt = time.Unix(identity, 0)
		}
		return c, err
	})
}

// SetDNSClientIdentity stores the name and MAC found for a client at at.
func (s *Store) SetDNSClientIdentity(ip, name, mac string, at time.Time) error {
	_, err := s.db.Exec("UPDATE dns_clients SET name = ?, mac = ?, identity_at = ? WHERE ip = ?",
		name, mac, at.Unix(), ip)
	return err
}

// PruneDNSClients removes the clients last seen before before and returns
// how many.
func (s *Store) PruneDNSClients(before time.Time) (int64, error) {
	res, err := s.db.Exec("DELETE FROM dns_clients WHERE last_seen < ?", before.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Reset empties domains, aliases, proxies, static hosts and DNS clients (`reset --db`).
// Settings are kept.
func (s *Store) Reset() error {
	return s.inTx(func(tx *sql.Tx) error {
		for _, table := range []string{"domains", "aliases", "proxies", "static_hosts", "dns_clients"} {
			if _, err := tx.Exec("DELETE FROM " + table); err != nil {
				return err
			}
		}
		return nil
	})
}

// --- Helpers -----------------------------------------------------------------

// queryer is a *sql.DB or a *sql.Tx.
type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func queryList[T any](q queryer, query string, scan func(*sql.Rows) (T, error), args ...any) ([]T, error) {
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func scanString(r *sql.Rows) (string, error) {
	var v string
	return v, r.Scan(&v)
}

func (s *Store) queryValue(query string, arg string) (string, bool, error) {
	values, err := queryList(s.db, query, scanString, arg)
	if err != nil || len(values) == 0 {
		return "", false, err
	}
	return values[0], true, nil
}

// execEach runs stmt once per item, with extra as the following arguments,
// in one transaction.
func (s *Store) execEach(stmt string, items []string, extra ...any) error {
	return s.inTx(func(tx *sql.Tx) error {
		for _, item := range items {
			if _, err := tx.Exec(stmt, append([]any{item}, extra...)...); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) inTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
