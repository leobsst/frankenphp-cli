package daemon

import (
	"net/netip"
	"testing"
)

// The network of a device: a private IPv4 address on a network of this
// machine, never a public address, IPv6 or a single-address network.
func TestNetworkOf(t *testing.T) {
	nets := []netip.Prefix{
		netip.MustParsePrefix("192.168.0.0/24"),
		netip.MustParsePrefix("10.20.0.0/16"),
		netip.MustParsePrefix("100.101.102.103/32"), // Tailscale
		netip.MustParsePrefix("fd00::/64"),
	}
	for _, c := range []struct {
		ip   string
		want ClientNetwork
	}{
		{"192.168.0.23", ClientNetwork{IP: "192.168.0.23", Prefix: 24}},
		{"10.20.3.4", ClientNetwork{IP: "10.20.3.4", Prefix: 16}},
		{"192.168.5.6", ClientNetwork{}},     // private, not a network of this machine
		{"100.101.102.104", ClientNetwork{}}, // CGNAT, not private
		{"8.8.8.8", ClientNetwork{}},
		{"fd00::5", ClientNetwork{}},
		{"127.0.0.1", ClientNetwork{}},
	} {
		if got := networkOf(netip.MustParseAddr(c.ip), nets); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.ip, got, c.want)
		}
	}
}
