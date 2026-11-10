// Package ssl generates the site certificates with mkcert.
package ssl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/command"
)

const (
	// maxAge is the age under which a certificate is kept.
	maxAge = 30 * 24 * time.Hour
	// certMode is the mode of every file of the certs directory.
	certMode = 0o750

	setupHint = "Run `sudo frankenmanager setup --install-mkcert` first."
)

// Error is a certificate failure.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// Logger receives the progress messages.
type Logger interface {
	Info(msg string)
	Success(msg string)
}

// Manager writes certificates into a certs directory.
type Manager struct {
	Dir string // <CADDY_DIR>/certs, mounted as /certs in the containers
	Run command.Runner
	Log Logger
	Now func() time.Time
}

// New returns a Manager for certsDir, which it creates.
func New(certsDir string, log Logger) (*Manager, error) {
	if err := os.MkdirAll(certsDir, 0o755); err != nil {
		return nil, err
	}
	return &Manager{Dir: certsDir, Run: command.Exec{}, Log: log, Now: time.Now}, nil
}

func (m *Manager) mkcert() (string, error) {
	p := m.Run.LookPath("mkcert")
	if p == "" {
		return "", &Error{"mkcert not found. Please install mkcert first."}
	}
	return p, nil
}

// InstallCA copies the root CA installed by `setup` into the certs
// directory: the containers trust it on startup, so that PHP calls between
// local domains validate too.
func (m *Manager) InstallCA(ctx context.Context) error {
	mkcert, err := m.mkcert()
	if err != nil {
		return err
	}
	res, err := m.Run.Run(ctx, command.Cmd{Name: mkcert, Args: []string{"-CAROOT"}})
	if err != nil || !res.OK() {
		return &Error{"Could not determine mkcert's CA root. " + setupHint}
	}
	caRoot := strings.TrimSpace(string(res.Stdout))
	cert, key := filepath.Join(caRoot, "rootCA.pem"), filepath.Join(caRoot, "rootCA-key.pem")
	if !isFile(cert) || !isFile(key) {
		return &Error{"No root CA installed yet. " + setupHint}
	}
	if f, err := os.Open(key); err != nil {
		// Typically left owned by root by an older `sudo frankenmanager setup`
		return &Error{fmt.Sprintf("Cannot read the root CA key %s (permission denied). "+
			"Run `sudo frankenmanager setup --install-mkcert` again to fix its "+
			`ownership, or: sudo chown -R "$(whoami)" "%s"`, key, caRoot)}
	} else {
		_ = f.Close()
	}
	data, err := os.ReadFile(cert)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.Dir, "rootCA.pem"), data, 0o644)
}

// generate runs mkcert for domain into <domain>.pem / <domain>-key.pem.
func (m *Manager) generate(ctx context.Context, mkcert, domain string) (command.Result, error) {
	return m.Run.Run(ctx, command.Cmd{Name: mkcert, Args: []string{
		"-cert-file", filepath.Join(m.Dir, domain+".pem"),
		"-key-file", filepath.Join(m.Dir, domain+"-key.pem"),
		domain,
	}})
}

// GenerateLocalhost (re)generates the localhost certificate.
func (m *Manager) GenerateLocalhost(ctx context.Context) error {
	mkcert, err := m.mkcert()
	if err != nil {
		return err
	}
	res, err := m.generate(ctx, mkcert, "localhost")
	if err != nil || !res.OK() {
		return &Error{"Failed to generate localhost certificate: " + failure(res, err)}
	}
	return nil
}

// GenerateDomain generates the certificate of domain, unless one younger
// than 30 days exists and force is off. It reports whether it generated.
func (m *Manager) GenerateDomain(ctx context.Context, domain string, force bool) (bool, error) {
	mkcert, err := m.mkcert()
	if err != nil {
		return false, err
	}
	if !force && isFile(filepath.Join(m.Dir, domain+"-key.pem")) {
		if age, ok := m.age(domain); ok && age < maxAge {
			return false, nil
		}
	}
	res, err := m.generate(ctx, mkcert, domain)
	if err != nil || !res.OK() {
		return false, &Error{fmt.Sprintf("Failed to generate certificate for %s: %s", domain, failure(res, err))}
	}
	return true, nil
}

func failure(res command.Result, err error) string {
	if err != nil {
		return err.Error()
	}
	return string(res.Stderr)
}

// age returns the age of a domain's certificate (file modification time).
func (m *Manager) age(domain string) (time.Duration, bool) {
	info, err := os.Stat(filepath.Join(m.Dir, domain+".pem"))
	if err != nil {
		return 0, false
	}
	return m.Now().Sub(info.ModTime()), true
}

// GenerateAll syncs the CA, regenerates the localhost certificate and, in
// development, generates the certificates of domains. Every file of the
// certs directory then gets mode 0750.
func (m *Manager) GenerateAll(ctx context.Context, domains []string, force, production bool) error {
	if err := m.InstallCA(ctx); err != nil {
		return err
	}
	if err := m.GenerateLocalhost(ctx); err != nil {
		return err
	}
	if !production {
		m.Log.Info("Creating SSL certificates for: " + strings.Join(domains, ", "))
		for _, d := range domains {
			generated, err := m.GenerateDomain(ctx, d, force)
			if err != nil {
				return err
			}
			if !generated {
				age, _ := m.age(d)
				m.Log.Info(fmt.Sprintf("Certificate for %s still valid (%d days old), skipping", d,
					int(age/(24*time.Hour))))
			}
		}
		m.Log.Success("SSL certificates generated!")
	}

	entries, err := os.ReadDir(m.Dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			if err := os.Chmod(filepath.Join(m.Dir, e.Name()), certMode); err != nil {
				return err
			}
		}
	}
	return nil
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}
