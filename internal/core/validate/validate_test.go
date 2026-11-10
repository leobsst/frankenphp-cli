package validate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDomain(t *testing.T) {
	for d, ok := range map[string]bool{
		"myapp.test": true, "s3.api-v2.test": true, "localhost": true, "a.b.c.dev": true,
		"myapp": false, "-a.test": false, "a-.test": false, "a.t": false, "a.te5t": false,
		"a_b.test": false, "": false, "my app.test": false,
		"api.v2.test": true, "s3.ppd.api-shop.test": true, "s3..api.test": false, "s3.-api.test": false,
	} {
		if got := Domain(d) == nil; got != ok {
			t.Errorf("Domain(%q) valid = %v", d, got)
		}
	}
	if err := Domain("bad"); err.Error() != "Invalid domain name: bad" {
		t.Errorf("message = %q", err)
	}
}

func TestProxyTarget(t *testing.T) {
	for target, ok := range map[string]bool{
		"127.0.0.1:8006": true, "http://127.0.0.1:8006": true, "https://backend.internal:9000/": true,
		"backend": true, "ftp://x": false, "127.0.0.1:123456": false, "host/path": false, "": false,
	} {
		if got := ProxyTarget(target) == nil; got != ok {
			t.Errorf("ProxyTarget(%q) valid = %v", target, got)
		}
	}
}

func TestDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := Directory(dir); err != nil {
		t.Errorf("existing directory: %v", err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{file, filepath.Join(dir, "missing")} {
		if err := Directory(p); err == nil || err.Error() != "Directory does not exist: "+p {
			t.Errorf("%s: %v", p, err)
		}
	}
}
