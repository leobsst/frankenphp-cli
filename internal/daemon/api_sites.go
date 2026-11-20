package daemon

import (
	"context"
	"errors"
	"net/http"

	"github.com/leobsst/frankenphp-cli/internal/core/domains"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// --- Site Caddyfiles ---------------------------------------------------------

// siteRequest is the body of the Caddyfile edits.
type siteRequest struct {
	Content string `json:"content"`
}

func (s *Server) siteFile(w http.ResponseWriter, r *http.Request) {
	s.read(w, func(ws *Workspace) (any, error) { return ws.Domains().SiteFile(r.PathValue("name")) })
}

// validateSite checks a Caddyfile without saving it: 200 when Caddy takes
// it, 422 with Caddy's message otherwise.
func (s *Server) validateSite(w http.ResponseWriter, r *http.Request) {
	var req siteRequest
	if !readJSON(w, r, &req) {
		return
	}
	ws, err := s.Backend(events.NewLog(nil))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = ws.Close() }()
	err = ws.Domains().ValidateSite(r.Context(), r.PathValue("name"), req.Content)
	var invalid *domains.SiteError
	switch {
	case errors.As(err, &invalid):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case err != nil:
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"valid": true})
	}
}

// saveSite checks and saves a Caddyfile, then restarts what serves it.
func (s *Server) saveSite(w http.ResponseWriter, r *http.Request) {
	var req siteRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, i18n.Tf("Missing field '%s'.", "content"))
		return
	}
	host := r.PathValue("name")
	s.submit(w, r, "hosts.caddyfile.save", func(ctx context.Context, ws *Workspace) (any, error) {
		svc, err := running(ctx, ws, errNotRunning)
		if err != nil {
			return nil, err
		}
		return svc.SaveSite(ctx, host, req.Content)
	})
}

// resetSite writes a Caddyfile from its template again.
func (s *Server) resetSite(w http.ResponseWriter, r *http.Request) {
	host := r.PathValue("name")
	s.submit(w, r, "hosts.caddyfile.reset", func(ctx context.Context, ws *Workspace) (any, error) {
		svc, err := running(ctx, ws, errNotRunning)
		if err != nil {
			return nil, err
		}
		return svc.ResetSite(ctx, host)
	})
}
