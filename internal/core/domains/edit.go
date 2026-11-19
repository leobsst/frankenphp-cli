package domains

import (
	"context"
	"slices"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/core/dns"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
	"github.com/leobsst/frankenphp-cli/internal/core/resources"
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/core/soketi"
	"github.com/leobsst/frankenphp-cli/internal/core/stack"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// without returns the versions of before missing from after, sorted.
func without(before, after []string) []string {
	var out []string
	for _, v := range before {
		if !slices.Contains(after, v) {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}

// stopVersions stops and removes the containers of versions left without
// domains, then regenerates the compose file without them.
func (s *Service) stopVersions(ctx context.Context, orphaned, remaining []string) error {
	for _, v := range orphaned {
		name := phpversions.ContainerName(v)
		s.Log.Step(i18n.Tf("Stopping %s (no more domains)...", name))
		s.Docker.StopContainer(ctx, name)
	}
	if len(orphaned) == 0 {
		return nil
	}
	return stack.GenerateCompose(s.Dir, s.Env, remaining)
}

// RemoveResult is what Remove did.
type RemoveResult struct {
	Domains []store.Domain `json:"domains"` // the PHP domains, with their version
	// Aliases are the alternate hosts requested and those of the removed
	// domains and proxy hosts
	Aliases          []string `json:"aliases"`
	Proxies          []string `json:"proxies"`
	Statics          []string `json:"statics"`
	ArchiveDir       string   `json:"archive_dir"`        // where the site files of the domains went
	StaticArchiveDir string   `json:"static_archive_dir"` // where those of the static hosts went
}

// Empty reports whether nothing was removed.
func (r RemoveResult) Empty() bool {
	return len(r.Domains)+len(r.Aliases)+len(r.Proxies)+len(r.Statics) == 0
}

// Remove removes domains, aliases, proxy and static hosts, archiving the
// site files of the domains and static hosts.
func (s *Service) Remove(ctx context.Context, names []string) (RemoveResult, error) {
	existing, err := s.Store.Domains()
	if err != nil {
		return RemoveResult{}, err
	}
	versionOf := map[string]string{}
	for _, d := range existing {
		versionOf[d.Name] = d.PHPVersion
	}
	aliasList, err := s.Store.Aliases()
	if err != nil {
		return RemoveResult{}, err
	}
	proxyList, err := s.Store.Proxies()
	if err != nil {
		return RemoveResult{}, err
	}
	isAlias := map[string]bool{}
	for _, al := range aliasList {
		isAlias[al.Alias] = true
	}
	isProxy := map[string]bool{}
	for _, p := range proxyList {
		isProxy[p.Domain] = true
	}
	staticList, err := s.Store.StaticHostNames()
	if err != nil {
		return RemoveResult{}, err
	}

	var domains, aliases, proxies, statics []string
	pick := func(list *[]string, name string) {
		if slices.Contains(*list, name) {
			s.Log.Info(i18n.Tf("Domain '%s' is duplicated in input, skipping.", name))
		} else {
			*list = append(*list, name)
		}
	}
	for _, name := range names {
		switch {
		case versionOf[name] != "":
			pick(&domains, name)
		case isAlias[name]:
			pick(&aliases, name)
		case isProxy[name]:
			pick(&proxies, name)
		case slices.Contains(staticList, name):
			pick(&statics, name)
		default:
			s.Log.Warning(i18n.Tf("Domain '%s' is not configured, skipping.", name))
		}
	}
	if len(domains)+len(aliases)+len(proxies)+len(statics) == 0 {
		s.Log.Info(i18n.T("No domains to remove."))
		return RemoveResult{}, nil
	}

	removeEntry := func(name string) {
		if _, err := s.Hosts.RemoveEntry(ctx, "127.0.0.1", name); err != nil {
			s.Log.Warning(i18n.Tf("Failed to remove hosts entry for %s: %v", name, err))
		}
	}
	g := s.generator(s.Log)
	before, err := s.Store.ActivePHPVersions()
	if err != nil {
		return RemoveResult{}, err
	}
	var touched []string // versions losing a domain
	for _, d := range domains {
		if !slices.Contains(touched, versionOf[d]) {
			touched = append(touched, versionOf[d])
		}
	}

	var removedAliases []string
	err = func() error {
		s.Log.Info(i18n.Tf("Removing %d domain(s), %d alternate host(s), %d proxy host(s), and %d static host(s)...",
			len(domains), len(aliases), len(proxies), len(statics)))
		s.Log.Step(i18n.T("Removing hosts entries..."))
		for _, d := range domains {
			removeEntry(d)
		}
		if len(domains) > 0 {
			s.Log.Step(i18n.T("Archiving Caddyfiles..."))
			if err := g.Archive(domains); err != nil {
				return err
			}
		}
		if err := s.Store.RemoveDomains(domains); err != nil {
			return err
		}
		// The requested aliases, then those of the removed domains and
		// proxy hosts
		if err := s.Store.RemoveAliases(aliases); err != nil {
			return err
		}
		cascaded, err := s.Store.RemoveAliasesForTargets(append(slices.Clone(domains), proxies...))
		if err != nil {
			return err
		}
		removedAliases = append(slices.Clone(aliases), cascaded...)
		if len(cascaded) > 0 {
			s.Log.Info(i18n.Tf("Also removing alternate host(s) pointing to removed host(s): %s", strings.Join(cascaded, ", ")))
		}
		for _, al := range removedAliases {
			removeEntry(al)
		}
		if err := s.Store.RemoveProxies(proxies); err != nil {
			return err
		}
		for _, p := range proxies {
			removeEntry(p)
		}
		if len(statics) > 0 {
			s.Log.Step(i18n.T("Archiving Caddyfiles..."))
			if err := g.ArchiveStatic(statics); err != nil {
				return err
			}
		}
		if err := s.Store.RemoveStaticHosts(statics); err != nil {
			return err
		}
		for _, st := range statics {
			removeEntry(st)
		}

		after, err := s.Store.ActivePHPVersions()
		if err != nil {
			return err
		}
		orphaned := without(before, after)
		for _, v := range orphaned {
			name := phpversions.ContainerName(v)
			s.Log.Step(i18n.Tf("Stopping %s (no more domains)...", name))
			s.Docker.StopContainer(ctx, name)
		}
		slices.Sort(touched)
		for _, v := range touched {
			if slices.Contains(after, v) {
				s.restartVersion(ctx, v)
			}
		}
		if len(orphaned) > 0 {
			if err := stack.GenerateCompose(s.Dir, s.Env, after); err != nil {
				return err
			}
		}
		if err := s.writeMain(); err != nil {
			return err
		}
		if err := dns.Sync(s.Dir, s.Env, s.Store); err != nil {
			return err
		}
		s.restartProxy(ctx)
		return nil
	}()
	if err != nil {
		s.Log.Error(i18n.Tf("An error occurred: %v", err))
		return RemoveResult{}, &events.ReportedError{Err: err}
	}

	res := RemoveResult{Aliases: removedAliases, Proxies: proxies, Statics: statics,
		ArchiveDir: g.ArchiveDir(), StaticArchiveDir: g.StaticArchiveDir()}
	for _, d := range domains {
		res.Domains = append(res.Domains, store.Domain{Name: d, PHPVersion: versionOf[d]})
	}
	return res, nil
}

// Restore brings archived domains back with php, a supported version (see
// stack.ResolvePHP), not their original one. The result is empty when nothing
// was restored.
func (s *Service) Restore(ctx context.Context, names []string, php string, forceSSL bool) (PHPResult, error) {
	if err := phpversions.Validate(php); err != nil {
		return PHPResult{}, err
	}
	existing, err := s.Store.DomainNames()
	if err != nil {
		return PHPResult{}, err
	}
	statics, err := s.Store.StaticHostNames()
	if err != nil {
		return PHPResult{}, err
	}
	existing = append(existing, statics...)
	// The names are compared without their TLD: app.test is active when
	// app.dev is registered
	short := func(d string) string {
		if i := strings.LastIndex(d, "."); i >= 0 {
			return d[:i]
		}
		return d
	}
	var restore []string
	for _, name := range names {
		active := slices.ContainsFunc(existing, func(d string) bool { return short(d) == short(name) })
		switch {
		case active:
			s.Log.Info(i18n.Tf("Domain '%s' is already active, skipping.", name))
		case slices.Contains(restore, name):
			s.Log.Info(i18n.Tf("Domain '%s' is duplicated in input, skipping.", name))
		default:
			restore = append(restore, name)
		}
	}
	if len(restore) == 0 {
		s.Log.Info(i18n.T("No domains to restore."))
		return PHPResult{}, nil
	}

	active, err := s.Store.ActivePHPVersions()
	if err != nil {
		return PHPResult{}, err
	}
	if err := resources.New(s.Dir, s.Log).EnsurePHPVersionConfig(php); err != nil {
		return PHPResult{}, err
	}
	certs, err := s.Certs()
	if err != nil {
		return PHPResult{}, err
	}
	c := &cleanup{s: s}
	var restored []string
	err = func() error {
		s.Log.Info(i18n.Tf("Restoring %d domain(s) with PHP %s...", len(restore), php))
		s.Log.Step(i18n.T("Restoring Caddyfiles..."))
		var err error
		if restored, err = s.generator(s.Log).RestoreToVersion(restore, php); err != nil {
			return err
		}
		if len(restored) == 0 {
			return nil
		}
		s.Log.Step(i18n.T("Generating SSL certificates..."))
		if err := certs.GenerateAll(ctx, restored, forceSSL, s.Env.IsProduction()); err != nil {
			return err
		}
		s.Log.Step(i18n.T("Adding hosts entries..."))
		for _, domain := range restored {
			if err := c.add(ctx, domain); err != nil {
				return err
			}
		}
		if err := s.Store.AddDomains(restored, php); err != nil {
			return err
		}
		if err := dns.Sync(s.Dir, s.Env, s.Store); err != nil {
			return err
		}
		if !slices.Contains(active, php) {
			if err := s.startVersion(ctx, php, active); err != nil {
				return err
			}
		} else {
			s.restartVersion(ctx, php)
		}
		if err := s.writeMain(); err != nil {
			return err
		}
		s.restartProxy(ctx)
		return nil
	}()
	if err != nil {
		// The domains leave the database and their site files go back to
		// the archive, ready for another restore
		return PHPResult{}, c.fail(ctx, err, i18n.T("The host(s) were not restored."), func() {
			_ = s.Store.RemoveDomains(restored)
			_ = s.generator(events.NewLog(nil)).Archive(restored)
		})
	}
	if len(restored) == 0 {
		s.Log.Info(i18n.T("No Caddyfiles were restored."))
		return PHPResult{}, nil
	}
	return PHPResult{Domains: restored, PHP: php}, nil
}

// SwitchResult is what SwitchPHP did.
type SwitchResult struct {
	Domain   string `json:"domain"`
	From     string `json:"from"`
	To       string `json:"to"`
	Switched bool   `json:"switched"` // false when the domain already used To
}

// SwitchPHP moves a domain to php, a supported version, restarting only
// the containers involved. A failure rolls the change back.
func (s *Service) SwitchPHP(ctx context.Context, domain, php string) (SwitchResult, error) {
	if err := phpversions.Validate(php); err != nil {
		return SwitchResult{}, err
	}
	current, found, err := s.Store.DomainPHPVersion(domain)
	if err != nil {
		return SwitchResult{}, err
	}
	if !found {
		return SwitchResult{}, i18n.Errorf("Domain '%s' is not configured.", domain)
	}
	res := SwitchResult{Domain: domain, From: current, To: php}
	if current == php {
		s.Log.Info(i18n.Tf("Domain '%s' is already using PHP %s.", domain, php))
		return res, nil
	}
	s.Log.Info(i18n.Tf("Switching '%s' from PHP %s to PHP %s...", domain, current, php))
	if err := resources.New(s.Dir, s.Log).EnsurePHPVersionConfig(php); err != nil {
		return SwitchResult{}, err
	}
	g := s.generator(s.Log)
	before, err := s.Store.ActivePHPVersions()
	if err != nil {
		return SwitchResult{}, err
	}
	if err := g.MoveToVersion(domain, current, php); err != nil {
		return SwitchResult{}, err
	}
	if _, err := s.Store.UpdateDomainPHPVersion(domain, php); err != nil {
		return SwitchResult{}, err
	}
	after, err := s.Store.ActivePHPVersions()
	if err != nil {
		return SwitchResult{}, err
	}

	err = func() error {
		if !slices.Contains(before, php) {
			if err := s.startVersion(ctx, php, after); err != nil {
				return err
			}
		} else {
			s.restartVersion(ctx, php)
		}
		// The old version lost a site
		if slices.Contains(after, current) {
			s.restartVersion(ctx, current)
		}
		if err := s.stopVersions(ctx, without(before, after), after); err != nil {
			return err
		}
		if err := s.writeMain(); err != nil {
			return err
		}
		s.restartProxy(ctx)
		return nil
	}()
	if err != nil {
		s.Log.Error(i18n.Tf("An error occurred: %v", err))
		_ = g.MoveToVersion(domain, php, current)
		_, _ = s.Store.UpdateDomainPHPVersion(domain, current)
		s.Log.Error(i18n.T("Rolled back version change."))
		return SwitchResult{}, &events.ReportedError{Err: err}
	}
	res.Switched = true
	return res, nil
}

// RenewCertificate generates the certificate of a registered host again,
// whatever its age, then restarts what serves it: the PHP container of a
// domain, and the reverse proxy.
func (s *Service) RenewCertificate(ctx context.Context, host string) error {
	php, isDomain, err := s.Store.DomainPHPVersion(host)
	if err != nil {
		return err
	}
	if !isDomain {
		taken, err := takenHosts(s.Store)
		if err != nil {
			return err
		}
		if !slices.Contains(taken, host) && !slices.Contains(services(s.Env), host) {
			return i18n.Errorf("Domain '%s' is not configured.", host)
		}
	}
	certs, err := s.Certs()
	if err != nil {
		return err
	}
	s.Log.Step(i18n.T("Generating SSL certificates..."))
	if err := certs.GenerateAll(ctx, []string{host}, true, s.Env.IsProduction()); err != nil {
		return err
	}
	if isDomain {
		s.restartVersion(ctx, php)
	}
	s.restartProxy(ctx)
	return nil
}

// services returns the hosts of the optional services that are on.
func services(e *env.Env) []string {
	hosts := soketi.Domains(e)
	if s3.Enabled(e) {
		hosts = append(hosts, s3.Domain)
	}
	return hosts
}
