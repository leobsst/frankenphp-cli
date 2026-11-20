package domains

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/core/caddyfile"
	"github.com/leobsst/frankenphp-cli/internal/core/compose"
	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/randstr"
)

// SiteFile is the Caddyfile of a PHP domain or of a static host, as the
// user may have edited it, and the one its template would give.
type SiteFile struct {
	Host     string `json:"host"`
	Kind     string `json:"kind"` // "php" or "static"
	PHP      string `json:"php,omitempty"`
	Path     string `json:"path"`
	Content  string `json:"content"`
	Template string `json:"template"`
	// Modified tells that Content differs from Template
	Modified bool `json:"modified"`
}

// siteFile locates the site file of host, with what its template gives.
func (s *Service) siteFile(host string) (SiteFile, error) {
	g := s.generator(s.Log)
	f := SiteFile{Host: host}
	php, isDomain, err := s.Store.DomainPHPVersion(host)
	if err != nil {
		return f, err
	}
	if isDomain {
		f.Kind, f.PHP, f.Path = "php", php, g.SitePath(host, php)
		return f, nil
	}
	statics, err := s.Store.StaticHosts()
	if err != nil {
		return f, err
	}
	for _, h := range statics {
		if h.Domain == host {
			f.Kind, f.Path = "static", g.SitePath(host, "")
			f.Template = caddyfile.StaticTemplate(h)
			return f, nil
		}
	}
	return f, i18n.Errorf("'%s' has no site Caddyfile: only PHP domains and static hosts have one.", host)
}

// SiteFile returns the Caddyfile of a PHP domain or static host.
func (s *Service) SiteFile(host string) (SiteFile, error) {
	f, err := s.siteFile(host)
	if err != nil {
		return f, err
	}
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return f, err
	}
	f.Content = string(data)
	if f.Kind == "php" {
		// The template keeps the root the file serves
		if f.Template, err = s.generator(s.Log).PHPTemplate(host, caddyfile.SiteRoot(f.Content)); err != nil {
			return f, err
		}
	}
	f.Modified = strings.TrimSpace(f.Content) != strings.TrimSpace(f.Template)
	return f, nil
}

// ValidateSite checks content as the Caddyfile of host with the Caddy of
// the container serving it (`adapt`: syntax and directives, nothing is
// loaded). The error carries Caddy's message.
func (s *Service) ValidateSite(ctx context.Context, host, content string) error {
	f, err := s.siteFile(host)
	if err != nil {
		return err
	}
	return s.validateSite(ctx, f, content)
}

func (s *Service) validateSite(ctx context.Context, f SiteFile, content string) error {
	container, binary := compose.ReverseProxyContainer, "caddy"
	if f.Kind == "php" {
		container, binary = phpversions.ContainerName(f.PHP), "frankenphp"
	}
	if !s.Docker.ContainerStatuses(ctx).Running(container) {
		return i18n.Errorf("%s is not running: the Caddyfile cannot be checked.", container)
	}
	// Through the log directory, mounted at /var/log in both containers
	name := ".fm-check-" + randstr.New("abcdefghijklmnopqrstuvwxyz0123456789", 12) + "_Caddyfile"
	path := filepath.Join(s.CaddyDir, "log", name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	defer func() { _ = os.Remove(path) }()
	if code, out := s.Docker.Exec(ctx, container, binary, "adapt", "--config", "/var/log/"+name, "--adapter", "caddyfile"); code != 0 {
		return &SiteError{Message: strings.TrimSpace(out)}
	}
	return nil
}

// SiteError is a Caddyfile Caddy refused.
type SiteError struct{ Message string }

func (e *SiteError) Error() string { return i18n.Tf("Invalid Caddyfile: %s", e.Message) }

// SaveSite checks content, writes it as the Caddyfile of host, then
// restarts what serves it.
func (s *Service) SaveSite(ctx context.Context, host, content string) (SiteFile, error) {
	f, err := s.siteFile(host)
	if err != nil {
		return f, err
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	s.Log.Step(i18n.T("Checking the Caddyfile..."))
	if err := s.validateSite(ctx, f, content); err != nil {
		return f, err
	}
	if err := os.WriteFile(f.Path, []byte(content), 0o644); err != nil {
		return f, err
	}
	s.Log.Success(i18n.Tf("Caddyfile of %s saved.", host))
	s.restartSite(ctx, f)
	return s.SiteFile(host)
}

// ResetSite writes the Caddyfile of host from its template again (a PHP
// domain keeps its root), then restarts what serves it.
func (s *Service) ResetSite(ctx context.Context, host string) (SiteFile, error) {
	f, err := s.SiteFile(host)
	if err != nil {
		return f, err
	}
	if err := os.WriteFile(f.Path, []byte(f.Template), 0o644); err != nil {
		return f, err
	}
	s.Log.Success(i18n.Tf("Caddyfile of %s reset to the template.", host))
	s.restartSite(ctx, f)
	return s.SiteFile(host)
}

func (s *Service) restartSite(ctx context.Context, f SiteFile) {
	if f.Kind == "php" {
		s.restartVersion(ctx, f.PHP)
		return
	}
	s.restartProxy(ctx)
}
