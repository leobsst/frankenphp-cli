package stack

import (
	"context"
	"slices"

	"github.com/leobsst/frankenphp-cli/internal/core/compose"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// OptionalServices are the optional services (dns, redis, s3, soketi), in
// display order.
var OptionalServices = slices.Sorted(func(yield func(string) bool) {
	for name := range env.Services {
		if !yield(name) {
			return
		}
	}
})

// composeServices maps a containerized optional service to its compose
// services. The LAN DNS server is the background process, not a container.
var composeServices = map[string][]string{
	"redis": {"cache"}, "soketi": {"soketi"}, "s3": {s3.ComposeService, s3.GatewayService},
}

// serviceContainers maps a containerized optional service to its
// containers, in stop order.
var serviceContainers = map[string][]string{
	"redis": {compose.RedisContainer}, "soketi": {soketi.ContainerName}, "s3": {s3.GatewayContainer, s3.ContainerName},
}

// OptionalResult is what SetOptional or ApplyOptional did.
type OptionalResult struct {
	Name string
	On   bool
	// Changed is false when the service was already in that state
	Changed bool
	// Live reports that the server runs and the change was applied; when
	// false, it applies on the next start
	Live bool
}

// SetOptional turns an optional service on or off in the configuration,
// then applies it live.
func (s *Service) SetOptional(ctx context.Context, name string, on bool) (OptionalResult, error) {
	key := env.Services[name]
	value := "false"
	if on {
		value = "true"
	}
	if s.Env.Get(key) == value {
		return OptionalResult{Name: name, On: on}, nil
	}
	if err := s.Env.Set(key, value); err != nil {
		return OptionalResult{}, err
	}
	return s.ApplyOptional(ctx, name)
}

// ApplyOptional brings an optional service in line with the configuration
// when the server runs: started or stopped, with what depends on it
// (certificate, hosts entry, reverse proxy route, DNS records and watcher).
func (s *Service) ApplyOptional(ctx context.Context, name string) (OptionalResult, error) {
	e, st, d := s.Env, s.Store, s.Docker
	on := e.Get(env.Services[name]) == "true"
	res := OptionalResult{Name: name, On: on, Changed: true}

	running, err := IsRunning(ctx, d, st)
	if err != nil || !running {
		return res, err
	}
	res.Live = true

	if name == "soketi" && on {
		if _, err := soketi.EnsureCredentials(e); err != nil {
			return OptionalResult{}, err
		}
	}
	if name == "s3" && on {
		if _, err := s3.EnsureCredentials(e); err != nil {
			return OptionalResult{}, err
		}
	}
	// The compose file and its variables, for the containerized services
	var vars map[string]string
	if name != "dns" {
		active, err := st.ActivePHPVersions()
		if err != nil {
			return OptionalResult{}, err
		}
		if err := GenerateCompose(s.Dir, e, active); err != nil {
			return OptionalResult{}, err
		}
		customPath, err := ProjectPath(e, st)
		if err != nil {
			return OptionalResult{}, err
		}
		if vars, err = e.ComposeEnvVars(s.Dir, customPath, e.DBEngines()); err != nil {
			return OptionalResult{}, err
		}
	}

	// The certificate, hosts entry and route of the services with a domain
	domain := map[string]string{"soketi": soketi.Domain, "s3": s3.Domain}[name]
	switch {
	case name == "dns" && on:
		if err := dns.Sync(s.Dir, e, st); err != nil {
			return OptionalResult{}, err
		}
		RemoveLegacyDNS(ctx, d)
	case domain != "":
		h := s.Hosts
		switch {
		case on:
			certs, err := s.Certs(e.CertsDir(s.Dir))
			if err != nil {
				return OptionalResult{}, err
			}
			if err := certs.GenerateAll(ctx, []string{domain}, false, e.IsProduction()); err != nil {
				return OptionalResult{}, err
			}
			if _, err := h.AddEntry(ctx, "127.0.0.1", domain); err != nil {
				return OptionalResult{}, err
			}
		case name == "s3":
			RemoveS3Hosts(ctx, h, st)
		default:
			if _, err := h.RemoveEntry(ctx, "127.0.0.1", domain); err != nil {
				return OptionalResult{}, err
			}
		}
		if err := WriteMain(e.ResolveDir("CADDY_DIR", "./caddy", s.Dir), e, st); err != nil {
			return OptionalResult{}, err
		}
		if dns.Enabled(e) {
			if err := dns.Sync(s.Dir, e, st); err != nil {
				return OptionalResult{}, err
			}
		}
	}

	switch {
	case name == "dns" && on:
		// The background process, started or stopped below
		s.Log.Step(i18n.Tf("Starting %s...", name))
	case name == "dns":
		s.Log.Step(i18n.Tf("Stopping %s...", name))
	case on:
		s.Log.Step(i18n.Tf("Starting %s...", name))
		if name == "s3" {
			if err := BuildS3Gateway(ctx, d); err != nil {
				return OptionalResult{}, err
			}
		}
		if err := d.ComposeUp(ctx, vars, composeServices[name]...); err != nil {
			return OptionalResult{}, err
		}
	default:
		s.Log.Step(i18n.Tf("Stopping %s...", name))
		for _, c := range serviceContainers[name] {
			d.StopContainer(ctx, c)
		}
	}
	if domain != "" {
		RestartProxy(ctx, d, s.Log)
	}
	if name == "s3" && on {
		if err := s.SyncS3(ctx); err != nil {
			return OptionalResult{}, err
		}
	}
	if name == "dns" {
		w, err := s.Watcher(s.Dir)
		if err != nil {
			return OptionalResult{}, err
		}
		if _, err := w.Update(ctx, e); err != nil {
			return OptionalResult{}, err
		}
	}
	return res, nil
}

// OptionalStatus is the state of an optional service.
type OptionalStatus struct {
	Name    string
	Key     string // its setting
	Enabled bool
	// State is the state of its front container ("running", "exited",
	// "" when absent), or of the LAN DNS process ("running" or "stopped")
	State string
}

// OptionalStatuses returns the state of the optional services, only one
// when only is not "".
func (s *Service) OptionalStatuses(ctx context.Context, only string) []OptionalStatus {
	statuses := s.Docker.ContainerStatuses(ctx)
	var out []OptionalStatus
	for _, name := range OptionalServices {
		if only != "" && name != only {
			continue
		}
		key := env.Services[name]
		var state string
		if name == "dns" {
			state = s.DNSProcessState()
		} else {
			// The front container: the S3 gateway is unhealthy without Garage
			state = statuses.Get(serviceContainers[name][0]).State
		}
		out = append(out, OptionalStatus{Name: name, Key: key, Enabled: s.Env.Get(key) == "true", State: state})
	}
	return out
}

// DNSProcessState is the state of the LAN DNS server process: "running" or
// "stopped".
func (s *Service) DNSProcessState() string {
	if w, err := s.Watcher(s.Dir); err == nil && w.RunningPID() != 0 {
		return "running"
	}
	return "stopped"
}
