package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/events"
)

// Delays of the container watch.
var (
	// watchDebounce groups the events of one change (a restart sends
	// several) into one state.
	watchDebounce = 300 * time.Millisecond
	// watchRetry is the wait after the event stream broke (Docker stopped).
	watchRetry = 5 * time.Second
)

// DockerState is the "docker" message of the event stream: whether the
// Docker engine answers.
type DockerState struct {
	Reachable bool `json:"reachable"`
}

// watchContainers publishes the state of the containers of the stack
// ("containers" on the event stream) when Docker reports a change, and
// whether Docker answers ("docker") when that changes, until ctx ends. One
// event stream, shared by every client.
func (s *Server) watchContainers(ctx context.Context) {
	var last []byte
	var reachable *bool // last published, nil before the first check
	checkDocker := func(ws *Workspace) bool {
		up := ws.Stack.Docker.Reachable(ctx)
		if reachable == nil || *reachable != up {
			reachable = &up
			s.Bus.Publish("docker", DockerState{Reachable: up})
		}
		return up
	}
	publish := func() {
		ws, err := s.Backend(events.NewLog(nil))
		if err != nil {
			return
		}
		defer func() { _ = ws.Close() }()
		list, err := ws.Stack.Containers(ctx)
		if err != nil {
			return
		}
		data, err := json.Marshal(list)
		if err != nil || bytes.Equal(data, last) {
			return
		}
		last = data
		s.Bus.Publish("containers", list)
	}
	// follow publishes the containers on each Docker event, until the
	// events stream breaks
	follow := func(ws *Workspace) {
		changed := make(chan struct{}, 1)
		watchCtx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			timer := time.NewTimer(0)
			defer timer.Stop()
			for {
				select {
				case <-watchCtx.Done():
					return
				case <-changed:
					timer.Reset(watchDebounce)
				case <-timer.C:
					publish()
				}
			}
		}()
		_ = ws.Stack.Docker.WatchEvents(watchCtx, func() {
			select {
			case changed <- struct{}{}:
			default:
			}
		})
		stop()
		<-done
	}
	for ctx.Err() == nil {
		if ws, err := s.Backend(events.NewLog(nil)); err == nil {
			// While Docker does not answer, its events stream would break at
			// once: it is checked again after watchRetry
			if checkDocker(ws) {
				follow(ws)
				// The stream broke: Docker may have stopped, its containers
				// are then reported "not found"
				if ctx.Err() == nil && !checkDocker(ws) {
					publish()
				}
			}
			_ = ws.Close()
		}
		select {
		case <-ctx.Done():
		case <-time.After(watchRetry):
		}
	}
}
