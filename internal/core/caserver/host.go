package caserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// Host shares the root CA from this process (the daemon) rather than a
// detached one. Its Start, Stop and State fit Sharing.
type Host struct {
	mu   sync.Mutex
	srv  *http.Server
	port int
}

// Start serves certFile on 0.0.0.0:port, replacing a sharing in progress.
// It fails at once when the port is taken.
func (h *Host) Start(certFile string, port int) error {
	cert, err := os.ReadFile(certFile)
	if err != nil {
		return &Error{i18n.Tf("Root CA not found at %s. "+
			"Run `sudo frankenmanager setup --install-mkcert` first.", certFile)}
	}
	h.Stop()
	ln, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: Handler(cert), ReadHeaderTimeout: 10 * time.Second}
	h.mu.Lock()
	h.srv, h.port = srv, port
	h.mu.Unlock()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.mu.Lock()
			if h.srv == srv {
				h.srv = nil
			}
			h.mu.Unlock()
		}
	}()
	return nil
}

// Stop ends the sharing and reports whether it was on.
func (h *Host) Stop() bool {
	h.mu.Lock()
	srv := h.srv
	h.srv = nil
	h.mu.Unlock()
	if srv == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	return true
}

// State returns the port of the sharing in progress.
func (h *Host) State() (port int, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.port, h.srv != nil
}

// Sharing returns the sharing of dataDir served by h.
func (h *Host) Sharing(dataDir string, rootCA func(ctx context.Context) (string, error), lanIP func() string) Sharing {
	return Sharing{
		Dir:    dataDir,
		RootCA: rootCA,
		Start:  func(_, certFile string, port int) error { return h.Start(certFile, port) },
		Stop:   func(string) bool { return h.Stop() },
		State: func(string) (int, int, bool) {
			port, ok := h.State()
			return os.Getpid(), port, ok
		},
		LANIP: lanIP,
	}
}
