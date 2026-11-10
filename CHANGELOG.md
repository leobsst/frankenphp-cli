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
- Cross-platform support for macOS (arm64/x86_64), Linux (x86_64/arm64/armv7), and Windows (x86_64/arm64)
- A single executable per platform, with nothing else to install: commands start instantly

#### Commands
- `start` - Start the development server with optional domains and project path
  - Automatic domain registration in SQLite database
  - Merge registered domains with newly provided domains
  - Support for `--force-ssl` flag to regenerate certificates
- `stop` - Stop all running containers
- `restart` - Restart all or specific containers (caddy, database, cache, phpmyadmin, dns, soketi)
  - Support for individual service restart with `--caddy`, `--db`, `--cache`, `--pma`, `--dns`, `--soketi` flags
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
  - Refreshes the static files of the data directory right after installing the new version (as the real user under `sudo`)
  - Works on Windows too, from an Administrator terminal: the running `.exe` is moved aside and removed by the next update
- `trust-ca` - Share the local mkcert root CA over the LAN (off by default)
  - `on`/`off`/`status` actions, optional `--port` (default `9080`)
  - Serves only the public root certificate, never its private key
  - Lets other devices on the network trust locally-issued `.test` domains without an MDM/GPO push
- `exec` - Run a command with a domain's PHP version (e.g. `exec myapp.test php artisan migrate`)
  - Uses the domain's PHP image and `php.ini`; alternate hosts follow their target domain
  - A PHP version can be given instead of a domain (e.g. `exec 8.4 php -v`)
  - Runs from the current directory, wherever it is (mounted at the same path in a one-off container)
  - Always runs as the current user (UID/GID from `.env`, or `1000:1000`, on Windows)
  - Quoted or unquoted command: a single quoted argument goes through a shell (pipes, `&&`)
  - Interactive TTY when run from a terminal, exits with the command's exit code

#### Services
- **FrankenPHP** (webserver-and-caddy) - PHP 8.3 with Caddy web server
  - Brotli and Gzip compression
  - Static asset caching
  - Security headers (HSTS, X-Frame-Options, CSP, etc.)
- **MariaDB** (franken_mariadb) - Database server with health checks
  - Automatic password sync between containers
  - Configurable max packet size (default 512M)
- **Redis** (franken_redis) - Cache server
- **Soketi** (franken_soketi) - Optional Pusher-compatible WebSocket server (`SOKETI_ENABLED=true`)
  - Served over TLS by the reverse proxy at `wss://ws.frankenmanager.test` (certificate, hosts entry and LAN DNS record handled automatically)
  - Default app configured through `SOKETI_DEFAULT_APP_ID`, `SOKETI_DEFAULT_APP_KEY` and `SOKETI_DEFAULT_APP_SECRET` (generated on start when empty)
  - Reachable by the PHP apps at `127.0.0.1:SOKETI_PORT` (default `6001`), restartable with `restart --soketi`
- **phpMyAdmin** (franken_phpmyadmin) - Web-based database management UI
- **CoreDNS** (franken_dns) - Optional LAN DNS server (`DNS_SERVER=true`), so other devices on the network reach the `.test` domains without editing their hosts file
  - Resolves every registered domain, alternate host, proxy host and the Soketi host to this machine's LAN IP (or `DNS_IP`); every other query is forwarded to `DNS_UPSTREAM`
  - Listens on every network interface by default (`0.0.0.0:53`), `DNS_LISTEN_IP` and `DNS_PORT` to pin it
  - Records kept in sync on every domain change (add, remove, restore, alias, proxy host) without restarting the container, with a short TTL (30 s)
  - Follows LAN IP changes (e.g. Wi-Fi to Ethernet) while the server runs, unless `DNS_IP` is set
    - The watcher runs as a LaunchAgent on macOS and a systemd user unit on Linux: independent of the terminal, back after a reboot or login, relaunched if it crashes (detached background process elsewhere)
    - macOS: shown as "FrankenManager" in Login Items, through a minimal `FrankenManager.app` shipped in the bundle, instead of the name on the signing certificate
  - `start` and `status` list every LAN address the server answers on (e.g. Wi-Fi and Ethernet at once), to set as primary and secondary DNS servers on the other devices
  - `restart --dns` refreshes the records and applies the DNS settings changed in `.env`

#### Custom Docker Image
- Composer bundled in the custom FrankenPHP image
- Alpine-based image by default, roughly half the size of the Debian one; `PHP_IMAGE_DISTRO=trixie` switches back to Debian Trixie
- Optional `pdftk` (with its Java runtime) via `INSTALL_PDFTK=true`, on both distributions
- Optional extra Linux packages via `EXTRA_APK_PACKAGES` (Alpine) or `EXTRA_APT_PACKAGES` (Trixie) in `.env`, installed on top of the built-in ones
- Optional extra global Composer packages via `EXTRA_COMPOSER_PACKAGES` in `.env`, installed with `composer global require`
- All scoped per PHP version with `EXTRA_APK_PACKAGES_<version>` / `EXTRA_APT_PACKAGES_<version>` / `EXTRA_COMPOSER_PACKAGES_<version>` (e.g. `EXTRA_APT_PACKAGES_84`), merged with the version-agnostic ones
- Applied on the next image build (`frankenmanager start`, or when a new PHP version container is created)

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
- The local mkcert root CA is trusted inside PHP containers too, not just the host browser - outgoing PHP/curl calls to other local domains (e.g. cross-project API calls) validate correctly
- Share the root CA over the LAN via `trust-ca` so other devices can trust it without manual per-device setup

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
- Color-coded console output and tables, plain when the output is not a terminal
- Detailed error messages and troubleshooting hints
- Comprehensive help system for all commands
- `--verbose` global option: shows the `docker build` logs whenever a command builds a PHP image
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

#### Distribution
- Release archives for 7 targets (`frankenmanager-<platform>.tar.gz`), each holding a `frankenmanager/` directory with the executable (plus `FrankenManager.app` on macOS), bare executables, and `SHA256SUMS.txt`
- macOS executables and app signed with a Developer ID and notarized
- `install.ps1` for Windows: installs `frankenmanager.exe` in `%LOCALAPPDATA%\Programs\FrankenManager` without Administrator rights, adds it to the user `PATH`, and checks the archive against `SHA256SUMS.txt`
- `install.sh` for macOS and Linux: installs in `/usr/local/lib/frankenmanager` with a `/usr/local/bin/frankenmanager` link, picks the arm64 build on Apple Silicon even from a Rosetta shell, and checks the archive against `SHA256SUMS.txt`
- Version checking and display
- Static resource templates (Dockerfile, docker-compose files, Caddyfile templates, base php.ini) embedded in the executable and automatically resynced on update when the installed version changes, leaving generated/live files (Caddyfile, docker-compose-generated.yml, per-domain site configs) untouched

### Technical Details
- Written in Go, built without cgo: every target cross-compiles from one machine
- `cobra` for the CLI, `lipgloss` for the terminal output
- Docker driven through the `docker` CLI
- SQLite for state management (pure Go driver)

### Upgrading from 0.1.x
- `sudo frankenmanager update` from 0.1.x installs 1.0.0 by itself, at the same place. The data directory (`~/.frankenmanager`), privilege setup, hosts entries, certificates, PHP images and the LAN DNS watcher service are picked up as they are: nothing to redo.
- To go back, reinstall 0.1.63 (`VERSION=0.1.63 install.sh`): the data directory stays compatible both ways.
- What changes:
  - `list` and `status` show bare domain names (`app.test`) in their tables
  - A failed `start`, `add-host`, `add-alias` or `restore-host` prints its error once, cleans up, and exits with code 1
  - `restore-host --list` sorts the archived hosts by name
  - Windows: `setup` requires an Administrator terminal and edits the hosts file directly; the PowerShell helper script is removed
  - The Python package (wheel, sdist) is no longer published
  - Updating an install older than the bundle layout (a single file in `/usr/local/bin`) in place, the fallback on `docker-compose.yml` / `docker-compose-prod.yml` when the generated compose file is missing, and the migration of the legacy `.config` file are removed
- Fixed since 0.1.x:
  - The hosts helper script could not remove entries on Linux; `setup` replaces an outdated helper
  - Windows: checking whether the CA server or the DNS watcher runs no longer kills it, and the first `start` no longer fails when creating `.env`
  - `restart` generated certificates in `CERTS_DIR` instead of `CADDY_DIR/certs`
  - The site template ignored a custom `CADDY_DIR`; the built-in fallback site had doubled braces
  - The default `DNS_UPSTREAM` matches `.env.example` (`1.1.1.1 8.8.8.8`)
  - An invalid `--php` printed a traceback instead of an error
  - `switch-php` on an unknown domain exited with code 0
  - mkcert is installed automatically on Linux armv7 too
  - `start` wrote generated passwords and Soketi credentials to `.env` even when the server was already running
  - Linux: under `sudo`, mkcert could put its CA in root's home directory
  - A failed `add-host` or `restore-host` left its domains registered; `restore-host` also moves the site file back to the archive

### Documentation
- Comprehensive README with quick start guide
- Command reference table
- Multi-domain setup examples
- Architecture and directory structure documentation
- Troubleshooting guide
- Platform-specific installation instructions

[1.0.0]: https://github.com/leobsst/frankenphp-cli/releases/tag/v1.0.0
