package daemon

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/updater"
)

// fakeGitHub answers the releases lookups from memory: no socket is ever
// opened. Each answer is built on demand, so it can be served again.
type fakeGitHub struct {
	answer func() *http.Response
	calls  int
}

func (f *fakeGitHub) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls++
	return f.answer(), nil
}

func githubAnswer(code int, header http.Header, body string) func() *http.Response {
	return func() *http.Response {
		if header == nil {
			header = http.Header{}
		}
		return &http.Response{StatusCode: code, Header: header, Body: io.NopCloser(strings.NewReader(body))}
	}
}

const releasesURL = "https://api.test/releases"

// releasesJSON lists, newest first, a draft, a pre-release, two releases
// newer than 1.2.3, the installed one and an older one.
const releasesJSON = `[
	{"tag_name": "v1.5.0", "draft": true, "body": "draft"},
	{"tag_name": "v1.4.1-rc.1", "prerelease": true, "body": "rc"},
	{"tag_name": "v1.4.0", "name": "FrankenManager v1.4.0", "body": "## What's changed\r\n\r\n- B",
	 "html_url": "https://github.test/v1.4.0", "published_at": "2026-11-20T10:00:00Z",
	 "assets": [{"name": "frankenmanager-macos-arm64.tar.gz", "browser_download_url": "https://dl.test/1.4.0"}]},
	{"tag_name": "v1.3.0", "name": "FrankenManager v1.3.0", "body": "- A", "published_at": "2026-11-10T10:00:00Z"},
	{"tag_name": "v1.2.3", "body": "- installed"},
	{"tag_name": "v1.2.0", "body": "- old"}
]`

func updateServer(t *testing.T, gh *fakeGitHub, now *time.Time) *Server {
	t.Helper()
	s := New(nil, "secret", 9081, "1.2.3")
	s.Updater = &updater.Updater{
		Version: "1.2.3", GOOS: "darwin", GOARCH: "arm64",
		DataDir:     t.TempDir(),
		Client:      &http.Client{Transport: gh},
		ReleasesURL: releasesURL,
		Now:         func() time.Time { return *now },
	}
	return s
}

func getUpdate(t *testing.T, s *Server, query string) (int, UpdateInfo, string) {
	t.Helper()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/update"+query, nil))
	var info UpdateInfo
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
			t.Fatal(err)
		}
	}
	return w.Code, info, w.Body.String()
}

// The newer stable releases come with their notes, newest first; the
// dashboards polling are served from the cache, ?refresh=true looks up again.
func TestUpdateInfoListsNewerReleasesAndCaches(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	gh := &fakeGitHub{answer: githubAnswer(200, nil, releasesJSON)}
	s := updateServer(t, gh, &now)

	code, info, body := getUpdate(t, s, "")
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, body)
	}
	var versions []string
	for _, r := range info.Releases {
		versions = append(versions, r.Version)
	}
	if info.Version != "1.2.3" || info.Latest == nil || *info.Latest != "1.4.0" || !info.UpdateAvailable ||
		!slices.Equal(versions, []string{"1.4.0", "1.3.0"}) || info.Command != "sudo frankenmanager update" {
		t.Errorf("info = %+v, versions %v", info, versions)
	}
	if got := info.Releases[0].Notes; got != "## What's changed\n\n- B" {
		t.Errorf("notes = %q", got)
	}

	now = now.Add(updateTTL / 2)
	getUpdate(t, s, "")
	if gh.calls != 1 {
		t.Errorf("cached lookup: %d calls", gh.calls)
	}
	getUpdate(t, s, "?refresh=true")
	if gh.calls != 2 {
		t.Errorf("refresh: %d calls", gh.calls)
	}
	now = now.Add(updateTTL)
	getUpdate(t, s, "")
	if gh.calls != 3 {
		t.Errorf("expired cache: %d calls", gh.calls)
	}
}

// GitHub out of reach: the last answer is kept with the error, none at all
// is a 502 with the reason, and the failure is not retried on every poll.
func TestUpdateInfoFailures(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	limited := githubAnswer(403, http.Header{"X-Ratelimit-Remaining": []string{"0"}}, `{"message": "API rate limit exceeded"}`)
	gh := &fakeGitHub{answer: limited}
	s := updateServer(t, gh, &now)

	code, _, body := getUpdate(t, s, "")
	if code != http.StatusBadGateway || !strings.Contains(body, updater.ErrRateLimited.Error()) {
		t.Errorf("status %d: %s", code, body)
	}
	getUpdate(t, s, "")
	if gh.calls != 1 {
		t.Errorf("failure retried at once: %d calls", gh.calls)
	}

	gh.answer = githubAnswer(200, nil, releasesJSON)
	if code, _, body := getUpdate(t, s, "?refresh=true"); code != http.StatusOK {
		t.Fatalf("status %d: %s", code, body)
	}
	gh.answer = githubAnswer(500, nil, "")
	code, info, body := getUpdate(t, s, "?refresh=true")
	if code != http.StatusOK || info.Error == "" || info.Latest == nil || *info.Latest != "1.4.0" {
		t.Errorf("stale answer: status %d: %s", code, body)
	}
}

// A development build never offers to update itself.
func TestUpdateInfoDevelopmentBuild(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := updateServer(t, &fakeGitHub{answer: githubAnswer(200, nil, releasesJSON)}, &now)
	s.Updater.Version = "dev"
	_, info, body := getUpdate(t, s, "")
	if info.UpdateAvailable || len(info.Releases) != 0 || info.Latest == nil || *info.Latest != "1.4.0" {
		t.Errorf("dev build: %s", body)
	}
}
