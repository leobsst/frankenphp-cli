// Package updater checks GitHub for new releases and installs them.
//
// A release archive (frankenmanager-<os>-<arch>.tar.gz, the names earlier
// releases used) holds a frankenmanager/ directory with the executable and,
// on macOS, FrankenManager.app. Each of its entries replaces its counterpart
// in the install directory (/usr/local/lib/frankenmanager) by renaming: the
// old one is moved aside, put back if anything fails, then removed. Renaming
// entries rather than the whole directory works on Windows too (a running
// .exe can be renamed, not its directory), and never swaps a directory the
// binary merely happens to live in.
package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/platform"
	"github.com/leobsst/frankenphp-cli/internal/version"
)

const (
	// Repo is the GitHub repository of the releases.
	Repo = "leobsst/frankenphp-cli"
	// APIURL is the GitHub API endpoint of the latest release.
	APIURL = "https://api.github.com/repos/" + Repo + "/releases/latest"

	// BundleName is the directory inside release archives, and the name of
	// the executable inside it.
	BundleName = "frankenmanager"

	// CacheFile caches the last GitHub lookup in the data directory. A
	// successful lookup is kept CacheTTL; while it is stale, a failed one
	// is retried at most once per RetryDelay.
	CacheFile  = "update_check.json"
	CacheTTL   = 6 * time.Hour
	RetryDelay = 10 * time.Minute

	// RefreshCommand is the hidden command refreshing the cache, spawned
	// detached at startup.
	RefreshCommand = "_refresh-update-cache"
	// SyncResourcesCommand is the hidden command the newly installed binary
	// runs at the end of `update` to resync the data directory's static
	// files; SyncResourcesNothingToDo is its exit code when there is no
	// data directory yet. The previous updater, still running when an
	// existing installation updates to this version, runs the new binary with
	// them: the name and exit code must not change.
	SyncResourcesCommand     = "_sync-resources-internal"
	SyncResourcesTimeout     = 60 * time.Second
	SyncResourcesNothingToDo = 3

	apiTimeout      = 10 * time.Second
	downloadTimeout = 10 * time.Minute
	userAgent       = "FrankenManager"
	// stagingName is the directory, inside the install directory (same
	// filesystem, so the swap is a rename), where the archive is extracted.
	stagingName = ".frankenmanager-update"
	oldSuffix   = ".old"
)

// Logger receives the messages of the update.
type Logger interface {
	Info(msg string)
	Success(msg string)
	Warning(msg string)
	Error(msg string)
}

// Release is a GitHub release, as cached in CacheFile.
type Release struct {
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
	Name        string `json:"name"`
}

// Updater checks for and installs releases. Every field is replaceable in
// tests: none of them touches the network or spawns anything by itself.
type Updater struct {
	Version       string // running version, version.Version
	GOOS, GOARCH  string
	ARM64Hardware bool   // Apple Silicon, even under Rosetta
	DataDir       string // holds CacheFile
	Exe           string // real path of the running executable
	Client        *http.Client
	APIURL        string
	Log           Logger
	Now           func() time.Time
	// Spawn starts exe with args, detached (the cache refresh).
	Spawn func(exe string, args ...string) error
	// RunSync runs exe's SyncResourcesCommand and returns its exit code.
	RunSync func(ctx context.Context, exe string) (int, error)
}

// New returns an Updater for the running binary.
func New(dataDir string, log Logger) *Updater {
	exe, _ := platform.Executable()
	return &Updater{
		Version:       version.Version,
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		ARM64Hardware: platform.ARM64Hardware(),
		DataDir:       dataDir,
		Exe:           exe,
		Client:        http.DefaultClient,
		APIURL:        APIURL,
		Log:           log,
		Now:           time.Now,
		Spawn:         spawn,
		RunSync:       runSync,
	}
}

// ArchiveName is the release archive for a platform. The names of the
// platforms earlier releases supported are kept, otherwise the previous
// updater would not find the new release. A Mac with Apple Silicon gets
// arm64 even when an amd64 binary runs under Rosetta.
func ArchiveName(goos, goarch string, arm64Hardware bool) (string, error) {
	var name string
	switch {
	case goos == "darwin" && (goarch == "arm64" || arm64Hardware):
		name = "macos-arm64"
	case goos == "darwin" && goarch == "amd64":
		name = "macos-x86_64"
	case goos == "linux" && goarch == "amd64":
		name = "linux-x86_64"
	case goos == "linux" && goarch == "arm64":
		name = "linux-arm64"
	case goos == "linux" && goarch == "arm":
		name = "linux-armv7"
	case goos == "windows" && goarch == "amd64":
		name = "windows-x86_64"
	case goos == "windows" && goarch == "arm64":
		name = "windows-arm64"
	default:
		return "", fmt.Errorf("Unsupported platform: %s-%s", goos, goarch) //nolint:staticcheck // user-facing message
	}
	return BundleName + "-" + name + ".tar.gz", nil
}

// releaseRe matches the version formats earlier releases compare (1.2.3,
// v1.2, 1.2.4rc1, 1.2.4.dev5, 1.2.4.post1, 1.2.4+local...), the release
// segment captured.
var releaseRe = regexp.MustCompile(`(?i)^\s*v?(?:\d+!)?(\d+(?:\.\d+)*)` +
	`(?:[-_.]?(?:a|b|c|rc|alpha|beta|pre|preview)[-_.]?\d*)?` +
	`(?:-\d+|[-_.]?(?:post|rev|r)[-_.]?\d*)?` +
	`(?:[-_.]?dev[-_.]?\d*)?` +
	`(?:\+[a-z0-9]+(?:[-_.][a-z0-9]+)*)?\s*$`)

// releaseTuple is the major.minor.micro part of a version: pre-release and
// dev suffixes are ignored, so a dev build compares on the release it builds
// towards, as earlier releases compare them.
func releaseTuple(v string) ([]int, bool) {
	m := releaseRe.FindStringSubmatch(v)
	if m == nil {
		return nil, false
	}
	parts := strings.Split(m[1], ".")
	out := make([]int, 0, 3)
	for _, p := range parts[:min(3, len(parts))] {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// Newer reports whether version latest is newer than current. Unparsable
// versions are never newer. Release segments compare element by element,
// a shorter one first when it is a prefix: 1.2 < 1.2.0, as in earlier
// releases.
func Newer(latest, current string) bool {
	l, ok1 := releaseTuple(latest)
	c, ok2 := releaseTuple(current)
	return ok1 && ok2 && slices.Compare(l, c) > 0
}

// isRelease reports whether the running binary is a release build. A build
// without a version (go build, "dev") neither notifies nor updates itself.
func (u *Updater) isRelease() bool {
	_, ok := releaseTuple(u.Version)
	return ok
}

// newerThanRunning reports whether r is newer than the running version.
// A development build always surfaces the latest release.
func (u *Updater) newerThanRunning(r Release) bool {
	return !u.isRelease() || Newer(r.Version, u.Version)
}

// httpError formats a non-2xx response as its reason phrase (e.g. "Not
// Found"), the status line when the code has none.
func httpError(res *http.Response) string {
	if text := http.StatusText(res.StatusCode); text != "" {
		return text
	}
	return res.Status
}

// Fetch returns the latest release and its archive for this platform.
func (u *Updater) Fetch(ctx context.Context) (Release, error) {
	archive, err := ArchiveName(u.GOOS, u.GOARCH, u.ARM64Hardware)
	if err != nil {
		return Release{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.APIURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", userAgent)
	res, err := u.Client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("Could not reach GitHub (%w).", err) //nolint:staticcheck // user-facing message
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		return Release{}, fmt.Errorf("Could not reach GitHub (%s).", httpError(res)) //nolint:staticcheck // user-facing message
	}

	var data struct {
		TagName *string `json:"tag_name"`
		Name    string  `json:"name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return Release{}, fmt.Errorf("GitHub returned an unreadable response (%w).", err) //nolint:staticcheck // user-facing message
	}
	if data.TagName == nil {
		return Release{}, errors.New("Unexpected release format from GitHub (missing 'tag_name').") //nolint:staticcheck // user-facing message
	}
	r := Release{Version: strings.TrimLeft(*data.TagName, "v"), Name: data.Name}
	for _, a := range data.Assets {
		if a.Name == archive {
			r.DownloadURL = a.URL
			break
		}
	}
	if r.DownloadURL == "" {
		return Release{}, fmt.Errorf("The latest release (%s) has no build for this platform (%s).", r.Version, archive) //nolint:staticcheck // user-facing message
	}
	return r, nil
}

// cache is CacheFile. Times are Unix seconds as floats, the format earlier
// releases write and read in the same file.
type cache struct {
	CheckedAt   float64  `json:"checked_at,omitempty"`
	AttemptedAt float64  `json:"attempted_at,omitempty"`
	Latest      *Release `json:"latest,omitempty"`
}

func (u *Updater) cachePath() string { return filepath.Join(u.DataDir, CacheFile) }

func (u *Updater) readCache() cache {
	var c cache
	data, err := os.ReadFile(u.cachePath())
	if err != nil || json.Unmarshal(data, &c) != nil {
		return cache{}
	}
	return c
}

// writeCache writes the cache atomically: a concurrent reader never sees
// half a file. Under sudo, it may be the first write to the data
// directory: both stay the real user's. Errors are ignored.
func (u *Updater) writeCache(c cache) {
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	p := u.cachePath()
	tmp := fmt.Sprintf("%s.%d.tmp", p, os.Getpid())
	if platform.MakeUserDir(u.DataDir) != nil {
		return
	}
	if os.WriteFile(tmp, data, 0o644) != nil || os.Rename(tmp, p) != nil {
		_ = os.Remove(tmp)
		return
	}
	platform.ChownToSudoUser(p)
}

func (u *Updater) now() float64 { return float64(u.Now().UnixNano()) / 1e9 }

// Refresh fetches the latest release and caches it on success
// (RefreshCommand).
func (u *Updater) Refresh(ctx context.Context) (Release, error) {
	r, err := u.Fetch(ctx)
	if err != nil {
		return Release{}, err
	}
	c := u.readCache()
	c.CheckedAt, c.Latest = u.now(), &r
	u.writeCache(c)
	return r, nil
}

// Check refreshes the cache and returns the latest release when it is
// newer than the running version, nil otherwise (`update --check`).
func (u *Updater) Check(ctx context.Context) (*Release, error) {
	r, err := u.Refresh(ctx)
	if err != nil || !u.newerThanRunning(r) {
		return nil, err
	}
	return &r, nil
}

// Notify announces a newer release known from the cache, before each
// command. It never touches the network: a stale cache is refreshed by a
// detached RefreshCommand, whose result the next command shows. Errors are
// ignored.
func (u *Updater) Notify() {
	if !u.isRelease() {
		return
	}
	c := u.readCache()
	now := u.now()
	if now-c.CheckedAt >= CacheTTL.Seconds() && now-c.AttemptedAt >= RetryDelay.Seconds() {
		c.AttemptedAt = now
		u.writeCache(c)
		if u.Exe != "" {
			_ = u.Spawn(u.Exe, "--no-update-check", RefreshCommand)
		}
	}
	if c.Latest != nil && u.newerThanRunning(*c.Latest) {
		u.Log.Warning(fmt.Sprintf("A new version is available: %s (current: %s)", c.Latest.Version, u.Version))
		if u.GOOS == "windows" {
			u.Log.Info("Run 'frankenmanager update' in an administrator terminal to update.")
		} else {
			u.Log.Info("Run 'sudo frankenmanager update' to update.")
		}
	}
}

// Update installs the latest release (or reinstalls it, with force), then
// has the new binary resync the static files. It returns false on failure.
func (u *Updater) Update(ctx context.Context, force bool) bool {
	if !u.isRelease() {
		u.Log.Warning("Self-update is only available for pre-compiled binaries.")
		u.Log.Info("Download the latest binary from GitHub releases.")
		return false
	}

	u.Log.Info("Checking for updates...")
	latest, err := u.Refresh(ctx)
	if err != nil {
		u.Log.Error(err.Error())
		return false
	}
	u.Log.Info("Current version: " + u.Version)
	u.Log.Info("Latest version:  " + latest.Version)
	if !force && !Newer(latest.Version, u.Version) {
		u.Log.Success("Already on the latest version!")
		return true
	}

	u.Log.Info(fmt.Sprintf("Updating to version %s...", latest.Version))
	if u.Exe == "" {
		u.Log.Error("Update failed: cannot locate the running executable.")
		return false
	}
	dir := filepath.Dir(u.Exe)
	if err := u.install(ctx, latest.DownloadURL, dir); err != nil {
		if !errors.Is(err, errDownload) {
			if errors.Is(err, fs.ErrPermission) {
				u.Log.Error("Permission denied. Try running with sudo/administrator privileges.")
			} else {
				u.Log.Error(fmt.Sprintf("Update failed: %v", err))
			}
		}
		return false
	}

	u.Log.Success(fmt.Sprintf("Successfully updated to version %s!", latest.Version))
	u.syncResources(ctx, filepath.Join(dir, u.exeName()))
	u.Log.Info("Please restart FrankenManager to use the new version.")
	return true
}

func (u *Updater) exeName() string {
	if u.GOOS == "windows" {
		return BundleName + ".exe"
	}
	return BundleName
}

// errDownload marks a download failure, already reported.
var errDownload = errors.New("download failed")

// install downloads and extracts the archive in a staging directory inside
// dir, then swaps every entry of the bundle into dir.
func (u *Updater) install(ctx context.Context, url, dir string) error {
	staging := filepath.Join(dir, stagingName)
	if err := os.RemoveAll(staging); err != nil {
		return err
	}
	if err := os.Mkdir(staging, 0o755); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()

	archive := filepath.Join(staging, "release.tar.gz")
	if !u.download(ctx, url, archive) {
		return errDownload
	}
	bundle, err := ExtractBundle(archive, filepath.Join(staging, "extracted"), u.exeName())
	if err != nil {
		return err
	}
	return swap(bundle, dir)
}

// ExtractBundle unpacks a release archive into dest and returns its bundle
// directory, which must hold the executable exeName ("frankenmanager", or
// "frankenmanager.exe" on Windows).
func ExtractBundle(archive, dest, exeName string) (string, error) {
	if err := extract(archive, dest); err != nil {
		return "", err
	}
	bundle := filepath.Join(dest, BundleName)
	if info, err := os.Stat(filepath.Join(bundle, exeName)); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("Release archive does not contain %s/%s", BundleName, exeName) //nolint:staticcheck // user-facing message
	}
	return bundle, nil
}

// download saves url to dest, logging a failure as "Download failed: ...".
func (u *Updater) download(ctx context.Context, url, dest string) bool {
	u.Log.Info(fmt.Sprintf("Downloading from %s...", url))
	err := func() error {
		ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", userAgent)
		res, err := u.Client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode/100 != 2 {
			return fmt.Errorf("HTTP Error %d: %s", res.StatusCode, httpError(res))
		}
		f, err := os.Create(dest)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, res.Body); err != nil {
			_ = f.Close()
			return err
		}
		return f.Close()
	}()
	if err != nil {
		u.Log.Error(fmt.Sprintf("Download failed: %v", err))
		return false
	}
	return true
}

// extract unpacks a .tar.gz into dest. Everything stays inside dest
// (os.Root): absolute paths, "..", and links pointing outside are refused.
// Modes lose group/other write and setuid.
func extract(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	if err := os.Mkdir(dest, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(hdr.Name)
		if !filepath.IsLocal(filepath.FromSlash(name)) {
			return fmt.Errorf("unsafe path in release archive: %s", hdr.Name)
		}
		if dir := path.Dir(name); dir != "." {
			if err := root.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			perm := fs.FileMode(hdr.Mode).Perm()&^0o022 | 0o600
			out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			if path.IsAbs(hdr.Linkname) || !filepath.IsLocal(filepath.FromSlash(path.Join(path.Dir(name), hdr.Linkname))) {
				return fmt.Errorf("unsafe link in release archive: %s -> %s", hdr.Name, hdr.Linkname)
			}
			if err := root.Symlink(hdr.Linkname, name); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported entry in release archive: %s", hdr.Name)
		}
	}
}

// swap moves every entry of bundle into dir. Each replaced entry is first
// renamed <name>.old; on failure, everything is put back. The .old entries
// are then removed: on Windows the running .exe cannot be, it is removed
// by the next update.
func swap(bundle, dir string) error {
	entries, err := os.ReadDir(bundle)
	if err != nil {
		return err
	}
	type moved struct{ target, old string }
	var done []moved
	rollback := func() {
		for i := len(done) - 1; i >= 0; i-- {
			_ = os.RemoveAll(done[i].target)
			if done[i].old != "" {
				_ = os.Rename(done[i].old, done[i].target)
			}
		}
	}
	for _, e := range entries {
		target := filepath.Join(dir, e.Name())
		old := target + oldSuffix
		_ = os.RemoveAll(old) // left by a previous update (Windows)
		m := moved{target: target}
		if _, err := os.Lstat(target); err == nil {
			if err := os.Rename(target, old); err != nil {
				rollback()
				return err
			}
			m.old = old
		}
		if err := os.Rename(filepath.Join(bundle, e.Name()), target); err != nil {
			if m.old != "" {
				_ = os.Rename(m.old, target)
			}
			rollback()
			return err
		}
		done = append(done, m)
	}
	for _, m := range done {
		if m.old != "" {
			_ = os.RemoveAll(m.old)
		}
	}
	return nil
}

// syncResources has the new binary rewrite the static files right away:
// this one does not know which templates the new version ships. It runs
// as the real user under sudo. On failure, the next command does it.
func (u *Updater) syncResources(ctx context.Context, exe string) {
	if _, err := os.Stat(exe); err != nil {
		u.Log.Warning("Static files will be updated on the next command.")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, SyncResourcesTimeout)
	defer cancel()
	code, err := u.RunSync(ctx, exe)
	switch {
	case err == nil && code == SyncResourcesNothingToDo:
	case err != nil || code != 0:
		u.Log.Warning("Could not update the static files now: they will be on the next command.")
	default:
		u.Log.Info("Static files (Dockerfile, compose and Caddy templates, php.ini) updated.")
	}
}

// syncCommand is exe's SyncResourcesCommand, as the real user under sudo,
// output discarded.
func syncCommand(ctx context.Context, exe string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, exe, "--no-update-check", SyncResourcesCommand)
	platform.AsRealUser(cmd)
	return cmd
}

// runSync runs syncCommand and returns its exit code.
func runSync(ctx context.Context, exe string) (int, error) {
	err := syncCommand(ctx, exe).Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && ctx.Err() == nil {
		return exitErr.ExitCode(), nil
	}
	return 0, err
}

// spawn starts exe detached, without stdio: it outlives this command.
func spawn(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	platform.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
