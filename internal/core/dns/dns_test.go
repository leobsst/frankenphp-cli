package dns

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

type memConfig map[string]string

func (m memConfig) Get(k string) string { return m[k] }

func golden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fakeNetwork replaces the LAN IP and the interfaces.
func fakeNetwork(t *testing.T, lanIP string, addrs []Address) {
	t.Helper()
	oldIP, oldIfaces := LANIP, Interfaces
	LANIP = func() string { return lanIP }
	Interfaces = func() ([]Address, error) { return addrs, nil }
	t.Cleanup(func() { LANIP, Interfaces = oldIP, oldIfaces })
}

// The testdata files are the reference outputs of earlier releases: the
// generated DNS files must stay identical to them.
func TestGeneratedFilesMatchReference(t *testing.T) {
	if got := Corefile("1.1.1.1 9.9.9.9"); got != string(golden(t, "Corefile")) {
		t.Errorf("Corefile:\n%s", got)
	}
	got := Records("192.168.1.20", []string{"b.test", "a.test", "ws.frankenmanager.test", "a.test"})
	if got != string(golden(t, "records")) {
		t.Errorf("records:\n%s", got)
	}

	var describe map[string][]string
	if err := json.Unmarshal(golden(t, "describe.json"), &describe); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]Address{
		"none": nil,
		"one":  {{"en0", "192.168.1.20"}},
		"many": {{"en0", "192.168.1.20"}, {"en5", "10.0.0.4"}, {"", "172.16.0.2"}},
	}
	for name, addrs := range cases {
		got := DescribeServerAddresses(addrs, "192.168.1.20")
		if !slices.Equal(got, describe[name]) {
			t.Errorf("%s: got %q, want %q", name, got, describe[name])
		}
	}
}

func TestResolveAndListen(t *testing.T) {
	fakeNetwork(t, "192.168.1.20", nil)
	for _, tc := range []struct {
		c              memConfig
		ip, listen, er string
	}{
		{memConfig{}, "192.168.1.20", "", ""},
		{memConfig{"DNS_SERVER": "true"}, "192.168.1.20", "0.0.0.0:53", ""},
		{memConfig{"DNS_SERVER": "true", "DNS_IP": "10.0.0.5", "DNS_LISTEN_IP": "FE80::0:1", "DNS_PORT": "5353"},
			"10.0.0.5", "[fe80::1]:5353", ""},
		{memConfig{"DNS_SERVER": "true", "DNS_LISTEN_IP": "1.2.3"}, "192.168.1.20", "",
			"Invalid DNS_LISTEN_IP '1.2.3': '1.2.3' does not appear to be an IPv4 or IPv6 address"},
	} {
		ip, _ := ResolveIP(tc.c)
		listen, err := ListenAddress(tc.c)
		if ip != tc.ip || listen != tc.listen || (err != nil) != (tc.er != "") || (err != nil && err.Error() != tc.er) {
			t.Errorf("%v: ip %q listen %q err %v", tc.c, ip, listen, err)
		}
	}
	if _, err := ResolveIP(memConfig{"DNS_IP": "01.2.3.4"}); err == nil {
		t.Error("leading zero accepted")
	}
}

func TestServerAddresses(t *testing.T) {
	fakeNetwork(t, "192.168.1.20", []Address{
		{"lo0", "127.0.0.1"}, {"en5", "10.0.0.4"}, {"docker0", "172.17.0.1"}, {"bridge100", "192.168.64.1"},
		{"en0", "192.168.1.20"}, {"en7", "169.254.3.3"}, {"utun1", "10.0.0.4"},
	})
	got, err := ServerAddresses(memConfig{})
	want := []Address{{"en0", "192.168.1.20"}, {"en5", "10.0.0.4"}, {"bridge100", "192.168.64.1"}}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("got %v, %v", got, err)
	}

	got, _ = ServerAddresses(memConfig{"DNS_LISTEN_IP": "192.168.1.30"})
	if !slices.Equal(got, []Address{{"", "192.168.1.30"}}) {
		t.Errorf("pinned: %v", got)
	}
	got, _ = ServerAddresses(memConfig{"DNS_LISTEN_IP": "0.0.0.0"})
	if len(got) != 3 {
		t.Errorf("0.0.0.0: %v", got)
	}

	fakeNetwork(t, "192.168.1.20", nil)
	if got, _ := ServerAddresses(memConfig{}); !slices.Equal(got, []Address{{"", "192.168.1.20"}}) {
		t.Errorf("no interface: %v", got)
	}
	fakeNetwork(t, "127.0.0.1", nil)
	if got, _ := ServerAddresses(memConfig{}); len(got) != 0 {
		t.Errorf("offline: %v", got)
	}
}

type fakeRegistry struct{}

func (fakeRegistry) DomainNames() ([]string, error) { return []string{"b.test", "a.test"}, nil }
func (fakeRegistry) Aliases() ([]store.Alias, error) {
	return []store.Alias{{Alias: "c.test", Target: "a.test"}}, nil
}
func (fakeRegistry) Proxies() ([]store.Proxy, error) {
	return []store.Proxy{{Domain: "api.test", Target: "127.0.0.1:8000"}}, nil
}

func TestSync(t *testing.T) {
	fakeNetwork(t, "192.168.1.20", nil)
	dir := t.TempDir()
	if err := Sync(dir, memConfig{}, fakeRegistry{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Dir(dir)); err == nil {
		t.Error("disabled DNS wrote files")
	}

	c := memConfig{"DNS_SERVER": "true", "SOKETI_ENABLED": "true", "DNS_UPSTREAM": "8.8.8.8"}
	if err := Sync(dir, c, fakeRegistry{}); err != nil {
		t.Fatal(err)
	}
	core, _ := os.ReadFile(filepath.Join(Dir(dir), "Corefile"))
	hosts, _ := os.ReadFile(filepath.Join(Dir(dir), "hosts"))
	if string(core) != Corefile("8.8.8.8") {
		t.Errorf("Corefile:\n%s", core)
	}
	want := Records("192.168.1.20", []string{"a.test", "b.test", "c.test", "api.test", "ws.frankenmanager.test"})
	if string(hosts) != want {
		t.Errorf("hosts:\n%s", hosts)
	}
	entries, _ := os.ReadDir(Dir(dir))
	if len(entries) != 2 {
		t.Errorf("temporary files left: %v", entries)
	}
}
