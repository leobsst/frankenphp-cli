package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/events"
)

// Status is the state of an operation.
type Status string

// The states of an operation, in order.
const (
	Queued    Status = "queued"
	Running   Status = "running"
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
)

// Event is an event of an operation, as the clients receive it.
type Event struct {
	Operation string    `json:"operation"`
	Kind      string    `json:"kind"`  // message, step, break or output
	Level     string    `json:"level"` // info, success, warning or error
	Message   string    `json:"message,omitempty"`
	Time      time.Time `json:"time"`
}

var kindNames = map[events.Kind]string{events.Message: "message", events.Step: "step", events.Break: "break",
	events.Output: "output"}

var levelNames = map[events.Level]string{
	events.Info: "info", events.Success: "success", events.Warning: "warning", events.Error: "error",
}

// Core returns e as the services reported it, for a client that prints it
// like they do.
func (e Event) Core() events.Event {
	out := events.Event{Message: e.Message}
	for k, name := range kindNames {
		if name == e.Kind {
			out.Kind = k
		}
	}
	for l, name := range levelNames {
		if name == e.Level {
			out.Level = l
		}
	}
	return out
}

// Operation is an action run by the queue.
type Operation struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // e.g. "hosts.add"
	Status Status `json:"status"`
	Error  string `json:"error,omitempty"`
	// Reported tells that the events already report the error (and its
	// cleanup): a client prints them, not the error again
	Reported bool       `json:"reported,omitempty"`
	Result   any        `json:"result,omitempty"`
	Events   []Event    `json:"events"`
	Created  time.Time  `json:"created"`
	Started  *time.Time `json:"started,omitempty"`
	Finished *time.Time `json:"finished,omitempty"`
}

// Job is the work of an operation: it reports through log and returns its
// result.
type Job func(ctx context.Context, log events.Log) (any, error)

// keptOperations is how many finished operations the queue remembers.
const keptOperations = 100

// Queue runs the operations one at a time: only one of them works on the
// shared state (SQLite, Caddyfiles, hosts file, Docker) at once.
type Queue struct {
	bus  *Bus
	jobs chan queued
	now  func() time.Time

	mu    sync.Mutex
	seq   int
	ops   map[string]*Operation
	order []string
}

type queued struct {
	op  *Operation
	job Job
}

// NewQueue returns a queue publishing the changes of its operations on bus.
func NewQueue(bus *Bus) *Queue {
	return &Queue{bus: bus, jobs: make(chan queued, 64), now: time.Now, ops: map[string]*Operation{}}
}

// Submit queues job as an operation of kind and returns it, queued.
func (q *Queue) Submit(kind string, job Job) Operation {
	q.mu.Lock()
	q.seq++
	op := &Operation{ID: fmt.Sprintf("op-%d", q.seq), Kind: kind, Status: Queued, Events: []Event{}, Created: q.now()}
	q.ops[op.ID] = op
	q.order = append(q.order, op.ID)
	q.forget()
	snapshot := q.snapshot(op)
	q.mu.Unlock()

	q.bus.Publish("operation", snapshot)
	q.jobs <- queued{op: op, job: job}
	return snapshot
}

// forget drops the oldest finished operations beyond keptOperations.
func (q *Queue) forget() {
	for len(q.order) > keptOperations {
		id := q.order[0]
		if s := q.ops[id].Status; s == Queued || s == Running {
			return
		}
		delete(q.ops, id)
		q.order = q.order[1:]
	}
}

// snapshot copies op, for the clients. The caller holds q.mu.
func (q *Queue) snapshot(op *Operation) Operation {
	c := *op
	c.Events = slices.Clone(op.Events)
	return c
}

// Get returns an operation.
func (q *Queue) Get(id string) (Operation, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	op, ok := q.ops[id]
	if !ok {
		return Operation{}, false
	}
	return q.snapshot(op), true
}

// List returns the operations, the latest first.
func (q *Queue) List() []Operation {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Operation, 0, len(q.order))
	for i := len(q.order) - 1; i >= 0; i-- {
		out = append(out, q.snapshot(q.ops[q.order[i]]))
	}
	return out
}

// Run runs the queued operations until ctx is done.
func (q *Queue) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-q.jobs:
			q.run(ctx, j)
		}
	}
}

func (q *Queue) run(ctx context.Context, j queued) {
	q.update(j.op, func(op *Operation) {
		t := q.now()
		op.Status, op.Started = Running, &t
	})
	log := events.NewLog(events.Func(func(e events.Event) {
		ev := Event{Operation: j.op.ID, Kind: kindNames[e.Kind], Level: levelNames[e.Level], Message: e.Message, Time: q.now()}
		q.mu.Lock()
		j.op.Events = append(j.op.Events, ev)
		q.mu.Unlock()
		q.bus.Publish("event", ev)
	}))
	result, err := q.call(ctx, j.job, log)
	q.update(j.op, func(op *Operation) {
		t := q.now()
		op.Finished = &t
		if err != nil {
			var reported *events.ReportedError
			op.Status, op.Error, op.Reported = Failed, err.Error(), errors.As(err, &reported)
			return
		}
		op.Status, op.Result = Succeeded, result
	})
}

// call runs job, a panic becoming the error of the operation rather than
// stopping the daemon.
func (q *Queue) call(ctx context.Context, job Job, log events.Log) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("internal error: %v", r)
		}
	}()
	return job(ctx, log)
}

// update changes op and publishes it.
func (q *Queue) update(op *Operation, change func(*Operation)) {
	q.mu.Lock()
	change(op)
	snapshot := q.snapshot(op)
	q.mu.Unlock()
	q.bus.Publish("operation", snapshot)
}
