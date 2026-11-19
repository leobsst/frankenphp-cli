// Package client talks to the daemon on its Unix socket: the CLI side of
// the API. Ensure starts the daemon when it does not answer, and replaces a
// daemon of another version.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/daemon"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

// LogFile is the output of the daemon started by Ensure, in the data
// directory.
const LogFile = "daemon.log"

// Client calls the API of the daemon of a data directory.
type Client struct {
	socket string
	http   *http.Client
}

// New returns the client of the daemon of dir.
func New(dir string) *Client {
	socket := filepath.Join(dir, daemon.SocketFile)
	return &Client{socket: socket, http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}}
}

// Error is an error answered by the daemon.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// Do sends a request to path (e.g. "/api/status") with body as JSON (nil:
// none) and decodes the answer into out (nil: ignored).
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	// The host is ignored: the transport dials the socket
	req, err := http.NewRequestWithContext(ctx, method, "http://daemon"+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		var apiErr struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &apiErr) != nil || apiErr.Error == "" {
			apiErr.Error = res.Status
		}
		return &Error{Status: res.StatusCode, Message: apiErr.Error}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// Version returns the version of the running daemon, an error when it
// does not answer.
func (c *Client) Version(ctx context.Context) (string, error) {
	var h struct {
		Version string `json:"version"`
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return h.Version, c.Do(ctx, http.MethodGet, "/api/health", nil, &h)
}

// Running reports whether the daemon answers.
func (c *Client) Running(ctx context.Context) bool {
	_, err := c.Version(ctx)
	return err == nil
}

// ErrNotRunning is raised when stopping a daemon that does not answer.
var ErrNotRunning = i18n.NewError("The daemon is not running.")

// Stop stops the daemon and waits until it no longer answers.
func (c *Client) Stop(ctx context.Context) error {
	if !c.Running(ctx) {
		return ErrNotRunning
	}
	if err := c.Do(ctx, http.MethodPost, "/api/daemon/stop", nil, nil); err != nil {
		return err
	}
	return c.waitFor(ctx, false, 10*time.Second, nil)
}

// waitFor waits until the daemon answers (up) or no longer does, failing
// after timeout or as soon as exited is closed.
func (c *Client) waitFor(ctx context.Context, up bool, timeout time.Duration, exited <-chan struct{}) error {
	deadline := time.Now().Add(timeout)
	for {
		if c.Running(ctx) == up {
			return nil
		}
		select {
		case <-exited:
			return errExited
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return errTimeout
		}
	}
}

var (
	errExited  = errors.New("exited")
	errTimeout = errors.New("timeout")
)

// Starter starts the daemon of dir, detached from the caller, and returns
// a channel closed when it exits.
type Starter func(dir string) (exited <-chan struct{}, err error)

// StartDetached returns the Starter running `exe daemon run`, its output
// in daemon.log.
func StartDetached(exe string) Starter {
	return func(dir string) (<-chan struct{}, error) {
		// A file, not a pipe: the caller exits while the daemon writes
		log, err := os.Create(filepath.Join(dir, LogFile))
		if err != nil {
			return nil, err
		}
		// --no-update-check is a root option: before the subcommand
		cmd := exec.Command(exe, "--no-update-check", "daemon", "run")
		cmd.Stdout, cmd.Stderr = log, log
		platform.Detach(cmd)
		err = cmd.Start()
		_ = log.Close()
		if err != nil {
			return nil, err
		}
		exited := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(exited)
		}()
		return exited, nil
	}
}

// Started tells what Ensure did.
type Started int

// What Ensure did.
const (
	AlreadyRunning Started = iota // the daemon of this version answered
	StartedNew                    // none answered: started
	Replaced                      // one of another version was stopped, then started
)

// Ensure returns the client of a daemon of version, starting one with start
// when none answers, or replacing one of another version: a CLI always
// talks to the daemon of its own release.
func Ensure(ctx context.Context, dir, version string, start Starter) (*Client, Started, error) {
	c := New(dir)
	what := StartedNew
	if v, err := c.Version(ctx); err == nil {
		if v == version {
			return c, AlreadyRunning, nil
		}
		if err := c.Stop(ctx); err != nil {
			return nil, 0, i18n.Errorf("Could not stop the daemon of version %s: %v", v, err)
		}
		what = Replaced
	}
	exited, err := start(dir)
	if err != nil {
		return nil, 0, i18n.Errorf("The daemon failed to start: %v", err)
	}
	log := filepath.Join(dir, LogFile)
	switch err := c.waitFor(ctx, true, 15*time.Second, exited); {
	case errors.Is(err, errExited):
		return nil, 0, i18n.Errorf("The daemon stopped at once: see %s", log)
	case err != nil:
		return nil, 0, i18n.Errorf("The daemon does not answer (%v): see %s", err, log)
	}
	if v, err := c.Version(ctx); err == nil && v != version {
		return nil, 0, i18n.Errorf("The daemon started is version %s, not %s.", v, version)
	}
	return c, what, nil
}
