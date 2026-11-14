# FrankenManager

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![PHP](https://img.shields.io/badge/PHP-8.2%20|%208.3%20|%208.4%20|%208.5-777BB4?style=flat-square&logo=php&logoColor=white)](https://php.net)
[![Docker](https://img.shields.io/badge/Docker-Required-2496ED?style=flat-square&logo=docker&logoColor=white)](https://docker.com)
[![FrankenPHP](https://img.shields.io/badge/FrankenPHP-Caddy-00ADD8?style=flat-square&logo=go&logoColor=white)](https://frankenphp.dev)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-macOS%20|%20Linux%20|%20Windows-lightgrey?style=flat-square)]()

✨ A powerful CLI tool for managing local PHP development environments using Docker containers with [FrankenPHP](https://frankenphp.dev) (Caddy + PHP 8.2 / 8.3 / 8.4 / 8.5).

**FrankenManager** simplifies the process of running multiple PHP projects locally with:
- Automatic HTTPS with locally-trusted certificates
- No manual `/etc/hosts` editing (passwordless after setup)
- One command to start/stop your entire development stack
- Multiple PHP versions running side-by-side, switchable per domain
- A single executable, nothing else to install

## Features

- Multi-site management with custom local domains (`.test`)
- **Multiple PHP versions** (8.2, 8.3, 8.4, 8.5) running simultaneously, per-domain
- Live PHP version switching per domain (`switch-php`)
- Alternate hosts (aliases) that reverse-proxy to an existing domain's site (`add-alias`), with no extra Caddyfile or container
- Proxy hosts that reverse-proxy straight to a raw upstream address (`add-host <domain> <target>`), with no PHP container or per-site Caddyfile
- Automated SSL certificate generation via mkcert
- Optional extra apt/Composer packages in the PHP image, scoped per PHP version
- FrankenPHP with Caddy reverse proxy
- MariaDB, MySQL and/or PostgreSQL databases with health checks
- Configuration stored in SQLite, changed with `frankenmanager configuration set` (with shell completion), applied live when possible
- Optional Redis cache server, turned on and off live (`service enable redis`)
- Optional Soketi WebSocket server (`service enable soketi`), served over TLS at `wss://ws.frankenmanager.test`
- Short `fkm` alias of the command (configurable, `frankenmanager` always works)
- phpMyAdmin / pgAdmin for database management, served at `https://localhost`
- Automatic `/etc/hosts` management (passwordless after setup)
- Optional LAN DNS server so other devices reach your domains without editing their hosts file
- Production and development configurations
- Brotli + Gzip compression
- Static asset caching
- Security headers (HSTS, X-Frame-Options, etc.)
- WebSocket support (pre-configured, opt-in)
- Cross-platform support (macOS, Linux, Windows)
- A single executable for macOS, Linux and Windows (arm64 and x86_64, plus Linux armv7)

## Installation

### Option 1: Install script (Recommended)

The install script downloads the latest release for your platform, checks it against the published `SHA256SUMS.txt`, and installs it:

```bash
curl -fsSL https://raw.githubusercontent.com/leobsst/frankenphp-cli/refs/heads/main/install.sh | bash
```

It installs the bundle (the `frankenmanager` executable, plus `FrankenManager.app` on macOS) in `/usr/local/lib/frankenmanager` and links it as `/usr/local/bin/frankenmanager`, plus the short alias `/usr/local/bin/fkm` (see [CLI alias](#cli-alias)). Set `VERSION=x.y.z` to pin a version. On Apple Silicon, it picks the arm64 build even from a Rosetta shell.

### Option 2: Manual install

Download the archive for your platform from the [Releases](https://github.com/leobsst/frankenphp-cli/releases) page:

| Platform | Architecture | Archive |
|----------|--------------|---------|
| macOS | Apple Silicon | `frankenmanager-macos-arm64.tar.gz` |
| macOS | Intel | `frankenmanager-macos-x86_64.tar.gz` |
| Linux | x86_64 | `frankenmanager-linux-x86_64.tar.gz` |
| Linux | arm64 | `frankenmanager-linux-arm64.tar.gz` |
| Linux | armv7 | `frankenmanager-linux-armv7.tar.gz` |
| Windows | x86_64 | `frankenmanager-windows-x86_64.tar.gz` |
| Windows | arm64 | `frankenmanager-windows-arm64.tar.gz` |

e.g. on macOS (Apple Silicon):

```bash
curl -fsSL https://github.com/leobsst/frankenphp-cli/releases/latest/download/frankenmanager-macos-arm64.tar.gz | tar -xz -C /tmp
sudo mkdir -p /usr/local/lib
sudo mv /tmp/frankenmanager /usr/local/lib/frankenmanager
sudo ln -sfn /usr/local/lib/frankenmanager/frankenmanager /usr/local/bin/frankenmanager
```

On macOS, keep the extracted `frankenmanager/` directory whole: the DNS watcher service runs through the `FrankenManager.app` next to the executable. Elsewhere, the executable alone is enough.

#### Windows

In PowerShell:

```powershell
irm https://raw.githubusercontent.com/leobsst/frankenphp-cli/refs/heads/main/install.ps1 | iex
```

It installs `frankenmanager.exe` in `%LOCALAPPDATA%\Programs\FrankenManager` (no Administrator rights needed), adds that directory to your user PATH, and checks the archive against `SHA256SUMS.txt`. Set `$env:VERSION = "x.y.z"` first to pin a version.

To install manually, extract `frankenmanager.exe` from `frankenmanager-windows-x86_64.tar.gz` (or `-arm64`), or download the bare `frankenmanager-windows-x86_64.exe`, into a directory of your PATH.

Either way, run FrankenManager from an Administrator terminal whenever it changes the hosts file.

### Option 3: From Source

Requires [Go](https://go.dev) 1.26 or later:

```bash
git clone https://github.com/leobsst/frankenphp-cli.git
cd frankenphp-cli
make build            # bin/frankenmanager
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

## Quick Command Reference

| Command | Description |
|---------|-------------|
| `frankenmanager start [domains] [--path <path>] [--php <version>] [--build]` | Start the development server with optional domains |
| `frankenmanager stop` | Stop the development server |
| `frankenmanager restart [options] [--build]` | Restart all or specific containers |
| `frankenmanager status` | Show current server and container status |
| `frankenmanager list` | List all registered domains with their PHP versions |
| `frankenmanager add-host <domains> [--php <version>]` | Add new domains while server is running |
| `frankenmanager add-host <domain> <target>` | Add domain(s) that reverse-proxy to a raw upstream address (e.g. `127.0.0.1:8006`) |
| `frankenmanager add-alias <target> <aliases>` | Add alternate host(s) that reverse-proxy to an existing domain |
| `frankenmanager remove-host <domains>` | Remove domains (or alternate hosts) while server is running |
| `frankenmanager restore-host [domains] [--php <version>]` | Restore archived domains |
| `frankenmanager switch-php <domain> --php <version>` | Switch the PHP version for a domain (live, no downtime for other domains) |
| `frankenmanager exec <domain\|version> <command>` | Run a command with a PHP version (or the domain's), from the current directory |
| `frankenmanager reset --db \| --caddyfiles` | Reset configuration and/or Caddyfiles (requires stopped server) |
| `frankenmanager setup` | Configure passwordless operation |
| `frankenmanager update` | Update FrankenManager to latest version |
| `frankenmanager restart --dns` | Restart the LAN DNS server (when `DNS_SERVER=true`) |
| `frankenmanager configuration [list\|get\|set\|reset]` | Show or change the configuration (see [Settings](#settings)) |
| `frankenmanager service enable\|disable\|status [dns\|redis\|soketi]` | Turn an optional service on or off, live |
| `frankenmanager completion bash\|zsh\|fish\|powershell` | Generate the shell completion script |
| `frankenmanager trust-ca on \| off \| status [--port <port>]` | Share the local mkcert root CA over the LAN so other devices can trust your `.test` domains (off by default) |
| `frankenmanager --help` | Show help for all commands |

## Usage

### Start the server

```bash
# Start with specific domains (default PHP version: DEFAULT_PHP_VERSION)
frankenmanager start "domain1.test domain2.test" /path/to/projects

# Start with a specific PHP version
frankenmanager start "myapp.test" --php 8.4

# Start with registered domains from database (after first run)
frankenmanager start

# Start with new domains + registered domains (merges both)
frankenmanager start "newdomain.test"

# Force a rebuild of the PHP images
frankenmanager start --build
```

- **domains** (optional): Space-separated list of local domains (quoted)
  - If omitted, uses domains registered in the database from previous runs
  - If omitted on a fresh install (nothing registered yet), the server starts without any PHP container: only the reverse proxy, the databases and their admin UIs run. Add hosts afterwards with `add-host`, e.g. proxy hosts (`add-host "app.test" 127.0.0.1:3000`) that never need PHP
  - If provided, merges with registered domains (adds new ones to existing)
- **--path** (optional): Path to the directory containing your project folders
  - Uses the `DEFAULT_PROJECT_PATH` setting if not specified
  - Only required when at least one PHP domain is served
- **--php** (optional): PHP version for the new domains (`8.2`, `8.3`, `8.4`, `8.5`)
  - Defaults to the `DEFAULT_PHP_VERSION` setting (`8.3`)
  - PHP 8.2 is deprecated: it still works, but a warning is shown
- **--build** (optional): Rebuild the PHP images even if they are up to date

PHP images are not rebuilt on every start: an image is only built when it is missing or when one of its inputs changed (`Dockerfile`, entrypoint, `PHP_IMAGE_DISTRO`, `EXTRA_*_PACKAGES`, `INSTALL_PDFTK`, project path). Use `--build` to force a rebuild, e.g. to pull the latest FrankenPHP base image.

### Multi-Domain Setup

FrankenManager allows you to serve multiple PHP/Laravel projects simultaneously, each with its own local domain. The domain name determines which project folder is served.

**How it works:**

1. The domain name (without TLD) maps to a subdirectory under your projects path
2. Each project must have a `public/` directory (Laravel standard)
3. SSL certificates are automatically generated for each domain
4. All domains are added to `/etc/hosts` pointing to `127.0.0.1`

**Example 1: Initial start with domains**

```bash
frankenmanager start "shop.test blog.test api.test" /home/dev/projects
```

This creates the following mapping and saves the domains to the database:

| Domain | Project Path |
|--------|--------------|
| `https://shop.test` | `/home/dev/projects/shop/public/` |
| `https://blog.test` | `/home/dev/projects/blog/public/` |
| `https://api.test` | `/home/dev/projects/api/public/` |

**Example 2: Restart with registered domains**

After stopping the server, you can restart with the same domains without specifying them:

```bash
frankenmanager start
# Uses: shop.test, blog.test, api.test (from database)
```

**Example 3: Add new domains while keeping existing ones**

```bash
frankenmanager start "admin.test"
# Result: shop.test, blog.test, api.test, admin.test (merged)
```

The server automatically:
- Loads registered domains from the database
- Merges with newly provided domains
- Removes duplicates
- Updates the database with the complete list

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
4. Creates Caddyfile configuration for each domain (HTTP-only worker sites; TLS is handled by the reverse proxy)
5. Builds the PHP images that are missing or outdated (all of them with `--build`), then starts the Docker containers
6. Syncs MariaDB password if changed

### Multi-PHP Version Support

FrankenManager can run multiple PHP versions simultaneously, each in its own FrankenPHP container. Every domain is assigned a PHP version; the Caddy reverse proxy routes traffic to the correct container automatically.

**Supported versions:** 8.2 (deprecated), 8.3, 8.4, 8.5

> PHP 8.2 is deprecated and will be removed in a future release. Using it, through `--php 8.2` or `DEFAULT_PHP_VERSION=8.2`, prints a warning.

#### Assign a PHP version when starting

```bash
# Start domains on PHP 8.4
frankenmanager start "api.test shop.test" --php 8.4

# Start another domain on PHP 8.2 (both versions run side-by-side)
frankenmanager start "legacy.test" --php 8.2
```

#### List domains and their PHP versions

```bash
frankenmanager list
```

#### Switch a domain to a different PHP version

The `switch-php` command moves a domain to another PHP version live — only the affected container is restarted, other domains are not interrupted.

```bash
frankenmanager switch-php "myapp.test" --php 8.5
```

What happens:
1. Moves the domain's Caddyfile to the target version directory
2. Starts a new container for the target version (if not already running)
3. Restarts only the impacted containers
4. Stops any container that no longer serves any domain
5. Updates the reverse proxy configuration

#### Run a command with a domain's PHP version

The `exec` command runs a command with a PHP version (and its extensions, `php.ini`, Composer...), in a one-off container of that version's PHP image. The version is given directly (`8.4`) or taken from a domain. It runs from your current directory, wherever it is: the directory is mounted at the same path in the container, along with the projects root. The container uses the host network, like the PHP containers, so databases and other domains are reachable.

Commands always run as the current user, so created files (`vendor/`, caches...) belong to you. On Windows, which has no Unix user IDs, the `UID`/`GID` settings are used (`1000:1000` if unset).

```bash
cd ~/projects/myapp
frankenmanager exec myapp.test php -v
frankenmanager exec myapp.test composer install

# Or a PHP version directly
frankenmanager exec 8.4 php -v
frankenmanager exec 8.2 composer update
frankenmanager exec myapp.test php artisan migrate --force

# Quoted: run through a shell, so pipes and && work
frankenmanager exec myapp.test "php artisan config:clear && php artisan cache:clear"
```

- Everything after the domain is passed to the command as-is (options such as `-v` or `--help` included).
- Alternate hosts (aliases) use their target domain's PHP version; proxy hosts have none.
- The server doesn't need to be running, but the PHP image of the version must have been built (`start` / `add-host`).
- A TTY is allocated when run from a terminal (e.g. `php artisan tinker`), and `exec` exits with the command's exit code.

#### Add a host to a specific PHP version

```bash
frankenmanager add-host "newapp.test" --php 8.4
```

#### Default PHP version

Run `frankenmanager configuration set DEFAULT_PHP_VERSION 8.4` to change the default for all commands that accept `--php`. Commands resolve the version in this order: `--php` flag → `DEFAULT_PHP_VERSION` setting → `8.3`.

#### Architecture

Each PHP version gets its own container (`frankenphp-8.2`, `frankenphp-8.3`, etc.) listening on a dedicated internal port. A single Caddy reverse proxy container handles TLS and routes each domain to its version container over plain HTTP.

| PHP Version | Container | Internal HTTP Port |
|-------------|-----------|-------------------|
| 8.2 | `frankenphp-8.2` | 8280 |
| 8.3 | `frankenphp-8.3` | 8380 |
| 8.4 | `frankenphp-8.4` | 8480 |
| 8.5 | `frankenphp-8.5` | 8580 |

Per-site Caddyfiles are stored in version-specific subdirectories:

```
~/.frankenmanager/caddy/sites/
├── php-8.2/
│   └── old_Caddyfile
├── php-8.3/
│   ├── shop_Caddyfile
│   └── blog_Caddyfile
├── php-8.4/
│   └── api_Caddyfile
└── archive/            # Caddyfiles of removed domains (see restore-host)
```

### Stop the server

```bash
frankenmanager stop
```

### Restart containers

Restart all containers or specific services:

```bash
# Restart all containers
frankenmanager restart

# Restart only the web server / Caddy
frankenmanager restart --caddy

# Restart only the database
frankenmanager restart --database
# or use the short alias
frankenmanager restart --db

# Restart only the Redis cache (when REDIS_ENABLED=true)
frankenmanager restart --cache

# Restart only phpMyAdmin
frankenmanager restart --phpmyadmin
# or use the short alias
frankenmanager restart --pma

# Restart only the LAN DNS server (when DNS_SERVER=true)
frankenmanager restart --dns

# Restart only the Soketi WebSocket server (when SOKETI_ENABLED=true)
frankenmanager restart --soketi

# Restart multiple specific containers
frankenmanager restart --cache --pma

# Restart with forced SSL regeneration (for Caddy)
frankenmanager restart --caddy --force-ssl

# Rebuild the PHP images, recreate their containers, then restart
frankenmanager restart --build
```

**Available container options:**

| Option | Alias | Container | Description |
|--------|-------|-----------|-------------|
| `--caddy` | - | webserver-and-caddy | Web server with Caddy and FrankenPHP |
| `--database` | `--db` | franken_mariadb | MariaDB database server |
| `--cache` | - | franken_redis | Redis cache server (when `REDIS_ENABLED=true`) |
| `--phpmyadmin` | `--pma` | franken_phpmyadmin | phpMyAdmin database UI |
| `--dns` | - | franken_dns | LAN DNS server (when `DNS_SERVER=true`) |
| `--soketi` | `--ws` | franken_soketi | Soketi WebSocket server (when `SOKETI_ENABLED=true`) |

**Notes:**
- When restarting `--caddy`, SSL certificates are regenerated (use `--force-ssl` to force full regeneration)
- When restarting `--database`, the MariaDB password is automatically synced
- When the LAN DNS server is enabled, `restart` (and `restart --dns`) refreshes its records and applies the DNS settings changed since the last start
- Multiple containers can be restarted simultaneously by combining options
- `restart` never rebuilds images unless `--build` is given

### Check status

```bash
frankenmanager status
```

### Dynamic Host Management

You can add, remove, and restore hosts while the server is running without restarting the entire stack.

#### Add a new host

```bash
# Add a single domain (default PHP version)
frankenmanager add-host "newapp.test"

# Add a domain on a specific PHP version
frankenmanager add-host "newapp.test" --php 8.4

# Add multiple domains
frankenmanager add-host "app1.test app2.test"

# Force SSL certificate regeneration
frankenmanager add-host "newapp.test" --force-ssl
```

This will:
1. Validate the domain name(s)
2. Generate SSL certificates
3. Add entries to `/etc/hosts`
4. Generate Caddyfile configuration(s)
5. Restart only the FrankenPHP container (other services remain untouched)

#### Add a proxy host

Give `add-host` a second argument — a raw upstream address — to register a domain that reverse-proxies straight to it instead of getting its own PHP container. This is useful for fronting another local service (e.g. something running directly on the host, or in an unrelated container) with a `.test` domain and HTTPS.

```bash
# Proxy to a bare host:port
frankenmanager add-host "proxy.test" 127.0.0.1:8006

# Proxy with an explicit scheme
frankenmanager add-host "proxy.test" http://127.0.0.1:8006

# Force SSL certificate regeneration
frankenmanager add-host "proxy.test" 127.0.0.1:8006 --force-ssl
```

This will:
1. Validate the domain name(s) and the upstream target
2. Generate SSL certificates
3. Add entries to `/etc/hosts`
4. Add the domain to the reverse proxy Caddyfile (no per-site Caddyfile, no new PHP container)
5. Restart only the reverse proxy

`--php` is ignored when a target is given — proxy hosts have no PHP container of their own. Proxy hosts are shown in their own table in `frankenmanager list`.

#### Add an alternate host (alias)

`add-alias` attaches one or more extra domains to a host that's already configured with `add-host`/`start`. Unlike `add-host`, it does **not** create a per-site Caddyfile or a new PHP container: the alias gets its own SSL certificate and `/etc/hosts` entry, and is added to the reverse proxy's Caddyfile only, forwarding to the target domain's existing site.

```bash
# Add a single alternate host for an existing domain
frankenmanager add-alias "myapp.test" "myapp-alt.test"

# Add multiple alternate hosts at once
frankenmanager add-alias "myapp.test" "alt1.test alt2.test"

# Force SSL certificate regeneration
frankenmanager add-alias "myapp.test" "myapp-alt.test" --force-ssl
```

This will:
1. Validate the target domain is already configured
2. Generate SSL certificates for the alias domain(s)
3. Add entries to `/etc/hosts`
4. Add the alias to the reverse proxy Caddyfile (no per-site Caddyfile, no new PHP container)
5. Restart only the reverse proxy

An alias always follows its target's PHP version — including if the target is later moved with `switch-php`. Aliases are shown alongside registered domains in `frankenmanager list`.

#### Remove a host

```bash
# Remove a single domain
frankenmanager remove-host "oldapp.test"

# Remove multiple domains
frankenmanager remove-host "app1.test app2.test"

# Remove an alternate host (alias) only, leaving the target untouched
frankenmanager remove-host "myapp-alt.test"

# Remove a proxy host
frankenmanager remove-host "proxy.test"
```

This will:
1. Remove entries from `/etc/hosts`
2. Archive the Caddyfile to `caddy/sites/archive/` (not deleted) — skipped for aliases and proxy hosts, which have no Caddyfile of their own
3. Restart only the FrankenPHP container

Removing a domain also removes any alternate hosts (aliases) pointing to it, since they'd otherwise be left proxying to a domain that no longer exists.

#### Restore an archived host

```bash
# List all archived hosts
frankenmanager restore-host --list

# Restore a single domain
frankenmanager restore-host "myapp.test"

# Restore on a specific PHP version
frankenmanager restore-host "myapp.test" --php 8.4

# Restore multiple domains with SSL regeneration
frankenmanager restore-host "app1.test app2.test" --force-ssl
```

This will:
1. Move the Caddyfile from `archive/` back to `custom/`
2. Generate SSL certificates
3. Add entries to `/etc/hosts`
4. Restart only the FrankenPHP container

### Caddyfile Customization

Each domain gets its own Caddyfile generated from `caddy/Caddyfile.template`. You can edit a site's Caddyfile directly at `~/.frankenmanager/caddy/sites/php-<version>/<domain>_Caddyfile` to customize behavior for that site.

If the custom template at `caddy/Caddyfile.template` is unreadable, FrankenManager will automatically fall back to its built-in template.

#### WebSocket support

For a ready-to-use Pusher-compatible WebSocket server shared by every app, see [WebSocket server (Soketi)](#websocket-server-soketi). The steps below are for an app that runs its own WebSocket server.

The generated Caddyfile includes commented-out WebSocket configuration. To enable it for a site:

1. Open the site's Caddyfile (`~/.frankenmanager/caddy/sites/php-<version>/<domain>_Caddyfile`)
2. Replace the default compression directive:

```caddyfile
# Switch from blanket compression to websocket-safe compression
# Comment out:
#   encode br gzip
# Uncomment:
#   @notWebsocket {
#     not path /app*
#   }
#   encode @notWebsocket br gzip
```

3. Uncomment the WebSocket reverse proxy block:

```caddyfile
@websocket {
    path /app*
}
handle @websocket {
    reverse_proxy 127.0.0.1:7006
}
```

4. Restart the Caddy container: `frankenmanager restart --caddy`

#### Internal architecture

Worker sites run on plain HTTP internally. TLS termination happens at the reverse proxy layer (Caddy), which forwards requests over HTTP to the FrankenPHP workers. This means:
- Per-site Caddyfiles do **not** include `tls` directives for worker-to-proxy traffic: the `tls` lines of the template are commented out when a site Caddyfile is generated

### Force SSL certificate regeneration

```bash
frankenmanager start "domain.test" /path/to/projects --force-ssl
```

### Share the root CA over your LAN

mkcert's root CA is only trusted on the machine where it was installed. Accessing your `.test` domains from another device on the same network (phone, another computer) shows a certificate warning until that device trusts the same root CA too.

`trust-ca` serves `rootCA.pem` (never the private key) from your host's LAN IP over plain HTTP, so another device can download and install it without needing an MDM/GPO push. It's off by default and only runs while explicitly turned on:

```bash
# Start sharing on the default port (9080)
frankenmanager trust-ca on

# Start sharing on a custom port
frankenmanager trust-ca on --port 9443

# Check whether sharing is currently active
frankenmanager trust-ca status

# Stop sharing
frankenmanager trust-ca off
```

`trust-ca on` prints a URL such as `http://192.168.1.42:9080/` - open it in a browser on the other device to download `rootCA.pem`, then install it as a trusted root certificate (Keychain Access on macOS, Certificate Manager on Windows; for iOS and Android, see [iOS and Android devices](#ios-and-android-devices)). Turn sharing back off once the other device has installed it.

### WebSocket server (Soketi)

Apps that need WebSockets (e.g. Laravel broadcasting with Echo) can use an optional [Soketi](https://docs.soketi.app) server, compatible with the Pusher protocol. Turn it on:

```bash
frankenmanager service enable soketi
# Optional: the credentials are generated when empty
frankenmanager configuration get SOKETI_DEFAULT_APP_KEY
```

When the server runs, Soketi starts at once (otherwise on the next `start`): the `franken_soketi` container runs and:

- browsers connect to `wss://ws.frankenmanager.test` (port 443): the reverse proxy terminates TLS with a mkcert certificate and forwards the WebSocket traffic to Soketi. The hosts entry (and the LAN DNS record, when `DNS_SERVER=true`) is added for you;
- PHP apps publish events to its HTTP API at `127.0.0.1:6001` (`SOKETI_PORT`), with no TLS.

Every app shares the default app: use its credentials in each project. For a Laravel app (`BROADCAST_CONNECTION=pusher`, or `BROADCAST_DRIVER` before Laravel 11):

```bash
PUSHER_APP_ID=<SOKETI_DEFAULT_APP_ID>
PUSHER_APP_KEY=<SOKETI_DEFAULT_APP_KEY>
PUSHER_APP_SECRET=<SOKETI_DEFAULT_APP_SECRET>
# Server side: PHP -> Soketi
PUSHER_HOST=127.0.0.1
PUSHER_PORT=6001
PUSHER_SCHEME=http
PUSHER_APP_CLUSTER=mt1

# Browser side: Laravel Echo -> reverse proxy
VITE_PUSHER_APP_KEY="${PUSHER_APP_KEY}"
VITE_PUSHER_HOST=ws.frankenmanager.test
VITE_PUSHER_PORT=443
VITE_PUSHER_SCHEME=https
VITE_PUSHER_APP_CLUSTER="${PUSHER_APP_CLUSTER}"
```

Changing a credential (`configuration set SOKETI_DEFAULT_APP_KEY ...`) takes effect on the next `stop`/`start`. `service disable soketi` removes the container, the reverse proxy route and the `ws.frankenmanager.test` host at once.

### LAN DNS server

The `/etc/hosts` entries only exist on your machine: other devices on the network can't resolve your `.test` domains. Instead of editing the hosts file of each device, you can turn on an optional DNS server container ([CoreDNS](https://coredns.io)):

```bash
frankenmanager service enable dns
```

When the server runs, it starts at once (otherwise on the next `start`). It resolves every registered domain (including alternate and proxy hosts) to your machine's LAN IP, and forwards every other query to `DNS_UPSTREAM`. It listens on every network interface (`0.0.0.0:53`), so it keeps answering when you switch from Wi-Fi to Ethernet. `start` and `status` print the IP to give the other devices, e.g. `192.168.1.42`. When your machine is connected through several interfaces (e.g. Wi-Fi and Ethernet at once), `start` and `status` list every address the server answers on: on the same network, set two of them as primary and secondary DNS servers so that the devices keep resolving if one interface goes down.

Then point the other devices at it, either:
- per device: set `192.168.1.42` as the DNS server in its Wi-Fi/network settings;
- for the whole network: set it as the DNS server handed out by your router (DHCP settings).

Domains added or removed with `add-host`, `add-alias`, `remove-host` or `restore-host` are picked up within a few seconds, without restarting anything.

| Setting | Default | Description |
|---------|---------|-------------|
| `DNS_SERVER` | `false` | Run the LAN DNS server |
| `DNS_IP` | (auto-detected, kept up to date) | IP the domains resolve to. Set it if the detected LAN IP is wrong (several network interfaces, VPN...): it then stays fixed |
| `DNS_LISTEN_IP` | `0.0.0.0` | Address the server listens on. Set it only if port 53 is already taken on some address (e.g. `systemd-resolved` on Linux): use your LAN IP then |
| `DNS_PORT` | `53` | Port the server listens on. Keep `53`: most devices can't use another port |
| `DNS_UPSTREAM` | `1.1.1.1 8.8.8.8` | Space-separated resolvers for every other query. Use your router's or company's DNS if public resolvers are blocked |

**Notes:**
- The domains follow your machine's LAN IP: while the server runs, a background process checks it every few seconds and, when it changes (Wi-Fi to Ethernet, new DHCP lease...), updates the records, which CoreDNS reloads on its own. Nothing to restart. `status` shows whether it is active ("Follows LAN IP changes"); it is off when `DNS_IP` is set, since the IP is then fixed.
- That process doesn't depend on your terminal: on macOS it runs as a LaunchAgent (`~/Library/LaunchAgents/dev.frankenmanager.dns-watcher.plist`), on Linux as a systemd user unit (`frankenmanager-dns-watcher.service`). It comes back on its own after a reboot or a new login, like the containers, and is relaunched if it crashes; `stop` removes it. macOS may notify you that a background item was added: it is listed as "FrankenManager" in System Settings > General > Login Items, through the `FrankenManager.app` shipped in the install directory (`/usr/local/lib/frankenmanager/`). On Windows, or on Linux without a systemd user instance (e.g. WSL), it is a plain background process: run `frankenmanager restart --dns` after a reboot. Its log is `.dns_watcher.log` in the FrankenManager directory.
- The other devices still reach the DNS server through the IP you gave them: if your machine's IP changes, they lose it too. Reserve a fixed IP for your machine in your router's DHCP settings (one per network interface, e.g. Wi-Fi and Ethernet) to avoid reconfiguring them.
- `DNS_UPSTREAM` defaults to public resolvers rather than your machine's own: if the router hands this server out, your machine may itself resolve through it, and forwarding to its own resolver would loop.
- Anyone on your network can query this server while it runs. It only answers for your registered domains and forwards the rest, so it exposes no more than your web server already does.
- Combine it with [`trust-ca`](#share-the-root-ca-over-your-lan) so the other devices also trust the HTTPS certificates.
- Setting up a phone, tablet or simulator: see [iOS and Android devices](#ios-and-android-devices).

### iOS and Android devices

Reaching your domains from a phone, a tablet, a simulator or an emulator takes two things:
1. **The root CA**, so HTTPS works without warnings: turn on [`trust-ca`](#share-the-root-ca-over-your-lan).
2. **DNS**, so the device resolves your domains to your machine: turn on the [LAN DNS server](#lan-dns-server) (physical devices and Android emulator only).

```bash
frankenmanager trust-ca on     # prints the download URL, e.g. http://192.168.1.42:9080/
frankenmanager status          # prints the DNS server IP, e.g. 192.168.1.42
```

The device and your machine must be on the same network, and your firewall must allow incoming traffic on port 53 (DNS), 9080 (`trust-ca`) and 80/443 (web). Turn `trust-ca off` once your devices have installed the CA.

| | Root CA | DNS |
|---|---|---|
| iPhone / iPad | Profile + full trust (below) | Wi-Fi settings or router |
| iOS Simulator | `xcrun simctl keychain` | Nothing: uses your Mac's `/etc/hosts` |
| Android device | Settings > Install a certificate | Wi-Fi settings or router |
| Android Emulator | Settings > Install a certificate | `-dns-server` option |

#### iPhone / iPad

**Root CA**
1. Open the `trust-ca` URL in **Safari** (other browsers can't install profiles) and download `rootCA.pem`. Allow the configuration profile download.
2. Open **Settings > General > VPN & Device Management** (or the "Profile Downloaded" entry at the top of Settings), select the mkcert profile and tap **Install**.
3. Open **Settings > General > About > Certificate Trust Settings** and turn on full trust for the mkcert root certificate. Without this step, the certificate is installed but not trusted.

**DNS**
1. Open **Settings > Wi-Fi**, tap the **(i)** next to the current network, then **Configure DNS**.
2. Choose **Manual**, remove the existing servers and add your machine's IP (e.g. `192.168.1.42`). This setting only applies to that Wi-Fi network: there is no equivalent on cellular data.
3. If domains still don't resolve, turn off **Limit IP Address Tracking** and **iCloud Private Relay** for that network, and check no VPN or DNS profile (e.g. an ad blocker) is active: they bypass the configured DNS server.

#### iOS Simulator

The simulator uses your Mac's network stack, so the `/etc/hosts` entries already apply: no DNS setup needed. Add the root CA to the booted simulator (it is trusted straight away):

```bash
xcrun simctl keychain booted add-root-cert "$(mkcert -CAROOT)/rootCA.pem"
```

Alternatively, drag `rootCA.pem` onto the simulator window, then install and trust it as on a physical device. Repeat for each simulator (and after erasing one).

#### Android device

**Root CA**
1. Open the `trust-ca` URL in the browser and download `rootCA.pem`.
2. Open **Settings > Security & privacy > More security settings > Encryption & credentials > Install a certificate > CA certificate** (the path varies by vendor: search for "CA certificate" in Settings), confirm **Install anyway** and pick the downloaded file.

This installs a *user* CA: Chrome trusts it, but apps don't by default. To test your own app against your domains, allow user CAs in its debug builds with a [network security config](https://developer.android.com/privacy-and-security/security-config):

```xml
<!-- res/xml/network_security_config.xml, referenced by
     android:networkSecurityConfig="@xml/network_security_config" in AndroidManifest.xml -->
<network-security-config>
    <debug-overrides>
        <trust-anchors>
            <certificates src="user" />
        </trust-anchors>
    </debug-overrides>
</network-security-config>
```

Firefox for Android uses its own certificate store: enable **Settings > About Firefox** (tap the logo 5 times) then **Settings > Secret Settings > Use third party CA certificates**.

**DNS**
1. Open **Settings > Network & internet > Private DNS** and set it to **Off**: a Private DNS hostname bypasses the configured DNS server.
2. Open **Settings > Network & internet > Internet**, tap the gear next to the current Wi-Fi network, then **Edit** (pencil icon) **> Advanced options > IP settings > Static**.
3. Keep the IP address, gateway and prefix length the device currently has, and set **DNS 1** to your machine's IP (e.g. `192.168.1.42`).

Since Android only accepts a custom DNS server with a static IP, setting it in your router's DHCP settings is often simpler when several devices are involved.

#### Android Emulator

The emulator doesn't read your machine's `/etc/hosts`: start it with the LAN DNS server as its resolver.

```bash
emulator -avd <avd_name> -dns-server 192.168.1.42
```

List your AVDs with `emulator -list-avds` (the `emulator` binary lives in `$ANDROID_HOME/emulator`). Install the root CA by pushing it to the emulator, then follow the same Settings steps as on a physical device:

```bash
adb push "$(mkcert -CAROOT)/rootCA.pem" /sdcard/Download/
```

The same app limitation applies: use the network security config above for your own app.

### Show version

```bash
frankenmanager --version
```

### Show help

```bash
frankenmanager --help
frankenmanager start --help
frankenmanager add-host --help
frankenmanager add-alias --help
frankenmanager remove-host --help
frankenmanager restore-host --help
frankenmanager switch-php --help
frankenmanager list --help
frankenmanager reset --help
frankenmanager setup --help
```

### Show the image build logs

PHP images are built quietly. Add `--verbose`, before or after the command, to follow the `docker build` logs whenever a command builds an image (`start`, `start --build`, `restart --build`, or `add-host` / `restore-host` / `switch-php` on a new PHP version):

```bash
frankenmanager start --build --verbose
```

### Reset configuration and/or Caddyfiles

Reset specific FrankenManager components to their default state. This is useful when you want to start fresh or clean up your configuration.

⚠️ **IMPORTANT**:
- The server must be stopped before running this command
- This action cannot be undone
- You will be prompted for confirmation before proceeding
- **This does NOT affect your MariaDB database data** (your application data is safe)

```bash
# Reset only FrankenManager configuration (domain list)
frankenmanager reset --db

# Reset only custom Caddyfiles
frankenmanager reset --caddyfiles

# Reset both configuration and Caddyfiles
frankenmanager reset --db --caddyfiles
```

**What gets reset:**

| Option | Alias | What it does |
|--------|-------|--------------|
| `--db` | `--database` | Clears the registered domain list in FrankenManager's configuration (`db.sqlite`) |
| `--caddyfiles` | `--caddy` | Deletes every per-site Caddyfile in `caddy/sites/php-<version>/` (the archive is kept) |

**Use cases:**
- Clean up after testing multiple domains
- Remove all custom Caddyfiles before rebuilding configurations
- Reset to a clean state when troubleshooting issues
- Prepare for a fresh start without reinstalling FrankenManager

**Notes**:
- Archived Caddyfiles (in `caddy/sites/archive/`) are not affected by the reset command
- Your MariaDB database data remains untouched - this only resets FrankenManager's own configuration
- If you need to reset MariaDB data, you must manually delete the Docker volume

### Update to latest version

FrankenManager can update itself when new releases are available:

```bash
# Check for updates without installing
frankenmanager update --check

# Update to latest version (requires admin/sudo, see below)
sudo frankenmanager update

# Force reinstall latest version (requires admin/sudo)
sudo frankenmanager update --force
```

**Note**: the binary is installed in a system directory (e.g. `/usr/local/lib`), so `frankenmanager update` and `frankenmanager update --force` need to be run with `sudo` (or as an administrator). `frankenmanager update --check` does not modify anything and does not require elevated privileges.

Once the new version is installed, `update` also refreshes the static files of your data directory (`Dockerfile`, `docker-compose*.yml`, `docker/entrypoint.sh`, `caddy/Caddyfile.worker`, `caddy/Caddyfile.template`, `php/php.ini`, `php/php-prod.ini`), as your user even under `sudo`. Generated files (`caddy/Caddyfile`, `docker-compose-generated.yml`, per-site Caddyfiles) and the per-version `php/<version>/php.ini` are left untouched.

`update` also installs the [CLI alias](#cli-alias) when missing (releases before it had none).

On Windows, run `frankenmanager update` from an Administrator terminal: the running `frankenmanager.exe` is moved aside and removed by the next update.

FrankenManager also notifies you when a new version is available. This check never slows a command down: the latest release found on GitHub is cached for 6 hours (in `update_check.json` in the data directory) and refreshed in the background, so a new release is announced from the next command on.

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
├── db.sqlite               # Configuration (including the database passwords) and server state (domains)
├── .db_password_history_*  # Previous database root passwords (used to sync password changes)
├── docker-compose.yml      # Docker Compose configuration
├── Dockerfile              # Custom FrankenPHP image
├── caddy/
│   ├── Caddyfile           # Main Caddy configuration
│   ├── Caddyfile.template  # Per-site template
│   ├── sites/
│   │   ├── custom/         # Active site configs
│   │   ├── default/        # Default site configs
│   │   └── archive/        # Archived site configs (from remove-host)
│   ├── certs/              # SSL certificates
│   ├── data/               # Caddy data
│   ├── config/             # Caddy config
│   └── log/                # Access and error logs
├── dns/                    # LAN DNS server config and records (when DNS_SERVER=true)
├── phpmyadmin/             # Generated phpMyAdmin config (multi-server auto-login)
├── php/
│   ├── php.ini             # PHP configuration
│   └── php-prod.ini        # Production overrides
└── database/               # MariaDB data
```

Check your data directory location:

```bash
frankenmanager setup --status
```

### Server State Database

FrankenManager uses a SQLite database (`db.sqlite`) to store server state and domain configurations. This provides:

- **Real-time status checking**: Server status reflects actual Docker container states
- **Reliable state management**: ACID-compliant database transactions
- **Timestamps**: Track when domains were added and when status changed
- **Data integrity**: Unique constraints prevent duplicate domains
- **Future-proof**: Easy schema evolution for new features

**Database Schema:**

```sql
-- Server state table
CREATE TABLE server_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    status TEXT NOT NULL DEFAULT 'stopped',
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Domains table
CREATE TABLE domains (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain TEXT NOT NULL UNIQUE,
    php_version TEXT NOT NULL DEFAULT '8.3',
    added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Aliases table (alternate hosts added via `add-alias`)
CREATE TABLE aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alias_domain TEXT NOT NULL UNIQUE,
    target_domain TEXT NOT NULL,
    added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Proxies table (raw upstream proxy hosts added via `add-host <domain> <target>`)
CREATE TABLE proxies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain TEXT NOT NULL UNIQUE,
    target TEXT NOT NULL,
    added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### Settings

The configuration lives in the `configuration` table of `db.sqlite`: only the values you change are stored, the others use their default. Show and change it with the `configuration` command (alias `config`):

```bash
frankenmanager configuration                         # Every setting, its value, its source and how a change applies
frankenmanager configuration get DEFAULT_PHP_VERSION # The value alone, for scripts
frankenmanager configuration set DEFAULT_PHP_VERSION 8.4
frankenmanager configuration set REDIS_ENABLED true  # Starts Redis at once when the server runs
frankenmanager configuration reset WEB_HTTPS_PORT    # Back to the default
frankenmanager configuration --show-secrets          # Show the passwords in clear
```

Names are case-insensitive and values are checked (`true`/`false`, ports, PHP versions, IPs...). Depending on the setting, a change applies:

| Applies | Settings | What happens |
|---------|----------|--------------|
| live | `REDIS_ENABLED`, `DNS_SERVER`, `SOKETI_ENABLED` | The service starts or stops at once (same as `service enable\|disable`) |
| immediate | `CLI_ALIAS` | The alias is replaced at once |
| next command | `DEFAULT_PHP_VERSION`, `DEFAULT_PROJECT_PATH` | Read by the next command |
| `restart --dns` | `DNS_IP`, `DNS_LISTEN_IP`, `DNS_PORT`, `DNS_UPSTREAM` | Refreshes the LAN DNS server |
| `restart --database` | `MARIADB_ROOT_PASSWORD`, `MYSQL_ROOT_PASSWORD` | Syncs the root passwords |
| `restart --build` | `PHP_IMAGE_DISTRO`, `INSTALL_PDFTK`, `EXTRA_*_PACKAGES*`, `WWWGROUP` | Rebuilds the PHP images |
| stop/start | everything else (ports, versions, `DB_ENGINES`, `APP_ENV`...) | The containers must be recreated |

When the server runs, `configuration set` tells which command applies the change; nothing is restarted behind your back.

**Shell completion.** `configuration set` completes the setting names (with their description) and their accepted values. Install the completion once, then again after changing the alias:

```bash
echo 'source <(frankenmanager completion zsh)' >> ~/.zshrc    # zsh (after compinit)
echo 'source <(frankenmanager completion bash)' >> ~/.bashrc  # bash (needs bash-completion)
frankenmanager completion fish > ~/.config/fish/completions/frankenmanager.fish
frankenmanager completion powershell >> $PROFILE
```

| Setting | Default | Description |
|---------|---------|-------------|
| `CLI_ALIAS` | `fkm` | Short alias of the command, see [CLI alias](#cli-alias) (empty: none) |
| `APP_ENV` | `dev` | `dev` for local development (always the case on a workstation), `prod`/`production` only to run FrankenManager on a public/cloud server, see [Production Mode](#production-mode) |
| `DEFAULT_PHP_VERSION` | `8.3` | Default PHP version for new domains (`8.2` (deprecated), `8.3`, `8.4`, `8.5`) |
| `UID` | (current user) | System user ID for file ownership |
| `GID` | (current group) | System group ID for file ownership |
| `DEFAULT_PROJECT_PATH` | (empty) | Projects root used when `start` has no `--path` |
| `CADDY_DIR` | `./caddy` | Caddy files directory, certificates in its `certs/` (relative to the data directory, or absolute) |
| `DATABASE_DIR` | `./database` | Database data directory |
| `MARIADB_ROOT_PASSWORD` | (auto-generated) | MariaDB root password |
| `EXPOSE_SERVICES` | `false` | Expose services to the network (not just localhost) |
| `DB_PORT` | `3306` | MariaDB port |
| `PMA_PORT` | `8080` | phpMyAdmin port |
| `DB_ENGINES` | `mariadb` | Database engines to run, comma-separated: `mariadb`, `mysql`, `postgresql` |
| `MYSQL_ROOT_PASSWORD` | (auto-generated) | MySQL root password |
| `MYSQL_PORT` | `3307` | MySQL port |
| `POSTGRES_PASSWORD` | (auto-generated) | PostgreSQL password (also the pgAdmin password) |
| `POSTGRES_PORT` | `5432` | PostgreSQL port |
| `PGADMIN_PORT` | `8081` | pgAdmin port |
| `REDIS_ENABLED` | `false` | Run the Redis cache container |
| `REDIS_PORT` | `6379` | Redis port |
| `SOKETI_ENABLED` | `false` | Run the Soketi WebSocket server (see [WebSocket server (Soketi)](#websocket-server-soketi)) |
| `SOKETI_PORT` | `6001` | Soketi port the PHP apps reach (browsers use `wss://ws.frankenmanager.test`) |
| `SOKETI_DEFAULT_APP_ID` | (auto-generated) | Soketi default app ID |
| `SOKETI_DEFAULT_APP_KEY` | (auto-generated) | Soketi default app key |
| `SOKETI_DEFAULT_APP_SECRET` | (auto-generated) | Soketi default app secret |
| `WEB_HTTP_PORT` | `80` | Web server HTTP port |
| `WEB_HTTPS_PORT` | `443` | Web server HTTPS port |
| `DNS_SERVER` | `false` | Run the LAN DNS server (see [LAN DNS server](#lan-dns-server)) |
| `DNS_IP` | (auto-detected) | IP the LAN DNS server resolves domains to |
| `DNS_LISTEN_IP` | (every interface) | Address the LAN DNS server listens on |
| `DNS_PORT` | `53` | LAN DNS server port |
| `DNS_UPSTREAM` | `1.1.1.1 8.8.8.8` | Resolvers the LAN DNS server forwards other queries to |
| `MYSQL_MAX_ALLOWED_PACKET` | `512M` | MariaDB max packet size |
| `PHP_IMAGE_DISTRO` | `alpine` | Base distribution of the PHP images: `alpine` (smaller) or `trixie` (Debian, glibc) |
| `INSTALL_PDFTK` | `false` | Install `pdftk` in the PHP image (adds a Java runtime, ~200 MB) |
| `EXTRA_APK_PACKAGES` | (empty) | Extra Linux (apk) packages to install in the PHP image when `PHP_IMAGE_DISTRO=alpine`, space-separated, for every PHP version |
| `EXTRA_APK_PACKAGES_<version>` | (empty) | Extra apk packages for one PHP version only (e.g. `EXTRA_APK_PACKAGES_84`), merged with `EXTRA_APK_PACKAGES` |
| `EXTRA_APT_PACKAGES` | (empty) | Extra Linux (apt) packages to install in the PHP image when `PHP_IMAGE_DISTRO=trixie`, space-separated, for every PHP version |
| `EXTRA_APT_PACKAGES_<version>` | (empty) | Extra apt packages for one PHP version only (e.g. `EXTRA_APT_PACKAGES_84`), merged with `EXTRA_APT_PACKAGES` |
| `EXTRA_COMPOSER_PACKAGES` | (empty) | Extra global Composer packages to install in the PHP image, space-separated, for every PHP version |
| `EXTRA_COMPOSER_PACKAGES_<version>` | (empty) | Extra global Composer packages for one PHP version only (e.g. `EXTRA_COMPOSER_PACKAGES_84`), merged with `EXTRA_COMPOSER_PACKAGES` |

### Base Image (Alpine / Trixie)

The PHP images are built on Alpine by default, which keeps them around half the size of the Debian ones. Run `frankenmanager configuration set PHP_IMAGE_DISTRO trixie` to build on Debian Trixie instead, e.g. if a project relies on glibc-specific behavior (`iconv` transliteration, locales...).

`pdftk` is not installed by default since it needs a Java runtime (~200 MB). Run `frankenmanager configuration set INSTALL_PDFTK true` to add it, on either distribution.

### Extra Packages (apk / apt / Composer)

The custom FrankenPHP image can install additional Linux and global Composer packages on top of the built-in ones, configured with `frankenmanager configuration set`. Linux packages are read from `EXTRA_APK_PACKAGES` on Alpine and from `EXTRA_APT_PACKAGES` on Trixie, as package names differ. `<version>` is the PHP version without the dot (`82`, `83`, `84`, `85`).

```bash
# Applies to every PHP version
frankenmanager configuration set EXTRA_APK_PACKAGES "htop vim"
frankenmanager configuration set EXTRA_COMPOSER_PACKAGES deployer/deployer

# Applies to PHP 8.4 only, merged with the settings above
frankenmanager configuration set EXTRA_APK_PACKAGES_84 redis
frankenmanager configuration set EXTRA_COMPOSER_PACKAGES_84 laravel/installer
```

Composer packages are installed with `composer global require`, so their binaries are available on the container's `PATH`.

Changes take effect on the next image build: images whose build options changed are rebuilt automatically on the next `frankenmanager start` (or `add-host`/`restore-host`/`switch-php` when they start a new version container). Use `frankenmanager restart --build` to apply them without stopping the server.

### CLI alias

`fkm` is a short alias of `frankenmanager`: `fkm start`, `fkm config set ...`. `frankenmanager` always stays available. The install script creates it next to the command (`/usr/local/bin/fkm`, a symlink; `fkm.cmd` on Windows), as do `setup` and `update` when it is missing.

```bash
frankenmanager configuration set CLI_ALIAS fk   # Replaces fkm by fk
frankenmanager configuration set CLI_ALIAS ""   # No alias
frankenmanager configuration reset CLI_ALIAS    # Back to fkm
```

An existing file of that name that is not a FrankenManager alias is never replaced: the command warns instead. When the directory is not writable, it prints the command to create the alias by hand (e.g. `sudo ln -s frankenmanager /usr/local/bin/fkm`). If another `fkm` comes first in your `PATH`, the command warns that `fkm` runs the other program. The default is `fkm` rather than `fm`, which macOS already ships as `/usr/bin/fm`.

### Databases

Enable one or more engines with `DB_ENGINES` (e.g. `DB_ENGINES=mariadb,mysql,postgresql`). Their root passwords are generated on the first start and stored in the configuration (`db.sqlite` of the data directory):

| Engine | User | Password setting |
|--------|------|------------------|
| MariaDB | `root` | `MARIADB_ROOT_PASSWORD` |
| MySQL | `root` | `MYSQL_ROOT_PASSWORD` |
| PostgreSQL | `POSTGRES_USER` (`postgres`) | `POSTGRES_PASSWORD` |

```bash
frankenmanager configuration get MARIADB_ROOT_PASSWORD
```

Changing a password (`configuration set MARIADB_ROOT_PASSWORD ...`) is applied to the running database on the next `start` or `restart --db` (MariaDB/MySQL).

**Admin UIs (dev only).** They are started automatically depending on the enabled engines, and one of them is served at `https://localhost` (unless `localhost` is registered as a host):

- **phpMyAdmin** runs as soon as MariaDB or MySQL is enabled, on `PMA_PORT` (`8080`) and at `https://localhost`. A single phpMyAdmin connects to every enabled MariaDB/MySQL server, already logged in as `root`: when both are enabled, switch between them with phpMyAdmin's server selector (MariaDB is the default).
- **pgAdmin** runs as soon as PostgreSQL is enabled, on `PGADMIN_PORT` (`8081`). It is served at `https://localhost` when PostgreSQL is the only engine; otherwise `https://localhost` stays on phpMyAdmin. Log in with `PGADMIN_EMAIL` (`admin@admin.com`) and `POSTGRES_PASSWORD`.

### Production Mode

`APP_ENV` is only meant for running FrankenManager on a public/cloud server. It plays no part in local development: leave it on `dev` (the default) on a workstation, whatever the environment of the apps you serve. Production mode drops what local development relies on (mkcert certificates per domain, phpMyAdmin/pgAdmin, published ports).

On such a server, run `frankenmanager configuration set APP_ENV prod` (then `stop`/`start`) to enable:

- Production Docker Compose configuration (host networking, containers run as `UID`:`GID`)
- No phpMyAdmin/pgAdmin, nothing served at `https://localhost`
- PHP OPCache enabled
- `expose_php` disabled
- Output buffering (4096 bytes)
- Let's Encrypt certificates from `/etc/letsencrypt` (instead of mkcert)

## Architecture

### Source Code Structure

```
frankenphp-cli/
├── cmd/
│   ├── frankenmanager/         The CLI
│   ├── frankenmanager-agent/   Executable of FrankenManager.app (macOS)
│   └── release/                Builds the release archives
├── internal/
│   ├── cli/                    Commands (cobra)
│   ├── core/                   Everything the commands rely on
│   │   ├── caddyfile/          Caddyfile generation
│   │   ├── caserver/           Root CA sharing (trust-ca)
│   │   ├── compose/            docker-compose-generated.yml
│   │   ├── dns/                LAN DNS server (CoreDNS) config
│   │   ├── dnswatcher/         LAN IP watcher (LaunchAgent, systemd unit)
│   │   ├── docker/             docker CLI wrapper, image builds
│   │   ├── alias/              CLI alias (fkm) next to the command
│   │   ├── env/                Configuration: settings registry
│   │   ├── hosts/              Hosts file management
│   │   ├── mkcert/             mkcert install and CA
│   │   ├── password/           Database password sync
│   │   ├── phpversions/        PHP versions and port assignments
│   │   ├── privilege/          Passwordless hosts helper (sudoers)
│   │   ├── resources/          Data directory and embedded resources
│   │   ├── soketi/             Soketi WebSocket server
│   │   ├── ssl/                Site certificates
│   │   ├── stack/              Compose helpers shared by the commands
│   │   ├── store/              SQLite state (db.sqlite)
│   │   ├── updater/            Self-update
│   │   └── validate/           Input validation
│   ├── platform/               OS-specific code (paths, elevation, processes)
│   ├── release/                Release archives and checksums
│   └── ui/                     Log lines and tables
├── assets.go                   Embeds the resources below into the executable
├── docker-compose.yml          Docker Compose (dev)
├── docker-compose-prod.yml     Docker Compose (prod)
├── Dockerfile                  Custom FrankenPHP image
├── caddy/                      Caddy configuration
├── php/                        PHP configuration
├── install.sh                  Installer (macOS, Linux)
└── install.ps1                 Installer (Windows)
```

## Services

| Service | Container | Default Port | Description |
|---------|-----------|--------------|-------------|
| Caddy (reverse proxy) | `franken-proxy` | 80, 443 | TLS termination and routing |
| FrankenPHP 8.2 | `frankenphp-8.2` | 8280 (internal) | PHP 8.2 worker |
| FrankenPHP 8.3 | `frankenphp-8.3` | 8380 (internal) | PHP 8.3 worker |
| FrankenPHP 8.4 | `frankenphp-8.4` | 8480 (internal) | PHP 8.4 worker |
| FrankenPHP 8.5 | `frankenphp-8.5` | 8580 (internal) | PHP 8.5 worker |
| MariaDB | `franken_mariadb` | 3306 | Database |
| MySQL (optional) | `franken_mysql` | 3307 | Database, when `DB_ENGINES` includes `mysql` |
| PostgreSQL (optional) | `franken_postgresql` | 5432 | Database, when `DB_ENGINES` includes `postgresql` |
| Redis (optional) | `franken_redis` | 6379 | Cache, when `REDIS_ENABLED=true` |
| Soketi (optional) | `franken_soketi` | 6001 | WebSocket server, when `SOKETI_ENABLED=true`, served at `wss://ws.frankenmanager.test` |
| phpMyAdmin | `franken_phpmyadmin` | 8080 | Database UI for MariaDB and MySQL (dev only), also served at `https://localhost` (see [Databases](#databases)) |
| pgAdmin (optional) | `franken_pgadmin` | 8081 | Database UI for PostgreSQL (dev only), served at `https://localhost` when PostgreSQL is the only engine |
| CoreDNS (optional) | `franken_dns` | 53 (LAN IP only) | LAN DNS server, when `DNS_SERVER=true` |

Only containers for PHP versions that have at least one domain are started: with no PHP domain at all, no FrankenPHP container runs.

All ports are configurable with `frankenmanager configuration set`. See [Settings](#settings) for details.

## Development

Requires [Go](https://go.dev) 1.26 or later.

### Build

```bash
make build        # bin/frankenmanager, for this machine
make build-all    # the 7 release targets in bin/
```

### Run tests

```bash
make test
```

The tests never reach the network, Docker or your real data directory: external programs, GitHub and the hosts file are faked.

### Linting

```bash
make lint     # golangci-lint, the version of the CI (fetched by go run)
make hooks    # once per clone: lint the changed Go code before each commit
```

### Release archives

```bash
go run ./cmd/release all -version 1.2.0    # dist/assets: archives, bare executables, SHA256SUMS.txt
```

### Releasing

Releases are published by the `Release` workflow, run manually from the GitHub Actions UI (*Actions → Release → Run workflow*) or with `gh workflow run release.yml -f version=1.2.0`. Pushing a tag does not publish anything.

- Only repository admins can run it: the workflow stops right away for anyone else.
- The release and its `v<version>` tag are created in the name of the admin who runs it, with their own token taken from the `RELEASE_TOKENS` secret, a JSON object mapping GitHub logins to fine-grained personal access tokens (this repository only, *Contents: Read and write*): `{"alice": "github_pat_..."}`.
- `version`: `X.Y.Z`, or `X.Y.Z-suffix` for a pre-release (e.g. `1.2.0-rc.1`).
- `changelog` (optional): Markdown inserted as the release notes' *What's Changed* section. GitHub only offers single-line text inputs, so this suits short notes: type `\n` for line breaks in the UI field, or pass a file from the CLI, which keeps real line breaks: `gh workflow run release.yml -f version=1.2.0 -f changelog="$(cat notes.md)"`. When empty, the version's section of `CHANGELOG.md` is used.
- `draft` (optional): creates the release as a draft, with its binaries attached. Open it from the *Releases* page, write the notes in GitHub's Markdown editor (with preview), then click *Publish release*. The `v<version>` tag is only created at that point.

## Platform Support

| Feature | macOS | Linux | Windows |
|---------|-------|-------|---------|
| Pre-built binary | arm64 + x86_64 | x86_64 + arm64 + armv7 | x86_64 + arm64 |
| Passwordless hosts | sudoers helper | sudoers helper | Admin required |
| mkcert auto-install | Homebrew | Direct download | Chocolatey/Scoop |
| Self-update | yes | yes | yes (Administrator terminal) |

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

**`cURL error 60: unable to get local issuer certificate` from PHP on the host (macOS)** while browsers accept the site: Homebrew's PHP and curl read Homebrew's CA bundle, not the keychain. `setup` adds the CA to that bundle on its own; if it could not, run `brew upgrade ca-certificates` (or `brew postinstall ca-certificates` when it is up to date). Inside the containers the CA is trusted on startup: restart them after a new CA.

### Certificate errors from another device on the network

This is expected: mkcert's root CA is only trusted on the machine that installed it. Use `frankenmanager trust-ca on` to serve the root CA over your LAN so the other device can download and trust it - see [Share the root CA over your LAN](#share-the-root-ca-over-your-lan).

### Another device can't resolve the domains

The domains are only in your machine's `/etc/hosts`. Turn on the [LAN DNS server](#lan-dns-server) and set it as that device's DNS server (see [iOS and Android devices](#ios-and-android-devices) for phones and emulators). If it still fails, check that `frankenmanager status` shows `franken_dns` running and that your firewall allows incoming traffic on port 53 (UDP and TCP). If your machine's IP changed (new network, Wi-Fi to Ethernet), the records follow it on their own, but update the DNS server set on the device. If `status` shows "Follows LAN IP changes: no", run `frankenmanager restart --dns`.

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
sudo rm -rf /usr/local/bin/frankenmanager /usr/local/bin/fkm /usr/local/lib/frankenmanager

# Remove data directory
rm -rf ~/.frankenmanager

# Or on Windows:
# rmdir /s %LOCALAPPDATA%\frankenmanager
```

## License

Distributed under the MIT License. See [LICENSE](LICENSE) for details.
