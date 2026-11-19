package dnswatcher

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

// The daemon serves only when DNS_SERVER=true, stops cleanly, and serves
// again after a failure.
func TestHost(t *testing.T) {
	old := retryDelays
	retryDelays = []time.Duration{time.Millisecond}
	defer func() { retryDelays = old }()

	var serves atomic.Int32
	failing := atomic.Bool{}
	failing.Store(true)
	h := &Host{DataDir: t.TempDir(), Log: io.Discard, Serve: func(ctx context.Context, _ string, _ io.Writer) error {
		serves.Add(1)
		if failing.Load() {
			return errors.New("port refused")
		}
		<-ctx.Done()
		return nil
	}}

	if h.Apply(memConfig{"DNS_SERVER": "false"}) || h.Running() || serves.Load() != 0 {
		t.Fatal("serves while DNS_SERVER=false")
	}
	if !h.Apply(memConfig{"DNS_SERVER": "true"}) || !h.Running() {
		t.Fatal("does not serve while DNS_SERVER=true")
	}
	deadline := time.Now().Add(5 * time.Second)
	for serves.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if serves.Load() < 3 {
		t.Fatalf("served %d times after failures, want retries", serves.Load())
	}
	failing.Store(false)
	h.Stop()
	if h.Running() {
		t.Error("still running after Stop")
	}
}
