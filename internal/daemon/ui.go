package daemon

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// isAPI reports whether path belongs to the API; the other paths are the
// pages of the dashboard.
func isAPI(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/")
}

// uiConfig is /config.json: where the page reaches the API. Empty API: the
// page's own origin (the remote access, the HTTPS listener on 127.0.0.1);
// otherwise the HTTPS listener on 127.0.0.1, for the page served at
// https://frankenmanager.test by the reverse proxy.
type uiConfig struct {
	API     string `json:"api"`
	Mode    string `json:"mode"` // local or remote
	Version string `json:"version"`
}

// serveUI answers the pages of the dashboard (s.UI, embedded at build time)
// and their /config.json. Unknown paths answer index.html: the dashboard
// routes in the page itself.
func (s *Server) serveUI(w http.ResponseWriter, r *http.Request, cfg uiConfig) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, i18n.T("Method not allowed."))
		return
	}
	if s.UI == nil {
		writeError(w, http.StatusNotFound, i18n.T("The dashboard is not part of this build (make web)."))
		return
	}
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	if r.URL.Path == "/config.json" {
		cfg.Version = s.Version
		h.Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, cfg)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	if st, err := fs.Stat(s.UI, name); err != nil || st.IsDir() {
		name = "index.html"
	}
	if name == "index.html" {
		h.Set("Cache-Control", "no-cache")
	} else if strings.HasPrefix(name, "assets/") {
		// Hashed by the build: a new version has new names
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFileFS(w, r, s.UI, name)
}
