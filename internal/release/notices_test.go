package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	for name, text := range files {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParseModules(t *testing.T) {
	out := "\n" + // a standard library package
		"example.com/a\tv1.0.0\t/cache/a\tfalse\n" +
		"github.com/leobsst/frankenphp-cli\t\t/src\ttrue\n" +
		"example.com/a\tv1.0.0\t/cache/a\tfalse\n"
	mods, err := parseModules([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 2 || mods[0].Path != "example.com/a" || !mods[1].Main {
		t.Errorf("modules = %+v", mods)
	}
}

func TestCollectNotices(t *testing.T) {
	root := t.TempDir()
	goroot := writeFiles(t, filepath.Join(root, "go"), map[string]string{"LICENSE": "go license\n"})
	main := writeFiles(t, filepath.Join(root, "main"), map[string]string{"LICENSE": "MIT\n"})
	apache := writeFiles(t, filepath.Join(root, "apache"), map[string]string{
		"LICENSE":     "apache license\n",
		"NOTICE":      "apache notice\n",
		"README.md":   "not a license",
		"sub/LICENSE": "nested, not at the module root",
	})
	x := writeFiles(t, filepath.Join(root, "x"), map[string]string{"LICENSE": "bsd\n", "PATENTS": "patents\n"})

	license, notices, err := CollectNotices(goroot, []Dependency{
		{Path: "golang.org/x/sys", Version: "v0.1.0", Dir: x},
		{Path: "github.com/leobsst/frankenphp-cli", Dir: main, Main: true},
		{Path: "github.com/coredns/coredns", Version: "v1.15.0", Dir: apache},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(license) != "MIT\n" {
		t.Errorf("license = %q", license)
	}
	text := string(notices)
	// Go first, then the modules sorted by path, each license file in name order
	var order []int
	for _, s := range []string{"go license", "apache license", "apache notice", "bsd", "patents"} {
		order = append(order, strings.Index(text, s))
	}
	for i, at := range order {
		if at < 0 || (i > 0 && at < order[i-1]) {
			t.Fatalf("notices out of order (%v):\n%s", order, text)
		}
	}
	for _, s := range []string{"github.com/coredns/coredns v1.15.0 (NOTICE)", "golang.org/x/sys v0.1.0 (PATENTS)"} {
		if !strings.Contains(text, s) {
			t.Errorf("missing %q", s)
		}
	}
	for _, s := range []string{"not a license", "nested", "MIT"} {
		if strings.Contains(text, s) {
			t.Errorf("unexpected %q", s)
		}
	}
}

// Packagers like Homebrew move the Go LICENSE beside GOROOT (libexec).
func TestCollectNoticesGoLicenseBesideGOROOT(t *testing.T) {
	root := t.TempDir()
	prefix := writeFiles(t, filepath.Join(root, "go"), map[string]string{"LICENSE": "go license", "libexec/PATENTS": "go patents"})
	main := writeFiles(t, filepath.Join(root, "main"), map[string]string{"LICENSE": "MIT"})
	_, notices, err := CollectNotices(filepath.Join(prefix, "libexec"), []Dependency{
		{Path: "github.com/leobsst/frankenphp-cli", Dir: main, Main: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(notices), "go license") || !strings.Contains(string(notices), "go patents") {
		t.Errorf("notices:\n%s", notices)
	}
}

// A module without license terms stops the release instead of shipping it
// without them.
func TestCollectNoticesMissingLicense(t *testing.T) {
	root := t.TempDir()
	goroot := writeFiles(t, filepath.Join(root, "go"), map[string]string{"LICENSE": "go"})
	main := writeFiles(t, filepath.Join(root, "main"), map[string]string{"LICENSE": "MIT"})
	bare := writeFiles(t, filepath.Join(root, "bare"), map[string]string{"go.mod": "module example.com/bare"})
	_, _, err := CollectNotices(goroot, []Dependency{
		{Path: "github.com/leobsst/frankenphp-cli", Dir: main, Main: true},
		{Path: "example.com/bare", Version: "v1.0.0", Dir: bare},
	})
	if err == nil || !strings.Contains(err.Error(), "example.com/bare") {
		t.Errorf("err = %v", err)
	}
}
