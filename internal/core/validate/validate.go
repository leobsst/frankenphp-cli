// Package validate checks user input.
package validate

import (
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// domainRe: one or more labels of letters, digits and inner hyphens, then
// a letters-only TLD (myapp.test, s3.api-v2.test).
var domainRe = regexp.MustCompile(`^(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,63}$`)

// proxyTargetRe: optional http(s):// scheme, host or IP, optional port and
// trailing slash (127.0.0.1:8006, http://backend.internal:9000/).
var proxyTargetRe = regexp.MustCompile(`^(https?://)?[a-zA-Z0-9]([a-zA-Z0-9_.-]*[a-zA-Z0-9])?(:\d{1,5})?/?$`)

// Error is invalid input.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// Domain checks a domain name; localhost is allowed without TLD.
func Domain(domain string) error {
	if domain == "localhost" || domainRe.MatchString(domain) {
		return nil
	}
	return &Error{i18n.Tf("Invalid domain name: %s", domain)}
}

// ProxyTarget checks a reverse-proxy upstream address.
func ProxyTarget(target string) error {
	if proxyTargetRe.MatchString(target) {
		return nil
	}
	return &Error{i18n.Tf("Invalid proxy target: %s", target)}
}

// Directory checks that path is an existing directory.
func Directory(path string) error {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return nil
	}
	return &Error{i18n.Tf("Directory does not exist: %s", path)}
}

// rootRe: path segments of letters, digits, dots, dashes,
// underscores, @ and +, separated by slashes. No space nor brace: the root
// is written in a Caddyfile.
var rootRe = regexp.MustCompile(`^[a-zA-Z0-9._@+-]+(?:/[a-zA-Z0-9._@+-]+)*/?$`)

// Root checks the --root of a host: a path relative to the projects root,
// staying inside it.
func Root(root string) error {
	root = strings.ReplaceAll(root, `\`, "/")
	if rootRe.MatchString(root) && !slices.Contains(strings.Split(strings.TrimSuffix(root, "/"), "/"), "..") {
		return nil
	}
	return &Error{i18n.Tf("Invalid root: %s (a path relative to the project path, without '..')", root)}
}
