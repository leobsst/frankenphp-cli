// Package resources extracts the embedded resources into the data directory
// and keeps them up to date.
//
// On the first run, every resource is copied and the directory tree is
// created. Afterwards, the static files (never edited by the CLI) are
// rewritten whenever the version changes, so a new release ships its new
// templates. Generated files, user files and per-version php.ini copies are
// never overwritten.
package resources

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	assets "github.com/leobsst/frankenphp-cli"
	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/platform"
	"github.com/leobsst/frankenphp-cli/internal/version"
)

const (
	// InitializedMarker marks a data directory whose resources were extracted.
	InitializedMarker = ".initialized"
	// VersionMarker holds the version the static files were last synced with.
	VersionMarker = ".resources_version"

	// fileMode is the mode of every copied file, whatever the mode of the
	// bundled file (sometimes executable by accident): 0644 everywhere is
	// harmless, the Dockerfile makes the entrypoint executable itself.
	fileMode = 0o644
)

// Logger receives the messages of the extraction.
type Logger interface {
	Info(msg string)
	Warning(msg string)
}

// Extractor copies the embedded resources into a data directory.
type Extractor struct {
	Dir     string   // data directory
	Assets  fs.FS    // embedded resources
	Files   []string // every resource, copied on the first run
	Static  []string // resources rewritten on each version change
	Version string   // current version, stored in VersionMarker
	Log     Logger
}

// New returns an Extractor for dir with the embedded resources and the
// build version.
func New(dir string, log Logger) *Extractor {
	return &Extractor{
		Dir:     dir,
		Assets:  assets.FS,
		Files:   assets.Files,
		Static:  assets.StaticFiles,
		Version: version.Version,
		Log:     log,
	}
}

// Ensure prepares the data directory for a command: the directory tree
// always, the resources on the first run, the static files on a version
// change (ensure_resources_extracted).
func (e *Extractor) Ensure() error {
	if err := e.ensureStructure(); err != nil {
		return err
	}
	if exists(e.path(InitializedMarker)) {
		return e.SyncStatic()
	}

	e.Log.Info("Initializing FrankenManager data directory: " + e.Dir)

	// Docker creates an empty directory when it mounts a file that does not
	// exist: remove those before copying the real files
	if err := e.removeDirsAtFilePaths(); err != nil {
		return err
	}
	for _, name := range e.Files {
		if err := e.copyResource(name); err != nil {
			return err
		}
	}
	if err := e.ensureStructure(); err != nil {
		return err
	}
	for _, v := range phpversions.Supported {
		if err := e.EnsurePHPVersionConfig(v); err != nil {
			return err
		}
	}
	if err := e.writeMarker(InitializedMarker, "1"); err != nil {
		return err
	}
	if err := e.writeMarker(VersionMarker, e.Version); err != nil {
		return err
	}

	e.Log.Info("FrankenManager initialized successfully.")
	return nil
}

// SyncStatic rewrites the static files when VersionMarker differs from the
// current version (_sync_static_resources).
func (e *Extractor) SyncStatic() error {
	if data, err := os.ReadFile(e.path(VersionMarker)); err == nil && strings.TrimSpace(string(data)) == e.Version {
		return nil
	}
	for _, name := range e.Static {
		if err := e.copyResource(name); err != nil {
			return err
		}
	}
	return e.writeMarker(VersionMarker, e.Version)
}

// SyncInstalled rewrites the static files of an already initialized data
// directory (sync_installed_resources), for `_sync-resources-internal`,
// run by `update` with the new binary. It returns false, doing nothing, when
// the directory was never initialized: its first command extracts
// everything anyway.
func (e *Extractor) SyncInstalled() (bool, error) {
	if !exists(e.path(InitializedMarker)) {
		return false, nil
	}
	return true, e.SyncStatic()
}

// EnsurePHPVersionConfig copies php/php.ini and php/php-prod.ini into
// php/<version>/ when missing there (ensure_php_version_config). Existing
// copies are the user's and are never overwritten.
func (e *Extractor) EnsurePHPVersionConfig(phpVersion string) error {
	versionDir := e.path("php", phpVersion)
	if err := platform.MakeUserDir(versionDir); err != nil {
		return err
	}
	for _, name := range []string{"php.ini", "php-prod.ini"} {
		base, target := e.path("php", name), filepath.Join(versionDir, name)
		if exists(target) || !isFile(base) {
			continue
		}
		data, err := os.ReadFile(base)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, fileMode); err != nil {
			return err
		}
		platform.ChownToSudoUser(target)
	}
	return nil
}

// ensureStructure creates the directory tree, with a clear error when a
// previous sudo run left the data directory owned by root.
func (e *Extractor) ensureStructure() error {
	dirs := []string{
		e.Dir,
		e.path("caddy", "sites", "custom"),
		e.path("caddy", "sites", "archive"),
	}
	for _, v := range phpversions.Supported {
		dirs = append(dirs, e.path("caddy", "sites", "php-"+v))
	}
	dirs = append(dirs,
		e.path("caddy", "certs"),
		e.path("caddy", "log", "caddy"),
		e.path("caddy", "log", "php"),
		e.path("caddy", "data"),
		e.path("caddy", "config"),
	)
	for _, v := range phpversions.Supported {
		dirs = append(dirs, e.path("php", v))
	}
	dirs = append(dirs, e.path("database"))

	for _, dir := range dirs {
		if err := platform.MakeUserDir(dir); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return permissionError(err, e.Dir)
			}
			return err
		}
	}
	return nil
}

func permissionError(err error, dataDir string) error {
	name := dataDir
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) && pathErr.Path != "" {
		name = pathErr.Path
	}
	return fmt.Errorf("Permission denied on %s: the FrankenManager data directory is probably "+ //nolint:staticcheck // user-facing message
		"owned by root (created by a previous sudo run). Fix it with: sudo chown -R \"$(whoami)\" \"%s\"",
		name, dataDir)
}

// removeDirsAtFilePaths removes the directories found where a resource file
// is expected (_cleanup_incorrect_directories).
func (e *Extractor) removeDirsAtFilePaths() error {
	paths := []string{
		e.path("caddy", "Caddyfile"),
		e.path("caddy", "Caddyfile.worker"),
		e.path("caddy", "Caddyfile.template"),
		e.path("php", "php.ini"),
		e.path("php", "php-prod.ini"),
	}
	for _, v := range phpversions.Supported {
		paths = append(paths, e.path("php", v, "php.ini"), e.path("php", v, "php-prod.ini"))
	}
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			if err := os.RemoveAll(p); err != nil {
				return err
			}
			e.Log.Warning("Removed incorrectly created directory: " + p)
		}
	}
	return nil
}

// copyResource writes an embedded resource to the same relative path in the
// data directory, replacing any existing file.
func (e *Extractor) copyResource(name string) error {
	data, err := fs.ReadFile(e.Assets, name)
	if err != nil {
		return err
	}
	dst := e.path(filepath.FromSlash(name))
	if err := platform.MakeUserDir(filepath.Dir(dst)); err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, fileMode); err != nil {
		return err
	}
	// WriteFile keeps the mode of an existing file
	if err := os.Chmod(dst, fileMode); err != nil {
		return err
	}
	platform.ChownToSudoUser(dst)
	return nil
}

func (e *Extractor) writeMarker(name, content string) error {
	p := e.path(name)
	if err := os.WriteFile(p, []byte(content), fileMode); err != nil {
		return err
	}
	platform.ChownToSudoUser(p)
	return nil
}

func (e *Extractor) path(elem ...string) string {
	return filepath.Join(append([]string{e.Dir}, elem...)...)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}
