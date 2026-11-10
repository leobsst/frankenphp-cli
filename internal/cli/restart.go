package cli

import (
	"context"
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/compose"
	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/password"
	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

// restartTargets are the --<target> options of restart, in the order they
// are processed.
var restartTargets = []string{"caddy", "database", "cache", "phpmyadmin", "dns", "soketi"}

// restartContainers maps the static targets to their container.
var restartContainers = map[string]string{
	"cache":      compose.RedisContainer,
	"phpmyadmin": compose.PHPMyAdminContainer,
	"dns":        compose.DNSContainer,
	"soketi":     soketi.ContainerName,
}

func newRestartCmd(a *app) *cobra.Command {
	var forceSSL, build bool
	selected := map[string]*bool{}
	for _, t := range restartTargets {
		selected[t] = new(bool)
	}
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the FrankenPHP development server or specific containers.",
		Long: `Restart the FrankenPHP development server or specific containers.

Examples:
    frankenmanager restart                    # Restart all containers
    frankenmanager restart --caddy            # Restart only web server/Caddy
    frankenmanager restart --database         # Restart only database
    frankenmanager restart --cache --pma      # Restart Redis and phpMyAdmin
    frankenmanager restart --dns              # Restart the LAN DNS server
    frankenmanager restart --soketi           # Restart the Soketi WebSocket server
    frankenmanager restart --build            # Rebuild PHP images, then restart`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var targets []string
			for _, t := range restartTargets {
				if *selected[t] {
					targets = append(targets, t)
				}
			}
			return a.restart(cmd.Context(), forceSSL, targets, build)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&forceSSL, "force-ssl", false, "Force SSL certificate regeneration")
	f.BoolVar(selected["caddy"], "caddy", false, "Restart only the web server / Caddy container")
	// Options with two long names: the second one is a hidden alias
	alias := func(name, short, help string) {
		f.BoolVar(selected[name], name, false, help)
		f.BoolVar(selected[name], short, false, help)
		_ = f.MarkHidden(short)
	}
	alias("database", "db", "Restart only the database container (alias: --db)")
	f.BoolVar(selected["cache"], "cache", false, "Restart only the Redis cache container (REDIS_ENABLED=true)")
	alias("phpmyadmin", "pma", "Restart only the phpMyAdmin container (alias: --pma)")
	f.BoolVar(selected["dns"], "dns", false, "Restart only the LAN DNS server container")
	alias("soketi", "ws", "Restart only the Soketi WebSocket server (SOKETI_ENABLED=true) (alias: --ws)")
	f.BoolVar(&build, "build", false, "Rebuild the PHP images and recreate their containers")
	return cmd
}

// restart restarts the stack or some of its containers. targets is empty to
// restart everything.
func (a *app) restart(ctx context.Context, forceSSL bool, targets []string, build bool) (err error) {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	d := a.docker(dir)
	s, err := a.openStore(dir)
	if err != nil {
		return err
	}
	defer closeStore(s, &err)
	if running, err := a.isRunning(ctx, d, s); err != nil {
		return err
	} else if !running {
		return errors.New("The server is not running.") //nolint:staticcheck // user-facing message
	}
	active, err := s.ActivePHPVersions()
	if err != nil {
		return err
	}

	all := len(targets) == 0
	has := func(t string) bool {
		for _, x := range targets {
			if x == t {
				return true
			}
		}
		return false
	}
	if all {
		a.log.Info("Restarting web server...")
	} else {
		a.log.Info("Restarting " + strings.Join(targets, ", ") + "...")
	}

	if all || has("caddy") {
		domains, err := s.DomainNames()
		if err != nil {
			return err
		}
		domains = append(domains, soketi.Domains(e)...)
		a.con.Println()
		a.log.Info("Generating SSL certificates...")
		// <CADDY_DIR>/certs, like the other commands, not CERTS_DIR
		certs, err := a.ssl(e.CertsDir(dir))
		if err != nil {
			return err
		}
		if err := certs.GenerateAll(ctx, domains, forceSSL, e.IsProduction()); err != nil {
			return err
		}
	}

	engines := e.DBEngines()
	redis, websocket, dnsOn := e.RedisEnabled(), soketi.Enabled(e), dns.Enabled(e)

	if build && len(active) > 0 {
		customPath, err := stack.ProjectPath(e, s)
		if err != nil {
			return err
		}
		if customPath == "" {
			return errors.New("No project path known: set DEFAULT_PROJECT_PATH in .env " + //nolint:staticcheck // user-facing message
				"or use 'stop' then 'start --build --path <path>'.")
		}
		if err := stack.BuildImages(ctx, d, e, a.log, customPath, active, true); err != nil {
			return err
		}
		listen, err := dns.ListenAddress(e)
		if err != nil {
			return err
		}
		// Recreate the PHP containers on the new images
		if err := stack.ComposeUp(ctx, d, e, active, listen, customPath); err != nil {
			return err
		}
	} else if build {
		a.log.Info("No PHP domain registered: no image to build.")
	}

	if all || has("dns") {
		if dnsOn {
			if err := a.refreshDNS(ctx, dir, d, e, s, active); err != nil {
				return err
			}
		}
		// Picks up DNS_SERVER / DNS_IP changes made in .env since the last start
		w, err := a.deps.Watcher(dir)
		if err != nil {
			return err
		}
		if _, err := w.Update(ctx, e); err != nil {
			return err
		}
	}

	if all {
		d.RestartAll(ctx, docker.Containers{
			PHPVersions: active, DBEngines: engines, Production: e.IsProduction(),
			DNS: dnsOn, Redis: redis, Websocket: websocket,
		})
	} else {
		for _, t := range targets {
			switch {
			case t == "caddy":
				// Every FrankenPHP container, then the reverse proxy
				for _, v := range active {
					d.RestartContainer(ctx, phpversions.ContainerName(v))
				}
				d.RestartContainer(ctx, compose.ReverseProxyContainer)
			case t == "database":
				for _, engine := range engines {
					if name, ok := compose.DBContainers[engine]; ok {
						d.RestartContainer(ctx, name)
					}
				}
			case t == "cache" && !redis:
				a.log.Warning("Redis is disabled: set REDIS_ENABLED=true in .env to use it.")
			case t == "soketi" && !websocket:
				a.log.Warning("Soketi is disabled: set SOKETI_ENABLED=true in .env to use it.")
			case t == "dns" && !dnsOn:
				a.log.Warning("The LAN DNS server is disabled: set DNS_SERVER=true in .env.")
			default:
				d.RestartContainer(ctx, restartContainers[t])
			}
		}
	}

	if all || has("database") {
		passwords, err := dbPasswords(e, engines)
		if err != nil {
			return err
		}
		if err := password.New(dir, d, a.log).SyncAll(ctx, engines, passwords); err != nil {
			return err
		}
	}

	a.con.Println()
	if all {
		a.log.Success("Web server restarted!")
	} else {
		a.log.Success("Container(s) restarted: " + strings.Join(targets, ", "))
	}
	return nil
}

// refreshDNS rewrites the records with the current LAN IP, then recreates
// the DNS container when its published address changed since the last
// start (_refresh_dns).
func (a *app) refreshDNS(ctx context.Context, dir string, d *docker.Manager, e *env.Env,
	s *store.Store, active []string) error {
	if err := dns.Sync(dir, e, s); err != nil {
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
	if err := d.ComposeUp(ctx, vars, "dns"); err != nil {
		return err
	}
	ip, err := dns.ResolveIP(e)
	if err != nil {
		return err
	}
	a.log.Info("LAN DNS server: domains resolve to " + ip)
	return nil
}
