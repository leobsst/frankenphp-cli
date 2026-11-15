package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

// The tests expect the English messages, whatever the language of the
// machine running them.
func TestMain(m *testing.M) {
	_ = os.Setenv(i18n.EnvVar, "en")
	code := m.Run()
	i18n.SetLanguage(i18n.Default)
	os.Exit(code)
}

func helpIn(t *testing.T, deps Deps) string {
	t.Helper()
	var out, errOut bytes.Buffer
	ExecuteWith([]string{"--no-update-check", "--help"}, &out, &errOut, deps)
	t.Cleanup(func() { i18n.SetLanguage(i18n.Default) })
	return out.String()
}

// CLI_LANGUAGE selects the language over the system one; FRANKENMANAGER_LANG
// wins over both.
func TestLanguageSetting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(platform.DataDirEnv, dir)
	t.Setenv(i18n.EnvVar, "")
	english := Deps{Language: func() string { return "en_US.UTF-8" }}
	french := Deps{Language: func() string { return "fr_FR.UTF-8" }}

	if out := helpIn(t, french); !strings.Contains(out, "Utilisation :") {
		t.Errorf("auto, French system:\n%s", out)
	}
	if err := (store.ConfigDB{Path: filepath.Join(dir, "db.sqlite")}).SetConfig(map[string]string{env.LanguageKey: "fr"}); err != nil {
		t.Fatal(err)
	}
	if out := helpIn(t, english); !strings.Contains(out, "Utilisation :") {
		t.Errorf("CLI_LANGUAGE=fr, English system:\n%s", out)
	}
	t.Setenv(i18n.EnvVar, "en")
	if out := helpIn(t, french); !strings.Contains(out, "Usage:") {
		t.Errorf("FRANKENMANAGER_LANG=en:\n%s", out)
	}
}
