package dnsclients

import (
	"bufio"
	"context"
	"encoding/hex"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ThisMachine is the name of the host's own addresses.
const ThisMachine = "this machine"

// LookupTimeout bounds each name lookup.
const LookupTimeout = time.Second

// Identifier puts a name and a MAC on an IP, best effort. Each function
// may fail; the zero value of a missing one is "unknown".
type Identifier struct {
	// Gateway returns the default gateway IP (the router).
	Gateway func(ctx context.Context) (string, error)
	// LookupAddr returns the PTR names of ip, asked to server ("" for the
	// system resolver).
	LookupAddr func(ctx context.Context, server, ip string) ([]string, error)
	// ARP returns the host's ARP table, IP -> MAC.
	ARP func(ctx context.Context) (map[string]string, error)
	// Self returns the host's own IPs.
	Self func() []string
}

// SystemIdentifier asks the router and the OS.
func SystemIdentifier() Identifier {
	return Identifier{Gateway: systemGateway, LookupAddr: lookupAddr, ARP: systemARP, Self: selfIPs}
}

// Identify returns the name and MAC of ip, "" for each one unknown. The
// router is asked first: it knows the DHCP host names, the public
// upstreams do not.
func (id Identifier) Identify(ctx context.Context, ip string) (name, mac string) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return "", ""
	}
	if addr.IsLoopback() || id.isSelf(addr) {
		return ThisMachine, ""
	}
	if id.LookupAddr != nil {
		var servers []string
		if id.Gateway != nil {
			if gw, err := id.Gateway(ctx); err == nil && gw != "" {
				servers = append(servers, gw)
			}
		}
		for _, server := range append(servers, "") {
			lctx, cancel := context.WithTimeout(ctx, LookupTimeout)
			names, err := id.LookupAddr(lctx, server, ip)
			cancel()
			if err == nil && len(names) > 0 && names[0] != "" {
				name = strings.TrimSuffix(names[0], ".")
				break
			}
		}
	}
	if id.ARP != nil {
		if table, err := id.ARP(ctx); err == nil {
			mac = table[addr.Unmap().String()]
		}
	}
	return name, mac
}

func (id Identifier) isSelf(addr netip.Addr) bool {
	if id.Self == nil {
		return false
	}
	for _, s := range id.Self() {
		if a, err := netip.ParseAddr(s); err == nil && a.Unmap() == addr.Unmap() {
			return true
		}
	}
	return false
}

// RandomizedMAC reports whether mac is locally administered, as the private
// addresses of phones and recent laptops are: it changes per network.
func RandomizedMAC(mac string) bool {
	first, _, ok := strings.Cut(mac, ":")
	if !ok {
		return false
	}
	b, err := strconv.ParseUint(first, 16, 8)
	return err == nil && b&0x02 != 0
}

// normalizeMAC returns mac as lowercase aa:bb:cc:dd:ee:ff ("-" separators
// and one-digit octets accepted), "" when it is not a usable MAC.
func normalizeMAC(mac string) string {
	parts := strings.FieldsFunc(strings.ToLower(mac), func(r rune) bool { return r == ':' || r == '-' })
	if len(parts) != 6 {
		return ""
	}
	zero := true
	for i, p := range parts {
		if len(p) == 1 {
			p = "0" + p
		}
		if len(p) != 2 {
			return ""
		}
		if _, err := hex.DecodeString(p); err != nil {
			return ""
		}
		if p != "00" {
			zero = false
		}
		parts[i] = p
	}
	if zero || strings.Join(parts, ":") == "ff:ff:ff:ff:ff:ff" {
		return ""
	}
	return strings.Join(parts, ":")
}

// --- Parsers -----------------------------------------------------------------

var (
	// ? (192.168.1.42) at aa:bb:cc:dd:ee:ff on en0 ifscope [ethernet]
	darwinARPLine = regexp.MustCompile(`\((\d+\.\d+\.\d+\.\d+)\) at ([0-9a-fA-F:]+) `)
	// 192.168.1.42          aa-bb-cc-dd-ee-ff     dynamic
	windowsARPLine = regexp.MustCompile(`^\s*(\d+\.\d+\.\d+\.\d+)\s+([0-9a-fA-F-]{17})\s`)
)

// parseARP reads the ARP table output of goos: `arp -an` on macOS,
// /proc/net/arp on Linux, `arp -a` on Windows.
func parseARP(goos, out string) map[string]string {
	table := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		var ip, mac string
		switch goos {
		case "darwin":
			if m := darwinARPLine.FindStringSubmatch(line); m != nil {
				ip, mac = m[1], m[2]
			}
		case "linux":
			// IP address  HW type  Flags  HW address  Mask  Device
			f := strings.Fields(line)
			if len(f) >= 4 && f[2] != "0x0" {
				ip, mac = f[0], f[3]
			}
		case "windows":
			if m := windowsARPLine.FindStringSubmatch(line); m != nil {
				ip, mac = m[1], m[2]
			}
		}
		if mac = normalizeMAC(mac); mac != "" && net.ParseIP(ip) != nil {
			table[ip] = mac
		}
	}
	return table
}

var (
	darwinGateway  = regexp.MustCompile(`(?m)^\s*gateway:\s*(\S+)`)
	windowsGateway = regexp.MustCompile(`(?m)^\s*0\.0\.0\.0\s+0\.0\.0\.0\s+(\d+\.\d+\.\d+\.\d+)\s`)
)

// parseGateway reads the default gateway from the output of goos: `route
// -n get default` on macOS, /proc/net/route on Linux, `route print -4
// 0.0.0.0` on Windows. "" when none.
func parseGateway(goos, out string) string {
	switch goos {
	case "darwin":
		if m := darwinGateway.FindStringSubmatch(out); m != nil && net.ParseIP(m[1]) != nil {
			return m[1]
		}
	case "linux":
		// Iface  Destination  Gateway  Flags ...  (hex, little endian)
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) < 3 || f[1] != "00000000" || f[2] == "00000000" {
				continue
			}
			b, err := hex.DecodeString(f[2])
			if err != nil || len(b) != 4 {
				continue
			}
			return net.IPv4(b[3], b[2], b[1], b[0]).String()
		}
	case "windows":
		if m := windowsGateway.FindStringSubmatch(out); m != nil {
			return m[1]
		}
	}
	return ""
}

// --- System ------------------------------------------------------------------

func output(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

func systemARP(ctx context.Context) (map[string]string, error) {
	var out string
	var err error
	switch runtime.GOOS {
	case "linux":
		var b []byte
		b, err = os.ReadFile("/proc/net/arp")
		out = string(b)
	case "windows":
		out, err = output(ctx, "arp", "-a")
	default:
		out, err = output(ctx, "arp", "-an")
	}
	if err != nil {
		return nil, err
	}
	return parseARP(runtime.GOOS, out), nil
}

func systemGateway(ctx context.Context) (string, error) {
	var out string
	var err error
	switch runtime.GOOS {
	case "linux":
		var b []byte
		b, err = os.ReadFile("/proc/net/route")
		out = string(b)
	case "windows":
		out, err = output(ctx, "route", "print", "-4", "0.0.0.0")
	default:
		out, err = output(ctx, "route", "-n", "get", "default")
	}
	if err != nil {
		return "", err
	}
	return parseGateway(runtime.GOOS, out), nil
}

// lookupAddr asks server (port 53) for the PTR of ip, or the system
// resolver when server is "".
func lookupAddr(ctx context.Context, server, ip string) ([]string, error) {
	if server == "" {
		return net.DefaultResolver.LookupAddr(ctx, ip)
	}
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, net.JoinHostPort(server, "53"))
	}}
	return r.LookupAddr(ctx, ip)
}

func selfIPs() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var ips []string
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			ips = append(ips, n.IP.String())
		}
	}
	return ips
}
