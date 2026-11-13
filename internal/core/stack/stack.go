// Package stack holds the compose helpers shared by the server commands.
package stack

import (
	"context"
	"slices"

	"github.com/leobsst/frankenphp-cli/internal/core/compose"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

// ProjectPath returns the projects root the server was started with,
// DEFAULT_PROJECT_PATH for servers started before it was stored, "" when
// unknown.
func ProjectPath(e *env.Env, s *store.Store) (string, error) {
	path, _, err := s.Setting(store.SettingProjectPath)
	if err != nil {
		return "", err
	}
	if path == "" {
		path = e.Get("DEFAULT_PROJECT_PATH")
	}
	return path, nil
}

// ComposeOptions returns what the compose file describes, from the
// configuration.
// dnsListen is where the LAN DNS server listens, "" when disabled.
func ComposeOptions(e *env.Env, phpVersions []string, dnsListen string) compose.Options {
	return compose.Options{
		PHPVersions: phpVersions,
		Production:  e.IsProduction(),
		DBEngines:   e.DBEngines(),
		DNSListen:   dnsListen,
		Redis:       e.RedisEnabled(),
		Websocket:   e.SoketiEnabled(),
	}
}

// GenerateCompose rewrites the compose file from the configuration.
func GenerateCompose(dataDir string, e *env.Env, phpVersions []string, dnsListen string) error {
	_, err := compose.Write(dataDir, ComposeOptions(e, phpVersions, dnsListen))
	return err
}

// ComposeUp regenerates the compose file and brings the whole stack up to
// date, customPath being the projects root mounted in the PHP containers.
func ComposeUp(ctx context.Context, d *docker.Manager, e *env.Env, phpVersions []string,
	dnsListen, customPath string) error {
	if err := GenerateCompose(d.Dir, e, phpVersions, dnsListen); err != nil {
		return err
	}
	vars, err := e.ComposeEnvVars(d.Dir, customPath, e.DBEngines())
	if err != nil {
		return err
	}
	return d.ComposeUp(ctx, vars)
}

// BuildImage builds the image of a PHP version with the configured build
// options, unless an up-to-date one exists and force is off. It reports
// whether it built.
func BuildImage(ctx context.Context, d *docker.Manager, e *env.Env, log Logger,
	customPath, phpVersion string, force bool) (bool, error) {
	distro, err := e.ImageDistro()
	if err != nil {
		return false, err
	}
	packages, warning, err := e.ExtraSystemPackages(phpVersion)
	if err != nil {
		return false, err
	}
	if warning != "" {
		log.Warning(warning)
	}
	return d.BuildImage(ctx, docker.Build{
		CustomPath:            customPath,
		PHPVersion:            phpVersion,
		WWWGroup:              e.Get("WWWGROUP"),
		ExtraSystemPackages:   packages,
		ExtraComposerPackages: e.ExtraPackages(phpVersion, "COMPOSER"),
		Distro:                distro,
		InstallPdftk:          e.PdftkEnabled(),
		Force:                 force,
	})
}

// BuildImages builds the images of phpVersions that need it, in version
// order: missing, built from other inputs, or force.
func BuildImages(ctx context.Context, d *docker.Manager, e *env.Env, log Logger,
	customPath string, phpVersions []string, force bool) error {
	for _, v := range slices.Sorted(slices.Values(phpVersions)) {
		if _, err := BuildImage(ctx, d, e, log, customPath, v, force); err != nil {
			return err
		}
	}
	return nil
}

// Logger receives the warnings.
type Logger interface {
	Warning(msg string)
}
