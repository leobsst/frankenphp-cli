package caddyfile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

// memLog records the messages.
type memLog struct{ lines []string }

func (l *memLog) Info(msg string)    { l.lines = append(l.lines, "info: "+msg) }
func (l *memLog) Success(msg string) { l.lines = append(l.lines, "success: "+msg) }
func (l *memLog) Warning(msg string) { l.lines = append(l.lines, "warning: "+msg) }

func golden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newCaddyDir returns a data directory whose caddy/ holds the real template.
func newCaddyDir(t *testing.T) (dataDir, caddyDir string) {
	t.Helper()
	dataDir = t.TempDir()
	caddyDir = filepath.Join(dataDir, "caddy")
	write(t, filepath.Join(caddyDir, TemplateName), read(t, "../../../caddy/"+TemplateName))
	return dataDir, caddyDir
}

// The testdata files are the reference outputs of earlier releases: the
// generated Caddyfiles must stay identical to them.
func TestWriteMainMatchesReference(t *testing.T) {
	caddyDir := t.TempDir()
	domains := []store.Domain{{Name: "app.test", PHPVersion: "8.3"}, {Name: "my.shop.test", PHPVersion: "8.5"},
		{Name: "localhost", PHPVersion: "8.2"}}
	aliases := []store.AliasEntry{{Alias: "alias.test", PHPVersion: "8.3", Target: "app.test"}}
	proxies := []store.Proxy{{Domain: "api.test", Target: "127.0.0.1:8006"}, {Domain: "ext.test", Target: "http://host:9000"}}
	main := filepath.Join(caddyDir, MainName)

	// localhost registered: no admin block; no ws certificate: no ws block
	err := WriteMain(caddyDir, Main{Domains: domains, Aliases: aliases, Proxies: proxies,
		DBAdminTarget: "127.0.0.1:8080", WebsocketTarget: "127.0.0.1:6001"})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, main); got != golden(t, "main") {
		t.Errorf("main Caddyfile differs from the reference:\n%s", got)
	}

	write(t, filepath.Join(caddyDir, "certs", soketi.Domain+".pem"), "")
	err = WriteMain(caddyDir, Main{Domains: domains[:2], Aliases: aliases, Proxies: proxies,
		DBAdminTarget: "127.0.0.1:8081", WebsocketTarget: "127.0.0.1:6002"})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, main); got != golden(t, "main_ws") {
		t.Errorf("main Caddyfile with ws differs from the reference:\n%s", got)
	}
}

func TestWriteMainSkipsRegisteredWebsocketHost(t *testing.T) {
	caddyDir := t.TempDir()
	write(t, filepath.Join(caddyDir, "certs", soketi.Domain+".pem"), "")
	err := WriteMain(caddyDir, Main{Proxies: []store.Proxy{{Domain: soketi.Domain, Target: "127.0.0.1:9999"}},
		WebsocketTarget: "127.0.0.1:6001"})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(caddyDir, MainName)); strings.Count(got, soketi.Domain+" {") != 1 ||
		strings.Contains(got, "6001") {
		t.Errorf("ws host served twice:\n%s", got)
	}
}

// https://localhost forwards to the database admin UI, unless there is none
// or the user registered localhost.
func TestWriteMainLocalhostBlock(t *testing.T) {
	for name, tc := range map[string]struct {
		m      Main
		blocks int
		admin  bool
	}{
		"no admin UI":           {Main{}, 0, false},
		"admin UI":              {Main{DBAdminTarget: "127.0.0.1:8080"}, 1, true},
		"localhost as a proxy":  {Main{DBAdminTarget: "127.0.0.1:8080", Proxies: []store.Proxy{{Domain: "localhost", Target: "127.0.0.1:3000"}}}, 1, false},
		"localhost as a domain": {Main{DBAdminTarget: "127.0.0.1:8080", Domains: []store.Domain{{Name: "localhost", PHPVersion: "8.3"}}}, 1, false},
	} {
		caddyDir := t.TempDir()
		if err := WriteMain(caddyDir, tc.m); err != nil {
			t.Fatal(err)
		}
		got := read(t, filepath.Join(caddyDir, MainName))
		if strings.Count(got, "localhost {") != tc.blocks || strings.Contains(got, "127.0.0.1:8080") != tc.admin {
			t.Errorf("%s:\n%s", name, got)
		}
	}
}

func TestWriteMainRejectsUnknownVersion(t *testing.T) {
	if err := WriteMain(t.TempDir(), Main{Domains: []store.Domain{{Name: "a.test", PHPVersion: "7.4"}}}); err == nil {
		t.Error("no error for PHP 7.4")
	}
}

func TestGenerateForVersionFromTemplate(t *testing.T) {
	dataDir, caddyDir := newCaddyDir(t)
	log := &memLog{}
	g := New(caddyDir, dataDir, log)
	if err := g.GenerateForVersion([]string{"my.shop.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(caddyDir, "sites", "php-8.4", "my.shop_Caddyfile")
	if got := read(t, path); got != golden(t, "site") {
		t.Errorf("site Caddyfile differs from the reference:\n%s", got)
	}
	want := []string{"info: Creating my.shop_Caddyfile (PHP 8.4)...", "success: Caddyfile configurations generated"}
	if !slices.Equal(log.lines, want) {
		t.Errorf("log = %v", log.lines)
	}

	// Existing files are left alone
	write(t, path, "edited")
	if err := g.GenerateForVersion([]string{"my.shop.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	if read(t, path) != "edited" {
		t.Error("existing site file overwritten")
	}
}

// The built-in site must use single braces, which Caddy accepts.
func TestFallbackSiteWithoutTemplate(t *testing.T) {
	dir := t.TempDir()
	g := New(filepath.Join(dir, "caddy"), dir, &memLog{})
	if err := g.GenerateForVersion([]string{"app.test"}, "8.3"); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(dir, "caddy", "sites", "php-8.3", "app_Caddyfile"))
	if strings.Contains(got, "{{") || strings.Count(got, "{") != strings.Count(got, "}") ||
		!strings.HasPrefix(got, "http://app.test {\n\troot * /{$CUSTOM_PATH}/app/public/\n") {
		t.Errorf("fallback site:\n%s", got)
	}
}

// The template is read from CADDY_DIR when it has one.
func TestTemplateFollowsCaddyDir(t *testing.T) {
	dataDir := t.TempDir()
	caddyDir := filepath.Join(dataDir, "custom-caddy")
	if g := New(caddyDir, dataDir, nil); g.TemplatePath != filepath.Join(dataDir, "caddy", TemplateName) {
		t.Errorf("without its own template: %s", g.TemplatePath)
	}
	write(t, filepath.Join(caddyDir, TemplateName), "")
	if g := New(caddyDir, dataDir, nil); g.TemplatePath != filepath.Join(caddyDir, TemplateName) {
		t.Errorf("with its own template: %s", g.TemplatePath)
	}
}

func TestMoveArchiveRestore(t *testing.T) {
	caddyDir := t.TempDir()
	sites := filepath.Join(caddyDir, "sites")
	log := &memLog{}
	g := New(caddyDir, caddyDir, log)
	write(t, filepath.Join(sites, "php-8.3", "my.app_Caddyfile"), "http://my.app.test {\n}\n")
	write(t, filepath.Join(sites, "php-8.3", "blog_Caddyfile"), "http://blog.test {\n}\n")

	if err := g.MoveToVersion("my.app.test", "8.3", "8.4"); err != nil {
		t.Fatal(err)
	}
	if err := g.MoveToVersion("blog.test", "8.3", "8.4"); err != nil {
		t.Fatal(err)
	}
	if err := g.MoveToVersion("none.test", "8.3", "8.4"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"my.app_Caddyfile", "blog_Caddyfile"} {
		if !exists(filepath.Join(sites, "php-8.4", name)) {
			t.Errorf("%s not moved", name)
		}
	}

	write(t, filepath.Join(sites, "archive", "my.app_Caddyfile"), "older archive")
	if err := g.Archive([]string{"my.app.test", "blog.test", "none.test"}); err != nil {
		t.Fatal(err)
	}
	archived, err := g.ListArchived()
	if err != nil {
		t.Fatal(err)
	}
	// Only the _Caddyfile suffix is cut: "my.app", not "my"
	want := []Archived{{"blog", "blog.test"}, {"my.app", "my.app.test"}}
	if !slices.Equal(archived, want) {
		t.Errorf("archived = %v", archived)
	}

	restored, err := g.RestoreToVersion([]string{"my.app.test", "blog", "none"}, "8.5")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(restored, []string{"my.app.test", "blog.test"}) {
		t.Errorf("restored = %v", restored)
	}
	if read(t, filepath.Join(sites, "php-8.5", "my.app_Caddyfile")) != "http://my.app.test {\n}\n" {
		t.Error("archive not replaced on archive")
	}

	want2 := []string{
		"info: Moved my.app_Caddyfile from PHP 8.3 to PHP 8.4",
		"info: Moved blog_Caddyfile from PHP 8.3 to PHP 8.4",
		"warning: Caddyfile for none.test not found",
		"info: Archived my.app_Caddyfile",
		"info: Archived blog_Caddyfile",
		"warning: Caddyfile for none.test not found, skipping archive",
		"info: Restored my.app_Caddyfile (PHP 8.5)",
		"info: Restored blog_Caddyfile (PHP 8.5)",
		"warning: Archived Caddyfile for 'none' not found, skipping",
	}
	if !slices.Equal(log.lines, want2) {
		t.Errorf("log =\n%s", strings.Join(log.lines, "\n"))
	}
}

func TestExtractDomain(t *testing.T) {
	dir := t.TempDir()
	for content, want := range map[string]string{
		"https://a.test {\n":    "a.test",
		"  b.test{\n":           "b.test",
		"# comment\nc.test {\n": "",
		"":                      "",
	} {
		p := filepath.Join(dir, "f")
		write(t, p, content)
		if got := extractDomain(p); got != want {
			t.Errorf("extractDomain(%q) = %q, want %q", content, got, want)
		}
	}
	if got := extractDomain(filepath.Join(dir, "missing")); got != "" {
		t.Errorf("missing file: %q", got)
	}
}
