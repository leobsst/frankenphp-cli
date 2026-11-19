package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/dnsclients"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

func newDNSClientsCmd(a *app) *cobra.Command {
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "dns-clients",
		Short: i18n.T("List the devices using the LAN DNS server."),
		Long: i18n.T(`List the devices using the LAN DNS server (DNS_SERVER=true).

A device is connected when it queried the server in the last 5 minutes.
Its name comes from your router (DHCP host name) and its MAC address from
this machine's ARP table, when they know them.

Examples:
    frankenmanager dns-clients          # Connected devices
    frankenmanager dns-clients --all    # Also the idle ones (last 30 days)
    frankenmanager dns-clients --json   # Machine-readable list`),
		Args: noArgs,
		RunE: func(*cobra.Command, []string) error { return a.dnsClients(all, asJSON, time.Now()) },
	}
	f := cmd.Flags()
	f.BoolVarP(&all, "all", "a", false, i18n.T("Also list the idle devices"))
	f.BoolVar(&asJSON, "json", false, i18n.T("Print the devices as JSON"))
	return cmd
}

// dnsClients prints the devices of the LAN DNS server, connected ones only
// unless all.
func (a *app) dnsClients(all, asJSON bool, now time.Time) (err error) {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	if !dns.Enabled(e) {
		return i18n.NewError("The LAN DNS server is disabled: run 'frankenmanager service enable dns'.")
	}
	s, err := a.openStore(dir)
	if err != nil {
		return err
	}
	defer closeStore(s, &err)
	stored, err := s.DNSClients()
	if err != nil {
		return err
	}

	clients, connected := dnsclients.Devices(stored, now, all)

	if asJSON {
		data, err := json.MarshalIndent(clients, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(a.out, string(data))
		return err
	}

	if a.dnsProcessState(context.Background(), dir) != "running" {
		a.log.Warning(i18n.T("The LAN DNS server is not running: run 'frankenmanager restart --dns'."))
	}
	c := a.con
	if len(stored) == 0 {
		c.Println(i18n.T("\nNo device has queried the LAN DNS server yet."))
		recordsIP, err := dns.ResolveIP(e)
		if err != nil {
			return err
		}
		addrs, err := dns.ServerAddresses(e)
		if err != nil {
			return err
		}
		for _, line := range dns.DescribeServerAddresses(addrs, recordsIP) {
			c.Println(line)
		}
		c.Println()
		return nil
	}
	if len(clients) == 0 {
		c.Println(i18n.Tf("\nNo device connected in the last %d minutes (%d known: --all).\n",
			int(dnsclients.ActiveWindow.Minutes()), len(stored)))
		return nil
	}

	t := ui.Table{Title: i18n.T("LAN DNS clients"), Columns: []ui.Column{
		{Header: i18n.T("Status")}, {Header: "IP", Color: ui.Cyan}, {Header: i18n.T("Name")}, {Header: "MAC", Faint: true},
		{Header: i18n.T("Last seen")}, {Header: i18n.T("Queries")},
	}}
	randomized := false
	for _, cl := range clients {
		status := c.Color(ui.Green, i18n.T("connected"))
		if cl.Status != dnsclients.Connected {
			status = c.Dim(i18n.T("idle"))
		}
		mac := orDash(cl.MAC)
		if cl.RandomizedMAC {
			mac += "*"
			randomized = true
		}
		t.Rows = append(t.Rows, []string{status, cl.IP, orDash(clientName(cl.Name)), mac, ago(now.Sub(cl.LastSeen)),
			strconv.FormatInt(cl.Queries, 10)})
	}
	c.Println()
	c.PrintTable(t)
	var footer string
	switch idle := !all && len(stored) > connected; {
	case idle && randomized:
		footer = i18n.Tf("%d device(s) connected (idle ones: --all). * private MAC address, changes per network", connected)
	case idle:
		footer = i18n.Tf("%d device(s) connected (idle ones: --all)", connected)
	case randomized:
		footer = i18n.Tf("%d device(s) connected. * private MAC address, changes per network", connected)
	default:
		footer = i18n.Tf("%d device(s) connected", connected)
	}
	c.Println("\n", c.Dim(footer), "\n")
	return nil
}

// clientName is the name of a device as shown, the host's own translated.
func clientName(name string) string {
	if name == dnsclients.ThisMachine {
		return i18n.T("this machine")
	}
	return name
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// ago is a short relative duration: "now", "12s ago", "3m ago", "2h ago",
// "4d ago".
func ago(d time.Duration) string {
	switch {
	case d < 5*time.Second:
		return i18n.T("now")
	case d < time.Minute:
		return i18n.Tf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return i18n.Tf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return i18n.Tf("%dh ago", int(d.Hours()))
	}
	return i18n.Tf("%dd ago", int(d.Hours()/24))
}
