package cli

import (
	"context"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

// serviceNames are the optional services, in display order.
var serviceNames = stack.OptionalServices

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
	dir, _, err := a.prepare()
	if err != nil {
		return err
	}
	var res stack.OptionalResult
	if err := a.runOp(ctx, dir, http.MethodPut, "/api/services/"+name, map[string]any{"enabled": on}, &res); err != nil {
		return reported(err)
	}
	return a.reportServiceOf(dir, res)
}

// reportServiceOf tells what happened to an optional service of dir, with
// the configuration as the operation left it (credentials generated...).
func (a *app) reportServiceOf(dir string, res stack.OptionalResult) error {
	e, _, err := env.Open(dir)
	if err != nil {
		return err
	}
	return a.reportService(e, res)
}

// reportService tells what happened to an optional service.
func (a *app) reportService(e *env.Env, res stack.OptionalResult) error {
	name := res.Name
	switch {
	case !res.Changed && res.On:
		a.log.Info(i18n.Tf("Service %s is already enabled.", name))
		return nil
	case !res.Changed:
		a.log.Info(i18n.Tf("Service %s is already disabled.", name))
		return nil
	case !res.Live && res.On:
		a.log.Success(i18n.Tf("Service %s enabled. It applies on the next start.", name))
		return nil
	case !res.Live:
		a.log.Success(i18n.Tf("Service %s disabled. It applies on the next start.", name))
		return nil
	case res.On:
		a.log.Success(i18n.Tf("Service %s enabled.", name))
	default:
		a.log.Success(i18n.Tf("Service %s disabled.", name))
		return nil
	}
	switch name {
	case "dns":
		ip, err := dns.ResolveIP(e)
		if err != nil {
			return err
		}
		listen, err := dns.ListenAddress(e)
		if err != nil {
			return err
		}
		a.log.Info(i18n.Tf("LAN DNS server listening on %s (domains resolve to %s)", listen, ip))
	case "soketi":
		a.log.Info(i18n.Tf("Soketi WebSocket server: wss://%s (apps: 127.0.0.1:%s)", soketi.Domain, soketi.Port(e)))
	case "s3":
		a.logS3Endpoints(e)
	case "redis":
		a.log.Info(i18n.Tf("Redis listening on 127.0.0.1:%s", e.Get("REDIS_PORT")))
	}
	return nil
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
	t := ui.Table{Title: i18n.T("Optional services"), Columns: []ui.Column{
		{Header: i18n.T("Service"), Color: ui.Cyan}, {Header: i18n.T("Enabled")}, {Header: i18n.T("State")},
		{Header: i18n.T("Setting"), Faint: true},
	}}
	for _, st := range a.stackService(dir, e, a.docker(dir), nil).OptionalStatuses(ctx, only) {
		t.Rows = append(t.Rows, []string{st.Name, yesNo(st.Enabled), st.State, st.Key})
	}
	a.con.PrintTable(t)
	return nil
}

// dnsProcessState is the state of the LAN DNS server: "running" or
// "stopped".
func (a *app) dnsProcessState(ctx context.Context, dir string) string {
	return a.stackService(dir, nil, nil, nil).DNSProcessState(ctx)
}
