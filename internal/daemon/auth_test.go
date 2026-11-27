package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/access"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

const testPassword = "correct horse battery"

// authServer is a daemon whose access is cfg, its sessions in a temporary
// database.
func authServer(t *testing.T, cfg AccessConfig) *Server {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	s := New(nil, "secret", 9081, "1.2.3")
	s.OpenStore = func() (*store.Store, error) { return store.Open(path) }
	if cfg.PasswordHash == "" {
		hash, err := access.HashPassword(testPassword)
		if err != nil {
			t.Fatal(err)
		}
		cfg.PasswordHash = hash
	}
	s.accessCfg.Store(&cfg)
	return s
}

// remoteRequest sends a request to the remote access from ip.
func remoteRequest(h http.Handler, method, path, ip, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = ip + ":51000"
	r.Host = "192.168.1.10:9443"
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func login(t *testing.T, h http.Handler, ip string) string {
	t.Helper()
	w := remoteRequest(h, http.MethodPost, "/api/v1/auth/login", ip, `{"password":"`+testPassword+`","device":"phone"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	var res struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if c := w.Result().Cookies(); len(c) != 1 || !c[0].HttpOnly || !c[0].Secure || c[0].SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie = %+v", c)
	}
	return res.Token
}

// The remote access lets in the allowed addresses only, then a session;
// the destructive actions need the password again.
func TestRemoteAccess(t *testing.T) {
	allow, _ := access.ParseAllowList("192.168.1.0/24, 8.8.8.8")
	s := authServer(t, AccessConfig{Mode: access.LAN, Allow: allow})
	h := s.remoteGuard(s.routes())

	// lan: public addresses refused even when listed; unlisted private ones too
	for _, ip := range []string{"8.8.8.8", "10.0.0.2"} {
		if w := remoteRequest(h, http.MethodGet, "/api/health", ip, "", nil); w.Code != http.StatusForbidden {
			t.Errorf("%s: %d", ip, w.Code)
		}
	}
	// Allowed: the health answers, the rest needs a session
	if w := remoteRequest(h, http.MethodGet, "/api/health", "192.168.1.5", "", nil); w.Code != http.StatusOK {
		t.Errorf("health: %d", w.Code)
	}
	if w := remoteRequest(h, http.MethodGet, "/api/v1/auth/sessions", "192.168.1.5", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d", w.Code)
	}
	if w := remoteRequest(h, http.MethodGet, "/api/v1/auth/sessions", "192.168.1.5", "",
		map[string]string{"Authorization": "Bearer forged"}); w.Code != http.StatusUnauthorized {
		t.Errorf("forged session: %d", w.Code)
	}
	// Across origins, HTTPS pages only (TestRemoteAccessCrossOrigins)
	if w := remoteRequest(h, http.MethodGet, "/api/health", "192.168.1.5", "",
		map[string]string{"Origin": "http://evil.test"}); w.Code != http.StatusForbidden {
		t.Errorf("foreign origin: %d", w.Code)
	}

	token := login(t, h, "192.168.1.5")
	bearer := map[string]string{"Authorization": "Bearer " + token}
	w := remoteRequest(h, http.MethodGet, "/api/v1/auth/sessions", "192.168.1.5", "", bearer)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"device":"phone"`) || !strings.Contains(w.Body.String(), `"current":true`) {
		t.Errorf("sessions: %d %s", w.Code, w.Body)
	}

	// Destructive: the password again
	if w := remoteRequest(h, http.MethodPost, "/api/v1/redis/flush", "192.168.1.5", "", bearer); w.Code != http.StatusPreconditionRequired {
		t.Errorf("unconfirmed: %d", w.Code)
	}
	wrong := map[string]string{"Authorization": "Bearer " + token, confirmHeader: "nope"}
	if w := remoteRequest(h, http.MethodPost, "/api/v1/redis/flush", "192.168.1.5", "", wrong); w.Code != http.StatusForbidden {
		t.Errorf("wrong confirmation: %d", w.Code)
	}
	right := map[string]string{"Authorization": "Bearer " + token, confirmHeader: testPassword}
	if w := remoteRequest(h, http.MethodPost, "/api/v1/redis/flush", "192.168.1.5", "", right); w.Code != http.StatusAccepted {
		t.Errorf("confirmed: %d %s", w.Code, w.Body)
	}
	// Not destructive: no confirmation; stopping a container is, starting is not
	if w := remoteRequest(h, http.MethodPost, "/api/v1/containers/franken_redis/start", "192.168.1.5", "", bearer); w.Code != http.StatusAccepted {
		t.Errorf("start: %d", w.Code)
	}
	if w := remoteRequest(h, http.MethodPost, "/api/v1/containers/franken_redis/stop", "192.168.1.5", "", bearer); w.Code != http.StatusPreconditionRequired {
		t.Errorf("stop: %d", w.Code)
	}

	// Signed out: the token is dead
	remoteRequest(h, http.MethodPost, "/api/v1/auth/logout", "192.168.1.5", "", bearer)
	if w := remoteRequest(h, http.MethodGet, "/api/v1/auth/sessions", "192.168.1.5", "", bearer); w.Code != http.StatusUnauthorized {
		t.Errorf("after logout: %d", w.Code)
	}

	st, _ := s.OpenStore()
	defer func() { _ = st.Close() }()
	events := []string{}
	log, _ := st.AccessLog(10)
	for _, e := range log {
		events = append(events, e.Event)
	}
	if strings.Join(events, ",") != "logout,confirm_failed,login,refused,refused" {
		t.Errorf("access log = %v", events)
	}
}

// The mobile app (Capacitor: capacitor://localhost on iOS, https://localhost
// on Android), its dev server and the dashboard of another machine (any
// HTTPS origin) call the remote access across origins: the CORS headers let
// them read every answer, a refusal too, and the preflight passes without a
// session. The cookie never crosses origins: no
// Access-Control-Allow-Credentials, and a cookie sent anyway is ignored.
// Any other origin stays refused.
func TestRemoteAccessCrossOrigins(t *testing.T) {
	allow, _ := access.ParseAllowList("192.168.1.0/24")
	s := authServer(t, AccessConfig{Mode: access.LAN, Allow: allow})
	h := s.remoteGuard(s.routes())
	app := func(origin string, extra map[string]string) map[string]string {
		headers := map[string]string{"Origin": origin}
		for k, v := range extra {
			headers[k] = v
		}
		return headers
	}

	for _, origin := range []string{"capacitor://localhost", "https://localhost", "http://localhost:5174", "https://192.168.1.42:9443"} {
		w := remoteRequest(h, http.MethodOptions, "/api/v1/auth/login", "192.168.1.5", "",
			app(origin, map[string]string{"Access-Control-Request-Method": "POST"}))
		got := w.Header()
		if w.Code != http.StatusNoContent || got.Get("Access-Control-Allow-Origin") != origin ||
			!strings.Contains(got.Get("Access-Control-Allow-Headers"), "Authorization") ||
			!strings.Contains(got.Get("Access-Control-Allow-Headers"), confirmHeader) ||
			got.Get("Access-Control-Allow-Credentials") != "" {
			t.Errorf("preflight from %s: %d %v", origin, w.Code, got)
		}
	}

	origin := "capacitor://localhost"
	if w := remoteRequest(h, http.MethodGet, "/api/health", "192.168.1.5", "", app(origin, nil)); w.Code != http.StatusOK ||
		w.Header().Get("Access-Control-Allow-Origin") != origin {
		t.Errorf("health: %d %v", w.Code, w.Header())
	}
	// Without a session, the app reads the 401 to ask for the password
	if w := remoteRequest(h, http.MethodGet, "/api/v1/auth/sessions", "192.168.1.5", "", app(origin, nil)); w.Code != http.StatusUnauthorized ||
		w.Header().Get("Access-Control-Allow-Origin") != origin {
		t.Errorf("no session: %d %v", w.Code, w.Header())
	}
	// A refused address: the app reads why
	if w := remoteRequest(h, http.MethodGet, "/api/health", "8.8.8.8", "", app(origin, nil)); w.Code != http.StatusForbidden ||
		w.Header().Get("Access-Control-Allow-Origin") != origin {
		t.Errorf("refused address: %d %v", w.Code, w.Header())
	}

	token := login(t, h, "192.168.1.5")
	w := remoteRequest(h, http.MethodGet, "/api/v1/auth/sessions", "192.168.1.5", "",
		app(origin, map[string]string{"Authorization": "Bearer " + token}))
	if w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != origin {
		t.Errorf("sessions: %d %v", w.Code, w.Header())
	}

	// The dashboard of another machine: its Bearer token, never the cookie
	dashboard := "https://192.168.1.42:9443"
	if w := remoteRequest(h, http.MethodGet, "/api/v1/auth/sessions", "192.168.1.5", "",
		app(dashboard, map[string]string{"Authorization": "Bearer " + token})); w.Code != http.StatusOK ||
		w.Header().Get("Access-Control-Allow-Origin") != dashboard {
		t.Errorf("sessions from another dashboard: %d %v", w.Code, w.Header())
	}
	cookie := map[string]string{"Cookie": sessionCookie + "=" + token}
	if w := remoteRequest(h, http.MethodGet, "/api/v1/auth/sessions", "192.168.1.5", "", app(dashboard, cookie)); w.Code != http.StatusUnauthorized {
		t.Errorf("cookie across origins: %d", w.Code)
	}
	if w := remoteRequest(h, http.MethodGet, "/api/v1/auth/sessions", "192.168.1.5", "", cookie); w.Code != http.StatusOK {
		t.Errorf("cookie on the same origin: %d", w.Code)
	}

	for _, foreign := range []string{"http://192.168.1.42:9443", "http://localhost.evil.test", "capacitor://evil.test", "file://localhost", "null"} {
		w := remoteRequest(h, http.MethodOptions, "/api/v1/auth/login", "192.168.1.5", "", app(foreign, nil))
		if w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("foreign origin %s: %d %v", foreign, w.Code, w.Header())
		}
	}
}

// Too many wrong passwords lock the address, not the others.
func TestLoginLockout(t *testing.T) {
	s := authServer(t, AccessConfig{Mode: access.Internet})
	h := s.remoteGuard(s.routes())
	for range maxFailures {
		if w := remoteRequest(h, http.MethodPost, "/api/v1/auth/login", "203.0.113.9", `{"password":"guess"}`, nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("wrong password: %d", w.Code)
		}
	}
	if w := remoteRequest(h, http.MethodPost, "/api/v1/auth/login", "203.0.113.9", `{"password":"`+testPassword+`"}`, nil); w.Code != http.StatusTooManyRequests {
		t.Errorf("locked: %d", w.Code)
	}
	login(t, h, "198.51.100.4")
}

// The local access needs the token, and a session too with
// DASHBOARD_LOCAL_AUTH; the first password is set on the host only.
func TestLocalAuth(t *testing.T) {
	s := authServer(t, AccessConfig{Mode: access.Local, LocalAuth: true})
	h := s.guard(s.routes())
	local := func(path, session string) int {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Host = "127.0.0.1:9081"
		r.Header.Set("Authorization", "Bearer secret")
		if session != "" {
			r.Header.Set(sessionHeader, session)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if code := local("/api/v1/auth/sessions", ""); code != http.StatusUnauthorized {
		t.Errorf("no session: %d", code)
	}
	if code := local("/api/v1/auth", ""); code != http.StatusOK {
		t.Errorf("auth state: %d", code)
	}

	// No password: nothing to sign in with, and none settable remotely
	s2 := authServer(t, AccessConfig{Mode: access.Internet})
	s2.accessCfg.Store(&AccessConfig{Mode: access.Internet})
	h2 := s2.remoteGuard(s2.routes())
	if w := remoteRequest(h2, http.MethodPost, "/api/v1/auth/login", "203.0.113.9", `{"password":"x"}`, nil); w.Code != http.StatusConflict {
		t.Errorf("login without password: %d", w.Code)
	}
}

// Every destructive route exists: a typo would leave it unprotected.
func TestDestructiveRoutesExist(t *testing.T) {
	patterns := map[string]bool{}
	for _, r := range New(nil, "", 0, "").routeTable() {
		patterns[r.pattern] = true
	}
	for p := range destructive {
		if !patterns[p] {
			t.Errorf("%s is not a route", p)
		}
	}
}
