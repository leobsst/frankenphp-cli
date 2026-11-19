package cli

import (
	"context"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/compose"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
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

func newServiceCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "service ACTION [NAME]",
		Short: i18n.T("Turn an optional service (dns, redis, s3, soketi) on or off, live."),
		Long: i18n.Tf(`Turn an optional service on or off, live.

When the server runs, the service is started or stopped at once, without
restarting anything else. Otherwise, the change applies on the next start.

ACTION: enable, disable, or status.
NAME:   %s (status: all when omitted).

Examples:
    frankenmanager service enable redis    # Start a Redis server
    frankenmanager service enable s3       # Start a Garage S3 server
    frankenmanager service disable dns     # Stop the LAN DNS server
    frankenmanager service status          # Show every optional service`, strings.Join(serviceNames, ", ")),
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{i18n.NewError("Missing argument 'ACTION'.")}
			}
			if len(args) == 1 && args[0] != "status" {
				return usageError{i18n.NewError("Missing argument 'NAME'.")}
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
					return usageError{i18n.Errorf("Unknown service '%s'. Use %s.", args[1], strings.Join(serviceNames, ", "))}
				}
			}
			switch args[0] {
			case "enable", "disable":
				return a.setService(cmd.Context(), name, args[0] == "enable")
			case "status":
				return a.serviceStatus(cmd.Context(), name)
			}
			return usageError{i18n.Errorf("Unknown action '%s'. Use enable, disable, or status.", args[0])}
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
		if on {
			a.log.Info(i18n.Tf("Service %s is already enabled.", name))
		} else {
			a.log.Info(i18n.Tf("Service %s is already disabled.", name))
		}
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
	t := ui.Table{Title: i18n.T("Optional services"), Columns: []ui.Column{
		{Header: i18n.T("Service"), Color: ui.Cyan}, {Header: i18n.T("Enabled")}, {Header: i18n.T("State")},
		{Header: i18n.T("Setting"), Faint: true},
	}}
	for _, name := range serviceNames {
		if only != "" && name != only {
			continue
		}
		key := env.Services[name]
		var state string
		if name == "dns" {
			state = a.dnsProcessState(dir)
		} else {
			// The front container: the S3 gateway is unhealthy without Garage
			state = statuses.Get(serviceContainers[name][0]).State
		}
		t.Rows = append(t.Rows, []string{name, yesNo(e.Get(key) == "true"), state, key})
	}
	a.con.PrintTable(t)
	return nil
}

// applyService brings a service in line with the configuration when the
// server runs: started or stopped, with what depends on it (certificate,
// hosts entry, reverse proxy route, DNS records and watcher).
func (a *app) applyService(ctx context.Context, dir string, e *env.Env, name string) (err error) {
	on := e.Get(env.Services[name]) == "true"

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
		if on {
			a.log.Success(i18n.Tf("Service %s enabled. It applies on the next start.", name))
		} else {
			a.log.Success(i18n.Tf("Service %s disabled. It applies on the next start.", name))
		}
		return nil
	}

	if name == "soketi" && on {
		if _, err := soketi.EnsureCredentials(e); err != nil {
			return err
		}
	}
	if name == "s3" && on {
		if _, err := s3.EnsureCredentials(e); err != nil {
			return err
		}
	}
	// The compose file and its variables, for the containerized services
	var vars map[string]string
	if name != "dns" {
		active, err := s.ActivePHPVersions()
		if err != nil {
			return err
		}
		if err := stack.GenerateCompose(dir, e, active); err != nil {
			return err
		}
		customPath, err := stack.ProjectPath(e, s)
		if err != nil {
			return err
		}
		if vars, err = e.ComposeEnvVars(dir, customPath, e.DBEngines()); err != nil {
			return err
		}
	}

	switch name {
	case "dns":
		if on {
			if err := dns.Sync(dir, e, s); err != nil {
				return err
			}
			stack.RemoveLegacyDNS(ctx, d)
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
	case "s3":
		h := a.deps.Hosts(a.log)
		if on {
			certs, err := a.ssl(e.CertsDir(dir))
			if err != nil {
				return err
			}
			if err := certs.GenerateAll(ctx, []string{s3.Domain}, false, e.IsProduction()); err != nil {
				return err
			}
			if _, err := h.AddEntry(ctx, "127.0.0.1", s3.Domain); err != nil {
				return err
			}
		} else {
			stack.RemoveS3Hosts(ctx, h, s)
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

	switch {
	case name == "dns" && on:
		// The background process, started or stopped below
		a.log.Info(i18n.Tf("Starting %s...", name))
	case name == "dns":
		a.log.Info(i18n.Tf("Stopping %s...", name))
	case on:
		a.log.Info(i18n.Tf("Starting %s...", name))
		if name == "s3" {
			if err := stack.BuildS3Gateway(ctx, d); err != nil {
				return err
			}
		}
		if err := d.ComposeUp(ctx, vars, composeServices[name]...); err != nil {
			return err
		}
	default:
		a.log.Info(i18n.Tf("Stopping %s...", name))
		for _, c := range serviceContainers[name] {
			d.StopContainer(ctx, c)
		}
	}
	if name == "soketi" || name == "s3" {
		a.restartProxy(ctx, d)
	}
	if name == "s3" && on {
		if err := a.syncS3(ctx, dir, e, s); err != nil {
			return err
		}
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

	if on {
		a.log.Success(i18n.Tf("Service %s enabled.", name))
	} else {
		a.log.Success(i18n.Tf("Service %s disabled.", name))
	}
	switch {
	case on && name == "dns":
		ip, err := dns.ResolveIP(e)
		if err != nil {
			return err
		}
		listen, err := dns.ListenAddress(e)
		if err != nil {
			return err
		}
		a.log.Info(i18n.Tf("LAN DNS server listening on %s (domains resolve to %s)", listen, ip))
	case on && name == "soketi":
		a.log.Info(i18n.Tf("Soketi WebSocket server: wss://%s (apps: 127.0.0.1:%s)", soketi.Domain, soketi.Port(e)))
	case on && name == "s3":
		a.logS3Endpoints(e)
	case on && name == "redis":
		a.log.Info(i18n.Tf("Redis listening on 127.0.0.1:%s", e.Get("REDIS_PORT")))
	}
	return nil
}

// dnsProcessState is the state of the LAN DNS server process: "running" or
// "stopped".
func (a *app) dnsProcessState(dir string) string {
	if w, err := a.deps.Watcher(dir); err == nil && w.RunningPID() != 0 {
		return "running"
	}
	return "stopped"
}
