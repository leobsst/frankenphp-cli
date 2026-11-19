package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
)

// StartContainer starts a stopped container, false when it failed.
func (m *Manager) StartContainer(ctx context.Context, name string) bool {
	return m.ok(ctx, "start", name)
}

// HaltContainer stops a container and keeps it, to start it again
// (StopContainer removes it).
func (m *Manager) HaltContainer(ctx context.Context, name string) bool {
	return m.ok(ctx, "stop", name)
}

// Stat is the resource usage of a running container.
type Stat struct {
	Name       string  `json:"name"`
	CPUPercent float64 `json:"cpu_percent"`
	MemPercent float64 `json:"mem_percent"`
	MemUsage   string  `json:"mem_usage"` // e.g. "52.3MiB / 7.66GiB"
	NetIO      string  `json:"net_io"`
	BlockIO    string  `json:"block_io"`
}

// Stats returns the usage of the running containers among names; the
// others are left out (docker stats fails on a stopped one).
func (m *Manager) Stats(ctx context.Context, names ...string) ([]Stat, error) {
	statuses := m.ContainerStatuses(ctx)
	var up []string
	for _, n := range names {
		if statuses.Running(n) {
			up = append(up, n)
		}
	}
	if len(up) == 0 {
		return []Stat{}, nil
	}
	res, err := m.run(ctx, append([]string{"stats", "--no-stream", "--format", "json"}, up...)...)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, errors.New(strings.TrimSpace(string(res.Stderr)))
	}
	return parseStats(res.Stdout), nil
}

// parseStats parses `docker stats --format json`: one object per line.
func parseStats(out []byte) []Stat {
	stats := []Stat{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		var line struct {
			Name, CPUPerc, MemPerc, MemUsage, NetIO, BlockIO string
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Name == "" {
			continue
		}
		stats = append(stats, Stat{Name: line.Name, CPUPercent: percent(line.CPUPerc), MemPercent: percent(line.MemPerc),
			MemUsage: line.MemUsage, NetIO: line.NetIO, BlockIO: line.BlockIO})
	}
	return stats
}

// percent parses "12.34%", 0 when it cannot.
func percent(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
	return f
}

// ErrNoStream is raised when the runner cannot stream (tests).
var ErrNoStream = errors.New("the docker runner cannot stream")

// FollowLogs streams the logs of a container to out, with their
// timestamps, from its last tail lines, until ctx ends.
func (m *Manager) FollowLogs(ctx context.Context, name string, tail int, out io.Writer) error {
	s, ok := m.Runner.(Streamer)
	if !ok {
		return ErrNoStream
	}
	_, err := s.Stream(ctx, m.Dir, out, "logs", "--follow", "--timestamps", "--tail", strconv.Itoa(tail), name)
	if ctx.Err() != nil {
		return nil
	}
	return err
}
