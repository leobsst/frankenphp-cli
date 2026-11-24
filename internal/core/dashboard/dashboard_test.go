package dashboard

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

func TestDomains(t *testing.T) {
	for _, tc := range []struct {
		alias string
		want  []string
	}{
		{"", []string{"frankenmanager.test", "fkm.test"}}, // the default alias
		{"fm", []string{"frankenmanager.test", "fm.test"}},
		{"FM ", []string{"frankenmanager.test", "fm.test"}},
		{"frankenmanager", []string{"frankenmanager.test"}},
		{"not_a_label", []string{"frankenmanager.test"}},
	} {
		e := env.FromMap(map[string]string{})
		if tc.alias != "" {
			e = env.FromMap(map[string]string{env.AliasKey: tc.alias})
		}
		if got := Domains(e); !slices.Equal(got, tc.want) {
			t.Errorf("alias %q: %v, want %v", tc.alias, got, tc.want)
		}
	} // CLI_ALIAS set empty: no alias, frankenmanager.test only
	if got := Domains(env.FromMap(map[string]string{env.AliasKey: ""})); !slices.Equal(got, []string{"frankenmanager.test"}) {
		t.Errorf("no alias: %v", got)
	}
}

func TestOrigins(t *testing.T) {
	got := Origins([]string{"frankenmanager.test", "fkm.test"}, "443", 9444)
	want := []string{"https://frankenmanager.test", "https://fkm.test", "https://127.0.0.1:9444", "https://localhost:9444"}
	if !slices.Equal(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
	if got := Origins([]string{"frankenmanager.test"}, "8443", 9444); got[0] != "https://frankenmanager.test:8443" {
		t.Errorf("custom port: %v", got)
	}
}

// A dashboard name taken by a registered host stays with that host: its
// pages must never sign in the dashboard without the password.
func TestAvailable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	e := env.FromMap(map[string]string{})
	if got, err := Available(e, st); err != nil || !slices.Equal(got, []string{"frankenmanager.test", "fkm.test"}) {
		t.Errorf("free: %v %v", got, err)
	}
	if err := st.AddDomains([]string{"frankenmanager.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddStaticHosts([]store.StaticHost{{Domain: "fkm.test", Root: "fkm"}}); err != nil {
		t.Fatal(err)
	}
	if got, err := Available(e, st); err != nil || len(got) != 0 {
		t.Errorf("taken: %v %v", got, err)
	}
}
