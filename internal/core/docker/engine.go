package docker

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Engine talks to the Docker Engine API on its Unix socket: the cheapest
// way to read the stats, the logs and the events of the containers, with no
// docker process per call. The docker command stays the fallback (Windows
// named pipe, no socket found).
type Engine struct {
	HTTP *http.Client
}

// NewEngine returns the client of the Engine API on the Unix socket path.
func NewEngine(path string) *Engine {
	return &Engine{HTTP: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
	}}}
}

// get sends GET path to the engine; the caller closes the body.
func (e *Engine) get(ctx context.Context, path string, query url.Values) (*http.Response, error) {
	u := "http://docker" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := e.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		defer func() { _ = res.Body.Close() }()
		var body struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&body)
		return nil, fmt.Errorf("docker engine: %s (HTTP %d)", body.Message, res.StatusCode)
	}
	return res, nil
}

// ping reports whether the engine answers.
func (e *Engine) ping(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	res, err := e.get(ctx, "/_ping", nil)
	if err != nil {
		return false
	}
	_ = res.Body.Close()
	return true
}

// engineCache keeps the engine found, for the life of the process (the
// daemon): looking for it costs a docker call.
var engineCache struct {
	mu      sync.Mutex
	engine  *Engine
	checked time.Time
}

// engineRetry is how long a failed lookup is trusted before trying again.
const engineRetry = time.Minute

// engine returns the Engine API of the manager, nil when it must use the
// docker command: tests (their runner is not the real one), Windows named
// pipe, no socket.
func (m *Manager) engine(ctx context.Context) *Engine {
	if m.Engine != nil {
		return m.Engine
	}
	if _, real := m.Runner.(ExecRunner); !real {
		return nil
	}
	engineCache.mu.Lock()
	defer engineCache.mu.Unlock()
	if engineCache.engine != nil || time.Since(engineCache.checked) < engineRetry {
		return engineCache.engine
	}
	engineCache.checked = time.Now()
	if path := m.socketPath(ctx); path != "" {
		if e := NewEngine(path); e.ping(ctx) {
			engineCache.engine = e
		}
	}
	return engineCache.engine
}

// forgetEngine drops the engine after a failure: Docker may have moved.
func forgetEngine() {
	engineCache.mu.Lock()
	engineCache.engine, engineCache.checked = nil, time.Time{}
	engineCache.mu.Unlock()
}

// socketPath returns the Unix socket of the active Docker endpoint, as the
// docker command would use it: DOCKER_HOST, the active context, then the
// default socket. "" for another transport.
func (m *Manager) socketPath(ctx context.Context) string {
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		if res, err := m.run(ctx, "context", "inspect", "--format", "{{.Endpoints.docker.Host}}"); err == nil && res.ExitCode == 0 {
			host = strings.TrimSpace(string(res.Stdout))
		}
	}
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	path, ok := strings.CutPrefix(host, "unix://")
	if !ok {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// statuses lists every container, stopped ones included (docker ps --all).
func (e *Engine) statuses(ctx context.Context) (Statuses, error) {
	res, err := e.get(ctx, "/containers/json", url.Values{"all": {"true"}})
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	var list []struct {
		Names  []string
		State  string
		Status string
	}
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return nil, err
	}
	statuses := Statuses{}
	for _, c := range list {
		st := Status{State: c.State, Health: "N/A"}
		if st.State == "" {
			st.State = "unknown"
		}
		if m := healthPattern.FindStringSubmatch(c.Status); m != nil {
			st.Health = m[1]
		}
		for _, n := range c.Names {
			statuses[strings.TrimPrefix(n, "/")] = st
		}
	}
	return statuses, nil
}

// --- Stats -------------------------------------------------------------------

// engineStats is the part of GET /containers/{id}/stats the dashboard uses.
type engineStats struct {
	Read     time.Time `json:"read"`
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs  uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IOServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
}

// cpuSample is the CPU usage of a container at a time.
type cpuSample struct {
	total, system uint64
}

// lastCPU keeps the previous CPU sample of each container: the next
// snapshot computes the usage since then, with a one-shot call that does not
// wait for Docker to take a second sample.
var lastCPU = struct {
	mu      sync.Mutex
	samples map[string]cpuSample
}{samples: map[string]cpuSample{}}

// engineStat returns the usage of a running container.
func (e *Engine) engineStat(ctx context.Context, name string) (Stat, error) {
	lastCPU.mu.Lock()
	prev, known := lastCPU.samples[name]
	lastCPU.mu.Unlock()
	q := url.Values{"stream": {"false"}}
	if known {
		q.Set("one-shot", "true")
	}
	res, err := e.get(ctx, "/containers/"+url.PathEscape(name)+"/stats", q)
	if err != nil {
		return Stat{}, err
	}
	defer func() { _ = res.Body.Close() }()
	var s struct {
		engineStats
		PreCPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemUsage uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
	}
	if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
		return Stat{}, err
	}
	now := cpuSample{total: s.CPUStats.CPUUsage.TotalUsage, system: s.CPUStats.SystemUsage}
	if !known {
		prev = cpuSample{total: s.PreCPUStats.CPUUsage.TotalUsage, system: s.PreCPUStats.SystemUsage}
	}
	lastCPU.mu.Lock()
	lastCPU.samples[name] = now
	lastCPU.mu.Unlock()
	return statOf(name, s.engineStats, prev, now), nil
}

// statOf computes a Stat the way `docker stats` does.
func statOf(name string, s engineStats, prev, now cpuSample) Stat {
	st := Stat{Name: name}
	cpus := float64(s.CPUStats.OnlineCPUs)
	if cpus == 0 {
		cpus = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	if now.total > prev.total && now.system > prev.system {
		st.CPUPercent = float64(now.total-prev.total) / float64(now.system-prev.system) * cpus * 100
	}
	// The page cache does not count, as in docker stats (cgroup v2 then v1)
	used := s.MemoryStats.Usage
	for _, k := range []string{"inactive_file", "total_inactive_file"} {
		if v, ok := s.MemoryStats.Stats[k]; ok && v < used {
			used -= v
			break
		}
	}
	if s.MemoryStats.Limit > 0 {
		st.MemPercent = float64(used) / float64(s.MemoryStats.Limit) * 100
	}
	st.MemUsage = binarySize(used) + " / " + binarySize(s.MemoryStats.Limit)
	var rx, tx, read, write uint64
	for _, n := range s.Networks {
		rx, tx = rx+n.RxBytes, tx+n.TxBytes
	}
	for _, b := range s.BlkioStats.IOServiceBytesRecursive {
		switch strings.ToLower(b.Op) {
		case "read":
			read += b.Value
		case "write":
			write += b.Value
		}
	}
	st.NetIO = decimalSize(rx) + " / " + decimalSize(tx)
	st.BlockIO = decimalSize(read) + " / " + decimalSize(write)
	return st
}

// binarySize writes n in binary units (52.3MiB), as docker stats does for
// the memory.
func binarySize(n uint64) string {
	return humanSize(float64(n), 1024, []string{"B", "KiB", "MiB", "GiB", "TiB"})
}

// decimalSize writes n in decimal units (1.2kB), as docker stats does for
// the I/O.
func decimalSize(n uint64) string {
	return humanSize(float64(n), 1000, []string{"B", "kB", "MB", "GB", "TB"})
}

func humanSize(n, base float64, units []string) string {
	i := 0
	for n >= base && i < len(units)-1 {
		n /= base
		i++
	}
	return fmt.Sprintf("%.4g%s", n, units[i])
}

// engineStats returns the usage of the running containers among up, in
// parallel.
func (e *Engine) stats(ctx context.Context, up []string) ([]Stat, error) {
	out := make([]Stat, len(up))
	errs := make([]error, len(up))
	var wg sync.WaitGroup
	for i, n := range up {
		wg.Go(func() { out[i], errs[i] = e.engineStat(ctx, n) })
	}
	wg.Wait()
	stats := []Stat{}
	for i, st := range out {
		if errs[i] == nil {
			stats = append(stats, st)
		}
	}
	if len(stats) == 0 && len(up) > 0 {
		return nil, errors.Join(errs...)
	}
	return stats, nil
}

// --- Logs --------------------------------------------------------------------

// followLogs streams the logs of a container to out, with their timestamps,
// from its last tail lines, until ctx ends.
func (e *Engine) followLogs(ctx context.Context, name string, tail int, out io.Writer) error {
	q := url.Values{"follow": {"true"}, "stdout": {"true"}, "stderr": {"true"},
		"timestamps": {"true"}, "tail": {strconv.Itoa(tail)}}
	res, err := e.get(ctx, "/containers/"+url.PathEscape(name)+"/logs", q)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if strings.Contains(res.Header.Get("Content-Type"), "multiplexed") {
		err = demux(res.Body, out)
	} else {
		_, err = io.Copy(out, res.Body)
	}
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// demux copies the payloads of a multiplexed stream (a container without a
// TTY: an 8-byte header, stream and size, before each frame) to out.
func demux(r io.Reader, out io.Writer) error {
	br := bufio.NewReader(r)
	var header [8]byte
	for {
		if _, err := io.ReadFull(br, header[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if _, err := io.CopyN(out, br, int64(binary.BigEndian.Uint32(header[4:]))); err != nil {
			return err
		}
	}
}

// --- Events ------------------------------------------------------------------

// watchEvents calls changed for each container event, until ctx ends or
// the stream breaks.
func (e *Engine) watchEvents(ctx context.Context, changed func()) error {
	res, err := e.get(ctx, "/events", url.Values{"filters": {`{"type":["container"]}`}})
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	return eachLine(ctx, res.Body, changed)
}

// eachLine calls fn for each line of r, until ctx ends or r does.
func eachLine(ctx context.Context, r io.Reader, fn func()) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		fn()
	}
	if ctx.Err() != nil {
		return nil
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}
