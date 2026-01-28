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
- Cross-platform support (macOS, Linux, Windows)

## Prerequisites

- Python 3.9+
- Docker & Docker Compose v2
- [mkcert](https://github.com/FiloSottile/mkcert) (for local SSL certificates)

### macOS

```bash
brew install python mkcert
```

### Linux (Debian/Ubuntu)

```bash
sudo apt-get install -y python3 python3-pip python3-venv
# Install mkcert: https://github.com/FiloSottile/mkcert#installation
```

### Windows

```powershell
# Install Python from https://python.org
# Install mkcert from https://github.com/FiloSottile/mkcert#installation
```

## Installation

1. Clone the repository:

```bash
git clone https://github.com/LEOBSST/frankenphp-cli.git
cd frankenphp-cli
```

2. Create a virtual environment and install the CLI:

```bash
python3 -m venv .venv
source .venv/bin/activate  # On Windows: .venv\Scripts\activate
pip install -e .
```

3. Copy and configure the environment file:

```bash
cp .env.example .env
```

4. Edit `.env` and set your system user and group:

```env
APP_ENV=dev
USER=your-username
GROUP=your-group
```

## Usage

> **Note:** Make sure to activate the virtual environment before using the CLI:
> ```bash
> source .venv/bin/activate  # On Windows: .venv\Scripts\activate
> ```

### Start the server

```bash
frankenphp start "domain1.test domain2.test" /path/to/projects
```

- **domains**: Space-separated list of local domains (quoted)
- **path**: Path to the directory containing your project folders

### Multi-Domain Setup

FrankenPHP CLI allows you to serve multiple PHP/Laravel projects simultaneously, each with its own local domain. The domain name determines which project folder is served.

**How it works:**

1. The domain name (without TLD) maps to a subdirectory under your projects path
2. Each project must have a `public/` directory (Laravel standard)
3. SSL certificates are automatically generated for each domain
4. All domains are added to `/etc/hosts` pointing to `127.0.0.1`

**Example:**

```bash
frankenphp start "shop.test blog.test api.test" /home/dev/projects
```

This creates the following mapping:

| Domain | Project Path |
|--------|--------------|
| `https://shop.test` | `/home/dev/projects/shop/public/` |
| `https://blog.test` | `/home/dev/projects/blog/public/` |
| `https://api.test` | `/home/dev/projects/api/public/` |

**Directory structure required:**

```
/home/dev/projects/
├── shop/
│   └── public/
│       └── index.php
├── blog/
│   └── public/
│       └── index.php
└── api/
    └── public/
        └── index.php
```

**What happens on start:**

1. Validates all domain names format (e.g., `myapp.test`)
2. Generates SSL certificates for each domain via mkcert
3. Adds entries to `/etc/hosts` (requires sudo)
4. Creates Caddyfile configuration for each domain
5. Builds and starts Docker containers
6. Syncs MariaDB password if changed

**Single domain:**

```bash
frankenphp start "myapp.test" /home/dev/projects
```

**Multiple domains (same projects path):**

```bash
frankenphp start "app1.test app2.test app3.test" /home/dev/projects
```

### Stop the server

```bash
frankenphp stop
```

### Restart containers

```bash
frankenphp restart
```

### Check status

```bash
frankenphp status
```

### Force SSL certificate regeneration

```bash
frankenphp start "domain.test" /path/to/projects --force-ssl
```

### Show version

```bash
frankenphp --version
```

### Show help

```bash
frankenphp --help
frankenphp start --help
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
frankenphp-cli/
├── pyproject.toml              Python package configuration
├── src/frankenphp_cli/         CLI package
│   ├── cli.py                  Main CLI (Typer app)
│   ├── exceptions.py           Custom exceptions
│   ├── commands/               Command implementations
│   │   ├── start.py            Start server
│   │   ├── stop.py             Stop server
│   │   ├── restart.py          Restart containers
│   │   └── status.py           Show status
│   ├── core/                   Core functionality
│   │   ├── config.py           JSON config management
│   │   ├── environment.py      .env file handling
│   │   ├── docker_manager.py   Docker SDK integration
│   │   ├── ssl_manager.py      mkcert wrapper
│   │   ├── hosts_manager.py    /etc/hosts management
│   │   ├── caddyfile.py        Caddyfile generation
│   │   └── password_manager.py MariaDB password sync
│   └── utils/                  Utilities
│       ├── platform.py         Cross-platform detection
│       ├── logging.py          Rich console output
│       └── validation.py       Input validation
├── tests/                      Test suite
├── Dockerfile                  Custom FrankenPHP image
├── docker-compose.yml          Development setup
├── docker-compose-prod.yml     Production setup
├── php/
│   ├── php.ini                 Base PHP configuration
│   └── php-prod.ini            Production PHP overrides
├── caddy/
│   ├── Caddyfile               Main Caddy configuration
│   ├── Caddyfile.template      Per-site template
│   ├── sites/custom/           Generated site configs
│   ├── certs/                  SSL certificates
│   └── log/                    Access and error logs
├── config/
│   └── logrotate-php           PHP log rotation config
└── database/                   MariaDB data (gitignored)
```

## Services

| Service | Container | Port | Description |
|---------|-----------|------|-------------|
| FrankenPHP | webserver-and-caddy | 80, 443 | Web server (host network) |
| MariaDB | franken_mariadb | 3306 | Database |
| Redis | franken_redis | 6379 | Cache |
| phpMyAdmin | franken_phpmyadmin | 8080 | Database UI |

## Development

### Install development dependencies

```bash
pip install -e ".[dev]"
```

### Run tests

```bash
pytest
```

### Type checking

```bash
mypy src/
```

### Linting

```bash
ruff check src/
```

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
frankenphp stop
```

### Admin password prompt

The CLI runs as your local user but will prompt for `sudo` when modifying `/etc/hosts` or installing the mkcert root CA. This is expected.

### SSL certificate issues

Force regeneration:

```bash
frankenphp start "domain.test" /path --force-ssl
```

### Container health check failures

Check container logs:

```bash
docker logs webserver-and-caddy
docker logs franken_mariadb
```

### Module not found errors

Make sure the virtual environment is activated:

```bash
source .venv/bin/activate
```

## License

MIT
