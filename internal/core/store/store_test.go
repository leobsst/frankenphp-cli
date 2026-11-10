package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

// must returns v, failing the test (by panicking) on err. It takes no
// *testing.T so that must(s.Domains()) compiles.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// A database created by an early release: domains without
// php_version, none of the later tables.
func TestOpenMigratesOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`CREATE TABLE domains (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT NOT NULL UNIQUE,
		added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	); INSERT INTO domains (domain) VALUES ('old.test')`)
	check(t, err)
	check(t, raw.Close())

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	want := []Domain{{"old.test", "8.3"}}
	if got := must(s.Domains()); !reflect.DeepEqual(got, want) {
		t.Errorf("Domains() = %v, want %v", got, want)
	}
	// php_version is the last column, as after the column migration of
	// earlier releases
	columns := must(queryList(s.db, "SELECT name FROM pragma_table_info('domains')", scanString))
	if !reflect.DeepEqual(columns, []string{"id", "domain", "added_at", "php_version"}) {
		t.Errorf("columns = %v", columns)
	}
	for _, table := range []string{"aliases", "proxies", "settings"} {
		if _, err := s.db.Exec("SELECT * FROM " + table); err != nil {
			t.Errorf("table %s not created: %v", table, err)
		}
	}
}

func TestReopenKeepsData(t *testing.T) {
	s, path := openTemp(t)
	check(t, s.AddDomains([]string{"a.test"}, "8.4"))
	check(t, s.Close())

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	if got := must(s2.DomainNames()); !reflect.DeepEqual(got, []string{"a.test"}) {
		t.Errorf("DomainNames() = %v", got)
	}
}

func TestDomains(t *testing.T) {
	s, _ := openTemp(t)
	check(t, s.AddDomains([]string{"b.test", "a.test"}, "8.4"))
	check(t, s.AddDomains([]string{"a.test", "c.test"}, "8.2")) // a.test kept as is

	want := []Domain{{"b.test", "8.4"}, {"a.test", "8.4"}, {"c.test", "8.2"}}
	if got := must(s.Domains()); !reflect.DeepEqual(got, want) {
		t.Errorf("Domains() = %v, want %v", got, want)
	}
	if got := must(s.ActivePHPVersions()); !reflect.DeepEqual(got, []string{"8.2", "8.4"}) {
		t.Errorf("ActivePHPVersions() = %v", got)
	}

	if v, ok, err := s.DomainPHPVersion("c.test"); err != nil || !ok || v != "8.2" {
		t.Errorf("DomainPHPVersion(c.test) = %q, %v, %v", v, ok, err)
	}
	if _, ok, err := s.DomainPHPVersion("x.test"); err != nil || ok {
		t.Errorf("DomainPHPVersion(x.test) found: %v", err)
	}
	if ok := must(s.UpdateDomainPHPVersion("c.test", "8.5")); !ok {
		t.Error("UpdateDomainPHPVersion(c.test) = false")
	}
	if ok := must(s.UpdateDomainPHPVersion("x.test", "8.5")); ok {
		t.Error("UpdateDomainPHPVersion(x.test) = true")
	}

	check(t, s.RemoveDomains([]string{"b.test", "x.test"}))
	want = []Domain{{"a.test", "8.4"}, {"c.test", "8.5"}}
	if got := must(s.Domains()); !reflect.DeepEqual(got, want) {
		t.Errorf("after removal: %v, want %v", got, want)
	}

	// `start` rewrites the whole table in the given order
	check(t, s.SetDomains([]Domain{{"z.test", "8.3"}, {"a.test", "8.3"}}))
	want = []Domain{{"z.test", "8.3"}, {"a.test", "8.3"}}
	if got := must(s.Domains()); !reflect.DeepEqual(got, want) {
		t.Errorf("after SetDomains: %v, want %v", got, want)
	}
}

func TestAliases(t *testing.T) {
	s, _ := openTemp(t)
	check(t, s.AddDomains([]string{"app.test", "api.test"}, "8.4"))
	check(t, s.AddAliases([]string{"www.app.test", "app.local"}, "app.test"))
	check(t, s.AddAliases([]string{"docs.api.test"}, "api.test"))
	check(t, s.AddAliases([]string{"orphan.test"}, "gone.test"))
	check(t, s.AddAliases([]string{"app.local"}, "api.test")) // ignored, already an alias

	// Aliases of a missing target are skipped; versions follow the target
	_ = must(s.UpdateDomainPHPVersion("app.test", "8.5"))
	wantEntries := []AliasEntry{
		{"www.app.test", "8.5", "app.test"},
		{"app.local", "8.5", "app.test"},
		{"docs.api.test", "8.4", "api.test"},
	}
	if got := must(s.AliasEntries()); !reflect.DeepEqual(got, wantEntries) {
		t.Errorf("AliasEntries() = %v, want %v", got, wantEntries)
	}
	if target, ok, _ := s.AliasTarget("app.local"); !ok || target != "app.test" {
		t.Errorf("AliasTarget(app.local) = %q, %v", target, ok)
	}

	removed := must(s.RemoveAliasesForTargets([]string{"app.test", "nothing.test"}))
	if !reflect.DeepEqual(removed, []string{"www.app.test", "app.local"}) {
		t.Errorf("RemoveAliasesForTargets() = %v", removed)
	}
	if got := must(s.RemoveAliasesForTargets(nil)); got != nil {
		t.Errorf("no target: %v", got)
	}

	check(t, s.RemoveAliases([]string{"orphan.test"}))
	want := []Alias{{"docs.api.test", "api.test"}}
	if got := must(s.Aliases()); !reflect.DeepEqual(got, want) {
		t.Errorf("Aliases() = %v, want %v", got, want)
	}
}

func TestProxies(t *testing.T) {
	s, _ := openTemp(t)
	check(t, s.AddProxies([]string{"pve.test", "nas.test"}, "https://192.168.1.10:8006"))
	check(t, s.AddProxies([]string{"pve.test"}, "127.0.0.1:1")) // ignored

	want := []Proxy{{"pve.test", "https://192.168.1.10:8006"}, {"nas.test", "https://192.168.1.10:8006"}}
	if got := must(s.Proxies()); !reflect.DeepEqual(got, want) {
		t.Errorf("Proxies() = %v, want %v", got, want)
	}
	if target, ok, _ := s.ProxyTarget("nas.test"); !ok || target != "https://192.168.1.10:8006" {
		t.Errorf("ProxyTarget(nas.test) = %q, %v", target, ok)
	}
	check(t, s.RemoveProxies([]string{"pve.test"}))
	if got := must(s.Proxies()); len(got) != 1 || got[0].Domain != "nas.test" {
		t.Errorf("after removal: %v", got)
	}
}

func TestSettingsAndReset(t *testing.T) {
	s, _ := openTemp(t)
	if _, ok, err := s.Setting(SettingProjectPath); err != nil || ok {
		t.Errorf("unset setting found: %v", err)
	}
	check(t, s.SetSetting(SettingProjectPath, "/a"))
	check(t, s.SetSetting(SettingProjectPath, "/b"))
	if v, ok, err := s.Setting(SettingProjectPath); err != nil || !ok || v != "/b" {
		t.Errorf("Setting() = %q, %v, %v", v, ok, err)
	}

	check(t, s.AddDomains([]string{"a.test"}, "8.3"))
	check(t, s.AddAliases([]string{"b.test"}, "a.test"))
	check(t, s.AddProxies([]string{"c.test"}, "127.0.0.1:1"))
	check(t, s.Reset())

	if d, a, p := must(s.Domains()), must(s.Aliases()), must(s.Proxies()); len(d)+len(a)+len(p) != 0 {
		t.Errorf("after Reset: %v %v %v", d, a, p)
	}
	// `reset --db` keeps the settings
	if v, _, _ := s.Setting(SettingProjectPath); v != "/b" {
		t.Errorf("setting lost by Reset: %q", v)
	}
}
