package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

// seedClients stores a device seen a minute ago (named, private MAC) and
// one seen two hours ago.
func seedClients(t *testing.T, e *testEnv) {
	t.Helper()
	s := e.store()
	now := time.Now()
	if err := s.RecordDNSClients(map[string]store.DNSSighting{
		"192.168.1.42": {Queries: 12, Last: now.Add(-time.Minute)},
		"192.168.1.50": {Queries: 3, Last: now.Add(-2 * time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDNSClientIdentity("192.168.1.42", "iPhone-de-Leo.home", "aa:bb:cc:dd:ee:ff", now); err != nil {
		t.Fatal(err)
	}
}

func TestDNSClients(t *testing.T) {
	e := newEnv(t)
	// Pinned addresses: the real interfaces of the machine are not read
	e.initEnv("DNS_SERVER=true\nDNS_IP=192.168.1.20\nDNS_LISTEN_IP=192.168.1.20\n")
	e.dns.running = true

	// No device yet: which address to give them
	code, out, _ := e.run("dns-clients")
	if code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	mustContain(t, out, "No device has queried the LAN DNS server yet.\n")
	mustContain(t, out, "Set 192.168.1.20 as the DNS server")

	seedClients(t, e)
	code, out, _ = e.run("dns-clients")
	if code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	for _, want := range []string{"connected", "192.168.1.42", "iPhone-de-Leo.home", "aa:bb:cc:dd:ee:ff*", "1m ago", "12",
		"1 device(s) connected (idle ones: --all). * private MAC address"} {
		mustContain(t, out, want)
	}
	if strings.Contains(out, "192.168.1.50") || strings.Contains(out, "WARNING") {
		t.Errorf("idle device or warning listed:\n%s", out)
	}

	_, out, _ = e.run("dns-clients", "--all")
	for _, want := range []string{"192.168.1.50", "idle", "2h ago"} {
		mustContain(t, out, want)
	}

	_, out, _ = e.run("dns-clients", "--all", "--json")
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got) != 2 || got[0]["ip"] != "192.168.1.42" || got[0]["status"] != "connected" ||
		got[0]["name"] != "iPhone-de-Leo.home" || got[0]["randomized_mac"] != true || got[0]["queries"] != float64(12) ||
		got[1]["status"] != "idle" || got[0]["first_seen"] == nil || got[0]["last_seen"] == nil {
		t.Errorf("json = %v", got)
	}
}

func TestDNSClientsDisabledOrStopped(t *testing.T) {
	e := newEnv(t)
	e.initEnv("")
	code, out, _ := e.run("dns-clients")
	if code != 1 || !strings.Contains(out, "The LAN DNS server is disabled: run 'frankenmanager service enable dns'.") {
		t.Errorf("code %d, out:\n%s", code, out)
	}

	e.initEnv("DNS_SERVER=true\nDNS_IP=192.168.1.20\n")
	seedClients(t, e)
	code, out, _ = e.run("dns-clients")
	if code != 0 || !strings.Contains(out, "WARNING: The LAN DNS server is not running: run 'frankenmanager restart --dns'.") {
		t.Errorf("code %d, out:\n%s", code, out)
	}
}

func TestStatusShowsDNSDevices(t *testing.T) {
	e := newEnv(t)
	e.initEnv("DNS_SERVER=true\n")
	if err := e.store().AddDomains([]string{"app.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	seedClients(t, e)
	e.docker.ps = running
	e.dns.running = true

	code, out, _ := e.run("status")
	if code != 0 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	for _, want := range []string{"  Server: running\n", "  Follows LAN IP changes: yes\n",
		"  Devices connected: 1 (frankenmanager dns-clients)\n"} {
		mustContain(t, out, want)
	}
}
