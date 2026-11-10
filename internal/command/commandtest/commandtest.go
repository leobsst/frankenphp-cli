// Package commandtest fakes command.Runner: tests record the commands
// instead of running them.
package commandtest

import (
	"context"

	"github.com/leobsst/frankenphp-cli/internal/command"
)

// Runner records every command and answers with Handle (exit 0 when nil).
// Paths lists the programs LookPath finds, by name.
type Runner struct {
	Paths  map[string]string
	Handle func(c command.Cmd) (command.Result, error)
	Calls  []command.Cmd
}

// Run implements command.Runner.
func (r *Runner) Run(_ context.Context, c command.Cmd) (command.Result, error) {
	r.Calls = append(r.Calls, c)
	if r.Handle == nil {
		return command.Result{}, nil
	}
	return r.Handle(c)
}

// LookPath implements command.Runner.
func (r *Runner) LookPath(name string) string { return r.Paths[name] }

// Lines returns the command lines run, in order.
func (r *Runner) Lines() []string {
	lines := make([]string, len(r.Calls))
	for i, c := range r.Calls {
		lines[i] = c.String()
	}
	return lines
}

// Log records log lines as "<level>: <msg>".
type Log struct{ Lines []string }

func (l *Log) Info(msg string)    { l.Lines = append(l.Lines, "info: "+msg) }
func (l *Log) Success(msg string) { l.Lines = append(l.Lines, "success: "+msg) }
func (l *Log) Warning(msg string) { l.Lines = append(l.Lines, "warning: "+msg) }
func (l *Log) Error(msg string)   { l.Lines = append(l.Lines, "error: "+msg) }
