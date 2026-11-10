// Package soketi handles the optional Soketi WebSocket server (Pusher
// protocol).
//
// Off by default: SOKETI_ENABLED=true turns it on. It runs in its own
// container, served over TLS by the reverse proxy at
// wss://ws.frankenmanager.test, while PHP apps reach its HTTP API at
// 127.0.0.1:SOKETI_PORT.
package soketi

import (
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/randstr"
)

const (
	ContainerName = "franken_soketi"
	Image         = "quay.io/soketi/soketi:1.6-16-alpine"
	// Domain is the public WebSocket host, served over TLS by the reverse proxy.
	Domain       = "ws.frankenmanager.test"
	DefaultPort  = "6001"
	DefaultAppID = "frankenmanager"

	AppIDKey     = "SOKETI_DEFAULT_APP_ID"
	AppKeyKey    = "SOKETI_DEFAULT_APP_KEY"
	AppSecretKey = "SOKETI_DEFAULT_APP_SECRET"
)

// CredentialKeys are the default app credentials, in .env order.
var CredentialKeys = []string{AppIDKey, AppKeyKey, AppSecretKey}

// Getter reads the .env settings.
type Getter interface {
	Get(key string) string
}

// Config reads and writes the .env settings.
type Config interface {
	Getter
	Set(key, value string) error
	Require(key string) (string, error)
}

// Enabled reports whether SOKETI_ENABLED is "true" (trimmed, any case).
func Enabled(c Getter) bool {
	return strings.ToLower(strings.TrimSpace(c.Get("SOKETI_ENABLED"))) == "true"
}

// Port returns SOKETI_PORT, DefaultPort when empty.
func Port(c Getter) string {
	if p := strings.TrimSpace(c.Get("SOKETI_PORT")); p != "" {
		return p
	}
	return DefaultPort
}

// Target returns the upstream the reverse proxy forwards Domain to,
// "127.0.0.1:<port>", or "" when Soketi is disabled.
func Target(c Getter) string {
	if !Enabled(c) {
		return ""
	}
	return "127.0.0.1:" + Port(c)
}

// Domains returns the hosts Soketi needs a certificate, a hosts entry and a
// DNS record for: [Domain] when enabled, none otherwise.
func Domains(c Getter) []string {
	if !Enabled(c) {
		return nil
	}
	return []string{Domain}
}

// EnsureCredentials fills in the credentials left empty in .env and returns
// the names of the generated variables. Does nothing when disabled.
func EnsureCredentials(c Config) ([]string, error) {
	if !Enabled(c) {
		return nil, nil
	}
	var generated []string
	for _, key := range CredentialKeys {
		if strings.TrimSpace(c.Get(key)) != "" {
			continue
		}
		value := DefaultAppID
		if key != AppIDKey {
			value = randstr.New(randstr.LowerAlphanumeric, 32)
		}
		if err := c.Set(key, value); err != nil {
			return generated, err
		}
		generated = append(generated, key)
	}
	return generated, nil
}

// ComposeEnvVars returns the variables the Soketi compose service expects,
// none when disabled. localhost is the port binding prefix ("127.0.0.1:",
// or "" when the services are exposed).
func ComposeEnvVars(c Config, localhost string) (map[string]string, error) {
	if !Enabled(c) {
		return nil, nil
	}
	port := Port(c)
	vars := map[string]string{
		"SOKETI_PORT":    port,
		"SOKETI_PUBLISH": localhost + port + ":" + DefaultPort,
	}
	for _, key := range CredentialKeys {
		value, err := c.Require(key)
		if err != nil {
			return nil, err
		}
		vars[key] = value
	}
	return vars, nil
}

// Service returns the YAML lines of the Soketi compose service: host
// network in production, published port in development.
//
// No env_file on purpose: Soketi reads its own SOKETI_* variables, and the
// host port SOKETI_PORT must not leak into the container as is.
func Service(production bool) []string {
	lines := []string{
		"  soketi:",
		"    container_name: " + ContainerName,
		"    image: " + Image,
		"    restart: always",
		"    environment:",
		"      " + AppIDKey + ": ${" + AppIDKey + "}",
		"      " + AppKeyKey + ": ${" + AppKeyKey + "}",
		"      " + AppSecretKey + ": ${" + AppSecretKey + "}",
	}
	if production {
		lines = append(lines,
			"      SOKETI_PORT: '${SOKETI_PORT:-"+DefaultPort+"}'",
			"    network_mode: host",
		)
	} else {
		lines = append(lines,
			"      SOKETI_PORT: '"+DefaultPort+"'",
			"    ports:",
			"      - ${SOKETI_PUBLISH:-127.0.0.1:"+DefaultPort+":"+DefaultPort+"}",
			"    networks:",
			"      - franken_network",
		)
	}
	return append(lines,
		"    healthcheck:",
		`      test: ["CMD-SHELL", "wget -q -O /dev/null http://127.0.0.1:$${SOKETI_PORT}/ready"]`,
		"      interval: 10s",
		"      timeout: 5s",
		"      retries: 5",
		"      start_period: 10s",
		"",
	)
}
