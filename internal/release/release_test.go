package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/updater"
)

// The reference, testdata/Info.plist, is the one earlier releases shipped.
func TestInfoPlistMatchesReference(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	if got := InfoPlist("1.2.3-rc.1"); string(got) != string(want) {
		t.Errorf("Info.plist differs from the reference:\n%s", got)
	}
}

// The previous updater looks for the archives of its 4 platforms by name.
func TestAssetNames(t *testing.T) {
	want := map[string][2]string{
		"darwin/amd64":  {"frankenmanager-macos-x86_64.tar.gz", "frankenmanager-macos-x86_64"},
		"darwin/arm64":  {"frankenmanager-macos-arm64.tar.gz", "frankenmanager-macos-arm64"},
		"linux/amd64":   {"frankenmanager-linux-x86_64.tar.gz", "frankenmanager-linux-x86_64"},
		"linux/arm64":   {"frankenmanager-linux-arm64.tar.gz", "frankenmanager-linux-arm64"},
		"linux/arm":     {"frankenmanager-linux-armv7.tar.gz", "frankenmanager-linux-armv7"},
		"windows/amd64": {"frankenmanager-windows-x86_64.tar.gz", "frankenmanager-windows-x86_64.exe"},
		"windows/arm64": {"frankenmanager-windows-arm64.tar.gz", "frankenmanager-windows-arm64.exe"},
	}
	if len(Targets) != len(want) {
		t.Fatalf("%d targets", len(Targets))
	}
	for _, target := range Targets {
		w := want[target.String()]
		if target.Archive() != w[0] || target.Bare() != w[1] {
			t.Errorf("%s: %s, %s", target, target.Archive(), target.Bare())
		}
	}
	if arm, _ := ParseTarget("linux/arm"); arm.GOARM != "7" {
		t.Errorf("linux/arm = %+v", arm)
	}
	if _, err := ParseTarget("freebsd/amd64"); err == nil {
		t.Error("unknown target accepted")
	}
}

// fakeBuild writes "<pkg> <target> <version>" instead of compiling.
func fakeBuild(_ context.Context, pkg, out string, t Target, version string) error {
	return os.WriteFile(out, []byte(pkg+" "+t.String()+" "+version), 0o755)
}

func entries(t *testing.T, archive string) []*tar.Header {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var hdrs []*tar.Header
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return hdrs
		}
		if err != nil {
			t.Fatal(err)
		}
		hdrs = append(hdrs, h)
	}
}

func TestStageAndPack(t *testing.T) {
	dir := t.TempDir()
	mtime := time.Unix(1_800_000_000, 0).UTC()
	for _, target := range []Target{{GOOS: "darwin", GOARCH: "arm64"}, {GOOS: "windows", GOARCH: "amd64"}, {GOOS: "linux", GOARCH: "arm", GOARM: "7"}} {
		bundle, err := Stage(context.Background(), fakeBuild, target, "1.2.3", dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := Pack(bundle, target, dir, mtime); err != nil {
			t.Fatal(err)
		}
		archive := filepath.Join(dir, "assets", target.Archive())

		var names []string
		for _, h := range entries(t, archive) {
			names = append(names, h.Name)
			if !h.ModTime.Equal(mtime) || h.Uid != 0 || h.Gid != 0 || h.Uname != "" {
				t.Errorf("%s: %v %d:%d %q", h.Name, h.ModTime, h.Uid, h.Gid, h.Uname)
			}
			if h.Name == "frankenmanager/"+target.Exe() && runtime.GOOS != "windows" && h.Mode != 0o755 { // Windows keeps no execute bit
				t.Errorf("executable mode %o", h.Mode)
			}
		}
		want := []string{"frankenmanager/", "frankenmanager/" + target.Exe()}
		if target.GOOS == "darwin" {
			want = []string{
				"frankenmanager/",
				"frankenmanager/FrankenManager.app/",
				"frankenmanager/FrankenManager.app/Contents/",
				"frankenmanager/FrankenManager.app/Contents/Info.plist",
				"frankenmanager/FrankenManager.app/Contents/MacOS/",
				"frankenmanager/FrankenManager.app/Contents/MacOS/frankenmanager-agent",
				"frankenmanager/frankenmanager",
			}
		}
		if !slices.Equal(names, want) {
			t.Errorf("%s entries:\n%s", target, strings.Join(names, "\n"))
		}

		// The Go updater accepts it, with the executable of the platform
		extracted, err := updater.ExtractBundle(archive, filepath.Join(t.TempDir(), "x"), target.Exe())
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		exe, _ := os.ReadFile(filepath.Join(extracted, target.Exe()))
		if string(exe) != "cmd/frankenmanager "+target.String()+" 1.2.3" {
			t.Errorf("%s exe = %q", target, exe)
		}
		bare, _ := os.ReadFile(filepath.Join(dir, "assets", target.Bare()))
		if string(bare) != string(exe) {
			t.Errorf("%s bare = %q", target, bare)
		}
		if target.GOOS == "darwin" {
			agent, _ := os.ReadFile(filepath.Join(extracted, "FrankenManager.app", "Contents", "MacOS", "frankenmanager-agent"))
			plist, _ := os.ReadFile(filepath.Join(extracted, "FrankenManager.app", "Contents", "Info.plist"))
			if string(agent) != "cmd/frankenmanager-agent darwin/arm64 1.2.3" || string(plist) != string(InfoPlist("1.2.3")) {
				t.Errorf("app: %q", agent)
			}
		}
	}

	// Same files, same archive
	target := Targets[1]
	first, _ := os.ReadFile(filepath.Join(dir, "assets", target.Archive()))
	if err := Pack(StagedBundle(dir, target), target, dir, mtime); err != nil {
		t.Fatal(err)
	}
	if second, _ := os.ReadFile(filepath.Join(dir, "assets", target.Archive())); !bytes.Equal(first, second) {
		t.Error("archive not reproducible")
	}
}

func TestChecksums(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{"b.tar.gz": "bb", "a": "aa", "SHA256SUMS.txt": "old"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Checksums(dir); err != nil {
		t.Fatal(err)
	}
	sum := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	want := sum("aa") + "  a\n" + sum("bb") + "  b.tar.gz\n"
	if got, _ := os.ReadFile(filepath.Join(dir, "SHA256SUMS.txt")); string(got) != want {
		t.Errorf("SHA256SUMS.txt:\n%s", got)
	}
}
