# FrankenPHP CLI

A development and production server management tool for hosting Laravel and PHP applications using Docker containers with FrankenPHP (Caddy + PHP 8.3).

## Features

- Multi-site management with custom local domains (`.test`)
- Automated SSL certificate generation via mkcert
- FrankenPHP with Caddy web server
- MariaDB database with health checks
- Redis cache server
- phpMyAdmin for database management
- Automatic `/etc/hosts` management
- Production and development configurations
- Brotli + Gzip compression
- Static asset caching
- Security headers (HSTS, X-Frame-Options, CSP, etc.)

## Prerequisites

- Docker & Docker Compose v2
- [mkcert](https://github.com/FiloSottile/mkcert) (for local SSL certificates)
- jq (JSON processor)
- openssl
- Bash 4+

### macOS

```bash
brew install mkcert jq openssl
```

### Linux (Debian/Ubuntu)

```bash
sudo apt-get install -y jq openssl
# Install mkcert: https://github.com/FiloSottile/mkcert#installation
```

## Installation

1. Clone the repository:

```bash
git clone https://github.com/LEOBSST/frankenphp-cli.git
cd frankenphp-cli
```

2. Copy and configure the environment file:

```bash
cp .env.example .env
```

3. Edit `.env` and set your system user and group:

```env
APP_ENV=dev
USER=your-username
GROUP=your-group
```

## Usage

### Start the server

```bash
./server.sh start "domain1.test domain2.test" /path/to/projects
```

- **domains**: Space-separated list of local domains (quoted)
- **custom-path**: Path to the directory containing your project folders

Each domain maps to a subdirectory under the custom path. For example, `myapp.test` expects the project to be at `/path/to/projects/myapp/public/`.

### Stop the server

```bash
./server.sh stop
```

### Restart containers

```bash
./server.sh restart
```

### Check status

```bash
./server.sh status
```

### Force SSL certificate regeneration

```bash
./server.sh start "domain.test" /path/to/projects --force-ssl
```

## Configuration

### Environment Variables (.env)

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_ENV` | `dev` | Environment (`dev` or `prod`) |
| `USER` | - | System user for file ownership |
| `GROUP` | - | System group for file ownership |
| `CERTS_DIR` | `./caddy/certs` | SSL certificates directory |
| `MARIADB_ROOT_PASSWORD` | (auto-generated) | MariaDB root password |
| `EXPOSE_SERVICES` | `false` | Expose DB/Redis to the network (not just localhost) |

### Production Mode

Set `APP_ENV=prod` in `.env` to enable:

- Production Docker Compose configuration (host networking)
- PHP OPCache enabled
- `expose_php` disabled
- Output buffering (4096 bytes)
- Let's Encrypt certificates (instead of mkcert)

## Architecture

```
server.sh                      Entry point and dispatcher
actions/
  start.sh                     Server start logic
  stop.sh                      Server stop logic
  status.sh                    Server status and restart
utils.sh                       Shared utilities and helpers
generate_ssl.sh                SSL certificate generation
manage_hosts.sh                /etc/hosts management
generate_caddyfile.sh          Per-site Caddy config generation
check_files.sh                 Dependency validation
check_config.sh                JSON config validation
restart_server.sh              Container restart
Dockerfile                     Custom FrankenPHP image
docker-compose.yml             Development setup
docker-compose-prod.yml        Production setup
php/
  php.ini                      Base PHP configuration
  php-prod.ini                 Production PHP overrides
caddy/
  Caddyfile                    Main Caddy configuration
  Caddyfile.template           Per-site template
  sites/custom/                Generated site configs
  certs/                       SSL certificates
  log/                         Access and error logs
config/
  logrotate-php                PHP log rotation config
database/                      MariaDB data (gitignored)
```

## Services

| Service | Container | Port | Description |
|---------|-----------|------|-------------|
| FrankenPHP | webserver-and-caddy | 80, 443 | Web server (host network) |
| MariaDB | franken_mariadb | 3306 | Database |
| Redis | franken_redis | 6379 | Cache |
| phpMyAdmin | franken_phpmyadmin | 8080 | Database UI |

## Log Rotation

### PHP Error Logs

PHP errors are logged to `caddy/log/php/php_errors.log`. To set up automatic rotation on the host:

```bash
sudo cp config/logrotate-php /etc/logrotate.d/frankenphp-php
sudo sed -i "s|INSTALL_PATH|$(pwd)|g" /etc/logrotate.d/frankenphp-php
```

### Caddy Access Logs

Caddy logs are automatically rotated (10MB per file, 5 backups, 30 days retention).

## Troubleshooting

### "The server is already running"

Stop it first:

```bash
./server.sh stop
```

### Admin password prompt

The script runs as your local user but will prompt for `sudo` when modifying `/etc/hosts` or installing the mkcert root CA. This is expected.

### SSL certificate issues

Force regeneration:

```bash
./server.sh start "domain.test" /path --force-ssl
```

### Container health check failures

Check container logs:

```bash
docker logs webserver-and-caddy
docker logs franken_mariadb
```

## License

MIT
