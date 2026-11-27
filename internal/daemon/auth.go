package daemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/access"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// AccessConfig is the access to the dashboard, read from the settings.
type AccessConfig struct {
	Mode         string // access.Local, LAN or Internet
	RemotePort   int
	PasswordHash string
	Allow        access.AllowList
	LocalAuth    bool
	// TLSCert and TLSKey are the certificate of the remote access, empty
	// to generate one
	TLSCert, TLSKey string
	// DashboardOrigins are the pages of this machine that sign in at the
	// HTTPS listener on 127.0.0.1 without the password
	// (https://frankenmanager.test, see the dashboard package)
	DashboardOrigins []string
}

// PasswordSet reports whether the dashboard has a password.
func (c AccessConfig) PasswordSet() bool { return c.PasswordHash != "" }

// Timings of the sessions and of the protection against guessing.
var (
	// sessionIdle is how long an unused session stays valid.
	sessionIdle = 30 * 24 * time.Hour
	// touchEvery bounds the writes of the last use of a session.
	touchEvery = time.Minute
	// maxFailures wrong passwords from an address lock it for lockFor.
	maxFailures = 5
	lockFor     = 15 * time.Minute
)

// sessionCookie is the cookie of a browser session.
const sessionCookie = "fm_session"

// The headers of the dashboard requests.
const (
	// confirmHeader carries the password confirming a destructive action
	// requested remotely.
	confirmHeader = "X-FM-Password"
	// sessionHeader carries the session on the local access, whose
	// Authorization header holds the daemon token.
	sessionHeader = "X-FM-Session"
)

// requester is who sends a request, as the guards found it.
type requester struct {
	remote bool // through the remote access (LAN, Internet, tunnel)
	// dashboard: through the HTTPS listener on 127.0.0.1, the dashboard of
	// this machine (see dashboardGuard)
	dashboard bool
	ip        string
	session   *store.Session
	token     string // the session token, to sign out
}

type requesterKey struct{}

// requesterOf returns who sends r: the socket and the unguarded handler
// give a local requester without session.
func requesterOf(r *http.Request) requester {
	if rq, ok := r.Context().Value(requesterKey{}).(requester); ok {
		return rq
	}
	return requester{}
}

func withRequester(r *http.Request, rq requester) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), requesterKey{}, rq))
}

// clientAddr is the address the connection comes from (no proxy header
// is trusted).
func clientAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, _ := netip.ParseAddr(host)
	return a.Unmap()
}

// --- Guessing protection -----------------------------------------------------

// limiter locks an address after too many wrong passwords.
type limiter struct {
	mu    sync.Mutex
	fails map[string]*failures
}

type failures struct {
	count int
	until time.Time
}

// locked reports whether ip may not try a password now.
func (l *limiter) locked(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.fails[ip]
	return f != nil && now.Before(f.until)
}

// failed counts a wrong password of ip.
func (l *limiter) failed(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fails == nil {
		l.fails = map[string]*failures{}
	}
	f := l.fails[ip]
	if f == nil || (!f.until.IsZero() && now.After(f.until)) {
		f = &failures{}
		l.fails[ip] = f
	}
	f.count++
	if f.count >= maxFailures {
		f.until = now.Add(lockFor)
	}
}

// succeeded forgets the failures of ip.
func (l *limiter) succeeded(ip string) {
	l.mu.Lock()
	delete(l.fails, ip)
	l.mu.Unlock()
}

// --- Sessions ----------------------------------------------------------------

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// sessionToken returns the session token a request carries: the cookie,
// the X-FM-Session header, or (remote access) the Authorization header.
func sessionToken(r *http.Request, bearer bool) string {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		return c.Value
	}
	if v := r.Header.Get(sessionHeader); v != "" {
		return v
	}
	if bearer {
		return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	return ""
}

// session returns the valid session of token, nil when none.
func (s *Server) session(token, ip string) *store.Session {
	if token == "" || s.OpenStore == nil {
		return nil
	}
	st, err := s.OpenStore()
	if err != nil {
		return nil
	}
	defer func() { _ = st.Close() }()
	sess, ok, err := st.SessionByToken(hashToken(token))
	if err != nil || !ok {
		return nil
	}
	now := time.Now()
	if now.Sub(sess.LastSeen) > sessionIdle {
		_, _ = st.RemoveSession(sess.ID)
		return nil
	}
	if now.Sub(sess.LastSeen) > touchEvery || sess.IP != ip {
		_ = st.TouchSession(sess.ID, ip, now)
	}
	return &sess
}

// logAccess records a connection event.
func (s *Server) logAccess(rq requester, event, device string) {
	if s.OpenStore == nil {
		return
	}
	st, err := s.OpenStore()
	if err != nil {
		return
	}
	defer func() { _ = st.Close() }()
	_ = st.LogAccess(store.AccessEntry{At: time.Now().UTC(), IP: rq.ip, Event: event, Device: device, Remote: rq.remote})
}

// --- Guards ------------------------------------------------------------------

// publicPaths answer without a session: what a client needs before signing
// in.
var publicPaths = map[string]bool{
	"/api/health":        true,
	"/api/v1/auth":       true,
	"/api/v1/auth/login": true,
	"/api/v1/auth/local": true,
	"/api/v1/auth/pair":  true,
}

// streamPaths are the routes a browser opens without custom headers (event
// streams, downloads): the token may come in the query.
func streamPath(r *http.Request) bool {
	p := r.URL.Path
	return r.Method == http.MethodGet && (p == "/api/v1/events" || p == "/api/v1/logs" ||
		strings.HasSuffix(p, "/object") && strings.HasPrefix(p, "/api/v1/s3/buckets/"))
}

// requireSession lets r through with a valid session, or answers 401.
func (s *Server) requireSession(w http.ResponseWriter, r *http.Request, rq requester, next http.Handler) {
	if !publicPaths[r.URL.Path] {
		// The remote access and the dashboard listener carry the session in
		// the Authorization header: the daemon token is for 127.0.0.1:PORT
		bearer := rq.remote || rq.dashboard
		token := sessionToken(r, bearer)
		if token == "" && bearer && streamPath(r) {
			token = r.URL.Query().Get("access_token")
		}
		rq.session = s.session(token, rq.ip)
		if rq.session == nil {
			writeError(w, http.StatusUnauthorized, i18n.T("Sign in to the dashboard first."))
			return
		}
		rq.token = token
	}
	next.ServeHTTP(w, withRequester(r, rq))
}

// guard protects the local HTTP access (127.0.0.1): the daemon drives
// Docker and the hosts file, and FrankenManager runs its own DNS server.
// The Host and Origin headers must be the local address (no DNS
// rebinding), and the daemon token is required: in the Authorization
// header, or the access_token query parameter for the streams and
// downloads (a browser sends no header there). With DASHBOARD_LOCAL_AUTH,
// a session is required too.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		port := strconv.Itoa(s.Port)
		allowed := func(host string) bool {
			return strings.EqualFold(host, "127.0.0.1:"+port) || strings.EqualFold(host, "localhost:"+port)
		}
		if !allowed(r.Host) {
			writeError(w, http.StatusForbidden, i18n.T("Forbidden host."))
			return
		}
		if !isAPI(r.URL.Path) {
			// The pages, served at https://frankenmanager.test by the reverse
			// proxy: they reach the API at the HTTPS listener on 127.0.0.1
			s.serveUI(w, r, uiConfig{API: "https://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(s.HTTPSPort)), Mode: "local"})
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !allowed(strings.TrimPrefix(origin, "http://")) {
			writeError(w, http.StatusForbidden, i18n.T("Forbidden origin."))
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" && streamPath(r) {
			token = r.URL.Query().Get("access_token")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.Token)) != 1 {
			writeError(w, http.StatusUnauthorized, i18n.T("Missing or invalid access token."))
			return
		}
		rq := requester{ip: clientAddr(r).String()}
		if cfg := s.access(); cfg.LocalAuth && cfg.PasswordSet() {
			s.requireSession(w, r, rq, next)
			return
		}
		next.ServeHTTP(w, withRequester(r, rq))
	})
}

// remoteGuard protects the remote access (HTTPS): the address must be
// allowed by the mode (private networks for lan) and DASHBOARD_ALLOWED_IPS,
// the Origin must be the address itself or a client of corsOrigin (signed
// in with a Bearer token only), a session is required, and the destructive
// actions need the password again (X-FM-Password).
func (s *Server) remoteGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		cfg := s.access()
		addr := clientAddr(r)
		rq := requester{remote: true, ip: addr.String()}
		origin := r.Header.Get("Origin")
		crossOrigin := origin != "" && !strings.EqualFold(strings.TrimPrefix(origin, "https://"), r.Host)
		if crossOrigin && corsOrigin(origin) {
			// First, so that the client can read every answer, a refusal too
			allowCORS(w, origin)
		}
		if !access.Remote(cfg.Mode) || (cfg.Mode == access.LAN && !access.Private(addr)) || !cfg.Allow.Allows(addr) {
			s.logAccess(rq, "refused", r.UserAgent())
			writeError(w, http.StatusForbidden, i18n.T("This address may not reach the dashboard."))
			return
		}
		if !isAPI(r.URL.Path) && !crossOrigin {
			// The pages of the dashboard, which signs in with the password
			s.serveUI(w, r, uiConfig{Mode: "remote"})
			return
		}
		if crossOrigin {
			if !corsOrigin(origin) {
				writeError(w, http.StatusForbidden, i18n.T("Forbidden origin."))
				return
			}
			if r.Method == http.MethodOptions {
				// Preflight: the browser sends no Authorization header
				w.WriteHeader(http.StatusNoContent)
				return
			}
			// Another origin signs in with its Bearer token only: the cookie
			// of this address, which a browser may still attach (same site,
			// credentials: include), never authenticates it
			r.Header.Del("Cookie")
		}
		s.requireSession(w, r, rq, next)
	})
}

// dashboardGuard protects the HTTPS listener on 127.0.0.1, behind
// https://frankenmanager.test: the page served there signs in at
// POST /api/v1/auth/local without the password, then sends that session as
// Authorization: Bearer. Only a browser of this machine reaches 127.0.0.1,
// and only the dashboard origins are answered (Host and Origin checked, no
// DNS rebinding). With DASHBOARD_LOCAL_AUTH, the password is required here
// too. The requests count as local: no confirmation of the destructive
// actions.
func (s *Server) dashboardGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=31536000")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		port := strconv.Itoa(s.HTTPSPort)
		if !strings.EqualFold(r.Host, "127.0.0.1:"+port) && !strings.EqualFold(r.Host, "localhost:"+port) {
			writeError(w, http.StatusForbidden, i18n.T("Forbidden host."))
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" {
			if !slices.Contains(s.access().DashboardOrigins, origin) {
				writeError(w, http.StatusForbidden, i18n.T("Forbidden origin."))
				return
			}
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Expose-Headers", "Content-Disposition")
			h.Set("Access-Control-Max-Age", "600")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		if !isAPI(r.URL.Path) {
			s.serveUI(w, r, uiConfig{Mode: "local"})
			return
		}
		rq := requester{dashboard: true, ip: clientAddr(r).String()}
		s.requireSession(w, r, rq, next)
	})
}

// corsOrigin reports whether a page of origin may call the remote access
// across origins: the mobile app (Capacitor serves it from
// capacitor://localhost on iOS, https://localhost on Android) or its dev
// server (http://localhost:<port>), and the dashboard of another machine
// (any HTTPS origin), whose instance list manages this one too. They all
// sign in with a Bearer token, the cookie is never used across origins:
// without a session of this machine, a page of any origin reaches nothing
// but the public routes (sign-in, pairing), guarded against guessing.
func corsOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "capacitor", "http":
		return u.Hostname() == "localhost"
	}
	return false
}

// allowCORS answers the CORS headers of a client of corsOrigin, without
// Access-Control-Allow-Credentials.
func allowCORS(w http.ResponseWriter, origin string) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", origin)
	h.Add("Vary", "Origin")
	h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
	h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, "+confirmHeader)
	h.Set("Access-Control-Expose-Headers", "Content-Disposition")
	h.Set("Access-Control-Max-Age", "600")
}

// confirmed reports whether a destructive request may run: always on the
// host; remotely, with the password again in X-FM-Password (counted by
// the guessing protection).
func (s *Server) confirmed(w http.ResponseWriter, r *http.Request) bool {
	rq := requesterOf(r)
	if !rq.remote {
		return true
	}
	now := time.Now()
	if s.limiter.locked(rq.ip, now) {
		writeError(w, http.StatusTooManyRequests, i18n.T("Too many wrong passwords: try again later."))
		return false
	}
	password := r.Header.Get(confirmHeader)
	if password == "" {
		writeError(w, http.StatusPreconditionRequired, i18n.T("Confirm this action with the dashboard password."))
		return false
	}
	if !access.CheckPassword(s.access().PasswordHash, password) {
		s.limiter.failed(rq.ip, now)
		s.logAccess(rq, "confirm_failed", r.UserAgent())
		writeError(w, http.StatusForbidden, i18n.T("Wrong password."))
		return false
	}
	s.limiter.succeeded(rq.ip)
	return true
}
