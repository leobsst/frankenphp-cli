package embeddns

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	fmdns "github.com/leobsst/frankenphp-cli/internal/core/dns"
)

// freePort returns a UDP port of 127.0.0.1 free right now.
func freePort(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pc.Close() }()
	return strconv.Itoa(pc.LocalAddr().(*net.UDPAddr).Port)
}

// writeConfig writes the records of app.test and a Corefile on 127.0.0.1
// port; the upstream is unreachable, so nothing leaves the machine.
func writeConfig(t *testing.T, dir, port string) string {
	t.Helper()
	records := filepath.Join(dir, "hosts")
	if err := os.WriteFile(records, []byte(fmdns.Records("192.168.1.20", []string{"app.test"})), 0o644); err != nil {
		t.Fatal(err)
	}
	corefile := filepath.Join(dir, "Corefile")
	content := fmdns.Corefile(fmdns.Server{Records: records, ListenIP: "127.0.0.1", Port: port, Upstream: "127.0.0.1:9"})
	if err := os.WriteFile(corefile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return corefile
}

// query asks port for the A record of name and returns the answer, "" when
// nothing answers.
func query(port, name string) string {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), dns.TypeA)
	c := &dns.Client{Timeout: 300 * time.Millisecond}
	r, _, err := c.Exchange(m, "127.0.0.1:"+port)
	if err != nil || len(r.Answer) == 0 {
		return ""
	}
	if a, ok := r.Answer[0].(*dns.A); ok {
		return a.A.String()
	}
	return ""
}

// eventually retries cond for up to 5 s.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestRunServesRecordsReportsClientsAndReloads(t *testing.T) {
	old := ReloadInterval
	ReloadInterval = 100 * time.Millisecond
	t.Cleanup(func() { ReloadInterval = old })

	dir := t.TempDir()
	port := freePort(t)
	corefile := writeConfig(t, dir, port)

	var mu sync.Mutex
	var seen []string
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{Corefile: corefile, Log: io.Discard, OnQuery: func(ip string) {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, ip)
		}})
	}()

	eventually(t, "the record", func() bool { return query(port, "app.test") == "192.168.1.20" })
	mu.Lock()
	if len(seen) == 0 || seen[0] != "127.0.0.1" {
		t.Errorf("clients seen: %v", seen)
	}
	mu.Unlock()

	// A second server in the same process is refused
	if err := Run(context.Background(), Options{Corefile: corefile}); !errors.Is(err, ErrRunning) {
		t.Errorf("second Run: %v", err)
	}

	// A new Corefile is loaded without restarting: the new port answers
	newPort := freePort(t)
	writeConfig(t, dir, newPort)
	eventually(t, "the new port", func() bool { return query(newPort, "app.test") == "192.168.1.20" })
	eventually(t, "the old port to close", func() bool { return query(port, "app.test") == "" })

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if query(newPort, "app.test") != "" {
		t.Error("still answering after the context ended")
	}
}

func TestRunReportsABusyPort(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pc.Close() }()
	port := strconv.Itoa(pc.LocalAddr().(*net.UDPAddr).Port)

	corefile := writeConfig(t, t.TempDir(), port)
	err = Run(context.Background(), Options{Corefile: corefile, Log: io.Discard})
	if err == nil {
		t.Fatal("Run succeeded on a busy port")
	}
	if want := "127.0.0.1:" + port; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name %s", err, want)
	}
}
