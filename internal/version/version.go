// Package version holds the FrankenManager version, injected at build time:
//
//	go build -ldflags "-X github.com/leobsst/frankenphp-cli/internal/version.Version=1.2.3"
package version

// Version is the FrankenManager version, "dev" for builds without -ldflags.
var Version = "dev"
