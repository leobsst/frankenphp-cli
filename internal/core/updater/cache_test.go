package updater

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/platform"
)

// A failed lookup leaves the cache as it was: caching the failure would
// hide the next release.
func TestFailedRefreshKeepsCache(t *testing.T) {
	u, f, _ := newUpdater(t)
	f.responses[apiURL] = respond(200, releaseJSON("v1.3.0"))
	if _, err := u.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(u.cachePath())

	f.responses[apiURL] = respond(500, nil)
	if _, err := u.Check(context.Background()); err == nil {
		t.Error("no error")
	}
	if after, _ := os.ReadFile(u.cachePath()); string(after) != string(before) {
		t.Errorf("cache changed:\n%s\n->\n%s", before, after)
	}

	// No cache at all after a first failed lookup
	v, f2, _ := newUpdater(t)
	f2.responses[apiURL] = respond(500, nil)
	_, _ = v.Refresh(context.Background())
	if _, err := os.Stat(v.cachePath()); err == nil {
		t.Error("failure cached")
	}
}

// A stale cache holding a newer release: refreshed in the background, and
// the release announced right away. The same version: silent.
func TestNotifyStaleCacheAndSameVersion(t *testing.T) {
	u, _, log := newUpdater(t)
	spawned := 0
	u.Spawn = func(string, ...string) error { spawned++; return nil }
	old := float64(u.Now().Unix()) - CacheTTL.Seconds() - 1
	u.writeCache(cache{CheckedAt: old, AttemptedAt: old, Latest: &Release{Version: "1.3.0"}})
	u.Notify()
	if spawned != 1 || !slices.Equal(log.Lines, []string{
		"warning: A new version is available: 1.3.0 (current: 1.2.3)",
		"info: Run 'sudo frankenmanager update' to update.",
	}) {
		t.Errorf("spawned %d, log %v", spawned, log.Lines)
	}

	log.Lines = nil
	fresh := float64(u.Now().Unix())
	u.writeCache(cache{CheckedAt: fresh, Latest: &Release{Version: "1.2.3"}})
	u.Notify()
	if spawned != 1 || len(log.Lines) != 0 {
		t.Errorf("same version: spawned %d, log %v", spawned, log.Lines)
	}
}

// Under `sudo frankenmanager ...`, the cache may be the first write to the
// data directory: both go back to the real user.
func TestCacheUnderSudoBelongsToTheUser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no sudo")
	}
	u, _, _ := newUpdater(t)
	var owned []string
	old := platform.Chown
	platform.Chown = func(path string, uid, gid int) error {
		if uid != 501 || gid != 20 {
			t.Errorf("chown %s to %d:%d", path, uid, gid)
		}
		owned = append(owned, path)
		return nil
	}
	t.Cleanup(func() { platform.Chown = old })
	t.Setenv("SUDO_UID", "501")
	t.Setenv("SUDO_GID", "20")

	u.writeCache(cache{CheckedAt: 1})
	if want := []string{u.DataDir, u.cachePath()}; !slices.Equal(owned, want) {
		t.Errorf("chowned %v, want %v", owned, want)
	}
}

// The new binary resyncs the static files through its hidden command,
// before any other argument parsing.
func TestSyncCommand(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "frankenmanager")
	cmd := syncCommand(context.Background(), exe)
	if !slices.Equal(cmd.Args, []string{exe, "--no-update-check", "_sync-resources-internal"}) {
		t.Errorf("args = %v", cmd.Args)
	}
	if cmd.Stdout != nil || cmd.Stderr != nil {
		t.Error("output not discarded")
	}
}
