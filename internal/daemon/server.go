// Package daemon is the core of FrankenManager as a long-running process:
// one HTTP/JSON API, served on a Unix socket for the CLI and on 127.0.0.1
// for the dashboard. Every action that changes the shared state runs as an
// operation of a single queue; the clients follow them on an event stream
// (SSE).
package daemon

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

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

	mu   sync.Mutex
	stop context.CancelFunc // ends Serve, set while it runs
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

// guard protects the HTTP access: the daemon drives Docker and the hosts
// file, and FrankenManager runs its own DNS server. The Host and Origin
// headers must be the local address (no DNS rebinding), and the token is
// required: in the Authorization header, or the access_token query
// parameter for the event and log streams (EventSource sends no header).
func (s *Server) guard(next http.Handler) http.Handler {
	port := strconv.Itoa(s.Port)
	hosts := []string{"127.0.0.1:" + port, "localhost:" + port}
	allowed := func(host string) bool {
		for _, h := range hosts {
			if strings.EqualFold(host, h) {
				return true
			}
		}
		return false
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed(r.Host) {
			writeError(w, http.StatusForbidden, i18n.T("Forbidden host."))
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !allowed(strings.TrimPrefix(origin, "http://")) {
			writeError(w, http.StatusForbidden, i18n.T("Forbidden origin."))
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" && (r.URL.Path == "/api/events" || r.URL.Path == "/api/logs") {
			token = r.URL.Query().Get("access_token")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.Token)) != 1 {
			writeError(w, http.StatusUnauthorized, i18n.T("Missing or invalid access token."))
			return
		}
		next.ServeHTTP(w, r)
	})
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
