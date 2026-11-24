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
// routes in the page itself; the static files (spaFallback) answer 404.
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
		if !spaFallback(name) {
			writeError(w, http.StatusNotFound, i18n.T("Not found."))
			return
		}
		name = "index.html"
	}
	switch {
	case name == "index.html":
		// Checked on every load: the browser finds a new version of the
		// pages (and of the installed app) as soon as the daemon serves it
		h.Set("Cache-Control", "no-cache")
	case name == serviceWorker:
		// Set here, not guessed from the extension: the system's MIME table
		// says application/javascript on some Linux systems
		h.Set("Content-Type", "text/javascript; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
	case name == webManifest:
		h.Set("Content-Type", "application/manifest+json")
		h.Set("Cache-Control", "no-cache")
	case strings.HasPrefix(name, "assets/"):
		// Hashed by the build: a new version has new names
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFileFS(w, r, s.UI, name)
}

// The service worker and the web app manifest of the installable
// dashboard (PWA), at the root of the pages: the service worker's scope is
// then the whole dashboard, without a Service-Worker-Allowed header.
const (
	serviceWorker = "sw.js"
	webManifest   = "manifest.webmanifest"
)

// spaFallback reports whether a missing file answers index.html, the
// dashboard routing in the page. The files the browser loads as such do
// not: a service worker, a manifest, a script or an icon replaced by the
// page would be installed, cached or run in their place.
func spaFallback(name string) bool {
	return name != serviceWorker && name != webManifest &&
		!strings.HasPrefix(name, "assets/") && !strings.HasPrefix(name, "icons/")
}
