// Package s3 handles the optional S3 server, Garage.
//
// Off by default: S3_ENABLED=true turns it on. It runs in its own container
// and stores the objects in S3_DIR (<data directory>/s3 by default). The
// reverse proxy serves it over TLS at https://s3.frankenmanager.test, path
// style, and at https://<bucket>.s3.frankenmanager.test, virtual-host
// style. PHP apps reach its API at 127.0.0.1:S3_PORT.
//
// Garage has no ACL nor bucket policy: the ACL gateway (see gateway.go)
// serves S3_PORT in front of it and adds the object ACLs of AWS. A public
// bucket is a bucket whose website access is on, every object of it
// readable without credentials; a private bucket can hold public objects
// (x-amz-acl: public-read, PutObjectAcl). Both keep their S3 URL.
package s3

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/randstr"
)

const (
	ContainerName = "franken_s3"
	// ComposeService is the compose service of the container.
	ComposeService = "s3"
	Image          = "dxflrs/garage:v2.5.0"
	// Domain is the public S3 host, served over TLS by the reverse proxy;
	// <bucket>.Domain is the virtual-host style host of a bucket.
	Domain = "s3.frankenmanager.test"

	DefaultDir       = "./s3"
	DefaultPort      = "3900"
	DefaultWebPort   = "3902"
	DefaultAdminPort = "3903"
	DefaultRegion    = "us-east-1"
	// rpcPort is Garage's internal RPC port, never published.
	rpcPort = "3901"

	EnabledKey   = "S3_ENABLED"
	DirKey       = "S3_DIR"
	PortKey      = "S3_PORT"
	WebPortKey   = "S3_WEB_PORT"
	AdminPortKey = "S3_ADMIN_PORT"
	RegionKey    = "S3_REGION"
	AccessKeyKey = "S3_ACCESS_KEY_ID"
	SecretKeyKey = "S3_SECRET_ACCESS_KEY"
	// AdminTokenKey and RPCSecretKey are Garage's own secrets.
	AdminTokenKey = "S3_ADMIN_TOKEN"
	RPCSecretKey  = "S3_RPC_SECRET"

	// ConfigPath is Garage's configuration, relative to the data directory.
	ConfigPath = "garage/garage.toml"

	// GlobalKeyName is the name of the global access key in Garage.
	GlobalKeyName = "frankenmanager"
)

// generatedKeys are the credentials generated when empty, in display order.
var generatedKeys = []string{AccessKeyKey, SecretKeyKey, AdminTokenKey, RPCSecretKey}

const hexDigits = "0123456789abcdef"

// newValue returns a new value of a generated key, in the format Garage
// accepts: GK + 24 hex digits for an access key ID, 64 hex digits for the
// secrets.
func newValue(key string) string {
	if key == AccessKeyKey {
		return NewAccessKeyID()
	}
	return randstr.New(hexDigits, 64)
}

// NewAccessKeyID returns a new access key ID in Garage's format.
func NewAccessKeyID() string { return "GK" + randstr.New(hexDigits, 24) }

// Getter reads the configuration.
type Getter interface {
	Get(key string) string
}

// Config reads and writes the configuration.
type Config interface {
	Getter
	Set(key, value string) error
	Require(key string) (string, error)
}

func getOr(c Getter, key, def string) string {
	if v := strings.TrimSpace(c.Get(key)); v != "" {
		return v
	}
	return def
}

// Enabled reports whether S3_ENABLED is "true" (trimmed, any case).
func Enabled(c Getter) bool {
	return strings.ToLower(strings.TrimSpace(c.Get(EnabledKey))) == "true"
}

// Port returns S3_PORT, DefaultPort when empty.
func Port(c Getter) string { return getOr(c, PortKey, DefaultPort) }

// WebPort returns S3_WEB_PORT, DefaultWebPort when empty.
func WebPort(c Getter) string { return getOr(c, WebPortKey, DefaultWebPort) }

// AdminPort returns S3_ADMIN_PORT, DefaultAdminPort when empty.
func AdminPort(c Getter) string { return getOr(c, AdminPortKey, DefaultAdminPort) }

// Region returns S3_REGION, DefaultRegion when empty.
func Region(c Getter) string { return getOr(c, RegionKey, DefaultRegion) }

// Endpoint is the S3 API PHP apps reach, without TLS.
func Endpoint(c Getter) string { return "http://127.0.0.1:" + Port(c) }

// AdminURL is the base URL of Garage's admin API.
func AdminURL(c Getter) string { return "http://127.0.0.1:" + AdminPort(c) }

// PublicURL is the HTTPS URL of a bucket, path style.
func PublicURL(bucket string) string { return "https://" + Domain + "/" + bucket }

// BucketHost is the virtual-host style host of a bucket.
func BucketHost(bucket string) string { return bucket + "." + Domain }

// ProxyTarget returns the upstream the reverse proxy forwards Domain and
// the bucket hosts to, the ACL gateway; empty when S3 is disabled.
func ProxyTarget(c Getter) string {
	if !Enabled(c) {
		return ""
	}
	return "127.0.0.1:" + Port(c)
}

// Domains returns the hosts S3 needs a hosts entry and a DNS record for:
// Domain and the virtual-host style host of each bucket when enabled, none
// otherwise.
func Domains(c Getter, buckets []string) []string {
	if !Enabled(c) {
		return nil
	}
	domains := []string{Domain}
	for _, b := range buckets {
		domains = append(domains, BucketHost(b))
	}
	return domains
}

// CertNames are the names of the S3 certificate, stored as Domain.pem:
// Domain and every bucket host.
var CertNames = []string{Domain, "*." + Domain}

// EnsureCredentials fills in the credentials left empty and returns the
// names of the generated variables. Does nothing when disabled.
func EnsureCredentials(c Config) ([]string, error) {
	if !Enabled(c) {
		return nil, nil
	}
	var generated []string
	for _, key := range generatedKeys {
		if strings.TrimSpace(c.Get(key)) != "" {
			continue
		}
		if err := c.Set(key, newValue(key)); err != nil {
			return generated, err
		}
		generated = append(generated, key)
	}
	return generated, nil
}

var (
	accessKeyID = regexp.MustCompile(`^GK[0-9a-f]{24}$`)
	hexSecret   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ValidateAccessKeyID checks an access key ID: Garage only imports GK
// followed by 24 hex digits.
func ValidateAccessKeyID(v string) error {
	if !accessKeyID.MatchString(v) {
		return i18n.NewError("expected GK followed by 24 lowercase hex digits")
	}
	return nil
}

// ValidateHexSecret checks a secret: 64 lowercase hex digits.
func ValidateHexSecret(v string) error {
	if !hexSecret.MatchString(v) {
		return i18n.NewError("expected 64 lowercase hex digits")
	}
	return nil
}

var bucketName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

// ValidateBucket checks a bucket name: 3 to 63 lowercase letters, digits
// and hyphens, starting and ending with a letter or a digit. No dot, so
// that <bucket>.s3.frankenmanager.test matches the wildcard certificate.
func ValidateBucket(name string) error {
	if !bucketName.MatchString(name) || strings.HasPrefix(name, "xn--") {
		return i18n.Errorf("Invalid bucket name '%s': 3 to 63 lowercase letters, digits and hyphens, "+
			"starting and ending with a letter or a digit.", name)
	}
	return nil
}

// ComposeEnvVars returns the variables the S3 compose services expect,
// none when disabled. localhost is the port binding prefix ("127.0.0.1:",
// or "" when the services are exposed) of the gateway; the admin API stays
// on 127.0.0.1.
func ComposeEnvVars(c Config, localhost, dir string) (map[string]string, error) {
	if !Enabled(c) {
		return nil, nil
	}
	vars := map[string]string{
		DirKey:             dir,
		"S3_PUBLISH":       localhost + Port(c) + ":" + DefaultPort,
		"S3_ADMIN_PUBLISH": "127.0.0.1:" + AdminPort(c) + ":" + DefaultAdminPort,
		PortKey:            Port(c),
		WebPortKey:         WebPort(c),
		AdminPortKey:       AdminPort(c),
		GaragePortKey:      GaragePort(c),
		RegionKey:          Region(c),
	}
	for _, key := range []string{AdminTokenKey, RPCSecretKey, AccessKeyKey, SecretKeyKey} {
		value, err := c.Require(key)
		if err != nil {
			return nil, err
		}
		vars[key] = value
	}
	return vars, nil
}

// GarageConfig returns garage.toml: a single node, behind the ACL gateway.
// In production (host network) every port is on 127.0.0.1, the S3 API on
// S3_GARAGE_PORT. The secrets come from the environment
// (GARAGE_RPC_SECRET, GARAGE_ADMIN_TOKEN), not from this file.
func GarageConfig(c Getter, production bool) string {
	s3, web, admin, rpc := "[::]:"+DefaultPort, "[::]:"+DefaultWebPort, "[::]:"+DefaultAdminPort, "[::]:"+rpcPort
	if production {
		s3, web = "127.0.0.1:"+GaragePort(c), "127.0.0.1:"+WebPort(c)
		admin, rpc = "127.0.0.1:"+AdminPort(c), "127.0.0.1:"+rpcPort
	}
	return fmt.Sprintf(`# Generated by FrankenManager: do not edit, overwritten on start.
metadata_dir = "/var/lib/garage/meta"
data_dir = "/var/lib/garage/data"
db_engine = "sqlite"
replication_factor = 1
rpc_bind_addr = "%s"
rpc_public_addr = "127.0.0.1:%s"

[s3_api]
s3_region = "%s"
api_bind_addr = "%s"
root_domain = ".%s"

[s3_web]
bind_addr = "%s"
root_domain = ".%s"
index = "index.html"

[admin]
api_bind_addr = "%s"
`, rpc, rpcPort, Region(c), s3, Domain, web, Domain, admin)
}

// Service returns the YAML lines of the S3 compose services, Garage then
// the ACL gateway: host network in production; in development, only the
// gateway and the admin API are published.
func Service(production bool) []string {
	dir := "${" + DirKey + ":-./s3}"
	config := "./" + ConfigPath
	if production {
		dir = "${" + DirKey + ":-${PWD}/s3}"
		config = "${PWD}/" + ConfigPath
	}
	lines := []string{
		"  " + ComposeService + ":",
		"    container_name: " + ContainerName,
		"    image: " + Image,
		"    restart: always",
		"    user: ${UID}:${GID}",
		"    environment:",
		"      GARAGE_RPC_SECRET: ${" + RPCSecretKey + "}",
		"      GARAGE_ADMIN_TOKEN: ${" + AdminTokenKey + "}",
		"    volumes:",
		"      - " + config + ":/etc/garage.toml:ro",
		"      - " + dir + "/meta:/var/lib/garage/meta",
		"      - " + dir + "/data:/var/lib/garage/data",
	}
	if production {
		lines = append(lines, "    network_mode: host")
	} else {
		lines = append(lines,
			"    ports:",
			"      - ${S3_ADMIN_PUBLISH:-127.0.0.1:"+DefaultAdminPort+":"+DefaultAdminPort+"}",
			"    networks:",
			"      - franken_network",
		)
	}
	lines = append(lines,
		"    healthcheck:",
		`      test: ["CMD", "/garage", "status"]`,
		"      interval: 10s",
		"      timeout: 5s",
		"      retries: 5",
		"      start_period: 10s",
		"",
	)
	return append(lines, gatewayService(production)...)
}
