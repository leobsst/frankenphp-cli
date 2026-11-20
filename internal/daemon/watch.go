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

// watchContainers publishes the state of the containers of the stack
// ("containers" on the event stream) when Docker reports a change, until
// ctx ends. One event stream, shared by every client.
func (s *Server) watchContainers(ctx context.Context) {
	var last []byte
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
	for ctx.Err() == nil {
		ws, err := s.Backend(events.NewLog(nil))
		if err == nil {
			changed := make(chan struct{}, 1)
			watchCtx, stop := context.WithCancel(ctx)
			go func() {
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
			_ = ws.Close()
		}
		select {
		case <-ctx.Done():
		case <-time.After(watchRetry):
		}
	}
}
