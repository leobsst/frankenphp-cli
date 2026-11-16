package dnsclients

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

var t0 = time.Unix(1_800_000_000, 0)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func clock(at *time.Time) func() time.Time { return func() time.Time { return *at } }

func TestTrackerFlushAndIdentity(t *testing.T) {
	s := openStore(t)
	now := t0
	tr := &Tracker{Now: clock(&now)}

	for range 3 {
		tr.Seen("192.168.1.42")
	}
	stale, err := tr.Flush(s)
	if err != nil || !reflect.DeepEqual(stale, []string{"192.168.1.42"}) {
		t.Fatalf("Flush = %v, %v", stale, err)
	}
	if stale, _ := tr.Flush(s); stale != nil {
		t.Errorf("empty flush returned %v", stale)
	}

	id := Identifier{
		Gateway: func(context.Context) (string, error) { return "192.168.1.1", nil },
		LookupAddr: func(_ context.Context, server, ip string) ([]string, error) {
			if server == "192.168.1.1" && ip == "192.168.1.42" {
				return []string{"iPhone-de-Leo.home."}, nil
			}
			return nil, errors.New("no PTR")
		},
		ARP: func(context.Context) (map[string]string, error) {
			return map[string]string{"192.168.1.42": "aa:bb:cc:dd:ee:ff"}, nil
		},
	}
	if err := Identify(context.Background(), s, id, stale, now); err != nil {
		t.Fatal(err)
	}

	now = t0.Add(2 * time.Minute)
	tr.Seen("192.168.1.42")
	// Identity found 2 min ago: not stale
	if stale, err := tr.Flush(s); err != nil || stale != nil {
		t.Errorf("second Flush = %v, %v", stale, err)
	}
	clients, err := s.DNSClients()
	if err != nil {
		t.Fatal(err)
	}
	want := store.DNSClient{IP: "192.168.1.42", Name: "iPhone-de-Leo.home", MAC: "aa:bb:cc:dd:ee:ff",
		FirstSeen: t0, LastSeen: now, Queries: 4, IdentityAt: t0}
	if len(clients) != 1 || !reflect.DeepEqual(clients[0], want) {
		t.Errorf("clients = %+v", clients)
	}

	if got := Status(clients[0], now.Add(2*time.Minute)); got != Connected {
		t.Errorf("2 min after: %s", got)
	}
	if got := Status(clients[0], now.Add(10*time.Minute)); got != Idle {
		t.Errorf("10 min after: %s", got)
	}

	// Identity older than IdentityTTL: looked up again
	now = now.Add(IdentityTTL + time.Minute)
	tr.Seen("192.168.1.42")
	if stale, _ := tr.Flush(s); !reflect.DeepEqual(stale, []string{"192.168.1.42"}) {
		t.Errorf("stale identity not returned: %v", stale)
	}
}

type failingStore struct{ *store.Store }

func (failingStore) RecordDNSClients(map[string]store.DNSSighting) error { return errors.New("busy") }

func TestFlushKeepsSightingsOnError(t *testing.T) {
	s := openStore(t)
	now := t0
	tr := &Tracker{Now: clock(&now)}
	tr.Seen("192.168.1.42")
	if _, err := tr.Flush(failingStore{s}); err == nil {
		t.Fatal("no error")
	}
	tr.Seen("192.168.1.42")
	if _, err := tr.Flush(s); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.DNSClients(); len(c) != 1 || c[0].Queries != 2 {
		t.Errorf("clients = %+v", c)
	}
}

func TestIdentifyUnknownAndSelf(t *testing.T) {
	failing := Identifier{
		Gateway:    func(context.Context) (string, error) { return "", errors.New("no route") },
		LookupAddr: func(context.Context, string, string) ([]string, error) { return nil, errors.New("timeout") },
		ARP:        func(context.Context) (map[string]string, error) { return map[string]string{}, nil },
		Self:       func() []string { return []string{"192.168.1.72"} },
	}
	if name, mac := failing.Identify(context.Background(), "192.168.1.50"); name != "" || mac != "" {
		t.Errorf("unknown device: %q %q", name, mac)
	}
	for _, ip := range []string{"127.0.0.1", "::1", "192.168.1.72"} {
		if name, _ := failing.Identify(context.Background(), ip); name != ThisMachine {
			t.Errorf("%s: %q", ip, name)
		}
	}
	// The system resolver is the fallback when the router has no name
	fallback := failing
	fallback.LookupAddr = func(_ context.Context, server, _ string) ([]string, error) {
		if server == "" {
			return []string{"nas.local."}, nil
		}
		return nil, errors.New("refused")
	}
	if name, _ := fallback.Identify(context.Background(), "192.168.1.50"); name != "nas.local" {
		t.Errorf("fallback name %q", name)
	}
	// mDNS answers when the router does not, before the system resolver
	mdns := fallback
	mdns.LookupMDNS = func(_ context.Context, ip string) ([]string, error) {
		if ip == "192.168.1.50" {
			return []string{"MacBook-de-Leo.local."}, nil
		}
		return nil, errors.New("timeout")
	}
	if name, _ := mdns.Identify(context.Background(), "192.168.1.50"); name != "MacBook-de-Leo.local" {
		t.Errorf("mDNS name %q", name)
	}
	// NetBIOS answers when mDNS does not, before the system resolver
	netbios := fallback
	netbios.LookupMDNS = func(context.Context, string) ([]string, error) { return nil, errors.New("timeout") }
	netbios.LookupNetBIOS = func(context.Context, string) (string, error) { return "DESKTOP-LEO", nil }
	if name, _ := netbios.Identify(context.Background(), "192.168.1.50"); name != "DESKTOP-LEO" {
		t.Errorf("NetBIOS name %q", name)
	}
}

// nbstatEntry is one name of a node status response.
func nbstatEntry(name string, suffix byte, group bool) []byte {
	e := []byte(fmt.Sprintf("%-15s", name))
	flags := byte(0x04)
	if group {
		flags |= 0x80
	}
	return append(e, suffix, flags, 0x00)
}

func TestParseNBSTAT(t *testing.T) {
	resp := []byte{0x46, 0x4d, 0x84, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00}
	resp = append(resp, nbstatRequest[12:12+34]...) // answer name
	resp = append(resp, 0x00, 0x21, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
	resp = append(resp, 3)
	resp = append(resp, nbstatEntry("WORKGROUP", 0x00, true)...)
	resp = append(resp, nbstatEntry("DESKTOP-LEO", 0x20, false)...)
	resp = append(resp, nbstatEntry("DESKTOP-LEO", 0x00, false)...)
	if name, err := parseNBSTAT(resp); err != nil || name != "DESKTOP-LEO" {
		t.Errorf("parseNBSTAT = %q, %v", name, err)
	}
	if _, err := parseNBSTAT(resp[:40]); err == nil {
		t.Error("truncated response accepted")
	}
	if _, err := parseNBSTAT(nbstatRequest); err == nil {
		t.Error("request accepted as a response")
	}
}

func TestPrune(t *testing.T) {
	s := openStore(t)
	if err := s.RecordDNSClients(map[string]store.DNSSighting{
		"192.168.1.42": {Queries: 1, Last: t0.Add(-Retention - time.Hour)},
		"192.168.1.50": {Queries: 1, Last: t0.Add(-time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := Prune(s, t0); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.DNSClients(); len(c) != 1 || c[0].IP != "192.168.1.50" {
		t.Errorf("after prune: %+v", c)
	}
}

func TestParseARP(t *testing.T) {
	cases := map[string]string{
		"darwin": `? (192.168.1.1) at 0:24:d4:a1:b2:c3 on en0 ifscope [ethernet]
? (192.168.1.42) at aa:bb:cc:dd:ee:ff on en0 ifscope [ethernet]
? (192.168.1.60) at (incomplete) on en0 ifscope [ethernet]
? (192.168.1.255) at ff:ff:ff:ff:ff:ff on en0 ifscope [ethernet]
`,
		"linux": `IP address       HW type     Flags       HW address            Mask     Device
192.168.1.1      0x1         0x2         00:24:d4:a1:b2:c3     *        eth0
192.168.1.42     0x1         0x2         AA:BB:CC:DD:EE:FF     *        eth0
192.168.1.60     0x1         0x0         00:00:00:00:00:00     *        eth0
`,
		"windows": `
Interface: 192.168.1.72 --- 0x5
  Internet Address      Physical Address      Type
  192.168.1.1           00-24-d4-a1-b2-c3     dynamic
  192.168.1.42          aa-bb-cc-dd-ee-ff     dynamic
  192.168.1.255         ff-ff-ff-ff-ff-ff     static
`,
	}
	want := map[string]string{"192.168.1.1": "00:24:d4:a1:b2:c3", "192.168.1.42": "aa:bb:cc:dd:ee:ff"}
	for goos, out := range cases {
		if got := parseARP(goos, out); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v", goos, got)
		}
	}
}

func TestParseGateway(t *testing.T) {
	cases := map[string]string{
		"darwin": `   route to: default
destination: default
       mask: default
    gateway: 192.168.1.1
  interface: en0
`,
		"linux": `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
eth0	0001A8C0	00000000	0001	0	0	100	00FFFFFF	0	0	0
eth0	00000000	0101A8C0	0003	0	0	100	00000000	0	0	0
`,
		"windows": `IPv4 Route Table
===========================================================================
Active Routes:
Network Destination        Netmask          Gateway       Interface  Metric
          0.0.0.0          0.0.0.0      192.168.1.1     192.168.1.72     25
`,
	}
	for goos, out := range cases {
		if got := parseGateway(goos, out); got != "192.168.1.1" {
			t.Errorf("%s: %q", goos, got)
		}
	}
	if got := parseGateway("darwin", "route: writing to routing socket: not in table"); got != "" {
		t.Errorf("no gateway: %q", got)
	}
}

func TestRandomizedMAC(t *testing.T) {
	for mac, want := range map[string]bool{"aa:bb:cc:dd:ee:ff": true, "00:24:d4:a1:b2:c3": false, "": false} {
		if got := RandomizedMAC(mac); got != want {
			t.Errorf("%q: %v", mac, got)
		}
	}
}
