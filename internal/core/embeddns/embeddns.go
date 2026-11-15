// Package embeddns runs the LAN DNS server: CoreDNS embedded in the
// frankenmanager process, with only the plugins the Corefile uses (see
// dns.Corefile) plus "clients", which reports the address of every query.
//
// It runs on the host, not in a container: behind Docker's VM on macOS and
// Windows, a container only sees the VM gateway as the client of every
// query.
package embeddns

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"

	// The plugins of the Corefile
	_ "github.com/coredns/coredns/plugin/bind"
	_ "github.com/coredns/coredns/plugin/cache"
	_ "github.com/coredns/coredns/plugin/errors"
	_ "github.com/coredns/coredns/plugin/forward"
	_ "github.com/coredns/coredns/plugin/hosts"
)

// directives is the plugin chain order (CoreDNS's plugin.cfg order, which
// the Corefile order does not change), restricted to the plugins used:
// clients before cache, so that cached answers are reported too.
var directives = []string{"bind", "errors", "clients", "cache", "hosts", "forward"}

// ReloadInterval is how often the Corefile is checked for changes.
var ReloadInterval = 2 * time.Second

// Options configures Run.
type Options struct {
	// Corefile is the path of the configuration to serve.
	Corefile string
	// OnQuery is called with the client IP of every query; it must not
	// block. Nil ignores them.
	OnQuery func(ip string)
	// Log receives the server's messages (reloads, errors); nil is stderr.
	Log io.Writer
}

var (
	setupOnce sync.Once
	// running guards the process-wide CoreDNS state: one server at a time.
	running sync.Mutex
	// onQuery is read by the server goroutines.
	onQuery atomic.Pointer[func(string)]
)

func setup() {
	setupOnce.Do(func() {
		dnsserver.Directives = directives
		dnsserver.Quiet = true
		caddy.Quiet = true
		caddy.AppName = "frankenmanager-dns"
		registerClients()
	})
}

// ErrRunning is returned when a server already runs in this process.
var ErrRunning = errors.New("the LAN DNS server already runs in this process")

// Run serves o.Corefile until ctx ends, then stops the server. A changed
// Corefile is loaded without dropping the server; an invalid one is logged
// and the previous configuration keeps serving. The error is the one of
// the first start (e.g. port already in use, permission denied).
func Run(ctx context.Context, o Options) error {
	if !running.TryLock() {
		return ErrRunning
	}
	defer running.Unlock()
	setup()
	if o.OnQuery != nil {
		onQuery.Store(&o.OnQuery)
	}
	defer onQuery.Store(nil)
	if o.Log == nil {
		o.Log = os.Stderr
	}
	// CoreDNS and its plugins log through the standard logger
	log.SetOutput(o.Log)
	log.SetFlags(log.LstdFlags)

	content, err := os.ReadFile(o.Corefile)
	if err != nil {
		return err
	}
	inst, err := caddy.Start(input(o.Corefile, content))
	if err != nil {
		return fmt.Errorf("start the LAN DNS server: %w", err)
	}

	ticker := time.NewTicker(ReloadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return inst.Stop()
		case <-ticker.C:
		}
		next, err := os.ReadFile(o.Corefile)
		if err != nil || string(next) == string(content) {
			continue // a missing file keeps the running configuration
		}
		content = next
		restarted, err := inst.Restart(input(o.Corefile, next))
		if err != nil {
			_, _ = fmt.Fprintf(o.Log, "LAN DNS server: invalid Corefile, keeping the previous one: %v\n", err)
			continue
		}
		inst = restarted
	}
}

func input(path string, content []byte) caddy.CaddyfileInput {
	return caddy.CaddyfileInput{Contents: content, Filepath: path, ServerTypeName: "dns"}
}
