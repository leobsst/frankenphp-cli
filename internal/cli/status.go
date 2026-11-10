package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/dnswatcher"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

func newStatusCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current server status.",
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return a.status(cmd.Context()) },
	}
}

// status prints the server state, the LAN DNS server, the domains and the
// containers.
func (a *app) status(ctx context.Context) (err error) {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	s, err := a.openStore(dir)
	if err != nil {
		return err
	}
	defer closeStore(s, &err)
	domains, err := s.Domains()
	if err != nil {
		return err
	}
	active, err := s.ActivePHPVersions()
	if err != nil {
		return err
	}

	// One docker call for every container
	statuses := a.docker(dir).ContainerStatuses(ctx)
	running := docker.IsRunning(statuses, active)

	c := a.con
	c.Println("\n", c.Bold("=== FrankenPHP Server Status ==="), "\n")
	if running {
		c.Println("Status: ", c.Color(ui.Green, "running"))
	} else {
		c.Println("Status: ", c.Color(ui.Red, "stopped"))
	}

	listen, err := dns.ListenAddress(e)
	if err != nil {
		return err
	}
	if listen != "" {
		recordsIP, err := dns.ResolveIP(e)
		if err != nil {
			return err
		}
		c.Println("LAN DNS server: ", listen, " (domains resolve to ", recordsIP, ")")
		addrs, err := dns.ServerAddresses(e)
		if err != nil {
			return err
		}
		for _, line := range dns.DescribeServerAddresses(addrs, recordsIP) {
			c.Println(line)
		}
		if running && dnswatcher.Needed(e) {
			state := c.Color(ui.Red, "no") + " (run `restart --dns`)"
			if w, err := dnswatcher.New(dir); err == nil && w.RunningPID() != 0 {
				state = c.Color(ui.Green, "yes")
			}
			c.Println("  Follows LAN IP changes: ", state)
		}
	}

	if len(domains) > 0 {
		c.Println("\n", c.Bold("Domains:"))
		t := ui.Table{Columns: []ui.Column{{Header: "Domain", Width: 30}, {Header: "PHP Version", Width: 12}}}
		for _, d := range domains {
			t.Rows = append(t.Rows, []string{d.Name, "PHP " + d.PHPVersion})
		}
		c.PrintTable(t)
	}

	c.Println("\n", c.Bold("Containers:"))
	t := ui.Table{Columns: []ui.Column{{Header: "Container", Width: 25}, {Header: "Status", Width: 12}, {Header: "Health", Width: 12}}}
	row := func(name string) {
		st := statuses.Get(name)
		state := c.Color(ui.Red, st.State)
		switch st.State {
		case "running":
			state = c.Color(ui.Green, st.State)
		case docker.NotFound.State:
			state = c.Dim(st.State)
		}
		health := c.Color(ui.Yellow, st.Health)
		switch st.Health {
		case "healthy":
			health = c.Color(ui.Green, st.Health)
		case docker.NotFound.Health:
			health = c.Dim(st.Health)
		}
		t.Rows = append(t.Rows, []string{name, state, health})
	}
	for _, v := range active {
		row(phpversions.ContainerName(v))
	}
	for _, name := range docker.AllContainers(docker.Containers{
		DBEngines:  e.DBEngines(),
		Production: e.IsProduction(),
		DNS:        listen != "",
		Redis:      e.RedisEnabled(),
		Websocket:  e.SoketiEnabled(),
	}) {
		if !strings.HasPrefix(name, "frankenphp-") {
			row(name)
		}
	}
	c.PrintTable(t)
	c.Println()
	return nil
}
