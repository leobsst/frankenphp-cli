package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/events"
)

// The HTTP access drives Docker and the hosts file: only the local
// address, from no other origin, with the token.
func TestGuard(t *testing.T) {
	s := New(nil, "secret", 9081, "1.2.3")
	h := s.guard(s.routes())
	for _, tc := range []struct {
		name, host, origin, auth, path string
		want                           int
	}{
		{"token", "127.0.0.1:9081", "", "Bearer secret", "/api/health", http.StatusOK},
		{"localhost", "localhost:9081", "http://localhost:9081", "Bearer secret", "/api/health", http.StatusOK},
		{"no token", "127.0.0.1:9081", "", "", "/api/health", http.StatusUnauthorized},
		{"wrong token", "127.0.0.1:9081", "", "Bearer nope", "/api/health", http.StatusUnauthorized},
		{"rebinding", "evil.test:9081", "", "Bearer secret", "/api/health", http.StatusForbidden},
		{"other port", "127.0.0.1:80", "", "Bearer secret", "/api/health", http.StatusForbidden},
		{"other origin", "127.0.0.1:9081", "http://evil.test", "Bearer secret", "/api/health", http.StatusForbidden},
		{"query token elsewhere", "127.0.0.1:9081", "", "", "/api/health?access_token=secret", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.path, nil)
			r.Host = tc.host
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.auth != "" {
				r.Header.Set("Authorization", tc.auth)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d (%s)", w.Code, tc.want, w.Body)
			}
		})
	}
}

// waitFor returns the operation once finished.
func waitFor(t *testing.T, q *Queue, id string) Operation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if op, _ := q.Get(id); op.Status == Succeeded || op.Status == Failed {
			return op
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("operation %s not finished", id)
	return Operation{}
}

// The operations run one at a time, in order; a failure or a panic fails
// the operation only.
func TestQueueRunsOneAtATime(t *testing.T) {
	q := NewQueue(NewBus())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.Run(ctx)

	var running, overlap atomic.Int32
	slow := func(context.Context, events.Log) (any, error) {
		if running.Add(1) > 1 {
			overlap.Store(1)
		}
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		return "done", nil
	}
	a := q.Submit("a", slow)
	b := q.Submit("b", slow)
	failed := q.Submit("fail", func(_ context.Context, log events.Log) (any, error) {
		log.Step("Working...")
		log.Error("broken")
		return nil, errors.New("boom")
	})
	panicked := q.Submit("panic", func(context.Context, events.Log) (any, error) { panic("bug") })

	if op := waitFor(t, q, a.ID); op.Status != Succeeded || op.Result != "done" {
		t.Errorf("a = %+v", op)
	}
	waitFor(t, q, b.ID)
	if overlap.Load() != 0 {
		t.Error("two operations ran at once")
	}
	op := waitFor(t, q, failed.ID)
	if op.Status != Failed || op.Error != "boom" || len(op.Events) != 2 ||
		op.Events[0].Kind != "step" || op.Events[1].Level != "error" {
		t.Errorf("failed operation = %+v", op)
	}
	if op := waitFor(t, q, panicked.ID); op.Status != Failed {
		t.Errorf("panicked operation = %+v", op)
	}
	if list := q.List(); len(list) != 4 || list[0].ID != panicked.ID {
		t.Errorf("list = %+v, want the 4 operations, latest first", list)
	}
}

// The token is created once, for the user only.
func TestLoadToken(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadToken(dir)
	if err != nil || len(first) != 64 {
		t.Fatalf("token %q, %v", first, err)
	}
	// Windows has no Unix permission bits: the profile's ACLs protect it
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, TokenFile))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("token file mode = %v, %v", info.Mode(), err)
		}
	}
	if again, _ := LoadToken(dir); again != first {
		t.Error("token changed")
	}
}

// api/openapi.yaml is the contract of the dashboard: it must describe
// every route, and no other.
func TestSpecMatchesRoutes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// "  /api/x:" opens a path, "    get:" one of its operations
	pathLine := regexp.MustCompile(`^  (/api/\S*):\s*$`)
	methodLine := regexp.MustCompile(`^    (get|post|put|delete|patch):\s*$`)
	var spec []string
	inPaths, path := false, ""
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case line == "paths:":
			inPaths = true
		case inPaths && line != "" && !strings.HasPrefix(line, " "):
			inPaths = false
		case !inPaths:
		case pathLine.MatchString(line):
			path = pathLine.FindStringSubmatch(line)[1]
		case methodLine.MatchString(line):
			spec = append(spec, strings.ToUpper(methodLine.FindStringSubmatch(line)[1])+" "+path)
		}
	}
	var routes []string
	for _, r := range New(nil, "", 0, "").routeTable() {
		routes = append(routes, r.pattern)
	}
	slices.Sort(spec)
	slices.Sort(routes)
	if !slices.Equal(spec, routes) {
		t.Errorf("api/openapi.yaml and the routes differ:\nspec:   %v\nroutes: %v", spec, routes)
	}
}

// The log lines arrive in pieces: they are cut at line endings, the
// docker timestamp parsed.
func TestLineWriter(t *testing.T) {
	ch := make(chan LogLine, 10)
	lw := &lineWriter{container: "frankenphp-8.4", lines: ch, ctx: context.Background()}
	_, _ = lw.Write([]byte("2026-11-19T15:04:05.123456789Z first li"))
	_, _ = lw.Write([]byte("ne\r\nno timestamp\n2026-11-19T15:04:06Z last"))
	lw.flush()
	close(ch)
	var got []LogLine
	for l := range ch {
		got = append(got, l)
	}
	if len(got) != 3 || got[0].Line != "first line" || got[0].Time.Second() != 5 ||
		got[1].Line != "no timestamp" || !got[1].Time.IsZero() || got[2].Line != "last" ||
		got[2].Container != "frankenphp-8.4" {
		t.Errorf("lines = %+v", got)
	}
}
