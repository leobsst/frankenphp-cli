// Package release builds the release assets: for each target, a
// frankenmanager-<platform>.tar.gz holding the frankenmanager/ bundle (the
// executable, plus FrankenManager.app on macOS), the bare executable for
// old single-file installs, then SHA256SUMS.txt.
//
// The archive names and layout are those of earlier releases: the previous
// updater of an existing installation finds and installs this release
// without a bridge version.
package release

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/dnswatcher"
	"github.com/leobsst/frankenphp-cli/internal/core/updater"
)

// Module is the Go module, whose packages are built.
const Module = "github.com/leobsst/frankenphp-cli"

// Target is a platform to build for.
type Target struct {
	GOOS, GOARCH string
	GOARM        string // "7" for linux/arm
}

// Targets are the 7 release targets: two native macOS binaries (no
// universal one), Linux amd64, arm64 and armv7, Windows amd64 and arm64.
var Targets = []Target{
	{GOOS: "darwin", GOARCH: "amd64"},
	{GOOS: "darwin", GOARCH: "arm64"},
	{GOOS: "linux", GOARCH: "amd64"},
	{GOOS: "linux", GOARCH: "arm64"},
	{GOOS: "linux", GOARCH: "arm", GOARM: "7"},
	{GOOS: "windows", GOARCH: "amd64"},
	{GOOS: "windows", GOARCH: "arm64"},
}

// ParseTarget reads "goos/goarch" (linux/arm means armv7).
func ParseTarget(s string) (Target, error) {
	for _, t := range Targets {
		if t.String() == s {
			return t, nil
		}
	}
	return Target{}, fmt.Errorf("unknown target %q", s)
}

func (t Target) String() string { return t.GOOS + "/" + t.GOARCH }

// Archive is the name of the target's archive, the one the updaters look for.
func (t Target) Archive() string {
	name, err := updater.ArchiveName(t.GOOS, t.GOARCH, false)
	if err != nil {
		panic(err) // every Target has a name
	}
	return name
}

// Bare is the name of the target's bare executable.
func (t Target) Bare() string {
	name := strings.TrimSuffix(t.Archive(), ".tar.gz")
	if t.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// Exe is the executable's name in the bundle.
func (t Target) Exe() string {
	if t.GOOS == "windows" {
		return updater.BundleName + ".exe"
	}
	return updater.BundleName
}

// dir is the target's staging directory name.
func (t Target) dir() string { return t.GOOS + "-" + t.GOARCH }

// InfoPlist is the app's Info.plist, byte for byte the one earlier releases
// shipped (keys sorted).
func InfoPlist(version string) []byte {
	v := plistEscape(version)
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>en</string>
	<key>CFBundleDisplayName</key>
	<string>FrankenManager</string>
	<key>CFBundleExecutable</key>
	<string>` + dnswatcher.MacOSAppExecutable + `</string>
	<key>CFBundleIdentifier</key>
	<string>` + dnswatcher.MacOSAppBundleID + `</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>FrankenManager</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>` + v + `</string>
	<key>CFBundleVersion</key>
	<string>` + v + `</string>
	<key>LSBackgroundOnly</key>
	<true/>
	<key>LSMinimumSystemVersion</key>
	<string>11.0</string>
</dict>
</plist>
`)
}

func plistEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// Builder compiles a package of the module for a target into out.
type Builder func(ctx context.Context, pkg, out string, t Target, version string) error

// GoBuild runs `go build` without cgo, the version stamped, the paths and
// debug information stripped.
func GoBuild(ctx context.Context, pkg, out string, t Target, version string) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath",
		"-ldflags", "-s -w -X "+Module+"/internal/version.Version="+version,
		"-o", out, Module+"/"+pkg)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+t.GOOS, "GOARCH="+t.GOARCH, "GOARM="+t.GOARM)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build %s for %s: %w", pkg, t, err)
	}
	return nil
}

// Stage builds the bundle of t in <dir>/stage/<goos>-<goarch>/frankenmanager
// and returns it. On macOS it holds FrankenManager.app, unsigned: CI signs
// the stage before Pack.
func Stage(ctx context.Context, build Builder, t Target, version, dir string) (string, error) {
	bundle := filepath.Join(dir, "stage", t.dir(), updater.BundleName)
	if err := os.RemoveAll(bundle); err != nil {
		return "", err
	}
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		return "", err
	}
	if err := build(ctx, "cmd/frankenmanager", filepath.Join(bundle, t.Exe()), t, version); err != nil {
		return "", err
	}
	if t.GOOS != "darwin" {
		return bundle, nil
	}
	contents := filepath.Join(bundle, dnswatcher.MacOSAppName, "Contents")
	if err := os.MkdirAll(filepath.Join(contents, "MacOS"), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), InfoPlist(version), 0o644); err != nil {
		return "", err
	}
	agent := filepath.Join(contents, "MacOS", dnswatcher.MacOSAppExecutable)
	if err := build(ctx, "cmd/frankenmanager-agent", agent, t, version); err != nil {
		return "", err
	}
	return bundle, nil
}

// StagedBundle is the staging directory Stage writes for t.
func StagedBundle(dir string, t Target) string {
	return filepath.Join(dir, "stage", t.dir(), updater.BundleName)
}

// Pack writes the archive and the bare executable of a staged bundle into
// <dir>/assets. Every entry gets mtime and root ownership, in name order:
// the archive only depends on the files.
func Pack(bundle string, t Target, dir string, mtime time.Time) error {
	assets := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(bundle, t.Exe()), filepath.Join(assets, t.Bare()), 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(assets, t.Archive()))
	if err != nil {
		return err
	}
	if err := writeArchive(f, bundle, mtime); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func writeArchive(w io.Writer, bundle string, mtime time.Time) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	var paths []string
	err := filepath.WalkDir(bundle, func(p string, _ fs.DirEntry, err error) error {
		paths = append(paths, p)
		return err
	})
	if err != nil {
		return err
	}
	slices.Sort(paths)
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(bundle), p)
		if err != nil {
			return err
		}
		hdr := &tar.Header{
			Name:    filepath.ToSlash(rel),
			ModTime: mtime,
			Format:  tar.FormatPAX,
		}
		switch {
		case info.IsDir():
			hdr.Typeflag, hdr.Name, hdr.Mode = tar.TypeDir, hdr.Name+"/", 0o755
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			hdr.Typeflag, hdr.Linkname, hdr.Mode = tar.TypeSymlink, target, 0o777
		case info.Mode().IsRegular():
			hdr.Typeflag, hdr.Size, hdr.Mode = tar.TypeReg, info.Size(), 0o644
			if info.Mode().Perm()&0o111 != 0 {
				hdr.Mode = 0o755
			}
		default:
			return fmt.Errorf("unsupported file in the bundle: %s", p)
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg {
			if err := copyInto(tw, p); err != nil {
				return err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func copyInto(w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(w, f)
	return err
}

func copyFile(src, dst string, mode fs.FileMode) error {
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if err := copyInto(out, src); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

// Checksums writes <assets>/SHA256SUMS.txt for every other file, in
// `sha256sum` format, sorted by name.
func Checksums(assets string) error {
	entries, err := os.ReadDir(assets)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, e := range entries {
		if !e.Type().IsRegular() || e.Name() == "SHA256SUMS.txt" {
			continue
		}
		f, err := os.Open(filepath.Join(assets, e.Name()))
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		_ = f.Close()
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(h.Sum(nil)), e.Name())
	}
	return os.WriteFile(filepath.Join(assets, "SHA256SUMS.txt"), []byte(b.String()), 0o644)
}
