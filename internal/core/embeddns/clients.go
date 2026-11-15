package embeddns

import (
	"context"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
)

// clients is the "clients" plugin: it reports the client IP of each query
// to Options.OnQuery, then hands the query on. Takes no argument.
type clients struct{ next plugin.Handler }

func registerClients() {
	plugin.Register("clients", func(c *caddy.Controller) error {
		c.Next() // the directive name
		if c.NextArg() {
			return plugin.Error("clients", c.ArgErr())
		}
		dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
			return clients{next: next}
		})
		return nil
	})
}

func (c clients) Name() string { return "clients" }

func (c clients) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	if report := onQuery.Load(); report != nil {
		state := request.Request{W: w, Req: r}
		(*report)(state.IP())
	}
	return plugin.NextOrFailure(c.Name(), c.next, ctx, w, r)
}
