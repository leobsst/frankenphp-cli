package cli

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/command"
	"github.com/leobsst/frankenphp-cli/internal/core/access"
	"github.com/leobsst/frankenphp-cli/internal/core/dashboard"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/daemon"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// loadAccess reads the access settings of the dashboard of dir.
func loadAccess(dir string) (daemon.AccessConfig, error) {
	e, _, err := env.Open(dir)
	if err != nil {
		return daemon.AccessConfig{}, err
	}
	cfg := daemon.AccessConfig{
		Mode:         e.Get(env.DashboardAccessKey),
		PasswordHash: e.Get(env.DashboardPasswordKey),
		LocalAuth:    e.Get(env.DashboardLocalAuthKey) == "true",
		TLSCert:      e.Get(env.DashboardTLSCertKey),
		TLSKey:       e.Get(env.DashboardTLSKeyKey),
	}
	if !slices.Contains(access.Modes, cfg.Mode) {
		cfg.Mode = access.Local
	}
	if cfg.PasswordHash != "" && !access.IsHash(cfg.PasswordHash) {
		// Written around settings.Set: refused rather than compared in clear
		cfg.PasswordHash = ""
	}
	if cfg.RemotePort, err = strconv.Atoi(e.Get(env.DashboardRemotePortKey)); err != nil {
		cfg.RemotePort, _ = strconv.Atoi(env.DefaultDashboardRemotePort)
	}
	if cfg.Allow, err = access.ParseAllowList(e.Get(env.DashboardAllowedIPsKey)); err != nil {
		return cfg, err
	}
	// The dashboard names a registered host does not take: its pages would
	// sign in without the password otherwise
	names := dashboard.Domains(e)
	if st, err := store.Open(filepath.Join(dir, "db.sqlite")); err == nil {
		names, err = dashboard.Available(e, st)
		_ = st.Close()
		if err != nil {
			return cfg, err
		}
	}
	cfg.DashboardOrigins = dashboard.Origins(names, e.Get("WEB_HTTPS_PORT"), dashboard.HTTPSPort(e))
	return cfg, nil
}

// remoteTLSDir holds the certificate generated for the remote access.
const remoteTLSDir = "daemon-tls"

// remoteCert returns the certificate of the remote access: the one the
// settings name, or one generated with mkcert for this machine (its LAN
// address, its name, localhost), generated again when the LAN address
// changed. The other devices trust it with the root CA (trust-ca).
func (a *app) remoteCert(ctx context.Context, dir string, cfg daemon.AccessConfig) (string, string, error) {
	if cfg.TLSCert != "" || cfg.TLSKey != "" {
		for _, f := range []string{cfg.TLSCert, cfg.TLSKey} {
			if _, err := os.Stat(f); err != nil {
				return "", "", i18n.Errorf("Certificate file %s: %v", f, err)
			}
		}
		return cfg.TLSCert, cfg.TLSKey, nil
	}
	certDir := filepath.Join(dir, remoteTLSDir)
	certFile, keyFile := filepath.Join(certDir, "cert.pem"), filepath.Join(certDir, "key.pem")
	names := []string{"localhost", "127.0.0.1", "::1"}
	if host, err := os.Hostname(); err == nil && host != "" {
		names = append(names, host)
		if !strings.Contains(host, ".") {
			names = append(names, host+".local")
		}
	}
	if ip := a.deps.LANIP(); ip != "" && net.ParseIP(ip) != nil {
		names = append(names, ip)
	}
	if certCovers(certFile, names) {
		if _, err := os.Stat(keyFile); err == nil {
			return certFile, keyFile, nil
		}
	}
	mkcert := a.deps.Command.LookPath("mkcert")
	if mkcert == "" {
		return "", "", i18n.NewError("mkcert not found: install it (sudo frankenmanager setup --install-mkcert) or set DASHBOARD_TLS_CERT and DASHBOARD_TLS_KEY.")
	}
	if err := os.MkdirAll(certDir, 0o700); err != nil {
		return "", "", err
	}
	res, err := a.deps.Command.Run(ctx, command.Cmd{Name: mkcert,
		Args: append([]string{"-cert-file", certFile, "-key-file", keyFile}, names...)})
	if err != nil {
		return "", "", err
	}
	if !res.OK() {
		return "", "", i18n.Errorf("mkcert failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	return certFile, keyFile, os.Chmod(keyFile, 0o600)
}

// certCovers reports whether the certificate file exists and names every
// one of names.
func certCovers(file string, names []string) bool {
	data, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil || time.Until(cert.NotAfter) < 30*24*time.Hour {
		return false
	}
	for _, n := range names {
		if cert.VerifyHostname(n) != nil {
			return false
		}
	}
	return true
}
