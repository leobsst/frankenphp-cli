package release

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// NoticesFile is the third-party notices file of every bundle, next to the
// project's LICENSE.
const NoticesFile = "THIRD_PARTY_NOTICES.txt"

// Notices returns the project's license and the third-party notices of the
// packages built for a target.
type Notices func(ctx context.Context, t Target, pkgs ...string) (license, thirdParty []byte, err error)

// Dependency is a module compiled into the executables, with the directory of
// its source (the module cache, or the replacement's directory).
type Dependency struct {
	Path, Version, Dir string
	Main               bool
}

// GoNotices lists the modules `go build` compiles for t (go list -deps,
// with the target's GOOS/GOARCH: the dependencies differ between platforms)
// and gathers their license files, plus the Go license for the runtime and
// standard library linked into every executable.
func GoNotices(ctx context.Context, t Target, pkgs ...string) ([]byte, []byte, error) {
	args := []string{"list", "-deps", "-f",
		"{{with .Module}}{{.Path}}\t{{.Version}}\t{{.Dir}}\t{{.Main}}{{end}}"}
	for _, p := range pkgs {
		args = append(args, Module+"/"+p)
	}
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+t.GOOS, "GOARCH="+t.GOARCH, "GOARM="+t.GOARM)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("list the modules of %s: %w", t, err)
	}
	mods, err := parseModules(out)
	if err != nil {
		return nil, nil, err
	}
	goroot, err := exec.CommandContext(ctx, "go", "env", "GOROOT").Output()
	if err != nil {
		return nil, nil, fmt.Errorf("go env GOROOT: %w", err)
	}
	return CollectNotices(strings.TrimSpace(string(goroot)), mods)
}

// parseModules reads the go list lines, one per package: each module once.
func parseModules(out []byte) ([]Dependency, error) {
	seen := map[string]bool{}
	var mods []Dependency
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue // a standard library package
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			return nil, fmt.Errorf("unexpected go list line %q", line)
		}
		if seen[f[0]] {
			continue
		}
		seen[f[0]] = true
		mods = append(mods, Dependency{Path: f[0], Version: f[1], Dir: f[2], Main: f[3] == "true"})
	}
	return mods, sc.Err()
}

// licenseFile reports whether a file at the root of a module holds license
// terms to redistribute: LICENSE*, LICENCE*, COPYING*, NOTICE* (Apache 2.0
// requires it), PATENTS* (the golang.org/x grant), UNLICENSE.
func licenseFile(name string) bool {
	n := strings.ToUpper(name)
	for _, p := range []string{"LICENSE", "LICENCE", "COPYING", "NOTICE", "PATENTS", "UNLICENSE"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

func licenseFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && licenseFile(e.Name()) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}

// CollectNotices returns the main module's LICENSE and the notices of every
// other module, sorted by path, after the Go license. A module without any
// license file fails: its terms would be missing from the release.
func CollectNotices(goroot string, mods []Dependency) ([]byte, []byte, error) {
	var license []byte
	var err error
	var b bytes.Buffer
	b.WriteString("FrankenManager includes the following third-party software, under the\n" +
		"terms reproduced below.\n")

	for _, name := range []string{"LICENSE", "PATENTS"} {
		text, err := goFile(goroot, name)
		if err != nil && name == "LICENSE" {
			return nil, nil, fmt.Errorf("the Go license: %w", err)
		}
		if err == nil {
			writeNotice(&b, "Go runtime and standard library (https://go.dev)", name, text)
		}
	}

	others := slices.Clone(mods)
	slices.SortFunc(others, func(a, b Dependency) int { return strings.Compare(a.Path, b.Path) })
	var missing []string
	for _, m := range others {
		if m.Main {
			if license, err = os.ReadFile(filepath.Join(m.Dir, "LICENSE")); err != nil {
				return nil, nil, fmt.Errorf("the project license: %w", err)
			}
			continue
		}
		names, err := licenseFiles(m.Dir)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", m.Path, err)
		}
		if len(names) == 0 {
			missing = append(missing, m.Path)
			continue
		}
		for _, name := range names {
			text, err := os.ReadFile(filepath.Join(m.Dir, name))
			if err != nil {
				return nil, nil, err
			}
			writeNotice(&b, strings.TrimSpace(m.Path+" "+m.Version), name, text)
		}
	}
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf("no license file in %s", strings.Join(missing, ", "))
	}
	if license == nil {
		return nil, nil, errors.New("the main module is not among the listed modules")
	}
	return license, b.Bytes(), nil
}

// goFile reads a file of the Go distribution: at the root of GOROOT, or
// next to it where packagers move it (Homebrew: <prefix>/LICENSE beside
// <prefix>/libexec).
func goFile(goroot, name string) ([]byte, error) {
	text, err := os.ReadFile(filepath.Join(goroot, name))
	if errors.Is(err, os.ErrNotExist) {
		return os.ReadFile(filepath.Join(filepath.Dir(goroot), name))
	}
	return text, err
}

func writeNotice(b *bytes.Buffer, title, file string, text []byte) {
	rule := strings.Repeat("=", 80)
	fmt.Fprintf(b, "\n%s\n%s (%s)\n%s\n\n", rule, title, file, rule)
	b.Write(bytes.TrimRight(text, "\n"))
	b.WriteString("\n")
}
