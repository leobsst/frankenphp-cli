package cli

import (
	"context"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/dnsclients"
	"github.com/leobsst/frankenphp-cli/internal/core/dnswatcher"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

func newStatusCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: i18n.T("Show the current server status."),
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
	c.Println("\n", c.Bold(i18n.T("=== FrankenPHP Server Status ===")), "\n")
	if running {
		c.Println(i18n.Tf("Status: %s", c.Color(ui.Green, i18n.T("running"))))
	} else {
		c.Println(i18n.Tf("Status: %s", c.Color(ui.Red, i18n.T("stopped"))))
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
		c.Println(i18n.Tf("LAN DNS server: %s (domains resolve to %s)", listen, recordsIP))
		addrs, err := dns.ServerAddresses(e)
		if err != nil {
			return err
		}
		for _, line := range dns.DescribeServerAddresses(addrs, recordsIP) {
			c.Println(line)
		}
		if running {
			if a.dnsProcessState(dir) != "running" {
				c.Println(i18n.Tf("  Server: %s (run `restart --dns`)", c.Color(ui.Red, i18n.T("not running"))))
			} else {
				c.Println(i18n.Tf("  Server: %s", c.Color(ui.Green, i18n.T("running"))))
				if dnswatcher.FollowsIP(e) {
					c.Println(i18n.Tf("  Follows LAN IP changes: %s", c.Color(ui.Green, i18n.T("yes"))))
				}
				clients, err := s.DNSClients()
				if err != nil {
					return err
				}
				connected := 0
				for _, cl := range clients {
					if dnsclients.Status(cl, time.Now()) == dnsclients.Connected {
						connected++
					}
				}
				c.Println(i18n.Tf("  Devices connected: %d (frankenmanager dns-clients)", connected))
			}
		}
	}

	if len(domains) > 0 {
		c.Println("\n", c.Bold(i18n.T("Domains:")))
		t := ui.Table{Columns: []ui.Column{{Header: i18n.T("Domain"), Width: 30}, {Header: i18n.T("PHP Version"), Width: 12}}}
		for _, d := range domains {
			t.Rows = append(t.Rows, []string{d.Name, "PHP " + d.PHPVersion})
		}
		c.PrintTable(t)
	}

	c.Println("\n", c.Bold(i18n.T("Containers:")))
	t := ui.Table{Columns: []ui.Column{{Header: i18n.T("Container"), Width: 25}, {Header: i18n.T("Status"), Width: 12}, {Header: i18n.T("Health"), Width: 12}}}
	row := func(name string) {
		st := statuses.Get(name)
		state := c.Color(ui.Red, st.State)
		switch st.State {
		case "running":
			state = c.Color(ui.Green, st.State)
		case docker.NotFound.State:
			// Docker's own states are shown as they are; this one is ours
			state = c.Dim(i18n.T("not found"))
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
