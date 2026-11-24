package daemon

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/access"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/settings"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// AuthState is the access to the dashboard as a client sees it.
type AuthState struct {
	Mode         string `json:"mode"` // local, lan or internet
	PasswordSet  bool   `json:"password_set"`
	LocalAuth    bool   `json:"local_auth"`
	RemotePort   int    `json:"remote_port"`
	RemoteActive bool   `json:"remote_active"` // the remote access listens
	// Remote tells that this request came through the remote access
	Remote bool `json:"remote"`
	// SessionRequired tells that this client must sign in
	SessionRequired bool `json:"session_required"`
	Authenticated   bool `json:"authenticated"`
}

func (s *Server) authState(w http.ResponseWriter, r *http.Request) {
	cfg, rq := s.access(), requesterOf(r)
	st := AuthState{Mode: cfg.Mode, PasswordSet: cfg.PasswordSet(), LocalAuth: cfg.LocalAuth,
		RemotePort: cfg.RemotePort, RemoteActive: s.remote.running() != "", Remote: rq.remote,
		SessionRequired: rq.remote || rq.dashboard || (cfg.LocalAuth && cfg.PasswordSet())}
	if st.SessionRequired {
		st.Authenticated = s.session(sessionToken(r, rq.remote || rq.dashboard), rq.ip) != nil
	}
	writeJSON(w, http.StatusOK, st)
}

// setSessionCookie gives a browser its session (HttpOnly, SameSite=Strict,
// Secure on the remote access); an empty token removes it.
func setSessionCookie(w http.ResponseWriter, token string, secure bool) {
	c := &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteStrictMode, MaxAge: int(sessionIdle.Seconds())}
	if token == "" {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}

// login signs a device in with the dashboard password: a session cookie
// for a browser, the token in the answer for an app (Authorization:
// Bearer on the remote access).
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		Device   string `json:"device"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	cfg, rq := s.access(), requesterOf(r)
	if !cfg.PasswordSet() {
		writeError(w, http.StatusConflict, i18n.T("The dashboard has no password: set DASHBOARD_PASSWORD first."))
		return
	}
	now := time.Now()
	if s.limiter.locked(rq.ip, now) {
		writeError(w, http.StatusTooManyRequests, i18n.T("Too many wrong passwords: try again later."))
		return
	}
	device := req.Device
	if device == "" {
		device = r.UserAgent()
	}
	if len(device) > 200 {
		device = device[:200]
	}
	if !access.CheckPassword(cfg.PasswordHash, req.Password) {
		s.limiter.failed(rq.ip, now)
		s.logAccess(rq, "login_failed", device)
		writeError(w, http.StatusUnauthorized, i18n.T("Wrong password."))
		return
	}
	s.limiter.succeeded(rq.ip)
	s.openSession(w, rq, device, "login")
}

// openSession records a new session of device and answers its token: a
// cookie for a browser of the remote access or of 127.0.0.1:PORT, the token
// in the answer for an app and for the dashboard listener (Bearer).
func (s *Server) openSession(w http.ResponseWriter, rq requester, device, event string) {
	now := time.Now()
	st, err := s.openStore()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = st.Close() }()
	_ = st.RemoveSessions(now.Add(-sessionIdle))
	token := randomHex(32)
	sess := store.Session{ID: randomHex(8), Device: device, IP: rq.ip, Remote: rq.remote, Created: now.UTC(), LastSeen: now.UTC()}
	if err := st.AddSession(sess, hashToken(token)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logAccess(rq, event, device)
	if !rq.dashboard {
		setSessionCookie(w, token, rq.remote)
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "session": sess})
}

// localLogin signs in the dashboard of this machine without the password:
// only through the HTTPS listener on 127.0.0.1, from a dashboard origin
// (dashboardGuard), which only a browser of this machine reaches. With
// DASHBOARD_LOCAL_AUTH, the password is required: POST /api/v1/auth/login.
func (s *Server) localLogin(w http.ResponseWriter, r *http.Request) {
	rq := requesterOf(r)
	if !rq.dashboard || r.Header.Get("Origin") == "" {
		writeError(w, http.StatusNotFound, i18n.T("Not found."))
		return
	}
	if cfg := s.access(); cfg.LocalAuth && cfg.PasswordSet() {
		writeError(w, http.StatusForbidden, i18n.T("The dashboard password is required on this machine too (DASHBOARD_LOCAL_AUTH)."))
		return
	}
	device := r.UserAgent()
	if len(device) > 200 {
		device = device[:200]
	}
	s.openSession(w, rq, device, "local_login")
}

// logout ends the session of the request.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	rq := requesterOf(r)
	token := rq.token
	if token == "" {
		token = sessionToken(r, rq.remote)
	}
	if sess := s.session(token, rq.ip); sess != nil {
		if st, err := s.openStore(); err == nil {
			_, _ = st.RemoveSession(sess.ID)
			_ = st.Close()
		}
		s.logAccess(rq, "logout", sess.Device)
	}
	setSessionCookie(w, "", rq.remote)
	writeJSON(w, http.StatusOK, struct{}{})
}

// SessionInfo is a signed-in device, Current for the one asking.
type SessionInfo struct {
	store.Session
	Current bool `json:"current"`
}

func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	st, err := s.openStore()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = st.Close() }()
	list, err := st.Sessions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rq := requesterOf(r)
	out := []SessionInfo{}
	for _, sess := range list {
		out = append(out, SessionInfo{Session: sess, Current: rq.session != nil && rq.session.ID == sess.ID})
	}
	writeJSON(w, http.StatusOK, out)
}

// revokeSession signs a device out.
func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	st, err := s.openStore()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = st.Close() }()
	ok, err := st.RemoveSession(r.PathValue("id"))
	switch {
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	case !ok:
		writeError(w, http.StatusNotFound, i18n.T("Session not found."))
	default:
		s.logAccess(requesterOf(r), "revoked", r.PathValue("id"))
		writeJSON(w, http.StatusOK, struct{}{})
	}
}

// accessLog returns the latest connection events (?limit=, 100 by
// default).
func (s *Server) accessLog(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 1000 {
			writeError(w, http.StatusBadRequest, i18n.Tf("Invalid value '%s' for %s.", v, "limit"))
			return
		}
		limit = n
	}
	st, err := s.openStore()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = st.Close() }()
	list, err := st.AccessLog(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		// An empty log is [], not null: the dashboard maps over it
		list = []store.AccessEntry{}
	}
	writeJSON(w, http.StatusOK, list)
}

// setPassword sets or changes the dashboard password. A change needs the
// current one; the first one can only be set on the host. Every device is
// signed out.
func (s *Server) setPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	cfg, rq := s.access(), requesterOf(r)
	if len([]rune(req.Password)) < access.MinPasswordLength {
		writeError(w, http.StatusBadRequest, i18n.Tf("The password needs at least %d characters.", access.MinPasswordLength))
		return
	}
	now := time.Now()
	if cfg.PasswordSet() {
		if s.limiter.locked(rq.ip, now) {
			writeError(w, http.StatusTooManyRequests, i18n.T("Too many wrong passwords: try again later."))
			return
		}
		if !access.CheckPassword(cfg.PasswordHash, req.Current) {
			s.limiter.failed(rq.ip, now)
			s.logAccess(rq, "password_failed", r.UserAgent())
			writeError(w, http.StatusForbidden, i18n.T("Wrong password."))
			return
		}
		s.limiter.succeeded(rq.ip)
	} else if rq.remote {
		writeError(w, http.StatusForbidden, i18n.T("Set the first password on the host."))
		return
	}
	s.submit(w, r, "auth.password", func(ctx context.Context, ws *Workspace) (any, error) {
		d, ok := env.Lookup(env.DashboardPasswordKey)
		if !ok {
			return nil, i18n.Errorf("Unknown setting '%s'.", env.DashboardPasswordKey)
		}
		if _, err := settings.Set(ws.Env, d, &req.Password); err != nil {
			return nil, err
		}
		if err := ws.Store.RemoveSessions(time.Time{}); err != nil {
			return nil, err
		}
		s.logAccess(rq, "password_changed", r.UserAgent())
		ws.Stack.Log.Success(i18n.T("Dashboard password changed: every device is signed out."))
		s.ReloadAccess()
		return map[string]bool{"password_set": true}, nil
	})
}

// HostInfo is the machine the daemon runs on, and how the request reached
// it.
type HostInfo struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	LANIP    string `json:"lan_ip,omitempty"`
	// Remote tells that the request came through the remote access
	Remote bool   `json:"remote"`
	Mode   string `json:"mode"`
	// RemoteURL is where the remote access answers, "" when off
	RemoteURL string `json:"remote_url,omitempty"`
}

func (s *Server) hostInfo(w http.ResponseWriter, r *http.Request) {
	name, _ := os.Hostname()
	cfg := s.access()
	info := HostInfo{Hostname: name, OS: runtime.GOOS, Arch: runtime.GOARCH, Version: s.Version,
		Remote: requesterOf(r).remote, Mode: cfg.Mode}
	if s.LANIP != nil {
		info.LANIP = s.LANIP()
	}
	if s.remote.running() != "" && info.LANIP != "" {
		info.RemoteURL = "https://" + info.LANIP + ":" + strconv.Itoa(cfg.RemotePort)
	}
	writeJSON(w, http.StatusOK, info)
}

// openStore opens the database of the sessions.
func (s *Server) openStore() (*store.Store, error) {
	if s.OpenStore == nil {
		return nil, i18n.NewError("The sessions are not available here.")
	}
	return s.OpenStore()
}
