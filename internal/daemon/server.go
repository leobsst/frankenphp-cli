// Package daemon is the core of FrankenManager as a long-running process:
// one HTTP/JSON API, served on a Unix socket for the CLI and on 127.0.0.1
// for the dashboard. Every action that changes the shared state runs as an
// operation of a single queue; the clients follow them on an event stream
// (SSE).
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/access"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// Server is the API of the daemon.
type Server struct {
	Backend Backend
	// Token is required on the HTTP access, not on the socket
	Token string
	// Port is the port of the HTTP access, checked in the Host and Origin
	// headers
	Port    int
	Version string
	Queue   *Queue
	Bus     *Bus

	// OpenStore opens the database of the sessions; LoadAccess reads the
	// access settings; RemoteCert returns the certificate of the remote
	// access (generated when the settings name none). Nil without remote
	// access (tests, the in-process server of the CLI).
	OpenStore  func() (*store.Store, error)
	LoadAccess func() (AccessConfig, error)
	RemoteCert func(cfg AccessConfig) (certFile, keyFile string, err error)
	// LANIP returns the address of the host on the local network
	LANIP func() string
	// Logf reports what the remote access does (the daemon log)
	Logf func(format string, args ...any)

	mu   sync.Mutex
	stop context.CancelFunc // ends Serve, set while it runs

	serveCtx  context.Context // the context of Serve, while it runs
	accessCfg atomic.Pointer[AccessConfig]
	limiter   limiter
	remote    remoteListener
}

// accessRefresh is how often the access settings are read again.
var accessRefresh = 30 * time.Second

// serveContext is the context of Serve, Background before it runs.
func (s *Server) serveContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.serveCtx != nil {
		return s.serveCtx
	}
	return context.Background()
}

// access returns the access settings in force.
func (s *Server) access() AccessConfig {
	if c := s.accessCfg.Load(); c != nil {
		return *c
	}
	return AccessConfig{Mode: access.Local}
}

// New returns a server with its queue and bus.
func New(backend Backend, token string, port int, version string) *Server {
	bus := NewBus()
	return &Server{Backend: backend, Token: token, Port: port, Version: version, Queue: NewQueue(bus), Bus: bus}
}

// Handler is the API without the guard of the HTTP access: what the socket
// serves.
func (s *Server) Handler() http.Handler { return s.routes() }

// Serve runs the queue and serves the API on the socket listener (CLI, no
// token) and the HTTP one (dashboard, token and headers checked) until ctx
// is done. Either listener may be nil.
func (s *Server) Serve(ctx context.Context, socket, local net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	s.stop = cancel
	s.mu.Unlock()
	go s.Queue.Run(ctx)
	if s.Backend != nil {
		go s.watchContainers(ctx)
	}
	s.mu.Lock()
	s.serveCtx = ctx
	s.mu.Unlock()
	s.ReloadAccess()
	defer s.remote.stop()
	// A change made without the daemon (a command run in its own process)
	// is picked up too
	go func() {
		tick := time.NewTicker(accessRefresh)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				s.ReloadAccess()
			}
		}
	}()

	api := s.routes()
	var servers []*http.Server
	errs := make(chan error, 2)
	serve := func(ln net.Listener, h http.Handler) {
		srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second,
			BaseContext: func(net.Listener) context.Context { return ctx }}
		servers = append(servers, srv)
		go func() { errs <- srv.Serve(ln) }()
	}
	if socket != nil {
		serve(socket, api)
	}
	if local != nil {
		serve(local, s.guard(api))
	}

	select {
	case <-ctx.Done():
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			cancel()
			s.shutdown(servers)
			return err
		}
	}
	s.shutdown(servers)
	return nil
}

// Stop ends Serve, after the requests in progress.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop != nil {
		s.stop()
	}
}

func (s *Server) shutdown(servers []*http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, srv := range servers {
		_ = srv.Shutdown(ctx)
	}
}

// --- Responses ---------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// apiError is the body of an error response.
type apiError struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// readJSON decodes the body of r into v, answering 400 when it cannot.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, i18n.Tf("Invalid request body: %v", err))
		return false
	}
	return true
}

// --- Event stream ------------------------------------------------------------

// heartbeat keeps the event stream open through proxies and lets the
// clients notice a lost daemon.
const heartbeat = 25 * time.Second

// events streams the bus to the client as Server-Sent Events until it
// leaves.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, i18n.T("Streaming is not supported."))
		return
	}
	ch, cancel := s.Bus.Subscribe()
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
		case m, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(m.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", m.Type, data)
		}
		flusher.Flush()
	}
}
