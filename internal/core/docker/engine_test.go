package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A container without a TTY multiplexes its streams: the 8-byte headers
// are dropped, the frames kept in order.
func TestDemux(t *testing.T) {
	var in bytes.Buffer
	frame := func(stream byte, s string) {
		h := make([]byte, 8)
		h[0] = stream
		binary.BigEndian.PutUint32(h[4:], uint32(len(s)))
		in.Write(h)
		in.WriteString(s)
	}
	frame(1, "2026-11-20T10:00:00Z out\n")
	frame(2, "2026-11-20T10:00:01Z err\n")
	var out bytes.Buffer
	if err := demux(&in, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "2026-11-20T10:00:00Z out\n2026-11-20T10:00:01Z err\n" {
		t.Errorf("out = %q", out.String())
	}
}

// The usage is computed as docker stats does: CPU since the previous
// sample, memory without the page cache.
func TestStatOf(t *testing.T) {
	var s engineStats
	s.CPUStats.OnlineCPUs = 4
	s.MemoryStats.Usage = 60 << 20
	s.MemoryStats.Limit = 1 << 30
	s.MemoryStats.Stats = map[string]uint64{"inactive_file": 10 << 20}
	s.Networks = map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	}{"eth0": {RxBytes: 1500, TxBytes: 2_500_000}}
	st := statOf("frankenphp-8.4", s, cpuSample{total: 1000, system: 10000}, cpuSample{total: 1500, system: 20000})
	if st.CPUPercent != 20 { // 500/10000 x 4 CPUs
		t.Errorf("cpu = %v", st.CPUPercent)
	}
	if st.MemUsage != "50MiB / 1GiB" || st.MemPercent < 4.88 || st.MemPercent > 4.89 {
		t.Errorf("memory = %q, %v%%", st.MemUsage, st.MemPercent)
	}
	if st.NetIO != "1.5kB / 2.5MB" || st.BlockIO != "0B / 0B" {
		t.Errorf("io = %q, %q", st.NetIO, st.BlockIO)
	}
}

// With the Engine API, the container states are read without the docker
// command.
func TestEngineStatuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" || r.URL.Query().Get("all") != "true" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"Names":["/frankenphp-8.4"],"State":"running","Status":"Up 2 minutes (healthy)"},` +
			`{"Names":["/franken_redis"],"State":"exited","Status":"Exited (0) 1 hour ago"}]`))
	}))
	defer srv.Close()
	e := &Engine{HTTP: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", srv.Listener.Addr().String())
		},
	}}}
	m, r, _ := newManager(t, nil)
	m.Engine = e
	st := m.ContainerStatuses(context.Background())
	if st.Get("frankenphp-8.4") != (Status{"running", "healthy"}) || st.Get("franken_redis").State != "exited" {
		t.Errorf("statuses = %v", st)
	}
	if len(r.calls) != 0 {
		t.Errorf("docker called: %v", r.argLines())
	}
}
