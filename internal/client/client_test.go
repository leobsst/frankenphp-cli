package client

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/daemon"
)

// shortDir is a temporary data directory with a short path: a Unix socket
// path is limited to about 100 bytes.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "fm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// serve runs a daemon of version on the socket of dir, in this process,
// and returns a channel closed when it stops.
func serve(t *testing.T, dir, version string) <-chan struct{} {
	t.Helper()
	path := filepath.Join(dir, daemon.SocketFile)
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		_ = daemon.New(nil, "", 0, version).Serve(ctx, ln, nil, nil)
		close(stopped)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})
	return stopped
}

// starter starts a daemon of version in this process, counting the starts.
func starter(t *testing.T, version string, starts *int) Starter {
	return func(dir string) (<-chan struct{}, error) {
		*starts++
		return serve(t, dir, version), nil
	}
}

func TestEnsureStartsTheDaemon(t *testing.T) {
	dir := shortDir(t)
	starts := 0
	c, what, err := Ensure(context.Background(), dir, "1.0", starter(t, "1.0", &starts))
	if err != nil || what != StartedNew || starts != 1 {
		t.Fatalf("Ensure = %v, %v after %d starts", what, err, starts)
	}
	if v, err := c.Version(context.Background()); v != "1.0" || err != nil {
		t.Errorf("version %q, %v", v, err)
	}
	// Running: nothing to start
	if _, what, err := Ensure(context.Background(), dir, "1.0", starter(t, "1.0", &starts)); err != nil ||
		what != AlreadyRunning || starts != 1 {
		t.Errorf("second Ensure = %v, %v after %d starts", what, err, starts)
	}
}

// A CLI talks to the daemon of its own release: an older one is stopped
// and replaced.
func TestEnsureReplacesAnotherVersion(t *testing.T) {
	dir := shortDir(t)
	old := serve(t, dir, "1.0")
	starts := 0
	c, what, err := Ensure(context.Background(), dir, "2.0", starter(t, "2.0", &starts))
	if err != nil || what != Replaced || starts != 1 {
		t.Fatalf("Ensure = %v, %v after %d starts", what, err, starts)
	}
	// It stops answering before Serve returns: its shutdown ends right after
	select {
	case <-old:
	case <-time.After(10 * time.Second):
		t.Error("the old daemon still runs")
	}
	if v, _ := c.Version(context.Background()); v != "2.0" {
		t.Errorf("version %q, want 2.0", v)
	}
}

// A daemon that exits at once (port taken...) fails fast, pointing to its
// log.
func TestEnsureReportsADaemonThatExits(t *testing.T) {
	dir := shortDir(t)
	exited := func(string) (<-chan struct{}, error) {
		ch := make(chan struct{})
		close(ch)
		return ch, nil
	}
	_, _, err := Ensure(context.Background(), dir, "1.0", exited)
	if err == nil || !strings.Contains(err.Error(), LogFile) {
		t.Errorf("err = %v, want the log file", err)
	}
	if err := New(dir).Stop(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Stop = %v, want ErrNotRunning", err)
	}
}
