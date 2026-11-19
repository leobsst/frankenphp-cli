package dnswatcher

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// Host runs the LAN DNS server in this process (the daemon) rather than a
// background one, and serves again after a failure (port refused, network
// gone...), waiting longer each time.
type Host struct {
	DataDir string
	Log     io.Writer // the error output
	// Serve is the server, Serve by default (replaced in tests)
	Serve func(ctx context.Context, dataDir string, stderr io.Writer) error

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// Retry delays after a failure, the last one repeated.
var retryDelays = []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute}

// Apply serves when the LAN DNS server is on, stops it otherwise, and
// reports whether it runs. A running server is restarted, to read the
// DNS_* settings again.
func (h *Host) Apply(c Config) bool {
	h.Stop()
	if !Needed(c) {
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	h.mu.Lock()
	h.cancel, h.done = cancel, done
	h.mu.Unlock()
	go h.loop(ctx, done)
	return true
}

func (h *Host) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	serve := h.Serve
	if serve == nil {
		serve = Serve
	}
	for attempt := 0; ; attempt++ {
		err := serve(ctx, h.DataDir, h.Log)
		if ctx.Err() != nil {
			return
		}
		delay := retryDelays[min(attempt, len(retryDelays)-1)]
		_, _ = fmt.Fprintf(h.Log, "%s %s\n", time.Now().Format("2006-01-02T15:04:05"),
			i18n.Tf("LAN DNS server stopped (%v): retrying in %s", err, delay))
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// Stop stops the server and waits until it has written the pending
// devices.
func (h *Host) Stop() {
	h.mu.Lock()
	cancel, done := h.cancel, h.done
	h.cancel, h.done = nil, nil
	h.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

// Running reports whether the server runs (or waits to serve again).
func (h *Host) Running() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cancel != nil
}
