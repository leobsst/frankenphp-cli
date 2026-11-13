package dnswatcher

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

// checkDir is a data directory with the DNS server on and one domain; the
// LAN IP is lanIP.
func checkDir(t *testing.T, envContent string, lanIP *string) string {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetConfig(env.Parse(envContent)); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDomains([]string{"app.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	old := dns.LANIP
	dns.LANIP = func() string { return *lanIP }
	t.Cleanup(func() { dns.LANIP = old })
	return dir
}

func TestCheckOnceFollowsTheLANIP(t *testing.T) {
	ip := "192.168.1.30"
	dir := checkDir(t, "DNS_SERVER=true\n", &ip)
	records := filepath.Join(dns.Dir(dir), "hosts")
	var log bytes.Buffer

	got, err := CheckOnce(dir, "", &log)
	if err != nil || got != "192.168.1.30" {
		t.Fatalf("CheckOnce = %q, %v", got, err)
	}
	data, _ := os.ReadFile(records)
	if string(data) != dns.Records("192.168.1.30", []string{"app.test"}) {
		t.Errorf("records:\n%s", data)
	}
	if !regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d LAN IP changed: None -> 192\.168\.1\.30, DNS records updated\n$`).Match(log.Bytes()) {
		t.Errorf("log %q", log.String())
	}

	// Same IP: nothing rewritten
	if err := os.Remove(records); err != nil {
		t.Fatal(err)
	}
	if got, _ := CheckOnce(dir, "192.168.1.30", &log); got != "192.168.1.30" {
		t.Errorf("same IP: %q", got)
	}
	if _, err := os.Stat(records); err == nil {
		t.Error("records rewritten for the same IP")
	}

	// No network (switching Wi-Fi to Ethernet): the records stay as they are
	ip = "127.0.0.1"
	log.Reset()
	if got, _ := CheckOnce(dir, "192.168.1.30", &log); got != "192.168.1.30" || log.Len() != 0 {
		t.Errorf("offline: %q, log %q", got, log.String())
	}

	// A new IP: rewritten, old -> new logged
	ip = "10.0.0.7"
	if got, _ := CheckOnce(dir, "192.168.1.30", &log); got != "10.0.0.7" ||
		!strings.Contains(log.String(), "LAN IP changed: 192.168.1.30 -> 10.0.0.7") {
		t.Errorf("changed: %q, log %q", got, log.String())
	}
	data, _ = os.ReadFile(records)
	if !strings.Contains(string(data), "10.0.0.7 app.test") {
		t.Errorf("records:\n%s", data)
	}
}

// DNS_IP pins the records: the watcher leaves them alone.
func TestCheckOnceLeavesAFixedDNSIPAlone(t *testing.T) {
	ip := "192.168.1.30"
	dir := checkDir(t, "DNS_SERVER=true\nDNS_IP=192.168.1.10\n", &ip)
	var log bytes.Buffer
	if got, err := CheckOnce(dir, "", &log); got != "" || err != nil || log.Len() != 0 {
		t.Errorf("CheckOnce = %q, %v, log %q", got, err, log.String())
	}
	if _, err := os.Stat(dns.Dir(dir)); err == nil {
		t.Error("records written")
	}
}

// Stop of a systemd watcher: unit disabled and removed, reported running.
func TestStopSystemd(t *testing.T) {
	w, r, _ := newWatcher(t, "linux", map[string]string{"systemctl": "/usr/bin/systemctl"})
	if err := os.MkdirAll(filepath.Dir(w.UnitPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.UnitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !w.Stop(t.Context()) {
		t.Error("Stop found nothing")
	}
	if _, err := os.Stat(w.UnitPath); err == nil {
		t.Error("unit left: it would restart the watcher at login")
	}
	want := "systemctl --user disable --now " + SystemdUnit + "|systemctl --user daemon-reload"
	if got := strings.Join(r.Lines(), "|"); got != want {
		t.Errorf("calls = %s", got)
	}
	if w.Stop(t.Context()) {
		t.Error("second Stop found something")
	}
}
