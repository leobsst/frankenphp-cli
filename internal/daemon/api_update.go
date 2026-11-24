package daemon

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/updater"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// The lookups of GET /api/v1/update are kept updateTTL: the dashboards
// poll, GitHub allows 60 unauthenticated requests per hour. A failed one is
// retried at most once per updateRetry; ?refresh=true always looks up.
var (
	updateTTL   = time.Hour
	updateRetry = 10 * time.Minute
)

// UpdateInfo is GET /api/v1/update: the installed version, the latest
// release and the notes of every version in between.
type UpdateInfo struct {
	Version string `json:"version"`
	// Latest is the latest stable release, null when GitHub lists none
	Latest          *string `json:"latest"`
	UpdateAvailable bool    `json:"update_available"`
	// Releases are the releases newer than Version, newest first
	Releases []updater.Notes `json:"releases"`
	// Command installs the update, in a terminal of the host: it needs the
	// rights of an administrator, which the daemon has not
	Command   string    `json:"command"`
	CheckedAt time.Time `json:"checked_at"`
	// Error is the failure of the last lookup, when an earlier one answers
	Error string `json:"error,omitempty"`
}

// updateCache holds the last lookup of the releases.
type updateCache struct {
	mu       sync.Mutex
	info     *UpdateInfo // last successful lookup
	err      error       // last failure, nil after a success
	failedAt time.Time
}

// updateInfo answers the release lookup, from the cache unless
// ?refresh=true. GitHub unreachable or rate limited without an earlier
// answer: 502 with the reason.
func (s *Server) updateInfo(w http.ResponseWriter, r *http.Request) {
	if s.Updater == nil {
		writeError(w, http.StatusServiceUnavailable, i18n.T("Update checks are not available here."))
		return
	}
	// The lookup is shared by every client: one leaving does not cancel it
	info, err := s.checkUpdate(context.WithoutCancel(r.Context()), r.URL.Query().Get("refresh") == "true")
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// checkUpdate returns the cached lookup while fresh, or looks up GitHub
// (force: always). One lookup at a time: concurrent requests wait for it.
func (s *Server) checkUpdate(ctx context.Context, force bool) (UpdateInfo, error) {
	c := &s.updates
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if s.Updater.Now != nil {
		now = s.Updater.Now()
	}
	if !force {
		if c.info != nil && c.err == nil && now.Sub(c.info.CheckedAt) < updateTTL {
			return *c.info, nil
		}
		if c.err != nil && now.Sub(c.failedAt) < updateRetry {
			return c.stale()
		}
	}
	cl, err := s.Updater.Changelog(ctx)
	if err != nil {
		c.err, c.failedAt = err, now
		return c.stale()
	}
	info := UpdateInfo{Version: s.Version, UpdateAvailable: cl.Available, Releases: cl.Newer,
		Command: updateCommand(s.Updater.GOOS), CheckedAt: now}
	if cl.Latest != nil {
		info.Latest = &cl.Latest.Version
	}
	if info.Releases == nil {
		info.Releases = []updater.Notes{}
	}
	c.info, c.err = &info, nil
	return info, nil
}

// stale returns the last successful lookup with the last error, or the
// error alone when there was none.
func (c *updateCache) stale() (UpdateInfo, error) {
	if c.info == nil {
		return UpdateInfo{}, c.err
	}
	info := *c.info
	info.Error = c.err.Error()
	return info, nil
}

// updateCommand is the command installing the update on goos.
func updateCommand(goos string) string {
	if goos == "windows" {
		return "frankenmanager update"
	}
	return "sudo frankenmanager update"
}
