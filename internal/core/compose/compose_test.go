package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The testdata files are the reference outputs of earlier releases for the
// same arguments, without the DNS container: Build must keep producing them
// byte for byte.
func TestBuildMatchesReference(t *testing.T) {
	all := []string{"postgresql", "mysql", "mariadb"}
	cases := map[string]Options{
		"dev_default":      {PHPVersions: []string{"8.3"}},
		"prod_default":     {PHPVersions: []string{"8.3"}, Production: true},
		"dev_all":          {PHPVersions: []string{"8.5", "8.2", "8.4"}, DBEngines: all, Redis: true, Websocket: true},
		"prod_all":         {PHPVersions: []string{"8.5", "8.2", "8.4"}, Production: true, DBEngines: all, Redis: true, Websocket: true},
		"dev_postgresql":   {PHPVersions: []string{"8.4"}, DBEngines: []string{"postgresql"}, Redis: true},
		"prod_postgresql":  {PHPVersions: []string{"8.4"}, Production: true, DBEngines: []string{"postgresql"}, Websocket: true},
		"dev_mysql_no_php": {DBEngines: []string{"mysql"}, Websocket: true},
		"prod_no_db":       {PHPVersions: []string{"8.3"}, Production: true, DBEngines: []string{}},
		"dev_no_db_redis":  {PHPVersions: []string{"8.3"}, DBEngines: []string{}, Redis: true},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", name+".yml"))
			if err != nil {
				t.Fatal(err)
			}
			if got := Build(o); got != string(want) {
				t.Errorf("Build differs from the reference:\n--- got\n%s\n--- want\n%s", got, want)
			}
		})
	}
}

func TestWritePHPMyAdminConfigInDevelopmentOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    Options
		want bool
	}{
		{"dev mariadb", Options{}, true},
		{"dev postgresql only", Options{DBEngines: []string{"postgresql"}}, false},
		{"prod mariadb", Options{Production: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path, err := Write(dir, tc.o)
			if err != nil {
				t.Fatal(err)
			}
			if path != filepath.Join(dir, FileName) {
				t.Errorf("path = %s", path)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != Build(tc.o) {
				t.Errorf("compose file not written: %v", err)
			}
			pma, err := os.ReadFile(filepath.Join(dir, PHPMyAdminConfigPath))
			if got := err == nil; got != tc.want {
				t.Fatalf("phpMyAdmin config written = %v, want %v", got, tc.want)
			}
			if tc.want && string(pma) != PHPMyAdminConfig {
				t.Errorf("phpMyAdmin config = %q", pma)
			}
		})
	}
}

// The reverse proxy mounts the static sites and the projects root only
// when there are static hosts.
func TestCaddyServiceStaticSites(t *testing.T) {
	mounts := []string{
		"      CUSTOM_PATH: ${CUSTOM_PATH}\n",
		"      - ${CADDY_DIR:-./caddy}/sites/static:/etc/caddy/sites/static:ro\n",
		"      - ${CUSTOM_PATH}:/${CUSTOM_PATH}:ro\n",
	}
	with := Build(Options{StaticSites: true})
	without := Build(Options{})
	for _, m := range mounts {
		if !strings.Contains(with, m) {
			t.Errorf("missing %q in:\n%s", m, with)
		}
		if strings.Contains(without, m) {
			t.Errorf("unexpected %q without static sites", m)
		}
	}
}
