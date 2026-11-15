// Package dnsclients tracks the devices using the LAN DNS server: the
// embedded server reports each query to a Tracker, which the background
// process writes to db.sqlite in batches; the CLI and the dashboard read
// them back with the store and Status.
package dnsclients

import (
	"context"
	"sync"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

const (
	// ActiveWindow is how long after its last query a device counts as
	// connected. Devices re-query often: the records TTL is 30 s.
	ActiveWindow = 5 * time.Minute
	// IdentityTTL is how long a found name and MAC are kept before being
	// looked up again (DHCP leases and ARP entries change).
	IdentityTTL = time.Hour
	// Retention is how long a device no longer seen is kept.
	Retention = 30 * 24 * time.Hour
)

// Status of a device.
const (
	Connected = "connected"
	Idle      = "idle"
)

// Status returns Connected when c queried the server within ActiveWindow
// before now, Idle otherwise.
func Status(c store.DNSClient, now time.Time) string {
	if now.Sub(c.LastSeen) <= ActiveWindow {
		return Connected
	}
	return Idle
}

// Store is where the clients are kept (store.Store).
type Store interface {
	RecordDNSClients(map[string]store.DNSSighting) error
	DNSClients() ([]store.DNSClient, error)
	SetDNSClientIdentity(ip, name, mac string, at time.Time) error
	PruneDNSClients(before time.Time) (int64, error)
}

// Tracker counts the queries in memory, off the query path's way: Seen
// only takes a lock.
type Tracker struct {
	Now func() time.Time

	mu      sync.Mutex
	pending map[string]store.DNSSighting
}

// NewTracker returns an empty Tracker on the real clock.
func NewTracker() *Tracker { return &Tracker{Now: time.Now} }

// Seen counts one query from ip.
func (t *Tracker) Seen(ip string) {
	now := t.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending == nil {
		t.pending = map[string]store.DNSSighting{}
	}
	v := t.pending[ip]
	v.Queries++
	v.Last = now
	t.pending[ip] = v
}

// take returns the pending sightings and starts a new batch.
func (t *Tracker) take() map[string]store.DNSSighting {
	t.mu.Lock()
	defer t.mu.Unlock()
	taken := t.pending
	t.pending = nil
	return taken
}

// giveBack merges sightings that could not be written into the batch.
func (t *Tracker) giveBack(sightings map[string]store.DNSSighting) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending == nil {
		t.pending = map[string]store.DNSSighting{}
	}
	for ip, v := range sightings {
		p := t.pending[ip]
		p.Queries += v.Queries
		if v.Last.After(p.Last) {
			p.Last = v.Last
		}
		t.pending[ip] = p
	}
}

// Flush writes the pending sightings and returns the IPs of the written
// clients whose name and MAC are unknown or older than IdentityTTL. On a
// write error the sightings are kept for the next flush.
func (t *Tracker) Flush(s Store) ([]string, error) {
	taken := t.take()
	if len(taken) == 0 {
		return nil, nil
	}
	if err := s.RecordDNSClients(taken); err != nil {
		t.giveBack(taken)
		return nil, err
	}
	clients, err := s.DNSClients()
	if err != nil {
		return nil, err
	}
	now := t.Now()
	var stale []string
	for _, c := range clients {
		if _, ok := taken[c.IP]; ok && (c.IdentityAt.IsZero() || now.Sub(c.IdentityAt) > IdentityTTL) {
			stale = append(stale, c.IP)
		}
	}
	return stale, nil
}

// Identify looks up and stores the name and MAC of ips. A failed lookup
// stores empty values, so that it is not retried before IdentityTTL.
func Identify(ctx context.Context, s Store, id Identifier, ips []string, now time.Time) error {
	for _, ip := range ips {
		name, mac := id.Identify(ctx, ip)
		if err := s.SetDNSClientIdentity(ip, name, mac, now); err != nil {
			return err
		}
	}
	return nil
}

// Prune removes the devices not seen for Retention.
func Prune(s Store, now time.Time) error {
	_, err := s.PruneDNSClients(now.Add(-Retention))
	return err
}
