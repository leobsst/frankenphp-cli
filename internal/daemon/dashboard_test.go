package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/leobsst/frankenphp-cli/internal/core/access"
)

var dashboardOrigins = []string{"https://frankenmanager.test", "https://fkm.test", "https://127.0.0.1:9444", "https://localhost:9444"}

// dashboardServer is a daemon with pages and the HTTPS listener of the
// dashboard on 127.0.0.1:9444.
func dashboardServer(t *testing.T, cfg AccessConfig) *Server {
	t.Helper()
	cfg.DashboardOrigins = dashboardOrigins
	s := authServer(t, cfg)
	s.HTTPSPort = 9444
	s.UI = fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>dashboard</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	return s
}

// dashboardRequest sends a request to the HTTPS listener of the dashboard.
func dashboardRequest(h http.Handler, method, path, host string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = "127.0.0.1:51000"
	r.Host = host
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// The HTTPS listener of 127.0.0.1 behind https://frankenmanager.test: the
// dashboard origins sign in without the password, get a Bearer session
// (never a cookie), and act as on this machine; any other Host or Origin is
// refused, and the API stays closed without the session.
func TestDashboardListener(t *testing.T) {
	s := dashboardServer(t, AccessConfig{Mode: access.Local})
	h := s.dashboardGuard(s.routes())
	host := "127.0.0.1:9444"
	page := map[string]string{"Origin": "https://frankenmanager.test"}

	if w := dashboardRequest(h, http.MethodGet, "/", host, nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "dashboard") {
		t.Errorf("index: %d %s", w.Code, w.Body)
	}
	w := dashboardRequest(h, http.MethodGet, "/config.json", host, nil)
	var cfg uiConfig
	_ = json.Unmarshal(w.Body.Bytes(), &cfg)
	if w.Code != http.StatusOK || cfg.API != "" || cfg.Mode != "local" || cfg.Version != "1.2.3" {
		t.Errorf("config: %d %+v", w.Code, cfg)
	}
	// DNS rebinding and foreign pages
	if w := dashboardRequest(h, http.MethodGet, "/", "evil.test:9444", nil); w.Code != http.StatusForbidden {
		t.Errorf("foreign host: %d", w.Code)
	}
	for _, origin := range []string{"https://evil.test", "http://frankenmanager.test", "https://frankenmanager.test.evil.test"} {
		w := dashboardRequest(h, http.MethodPost, "/api/v1/auth/local", host, map[string]string{"Origin": origin})
		if w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("origin %s: %d %v", origin, w.Code, w.Header())
		}
	}
	// No session without a dashboard origin: a request that no page sent
	if w := dashboardRequest(h, http.MethodPost, "/api/v1/auth/local", host, nil); w.Code != http.StatusNotFound {
		t.Errorf("no origin: %d", w.Code)
	}

	if w := dashboardRequest(h, http.MethodOptions, "/api/v1/auth/local", host, page); w.Code != http.StatusNoContent ||
		w.Header().Get("Access-Control-Allow-Origin") != "https://frankenmanager.test" || w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf("preflight: %d %v", w.Code, w.Header())
	}
	w = dashboardRequest(h, http.MethodPost, "/api/v1/auth/local", host, page)
	var res struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != http.StatusOK || res.Token == "" || len(w.Result().Cookies()) != 0 {
		t.Fatalf("local sign-in: %d %s %v", w.Code, w.Body, w.Result().Cookies())
	}

	if w := dashboardRequest(h, http.MethodGet, "/api/v1/auth/sessions", host, page); w.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d", w.Code)
	}
	bearer := map[string]string{"Origin": "https://frankenmanager.test", "Authorization": "Bearer " + res.Token}
	if w := dashboardRequest(h, http.MethodGet, "/api/v1/auth/sessions", host, bearer); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"remote":false`) {
		t.Errorf("sessions: %d %s", w.Code, w.Body)
	}
	// This machine: the destructive actions need no password again
	if w := dashboardRequest(h, http.MethodPost, "/api/v1/redis/flush", host, bearer); w.Code != http.StatusAccepted {
		t.Errorf("flush: %d %s", w.Code, w.Body)
	}
	// The daemon token of 127.0.0.1:DASHBOARD_PORT is not a session here
	if w := dashboardRequest(h, http.MethodGet, "/api/v1/auth/sessions", host,
		map[string]string{"Authorization": "Bearer secret"}); w.Code != http.StatusUnauthorized {
		t.Errorf("daemon token: %d", w.Code)
	}
}

// With DASHBOARD_LOCAL_AUTH and a password, this machine signs in with the
// password too.
func TestDashboardListenerLocalAuth(t *testing.T) {
	s := dashboardServer(t, AccessConfig{Mode: access.Local, LocalAuth: true})
	h := s.dashboardGuard(s.routes())
	page := map[string]string{"Origin": "https://frankenmanager.test"}
	if w := dashboardRequest(h, http.MethodPost, "/api/v1/auth/local", "127.0.0.1:9444", page); w.Code != http.StatusForbidden {
		t.Errorf("local sign-in: %d", w.Code)
	}
}

// The pages are served on every listener, without a session; the API keeps
// its guard. On 127.0.0.1:DASHBOARD_PORT (behind the reverse proxy), the
// page reaches the API at the HTTPS listener.
func TestDashboardPages(t *testing.T) {
	s := dashboardServer(t, AccessConfig{Mode: access.LAN})
	local := s.guard(s.routes())
	r := httptest.NewRequest(http.MethodGet, "/config.json", nil)
	r.Host = "127.0.0.1:9081"
	w := httptest.NewRecorder()
	local.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"api":"https://127.0.0.1:9444"`) {
		t.Errorf("local config: %d %s", w.Code, w.Body)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	r.Host = "127.0.0.1:9081"
	w = httptest.NewRecorder()
	local.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("local API without token: %d", w.Code)
	}

	remote := s.remoteGuard(s.routes())
	if w := remoteRequest(remote, http.MethodGet, "/config.json", "192.168.1.5", "", nil); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), `"mode":"remote"`) {
		t.Errorf("remote config: %d %s", w.Code, w.Body)
	}
	if w := remoteRequest(remote, http.MethodGet, "/", "8.8.8.8", "", nil); w.Code != http.StatusForbidden {
		t.Errorf("refused address: %d", w.Code)
	}
	if w := remoteRequest(remote, http.MethodPost, "/", "192.168.1.5", "", nil); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST page: %d", w.Code)
	}
}

// The installable dashboard (PWA): the service worker and the manifest are
// served with their type and revalidated on every load, and a missing
// static file is a 404, never index.html (a browser would install or cache
// the page as a service worker, a manifest or a script); the routes of the
// page still fall back to index.html.
func TestDashboardPWAFiles(t *testing.T) {
	s := dashboardServer(t, AccessConfig{Mode: access.LAN})
	remote := s.remoteGuard(s.routes())
	for _, p := range []string{"/sw.js", "/manifest.webmanifest", "/assets/gone-123.js", "/icons/gone.png"} {
		if w := remoteRequest(remote, http.MethodGet, p, "192.168.1.5", "", nil); w.Code != http.StatusNotFound {
			t.Errorf("missing %s: %d %q", p, w.Code, w.Body)
		}
	}
	if w := remoteRequest(remote, http.MethodGet, "/some/route", "192.168.1.5", "", nil); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), "<title>dashboard</title>") {
		t.Errorf("SPA fallback: %d %q", w.Code, w.Body)
	}

	s.UI = fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>dashboard</title>")},
		"sw.js":                {Data: []byte("self.addEventListener('fetch', () => {})")},
		"manifest.webmanifest": {Data: []byte(`{"name":"FrankenManager"}`)},
	}
	for _, f := range []struct{ path, typ string }{
		{"/sw.js", "text/javascript"},
		{"/manifest.webmanifest", "application/manifest+json"},
	} {
		w := remoteRequest(remote, http.MethodGet, f.path, "192.168.1.5", "", nil)
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), f.typ) {
			t.Errorf("%s: %d %q", f.path, w.Code, w.Header().Get("Content-Type"))
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: Cache-Control %q", f.path, cc)
		}
		if strings.Contains(w.Body.String(), "<title>") {
			t.Errorf("%s answered index.html", f.path)
		}
	}
}
