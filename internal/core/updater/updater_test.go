package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/command/commandtest"
)

// fakeHTTP answers requests from memory: no socket is ever opened.
type fakeHTTP struct {
	responses map[string]*http.Response
	requests  []*http.Request
}

func (f *fakeHTTP) RoundTrip(req *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, req)
	res, ok := f.responses[req.URL.String()]
	if !ok {
		return &http.Response{StatusCode: 404, Status: "404 Not Found", Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	return res, nil
}

func respond(code int, body []byte) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(bytes.NewReader(body))}
}

const (
	apiURL      = "https://api.test/releases/latest"
	downloadURL = "https://dl.test/frankenmanager-macos-arm64.tar.gz"
)

func releaseJSON(tag string) []byte {
	return []byte(`{"tag_name": "` + tag + `", "name": "FrankenManager ` + tag + `", "assets": [
		{"name": "frankenmanager-linux-x86_64.tar.gz", "browser_download_url": "https://dl.test/linux"},
		{"name": "frankenmanager-macos-arm64.tar.gz", "browser_download_url": "` + downloadURL + `"}]}`)
}

// entry is a file of a test archive; a link when link is set.
type entry struct {
	name, body, link string
	mode             int64
	dir              bool
}

func archive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		switch {
		case e.dir:
			hdr.Typeflag, hdr.Size = tar.TypeDir, 0
		case e.link != "":
			hdr.Typeflag, hdr.Linkname, hdr.Size = tar.TypeSymlink, e.link, 0
		}
		if hdr.Mode == 0 {
			hdr.Mode = 0o644
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// newUpdater returns an Updater on macOS arm64, version 1.2.3, installed in
// a temporary <dir>/lib/frankenmanager, with a fake GitHub.
func newUpdater(t *testing.T) (*Updater, *fakeHTTP, *commandtest.Log) {
	t.Helper()
	dir := t.TempDir()
	install := filepath.Join(dir, "lib", BundleName)
	if err := os.MkdirAll(install, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &fakeHTTP{responses: map[string]*http.Response{}}
	log := &commandtest.Log{}
	u := &Updater{
		Version: "1.2.3", GOOS: "darwin", GOARCH: "arm64",
		DataDir: filepath.Join(dir, "data"),
		Exe:     filepath.Join(install, BundleName),
		Client:  &http.Client{Transport: f},
		APIURL:  apiURL,
		Log:     log,
		Now:     func() time.Time { return time.Unix(1_800_000_000, 0) },
		Spawn: func(string, ...string) error {
			t.Error("unexpected spawn")
			return nil
		},
		RunSync: func(context.Context, string) (int, error) {
			t.Error("unexpected sync")
			return 0, nil
		},
	}
	return u, f, log
}

func TestArchiveName(t *testing.T) {
	for _, tc := range []struct {
		goos, goarch string
		arm64        bool
		want         string
	}{
		// The names the previous updater looks for
		{"darwin", "arm64", true, "frankenmanager-macos-arm64.tar.gz"},
		{"darwin", "amd64", false, "frankenmanager-macos-x86_64.tar.gz"},
		{"linux", "amd64", false, "frankenmanager-linux-x86_64.tar.gz"},
		{"linux", "arm64", true, "frankenmanager-linux-arm64.tar.gz"},
		// Rosetta: amd64 binary on Apple Silicon
		{"darwin", "amd64", true, "frankenmanager-macos-arm64.tar.gz"},
		// New targets
		{"linux", "arm", false, "frankenmanager-linux-armv7.tar.gz"},
		{"windows", "amd64", false, "frankenmanager-windows-x86_64.tar.gz"},
		{"windows", "arm64", true, "frankenmanager-windows-arm64.tar.gz"},
	} {
		if got, err := ArchiveName(tc.goos, tc.goarch, tc.arm64); got != tc.want || err != nil {
			t.Errorf("%s/%s: %q %v", tc.goos, tc.goarch, got, err)
		}
	}
	if _, err := ArchiveName("freebsd", "amd64", false); err == nil || err.Error() != "Unsupported platform: freebsd-amd64" {
		t.Errorf("err = %v", err)
	}
}

// The expected results in testdata/newer.json are the reference behaviour
// of the updater of earlier releases.
func TestNewerMatchesReference(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "newer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Latest, Current string
		Newer           bool
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if got := Newer(c.Latest, c.Current); got != c.Newer {
			t.Errorf("Newer(%q, %q) = %v, reference %v", c.Latest, c.Current, got, c.Newer)
		}
	}
}

func TestFetch(t *testing.T) {
	u, f, _ := newUpdater(t)
	f.responses[apiURL] = respond(200, releaseJSON("v1.3.0"))
	r, err := u.Fetch(context.Background())
	want := Release{Version: "1.3.0", DownloadURL: downloadURL, Name: "FrankenManager v1.3.0"}
	if err != nil || r != want {
		t.Fatalf("got %+v, %v", r, err)
	}
	h := f.requests[0].Header
	if h.Get("Accept") != "application/vnd.github.v3+json" || h.Get("User-Agent") != "FrankenManager" {
		t.Errorf("headers = %v", h)
	}

	for body, msg := range map[string]string{
		"{":                    "GitHub returned an unreadable response (",
		`{"name": "x"}`:        "Unexpected release format from GitHub (missing 'tag_name').",
		`{"tag_name": "v1.4"}`: "The latest release (1.4) has no build for this platform (frankenmanager-macos-arm64.tar.gz).",
	} {
		f.responses[apiURL] = respond(200, []byte(body))
		if _, err := u.Fetch(context.Background()); err == nil || !strings.HasPrefix(err.Error(), msg) {
			t.Errorf("%s: %v", body, err)
		}
	}
	f.responses[apiURL] = respond(403, nil)
	if _, err := u.Fetch(context.Background()); err == nil || err.Error() != "Could not reach GitHub (Forbidden)." {
		t.Errorf("403: %v", err)
	}
}

func TestNotify(t *testing.T) {
	u, _, log := newUpdater(t)
	var spawned [][]string
	u.Spawn = func(exe string, args ...string) error {
		spawned = append(spawned, append([]string{exe}, args...))
		return nil
	}

	// No cache: a refresh is spawned, attempted_at recorded, nothing shown
	u.Notify()
	if len(spawned) != 1 || !slices.Equal(spawned[0], []string{u.Exe, "--no-update-check", RefreshCommand}) {
		t.Errorf("spawned = %v", spawned)
	}
	if c := u.readCache(); c.AttemptedAt != 1_800_000_000 || c.Latest != nil {
		t.Errorf("cache = %+v", c)
	}
	// Retried at most once per RetryDelay
	u.Notify()
	if len(spawned) != 1 || len(log.Lines) != 0 {
		t.Errorf("spawned %v, log %v", spawned, log.Lines)
	}

	// A cache written by an earlier release: fresh, with a newer release
	py := `{"checked_at": 1799999000.5, "attempted_at": 1799999000.5, "latest": ` +
		`{"version": "1.3.0", "download_url": "u", "name": "n"}}`
	if err := os.WriteFile(u.cachePath(), []byte(py), 0o644); err != nil {
		t.Fatal(err)
	}
	u.Notify()
	want := []string{
		"warning: A new version is available: 1.3.0 (current: 1.2.3)",
		"info: Run 'sudo frankenmanager update' to update.",
	}
	if len(spawned) != 1 || !slices.Equal(log.Lines, want) {
		t.Errorf("spawned %v, log %v", spawned, log.Lines)
	}

	// A development build stays silent
	log.Lines, u.Version = nil, "dev"
	u.Notify()
	if len(log.Lines) != 0 {
		t.Errorf("dev build: %v", log.Lines)
	}
}

func TestCheck(t *testing.T) {
	u, f, _ := newUpdater(t)
	f.responses[apiURL] = respond(200, releaseJSON("v1.2.3"))
	if r, err := u.Check(context.Background()); r != nil || err != nil {
		t.Errorf("same version: %v %v", r, err)
	}
	c := u.readCache()
	if c.CheckedAt != 1_800_000_000 || c.Latest == nil || c.Latest.Version != "1.2.3" {
		t.Errorf("cache = %+v", c)
	}
	f.responses[apiURL] = respond(200, releaseJSON("v1.2.4"))
	if r, err := u.Check(context.Background()); r == nil || r.Version != "1.2.4" || err != nil {
		t.Errorf("newer: %v %v", r, err)
	}
}

// installed lists the install directory: name -> content ("<dir>" or
// "-> target" for links).
func installed(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case d.Type()&os.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			got[rel] = "-> " + target
		case d.IsDir():
			got[rel] = "<dir>"
		default:
			data, _ := os.ReadFile(p)
			got[rel] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUpdate(t *testing.T) {
	u, f, log := newUpdater(t)
	dir := filepath.Dir(u.Exe)
	writeFiles(t, dir, map[string]string{
		BundleName: "old exe",
		"FrankenManager.app/Contents/MacOS/agent":     "old agent",
		"FrankenManager.app/Contents/Resources/stale": "old",
		"notes.txt": "kept",
	})
	f.responses[apiURL] = respond(200, releaseJSON("v1.3.0"))
	f.responses[downloadURL] = respond(200, archive(t,
		entry{name: "frankenmanager/", dir: true},
		entry{name: "frankenmanager/frankenmanager", body: "new exe", mode: 0o4777},
		entry{name: "frankenmanager/FrankenManager.app/Contents/MacOS/agent", body: "new agent", mode: 0o755},
		entry{name: "frankenmanager/FrankenManager.app/Contents/Info.plist", body: "plist"},
		entry{name: "frankenmanager/FrankenManager.app/Contents/Current", link: "MacOS"},
	))
	var synced string
	u.RunSync = func(_ context.Context, exe string) (int, error) {
		synced = exe
		return 0, nil
	}

	if !u.Update(context.Background(), false) {
		t.Fatalf("update failed: %v", log.Lines)
	}
	want := map[string]string{
		BundleName:                                "new exe",
		"FrankenManager.app":                      "<dir>",
		"FrankenManager.app/Contents":             "<dir>",
		"FrankenManager.app/Contents/MacOS":       "<dir>",
		"FrankenManager.app/Contents/MacOS/agent": "new agent",
		"FrankenManager.app/Contents/Info.plist":  "plist",
		"FrankenManager.app/Contents/Current":     "-> MacOS",
		"notes.txt":                               "kept",
	}
	if got := installed(t, dir); !mapsEqual(got, want) {
		t.Errorf("installed:\n%v", got)
	}
	if info, _ := os.Stat(u.Exe); info.Mode().Perm() != 0o755 {
		t.Errorf("exe mode = %v", info.Mode())
	}
	if synced != u.Exe {
		t.Errorf("synced with %q", synced)
	}
	wantLog := []string{
		"info: Checking for updates...",
		"info: Current version: 1.2.3",
		"info: Latest version:  1.3.0",
		"info: Updating to version 1.3.0...",
		"info: Downloading from " + downloadURL + "...",
		"success: Successfully updated to version 1.3.0!",
		"info: Static files (Dockerfile, compose and Caddy templates, php.ini) updated.",
		"info: Please restart FrankenManager to use the new version.",
	}
	if !slices.Equal(log.Lines, wantLog) {
		t.Errorf("log:\n%s", strings.Join(log.Lines, "\n"))
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestUpdateUpToDateAndDevBuild(t *testing.T) {
	u, f, log := newUpdater(t)
	f.responses[apiURL] = respond(200, releaseJSON("v1.2.3"))
	if !u.Update(context.Background(), false) || log.Lines[len(log.Lines)-1] != "success: Already on the latest version!" {
		t.Errorf("log = %v", log.Lines)
	}

	log.Lines, u.Version = nil, "dev"
	want := []string{
		"warning: Self-update is only available for pre-compiled binaries.",
		"info: Download the latest binary from GitHub releases.",
	}
	if u.Update(context.Background(), true) || !slices.Equal(log.Lines, want) {
		t.Errorf("dev: %v", log.Lines)
	}
}

// A bad archive leaves the installation untouched.
func TestUpdateRejectsBadArchives(t *testing.T) {
	for name, tc := range map[string]struct {
		entries []entry
		err     string
	}{
		"no executable": {
			[]entry{{name: "frankenmanager/other", body: "x"}},
			"error: Update failed: Release archive does not contain frankenmanager/frankenmanager",
		},
		"parent path": {
			[]entry{{name: "frankenmanager/../../evil", body: "x"}},
			"error: Update failed: unsafe path in release archive: frankenmanager/../../evil",
		},
		"absolute link": {
			[]entry{{name: "frankenmanager/frankenmanager", link: "/bin/sh"}},
			"error: Update failed: unsafe link in release archive: frankenmanager/frankenmanager -> /bin/sh",
		},
		"escaping link": {
			[]entry{{name: "frankenmanager/x", link: "../../.."}},
			"error: Update failed: unsafe link in release archive: frankenmanager/x -> ../../..",
		},
	} {
		t.Run(name, func(t *testing.T) {
			u, f, log := newUpdater(t)
			dir := filepath.Dir(u.Exe)
			writeFiles(t, dir, map[string]string{BundleName: "old exe"})
			f.responses[apiURL] = respond(200, releaseJSON("v1.3.0"))
			f.responses[downloadURL] = respond(200, archive(t, tc.entries...))
			if u.Update(context.Background(), false) {
				t.Fatal("update succeeded")
			}
			if last := log.Lines[len(log.Lines)-1]; last != tc.err {
				t.Errorf("log = %v", log.Lines)
			}
			if got := installed(t, dir); !mapsEqual(got, map[string]string{BundleName: "old exe"}) {
				t.Errorf("installed: %v", got)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil")); err == nil {
				t.Error("file written outside")
			}
		})
	}
}

func TestUpdateDownloadAndPermissionErrors(t *testing.T) {
	u, f, log := newUpdater(t)
	f.responses[apiURL] = respond(200, releaseJSON("v1.3.0"))
	f.responses[downloadURL] = respond(404, nil)
	if u.Update(context.Background(), false) || log.Lines[len(log.Lines)-1] != "error: Download failed: HTTP Error 404: Not Found" {
		t.Errorf("log = %v", log.Lines)
	}

	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := filepath.Dir(u.Exe)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	log.Lines = nil
	f.responses[apiURL] = respond(200, releaseJSON("v1.3.0"))
	if u.Update(context.Background(), false) ||
		log.Lines[len(log.Lines)-1] != "error: Permission denied. Try running with sudo/administrator privileges." {
		t.Errorf("log = %v", log.Lines)
	}
}

// A failed swap puts back what it had already replaced.
func TestSwapRollback(t *testing.T) {
	dir := t.TempDir()
	bundle, target := filepath.Join(dir, "bundle"), filepath.Join(dir, "install")
	writeFiles(t, bundle, map[string]string{"a": "new a", "b/file": "new b"})
	writeFiles(t, target, map[string]string{"a": "old a", "b": "old b is a file"})
	// "b" cannot replace: its .old name is taken by a non-empty directory
	// that RemoveAll cannot remove.
	writeFiles(t, target, map[string]string{"b.old/locked/x": ""})
	locked := filepath.Join(target, "b.old", "locked")
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	if err := swap(bundle, target); err == nil {
		t.Fatal("swap succeeded")
	}
	got := installed(t, target)
	if got["a"] != "old a" || got["b"] != "old b is a file" {
		t.Errorf("not rolled back: %v", got)
	}
}

func TestSyncResourcesExitCodes(t *testing.T) {
	u, _, log := newUpdater(t)
	writeFiles(t, filepath.Dir(u.Exe), map[string]string{BundleName: "exe"})
	for code, want := range map[int][]string{
		0: {"info: Static files (Dockerfile, compose and Caddy templates, php.ini) updated."},
		3: nil,
		1: {"warning: Could not update the static files now: they will be on the next command."},
	} {
		log.Lines = nil
		u.RunSync = func(context.Context, string) (int, error) { return code, nil }
		u.syncResources(context.Background(), u.Exe)
		if !slices.Equal(log.Lines, want) {
			t.Errorf("code %d: %v", code, log.Lines)
		}
	}
	log.Lines = nil
	u.syncResources(context.Background(), u.Exe+"-missing")
	if !slices.Equal(log.Lines, []string{"warning: Static files will be updated on the next command."}) {
		t.Errorf("missing: %v", log.Lines)
	}
}
