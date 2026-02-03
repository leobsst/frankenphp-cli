# FrankenManager

[![Python](https://img.shields.io/badge/Python-3.9+-3776AB?style=flat-square&logo=python&logoColor=white)](https://python.org)
[![PHP](https://img.shields.io/badge/PHP-8.3-777BB4?style=flat-square&logo=php&logoColor=white)](https://php.net)
[![Docker](https://img.shields.io/badge/Docker-Required-2496ED?style=flat-square&logo=docker&logoColor=white)](https://docker.com)
[![FrankenPHP](https://img.shields.io/badge/FrankenPHP-Caddy-00ADD8?style=flat-square&logo=go&logoColor=white)](https://frankenphp.dev)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-macOS%20|%20Linux%20|%20Windows-lightgrey?style=flat-square)]()

A powerful CLI tool for managing local PHP development environments using Docker containers with [FrankenPHP](https://frankenphp.dev) (Caddy + PHP 8.3).

**FrankenManager** simplifies the process of running multiple PHP projects locally with:
- Automatic HTTPS with locally-trusted certificates
- No manual `/etc/hosts` editing (passwordless after setup)
- One command to start/stop your entire development stack
- Pre-built binaries - no Python installation required

## Features

- Multi-site management with custom local domains (`.test`)
- Automated SSL certificate generation via mkcert
- FrankenPHP with Caddy web server
- MariaDB database with health checks
- Redis cache server
- phpMyAdmin for database management
- Automatic `/etc/hosts` management (passwordless after setup)
- Production and development configurations
- Brotli + Gzip compression
- Static asset caching
- Security headers (HSTS, X-Frame-Options, CSP, etc.)
- Cross-platform support (macOS, Linux, Windows)
- Pre-built binaries (no Python required)

## Installation

### Option 1: Pre-built Binaries (Recommended)

Download the latest binary for your platform from the [Releases](https://github.com/leobsst/frankenphp-cli/releases) page.

#### macOS (Apple Silicon)

```bash
curl -L https://github.com/leobsst/frankenphp-cli/releases/latest/download/frankenmanager-macos-arm64 -o /usr/local/bin/frankenmanager
chmod +x /usr/local/bin/frankenmanager
```

#### macOS (Intel)

```bash
curl -L https://github.com/leobsst/frankenphp-cli/releases/latest/download/frankenmanager-macos-x86_64 -o /usr/local/bin/frankenmanager
chmod +x /usr/local/bin/frankenmanager
```

#### Linux

```bash
curl -L https://github.com/leobsst/frankenphp-cli/releases/latest/download/frankenmanager-linux-x86_64 -o /usr/local/bin/frankenmanager
chmod +x /usr/local/bin/frankenmanager
```

#### Windows

Download `frankenmanager-windows-x86_64.exe` from the releases page and add it to your PATH.

### Option 2: From Source

```bash
git clone https://github.com/leobsst/frankenphp-cli.git
cd frankenphp-cli
python3 -m venv .venv
source .venv/bin/activate  # On Windows: .venv\Scripts\activate
pip install -e .
```

## First-Time Setup

After installation, run the setup command to configure passwordless privilege escalation and install dependencies:

```bash
# macOS/Linux
sudo frankenmanager setup --install-mkcert

# Windows (run in Administrator terminal)
frankenmanager setup --install-mkcert
```

This will:
- Configure passwordless `/etc/hosts` modifications (no more sudo prompts)
- Install mkcert for SSL certificate generation

Check the setup status:

```bash
frankenmanager setup --status
```

## Prerequisites

- Docker & Docker Compose v2
- mkcert (installed automatically with `--install-mkcert`)

## Usage

### Start the server

```bash
frankenmanager start "domain1.test domain2.test" /path/to/projects
```

- **domains**: Space-separated list of local domains (quoted)
- **path**: Path to the directory containing your project folders

### Multi-Domain Setup

FrankenManager allows you to serve multiple PHP/Laravel projects simultaneously, each with its own local domain. The domain name determines which project folder is served.

**How it works:**

1. The domain name (without TLD) maps to a subdirectory under your projects path
2. Each project must have a `public/` directory (Laravel standard)
3. SSL certificates are automatically generated for each domain
4. All domains are added to `/etc/hosts` pointing to `127.0.0.1`

**Example:**

```bash
frankenmanager start "shop.test blog.test api.test" /home/dev/projects
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
3. Adds entries to `/etc/hosts` (passwordless if setup was run)
4. Creates Caddyfile configuration for each domain
5. Builds and starts Docker containers
6. Syncs MariaDB password if changed

### Stop the server

```bash
frankenmanager stop
```

### Restart containers

```bash
frankenmanager restart
```

### Check status

```bash
frankenmanager status
```

### Force SSL certificate regeneration

```bash
frankenmanager start "domain.test" /path/to/projects --force-ssl
```

### Show version

```bash
frankenmanager --version
```

### Show help

```bash
frankenmanager --help
frankenmanager start --help
frankenmanager setup --help
```

### Update to latest version

FrankenManager can update itself when new releases are available:

```bash
# Update to latest version
frankenmanager update

# Check for updates without installing
frankenmanager update --check

# Force reinstall latest version
frankenmanager update --force
```

When you run any command, FrankenManager automatically checks for updates and notifies you if a new version is available.

## Configuration

### Data Directory

FrankenManager stores all configuration files, certificates, and generated data in a dedicated directory:

| Platform | Location |
|----------|----------|
| macOS/Linux | `~/.frankenmanager/` |
| Windows | `%LOCALAPPDATA%\frankenmanager\` |

You can override this location with the `FRANKENMANAGER_DATA_DIR` environment variable.

**Directory structure:**

```
~/.frankenmanager/
├── .config                 # Server state (running/stopped, domains)
├── .env                    # Environment configuration
├── docker-compose.yml      # Docker Compose configuration
├── Dockerfile              # Custom FrankenPHP image
├── caddy/
│   ├── Caddyfile           # Main Caddy configuration
│   ├── Caddyfile.template  # Per-site template
│   ├── sites/custom/       # Generated site configs
│   ├── certs/              # SSL certificates
│   ├── data/               # Caddy data
│   ├── config/             # Caddy config
│   └── log/                # Access and error logs
├── php/
│   ├── php.ini             # PHP configuration
│   └── php-prod.ini        # Production overrides
└── database/               # MariaDB data
```

Check your data directory location:

```bash
frankenmanager setup --status
```

### Environment Variables (.env)

The `.env` file is automatically created in the data directory on first run. Edit it to configure:

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_ENV` | `dev` | Environment (`dev` or `prod`) |
| `UID` | (auto-filled) | System user ID for file ownership |
| `GID` | (auto-filled) | System group ID for file ownership |
| `CERTS_DIR` | `./caddy/certs` | SSL certificates directory |
| `MARIADB_ROOT_PASSWORD` | (auto-generated) | MariaDB root password |
| `EXPOSE_SERVICES` | `false` | Expose services to the network (not just localhost) |
| `DB_PORT` | `3306` | MariaDB port |
| `PMA_PORT` | `8080` | phpMyAdmin port |
| `REDIS_PORT` | `6379` | Redis port |
| `WEB_HTTP_PORT` | `80` | Web server HTTP port |
| `WEB_HTTPS_PORT` | `443` | Web server HTTPS port |
| `MYSQL_MAX_ALLOWED_PACKET` | `512M` | MariaDB max packet size |

### Production Mode

Set `APP_ENV=prod` in `.env` to enable:

- Production Docker Compose configuration (host networking)
- PHP OPCache enabled
- `expose_php` disabled
- Output buffering (4096 bytes)
- Let's Encrypt certificates (instead of mkcert)

## Architecture

### Source Code Structure

```
frankenphp-cli/
├── pyproject.toml              Python package configuration
├── frankenmanager.spec         PyInstaller build spec
├── .env.example                Environment template
├── docker-compose.yml          Docker Compose (dev)
├── docker-compose-prod.yml     Docker Compose (prod)
├── Dockerfile                  Custom FrankenPHP image
├── caddy/                      Caddy configuration
├── php/                        PHP configuration
├── src/frankenmanager/         CLI package
│   ├── cli.py                  Main CLI (Typer app)
│   ├── exceptions.py           Custom exceptions
│   ├── commands/               Command implementations
│   │   ├── start.py            Start server
│   │   ├── stop.py             Stop server
│   │   ├── restart.py          Restart containers
│   │   ├── status.py           Show status
│   │   └── setup.py            Privilege & mkcert setup
│   ├── core/                   Core functionality
│   │   ├── config.py           JSON config management
│   │   ├── environment.py      .env file handling
│   │   ├── docker_manager.py   Docker SDK integration
│   │   ├── ssl_manager.py      mkcert wrapper
│   │   ├── hosts_manager.py    /etc/hosts management
│   │   ├── caddyfile.py        Caddyfile generation
│   │   ├── password_manager.py MariaDB password sync
│   │   ├── privilege_manager.py Passwordless sudo setup
│   │   ├── resources.py        Data directory management
│   │   └── updater.py          Self-update functionality
│   └── utils/                  Utilities
│       ├── platform.py         Cross-platform detection
│       ├── logging.py          Rich console output
│       └── validation.py       Input validation
├── scripts/
│   └── build.py                Binary build script
└── tests/                      Test suite
```

## Services

| Service | Container | Default Port | Description |
|---------|-----------|--------------|-------------|
| FrankenPHP | webserver-and-caddy | 80, 443 | Web server (host network) |
| MariaDB | franken_mariadb | 3306 | Database |
| Redis | franken_redis | 6379 | Cache |
| phpMyAdmin | franken_phpmyadmin | 8080 | Database UI |

All ports are configurable via the `.env` file. See [Environment Variables](#environment-variables-env) for details.

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

### Build binaries

```bash
pip install pyinstaller
python scripts/build.py --clean --test --release
```

## Platform Support

| Feature | macOS | Linux | Windows |
|---------|-------|-------|---------|
| Pre-built binary | arm64 + x86_64 | x86_64 | x86_64 |
| Passwordless hosts | sudoers helper | sudoers helper | Admin required |
| mkcert auto-install | Homebrew | Direct download | Chocolatey/Scoop |

## Troubleshooting

### "The server is already running"

Stop it first:

```bash
frankenmanager stop
```

### Admin password prompt (after setup)

If you still get password prompts after running `sudo frankenmanager setup`:

```bash
# Check setup status
frankenmanager setup --status

# Re-run setup if needed
sudo frankenmanager setup
```

### SSL certificate issues

Force regeneration:

```bash
frankenmanager start "domain.test" /path --force-ssl
```

### mkcert not found

Install it automatically:

```bash
sudo frankenmanager setup --install-mkcert
```

Or manually:

```bash
# macOS
brew install mkcert

# Linux (see https://github.com/FiloSottile/mkcert)

# Windows
choco install mkcert
```

### Container health check failures

Check container logs:

```bash
docker logs webserver-and-caddy
docker logs franken_mariadb
```

### Windows: "Access Denied" for hosts file

Run your terminal as Administrator, or configure hosts file permissions:

1. Right-click on `C:\Windows\System32\drivers\etc\hosts`
2. Go to Properties -> Security -> Edit
3. Add your user with "Modify" permission

## Log Rotation

### PHP Error Logs

PHP errors are logged to `~/.frankenmanager/caddy/log/php/php_errors.log`. To set up automatic rotation on the host:

```bash
sudo cp config/logrotate-php /etc/logrotate.d/frankenmanager-php
sudo sed -i "s|INSTALL_PATH|$HOME/.frankenmanager|g" /etc/logrotate.d/frankenmanager-php
```

### Caddy Access Logs

Caddy logs are automatically rotated (10MB per file, 5 backups, 30 days retention).

## Uninstall

To completely remove FrankenManager:

```bash
# Stop running containers
frankenmanager stop

# Remove privilege configuration
sudo frankenmanager setup --remove

# Remove the binary (if installed via binary)
sudo rm /usr/local/bin/frankenmanager

# Remove data directory
rm -rf ~/.frankenmanager

# Or on Windows:
# rmdir /s %LOCALAPPDATA%\frankenmanager
```

## License

MIT
