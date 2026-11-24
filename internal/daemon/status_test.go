package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

// fakeDocker is a docker command whose server answers or not: no real
// Docker is ever reached.
type fakeDocker struct{ up bool }

func (f fakeDocker) Run(_ context.Context, _ string, _ map[string]string, args ...string) (docker.Result, error) {
	if !f.up {
		return docker.Result{ExitCode: 1, Stderr: []byte("Cannot connect to the Docker daemon")}, nil
	}
	if args[0] == "ps" {
		return docker.Result{Stdout: []byte(`{"Names":"franken-caddy-proxy","State":"running","Status":"Up 2 minutes"}` + "\n")}, nil
	}
	return docker.Result{}, nil
}

// stoppedDNS is a LAN DNS server that does not serve.
type stoppedDNS struct{}

func (stoppedDNS) Apply(context.Context) (bool, error) { return false, nil }
func (stoppedDNS) Stop(context.Context)                {}
func (stoppedDNS) Running(context.Context) bool        { return false }

// statusServer is a server whose workspace is a temporary data directory,
// with the fake docker.
func statusServer(t *testing.T, d fakeDocker) *Server {
	t.Helper()
	dir := t.TempDir()
	backend := func(log events.Log) (*Workspace, error) {
		e, _, err := env.Open(dir)
		if err != nil {
			return nil, err
		}
		st, err := store.Open(filepath.Join(dir, "db.sqlite"))
		if err != nil {
			return nil, err
		}
		m := docker.New(dir, log)
		m.Runner = d
		svc := &stack.Service{Dir: dir, Env: e, Store: st, Docker: m, DNS: stoppedDNS{}, Log: log}
		return &Workspace{Dir: dir, Env: e, Store: st, Stack: svc, Close: st.Close}, nil
	}
	return New(backend, "", 0, "test")
}

// Docker stopped is not the daemon gone: /api/v1/status still answers 200,
// with docker false and the stack stopped.
func TestStatusReportsDocker(t *testing.T) {
	for _, tc := range []struct {
		name        string
		up          bool
		wantDocker  bool
		wantRunning bool
	}{
		{"docker stopped", false, false, false},
		{"docker running", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := statusServer(t, fakeDocker{up: tc.up})
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d (%s)", w.Code, w.Body)
			}
			var got statusResponse
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Docker != tc.wantDocker || got.Running != tc.wantRunning {
				t.Errorf("docker = %v, running = %v; want %v, %v", got.Docker, got.Running, tc.wantDocker, tc.wantRunning)
			}
			if got.Hosts == nil || len(got.Services) == 0 {
				t.Errorf("hosts = %v, services = %v", got.Hosts, got.Services)
			}
		})
	}
}
