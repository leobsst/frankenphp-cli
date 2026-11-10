// Package phpversions lists the supported PHP versions and their settings.
package phpversions

import (
	"fmt"
	"slices"
	"strings"
)

// Supported lists the supported PHP versions, in order.
var Supported = []string{"8.2", "8.3", "8.4", "8.5"}

// Default is the PHP version used when DEFAULT_PHP_VERSION is unset or invalid.
const Default = "8.3"

// Deprecated versions are still supported but print a warning.
var Deprecated = []string{"8.2"}

// Distros are the supported base image distributions, appended to the tag.
var Distros = []string{"alpine", "trixie"}

// DefaultDistro is used when PHP_IMAGE_DISTRO is unset.
const DefaultDistro = "alpine"

// internalPorts are the HTTP and HTTPS ports of each PHP container, behind
// the reverse proxy listening on 80/443.
var internalPorts = map[string][2]int{
	"8.2": {8280, 8243},
	"8.3": {8380, 8343},
	"8.4": {8480, 8443},
	"8.5": {8580, 8543},
}

// Validate returns an error when version is not supported, reported to the
// user as a plain message listing the supported versions.
func Validate(version string) error {
	if !slices.Contains(Supported, version) {
		return fmt.Errorf("Unsupported PHP version '%s'. Supported: %s", //nolint:staticcheck // user-facing message
			version, strings.Join(Supported, ", "))
	}
	return nil
}

// ResolveDefault returns envValue (DEFAULT_PHP_VERSION) when it is a
// supported version, Default otherwise, silently.
func ResolveDefault(envValue string) string {
	if slices.Contains(Supported, envValue) {
		return envValue
	}
	return Default
}

// ResolveDistro returns the distribution configured by envValue
// (PHP_IMAGE_DISTRO, trimmed and lowercased), DefaultDistro when empty.
func ResolveDistro(envValue string) (string, error) {
	if envValue == "" {
		return DefaultDistro, nil
	}
	distro := strings.ToLower(strings.TrimSpace(envValue))
	if !slices.Contains(Distros, distro) {
		return "", fmt.Errorf("Unsupported PHP_IMAGE_DISTRO '%s'. Supported: %s", //nolint:staticcheck // user-facing message
			envValue, strings.Join(Distros, ", "))
	}
	return distro, nil
}

// ImageTag returns the FrankenPHP base image tag, e.g. "1-php8.3-alpine".
func ImageTag(version, distro string) (string, error) {
	if err := Validate(version); err != nil {
		return "", err
	}
	return "1-php" + version + "-" + distro, nil
}

// ContainerName returns the container of a version, e.g. "frankenphp-8.3".
func ContainerName(version string) string { return "frankenphp-" + version }

// ImageName returns the built image of a version, e.g. "custom-frankenphp:8.3".
func ImageName(version string) string { return "custom-frankenphp:" + version }

// InternalPorts returns the HTTP and HTTPS ports of a version's container.
func InternalPorts(version string) (http, https int, err error) {
	ports, ok := internalPorts[version]
	if !ok {
		return 0, 0, Validate(version)
	}
	return ports[0], ports[1], nil
}

// DeprecationWarning returns the warning to print for a deprecated version,
// "" otherwise.
func DeprecationWarning(version string) string {
	if !slices.Contains(Deprecated, version) {
		return ""
	}
	return fmt.Sprintf("PHP %s is deprecated and will be removed in a future release. "+
		"Consider upgrading to PHP %s or later.", version, Default)
}
