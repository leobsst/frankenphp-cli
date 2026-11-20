package access

import (
	"errors"
	"net/netip"
	"testing"
)

func TestPassword(t *testing.T) {
	if _, err := HashPassword("short"); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("weak password: %v", err)
	}
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !IsHash(hash) || hash == "correct horse battery" {
		t.Fatalf("hash = %q", hash)
	}
	if !CheckPassword(hash, "correct horse battery") {
		t.Error("right password refused")
	}
	for _, wrong := range []string{"", "correct horse batter", "Correct horse battery"} {
		if CheckPassword(hash, wrong) {
			t.Errorf("%q accepted", wrong)
		}
	}
	// A stored value that is not a hash never matches, even itself
	if CheckPassword("correct horse battery", "correct horse battery") || CheckPassword("", "") {
		t.Error("plain value accepted")
	}
	// Salted: two hashes of one password differ
	if again, _ := HashPassword("correct horse battery"); again == hash {
		t.Error("no salt")
	}
}

func TestAllowList(t *testing.T) {
	if _, err := ParseAllowList("10.0.0.1, nope"); err == nil {
		t.Error("invalid address accepted")
	}
	if _, err := ParseAllowList("10.0.0.0/33"); err == nil {
		t.Error("invalid network accepted")
	}
	l, err := ParseAllowList("192.168.1.0/24, 203.0.113.7 2001:db8::/32\n::1")
	if err != nil {
		t.Fatal(err)
	}
	for addr, want := range map[string]bool{
		"192.168.1.42": true, "192.168.2.1": false, "203.0.113.7": true, "203.0.113.8": false,
		"2001:db8::5": true, "2001:db9::5": false, "::1": true, "::ffff:192.168.1.9": true,
	} {
		if got := l.Allows(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Allows(%s) = %v", addr, got)
		}
	}
	if empty, _ := ParseAllowList(" "); !empty.Allows(netip.MustParseAddr("8.8.8.8")) {
		t.Error("an empty list must allow every address")
	}
}

func TestPrivate(t *testing.T) {
	for addr, want := range map[string]bool{
		"192.168.1.2": true, "10.1.2.3": true, "172.16.0.1": true, "127.0.0.1": true, "100.100.1.1": true,
		"169.254.1.1": true, "fd00::1": true, "fe80::1": true, "::1": true, "::ffff:10.0.0.1": true,
		"8.8.8.8": false, "2001:4860:4860::8888": false, "172.32.0.1": false,
	} {
		if got := Private(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Private(%s) = %v", addr, got)
		}
	}
}
