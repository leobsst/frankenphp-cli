# Changelog

All notable changes to FrankenManager will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-02-10

### Added

#### Core Features
- Multi-site PHP development environment management using Docker and FrankenPHP
- Automatic HTTPS with locally-trusted certificates via mkcert
- Passwordless `/etc/hosts` management (after initial setup)
- Cross-platform support for macOS (arm64/x86_64), Linux (x86_64), and Windows (x86_64)
- Pre-built binaries requiring no Python installation

#### Commands
- `start` - Start the development server with optional domains and project path
  - Automatic domain registration in SQLite database
  - Merge registered domains with newly provided domains
  - Support for `--force-ssl` flag to regenerate certificates
- `stop` - Stop all running containers
- `restart` - Restart all or specific containers (caddy, database, cache, phpmyadmin)
  - Support for individual service restart with `--caddy`, `--db`, `--cache`, `--pma` flags
  - `--force-ssl` option for certificate regeneration during restart
- `status` - Display current server and container status with real-time Docker state
- `add-host` - Add new domains dynamically without full server restart
  - Multiple domain support
  - Optional `--force-ssl` flag
  - Automatic Caddyfile generation and container restart
  - Optional raw upstream target (e.g. `add-host domain.test 127.0.0.1:8006`) registers the domain(s) as proxy host(s) that reverse-proxy straight to that address instead of getting a PHP container of their own — `--php` is ignored in this mode
  - Dedicated SSL certificate and `/etc/hosts` entry per proxy host
  - Shown in their own table in `frankenmanager list`
- `add-alias` - Add alternate host(s) that reverse-proxy to an already-configured domain
  - No per-site Caddyfile or new PHP container: only the reverse proxy's Caddyfile is updated
  - Dedicated SSL certificate and `/etc/hosts` entry per alias
  - Follows the target domain's PHP version automatically, including after `switch-php`
  - Shown alongside registered domains in `frankenmanager list`
  - Multiple alias support and optional `--force-ssl` flag
- `remove-host` - Remove domains and archive their Caddyfiles
  - Multiple domain support
  - Caddyfiles moved to archive directory (not deleted)
  - Automatic `/etc/hosts` cleanup
  - Also removes alternate hosts (aliases) that target a removed domain
  - Can remove an alternate host (alias) directly by name, leaving its target untouched
  - Can remove a proxy host directly by name
- `restore-host` - Restore archived domains
  - `--list` option to view all archived hosts
  - Multiple domain support
  - Optional `--force-ssl` flag
- `reset` - Reset configuration and/or Caddyfiles to default state
  - `--db`/`--database` flag to clear domain registry
  - `--caddyfiles`/`--caddy` flag to delete custom Caddyfiles
  - Safety checks requiring stopped server
  - Confirmation prompt before execution
- `setup` - Configure passwordless operation and install dependencies
  - `--install-mkcert` option for automatic mkcert installation
  - `--status` option to check setup configuration
  - `--remove` option to uninstall privilege configuration
  - Sudoers file management for Linux/macOS
- `update` - Self-update functionality
  - `--check` option to check for updates without installing
  - `--force` option to force reinstall latest version
  - Automatic update notifications on command execution

#### Services
- **FrankenPHP** (webserver-and-caddy) - PHP 8.3 with Caddy web server
  - Brotli and Gzip compression
  - Static asset caching
  - Security headers (HSTS, X-Frame-Options, CSP, etc.)
- **MariaDB** (franken_mariadb) - Database server with health checks
  - Automatic password sync between containers
  - Configurable max packet size (default 512M)
- **Redis** (franken_redis) - Cache server
- **phpMyAdmin** (franken_phpmyadmin) - Web-based database management UI

#### Configuration Management
- SQLite database for server state and domain management
  - Real-time status checking
  - ACID-compliant transactions
  - Timestamp tracking for domains and status changes
  - Automatic migration from legacy JSON config
  - Dedicated `aliases` table for alternate hosts, auto-created on upgrade (no reset required)
  - Dedicated `proxies` table for raw upstream proxy hosts, auto-created on upgrade (no reset required)
- `.env` file generation with sensible defaults
  - Auto-generated MariaDB root password
  - Auto-filled UID/GID for file ownership
  - Configurable ports for all services
  - Environment mode (`dev`/`prod`)
  - Network exposure control
- Dedicated data directory structure
  - macOS/Linux: `~/.frankenmanager/`
  - Windows: `%LOCALAPPDATA%\frankenmanager\`
  - Override via `FRANKENMANAGER_DATA_DIR` environment variable

#### SSL/TLS Management
- Automatic SSL certificate generation via mkcert
- Certificate storage in dedicated certs directory
- Force SSL regeneration option across all commands
- Per-domain certificate management

#### Caddyfile Management
- Template-based Caddyfile generation
- Per-site configuration files
- Three-tier directory structure:
  - `custom/` - Active site configurations
  - `default/` - Default site configurations
  - `archive/` - Archived configurations from removed hosts
- Automatic Caddy reload on configuration changes
- Alternate host (alias) support in the reverse proxy Caddyfile: dedicated TLS block per alias, proxied to its target domain's site with no separate per-site Caddyfile
- Proxy host support in the reverse proxy Caddyfile: dedicated TLS block per proxy host, forwarding straight to a raw upstream address with no separate per-site Caddyfile

#### Developer Experience
- Rich console output with color-coded messages
- Detailed error messages and troubleshooting hints
- Comprehensive help system for all commands
- Input validation for domain names and paths
- Platform-specific path handling

#### Production Features
- Production mode support (`APP_ENV=prod`)
  - Host networking configuration
  - PHP OPCache enabled
  - `expose_php` disabled
  - Output buffering (4096 bytes)
  - Let's Encrypt certificate support

#### Logging
- Caddy access logs with automatic rotation (10MB per file, 5 backups, 30 days retention)
- PHP error logs in `caddy/log/php/php_errors.log`
- Logrotate configuration for host-level log management
- Container-specific logs accessible via Docker

#### Package Management
- Python package with Typer CLI framework
- PyInstaller-based binary compilation
- Self-contained executables with embedded resources
- Version checking and display

### Technical Details
- Built with Python 3.9+
- Docker SDK for Python integration
- Rich library for terminal UI
- Typer for CLI framework
- SQLite for state management
- Cross-platform pathlib usage

### Documentation
- Comprehensive README with quick start guide
- Command reference table
- Multi-domain setup examples
- Architecture and directory structure documentation
- Troubleshooting guide
- Platform-specific installation instructions

[1.0.0]: https://github.com/leobsst/frankenphp-cli/releases/tag/v1.0.0
