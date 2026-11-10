package compose

import (
	"os"
	"path/filepath"
	"testing"
)

// The testdata files are the reference outputs of earlier releases for the
// same arguments: Build must keep producing them byte for byte.
func TestBuildMatchesReference(t *testing.T) {
	all := []string{"postgresql", "mysql", "mariadb"}
	cases := map[string]Options{
		"dev_default":      {PHPVersions: []string{"8.3"}},
		"prod_default":     {PHPVersions: []string{"8.3"}, Production: true},
		"dev_all":          {PHPVersions: []string{"8.5", "8.2", "8.4"}, DBEngines: all, DNSListen: "0.0.0.0:53", Redis: true, Websocket: true},
		"prod_all":         {PHPVersions: []string{"8.5", "8.2", "8.4"}, Production: true, DBEngines: all, DNSListen: "0.0.0.0:53", Redis: true, Websocket: true},
		"dev_postgresql":   {PHPVersions: []string{"8.4"}, DBEngines: []string{"postgresql"}, Redis: true},
		"prod_postgresql":  {PHPVersions: []string{"8.4"}, Production: true, DBEngines: []string{"postgresql"}, Websocket: true},
		"dev_mysql_no_php": {DBEngines: []string{"mysql"}, DNSListen: "192.168.1.10:5353", Websocket: true},
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
