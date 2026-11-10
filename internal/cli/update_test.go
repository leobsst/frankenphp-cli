package cli

import (
	"bytes"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/updater"
)

// fakeGitHub answers the latest release API from memory: no socket.
type fakeGitHub struct {
	code int
	tag  string
}

func (f fakeGitHub) RoundTrip(*http.Request) (*http.Response, error) {
	body := `{"tag_name": "` + f.tag + `", "name": "FrankenManager ` + f.tag + `", "assets": [
		{"name": "frankenmanager-macos-arm64.tar.gz", "browser_download_url": "https://dl.test/a.tar.gz"}]}`
	return &http.Response{StatusCode: f.code, Body: io.NopCloser(bytes.NewReader([]byte(body)))}, nil
}

func (e *testEnv) fakeUpdater(version string, gh fakeGitHub) {
	e.updater = func(dataDir string, log updater.Logger) *updater.Updater {
		return &updater.Updater{
			Version: version, GOOS: "darwin", GOARCH: "arm64",
			DataDir: dataDir, Exe: filepath.Join(e.root, "lib", "frankenmanager", "frankenmanager"),
			Client: &http.Client{Transport: gh}, APIURL: "https://api.test/latest", Log: log,
			Now: time.Now,
			Spawn: func(string, ...string) error {
				e.t.Error("unexpected spawn")
				return nil
			},
		}
	}
}

func TestUpdateCheck(t *testing.T) {
	e := newEnv(t)
	e.fakeUpdater("1.2.3", fakeGitHub{200, "v1.3.0"})
	code, out, _ := e.run("update", "--check")
	want := "-- Current version: 1.2.3\n-- New version available: 1.3.0\n-- Release: FrankenManager v1.3.0\n"
	if code != 0 || out != want {
		t.Errorf("code %d, out %q", code, out)
	}
	if !exists(filepath.Join(e.dir, updater.CacheFile)) {
		t.Error("cache not written")
	}

	e.fakeUpdater("1.3.0", fakeGitHub{200, "v1.3.0"})
	if code, out, _ := e.run("update", "-c"); code != 0 || out != "-- Current version: 1.3.0\n-- You are on the latest version!\n" {
		t.Errorf("code %d, out %q", code, out)
	}

	e.fakeUpdater("1.3.0", fakeGitHub{403, ""})
	if code, out, _ := e.run("update", "-c"); code != 1 || out != "-- Current version: 1.3.0\nERROR: Could not reach GitHub (Forbidden).\n" {
		t.Errorf("code %d, out %q", code, out)
	}
	// update asks GitHub itself: no startup notice
	if len(e.notified) != 0 {
		t.Errorf("notified %v", e.notified)
	}
}

func TestUpdate(t *testing.T) {
	e := newEnv(t)
	e.fakeUpdater("1.3.0", fakeGitHub{200, "v1.3.0"})
	if code, out, _ := e.run("update"); code != 0 || out != "-- Checking for updates...\n-- Current version: 1.3.0\n"+
		"-- Latest version:  1.3.0\n-- Already on the latest version!\n" {
		t.Errorf("code %d, out %q", code, out)
	}
	e.fakeUpdater("dev", fakeGitHub{200, "v1.3.0"})
	if code, out, _ := e.run("update", "--force"); code != 1 || out != "WARNING: Self-update is only available for pre-compiled binaries.\n"+
		"-- Download the latest binary from GitHub releases.\n" {
		t.Errorf("code %d, out %q", code, out)
	}
}
