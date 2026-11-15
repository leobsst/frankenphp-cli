package i18n

import (
	"errors"
	"testing"
	"testing/fstest"
)

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"fr_FR.UTF-8": "fr", "fr-CA": "fr", "FR": "fr", "en_US": "en",
		"C": "en", "C.UTF-8": "en", "POSIX": "en", "de_DE@euro": "de", "": "",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// FRANKENMANAGER_LANG, then the setting, then the system; whatever is not
// supported is skipped, English in the end.
func TestResolve(t *testing.T) {
	fr := func() string { return "fr_FR.UTF-8" }
	de := func() string { return "de_DE.UTF-8" }
	for _, c := range []struct {
		env, setting string
		system       func() string
		want         string
	}{
		{"", "", fr, "fr"},
		{"", Auto, fr, "fr"},
		{"", "en", fr, "en"},
		{"en", "fr", fr, "en"},
		{"xx", "fr", de, "fr"},
		{"", "xx", fr, "fr"},
		{"", Auto, de, "en"},
	} {
		t.Setenv(EnvVar, c.env)
		if got := Resolve(c.setting, c.system); got != c.want {
			t.Errorf("env %q, setting %q: %q, want %q", c.env, c.setting, got, c.want)
		}
	}
}

func TestSystemPrefersLocaleVariables(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "fr_FR.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := System(); got != "fr_FR.UTF-8" {
		t.Errorf("System() = %q", got)
	}
}

func withCatalog(t *testing.T, lang string, msgs map[string]string) {
	t.Helper()
	saved := catalogs
	catalogs = map[string]map[string]string{lang: msgs}
	SetLanguage(lang)
	t.Cleanup(func() { catalogs = saved; SetLanguage(Default) })
}

func TestTranslate(t *testing.T) {
	withCatalog(t, "fr", map[string]string{
		"%d domain":           "%d domaine",
		"%d domains":          "%d domaines",
		"%s is linked to %s":  "%[2]s est lié à %[1]s",
		"No server running":   "Aucun serveur lancé",
		"Unknown domain '%s'": "Domaine « %s » inconnu",
	})
	if got := Tf("%s is linked to %s", "a", "b"); got != "b est lié à a" {
		t.Errorf("reordered: %q", got)
	}
	if got := T("Not translated"); got != "Not translated" {
		t.Errorf("fallback: %q", got)
	}
	// French: 0 is singular
	for n, want := range map[int]string{0: "0 domaine", 1: "1 domaine", 2: "2 domaines"} {
		if got := Tn(n, "%d domain", "%d domains", n); got != want {
			t.Errorf("Tn(%d) = %q", n, got)
		}
	}
	inner := errors.New("boom")
	if err := Errorf("Unknown domain '%s': %w", "x", inner); !errors.Is(err, inner) {
		t.Errorf("Errorf does not wrap: %v", err)
	}
	// Created before the language changes, shown in the current one
	err := NewError("No server running")
	SetLanguage(Default)
	if err.Error() != "No server running" {
		t.Errorf("en: %q", err)
	}
	SetLanguage("fr")
	if err.Error() != "Aucun serveur lancé" {
		t.Errorf("fr: %q", err)
	}
}

func TestEnglishPlural(t *testing.T) {
	if got := Tn(0, "%d domain", "%d domains", 0); got != "0 domains" {
		t.Errorf("Tn(0) = %q", got)
	}
}

func TestLoadRejectsConflicts(t *testing.T) {
	fsys := fstest.MapFS{
		"locales/fr/a.json": {Data: []byte(`{"Done": "Terminé"}`)},
		"locales/fr/b.json": {Data: []byte(`{"Done": "Fini"}`)},
	}
	if _, err := Load(fsys); err == nil {
		t.Error("two translations of the same message accepted")
	}
	fsys["locales/fr/b.json"] = &fstest.MapFile{Data: []byte(`{"Done": "Terminé"}`)}
	if c, err := Load(fsys); err != nil || c["fr"]["Done"] != "Terminé" {
		t.Errorf("same translation twice: %v %v", c, err)
	}
}

// The embedded catalogs load (a broken JSON would panic at startup) and
// add French to English.
func TestEmbeddedLanguages(t *testing.T) {
	if langs := Languages(); len(langs) < 2 || langs[0] != "en" || !Supported("fr") {
		t.Errorf("Languages() = %v", langs)
	}
}
