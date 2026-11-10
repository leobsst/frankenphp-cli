// Package command runs external programs (sudo, mkcert, visudo, brew...)
// behind an interface, so that tests never run them.
package command

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

// Cmd is a program to run.
type Cmd struct {
	Name  string
	Args  []string
	Stdin []byte
	Env   map[string]string // added to the process environment
}

// String returns the command line, for logs and tests.
func (c Cmd) String() string { return strings.Join(append([]string{c.Name}, c.Args...), " ") }

// Result is the outcome of a command.
type Result struct {
	Stdout, Stderr []byte
	ExitCode       int // -1 when the command could not run at all
}

// OK reports whether the command exited with 0.
func (r Result) OK() bool { return r.ExitCode == 0 }

// Runner runs commands and finds programs.
type Runner interface {
	// Run runs c. err is set only when it could not run (missing program,
	// timeout): a non-zero exit is reported in ExitCode.
	Run(ctx context.Context, c Cmd) (Result, error)
	// LookPath returns the path of a program, "" when not found.
	LookPath(name string) string
}

// Exec runs the real programs.
type Exec struct{}

// Run implements Runner.
func (Exec) Run(ctx context.Context, c Cmd) (Result, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	if len(c.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range c.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	if c.Stdin != nil {
		cmd.Stdin = bytes.NewReader(c.Stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr) && ctx.Err() == nil:
		res.ExitCode = exitErr.ExitCode()
		err = nil
	default:
		res.ExitCode = -1
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}
	return res, err
}

// LookPath implements Runner.
func (Exec) LookPath(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}
