package i18ncheck

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestVerbs(t *testing.T) {
	for format, want := range map[string][]string{
		"no verb, 100%%":     nil,
		"%s and %d":          {"1:s", "2:d"},
		"%[2]s before %[1]s": {"1:s", "2:s"},
		"%-10s|%5.2f|%q":     {"1:s", "2:f", "3:q"},
		"%*d":                {"1:*", "2:d"},
		"%w after %v":        {"1:w", "2:v"},
		"%[1]s, again %[1]s": {"1:s", "1:s"},
	} {
		if got := Verbs(format); !slices.Equal(got, want) {
			t.Errorf("Verbs(%q) = %v, want %v", format, got, want)
		}
	}
}

func TestCheck(t *testing.T) {
	msgs := []Message{{ID: "Hello %s", Pos: "a.go:1"}, {ID: "Missing", Pos: "a.go:2"}, {ID: "Empty", Pos: "a.go:3"}}
	problems := Check(msgs, map[string]map[string]string{"fr": {
		"Hello %s": "Bonjour %d", // wrong verb
		"Empty":    " ",
		"Gone":     "Parti",
	}})
	want := []string{"a.go:2: no fr translation", "verbs of \"Hello %s\"", "empty translation of \"Empty\"", "\"Gone\" is no longer in the code"}
	if len(problems) != len(want) {
		t.Fatalf("problems:\n%s", strings.Join(problems, "\n"))
	}
	for _, w := range want {
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, w) }) {
			t.Errorf("missing %q in:\n%s", w, strings.Join(problems, "\n"))
		}
	}
}

func TestScan(t *testing.T) {
	dir := t.TempDir()
	src := `package x

import tr "github.com/leobsst/frankenphp-cli/internal/i18n"

var marked = tr.N("Marked")

func f(name string, n int) {
	_ = tr.T("Plain")
	_ = tr.Tf("Long message, " +
		"split %s", name)
	_ = tr.Tn(n, "%d item", "%d items", n)
	_ = tr.T(marked)
	_ = tr.Tf(name)
}
`
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// Tests are not scanned
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte(strings.ReplaceAll(src, "Plain", "Test only")), 0o644); err != nil {
		t.Fatal(err)
	}
	msgs, problems, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	slices.Sort(ids)
	if want := []string{"%d item", "%d items", "Long message, split %s", "Marked", "Plain"}; !slices.Equal(ids, want) {
		t.Errorf("ids = %q", ids)
	}
	// T(variable) is the display of N-marked text; Tf(variable) is not
	// checkable
	if len(problems) != 1 || !strings.Contains(problems[0], "i18n.Tf must be a string literal") {
		t.Errorf("problems = %q", problems)
	}
}
