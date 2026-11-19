package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// --- Containers --------------------------------------------------------------

func (s *Server) containers(w http.ResponseWriter, r *http.Request) {
	s.read(w, func(ws *Workspace) (any, error) { return ws.Stack.Containers(r.Context()) })
}

// containerAction starts, stops or restarts one container of the stack.
func (s *Server) containerAction(w http.ResponseWriter, r *http.Request) {
	name, action := r.PathValue("name"), r.PathValue("action")
	if !slices.Contains(stack.ContainerActions, action) {
		writeError(w, http.StatusNotFound, i18n.Tf("Unknown action '%s'. Use %s.", action, strings.Join(stack.ContainerActions, ", ")))
		return
	}
	s.submit(w, r, "containers."+action, func(ctx context.Context, ws *Workspace) (any, error) {
		return nil, ws.Stack.ContainerAction(ctx, name, action)
	})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	s.read(w, func(ws *Workspace) (any, error) { return ws.Stack.Stats(r.Context()) })
}

// renewCertificate generates the certificate of a host again.
func (s *Server) renewCertificate(w http.ResponseWriter, r *http.Request) {
	host := r.PathValue("name")
	s.submit(w, r, "hosts.certificate", func(ctx context.Context, ws *Workspace) (any, error) {
		svc, err := running(ctx, ws, errNotRunning)
		if err != nil {
			return nil, err
		}
		return nil, svc.RenewCertificate(ctx, host)
	})
}

// --- Logs --------------------------------------------------------------------

// LogLine is a line of the logs of a container.
type LogLine struct {
	Container string    `json:"container"`
	Time      time.Time `json:"time"`
	Line      string    `json:"line"`
}

// defaultTail is how many past lines of each container the log stream
// starts with.
const defaultTail = 100

// logs streams the logs of the stack (?container=<name>, repeated, for some
// of its containers), from the last ?tail= lines of each, as Server-Sent
// Events, until the client leaves.
func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, i18n.T("Streaming is not supported."))
		return
	}
	tail := defaultTail
	if v := r.URL.Query().Get("tail"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, i18n.Tf("Invalid value '%s' for %s.", v, "tail"))
			return
		}
		tail = n
	}
	ws, err := s.Backend(events.NewLog(nil))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = ws.Close() }()
	names := r.URL.Query()["container"]
	if len(names) == 0 {
		all, err := ws.Stack.Containers(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, c := range all {
			names = append(names, c.Name)
		}
	}
	for _, n := range names {
		if err := ws.Stack.CheckContainer(n); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	lines := make(chan LogLine, subscriberBuffer)
	var wg sync.WaitGroup
	for _, n := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lw := &lineWriter{container: n, lines: lines, ctx: ctx}
			_ = ws.Stack.Docker.FollowLogs(ctx, n, tail, lw)
			lw.flush()
		}()
	}
	defer wg.Wait()

	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
		case l := <-lines:
			data, err := json.Marshal(l)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
		}
		flusher.Flush()
	}
}

// lineWriter splits the output of `docker logs --timestamps` into lines.
type lineWriter struct {
	container string
	lines     chan<- LogLine
	ctx       context.Context
	buf       bytes.Buffer
}

func (lw *lineWriter) Write(p []byte) (int, error) {
	lw.buf.Write(p)
	for {
		i := bytes.IndexByte(lw.buf.Bytes(), '\n')
		if i < 0 {
			return len(p), nil
		}
		line := string(lw.buf.Next(i + 1))
		lw.send(strings.TrimRight(line, "\r\n"))
	}
}

// flush sends a last line without line ending.
func (lw *lineWriter) flush() {
	if lw.buf.Len() > 0 {
		lw.send(lw.buf.String())
		lw.buf.Reset()
	}
}

// send parses the timestamp docker writes first, then sends the line.
func (lw *lineWriter) send(line string) {
	l := LogLine{Container: lw.container, Line: line}
	if ts, rest, ok := strings.Cut(line, " "); ok {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			l.Time, l.Line = t, rest
		}
	}
	select {
	case lw.lines <- l:
	case <-lw.ctx.Done():
	}
}
