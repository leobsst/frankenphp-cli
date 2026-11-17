// Package assets embeds the resources FrankenManager extracts into its data
// directory (Dockerfile, compose templates, Caddy templates, php.ini...).
//
// It lives at the root of the repository because go:embed cannot reach
// parent directories: the files stay where they are in the repository.
package assets

import "embed"

// FS holds the embedded resources, at the same relative paths as in the data
// directory.
//
//go:embed docker-compose.yml docker-compose-prod.yml Dockerfile
//go:embed docker/entrypoint.sh
//go:embed caddy/Caddyfile caddy/Caddyfile.worker caddy/Caddyfile.template
//go:embed php/php.ini php/php-prod.ini
var FS embed.FS

// GatewayFS holds the sources of the S3 ACL gateway and its Dockerfile,
// from which the gateway image is built (see s3.WriteGatewayContext).
//
//go:embed docker/s3-gateway.Dockerfile cmd/s3gateway internal/sigv4
var GatewayFS embed.FS

// Files lists every embedded resource, in a fixed order.
var Files = []string{
	"docker-compose.yml",
	"docker-compose-prod.yml",
	"Dockerfile",
	"docker/entrypoint.sh",
	"caddy/Caddyfile",
	"caddy/Caddyfile.worker",
	"caddy/Caddyfile.template",
	"php/php.ini",
	"php/php-prod.ini",
}

// StaticFiles lists the resources rewritten on every version change: never
// edited by the CLI at runtime, unlike caddy/Caddyfile (regenerated) or the
// per-version php.ini copies. Mirrors resources.STATIC_RESOURCE_FILES.
var StaticFiles = []string{
	"docker-compose.yml",
	"docker-compose-prod.yml",
	"Dockerfile",
	"docker/entrypoint.sh",
	"caddy/Caddyfile.worker",
	"caddy/Caddyfile.template",
	"php/php.ini",
	"php/php-prod.ini",
}
