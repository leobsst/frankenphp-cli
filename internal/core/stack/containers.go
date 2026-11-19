package stack

import (
	"context"
	"slices"

	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// Container is a container of the stack and its state.
type Container struct {
	Name   string `json:"name"`
	State  string `json:"state"`  // "running", "exited", "not found"...
	Health string `json:"health"` // "healthy", "unhealthy", "starting" or "N/A"
}

// containerNames returns the containers the configuration runs.
func (s *Service) containerNames() ([]string, error) {
	active, err := s.Store.ActivePHPVersions()
	if err != nil {
		return nil, err
	}
	e := s.Env
	return docker.AllContainers(docker.Containers{
		PHPVersions: active, DBEngines: e.DBEngines(), Production: e.IsProduction(),
		Redis: e.RedisEnabled(), Websocket: soketi.Enabled(e), S3: s3.Enabled(e),
	}), nil
}

// Containers returns the containers of the stack with their state.
func (s *Service) Containers(ctx context.Context) ([]Container, error) {
	names, err := s.containerNames()
	if err != nil {
		return nil, err
	}
	statuses := s.Docker.ContainerStatuses(ctx)
	out := make([]Container, 0, len(names))
	for _, n := range names {
		st := statuses.Get(n)
		out = append(out, Container{Name: n, State: st.State, Health: st.Health})
	}
	return out, nil
}

// Stats returns the CPU and memory usage of the running containers of the
// stack.
func (s *Service) Stats(ctx context.Context) ([]docker.Stat, error) {
	names, err := s.containerNames()
	if err != nil {
		return nil, err
	}
	return s.Docker.Stats(ctx, names...)
}

// ContainerActions are what ContainerAction does to a container.
var ContainerActions = []string{"start", "stop", "restart"}

// ErrUnknownContainer is raised for a container that is not the stack's.
type ErrUnknownContainer struct{ Name string }

func (e *ErrUnknownContainer) Error() string {
	return i18n.Tf("Container '%s' is not part of the stack.", e.Name)
}

// CheckContainer returns an *ErrUnknownContainer when name is not a
// container of the stack.
func (s *Service) CheckContainer(name string) error {
	names, err := s.containerNames()
	if err != nil {
		return err
	}
	if !slices.Contains(names, name) {
		return &ErrUnknownContainer{Name: name}
	}
	return nil
}

// ContainerAction starts, stops (keeping it) or restarts one container of
// the stack.
func (s *Service) ContainerAction(ctx context.Context, name, action string) error {
	if err := s.CheckContainer(name); err != nil {
		return err
	}
	var ok bool
	var failed error
	switch action {
	case "start":
		s.Log.Step(i18n.Tf("Starting %s...", name))
		ok, failed = s.Docker.StartContainer(ctx, name), i18n.Errorf("Could not start %s.", name)
	case "stop":
		s.Log.Step(i18n.Tf("Stopping %s...", name))
		ok, failed = s.Docker.HaltContainer(ctx, name), i18n.Errorf("Could not stop %s.", name)
	case "restart":
		s.Log.Step(i18n.Tf("Restarting %s...", name))
		ok, failed = s.Docker.RestartContainer(ctx, name), i18n.Errorf("Could not restart %s.", name)
	default:
		return i18n.Errorf("Unknown action '%s'. Use %s.", action, "start, stop, restart")
	}
	if !ok {
		return failed
	}
	return nil
}
