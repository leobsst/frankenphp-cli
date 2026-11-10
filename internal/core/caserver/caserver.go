// Package caserver shares the public mkcert root CA over the LAN, so that
// other devices can choose to trust the local domains.
//
// Only rootCA.pem is served, never the key. The server runs as a detached
// `_serve-ca-internal` process, found again through .ca_server.state
// ("<pid> <port>"), a format shared with earlier releases: each version can
// stop a server started by the other.
package caserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/platform"
)

const (
	// Command is the hidden command running the server.
	Command = "_serve-ca-internal"
	// DefaultPort is the default listening port.
	DefaultPort = 9080
	// StartupTimeout is how long StartSharing waits for the server.
	StartupTimeout = 3 * time.Second

	stateName = ".ca_server.state"
	logName   = ".ca_server.log"
)

// Error is a sharing failure.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func statePath(dataDir string) string { return filepath.Join(dataDir, stateName) }
func logPath(dataDir string) string   { return filepath.Join(dataDir, logName) }

// RunningState returns the pid and port of the running server, ok false
// when none runs. A stale or unreadable state file is removed.
func RunningState(dataDir string) (pid, port int, ok bool) {
	data, err := os.ReadFile(statePath(dataDir))
	if err != nil {
		return 0, 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) == 2 {
		pid, err1 := strconv.Atoi(fields[0])
		port, err2 := strconv.Atoi(fields[1])
		if err1 == nil && err2 == nil && platform.ProcessAlive(pid) {
			return pid, port, true
		}
	}
	_ = os.Remove(statePath(dataDir))
	return 0, 0, false
}

// StartSharing serves certFile on 0.0.0.0:port in a detached process,
// started from exe, and returns its pid once it answers with the
// certificate.
func StartSharing(dataDir, exe, certFile string, port int) (int, error) {
	if _, _, ok := RunningState(dataDir); ok {
		return 0, &Error{"Root CA sharing is already running. Run `frankenmanager trust-ca off` first."}
	}
	cert, err := os.ReadFile(certFile)
	if err != nil {
		return 0, &Error{fmt.Sprintf("Root CA not found at %s. "+
			"Run `sudo frankenmanager setup --install-mkcert` first.", certFile)}
	}

	// stderr goes to a file, not a pipe: this launcher exits right away and
	// the server would die on its first write to a closed pipe
	logFile, err := os.Create(logPath(dataDir))
	if err != nil {
		return 0, err
	}
	// --no-update-check is a root option: before the subcommand
	cmd := exec.Command(exe, "--no-update-check", Command, certFile, strconv.Itoa(port))
	cmd.Stderr = logFile
	platform.Detach(cmd)
	err = cmd.Start()
	_ = logFile.Close()
	if err != nil {
		return 0, &Error{fmt.Sprintf("Root CA server failed to start: %v", err)}
	}

	// closed once the process exits; cmd.ProcessState then holds its code
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	if err := waitUntilServing(cmd, exited, port, cert, logPath(dataDir)); err != nil {
		return 0, err
	}
	if err := os.WriteFile(statePath(dataDir), fmt.Appendf(nil, "%d %d", cmd.Process.Pid, port), 0o644); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

// waitUntilServing waits until the server answers with the certificate.
// A bare connect is not enough: another process may hold the port.
func waitUntilServing(cmd *exec.Cmd, exited <-chan struct{}, port int, cert []byte, log string) error {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	deadline := time.Now().Add(StartupTimeout)
	kill := func() {
		_ = cmd.Process.Kill()
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
		}
	}
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			return &Error{"Root CA server failed to start: " + lastLine(log, cmd)}
		default:
		}
		resp, err := client.Get(url)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if bytes.Equal(body, cert) {
			return nil
		}
		kill()
		return &Error{fmt.Sprintf("Port %d is already in use by another process (it responded, "+
			"but not with the expected root CA certificate). Pick a different port with --port.", port)}
	}
	kill()
	return &Error{fmt.Sprintf("Root CA server did not start listening on port %d within %.0fs. "+
		"It may be blocked by a firewall.", port, StartupTimeout.Seconds())}
}

// lastLine returns the last line of the server log, or its exit code.
func lastLine(log string, cmd *exec.Cmd) string {
	data, _ := os.ReadFile(log)
	text := strings.TrimSpace(strings.ToValidUTF8(string(data), "�"))
	if text == "" {
		return fmt.Sprintf("exited with code %d", cmd.ProcessState.ExitCode())
	}
	lines := strings.Split(text, "\n")
	return lines[len(lines)-1]
}

// StopSharing stops the running server, false when none runs.
func StopSharing(dataDir string) bool {
	pid, _, ok := RunningState(dataDir)
	if !ok {
		return false
	}
	_ = platform.Terminate(pid)
	_ = os.Remove(statePath(dataDir))
	_ = os.Remove(logPath(dataDir))
	return true
}

// Handler serves cert for any path, GET and HEAD; other methods get 501.
func Handler(cert []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Unsupported method", http.StatusNotImplemented)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "application/x-x509-ca-cert")
		h.Set("Content-Disposition", `attachment; filename="mkcert-rootCA.pem"`)
		h.Set("Content-Length", strconv.Itoa(len(cert)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(cert)
		}
	})
}

// Serve runs the server on 0.0.0.0:port until ctx ends (the hidden command).
func Serve(ctx context.Context, certFile string, port int) error {
	cert, err := os.ReadFile(certFile)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: Handler(cert), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
