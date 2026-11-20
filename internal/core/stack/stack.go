// Package stack holds the compose helpers shared by the server commands.
package stack

import (
	"context"
	"slices"

	assets "github.com/leobsst/frankenphp-cli"
	"github.com/leobsst/frankenphp-cli/internal/core/caddyfile"
	"github.com/leobsst/frankenphp-cli/internal/core/compose"
	"github.com/leobsst/frankenphp-cli/internal/core/docker"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
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
// configuration. staticSites mounts the static hosts in the reverse proxy.
func ComposeOptions(e *env.Env, phpVersions []string, staticSites bool) compose.Options {
	return compose.Options{
		StaticSites: staticSites,
		PHPVersions: phpVersions,
		Production:  e.IsProduction(),
		DBEngines:   e.DBEngines(),
		Redis:       e.RedisEnabled(),
		Websocket:   e.SoketiEnabled(),
		S3:          e.S3Enabled(),
	}
}

// GenerateCompose rewrites the compose file from the configuration. The
// static hosts are mounted when their site files exist.
func GenerateCompose(dataDir string, e *env.Env, phpVersions []string) error {
	if e.S3Enabled() {
		dir := e.ResolveDir(s3.DirKey, s3.DefaultDir, dataDir)
		if err := s3.WriteConfig(dataDir, dir, e, e.IsProduction()); err != nil {
			return err
		}
	}
	static := caddyfile.HasStaticSites(e.ResolveDir("CADDY_DIR", "./caddy", dataDir))
	_, err := compose.Write(dataDir, ComposeOptions(e, phpVersions, static))
	return err
}

// ComposeDown stops the stack, with the variables of the compose file (the
// projects root the server runs with).
func ComposeDown(ctx context.Context, d *docker.Manager, e *env.Env, s *store.Store) error {
	customPath, err := ProjectPath(e, s)
	if err != nil {
		return err
	}
	vars, err := e.ComposeEnvVars(d.Dir, customPath, e.DBEngines())
	if err != nil {
		// A setting down does not need is missing (a password...): the
		// projects root is the variable compose cannot read the file
		// without, the others only warn
		vars = map[string]string{"CUSTOM_PATH": customPath, "UID": e.Get("UID"), "GID": e.Get("GID")}
	}
	return d.ComposeDown(ctx, vars)
}

// ComposeUp regenerates the compose file and brings the whole stack up to
// date, customPath being the projects root mounted in the PHP containers.
func ComposeUp(ctx context.Context, d *docker.Manager, e *env.Env, phpVersions []string,
	customPath string) error {
	if err := GenerateCompose(d.Dir, e, phpVersions); err != nil {
		return err
	}
	vars, err := e.ComposeEnvVars(d.Dir, customPath, e.DBEngines())
	if err != nil {
		return err
	}
	if e.S3Enabled() {
		if err := BuildS3Gateway(ctx, d); err != nil {
			return err
		}
	}
	return d.ComposeUp(ctx, vars)
}

// BuildS3Gateway writes the build context of the S3 ACL gateway and builds
// its image when its sources changed.
func BuildS3Gateway(ctx context.Context, d *docker.Manager) error {
	dir, hash, err := s3.WriteGatewayContext(assets.GatewayFS, d.Dir)
	if err != nil {
		return err
	}
	if d.IsImageCurrent(ctx, s3.GatewayImage, hash) {
		return nil
	}
	if d.Log != nil {
		d.Log.Info(i18n.T("Building the S3 ACL gateway image..."))
	}
	_, err = d.BuildLocalImage(ctx, s3.GatewayImage, hash, dir)
	return err
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
