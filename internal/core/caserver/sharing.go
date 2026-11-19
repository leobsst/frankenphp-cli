package caserver

import (
	"context"
	"fmt"

	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// ErrNotSharing is raised when stopping a sharing that does not run.
var ErrNotSharing = i18n.NewError("Root CA sharing is not currently running.")

// Sharing shares the root CA of a data directory on the LAN, through the
// detached server.
type Sharing struct {
	Dir string // data directory
	// RootCA returns the root CA file to serve
	RootCA func(ctx context.Context) (string, error)
	// Start starts the detached server, Stop stops it (false when it did
	// not run), State reports it
	Start func(dataDir, certFile string, port int) error
	Stop  func(dataDir string) bool
	State func(dataDir string) (pid, port int, ok bool)
	// LANIP is the address shown to the other devices
	LANIP func() string
}

// URL is the address the other devices download the root CA from.
func (s Sharing) URL(port int) string { return fmt.Sprintf("http://%s:%d/", s.LANIP(), port) }

// On starts sharing on port and returns the URL to open.
func (s Sharing) On(ctx context.Context, port int) (string, error) {
	cert, err := s.RootCA(ctx)
	if err != nil {
		return "", err
	}
	if err := s.Start(s.Dir, cert, port); err != nil {
		return "", err
	}
	return s.URL(port), nil
}

// Off stops sharing, ErrNotSharing when it did not run.
func (s Sharing) Off() error {
	if !s.Stop(s.Dir) {
		return ErrNotSharing
	}
	return nil
}

// Status returns the URL of the running sharing, ok false when off.
func (s Sharing) Status() (url string, ok bool) {
	_, port, ok := s.State(s.Dir)
	if !ok {
		return "", false
	}
	return s.URL(port), true
}
