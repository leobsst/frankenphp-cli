"""Start command implementation."""

import sys
from pathlib import Path
from typing import Optional

from ..core import dns_server, dns_watcher, soketi
from ..core.caddyfile import CaddyfileGenerator
from ..core.database import DatabaseManager
from ..core.docker_manager import DockerManager
from ..core.environment import EnvironmentManager
from ..core.hosts_manager import HostsManager
from ..core.password_manager import PasswordManager
from ..core.php_versions import (
    resolve_default_php_version,
    validate_php_version,
    warn_if_deprecated,
)
from ..core.resources import ensure_php_version_config, get_project_dir
from ..core.ssl_manager import SSLManager
from ..core.stack import PROJECT_PATH_SETTING, compose_up
from ..exceptions import ServerStateError
from ..utils.logging import log_error, log_info, log_success, log_warning
from ..utils.validation import validate_directory, validate_domain


def start_server(
    domains: Optional[list[str]],
    custom_path: Optional[Path],
    force_ssl: bool,
    php_version: Optional[str] = None,
    build: bool = False,
) -> None:
    """Start the FrankenPHP server.

    Starts even without any domain: the reverse proxy and databases come up
    alone, ready for proxy hosts (add-host <domain> <target>) or PHP hosts.

    Args:
        domains: List of domain names to serve (None to use registered domains from database).
        custom_path: Path to the project root (None to use DEFAULT_PROJECT_PATH).
        force_ssl: Whether to force SSL certificate regeneration.
        php_version: Default PHP version for new domains (None = use .env or fallback).
        build: Rebuild the PHP images even if they are up to date.
    """
    project_dir = get_project_dir()

    # Initialize managers
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")

    # Ensure .env exists
    if not env.ensure_env_exists():
        env_path = project_dir / ".env"
        log_info(f"Created .env file at {env_path}. Please review settings, then try again.")
        sys.exit(1)

    env.load()

    # Resolve PHP version from --php flag, .env, or fallback
    if php_version is None:
        php_version = resolve_default_php_version(env.get("DEFAULT_PHP_VERSION"))
    validate_php_version(php_version)
    warn_if_deprecated(php_version)

    # Check required environment variables
    try:
        env.require("UID")
        env.require("GID")
    except Exception as e:
        log_error(str(e))
        log_error("Please set UID and GID in .env file.")
        sys.exit(1)

    # Resolve project path (only required to serve PHP domains)
    if custom_path is None and env.get("DEFAULT_PROJECT_PATH"):
        custom_path = Path(env.get("DEFAULT_PROJECT_PATH") or "")

    db_engines = env.get_db_engines()

    # Generate passwords for enabled DB engines
    if "mariadb" in db_engines and not env.get("MARIADB_ROOT_PASSWORD"):
        password = env.generate_db_password()
        env.set("MARIADB_ROOT_PASSWORD", password)
        log_info("MariaDB password generated and saved to .env")

    if "mysql" in db_engines and not env.get("MYSQL_ROOT_PASSWORD"):
        password = env.generate_db_password()
        env.set("MYSQL_ROOT_PASSWORD", password)
        log_info("MySQL password generated and saved to .env")

    if "postgresql" in db_engines and not env.get("POSTGRES_PASSWORD"):
        password = env.generate_db_password()
        env.set("POSTGRES_PASSWORD", password)
        log_info("PostgreSQL password generated and saved to .env")

    generated = soketi.ensure_credentials(env)
    if generated:
        log_info(f"Soketi {', '.join(generated)} generated and saved to .env")

    env.load()  # Reload to get any new passwords

    # Resolve storage paths from environment
    caddy_dir = env.resolve_dir("CADDY_DIR", "./caddy", project_dir)
    certs_dir = caddy_dir / "certs"
    sites_dir = caddy_dir / "sites"
    database_dir = env.resolve_dir("DATABASE_DIR", "./database", project_dir)

    docker = DockerManager(project_dir)
    db = DatabaseManager(project_dir / "db.sqlite", docker)
    ssl = SSLManager(certs_dir)
    hosts = HostsManager()
    caddyfile = CaddyfileGenerator(project_dir, sites_dir)
    password_manager = PasswordManager(project_dir, docker)

    # Check state
    if db.is_running:
        raise ServerStateError("The server is already running.")

    # Load and merge domains
    registered = db.get_domains_with_versions()
    registered_domains = [d for d, _ in registered]
    registered_map = {d: v for d, v in registered}

    domains_with_versions: list[tuple[str, str]] = []

    if domains is None:
        # No domains provided, use registered ones (if any)
        domains_with_versions = list(registered)
        if registered:
            log_info(f"Using registered domains: {', '.join(registered_domains)}")
        else:
            log_info("No PHP domain registered: starting without PHP container.")
            log_info('Add hosts later with: frankenmanager add-host "myapp.test"')
    else:
        # Merge provided domains with registered ones

        # Keep registered domains with their existing versions
        for d, v in registered:
            domains_with_versions.append((d, v))

        # Add new domains with the specified PHP version
        new_domains_list = [d for d in domains if d not in registered_map]
        for d in new_domains_list:
            domains_with_versions.append((d, php_version))

        if registered:
            log_info(f"Registered domains: {', '.join(registered_domains)}")
            if new_domains_list:
                log_info(f"New domains (PHP {php_version}): {', '.join(new_domains_list)}")

    # Extract flat domain list for validation
    all_domains = [d for d, _ in domains_with_versions]

    # Validate inputs (the project path is only needed by PHP containers)
    if all_domains:
        if custom_path is None:
            log_error("No project path specified and DEFAULT_PROJECT_PATH is not set in .env")
            sys.exit(1)
        validate_directory(custom_path)
    for domain in all_domains:
        validate_domain(domain)

    # Remove duplicates while preserving order
    seen: set[str] = set()
    unique_domains_versions: list[tuple[str, str]] = []
    for d, v in domains_with_versions:
        if d not in seen:
            seen.add(d)
            unique_domains_versions.append((d, v))
    domains_with_versions = unique_domains_versions
    all_domains = [d for d, _ in domains_with_versions]

    # Determine which PHP versions are needed
    active_versions = {v for _, v in domains_with_versions}

    # Ensure php.ini exists for each version
    for version in active_versions:
        ensure_php_version_config(project_dir, version)

    # Alias (alternate host) domains persist in their own table and don't carry
    # a PHP version/container of their own, but still need certs + hosts entries
    alias_domains = [alias for alias, _, _ in db.get_alias_entries()]

    # Proxy hosts likewise persist independently and forward to a raw upstream
    # address, but still need certs + hosts entries
    proxy_entries = db.get_proxies()
    proxy_domains = [domain for domain, _ in proxy_entries]

    # The Soketi WebSocket host (when enabled) is served by the reverse proxy too
    websocket_domains = [d for d in soketi.get_domains(env) if d not in all_domains]

    hosts_added: list[str] = []

    try:
        served_domains = all_domains + alias_domains + proxy_domains + websocket_domains

        # Generate SSL certificates (real domains + aliases + proxies + WebSocket)
        ssl.generate_all(served_domains, force_ssl, env.is_production())

        # Add hosts entries (real domains + aliases + proxies + WebSocket)
        for domain in served_domains:
            hosts.add_entry("127.0.0.1", domain)
            hosts_added.append(domain)

        # Generate per-version Caddyfiles (new domains only)
        caddyfile.generate_all_for_versions(domains_with_versions)

        # Migrate existing Caddyfiles from TLS to HTTP-only worker format
        caddyfile.migrate_worker_caddyfiles()

        # Generate main reverse proxy Caddyfile
        caddyfile.generate_main_caddyfile(
            domains_with_versions,
            caddy_dir,
            env.is_production(),
            db.get_alias_entries(),
            proxy_entries,
            db_admin_target=env.get_db_admin_target(),
            websocket_target=soketi.get_target(env),
        )

        # Save domains to database with versions
        db.set_domains_with_versions(domains_with_versions)
        if custom_path is not None:
            db.set_setting(PROJECT_PATH_SETTING, str(custom_path))

        # LAN DNS server records (no-op unless DNS_SERVER=true)
        dns_server.sync(project_dir, env, db)
        dns_listen = dns_server.get_listen_address(env)

        # Fix .my-healthcheck.cnf permissions if world-writable (MySQL ignores such files)
        healthcheck_cnf = database_dir / ".my-healthcheck.cnf"
        if healthcheck_cnf.exists() and (healthcheck_cnf.stat().st_mode & 0o022):
            healthcheck_cnf.chmod(0o640)

        # Build PHP images only when missing, outdated or explicitly requested
        project_path = str(custom_path) if custom_path is not None else ""
        docker.build_images(project_path, active_versions, env, force=build)
        docker.compose_down(env.is_production())
        compose_up(project_dir, docker, env, active_versions, project_path)

        # Sync database passwords
        db_passwords = {}
        if "mariadb" in db_engines:
            db_passwords["mariadb"] = env.require("MARIADB_ROOT_PASSWORD")
        if "mysql" in db_engines:
            db_passwords["mysql"] = env.require("MYSQL_ROOT_PASSWORD")
        password_manager.sync_all_passwords(db_engines, db_passwords)

        # Follow LAN IP changes (no-op unless DNS_SERVER=true and DNS_IP unset)
        dns_watched = dns_watcher.update(project_dir, env)

        print()
        log_success("Web server started!")

        # Show PHP version summary
        for version in sorted(active_versions):
            version_domains = [d for d, v in domains_with_versions if v == version]
            log_info(f"  PHP {version}: {', '.join(version_domains)}")

        if soketi.is_enabled(env):
            log_info(
                f"Soketi WebSocket server: wss://{soketi.DOMAIN} "
                f"(apps: 127.0.0.1:{soketi.get_port(env)})"
            )

        if dns_listen:
            dns_ip = dns_server.resolve_ip(env)
            log_info(f"LAN DNS server listening on {dns_listen}")
            log_info(f"  Set {dns_ip} as the DNS server of your other devices (or of your router).")
            if dns_watched:
                log_info("  The domains follow this machine's LAN IP when it changes.")
            if dns_ip.startswith("127."):
                log_warning(
                    "No LAN IP detected: the domains resolve to this machine only. "
                    "Set DNS_IP in .env to this machine's LAN IP."
                )

    except Exception as e:
        # Cleanup on failure
        log_error(f"An error occurred: {e}")
        log_error("Cleaning up...")

        for domain in hosts_added:
            try:
                hosts.remove_entry("127.0.0.1", domain)
            except Exception:
                pass

        # Restore domains to their pre-attempt state rather than wiping the
        # database: previously registered domains/aliases are unrelated to
        # this failure and must survive it.
        db.set_domains_with_versions(registered)
        log_error("Cleanup complete. The server did not start.")
        raise
