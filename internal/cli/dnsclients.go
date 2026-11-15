package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/dnsclients"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

func newDNSClientsCmd(a *app) *cobra.Command {
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "dns-clients",
		Short: "List the devices using the LAN DNS server.",
		Long: `List the devices using the LAN DNS server (DNS_SERVER=true).

A device is connected when it queried the server in the last 5 minutes.
Its name comes from your router (DHCP host name) and its MAC address from
this machine's ARP table, when they know them.

Examples:
    frankenmanager dns-clients          # Connected devices
    frankenmanager dns-clients --all    # Also the idle ones (last 30 days)
    frankenmanager dns-clients --json   # Machine-readable list`,
		Args: noArgs,
		RunE: func(*cobra.Command, []string) error { return a.dnsClients(all, asJSON, time.Now()) },
	}
	f := cmd.Flags()
	f.BoolVarP(&all, "all", "a", false, "Also list the idle devices")
	f.BoolVar(&asJSON, "json", false, "Print the devices as JSON")
	return cmd
}

// dnsClient is a device as --json prints it.
type dnsClient struct {
	store.DNSClient
	Status        string `json:"status"`
	RandomizedMAC bool   `json:"randomized_mac"`
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
		return errors.New("The LAN DNS server is disabled: run 'frankenmanager service enable dns'.") //nolint:staticcheck // user-facing message
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

	clients := []dnsClient{}
	connected := 0
	for _, c := range stored {
		status := dnsclients.Status(c, now)
		if status == dnsclients.Connected {
			connected++
		} else if !all {
			continue
		}
		clients = append(clients, dnsClient{DNSClient: c, Status: status, RandomizedMAC: dnsclients.RandomizedMAC(c.MAC)})
	}

	if asJSON {
		data, err := json.MarshalIndent(clients, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(a.out, string(data))
		return err
	}

	if a.dnsProcessState(dir) != "running" {
		a.log.Warning("The LAN DNS server is not running: run 'frankenmanager restart --dns'.")
	}
	c := a.con
	if len(stored) == 0 {
		c.Println("\nNo device has queried the LAN DNS server yet.")
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
		c.Println(fmt.Sprintf("\nNo device connected in the last %d minutes (%d known: --all).\n",
			int(dnsclients.ActiveWindow.Minutes()), len(stored)))
		return nil
	}

	t := ui.Table{Title: "LAN DNS clients", Columns: []ui.Column{
		{Header: "Status"}, {Header: "IP", Color: ui.Cyan}, {Header: "Name"}, {Header: "MAC", Faint: true},
		{Header: "Last seen"}, {Header: "Queries"},
	}}
	randomized := false
	for _, cl := range clients {
		status := c.Color(ui.Green, cl.Status)
		if cl.Status != dnsclients.Connected {
			status = c.Dim(cl.Status)
		}
		mac := orDash(cl.MAC)
		if cl.RandomizedMAC {
			mac += "*"
			randomized = true
		}
		t.Rows = append(t.Rows, []string{status, cl.IP, orDash(cl.Name), mac, ago(now.Sub(cl.LastSeen)),
			strconv.FormatInt(cl.Queries, 10)})
	}
	c.Println()
	c.PrintTable(t)
	footer := fmt.Sprintf("%d device(s) connected", connected)
	if !all && len(stored) > connected {
		footer += " (idle ones: --all)"
	}
	if randomized {
		footer += ". * private MAC address, changes per network"
	}
	c.Println("\n", c.Dim(footer), "\n")
	return nil
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
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
