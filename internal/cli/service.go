package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/compose"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

// serviceNames are the optional services, in display order.
var serviceNames = slices.Sorted(func(yield func(string) bool) {
	for name := range env.Services {
		if !yield(name) {
			return
		}
	}
})

// composeServices maps an optional service to its compose service.
var composeServices = map[string]string{"dns": "dns", "redis": "cache", "soketi": "soketi"}

// serviceContainers maps an optional service to its container.
var serviceContainers = map[string]string{
	"dns": compose.DNSContainer, "redis": compose.RedisContainer, "soketi": soketi.ContainerName,
}

// enabledLabel is how the messages show a service state.
func enabledLabel(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}

func newServiceCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "service ACTION [NAME]",
		Short: "Turn an optional service (dns, redis, soketi) on or off, live.",
		Long: `Turn an optional service on or off, live.

When the server runs, the service is started or stopped at once, without
restarting anything else. Otherwise, the change applies on the next start.

ACTION: enable, disable, or status.
NAME:   ` + strings.Join(serviceNames, ", ") + ` (status: all when omitted).

Examples:
    frankenmanager service enable redis    # Start a Redis server
    frankenmanager service disable dns     # Stop the LAN DNS server
    frankenmanager service status          # Show every optional service`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{errors.New("Missing argument 'ACTION'.")} //nolint:staticcheck // user-facing message
			}
			if len(args) == 1 && args[0] != "status" {
				return usageError{errors.New("Missing argument 'NAME'.")} //nolint:staticcheck // user-facing message
			}
			return maxArgs(2)(cmd, args)
		},
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			switch len(args) {
			case 0:
				return []string{"enable", "disable", "status"}, cobra.ShellCompDirectiveNoFileComp
			case 1:
				return serviceNames, cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 2 {
				name = strings.ToLower(args[1])
				if _, ok := env.Services[name]; !ok {
					return usageError{fmt.Errorf("Unknown service '%s'. Use %s.", args[1], strings.Join(serviceNames, ", "))} //nolint:staticcheck // user-facing message
				}
			}
			switch args[0] {
			case "enable", "disable":
				return a.setService(cmd.Context(), name, args[0] == "enable")
			case "status":
				return a.serviceStatus(cmd.Context(), name)
			}
			return usageError{fmt.Errorf("Unknown action '%s'. Use enable, disable, or status.", args[0])} //nolint:staticcheck // user-facing message
		},
	}
}

// setService turns a service on or off in the configuration, then applies
// it live.
func (a *app) setService(ctx context.Context, name string, on bool) error {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	key := env.Services[name]
	value := "false"
	if on {
		value = "true"
	}
	if e.Get(key) == value {
		a.log.Info(fmt.Sprintf("Service %s is already %s.", name, enabledLabel(on)))
		return nil
	}
	if err := e.Set(key, value); err != nil {
		return err
	}
	return a.applyService(ctx, dir, e, name)
}

// serviceStatus shows whether the services are enabled and running.
func (a *app) serviceStatus(ctx context.Context, only string) error {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	statuses := a.docker(dir).ContainerStatuses(ctx)
	t := ui.Table{Title: "Optional services", Columns: []ui.Column{
		{Header: "Service", Color: ui.Cyan}, {Header: "Enabled"}, {Header: "Container"}, {Header: "Setting", Faint: true},
	}}
	for _, name := range serviceNames {
		if only != "" && name != only {
			continue
		}
		key := env.Services[name]
		t.Rows = append(t.Rows, []string{name, yesNo(e.Get(key) == "true"), statuses.Get(serviceContainers[name]).State, key})
	}
	a.con.PrintTable(t)
	return nil
}

// applyService brings a service in line with the configuration when the
// server runs: started or stopped, with what depends on it (certificate,
// hosts entry, reverse proxy route, DNS records and watcher).
func (a *app) applyService(ctx context.Context, dir string, e *env.Env, name string) (err error) {
	on := e.Get(env.Services[name]) == "true"
	state := enabledLabel(on)

	d := a.docker(dir)
	s, err := a.openStore(dir)
	if err != nil {
		return err
	}
	defer closeStore(s, &err)
	running, err := a.isRunning(ctx, d, s)
	if err != nil {
		return err
	}
	if !running {
		a.log.Success(fmt.Sprintf("Service %s %s. It applies on the next start.", name, state))
		return nil
	}

	if name == "soketi" && on {
		if _, err := soketi.EnsureCredentials(e); err != nil {
			return err
		}
	}
	active, err := s.ActivePHPVersions()
	if err != nil {
		return err
	}
	listen, err := dns.ListenAddress(e)
	if err != nil {
		return err
	}
	if err := stack.GenerateCompose(dir, e, active, listen); err != nil {
		return err
	}
	customPath, err := stack.ProjectPath(e, s)
	if err != nil {
		return err
	}
	vars, err := e.ComposeEnvVars(dir, customPath, e.DBEngines())
	if err != nil {
		return err
	}

	switch name {
	case "dns":
		if on {
			if err := dns.Sync(dir, e, s); err != nil {
				return err
			}
		}
	case "soketi":
		h := a.deps.Hosts(a.log)
		if on {
			certs, err := a.ssl(e.CertsDir(dir))
			if err != nil {
				return err
			}
			if err := certs.GenerateAll(ctx, []string{soketi.Domain}, false, e.IsProduction()); err != nil {
				return err
			}
			if _, err := h.AddEntry(ctx, "127.0.0.1", soketi.Domain); err != nil {
				return err
			}
		} else if _, err := h.RemoveEntry(ctx, "127.0.0.1", soketi.Domain); err != nil {
			return err
		}
		r := &server{dir: dir, e: e, d: d, s: s, caddyDir: e.ResolveDir("CADDY_DIR", "./caddy", dir)}
		if err := r.writeMain(); err != nil {
			return err
		}
		if dns.Enabled(e) {
			if err := dns.Sync(dir, e, s); err != nil {
				return err
			}
		}
	}

	if on {
		a.log.Info(fmt.Sprintf("Starting %s...", name))
		if err := d.ComposeUp(ctx, vars, composeServices[name]); err != nil {
			return err
		}
	} else {
		a.log.Info(fmt.Sprintf("Stopping %s...", name))
		d.StopContainer(ctx, serviceContainers[name])
	}
	if name == "soketi" {
		a.restartProxy(ctx, d)
	}
	if name == "dns" {
		w, err := a.deps.Watcher(dir)
		if err != nil {
			return err
		}
		if _, err := w.Update(ctx, e); err != nil {
			return err
		}
	}

	a.log.Success(fmt.Sprintf("Service %s %s.", name, state))
	switch {
	case on && name == "dns":
		ip, err := dns.ResolveIP(e)
		if err != nil {
			return err
		}
		a.log.Info(fmt.Sprintf("LAN DNS server listening on %s (domains resolve to %s)", listen, ip))
	case on && name == "soketi":
		a.log.Info(fmt.Sprintf("Soketi WebSocket server: wss://%s (apps: 127.0.0.1:%s)", soketi.Domain, soketi.Port(e)))
	case on && name == "redis":
		a.log.Info("Redis listening on 127.0.0.1:" + e.Get("REDIS_PORT"))
	}
	return nil
}
