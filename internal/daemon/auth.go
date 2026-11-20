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
	remote  bool // through the remote access (LAN, Internet, tunnel)
	ip      string
	session *store.Session
	token   string // the session token, to sign out
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
		token := sessionToken(r, rq.remote)
		if token == "" && rq.remote && streamPath(r) {
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
// the Origin must be the address itself, a session is required, and the
// destructive actions need the password again (X-FM-Password).
func (s *Server) remoteGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		cfg := s.access()
		addr := clientAddr(r)
		rq := requester{remote: true, ip: addr.String()}
		if !access.Remote(cfg.Mode) || (cfg.Mode == access.LAN && !access.Private(addr)) || !cfg.Allow.Allows(addr) {
			s.logAccess(rq, "refused", r.UserAgent())
			writeError(w, http.StatusForbidden, i18n.T("This address may not reach the dashboard."))
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !strings.EqualFold(strings.TrimPrefix(origin, "https://"), r.Host) {
			writeError(w, http.StatusForbidden, i18n.T("Forbidden origin."))
			return
		}
		s.requireSession(w, r, rq, next)
	})
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
