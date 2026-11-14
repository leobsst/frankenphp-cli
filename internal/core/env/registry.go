package env

import (
	"fmt"
	"net/netip"
	"os"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/core/phpversions"
)

// Apply tells what a new value needs to take effect on a running server.
type Apply int

const (
	// ApplyNow: read by the next command, nothing to restart.
	ApplyNow Apply = iota
	// ApplyLive: an optional service, started or stopped at once.
	ApplyLive
	// ApplyAlias: the CLI alias, installed at once.
	ApplyAlias
	// ApplyRecreate: the containers must be recreated (stop, then start).
	ApplyRecreate
	// ApplyRebuild: the PHP images must be rebuilt (restart --build).
	ApplyRebuild
	// ApplyRestartDNS: the LAN DNS server must be refreshed (restart --dns).
	ApplyRestartDNS
	// ApplyRestartDB: the root passwords are synced by restart --database.
	ApplyRestartDB
)

// Hint returns the command applying a change on a running server, "" when
// the change applies by itself.
func (a Apply) Hint() string {
	switch a {
	case ApplyRecreate:
		return "frankenmanager stop && frankenmanager start"
	case ApplyRebuild:
		return "frankenmanager restart --build"
	case ApplyRestartDNS:
		return "frankenmanager restart --dns"
	case ApplyRestartDB:
		return "frankenmanager restart --database"
	}
	return ""
}

// Label is how `configuration list` shows a.
func (a Apply) Label() string {
	switch a {
	case ApplyLive:
		return "live"
	case ApplyAlias:
		return "immediate"
	case ApplyRecreate:
		return "stop/start"
	case ApplyRebuild:
		return "restart --build"
	case ApplyRestartDNS:
		return "restart --dns"
	case ApplyRestartDB:
		return "restart --database"
	}
	return "next command"
}

// Definition describes a configuration key.
type Definition struct {
	Key         string
	Description string
	// Default is the value used when the key is not set (may be computed,
	// e.g. UID).
	Default func() string
	// Values are the accepted values, also offered by the completion. Nil
	// accepts anything Validate accepts.
	Values   []string
	Validate func(string) error
	Apply    Apply
	// Secret values are masked by `configuration list`.
	Secret bool
	// Service is the optional service a live key toggles.
	Service string
}

// DefaultValue returns the default of d, "" when none.
func (d Definition) DefaultValue() string {
	if d.Default == nil {
		return ""
	}
	return d.Default()
}

// Check validates a value for d. The empty value always passes: it means
// "use the built-in behavior" for keys without a default.
func (d Definition) Check(value string) error {
	if d.Values != nil && !slices.Contains(d.Values, value) {
		return fmt.Errorf("Invalid value '%s' for %s: expected one of %s", //nolint:staticcheck // user-facing message
			value, d.Key, strings.Join(d.Values, ", "))
	}
	if d.Validate != nil && value != "" {
		if err := d.Validate(value); err != nil {
			return fmt.Errorf("Invalid value '%s' for %s: %w", value, d.Key, err) //nolint:staticcheck // user-facing message
		}
	}
	return nil
}

// canonical returns the accepted value matching value regardless of case
// and surrounding spaces ("True" -> "true"), value as is otherwise.
func (d Definition) canonical(value string) string {
	for _, v := range d.Values {
		if strings.EqualFold(v, strings.TrimSpace(value)) {
			return v
		}
	}
	return value
}

// Services are the optional services that can be turned on and off live,
// with their key.
var Services = map[string]string{
	"dns":    "DNS_SERVER",
	"redis":  "REDIS_ENABLED",
	"soketi": "SOKETI_ENABLED",
}

// AliasKey is the key of the CLI alias.
const AliasKey = "CLI_ALIAS"

// DefaultAlias is the CLI alias installed by default.
const DefaultAlias = "fkm"

var booleans = []string{"true", "false"}

func static(v string) func() string { return func() string { return v } }

func validatePort(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("not a port number (1-65535)")
	}
	return nil
}

func validateIP(v string) error {
	if _, err := netip.ParseAddr(v); err != nil {
		return fmt.Errorf("not an IPv4 or IPv6 address")
	}
	return nil
}

func validateIPs(v string) error {
	for _, ip := range strings.Fields(v) {
		if err := validateIP(ip); err != nil {
			return err
		}
	}
	return nil
}

func validateID(v string) error {
	if n, err := strconv.Atoi(v); err != nil || n < 0 {
		return fmt.Errorf("not a numeric ID")
	}
	return nil
}

func validateDBEngines(v string) error {
	for _, e := range strings.Split(v, ",") {
		if !slices.Contains(validDBEngines, strings.ToLower(strings.TrimSpace(e))) {
			return fmt.Errorf("unknown engine '%s' (expected a comma-separated list of %s)",
				strings.TrimSpace(e), strings.Join(validDBEngines, ", "))
		}
	}
	return nil
}

var packetSize = regexp.MustCompile(`^[0-9]+[KMG]?$`)

func validatePacketSize(v string) error {
	if !packetSize.MatchString(v) {
		return fmt.Errorf("expected a size such as 16M or 512M")
	}
	return nil
}

var aliasName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,31}$`)

// ValidateAlias checks a CLI alias name.
func ValidateAlias(v string) error {
	if !aliasName.MatchString(v) {
		return fmt.Errorf("expected a command name (letters, digits, - and _, starting with a letter)")
	}
	if strings.EqualFold(v, "frankenmanager") {
		return fmt.Errorf("frankenmanager is always available, choose another name")
	}
	return nil
}

// currentID returns the user (uid) or group ID, 1000 on Windows.
func currentID(uid bool) func() string {
	return func() string {
		if runtime.GOOS == "windows" {
			return "1000"
		}
		if uid {
			return strconv.Itoa(os.Getuid())
		}
		return strconv.Itoa(os.Getgid())
	}
}

// definitions lists every key, in the order of `configuration list`.
var definitions = buildDefinitions()

func buildDefinitions() []Definition {
	defs := []Definition{
		{Key: "APP_ENV", Description: "Environment: dev, or prod/production (host network, no phpMyAdmin)",
			Default: static("dev"), Values: []string{"dev", "prod", "production"}, Apply: ApplyRecreate},
		{Key: "UID", Description: "User ID the containers run as (defaults to the current user)",
			Default: currentID(true), Validate: validateID, Apply: ApplyRecreate},
		{Key: "GID", Description: "Group ID the containers run as (defaults to the current group)",
			Default: currentID(false), Validate: validateID, Apply: ApplyRecreate},
		{Key: "WWWGROUP", Description: "Group ID of the www-data user in the PHP images",
			Validate: validateID, Apply: ApplyRebuild},
		{Key: AliasKey, Description: "Short alias of the frankenmanager command (empty: no alias)",
			Default: static(DefaultAlias), Validate: ValidateAlias, Apply: ApplyAlias},
		{Key: "DEFAULT_PROJECT_PATH", Description: "Projects root used when start has no --path",
			Apply: ApplyNow},
		{Key: "DEFAULT_PHP_VERSION", Description: "PHP version of new domains when --php is not given",
			Default: static(phpversions.Default), Values: phpversions.Supported, Apply: ApplyNow},
		{Key: "CADDY_DIR", Description: "Caddy files directory (relative to the data directory, or absolute)",
			Default: static("./caddy"), Apply: ApplyRecreate},
		{Key: "DATABASE_DIR", Description: "Database data directory (relative to the data directory, or absolute)",
			Default: static("./database"), Apply: ApplyRecreate},
		{Key: "EXPOSE_SERVICES", Description: "Expose the service ports to the network instead of 127.0.0.1",
			Default: static("false"), Values: booleans, Apply: ApplyRecreate},
		{Key: "WEB_HTTP_PORT", Description: "HTTP port of the web server",
			Default: static("80"), Validate: validatePort, Apply: ApplyRecreate},
		{Key: "WEB_HTTPS_PORT", Description: "HTTPS port of the web server",
			Default: static("443"), Validate: validatePort, Apply: ApplyRecreate},

		{Key: "DB_ENGINES", Description: "Database engines, comma-separated: mariadb, mysql, postgresql",
			Default: static("mariadb"), Validate: validateDBEngines, Apply: ApplyRecreate},
		{Key: "DB_PORT", Description: "MariaDB port", Default: static("3306"), Validate: validatePort, Apply: ApplyRecreate},
		{Key: "MARIADB_VERSION", Description: "MariaDB image version", Default: static("10.11.9"), Apply: ApplyRecreate},
		{Key: "MARIADB_ROOT_PASSWORD", Description: "MariaDB root password (generated on start when empty)",
			Secret: true, Apply: ApplyRestartDB},
		{Key: "MYSQL_VERSION", Description: "MySQL image version", Default: static("8.0"), Apply: ApplyRecreate},
		{Key: "MYSQL_PORT", Description: "MySQL port", Default: static("3307"), Validate: validatePort, Apply: ApplyRecreate},
		{Key: "MYSQL_ROOT_PASSWORD", Description: "MySQL root password (generated on start when empty)",
			Secret: true, Apply: ApplyRestartDB},
		{Key: "MYSQL_MAX_ALLOWED_PACKET", Description: "Maximum packet size of MariaDB and MySQL",
			Default: static("512M"), Validate: validatePacketSize, Apply: ApplyRecreate},
		{Key: "POSTGRES_VERSION", Description: "PostgreSQL image version", Default: static("16"), Apply: ApplyRecreate},
		{Key: "POSTGRES_USER", Description: "PostgreSQL user", Default: static("postgres"), Apply: ApplyRecreate},
		{Key: "POSTGRES_PASSWORD", Description: "PostgreSQL password (generated on start when empty)",
			Secret: true, Apply: ApplyRecreate},
		{Key: "POSTGRES_PORT", Description: "PostgreSQL port", Default: static("5432"), Validate: validatePort, Apply: ApplyRecreate},
		{Key: "PMA_PORT", Description: "phpMyAdmin port (dev only)", Default: static("8080"), Validate: validatePort, Apply: ApplyRecreate},
		{Key: "PGADMIN_PORT", Description: "pgAdmin port (dev only)", Default: static("8081"), Validate: validatePort, Apply: ApplyRecreate},

		{Key: "REDIS_ENABLED", Description: "Run a Redis cache server",
			Default: static("false"), Values: booleans, Apply: ApplyLive, Service: "redis"},
		{Key: "REDIS_PORT", Description: "Redis port", Default: static("6379"), Validate: validatePort, Apply: ApplyRecreate},

		{Key: "SOKETI_ENABLED", Description: "Run a Soketi WebSocket server at wss://ws.frankenmanager.test",
			Default: static("false"), Values: booleans, Apply: ApplyLive, Service: "soketi"},
		{Key: "SOKETI_PORT", Description: "Soketi port", Default: static("6001"), Validate: validatePort, Apply: ApplyRecreate},
		{Key: "SOKETI_DEFAULT_APP_ID", Description: "Soketi app ID (generated on start when empty)", Apply: ApplyRecreate},
		{Key: "SOKETI_DEFAULT_APP_KEY", Description: "Soketi app key (generated on start when empty)", Apply: ApplyRecreate},
		{Key: "SOKETI_DEFAULT_APP_SECRET", Description: "Soketi app secret (generated on start when empty)",
			Secret: true, Apply: ApplyRecreate},

		{Key: "DNS_SERVER", Description: "Run a LAN DNS server resolving every domain to this machine",
			Default: static("false"), Values: booleans, Apply: ApplyLive, Service: "dns"},
		{Key: "DNS_IP", Description: "IP the domains resolve to (empty: this machine's LAN IP, followed)",
			Validate: validateIP, Apply: ApplyRestartDNS},
		{Key: "DNS_LISTEN_IP", Description: "Address the DNS server listens on (empty: every interface)",
			Validate: validateIP, Apply: ApplyRestartDNS},
		{Key: "DNS_PORT", Description: "Port of the DNS server", Default: static("53"), Validate: validatePort, Apply: ApplyRestartDNS},
		{Key: "DNS_UPSTREAM", Description: "Space-separated resolvers for the other queries",
			Default: static("1.1.1.1 8.8.8.8"), Validate: validateIPs, Apply: ApplyRestartDNS},

		{Key: "PHP_IMAGE_DISTRO", Description: "Base distribution of the PHP images",
			Default: static(phpversions.DefaultDistro), Values: phpversions.Distros, Apply: ApplyRebuild},
		{Key: "INSTALL_PDFTK", Description: "Install pdftk in the PHP images (adds a Java runtime, ~200 MB)",
			Default: static("false"), Values: booleans, Apply: ApplyRebuild},
	}
	for _, t := range []struct{ kind, what string }{
		{"APK", "Alpine packages"}, {"APT", "Debian packages"}, {"COMPOSER", "global Composer packages"},
	} {
		defs = append(defs, Definition{
			Key:         "EXTRA_" + t.kind + "_PACKAGES",
			Description: "Extra " + t.what + " of every PHP image, space-separated",
			Apply:       ApplyRebuild,
		})
		for _, v := range phpversions.Supported {
			defs = append(defs, Definition{
				Key:         "EXTRA_" + t.kind + "_PACKAGES_" + strings.ReplaceAll(v, ".", ""),
				Description: "Extra " + t.what + " of the PHP " + v + " image only",
				Apply:       ApplyRebuild,
			})
		}
	}
	return defs
}

// Definitions returns every known key, in display order.
func Definitions() []Definition { return slices.Clone(definitions) }

// Keys returns every known key name, in display order.
func Keys() []string {
	keys := make([]string, len(definitions))
	for i, d := range definitions {
		keys[i] = d.Key
	}
	return keys
}

// Lookup returns the definition of key, case-insensitive.
func Lookup(key string) (Definition, bool) {
	for _, d := range definitions {
		if strings.EqualFold(d.Key, key) {
			return d, true
		}
	}
	return Definition{}, false
}

// defaultOf returns the default of a known key, "" otherwise.
func defaultOf(key string) string {
	if d, ok := Lookup(key); ok {
		return d.DefaultValue()
	}
	return ""
}
