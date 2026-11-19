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
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/platform"
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
		return i18n.T("live")
	case ApplyAlias:
		return i18n.T("immediate")
	case ApplyRecreate:
		return "stop/start"
	case ApplyRebuild:
		return "restart --build"
	case ApplyRestartDNS:
		return "restart --dns"
	case ApplyRestartDB:
		return "restart --database"
	}
	return i18n.T("next command")
}

// Definition describes a configuration key.
type Definition struct {
	Key string
	// Description is the English description, marked with i18n.N: Describe
	// translates it.
	Description string
	// descFormat and descArg build Description when it depends on a value
	// (the PHP version of the EXTRA_*_PACKAGES_<version> keys).
	descFormat, descArg string
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

// Describe returns the description of d in the selected language.
func (d Definition) Describe() string {
	if d.descFormat != "" {
		return fmt.Sprintf(i18n.T(d.descFormat), d.descArg)
	}
	return i18n.T(d.Description)
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
		return i18n.Errorf("Invalid value '%s' for %s: expected one of %s",
			value, d.Key, strings.Join(d.Values, ", "))
	}
	if d.Validate != nil && value != "" {
		if err := d.Validate(value); err != nil {
			return i18n.Errorf("Invalid value '%s' for %s: %w", value, d.Key, err)
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
	"s3":     s3.EnabledKey,
}

// LanguageKey is the key of the language of the CLI messages.
const LanguageKey = "CLI_LANGUAGE"

// AliasKey is the key of the CLI alias.
const AliasKey = "CLI_ALIAS"

// DefaultAlias is the CLI alias installed by default.
const DefaultAlias = "fkm"

var booleans = []string{"true", "false"}

func static(v string) func() string { return func() string { return v } }

func validatePort(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		return i18n.NewError("not a port number (1-65535)")
	}
	return nil
}

func validateIP(v string) error {
	if _, err := netip.ParseAddr(v); err != nil {
		return i18n.NewError("not an IPv4 or IPv6 address")
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
		return i18n.NewError("not a numeric ID")
	}
	return nil
}

func validateDBEngines(v string) error {
	for _, e := range strings.Split(v, ",") {
		if !slices.Contains(validDBEngines, strings.ToLower(strings.TrimSpace(e))) {
			return i18n.Errorf("unknown engine '%s' (expected a comma-separated list of %s)",
				strings.TrimSpace(e), strings.Join(validDBEngines, ", "))
		}
	}
	return nil
}

var packetSize = regexp.MustCompile(`^[0-9]+[KMG]?$`)

func validatePacketSize(v string) error {
	if !packetSize.MatchString(v) {
		return i18n.NewError("expected a size such as 16M or 512M")
	}
	return nil
}

var aliasName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,31}$`)

// ValidateAlias checks a CLI alias name.
func ValidateAlias(v string) error {
	if !aliasName.MatchString(v) {
		return i18n.NewError("expected a command name (letters, digits, - and _, starting with a letter)")
	}
	if strings.EqualFold(v, "frankenmanager") {
		return i18n.NewError("frankenmanager is always available, choose another name")
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
		{Key: "APP_ENV", Description: i18n.N("dev for local development; prod/production only on a public/cloud server"),
			Default: static("dev"), Values: []string{"dev", "prod", "production"}, Apply: ApplyRecreate},
		{Key: "UID", Description: i18n.N("User ID the containers run as (defaults to the current user)"),
			Default: currentID(true), Validate: validateID, Apply: ApplyRecreate},
		{Key: "GID", Description: i18n.N("Group ID the containers run as (defaults to the current group)"),
			Default: currentID(false), Validate: validateID, Apply: ApplyRecreate},
		{Key: "WWWGROUP", Description: i18n.N("Group ID of the www-data user in the PHP images"),
			Validate: validateID, Apply: ApplyRebuild},
		{Key: AliasKey, Description: i18n.N("Short alias of the frankenmanager command (empty: no alias)"),
			Default: static(DefaultAlias), Validate: ValidateAlias, Apply: ApplyAlias},
		{Key: LanguageKey, Description: i18n.N("Language of the messages: auto follows the system language (LANG, LC_ALL...)"),
			Default: static(i18n.Auto), Values: append([]string{i18n.Auto}, i18n.Languages()...), Apply: ApplyNow},
		{Key: "DEFAULT_PROJECT_PATH", Description: i18n.N("Projects root used when start has no --path"),
			Default: platform.DefaultProjectsDir, Apply: ApplyNow},
		{Key: "DEFAULT_PHP_VERSION", Description: i18n.N("PHP version of new domains when --php is not given"),
			Default: static(phpversions.Default), Values: phpversions.Supported, Apply: ApplyNow},
		{Key: "CADDY_DIR", Description: i18n.N("Caddy files directory (relative to the data directory, or absolute)"),
			Default: static("./caddy"), Apply: ApplyRecreate},
		{Key: "DATABASE_DIR", Description: i18n.N("Database data directory (relative to the data directory, or absolute)"),
			Default: static("./database"), Apply: ApplyRecreate},
		{Key: "EXPOSE_SERVICES", Description: i18n.N("Expose the service ports to the network instead of 127.0.0.1"),
			Default: static("false"), Values: booleans, Apply: ApplyRecreate},
		{Key: "WEB_HTTP_PORT", Description: i18n.N("HTTP port of the web server"),
			Default: static("80"), Validate: validatePort, Apply: ApplyRecreate},
		{Key: "WEB_HTTPS_PORT", Description: i18n.N("HTTPS port of the web server"),
			Default: static("443"), Validate: validatePort, Apply: ApplyRecreate},

		{Key: "DB_ENGINES", Description: i18n.N("Database engines, comma-separated: mariadb, mysql, postgresql"),
			Default: static("mariadb"), Validate: validateDBEngines, Apply: ApplyRecreate},
		{Key: "DB_PORT", Description: i18n.N("MariaDB port"), Default: static("3306"), Validate: validatePort, Apply: ApplyRecreate},
		{Key: "MARIADB_VERSION", Description: i18n.N("MariaDB image version"), Default: static("10.11.9"), Apply: ApplyRecreate},
		{Key: "MARIADB_ROOT_PASSWORD", Description: i18n.N("MariaDB root password (generated on start when empty)"),
			Secret: true, Apply: ApplyRestartDB},
		{Key: "MYSQL_VERSION", Description: i18n.N("MySQL image version"), Default: static("8.0"), Apply: ApplyRecreate},
		{Key: "MYSQL_PORT", Description: i18n.N("MySQL port"), Default: static("3307"), Validate: validatePort, Apply: ApplyRecreate},
		{Key: "MYSQL_ROOT_PASSWORD", Description: i18n.N("MySQL root password (generated on start when empty)"),
			Secret: true, Apply: ApplyRestartDB},
		{Key: "MYSQL_MAX_ALLOWED_PACKET", Description: i18n.N("Maximum packet size of MariaDB and MySQL"),
			Default: static("512M"), Validate: validatePacketSize, Apply: ApplyRecreate},
		{Key: "POSTGRES_VERSION", Description: i18n.N("PostgreSQL image version"), Default: static("16"), Apply: ApplyRecreate},
		{Key: "POSTGRES_USER", Description: i18n.N("PostgreSQL user"), Default: static("postgres"), Apply: ApplyRecreate},
		{Key: "POSTGRES_PASSWORD", Description: i18n.N("PostgreSQL password (generated on start when empty)"),
			Secret: true, Apply: ApplyRecreate},
		{Key: "POSTGRES_PORT", Description: i18n.N("PostgreSQL port"), Default: static("5432"), Validate: validatePort, Apply: ApplyRecreate},
		{Key: "PMA_PORT", Description: i18n.N("phpMyAdmin port (dev only)"), Default: static("8080"), Validate: validatePort, Apply: ApplyRecreate},
		{Key: "PGADMIN_PORT", Description: i18n.N("pgAdmin port (dev only)"), Default: static("8081"), Validate: validatePort, Apply: ApplyRecreate},

		{Key: "REDIS_ENABLED", Description: i18n.N("Run a Redis cache server"),
			Default: static("false"), Values: booleans, Apply: ApplyLive, Service: "redis"},
		{Key: "REDIS_PORT", Description: i18n.N("Redis port"), Default: static("6379"), Validate: validatePort, Apply: ApplyRecreate},

		{Key: "SOKETI_ENABLED", Description: i18n.N("Run a Soketi WebSocket server at wss://ws.frankenmanager.test"),
			Default: static("false"), Values: booleans, Apply: ApplyLive, Service: "soketi"},
		{Key: "SOKETI_PORT", Description: i18n.N("Soketi port"), Default: static("6001"), Validate: validatePort, Apply: ApplyRecreate},
		{Key: "SOKETI_DEFAULT_APP_ID", Description: i18n.N("Soketi app ID (generated on start when empty)"), Apply: ApplyRecreate},
		{Key: "SOKETI_DEFAULT_APP_KEY", Description: i18n.N("Soketi app key (generated on start when empty)"), Apply: ApplyRecreate},
		{Key: "SOKETI_DEFAULT_APP_SECRET", Description: i18n.N("Soketi app secret (generated on start when empty)"),
			Secret: true, Apply: ApplyRecreate},

		{Key: s3.EnabledKey, Description: i18n.N("Run a Garage S3 server at https://s3.frankenmanager.test"),
			Default: static("false"), Values: booleans, Apply: ApplyLive, Service: "s3"},
		{Key: s3.DirKey, Description: i18n.N("S3 objects directory (relative to the data directory, or absolute)"),
			Default: static(s3.DefaultDir), Apply: ApplyRecreate},
		{Key: s3.PortKey, Description: i18n.N("S3 API port"), Default: static(s3.DefaultPort), Validate: validatePort, Apply: ApplyRecreate},
		{Key: s3.WebPortKey, Description: i18n.N("Port of Garage's website endpoint behind the ACL gateway (production, on 127.0.0.1)"),
			Default: static(s3.DefaultWebPort), Validate: validatePort, Apply: ApplyRecreate},
		{Key: s3.GaragePortKey, Description: i18n.N("Port of Garage's S3 API behind the ACL gateway (production, on 127.0.0.1)"),
			Default: static(s3.DefaultGaragePort), Validate: validatePort, Apply: ApplyRecreate},
		{Key: s3.AdminPortKey, Description: i18n.N("S3 admin API port (always on 127.0.0.1)"),
			Default: static(s3.DefaultAdminPort), Validate: validatePort, Apply: ApplyRecreate},
		{Key: s3.RegionKey, Description: i18n.N("S3 region"), Default: static(s3.DefaultRegion), Apply: ApplyRecreate},
		{Key: s3.AccessKeyKey, Description: i18n.N("Global S3 access key ID, allowed on every bucket (generated on start when empty)"),
			Validate: s3.ValidateAccessKeyID, Apply: ApplyRecreate},
		{Key: s3.SecretKeyKey, Description: i18n.N("Global S3 secret access key (generated on start when empty)"),
			Validate: s3.ValidateHexSecret, Secret: true, Apply: ApplyRecreate},
		{Key: s3.AdminTokenKey, Description: i18n.N("S3 admin API token (generated on start when empty)"),
			Secret: true, Apply: ApplyRecreate},
		{Key: s3.RPCSecretKey, Description: i18n.N("S3 internal RPC secret (generated on start when empty)"),
			Validate: s3.ValidateHexSecret, Secret: true, Apply: ApplyRecreate},

		{Key: "DNS_SERVER", Description: i18n.N("Run a LAN DNS server resolving every domain to this machine"),
			Default: static("false"), Values: booleans, Apply: ApplyLive, Service: "dns"},
		{Key: "DNS_IP", Description: i18n.N("IP the domains resolve to (empty: this machine's LAN IP, followed)"),
			Validate: validateIP, Apply: ApplyRestartDNS},
		{Key: "DNS_LISTEN_IP", Description: i18n.N("Address the DNS server listens on (empty: every interface)"),
			Validate: validateIP, Apply: ApplyRestartDNS},
		{Key: "DNS_PORT", Description: i18n.N("Port of the DNS server"), Default: static("53"), Validate: validatePort, Apply: ApplyRestartDNS},
		{Key: "DNS_UPSTREAM", Description: i18n.N("Space-separated resolvers for the other queries"),
			Default: static("1.1.1.1 8.8.8.8"), Validate: validateIPs, Apply: ApplyRestartDNS},

		{Key: "PHP_IMAGE_DISTRO", Description: i18n.N("Base distribution of the PHP images"),
			Default: static(phpversions.DefaultDistro), Values: phpversions.Distros, Apply: ApplyRebuild},
		{Key: "INSTALL_PDFTK", Description: i18n.N("Install pdftk in the PHP images (adds a Java runtime, ~200 MB)"),
			Default: static("false"), Values: booleans, Apply: ApplyRebuild},
	}
	for _, t := range []struct{ kind, all, one string }{
		{"APK", i18n.N("Extra Alpine packages of every PHP image, space-separated"),
			i18n.N("Extra Alpine packages of the PHP %s image only")},
		{"APT", i18n.N("Extra Debian packages of every PHP image, space-separated"),
			i18n.N("Extra Debian packages of the PHP %s image only")},
		{"COMPOSER", i18n.N("Extra global Composer packages of every PHP image, space-separated"),
			i18n.N("Extra global Composer packages of the PHP %s image only")},
	} {
		defs = append(defs, Definition{
			Key:         "EXTRA_" + t.kind + "_PACKAGES",
			Description: t.all,
			Apply:       ApplyRebuild,
		})
		for _, v := range phpversions.Supported {
			defs = append(defs, Definition{
				Key:         "EXTRA_" + t.kind + "_PACKAGES_" + strings.ReplaceAll(v, ".", ""),
				Description: fmt.Sprintf(t.one, v),
				descFormat:  t.one,
				descArg:     v,
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
