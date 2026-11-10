package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The schemas of db.sqlite written by earlier releases, copied from git:
// what an existing installation may hold.
var (
	ddlDomainsV1 = `CREATE TABLE domains (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT NOT NULL UNIQUE,
		added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`
	// 4a45ba0: php_version, before added_at on a new database
	ddlDomainsV2 = `CREATE TABLE domains (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT NOT NULL UNIQUE,
		php_version TEXT NOT NULL DEFAULT '8.3',
		added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`
	// ... and last after the migration of a V1 database
	ddlMigrateV1 = `ALTER TABLE domains ADD COLUMN php_version TEXT NOT NULL DEFAULT '8.3'`
	// 69b5422
	ddlAliases = `CREATE TABLE aliases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		alias_domain TEXT NOT NULL UNIQUE,
		target_domain TEXT NOT NULL,
		added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`
	// 4ab90e8
	ddlProxies = `CREATE TABLE proxies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT NOT NULL UNIQUE,
		target TEXT NOT NULL,
		added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`
	// 5112757
	ddlSettings = `CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`
)

// columns are those of the latest schema of earlier releases: they open a
// database migrated by this version without changing it.
var columns = map[string][]string{
	"aliases":  {"id", "alias_domain", "target_domain", "added_at"},
	"proxies":  {"id", "domain", "target", "added_at"},
	"settings": {"key", "value"},
}

func TestOpenEveryEarlierSchema(t *testing.T) {
	for _, tc := range []struct {
		name string
		ddl  []string
		data []string
	}{
		{"v1 domains only", []string{ddlDomainsV1},
			[]string{`INSERT INTO domains (domain) VALUES ('a.test'), ('b.test')`}},
		{"v2 php_version", []string{ddlDomainsV2},
			[]string{`INSERT INTO domains (domain, php_version) VALUES ('a.test', '8.4'), ('b.test', '8.3')`}},
		{"v1 migrated by an earlier release", []string{ddlDomainsV1, ddlMigrateV1},
			[]string{`INSERT INTO domains (domain, php_version) VALUES ('a.test', '8.4'), ('b.test', '8.3')`}},
		// The shape of a real db.sqlite.bak: migrated, then aliases
		{"v3 aliases", []string{ddlDomainsV1, ddlMigrateV1, ddlAliases},
			[]string{`INSERT INTO domains (domain, php_version) VALUES ('a.test', '8.4'), ('b.test', '8.3')`,
				`INSERT INTO aliases (alias_domain, target_domain) VALUES ('www.a.test', 'a.test')`}},
		{"v4 proxies", []string{ddlDomainsV2, ddlAliases, ddlProxies},
			[]string{`INSERT INTO domains (domain, php_version) VALUES ('a.test', '8.4'), ('b.test', '8.3')`,
				`INSERT INTO aliases (alias_domain, target_domain) VALUES ('www.a.test', 'a.test')`,
				`INSERT INTO proxies (domain, target) VALUES ('vite.test', '127.0.0.1:5173')`}},
		{"v5 settings", []string{ddlDomainsV2, ddlAliases, ddlProxies, ddlSettings},
			[]string{`INSERT INTO domains (domain, php_version) VALUES ('a.test', '8.4'), ('b.test', '8.3')`,
				`INSERT INTO settings (key, value) VALUES ('project_path', '/projects')`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "db.sqlite")
			raw, err := sql.Open("sqlite", path)
			check(t, err)
			for _, stmt := range append(tc.ddl, tc.data...) {
				if _, err := raw.Exec(stmt); err != nil {
					t.Fatalf("%s: %v", stmt, err)
				}
			}
			check(t, raw.Close())

			// Opened twice: the second open changes nothing
			for range 2 {
				s, err := Open(path)
				if err != nil {
					t.Fatal(err)
				}
				domains := must(s.Domains())
				wantDomains := []Domain{{"a.test", "8.4"}, {"b.test", "8.3"}}
				if strings.HasPrefix(tc.name, "v1 domains") {
					wantDomains = []Domain{{"a.test", "8.3"}, {"b.test", "8.3"}}
				}
				if !reflect.DeepEqual(domains, wantDomains) {
					t.Errorf("domains = %v", domains)
				}
				for table, want := range columns {
					if got := must(queryList(s.db, "SELECT name FROM pragma_table_info('"+table+"')", scanString)); !reflect.DeepEqual(got, want) {
						t.Errorf("%s columns = %v, want %v", table, got, want)
					}
				}
				hasAlias := strings.Contains(strings.Join(tc.data, ";"), "www.a.test")
				if aliases := must(s.AliasEntries()); hasAlias != (len(aliases) == 1) {
					t.Errorf("aliases = %v", aliases)
				}
				if strings.HasPrefix(tc.name, "v5") {
					if p, _, _ := s.Setting(SettingProjectPath); p != "/projects" {
						t.Errorf("project path = %q", p)
					}
				}
				// Writes work on every schema
				check(t, s.AddDomains([]string{"c.test"}, "8.5"))
				check(t, s.RemoveDomains([]string{"c.test"}))
				check(t, s.Close())
			}
		})
	}
}
