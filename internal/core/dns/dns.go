// Package dns writes the configuration of the optional LAN DNS server.
//
// Off by default: DNS_SERVER=true turns it on. Other devices then use this
// machine as their DNS server and reach every registered domain without
// editing their hosts file; other queries are forwarded to DNS_UPSTREAM.
// The server is CoreDNS embedded in the background process (see embeddns
// and dnswatcher), running on the host so that it sees the real address of
// each device: it reloads its records file on its own, and its Corefile
// when it changes, so domain and setting changes only need Sync.
package dns

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

const (
	DefaultPort     = "53"
	DefaultListenIP = "0.0.0.0" // every interface: a network change needs nothing
	// DefaultUpstream are public resolvers rather than the host's own: once
	// the router hands this server out, the host may resolve through it,
	// and forwarding to the host's resolver would loop back here.
	// The same default as DNS_UPSTREAM in the configuration registry.
	DefaultUpstream = "1.1.1.1 8.8.8.8"
	// RecordsTTL is short, so that removed domains stop resolving quickly.
	RecordsTTL = 30
)

// virtualPrefixes are the interfaces of containers and VMs, unreachable
// from the other devices of the LAN.
var virtualPrefixes = []string{"docker", "br-", "veth", "virbr", "vboxnet", "vmnet", "cni", "flannel", "podman"}

// Error is a configuration error.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// Config reads the configuration.
type Config interface {
	Get(key string) string
}

// Enabled reports whether DNS_SERVER is exactly "true".
func Enabled(c Config) bool { return c.Get("DNS_SERVER") == "true" }

// Dir is the directory of the CoreDNS configuration.
func Dir(dataDir string) string { return filepath.Join(dataDir, "dns") }

// CorefilePath is the Corefile the embedded server loads.
func CorefilePath(dataDir string) string { return filepath.Join(Dir(dataDir), "Corefile") }

// LANIP returns the host's current LAN IP, replaced in tests.
var LANIP = platform.LANIP

// parseIP validates an address and returns its canonical form.
func parseIP(name, value string) (string, error) {
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return "", &Error{i18n.Tf("Invalid %s '%s': '%s' does not appear to be an IPv4 or IPv6 address",
			name, value, value)}
	}
	return addr.String(), nil
}

// ResolveIP returns the address the domains resolve to: DNS_IP, or the
// current LAN IP when unset.
func ResolveIP(c Config) (string, error) {
	configured := c.Get("DNS_IP")
	if configured == "" {
		return LANIP(), nil
	}
	return parseIP("DNS_IP", configured)
}

// listen returns the IP and port the server listens on: every interface by
// default, DNS_LISTEN_IP to pin one (e.g. next to systemd-resolved, already
// bound to port 53).
func listen(c Config) (ip, port string, err error) {
	ip = DefaultListenIP
	if configured := c.Get("DNS_LISTEN_IP"); configured != "" {
		if ip, err = parseIP("DNS_LISTEN_IP", configured); err != nil {
			return "", "", err
		}
	}
	port = c.Get("DNS_PORT")
	if port == "" {
		port = DefaultPort
	}
	return ip, port, nil
}

// ListenAddress returns the "ip:port" the server listens on, "" when
// disabled.
func ListenAddress(c Config) (string, error) {
	if !Enabled(c) {
		return "", nil
	}
	ip, port, err := listen(c)
	if err != nil {
		return "", err
	}
	if strings.Contains(ip, ":") {
		ip = "[" + ip + "]"
	}
	return ip + ":" + port, nil
}

// Address is a host address, with its interface ("" when unknown).
type Address struct {
	Interface string
	IP        string
}

// Interfaces lists the host's IPv4 addresses by interface, in interface
// order. Replaced in tests.
var Interfaces = func() ([]Address, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var addrs []Address
	for _, iface := range ifaces {
		list, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range list {
			if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP.To4() != nil {
				addrs = append(addrs, Address{iface.Name, ipNet.IP.To4().String()})
			}
		}
	}
	return addrs, nil
}

// lanCandidate reports whether another device could reach this host at ip.
func lanCandidate(a Address) bool {
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(a.Interface, p) {
			return false
		}
	}
	addr, err := netip.ParseAddr(a.IP)
	return err == nil && !addr.IsLoopback() && !addr.IsLinkLocalUnicast() && !addr.IsUnspecified()
}

// InterfaceIPs lists the host's IPv4 addresses reachable from the LAN,
// without duplicates. The interfaces come from net.Interfaces(), so their
// names are known on every platform, Windows included.
func InterfaceIPs() []Address {
	all, _ := Interfaces()
	seen := map[string]bool{}
	var out []Address
	for _, a := range all {
		if !seen[a.IP] && lanCandidate(a) {
			seen[a.IP] = true
			out = append(out, a)
		}
	}
	return out
}

// ServerAddresses returns the addresses the other devices can use as DNS
// server: DNS_LISTEN_IP when pinned, otherwise every LAN address, the one
// the domains resolve to first.
func ServerAddresses(c Config) ([]Address, error) {
	if configured := c.Get("DNS_LISTEN_IP"); configured != "" {
		ip, err := parseIP("DNS_LISTEN_IP", configured)
		if err != nil {
			return nil, err
		}
		if ip != DefaultListenIP {
			return []Address{{"", ip}}, nil
		}
	}
	addrs := InterfaceIPs()
	recordsIP, err := ResolveIP(c)
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(addrs, func(a, b Address) int {
		return boolRank(a.IP != recordsIP) - boolRank(b.IP != recordsIP)
	})
	if len(addrs) == 0 && !strings.HasPrefix(recordsIP, "127.") {
		addrs = []Address{{"", recordsIP}}
	}
	return addrs, nil
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// DescribeServerAddresses returns the lines telling which addresses to give
// the other devices (start and status), none when no address is known.
func DescribeServerAddresses(addrs []Address, recordsIP string) []string {
	switch len(addrs) {
	case 0:
		return nil
	case 1:
		return []string{i18n.Tf("  Set %s as the DNS server of your other devices (or of your router).", addrs[0].IP)}
	}
	lines := []string{i18n.T("  Set one of these IPs as the DNS server of your other devices (or of your router):")}
	for _, a := range addrs {
		var details []string
		if a.Interface != "" {
			details = append(details, a.Interface)
		}
		if a.IP == recordsIP {
			details = append(details, i18n.T("domains resolve to it"))
		}
		suffix := ""
		if len(details) > 0 {
			suffix = " (" + strings.Join(details, ", ") + ")"
		}
		lines = append(lines, "    - "+a.IP+suffix)
	}
	return append(lines, i18n.T("  On the same network, set two of them as primary and secondary DNS servers: "+
		"the devices keep resolving if one interface goes down."))
}

// Server is what the Corefile describes.
type Server struct {
	Records  string // path of the records file
	ListenIP string // DefaultListenIP for every interface
	Port     string
	Upstream string // space-separated resolvers
}

// Corefile returns the CoreDNS configuration. The "clients" plugin (see
// embeddns) reports the address of every query; "bind" pins the listen IP.
// The records path is quoted, with slashes: Windows paths may hold spaces
// and backslashes.
func Corefile(s Server) string {
	bind := ""
	if s.ListenIP != "" && s.ListenIP != DefaultListenIP {
		bind = "    bind " + s.ListenIP + "\n"
	}
	return fmt.Sprintf(`.:%s {
%s    clients
    hosts "%s" {
        ttl %d
        reload 2s
        fallthrough
    }
    forward . %s
    cache %d
    errors
}
`, s.Port, bind, filepath.ToSlash(s.Records), RecordsTTL, s.Upstream, RecordsTTL)
}

// Records returns the hosts-format records pointing every domain at ip,
// sorted, without duplicates.
func Records(ip string, domains []string) string {
	sorted := slices.Compact(slices.Sorted(slices.Values(domains)))
	lines := []string{"# Generated by FrankenManager, rewritten on every domain change."}
	for _, d := range sorted {
		lines = append(lines, ip+" "+d)
	}
	return strings.Join(lines, "\n") + "\n"
}

// writeAtomic replaces path in one step: CoreDNS never reloads a
// half-written file.
func writeAtomic(path, content string) error {
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Registry lists the registered hosts (store.Store).
type Registry interface {
	DomainNames() ([]string, error)
	Aliases() ([]store.Alias, error)
	Proxies() ([]store.Proxy, error)
}

// Sync writes the Corefile and the records of every domain, alias and
// proxy (plus the Soketi host), all served by this host's reverse proxy.
// Does nothing when disabled.
func Sync(dataDir string, c Config, r Registry) error {
	if !Enabled(c) {
		return nil
	}
	domains, err := r.DomainNames()
	if err != nil {
		return err
	}
	aliases, err := r.Aliases()
	if err != nil {
		return err
	}
	for _, a := range aliases {
		domains = append(domains, a.Alias)
	}
	proxies, err := r.Proxies()
	if err != nil {
		return err
	}
	for _, p := range proxies {
		domains = append(domains, p.Domain)
	}
	domains = append(domains, soketi.Domains(c)...)

	ip, err := ResolveIP(c)
	if err != nil {
		return err
	}
	upstream := c.Get("DNS_UPSTREAM")
	if upstream == "" {
		upstream = DefaultUpstream
	}
	listenIP, port, err := listen(c)
	if err != nil {
		return err
	}
	dir, err := filepath.Abs(Dir(dataDir))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	records := filepath.Join(dir, "hosts")
	// The records first: a new Corefile is loaded at once
	if err := writeAtomic(records, Records(ip, domains)); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, "Corefile"),
		Corefile(Server{Records: records, ListenIP: listenIP, Port: port, Upstream: upstream}))
}
