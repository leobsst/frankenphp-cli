// Package caddyfile writes the Caddy configuration:
//
//   - one site Caddyfile per domain, in caddy/sites/php-<v>/, served over
//     plain HTTP by the FrankenPHP container of that version. Created only
//     when missing: the user may edit it;
//   - one site Caddyfile per static host, in caddy/sites/static/, served
//     over HTTPS by the reverse proxy itself (no PHP container). Created
//     only when missing too;
//   - the main caddy/Caddyfile, rewritten every time, where the reverse
//     proxy terminates TLS for every host, forwards to the containers and
//     imports the static sites.
package caddyfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

const (
	// TemplateName is the site Caddyfile template, in the Caddy directory.
	TemplateName = "Caddyfile.template"
	// MainName is the reverse proxy Caddyfile, in the Caddy directory.
	MainName = "Caddyfile"

	// StaticDirName is the directory of the static site files, in
	// <caddyDir>/sites/ and in the reverse proxy container.
	StaticDirName = "static"
	// StaticMount is where the reverse proxy sees the static site files.
	StaticMount = "/etc/caddy/sites/" + StaticDirName

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
	return g.GenerateForVersionRoot(domains, phpVersion, "")
}

// GenerateForVersionRoot is GenerateForVersion with the root of the new
// site files set to root, relative to the projects root ("" keeps the
// template's). An existing site file is kept as is, root included.
func (g *Generator) GenerateForVersionRoot(domains []string, phpVersion, root string) error {
	versionDir := g.versionDir(phpVersion)
	if err := os.MkdirAll(versionDir, dirMode); err != nil {
		return err
	}

	for _, domain := range domains {
		simple := SimpleDomain(domain)
		path := filepath.Join(versionDir, simple+suffix)
		if exists(path) {
			if root != "" {
				g.Log.Warning(i18n.Tf("%s already exists: its root is kept, --root is ignored.", path))
			}
			continue
		}
		g.Log.Info(i18n.Tf("Creating %s%s (PHP %s)...", simple, suffix, phpVersion))
		content, err := g.buildWorkerSite(domain, simple)
		if err != nil {
			return err
		}
		if root != "" {
			content = withRoot(content, root)
		}
		if err := os.WriteFile(path, []byte(content), fileMode); err != nil {
			return err
		}
	}

	g.Log.Success(i18n.T("Caddyfile configurations generated"))
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
		g.Log.Warning(i18n.Tf("Cannot read template at %s (permission denied), using built-in fallback",
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

// rootDirective matches the root line of a site file.
var rootDirective = regexp.MustCompile(`(?m)^(\s*root \* )\S+`)

// withRoot sets the first root directive of a site file to root, relative
// to the projects root.
func withRoot(content, root string) string {
	loc := rootDirective.FindStringSubmatchIndex(content)
	if loc == nil {
		return content
	}
	return content[:loc[3]] + RootPath(root) + content[loc[1]:]
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
		g.Log.Info(i18n.Tf("Moved %s from PHP %s to PHP %s", name, fromVersion, toVersion))
		return nil
	}
	g.Log.Warning(i18n.Tf("Caddyfile for %s not found", domain))
	return nil
}

// Archive moves the site files of domains to sites/archive/php-<v>/, v
// being the PHP version directory they were found in, replacing older
// archives.
func (g *Generator) Archive(domains []string) error {
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
			g.Log.Warning(i18n.Tf("Caddyfile for %s not found, skipping archive", domain))
			continue
		}
		dst := filepath.Join(g.ArchiveDir(), filepath.Base(filepath.Dir(src)))
		if err := os.MkdirAll(dst, dirMode); err != nil {
			return err
		}
		if err := replace(src, filepath.Join(dst, name)); err != nil {
			return err
		}
		g.Log.Info(i18n.Tf("Archived %s", name))
	}
	return nil
}

// Archived kinds.
const (
	ArchivedPHP    = "php"
	ArchivedStatic = "static"
)

// Archived is a site file waiting in sites/archive/php-<v>/ or
// sites/archive/static/.
type Archived struct {
	SimpleDomain string
	FullDomain   string // read from the file, <simple>.test when unreadable
	Kind         string // ArchivedPHP or ArchivedStatic
	PHPVersion   string // the version it was served with, PHP only
}

// ListArchived returns the archived site files, PHP ones first (by
// version, then name), then static ones. The files left at the root of
// sites/archive/ by earlier releases are not listed.
//
// The simple domain is the file name without the _Caddyfile suffix only:
// "my.app_Caddyfile" gives "my.app", not "my".
func (g *Generator) ListArchived() ([]Archived, error) {
	php, err := filepath.Glob(filepath.Join(g.ArchiveDir(), "php-*", "*"+suffix))
	if err != nil {
		return nil, err
	}
	static, err := filepath.Glob(filepath.Join(g.StaticArchiveDir(), "*"+suffix))
	if err != nil {
		return nil, err
	}
	archived := make([]Archived, 0, len(php)+len(static))
	for _, f := range append(php, static...) {
		a := Archived{SimpleDomain: strings.TrimSuffix(filepath.Base(f), suffix), Kind: ArchivedStatic}
		if dir := filepath.Base(filepath.Dir(f)); dir != StaticDirName {
			a.Kind, a.PHPVersion = ArchivedPHP, strings.TrimPrefix(dir, "php-")
		}
		if a.FullDomain = extractDomain(f); a.FullDomain == "" {
			a.FullDomain = a.SimpleDomain + ".test"
		}
		archived = append(archived, a)
	}
	return archived, nil
}

// archivedPHP returns the archived PHP site file of a simple domain, the
// most recent one when several versions keep one, "" when none.
func (g *Generator) archivedPHP(simple string) string {
	files, _ := filepath.Glob(filepath.Join(g.ArchiveDir(), "php-*", simple+suffix))
	best, bestTime := "", time.Time{}
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && (best == "" || info.ModTime().After(bestTime)) {
			best, bestTime = f, info.ModTime()
		}
	}
	return best
}

// RestoreToVersion moves the archived PHP site files of domains (full or
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
		src := g.archivedPHP(simple)
		if src == "" {
			g.Log.Warning(i18n.Tf("Archived Caddyfile for '%s' not found, skipping", domain))
			continue
		}
		full := extractDomain(src)
		if full == "" {
			full = simple + ".test"
		}
		if err := replace(src, filepath.Join(versionDir, simple+suffix)); err != nil {
			return restored, err
		}
		g.Log.Info(i18n.Tf("Restored %s%s (PHP %s)", simple, suffix, phpVersion))
		restored = append(restored, full)
	}
	return restored, nil
}

var (
	// staticRoot matches the root of a static site file.
	staticRoot = regexp.MustCompile(`(?m)^[ \t]*root \* /\{\$CUSTOM_PATH\}/(.*?)/?[ \t]*$`)
	// staticSPA matches the active fallback of a single-page app.
	staticSPA = regexp.MustCompile(`(?m)^[ \t]*try_files \{path\} /index\.html`)
)

// ArchivedStatic returns the static hosts of the archived static site files
// of domains (full or simple names), root and fallback read from their
// file, warning about the missing ones.
func (g *Generator) ArchivedStatic(domains []string) []store.StaticHost {
	hosts, _ := g.restoreStatic(domains, false)
	return hosts
}

// RestoreStatic moves the archived static site files of domains (full or
// simple names) back to sites/static/, replacing the target, and returns
// the static hosts restored: root and fallback read from their file.
func (g *Generator) RestoreStatic(domains []string) ([]store.StaticHost, error) {
	return g.restoreStatic(domains, true)
}

func (g *Generator) restoreStatic(domains []string, move bool) ([]store.StaticHost, error) {
	if move {
		if err := os.MkdirAll(g.StaticDir(), dirMode); err != nil {
			return nil, err
		}
	}
	var restored []store.StaticHost
	for _, domain := range domains {
		simple := SimpleDomain(domain)
		src := filepath.Join(g.StaticArchiveDir(), simple+suffix)
		data, err := os.ReadFile(src)
		if err != nil {
			if !move {
				g.Log.Warning(i18n.Tf("Archived Caddyfile for '%s' not found, skipping", domain))
			}
			continue
		}
		h := store.StaticHost{Domain: extractDomain(src), SPA: staticSPA.Match(data)}
		if h.Domain == "" {
			h.Domain = simple + ".test"
		}
		if m := staticRoot.FindSubmatch(data); m != nil {
			h.Root = string(m[1])
		}
		if h.Root == "" {
			h.Root = "."
		}
		if move {
			if err := replace(src, filepath.Join(g.StaticDir(), simple+suffix)); err != nil {
				return restored, err
			}
			g.Log.Info(i18n.Tf("Restored %s%s (static)", simple, suffix))
		}
		restored = append(restored, h)
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

// StaticDir is the directory of the static site files.
func (g *Generator) StaticDir() string { return filepath.Join(g.SitesDir, StaticDirName) }

// StaticArchiveDir is where removed static hosts' site files are kept,
// apart from the PHP ones that restore-host brings back.
func (g *Generator) StaticArchiveDir() string { return filepath.Join(g.ArchiveDir(), StaticDirName) }

// GenerateStatic creates the missing site files of static hosts in
// sites/static/.
func (g *Generator) GenerateStatic(hosts []store.StaticHost) error {
	if len(hosts) == 0 {
		return nil
	}
	if err := os.MkdirAll(g.StaticDir(), dirMode); err != nil {
		return err
	}
	for _, h := range hosts {
		path := filepath.Join(g.StaticDir(), fileName(h.Domain))
		if exists(path) {
			if err := g.dropBrotli(path); err != nil {
				return err
			}
			continue
		}
		g.Log.Info(i18n.Tf("Creating %s (static)...", fileName(h.Domain)))
		if err := os.WriteFile(path, []byte(staticSite(h)), fileMode); err != nil {
			return err
		}
	}
	g.Log.Success(i18n.T("Caddyfile configurations generated"))
	return nil
}

// brotliEncoder matches the br format of an encode directive.
var brotliEncoder = regexp.MustCompile(`(?m)^([ \t]*encode\b[^\n{]*?)[ \t]+br\b`)

// dropBrotli removes the br format from the encode directives of an
// existing static site file. The reverse proxy runs the standard Caddy
// image, which has no Brotli encoder: Caddy would refuse to start, and the
// container would restart forever. Files written by older versions had it.
func (g *Generator) dropBrotli(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fixed := brotliEncoder.ReplaceAllString(string(data), "${1}")
	if fixed == string(data) {
		return nil
	}
	g.Log.Info(i18n.Tf("Removing the unsupported br encoding from %s...", filepath.Base(path)))
	return os.WriteFile(path, []byte(fixed), fileMode)
}

// ArchiveStatic moves the site files of static hosts to
// sites/archive/static/, replacing older archives.
func (g *Generator) ArchiveStatic(domains []string) error {
	if err := os.MkdirAll(g.StaticArchiveDir(), dirMode); err != nil {
		return err
	}
	for _, domain := range domains {
		name := fileName(domain)
		src := filepath.Join(g.StaticDir(), name)
		if !exists(src) {
			g.Log.Warning(i18n.Tf("Caddyfile for %s not found, skipping archive", domain))
			continue
		}
		if err := replace(src, filepath.Join(g.StaticArchiveDir(), name)); err != nil {
			return err
		}
		g.Log.Info(i18n.Tf("Archived %s", name))
	}
	return nil
}

// RootPath is the container path of a site root: the projects
// root (CUSTOM_PATH, mounted at the same path) joined with root.
func RootPath(root string) string {
	root = strings.Trim(filepath.ToSlash(root), "/")
	if root == "" || root == "." {
		return "/{$CUSTOM_PATH}/"
	}
	return "/{$CUSTOM_PATH}/" + root + "/"
}

// staticSite is the site file of a static host, served over HTTPS by the
// reverse proxy. A non-SPA site keeps the fallback commented out. No br
// encoding: the reverse proxy's standard Caddy image has no Brotli encoder.
func staticSite(h store.StaticHost) string {
	simple := SimpleDomain(h.Domain)
	lines := []string{
		h.Domain + " {",
		"\ttls /certs/" + h.Domain + ".pem /certs/" + h.Domain + "-key.pem",
		"",
		"\troot * " + RootPath(h.Root),
		"",
		"\tencode zstd gzip",
		"",
		"\tlog {",
		"\t\toutput file /var/log/caddy/" + simple + "_access.log {",
		"\t\t\troll_size 10mb",
		"\t\t\troll_keep 5",
		"\t\t\troll_keep_for 720h",
		"\t\t}",
		"\t\tformat console",
		"\t}",
		"",
		"\theader {",
		"\t\tX-Frame-Options \"SAMEORIGIN\"",
		"\t\tReferrer-Policy \"no-referrer\"",
		"\t\t-Server",
		"\t\t-X-Powered-By",
		"\t}",
		"",
	}
	if h.SPA {
		lines = append(lines,
			"\t# The entry page is never cached: it references the built assets",
			"\t@html path / /index.html",
			"\theader @html Cache-Control \"no-cache\"",
			"",
			"\t# Single-page app: unknown paths fall back to /index.html",
			"\ttry_files {path} /index.html",
		)
	} else {
		lines = append(lines,
			"\t# Single-page app: uncomment to fall back to /index.html",
			"\t# try_files {path} /index.html",
		)
	}
	return strings.Join(append(lines, "\tfile_server", "}"), "\n") + "\n"
}

// HasStaticSites reports whether caddyDir holds static site files: the
// reverse proxy then mounts them and the projects root.
func HasStaticSites(caddyDir string) bool {
	files, _ := filepath.Glob(filepath.Join(caddyDir, "sites", StaticDirName, "*"+suffix))
	return len(files) > 0
}

// Main is what the main Caddyfile serves.
type Main struct {
	Domains []store.Domain     // served by their PHP container
	Aliases []store.AliasEntry // served by their target's container or upstream
	Proxies []store.Proxy      // forwarded to a raw upstream
	// Statics are imported from their site file, skipped when it is
	// missing: Caddy would refuse to start
	Statics []store.StaticHost
	// DBAdminTarget is the database admin UI https://localhost forwards
	// to, "" for none (see env.DBAdminTarget).
	DBAdminTarget string
	// WebsocketTarget is the Soketi upstream of ws.frankenmanager.test, ""
	// when disabled (see soketi.Target).
	WebsocketTarget string
	// S3Target is the ACL gateway, upstream of s3.frankenmanager.test and
	// its bucket hosts, "" when disabled (see s3.ProxyTarget).
	S3Target string
}

// WriteMain rewrites <caddyDir>/Caddyfile: domains, aliases, proxies, static
// hosts, then
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
		if a.Upstream != "" {
			// Alias of a proxy host: its upstream, seeing the target host
			lines = append(lines, rawProxyBlock(a.Alias, a.Upstream,
				"\t\theader_up Host \""+a.Target+"\"")...)
		} else {
			block, err := proxyBlock(a.Alias, a.PHPVersion, a.Target)
			if err != nil {
				return err
			}
			lines = append(lines, block...)
		}
		registered[a.Alias] = true
	}
	for _, p := range m.Proxies {
		if p.NoWebsocket {
			lines = append(lines, noWebsocketBlock(p.Domain, p.Target)...)
		} else {
			lines = append(lines, rawProxyBlock(p.Domain, p.Target)...)
		}
		registered[p.Domain] = true
	}

	for _, h := range m.Statics {
		name := fileName(h.Domain)
		if registered[h.Domain] || !exists(filepath.Join(caddyDir, "sites", StaticDirName, name)) {
			continue
		}
		lines = append(lines, "import "+StaticMount+"/"+name, "")
		registered[h.Domain] = true
	}

	if m.DBAdminTarget != "" && !registered["localhost"] {
		// Certificate generated on start by the SSL manager
		lines = append(lines, rawProxyBlock("localhost", m.DBAdminTarget)...)
	}
	wsCert := filepath.Join(caddyDir, "certs", soketi.Domain+".pem")
	if m.WebsocketTarget != "" && !registered[soketi.Domain] && exists(wsCert) {
		lines = append(lines, rawProxyBlock(soketi.Domain, m.WebsocketTarget)...)
	}
	s3Cert := filepath.Join(caddyDir, "certs", s3.Domain+".pem")
	if m.S3Target != "" && !registered[s3.Domain] && exists(s3Cert) {
		lines = append(lines, s3Block(m.S3Target)...)
	}

	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(filepath.Join(caddyDir, MainName), []byte(content), fileMode)
}

// s3Block serves the S3 API at s3.frankenmanager.test (path style) and
// <bucket>.s3.frankenmanager.test (virtual-host style) through the ACL
// gateway, which also answers the anonymous reads of the public objects.
func s3Block(target string) []string {
	domain := s3.Domain
	return []string{
		domain + ", *." + domain + " {",
		"\ttls /certs/" + domain + ".pem /certs/" + domain + "-key.pem",
		"\treverse_proxy " + target,
		"\tlog {",
		"\t\toutput file /var/log/caddy/" + SimpleDomain(domain) + "_access.log {",
		"\t\t\troll_size 10mb",
		"\t\t\troll_keep 5",
		"\t\t\troll_keep_for 720h",
		"\t\t}",
		"\t\tformat console",
		"\t}",
		"}",
		"",
	}
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

// rawProxyBlock forwards domain to target as is (host:port or URL), with
// the given reverse_proxy subdirectives if any.
func rawProxyBlock(domain, target string, directives ...string) []string {
	if len(directives) == 0 {
		return siteBlock(domain, "\treverse_proxy "+target)
	}
	proxy := append([]string{"\treverse_proxy " + target + " {"}, directives...)
	return siteBlock(domain, append(proxy, "\t}")...)
}

// noWebsocketBlock forwards domain to target, refusing the WebSocket
// connections (an Upgrade request answers 403). Caddy forwards them
// otherwise.
func noWebsocketBlock(domain, target string) []string {
	return siteBlock(domain,
		"\t@websocket {",
		"\t\theader Connection *Upgrade*",
		"\t\theader Upgrade websocket",
		"\t}",
		"\trespond @websocket \"WebSocket connections are disabled for this host\" 403",
		"\treverse_proxy "+target,
	)
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

// SitePath returns the site file of a PHP domain (phpVersion set) or of a
// static host.
func (g *Generator) SitePath(domain, phpVersion string) string {
	if phpVersion == "" {
		return filepath.Join(g.StaticDir(), fileName(domain))
	}
	return filepath.Join(g.versionDir(phpVersion), fileName(domain))
}

// PHPTemplate returns the site file a PHP domain gets from the template,
// with root (relative to the projects root, "" for the template's).
func (g *Generator) PHPTemplate(domain, root string) (string, error) {
	content, err := g.buildWorkerSite(domain, SimpleDomain(domain))
	if err != nil || root == "" {
		return content, err
	}
	return withRoot(content, root), nil
}

// StaticTemplate returns the site file a static host gets.
func StaticTemplate(h store.StaticHost) string { return staticSite(h) }

// SiteRoot returns the root of a site file relative to the projects root,
// "" when it has none under it.
func SiteRoot(content string) string {
	m := rootDirective.FindStringSubmatchIndex(content)
	if m == nil {
		return ""
	}
	root, ok := strings.CutPrefix(content[m[3]:m[1]], "/{$CUSTOM_PATH}/")
	if !ok {
		return ""
	}
	return strings.TrimSuffix(root, "/")
}
