package cli

import (
	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

func newListCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: i18n.T("List all registered domains with their PHP versions."),
		Long:  i18n.T("List all registered domains with their PHP versions.\n\nWorks whether the server is running or stopped.\n\nExamples:\n    frankenmanager list"),
		Args:  noArgs,
		RunE:  func(*cobra.Command, []string) error { return a.list() },
	}
}

// list prints the domains, aliases, proxies and static hosts.
func (a *app) list() (err error) {
	dir, err := a.projectDir()
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
	aliases, err := s.Aliases()
	if err != nil {
		return err
	}
	proxies, err := s.Proxies()
	if err != nil {
		return err
	}
	statics, err := s.StaticHosts()
	if err != nil {
		return err
	}

	c := a.con
	if len(domains) == 0 && len(aliases) == 0 && len(proxies) == 0 && len(statics) == 0 {
		c.Println("\n" + i18n.T("No domains registered."))
		c.Println(i18n.T("Use 'frankenmanager start \"myapp.test\"' to add domains.") + "\n")
		return nil
	}

	section := func(t ui.Table, summary string) {
		c.Println()
		c.PrintTable(t)
		c.Println("\n", c.Dim(summary))
	}
	if len(domains) > 0 {
		t := ui.Table{Title: i18n.T("Registered Domains"), Columns: []ui.Column{
			{Header: i18n.T("Domain"), Color: ui.Cyan}, {Header: "PHP", Color: ui.Green},
		}}
		for _, d := range domains {
			t.Rows = append(t.Rows, []string{d.Name, d.PHPVersion})
		}
		section(t, i18n.Tf("%d domain(s) registered", len(domains)))
	}
	if len(aliases) > 0 {
		t := ui.Table{Title: i18n.T("Alternate Hosts (Aliases)"), Columns: []ui.Column{
			{Header: i18n.T("Alias"), Color: ui.Cyan}, {Header: i18n.T("Target Domain"), Color: ui.Magenta},
		}}
		for _, al := range aliases {
			t.Rows = append(t.Rows, []string{al.Alias, al.Target})
		}
		section(t, i18n.Tf("%d alternate host(s) registered", len(aliases)))
	}
	if len(proxies) > 0 {
		t := ui.Table{Title: i18n.T("Proxy Hosts"), Columns: []ui.Column{
			{Header: i18n.T("Domain"), Color: ui.Cyan}, {Header: i18n.T("Upstream"), Color: ui.Magenta},
		}}
		for _, p := range proxies {
			t.Rows = append(t.Rows, []string{p.Domain, p.Target})
		}
		section(t, i18n.Tf("%d proxy host(s) registered", len(proxies)))
	}
	if len(statics) > 0 {
		t := ui.Table{Title: i18n.T("Static Hosts"), Columns: []ui.Column{
			{Header: i18n.T("Domain"), Color: ui.Cyan}, {Header: i18n.T("Root"), Color: ui.Magenta}, {Header: "SPA", Faint: true},
		}}
		for _, st := range statics {
			t.Rows = append(t.Rows, []string{st.Domain, st.Root, yesNo(st.SPA)})
		}
		section(t, i18n.Tf("%d static host(s) registered", len(statics)))
	}
	c.Println()
	return nil
}
