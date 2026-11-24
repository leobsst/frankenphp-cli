// Package dashboard names where the dashboard answers on this machine:
// https://frankenmanager.test and https://<CLI_ALIAS>.test, served by the
// reverse proxy, and the daemon behind them on 127.0.0.1.
//
// These names are for this machine only: they get a hosts entry and a
// certificate, never a record of the LAN DNS server. The page they serve
// signs in by calling the daemon at https://127.0.0.1:DASHBOARD_HTTPS_PORT,
// which only a browser of this machine reaches; the other devices go
// through the remote access, with the password.
package dashboard

import (
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

// Domain is the name of the dashboard on this machine.
const Domain = "frankenmanager.test"

// aliasLabel is a CLI alias usable as a DNS label.
var aliasLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Domains returns frankenmanager.test, then <CLI_ALIAS>.test when the alias
// is set and makes a valid name.
func Domains(e *env.Env) []string {
	domains := []string{Domain}
	alias := strings.ToLower(strings.TrimSpace(e.Get(env.AliasKey)))
	if alias != "" && alias != "frankenmanager" && aliasLabel.MatchString(alias) {
		domains = append(domains, alias+".test")
	}
	return domains
}

// Available returns the dashboard names not taken by a registered host: a
// project, proxy, static host or alias of that name keeps it, and its pages
// must never sign in the dashboard without the password.
func Available(e *env.Env, st *store.Store) ([]string, error) {
	taken, err := registered(st)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, d := range Domains(e) {
		if !slices.Contains(taken, d) {
			out = append(out, d)
		}
	}
	return out, nil
}

// registered lists the hosts of the store: domains, aliases, proxies and
// static hosts.
func registered(st *store.Store) ([]string, error) {
	names, err := st.DomainNames()
	if err != nil {
		return nil, err
	}
	aliases, err := st.Aliases()
	if err != nil {
		return nil, err
	}
	for _, a := range aliases {
		names = append(names, a.Alias)
	}
	proxies, err := st.Proxies()
	if err != nil {
		return nil, err
	}
	for _, p := range proxies {
		names = append(names, p.Domain)
	}
	statics, err := st.StaticHostNames()
	if err != nil {
		return nil, err
	}
	return append(names, statics...), nil
}

// HTTPSPort is the port of the daemon's HTTPS listener on 127.0.0.1.
func HTTPSPort(e *env.Env) int {
	port, err := strconv.Atoi(e.Get(env.DashboardHTTPSPortKey))
	if err != nil || port < 1 || port > 65535 {
		port, _ = strconv.Atoi(env.DefaultDashboardHTTPSPort)
	}
	return port
}

// Origins are the pages allowed to sign in at the daemon's HTTPS listener
// without the password: the dashboard names (served by the reverse proxy on
// the HTTPS port of the stack) and the listener itself.
func Origins(domains []string, webHTTPSPort string, httpsPort int) []string {
	suffix := ""
	if webHTTPSPort != "" && webHTTPSPort != "443" {
		suffix = ":" + webHTTPSPort
	}
	var origins []string
	for _, d := range domains {
		origins = append(origins, "https://"+d+suffix)
	}
	local := strconv.Itoa(httpsPort)
	return append(origins, "https://"+net.JoinHostPort("127.0.0.1", local), "https://"+net.JoinHostPort("localhost", local))
}
