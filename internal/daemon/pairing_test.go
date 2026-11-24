package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/access"
)

// A pairing code is valid once, for pairingTTL, and only the latest
// maxPairingCodes are kept.
func TestPairingCodes(t *testing.T) {
	var p pairingCodes
	now := time.Now()
	code, id, expires := p.add(now)
	if len(code) < 22 || strings.ContainsAny(code, "+/=") {
		t.Errorf("code %q: not 128 bits of URL-safe text", code)
	}
	if !expires.Equal(now.Add(pairingTTL)) {
		t.Errorf("expires = %v", expires)
	}
	if _, ok := p.take("", now); ok {
		t.Error("empty code accepted")
	}
	if _, ok := p.take(code+"x", now); ok {
		t.Error("wrong code accepted")
	}
	if got, ok := p.take(code, now); !ok || got != id {
		t.Errorf("take = %q %v, want %q", got, ok, id)
	}
	if _, ok := p.take(code, now); ok {
		t.Error("code used twice")
	}

	// Expired
	code, _, _ = p.add(now)
	if _, ok := p.take(code, now.Add(pairingTTL)); ok {
		t.Error("expired code accepted")
	}

	// Beyond the cap, the oldest goes
	first, _, _ := p.add(now)
	codes := []string{}
	for range maxPairingCodes {
		c, _, _ := p.add(now)
		codes = append(codes, c)
	}
	if _, ok := p.take(first, now); ok {
		t.Error("the oldest code outlived the cap")
	}
	for _, c := range codes {
		if _, ok := p.take(c, now); !ok {
			t.Error("a recent code was dropped")
		}
	}
}

// pairingServer is a daemon whose remote access is open on the LAN, at
// 192.168.1.10:9443.
func pairingServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	allow, _ := access.ParseAllowList("192.168.1.0/24")
	s := authServer(t, AccessConfig{Mode: access.LAN, RemotePort: 9443, Allow: allow})
	s.LANIP = func() string { return "192.168.1.10" }
	s.remote.addr = ":9443" // as if listening
	return s, s.remoteGuard(s.routes())
}

// newPairing asks for a pairing code from a signed-in remote device, with
// the password again.
func newPairing(t *testing.T, h http.Handler, token string) Pairing {
	t.Helper()
	w := remoteRequest(h, http.MethodPost, "/api/v1/auth/pairing", "192.168.1.5", "",
		map[string]string{"Authorization": "Bearer " + token, confirmHeader: testPassword})
	if w.Code != http.StatusOK {
		t.Fatalf("pairing: %d %s", w.Code, w.Body)
	}
	var p Pairing
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// A signed-in client makes a code (remotely, with the password again); a
// device exchanges it once for a session, which the access log and the
// event stream report.
func TestPairing(t *testing.T) {
	s, h := pairingServer(t)
	token := login(t, h, "192.168.1.5")

	if w := remoteRequest(h, http.MethodPost, "/api/v1/auth/pairing", "192.168.1.5", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("without session: %d", w.Code)
	}
	if w := remoteRequest(h, http.MethodPost, "/api/v1/auth/pairing", "192.168.1.5", "",
		map[string]string{"Authorization": "Bearer " + token}); w.Code != http.StatusPreconditionRequired {
		t.Errorf("unconfirmed: %d", w.Code)
	}
	p := newPairing(t, h, token)
	if p.URL != "https://192.168.1.10:9443" || p.CAFingerprint != "" || p.Expires.IsZero() {
		t.Errorf("pairing = %+v", p)
	}
	qr, err := url.Parse(p.QR)
	if err != nil || qr.Scheme != "frankenmanager" || qr.Host != "pair" {
		t.Fatalf("qr = %q", p.QR)
	}
	if q := qr.Query(); q.Get("v") != "1" || q.Get("url") != p.URL || q.Get("code") != p.Code || q.Get("name") != p.Name || q.Has("ca") {
		t.Errorf("qr query = %v", q)
	}

	events, cancel := s.Bus.Subscribe()
	defer cancel()
	body := `{"code":"` + p.Code + `","device":"Pixel 9"}`
	w := remoteRequest(h, http.MethodPost, "/api/v1/auth/pair", "192.168.1.20", body, map[string]string{"Origin": "capacitor://localhost"})
	if w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != "capacitor://localhost" {
		t.Fatalf("pair: %d %s", w.Code, w.Body)
	}
	var res struct {
		Token   string `json:"token"`
		Session struct {
			ID     string `json:"id"`
			Device string `json:"device"`
		} `json:"session"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.Token == "" || res.Session.Device != "Pixel 9" {
		t.Errorf("pair answer = %s", w.Body)
	}
	if w := remoteRequest(h, http.MethodGet, "/api/v1/auth/sessions", "192.168.1.20", "",
		map[string]string{"Authorization": "Bearer " + res.Token}); w.Code != http.StatusOK {
		t.Errorf("paired session: %d", w.Code)
	}
	select {
	case m := <-events:
		if ev, ok := m.Data.(PairedEvent); m.Type != "paired" || !ok || ev.ID != p.ID || ev.Session != res.Session.ID {
			t.Errorf("event = %+v", m)
		}
	default:
		t.Error("no paired event")
	}

	// Single use
	if w := remoteRequest(h, http.MethodPost, "/api/v1/auth/pair", "192.168.1.20", body, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("reused code: %d", w.Code)
	}
	// Outside DASHBOARD_ALLOWED_IPS, a valid code does not pass either
	p = newPairing(t, h, token)
	if w := remoteRequest(h, http.MethodPost, "/api/v1/auth/pair", "10.0.0.2", `{"code":"`+p.Code+`"}`, nil); w.Code != http.StatusForbidden {
		t.Errorf("address not allowed: %d", w.Code)
	}

	st, _ := s.OpenStore()
	defer func() { _ = st.Close() }()
	log, _ := st.AccessLog(10)
	events2 := []string{}
	for _, e := range log {
		events2 = append(events2, e.Event)
	}
	if got := strings.Join(events2, ","); got != "refused,pair_failed,paired,login" {
		t.Errorf("access log = %s", got)
	}
}

// Wrong codes count toward the lockout of the wrong passwords: once
// locked, even a valid code is refused, and it stays valid.
func TestPairingLockout(t *testing.T) {
	_, h := pairingServer(t)
	token := login(t, h, "192.168.1.5")
	p := newPairing(t, h, token)
	for i := range maxFailures {
		if w := remoteRequest(h, http.MethodPost, "/api/v1/auth/pair", "192.168.1.30", `{"code":"guess"}`, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("guess %d: %d", i, w.Code)
		}
	}
	body := `{"code":"` + p.Code + `","device":"phone"}`
	if w := remoteRequest(h, http.MethodPost, "/api/v1/auth/pair", "192.168.1.30", body, nil); w.Code != http.StatusTooManyRequests {
		t.Errorf("locked: %d", w.Code)
	}
	// The lockout is per address: login is locked too, another device pairs
	if w := remoteRequest(h, http.MethodPost, "/api/v1/auth/login", "192.168.1.30", `{"password":"`+testPassword+`"}`, nil); w.Code != http.StatusTooManyRequests {
		t.Errorf("login after wrong codes: %d", w.Code)
	}
	if w := remoteRequest(h, http.MethodPost, "/api/v1/auth/pair", "192.168.1.31", body, nil); w.Code != http.StatusOK {
		t.Errorf("other address: %d %s", w.Code, w.Body)
	}
}

// No code while the remote access is closed: a phone could not reach it.
// The exchange answers on the remote access only.
func TestPairingRemoteClosed(t *testing.T) {
	s := authServer(t, AccessConfig{Mode: access.Local, RemotePort: 9443})
	s.LANIP = func() string { return "192.168.1.10" }
	h := s.routes() // the socket of the CLI
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/auth/pairing", nil))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "DASHBOARD_ACCESS") {
		t.Errorf("remote closed: %d %s", w.Code, w.Body)
	}

	// Open in the settings, but not listening (no password, no certificate)
	s.accessCfg.Store(&AccessConfig{Mode: access.LAN, RemotePort: 9443})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/auth/pairing", nil))
	if w.Code != http.StatusConflict {
		t.Errorf("remote not running: %d %s", w.Code, w.Body)
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/auth/pair", strings.NewReader(`{"code":"x"}`)))
	if w.Code != http.StatusNotFound {
		t.Errorf("pair off the remote access: %d", w.Code)
	}
}
