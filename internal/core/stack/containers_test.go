package stack

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

// The API controls the containers of the stack only, not any container of
// the machine.
func TestContainerActionIsLimitedToTheStack(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.AddDomains([]string{"app.test"}, "8.4"); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{}
	s := &Service{Env: newEnv("REDIS_ENABLED=true\n"), Store: st, Docker: &docker.Manager{Runner: r}, Log: events.NewLog(nil)}
	ctx := context.Background()

	var unknown *ErrUnknownContainer
	for _, name := range []string{"the-agency-agence-1", "franken_soketi"} {
		if err := s.ContainerAction(ctx, name, "stop"); !errors.As(err, &unknown) {
			t.Errorf("%s: err = %v, want ErrUnknownContainer", name, err)
		}
	}
	if len(r.args) != 0 {
		t.Fatalf("docker called: %v", r.args)
	}
	for _, name := range []string{"frankenphp-8.4", "franken_redis"} {
		if err := s.ContainerAction(ctx, name, "stop"); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	var calls []string
	for _, a := range r.args {
		calls = append(calls, strings.Join(a, " "))
	}
	// Stopped and kept, to start again
	if !slices.Equal(calls, []string{"stop frankenphp-8.4", "stop franken_redis"}) {
		t.Errorf("docker calls: %v", calls)
	}
}
