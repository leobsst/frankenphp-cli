// Package access holds the rules of the remote access to the dashboard:
// the password hash, the allowed addresses and the access modes. Standard
// library only.
package access

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/netip"
	"strconv"
	"strings"
)

// The access modes of the dashboard (DASHBOARD_ACCESS).
const (
	// Local: the loopback of the host only, the password optional.
	Local = "local"
	// LAN: also the private networks (HTTPS, password required).
	LAN = "lan"
	// Internet: also any address (HTTPS, password required): through a
	// router or a tunnel.
	Internet = "internet"
)

// Modes are the values of DASHBOARD_ACCESS.
var Modes = []string{Local, LAN, Internet}

// Remote reports whether mode opens the dashboard beyond the host.
func Remote(mode string) bool { return mode == LAN || mode == Internet }

// --- Password ----------------------------------------------------------------

const (
	hashPrefix = "pbkdf2-sha256"
	// iterations follows the OWASP recommendation for PBKDF2-HMAC-SHA256.
	iterations = 600_000
	saltSize   = 16
	keySize    = 32
)

// MinPasswordLength is the shortest password accepted.
const MinPasswordLength = 10

// ErrWeakPassword is raised for a password too short.
var ErrWeakPassword = errors.New("password too short")

// IsHash reports whether v is a stored password hash.
func IsHash(v string) bool { return strings.HasPrefix(v, hashPrefix+"$") }

// HashPassword returns the stored form of password:
// pbkdf2-sha256$<iterations>$<salt>$<key>, base64 without padding.
func HashPassword(password string) (string, error) {
	if len([]rune(password)) < MinPasswordLength {
		return "", ErrWeakPassword
	}
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, keySize)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return strings.Join([]string{hashPrefix, strconv.Itoa(iterations), enc.EncodeToString(salt), enc.EncodeToString(key)}, "$"), nil
}

// CheckPassword reports whether password matches hash, in constant time.
func CheckPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != hashPrefix {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 || iter > 10_000_000 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// --- Allowed addresses ------------------------------------------------------

// AllowList is a list of allowed addresses and networks; empty allows
// every address.
type AllowList []netip.Prefix

// ParseAllowList reads addresses and CIDR networks, IPv4 or IPv6,
// separated by commas or spaces.
func ParseAllowList(v string) (AllowList, error) {
	var list AllowList
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		if strings.Contains(f, "/") {
			p, err := netip.ParsePrefix(f)
			if err != nil {
				return nil, errors.New("invalid network " + f)
			}
			list = append(list, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(f)
		if err != nil {
			return nil, errors.New("invalid address " + f)
		}
		list = append(list, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return list, nil
}

// Allows reports whether addr is allowed.
func (l AllowList) Allows(addr netip.Addr) bool {
	if len(l) == 0 {
		return true
	}
	addr = addr.Unmap()
	for _, p := range l {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// cgnat is the shared address space (RFC 6598) of the overlay networks
// (Tailscale...).
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// Private reports whether addr belongs to a local network: private (RFC
// 1918, IPv6 ULA), link-local, loopback or shared (CGNAT).
func Private(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || cgnat.Contains(addr)
}
