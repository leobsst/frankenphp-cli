// Package caddyfile writes the Caddy configuration:
//
//   - one site Caddyfile per domain, in caddy/sites/php-<v>/, served over
//     plain HTTP by the FrankenPHP container of that version. Created only
//     when missing: the user may edit it;
//   - the main caddy/Caddyfile, rewritten every time, where the reverse
//     proxy terminates TLS for every host and forwards to the containers.
package caddyfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

const (
	// TemplateName is the site Caddyfile template, in the Caddy directory.
	TemplateName = "Caddyfile.template"
	// MainName is the reverse proxy Caddyfile, in the Caddy directory.
	MainName = "Caddyfile"

	suffix   = "_Caddyfile"
	fileMode = 0o644
	dirMode  = 0o755
)

var (
	// firstSiteAddress matches the site address at the very start of a file.
	firstSiteAddress = regexp.MustCompile(`^(\S+)\s*\{`)
	// tlsDirective matches the tls lines a site file served over HTTP must
	// not keep.
	tlsDirective = regexp.MustCompile(`(\t)(tls /certs/[^\n]+)`)
	// scheme is stripped from the address read back from a site file.
	scheme = regexp.MustCompile(`^https?://`)
)

// Logger receives the progress messages.
type Logger interface {
	Info(msg string)
	Success(msg string)
	Warning(msg string)
}

// Generator writes the site Caddyfiles of a Caddy directory.
type Generator struct {
	SitesDir     string // <CADDY_DIR>/sites
	TemplatePath string // site template, fallback used when missing
	Log          Logger
}

// New returns a Generator for caddyDir (CADDY_DIR, resolved).
//
// The template is read from <caddyDir>/Caddyfile.template, so that a custom
// CADDY_DIR is honoured, and falls back to <dataDir>/caddy/Caddyfile.template,
// where the resources are extracted, when a custom CADDY_DIR has no template
// of its own.
func New(caddyDir, dataDir string, log Logger) *Generator {
	template := filepath.Join(caddyDir, TemplateName)
	if !exists(template) {
		template = filepath.Join(dataDir, "caddy", TemplateName)
	}
	return &Generator{
		SitesDir:     filepath.Join(caddyDir, "sites"),
		TemplatePath: template,
		Log:          log,
	}
}

// ArchiveDir is where removed domains' site files are kept.
func (g *Generator) ArchiveDir() string { return filepath.Join(g.SitesDir, "archive") }

func (g *Generator) versionDir(phpVersion string) string {
	return filepath.Join(g.SitesDir, "php-"+phpVersion)
}

// SimpleDomain is the domain without its TLD ("my.app.test" -> "my.app"),
// the name of its site file and log file. A domain without a dot is kept.
func SimpleDomain(domain string) string {
	if i := strings.LastIndex(domain, "."); i >= 0 {
		return domain[:i]
	}
	return domain
}

func fileName(domain string) string { return SimpleDomain(domain) + suffix }

// GenerateAllForVersions creates the missing site files of every domain, in
// the directory of its PHP version. Versions are handled in the order they
// first appear.
func (g *Generator) GenerateAllForVersions(domains []store.Domain) error {
	var versions []string
	byVersion := map[string][]string{}
	for _, d := range domains {
		if _, ok := byVersion[d.PHPVersion]; !ok {
			versions = append(versions, d.PHPVersion)
		}
		byVersion[d.PHPVersion] = append(byVersion[d.PHPVersion], d.Name)
	}
	for _, v := range versions {
		if err := g.GenerateForVersion(byVersion[v], v); err != nil {
			return err
		}
	}
	return nil
}

// GenerateForVersion creates the missing site files of domains in
// sites/php-<phpVersion>/, built from the template.
func (g *Generator) GenerateForVersion(domains []string, phpVersion string) error {
	versionDir := g.versionDir(phpVersion)
	if err := os.MkdirAll(versionDir, dirMode); err != nil {
		return err
	}

	for _, domain := range domains {
		simple := SimpleDomain(domain)
		path := filepath.Join(versionDir, simple+suffix)
		if exists(path) {
			continue
		}
		g.Log.Info(fmt.Sprintf("Creating %s%s (PHP %s)...", simple, suffix, phpVersion))
		content, err := g.buildWorkerSite(domain, simple)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), fileMode); err != nil {
			return err
		}
	}

	g.Log.Success("Caddyfile configurations generated")
	return nil
}

// buildWorkerSite returns a new site file: the template with full_domain
// and custom_domain replaced, converted to HTTP. Without a readable
// template, a built-in minimal site is used.
func (g *Generator) buildWorkerSite(domain, simple string) (string, error) {
	data, err := os.ReadFile(g.TemplatePath)
	switch {
	case err == nil:
		content := strings.ReplaceAll(string(data), "full_domain", domain)
		content = strings.ReplaceAll(content, "custom_domain", simple)
		content, _ = toHTTP(content)
		return content, nil
	case errors.Is(err, fs.ErrPermission):
		g.Log.Warning(fmt.Sprintf("Cannot read template at %s (permission denied), using built-in fallback",
			g.TemplatePath))
	case !errors.Is(err, fs.ErrNotExist):
		return "", err
	}
	return fallbackSite(domain, simple), nil
}

// fallbackSite is the site used without template. Its log and php_server
// blocks use single braces: doubled braces would be rejected by Caddy.
func fallbackSite(domain, simple string) string {
	return strings.Join([]string{
		"http://" + domain + " {",
		"\troot * /{$CUSTOM_PATH}/" + simple + "/public/",
		"",
		"\tencode br gzip",
		"",
		"\tlog {",
		"\t\toutput file /var/log/caddy/" + simple + "_access.log",
		"\t\tformat console",
		"\t}",
		"",
		"\tphp_server {",
		"\t\tindex index.php",
		"\t\tresolve_root_symlink",
		"\t}",
		"}",
	}, "\n") + "\n"
}

// toHTTP serves a site over plain HTTP: "http://" before the first site
// address, tls directives commented out (kept for reference). Everything
// else is left untouched.
func toHTTP(content string) (string, bool) {
	out := content
	if loc := firstSiteAddress.FindStringSubmatchIndex(out); loc != nil {
		out = "http://" + out[loc[2]:loc[3]] + " {" + out[loc[1]:]
	}
	out = tlsDirective.ReplaceAllString(out, "${1}# ${2}")
	return out, out != content
}

// MoveToVersion moves a domain's site file from one PHP version directory
// to another, replacing the target.
func (g *Generator) MoveToVersion(domain, fromVersion, toVersion string) error {
	name := fileName(domain)
	dstDir := g.versionDir(toVersion)
	if err := os.MkdirAll(dstDir, dirMode); err != nil {
		return err
	}
	dst := filepath.Join(dstDir, name)

	if src := filepath.Join(g.versionDir(fromVersion), name); exists(src) {
		if err := replace(src, dst); err != nil {
			return err
		}
		g.Log.Info(fmt.Sprintf("Moved %s from PHP %s to PHP %s", name, fromVersion, toVersion))
		return nil
	}
	g.Log.Warning(fmt.Sprintf("Caddyfile for %s not found", domain))
	return nil
}

// Archive moves the site files of domains to sites/archive/, replacing
// older archives. Each file is searched in the PHP version directories.
func (g *Generator) Archive(domains []string) error {
	if err := os.MkdirAll(g.ArchiveDir(), dirMode); err != nil {
		return err
	}
	versionDirs, err := filepath.Glob(filepath.Join(g.SitesDir, "php-*"))
	if err != nil {
		return err
	}
	for _, domain := range domains {
		name := fileName(domain)
		src := ""
		for _, dir := range versionDirs {
			if p := filepath.Join(dir, name); exists(p) {
				src = p
				break
			}
		}
		if src == "" {
			g.Log.Warning(fmt.Sprintf("Caddyfile for %s not found, skipping archive", domain))
			continue
		}
		if err := replace(src, filepath.Join(g.ArchiveDir(), name)); err != nil {
			return err
		}
		g.Log.Info("Archived " + name)
	}
	return nil
}

// Archived is a site file waiting in sites/archive/.
type Archived struct {
	SimpleDomain string
	FullDomain   string // read from the file, <simple>.test when unreadable
}

// ListArchived returns the archived site files, sorted by name.
//
// The simple domain is the file name without the _Caddyfile suffix only:
// "my.app_Caddyfile" gives "my.app", not "my".
func (g *Generator) ListArchived() ([]Archived, error) {
	files, err := filepath.Glob(filepath.Join(g.ArchiveDir(), "*"+suffix))
	if err != nil {
		return nil, err
	}
	archived := make([]Archived, 0, len(files))
	for _, f := range files {
		simple := strings.TrimSuffix(filepath.Base(f), suffix)
		full := extractDomain(f)
		if full == "" {
			full = simple + ".test"
		}
		archived = append(archived, Archived{SimpleDomain: simple, FullDomain: full})
	}
	return archived, nil
}

// RestoreToVersion moves the archived site files of domains (full or
// simple names) to sites/php-<phpVersion>/, replacing the target, and
// returns the full domains restored.
func (g *Generator) RestoreToVersion(domains []string, phpVersion string) ([]string, error) {
	versionDir := g.versionDir(phpVersion)
	if err := os.MkdirAll(versionDir, dirMode); err != nil {
		return nil, err
	}
	var restored []string
	for _, domain := range domains {
		simple := SimpleDomain(domain)
		src := filepath.Join(g.ArchiveDir(), simple+suffix)
		if !exists(src) {
			g.Log.Warning(fmt.Sprintf("Archived Caddyfile for '%s' not found, skipping", domain))
			continue
		}
		full := extractDomain(src)
		if full == "" {
			full = simple + ".test"
		}
		if err := replace(src, filepath.Join(versionDir, simple+suffix)); err != nil {
			return restored, err
		}
		g.Log.Info(fmt.Sprintf("Restored %s%s (PHP %s)", simple, suffix, phpVersion))
		restored = append(restored, full)
	}
	return restored, nil
}

// extractDomain returns the site address of a site file's first line
// ("http://my.app.test {" -> "my.app.test"), "" when there is none.
func extractDomain(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	first, _, _ := strings.Cut(string(data), "\n")
	first = strings.TrimSpace(first)
	address, ok := strings.CutSuffix(first, "{")
	if !ok {
		return ""
	}
	return scheme.ReplaceAllString(strings.TrimSpace(address), "")
}

// replace moves src to dst, removing dst first.
func replace(src, dst string) error {
	if err := os.Remove(dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(src, dst)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Main is what the main Caddyfile serves.
type Main struct {
	Domains []store.Domain     // served by their PHP container
	Aliases []store.AliasEntry // served by their target's container
	Proxies []store.Proxy      // forwarded to a raw upstream
	// DBAdminTarget is the database admin UI https://localhost forwards
	// to, "" for none (see env.DBAdminTarget).
	DBAdminTarget string
	// WebsocketTarget is the Soketi upstream of ws.frankenmanager.test, ""
	// when disabled (see soketi.Target).
	WebsocketTarget string
}

// WriteMain rewrites <caddyDir>/Caddyfile: domains, aliases, proxies, then
// localhost and ws.frankenmanager.test unless already registered. The
// WebSocket block also waits for its certificate, so that Caddy never loads
// a block whose certificate is missing (Soketi turned on while running).
func WriteMain(caddyDir string, m Main) error {
	lines := []string{
		"{",
		"\thttp_port {$WEB_HTTP_PORT:80}",
		"\thttps_port {$WEB_HTTPS_PORT:443}",
		"}",
		"",
	}
	registered := map[string]bool{}
	for _, d := range m.Domains {
		block, err := proxyBlock(d.Name, d.PHPVersion, d.Name)
		if err != nil {
			return err
		}
		lines = append(lines, block...)
		registered[d.Name] = true
	}
	for _, a := range m.Aliases {
		block, err := proxyBlock(a.Alias, a.PHPVersion, a.Target)
		if err != nil {
			return err
		}
		lines = append(lines, block...)
		registered[a.Alias] = true
	}
	for _, p := range m.Proxies {
		lines = append(lines, rawProxyBlock(p.Domain, p.Target)...)
		registered[p.Domain] = true
	}

	if m.DBAdminTarget != "" && !registered["localhost"] {
		// Certificate generated on start by the SSL manager
		lines = append(lines, rawProxyBlock("localhost", m.DBAdminTarget)...)
	}
	wsCert := filepath.Join(caddyDir, "certs", soketi.Domain+".pem")
	if m.WebsocketTarget != "" && !registered[soketi.Domain] && exists(wsCert) {
		lines = append(lines, rawProxyBlock(soketi.Domain, m.WebsocketTarget)...)
	}

	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(filepath.Join(caddyDir, MainName), []byte(content), fileMode)
}

// proxyBlock forwards domain to the container of phpVersion over HTTP,
// with upstreamHost as Host header: the domain itself, or the target of an
// alias so that the container routes it to that site.
func proxyBlock(domain, phpVersion, upstreamHost string) ([]string, error) {
	httpPort, _, err := phpversions.InternalPorts(phpVersion)
	if err != nil {
		return nil, err
	}
	return siteBlock(domain,
		fmt.Sprintf("\treverse_proxy 127.0.0.1:%d {", httpPort),
		"\t\theader_up Host \""+upstreamHost+"\"",
		"\t}",
	), nil
}

// rawProxyBlock forwards domain to target as is (host:port or URL).
func rawProxyBlock(domain, target string) []string {
	return siteBlock(domain, "\treverse_proxy "+target)
}

func siteBlock(domain string, proxy ...string) []string {
	lines := []string{
		domain + " {",
		"\ttls /certs/" + domain + ".pem /certs/" + domain + "-key.pem",
	}
	lines = append(lines, proxy...)
	return append(lines,
		"\tlog {",
		"\t\toutput file /var/log/caddy/"+SimpleDomain(domain)+"_access.log {",
		"\t\t\troll_size 10mb",
		"\t\t\troll_keep 5",
		"\t\t\troll_keep_for 720h",
		"\t\t}",
		"\t\tformat console",
		"\t}",
		"}",
		"",
	)
}
