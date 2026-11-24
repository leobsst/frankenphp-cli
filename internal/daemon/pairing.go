package daemon

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/access"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// Pairing: the dashboard of the host shows a QR code holding a one-time
// code; the mobile app scans it and exchanges the code for a session at
// POST /api/v1/auth/pair, without typing the password.

// The limits of the pairing codes.
var (
	// pairingTTL is how long a pairing code stays valid.
	pairingTTL = 5 * time.Minute
	// maxPairingCodes bounds the codes valid at once: a new one replaces
	// the oldest beyond it.
	maxPairingCodes = 5
)

// pairingCodeBytes is the entropy of a pairing code (128 bits).
const pairingCodeBytes = 16

// pairingScheme is the URL the QR code holds, read by the mobile app.
const pairingScheme = "frankenmanager://pair"

// pairingCode is a valid code, kept as the SHA-256 of its text only.
type pairingCode struct {
	id      string // shown to the clients, to tell which code was used
	hash    [sha256.Size]byte
	expires time.Time
}

// pairingCodes are the codes valid now, in memory only: a restart of the
// daemon invalidates them.
type pairingCodes struct {
	mu    sync.Mutex
	codes []pairingCode // oldest first
}

// add records a new code valid until now+pairingTTL and returns its text
// and id.
func (p *pairingCodes) add(now time.Time) (code, id string, expires time.Time) {
	b := make([]byte, pairingCodeBytes)
	_, _ = rand.Read(b)
	code = base64.RawURLEncoding.EncodeToString(b)
	id = randomHex(8)
	expires = now.Add(pairingTTL)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prune(now)
	if len(p.codes) >= maxPairingCodes {
		p.codes = p.codes[len(p.codes)-maxPairingCodes+1:]
	}
	p.codes = append(p.codes, pairingCode{id: id, hash: sha256.Sum256([]byte(code)), expires: expires})
	return code, id, expires
}

// take consumes code: it reports the id of the code when it was valid, and
// it can never be used again. Every valid code is compared, in constant
// time.
func (p *pairingCodes) take(code string, now time.Time) (string, bool) {
	if code == "" {
		return "", false
	}
	sum := sha256.Sum256([]byte(code))
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prune(now)
	found := -1
	for i := range p.codes {
		if subtle.ConstantTimeCompare(sum[:], p.codes[i].hash[:]) == 1 {
			found = i
		}
	}
	if found < 0 {
		return "", false
	}
	id := p.codes[found].id
	p.codes = append(p.codes[:found], p.codes[found+1:]...)
	return id, true
}

// prune forgets the expired codes; p.mu is held.
func (p *pairingCodes) prune(now time.Time) {
	kept := p.codes[:0]
	for _, c := range p.codes {
		if now.Before(c.expires) {
			kept = append(kept, c)
		}
	}
	p.codes = kept
}

// Pairing is a new pairing code, as POST /api/v1/auth/pairing answers it.
type Pairing struct {
	ID      string    `json:"id"`
	Code    string    `json:"code"`
	Expires time.Time `json:"expires_at"`
	// URL is the base URL of the remote access, which the app calls
	URL  string `json:"url"`
	Name string `json:"name"`
	// CAFingerprint is the lowercase hex SHA-256 of the mkcert root CA
	// (DER), empty with a certificate of one's own
	CAFingerprint string `json:"ca_fingerprint,omitempty"`
	// QR is the text of the QR code
	QR string `json:"qr"`
}

// PairedEvent is the event stream message "paired": a device used a code.
type PairedEvent struct {
	ID      string `json:"id"` // of the code
	Device  string `json:"device"`
	Session string `json:"session"` // the id of its session
}

// pairingQR is the text of the QR code read by the mobile app.
func pairingQR(base, code, name, ca string) string {
	// In the order of the contract (url.Values.Encode sorts the keys)
	parts := []string{"v=1", "url=" + url.QueryEscape(base), "code=" + url.QueryEscape(code), "name=" + url.QueryEscape(name)}
	if ca != "" {
		parts = append(parts, "ca="+url.QueryEscape(ca))
	}
	return pairingScheme + "?" + strings.Join(parts, "&")
}

// createPairing makes a pairing code for a device to come: only a client
// already signed in (on the remote access, with the password again: see
// destructive), and only when the remote access is open, which the device
// will call.
func (s *Server) createPairing(w http.ResponseWriter, r *http.Request) {
	cfg := s.access()
	if !access.Remote(cfg.Mode) {
		writeError(w, http.StatusConflict, i18n.T("The remote access is closed (DASHBOARD_ACCESS=local): open it to the local network or the Internet to pair a device."))
		return
	}
	base := s.remoteURL(cfg)
	if base == "" {
		if s.remote.running() == "" {
			writeError(w, http.StatusConflict, i18n.T("The remote access is not running: set the dashboard password first, then check the daemon log."))
		} else {
			writeError(w, http.StatusConflict, i18n.T("The address of the remote access is unknown: this machine has no address on the local network."))
		}
		return
	}
	name, _ := os.Hostname()
	ca := ""
	if cfg.TLSCert == "" {
		ca = s.caFingerprint(r)
	}
	code, id, expires := s.pairings.add(time.Now())
	writeJSON(w, http.StatusOK, Pairing{ID: id, Code: code, Expires: expires.UTC(), URL: base, Name: name,
		CAFingerprint: ca, QR: pairingQR(base, code, name, ca)})
}

// pair exchanges a pairing code for a session, on the remote access only
// (remoteGuard: allowed addresses). A wrong code counts as a wrong
// password for the guessing protection.
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code   string `json:"code"`
		Device string `json:"device"`
	}
	rq := requesterOf(r)
	if !rq.remote {
		writeError(w, http.StatusNotFound, i18n.T("Not found."))
		return
	}
	if !readJSON(w, r, &req) {
		return
	}
	now := time.Now()
	if s.limiter.locked(rq.ip, now) {
		writeError(w, http.StatusTooManyRequests, i18n.T("Too many wrong passwords: try again later."))
		return
	}
	device := req.Device
	if device == "" {
		device = r.UserAgent()
	}
	if len(device) > 200 {
		device = device[:200]
	}
	id, ok := s.pairings.take(req.Code, now)
	if !ok {
		s.limiter.failed(rq.ip, now)
		s.logAccess(rq, "pair_failed", device)
		writeError(w, http.StatusUnauthorized, i18n.T("Invalid or expired pairing code: show a new one on the dashboard of the host."))
		return
	}
	s.limiter.succeeded(rq.ip)
	if sess := s.openSession(w, rq, device, "paired"); sess != nil {
		s.Bus.Publish("paired", PairedEvent{ID: id, Device: device, Session: sess.ID})
	}
}

// remoteURL is the base URL of the remote access, "" when it is off or
// its address unknown: the public name of a certificate of one's own
// (DASHBOARD_TLS_CERT), else the address of this machine on the local
// network.
func (s *Server) remoteURL(cfg AccessConfig) string {
	if s.remote.running() == "" {
		return ""
	}
	port := strconv.Itoa(cfg.RemotePort)
	if cfg.TLSCert != "" {
		if name := certName(cfg.TLSCert); name != "" {
			return "https://" + net.JoinHostPort(name, port)
		}
	}
	if s.LANIP != nil {
		if ip := s.LANIP(); ip != "" {
			return "https://" + net.JoinHostPort(ip, port)
		}
	}
	return ""
}

// certName is the first name a certificate file is made for, other than
// localhost and wildcards, "" when none.
func certName(file string) string {
	cert := readCert(file)
	if cert == nil {
		return ""
	}
	for _, n := range cert.DNSNames {
		if n != "localhost" && !strings.HasPrefix(n, "*") {
			return n
		}
	}
	return ""
}

// readCert parses the first certificate of a PEM file, nil when it cannot.
func readCert(file string) *x509.Certificate {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return nil
		}
		if block.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil
			}
			return cert
		}
	}
}

// caFingerprint is the lowercase hex SHA-256 of the mkcert root CA (DER)
// that signed the certificate of the remote access, "" when it cannot be
// read or did not sign it.
func (s *Server) caFingerprint(r *http.Request) string {
	if s.RootCAPath == nil {
		return ""
	}
	path, err := s.RootCAPath(r.Context())
	if err != nil {
		return ""
	}
	ca := readCert(path)
	if ca == nil {
		return ""
	}
	s.remote.mu.Lock()
	certFile, _, _ := strings.Cut(s.remote.cert, "\n")
	s.remote.mu.Unlock()
	if cert := readCert(certFile); cert == nil || cert.CheckSignatureFrom(ca) != nil {
		return ""
	}
	sum := sha256.Sum256(ca.Raw)
	return hex.EncodeToString(sum[:])
}
