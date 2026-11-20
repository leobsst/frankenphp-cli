package daemon

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/access"
)

// remoteListener is the HTTPS server of the remote access, while it runs.
type remoteListener struct {
	mu   sync.Mutex
	srv  *http.Server
	addr string // what it listens on, to restart it on a change
	cert string // the certificate files it serves, idem
}

func (l *remoteListener) stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = l.srv.Shutdown(ctx)
		l.srv, l.addr, l.cert = nil, "", ""
	}
}

// running reports the address of the remote access, "" when off.
func (l *remoteListener) running() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.addr
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// ReloadAccess reads the access settings again and starts, stops or
// restarts the remote access to match them: on at start, then after a
// change of a DASHBOARD_* setting. The remote access needs a password: it
// stays off without one.
func (s *Server) ReloadAccess() {
	ctx := s.serveContext()
	if s.LoadAccess == nil {
		return
	}
	cfg, err := s.LoadAccess()
	if err != nil {
		s.logf("dashboard access: %v", err)
		return
	}
	s.accessCfg.Store(&cfg)
	if !access.Remote(cfg.Mode) {
		s.remote.stop()
		return
	}
	if !cfg.PasswordSet() {
		s.remote.stop()
		s.logf("remote access (%s) is off: set DASHBOARD_PASSWORD first", cfg.Mode)
		return
	}
	certFile, keyFile, err := s.RemoteCert(cfg)
	if err != nil {
		s.remote.stop()
		s.logf("remote access is off: %v", err)
		return
	}
	addr := net.JoinHostPort("", strconv.Itoa(cfg.RemotePort))
	s.remote.mu.Lock()
	same := s.remote.srv != nil && s.remote.addr == addr && s.remote.cert == certFile+"\n"+keyFile
	s.remote.mu.Unlock()
	if same {
		// The mode, password and addresses are read on each request
		return
	}
	s.remote.stop()
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		s.logf("remote access is off: %v", err)
		return
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		s.logf("remote access is off: %v", err)
		return
	}
	srv := &http.Server{
		Handler:           s.remoteGuard(s.routes()),
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}},
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	s.remote.mu.Lock()
	s.remote.srv, s.remote.addr, s.remote.cert = srv, addr, certFile+"\n"+keyFile
	s.remote.mu.Unlock()
	s.logf("remote access (%s) listening on https://%s", cfg.Mode, ln.Addr())
	go func() {
		if err := srv.ServeTLS(ln, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logf("remote access stopped: %v", err)
		}
	}()
}
