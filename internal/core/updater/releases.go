package updater

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// ReleasesURL is the GitHub API endpoint listing the releases, newest
// first: the notes of the versions the dashboard announces.
const ReleasesURL = "https://api.github.com/repos/" + Repo + "/releases?per_page=30"

// ErrRateLimited is the error of a lookup GitHub refused for the rate limit
// of its API (60 unauthenticated requests per hour and address).
var ErrRateLimited = i18n.NewError("GitHub API rate limit reached: try again later.")

// Notes is a published release and its notes (the Markdown body of the
// GitHub release).
type Notes struct {
	Version     string    `json:"version"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
	URL         string    `json:"url"`
	Notes       string    `json:"notes"`
}

// Changelog is what a release lookup tells the dashboard.
type Changelog struct {
	// Latest is the latest stable release, nil when there is none
	Latest *Release
	// Available tells whether Latest is newer than the running release. A
	// development build never updates itself: never available
	Available bool
	// Newer are the stable releases newer than the running one, newest
	// first, among the last 30 published
	Newer []Notes
}

// Changelog lists the releases on GitHub (ReleasesURL): the latest stable
// one and the notes of those newer than the running version. Drafts and
// pre-releases are skipped. A latest release with a build for this platform
// also refreshes the cache of the CLI notice (CacheFile).
func (u *Updater) Changelog(ctx context.Context) (Changelog, error) {
	if u.ReleasesURL == "" {
		return Changelog{}, i18n.NewError("Update checks are not available here.")
	}
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.ReleasesURL, nil)
	if err != nil {
		return Changelog{}, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", userAgent)
	res, err := u.Client.Do(req)
	if err != nil {
		return Changelog{}, i18n.Errorf("Could not reach GitHub (%w).", err)
	}
	defer func() { _ = res.Body.Close() }()
	if rateLimited(res) {
		return Changelog{}, ErrRateLimited
	}
	if res.StatusCode/100 != 2 {
		return Changelog{}, i18n.Errorf("Could not reach GitHub (%s).", httpError(res))
	}

	var data []struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		Body        string    `json:"body"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
		Assets      []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return Changelog{}, i18n.Errorf("GitHub returned an unreadable response (%w).", err)
	}
	// No build for this platform: the notes are still worth showing
	archive, _ := ArchiveName(u.GOOS, u.GOARCH, u.ARM64Hardware)

	var out Changelog
	for _, d := range data {
		v := strings.TrimLeft(d.TagName, "v")
		if d.Draft || d.Prerelease || isPreRelease(v) {
			continue
		}
		if _, ok := releaseTuple(v); !ok {
			continue
		}
		if out.Latest == nil || Newer(v, out.Latest.Version) {
			r := Release{Version: v, Name: d.Name}
			for _, a := range d.Assets {
				if archive != "" && a.Name == archive {
					r.DownloadURL = a.URL
					break
				}
			}
			out.Latest = &r
		}
		if u.isRelease() && Newer(v, u.Version) {
			out.Newer = append(out.Newer, Notes{Version: v, Name: d.Name, PublishedAt: d.PublishedAt,
				URL: d.HTMLURL, Notes: strings.ReplaceAll(d.Body, "\r\n", "\n")})
		}
	}
	slices.SortStableFunc(out.Newer, func(a, b Notes) int {
		switch {
		case Newer(a.Version, b.Version):
			return -1
		case Newer(b.Version, a.Version):
			return 1
		}
		return 0
	})
	if out.Latest != nil {
		out.Available = u.isRelease() && Newer(out.Latest.Version, u.Version)
		// What Refresh would cache: the CLI notice needs no lookup of its own
		if out.Latest.DownloadURL != "" {
			c := u.readCache()
			c.CheckedAt, c.Latest = u.now(), out.Latest
			u.writeCache(c)
		}
	}
	return out, nil
}

// rateLimited reports whether GitHub refused the request for its rate limit.
func rateLimited(res *http.Response) bool {
	return res.StatusCode == http.StatusTooManyRequests ||
		res.StatusCode == http.StatusForbidden && res.Header.Get("X-RateLimit-Remaining") == "0"
}
