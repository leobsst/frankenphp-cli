"""Add host command implementation."""

from pathlib import Path
from typing import Optional

from ..core import dns_server
from ..core.caddyfile import CaddyfileGenerator
from ..core.database import DatabaseManager
from ..core.docker_manager import DockerManager
from ..core.environment import EnvironmentManager
from ..core.hosts_manager import HostsManager
from ..core.php_versions import (
    get_container_name,
    resolve_default_php_version,
    validate_php_version,
    warn_if_deprecated,
)
from ..core.resources import ensure_php_version_config, get_project_dir
from ..core.ssl_manager import SSLManager
from ..core.stack import compose_up, get_project_path
from ..exceptions import ServerStateError
from ..utils.logging import log_error, log_info, log_success
from ..utils.validation import validate_domain, validate_proxy_target


def _resolve_path(env_value: Optional[str], default: str, project_dir: Path) -> Path:
    """Resolve a path from environment variable or default.

    Args:
        env_value: Value from environment variable (may be empty/None).
        default: Default relative path.
        project_dir: Project directory for relative paths.

    Returns:
        Resolved absolute path.
    """
    path_str = env_value if env_value else default
    path = Path(path_str)
    if not path.is_absolute():
        path = project_dir / path
    return path


def add_host(
    domains: list[str],
    force_ssl: bool,
    php_version: Optional[str] = None,
    proxy_target: Optional[str] = None,
) -> None:
    """Add new host(s) to the running server.

    Args:
        domains: List of new domain names to add.
        force_ssl: Whether to force SSL certificate regeneration.
        php_version: PHP version for the new domains (None = use .env or fallback).
        proxy_target: If set, the new domain(s) reverse-proxy straight to this raw
            upstream address (e.g. "127.0.0.1:8006") instead of getting their own
            PHP container. `php_version` is ignored in that case.
    """
    project_dir = get_project_dir()

    # Initialize managers
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    env.load()

    docker = DockerManager(project_dir)
    db = DatabaseManager(project_dir / "db.sqlite", docker)

    # Check state - server must be running
    if not db.is_running:
        raise ServerStateError("The server is not running. Use 'start' command instead.")

    # Get existing domains, aliases, and proxy hosts
    existing_domains = db.get_domains()
    existing_aliases = {alias for alias, _ in db.get_aliases()}
    existing_proxies = {proxy for proxy, _ in db.get_proxies()}

    # Validate new domains and filter out duplicates
    new_domains: list[str] = []
    for domain in domains:
        validate_domain(domain)
        if domain in existing_domains or domain in existing_aliases or domain in existing_proxies:
            log_info(f"Domain '{domain}' is already configured, skipping.")
        elif domain in new_domains:
            log_info(f"Domain '{domain}' is duplicated in input, skipping.")
        else:
            new_domains.append(domain)

    if not new_domains:
        log_info("No new domains to add.")
        return

    # Resolve storage paths from environment
    caddy_dir = _resolve_path(env.get("CADDY_DIR"), "./caddy", project_dir)

    ssl = SSLManager(caddy_dir / "certs")
    hosts = HostsManager()
    caddyfile = CaddyfileGenerator(project_dir, caddy_dir / "sites")

    if proxy_target is not None:
        _add_proxy_hosts(
            new_domains, proxy_target, force_ssl, env, db, docker, ssl, hosts, caddyfile, caddy_dir
        )
        return

    # Resolve PHP version from --php flag, .env, or fallback
    if php_version is None:
        php_version = resolve_default_php_version(env.get("DEFAULT_PHP_VERSION"))
    validate_php_version(php_version)
    warn_if_deprecated(php_version)

    # Check if we need to start a new PHP version container
    active_versions = db.get_active_php_versions()
    need_new_container = php_version not in active_versions

    # Ensure php.ini exists for this version
    ensure_php_version_config(project_dir, php_version)

    hosts_added: list[str] = []

    try:
        log_info(f"Adding {len(new_domains)} new domain(s) with PHP {php_version}...")

        # Generate SSL certificates for new domains
        log_info("Generating SSL certificates...")
        ssl.generate_all(new_domains, force_ssl, env.is_production())

        # Add hosts entries for new domains
        log_info("Adding hosts entries...")
        for domain in new_domains:
            hosts.add_entry("127.0.0.1", domain)
            hosts_added.append(domain)

        # Generate Caddyfiles for new domains
        log_info("Generating Caddyfiles...")
        caddyfile.generate_for_version(new_domains, php_version)

        # Update database with new domains
        db.add_domains(new_domains, php_version)

        # Regenerate main reverse proxy Caddyfile
        all_domains_versions = db.get_domains_with_versions()
        caddyfile.generate_main_caddyfile(
            all_domains_versions,
            caddy_dir,
            env.is_production(),
            db.get_alias_entries(),
            db.get_proxies(),
            db_admin_target=env.get_db_admin_target(),
        )

        # Keep the LAN DNS server records in step (no-op unless DNS_SERVER=true)
        dns_server.sync(project_dir, env, db)

        if need_new_container:
            # Build the image if needed, then start the new container via compose
            custom_path = get_project_path(env, db)
            if not custom_path:
                raise ServerStateError(
                    "No project path known: set DEFAULT_PROJECT_PATH in .env "
                    "or restart the server with --path."
                )
            docker.build_image_from_env(custom_path, php_version, env)

            log_info(f"Starting PHP {php_version} container...")

            compose_up(project_dir, docker, env, active_versions | {php_version}, custom_path)
        else:
            # Restart the existing PHP container and the reverse proxy
            container_name = get_container_name(php_version)
            log_info(f"Restarting {container_name}...")
            docker.restart_container(container_name)

        # Restart the reverse proxy to pick up new routes
        from ..core.docker_manager import REVERSE_PROXY_CONTAINER  # noqa: PLC0415

        log_info("Restarting reverse proxy...")
        docker.restart_container(REVERSE_PROXY_CONTAINER)

        print()
        log_success("New host(s) added successfully!")
        log_info("Added domains:")
        for domain in new_domains:
            log_info(f"  - https://{domain} (PHP {php_version})")

    except Exception as e:
        # Cleanup on failure
        log_error(f"An error occurred: {e}")
        log_error("Cleaning up...")

        for domain in hosts_added:
            try:
                hosts.remove_entry("127.0.0.1", domain)
            except Exception:
                pass

        log_error("Cleanup complete. The host(s) were not added.")
        raise


def _add_proxy_hosts(
    new_domains: list[str],
    target: str,
    force_ssl: bool,
    env: EnvironmentManager,
    db: DatabaseManager,
    docker: DockerManager,
    ssl: SSLManager,
    hosts: HostsManager,
    caddyfile: CaddyfileGenerator,
    caddy_dir: Path,
) -> None:
    """Add new domain(s) that reverse-proxy straight to a raw upstream address.

    Unlike a regular host, these domains get no PHP container and no per-site
    Caddyfile: each gets its own SSL certificate and hosts entry, and is added
    to the main reverse-proxy Caddyfile, forwarding directly to `target`.

    Args:
        new_domains: List of new domain names to add.
        target: Raw upstream address (e.g. "127.0.0.1:8006", "http://host:port").
        force_ssl: Whether to force SSL certificate regeneration.
        env: Environment manager.
        db: Database manager.
        docker: Docker manager.
        ssl: SSL manager.
        hosts: Hosts file manager.
        caddyfile: Caddyfile generator.
        caddy_dir: Path to the caddy directory.
    """
    validate_proxy_target(target)

    hosts_added: list[str] = []

    try:
        log_info(f"Adding {len(new_domains)} new proxy host(s) -> {target}...")

        # Generate SSL certificates for the new domains
        log_info("Generating SSL certificates...")
        ssl.generate_all(new_domains, force_ssl, env.is_production())

        # Add hosts entries for the new domains
        log_info("Adding hosts entries...")
        for domain in new_domains:
            hosts.add_entry("127.0.0.1", domain)
            hosts_added.append(domain)

        # Record the proxy hosts (no per-site Caddyfile, no new PHP container)
        db.add_proxies(new_domains, target)

        # Regenerate main reverse proxy Caddyfile with the new proxy hosts included
        all_domains_versions = db.get_domains_with_versions()
        caddyfile.generate_main_caddyfile(
            all_domains_versions,
            caddy_dir,
            env.is_production(),
            db.get_alias_entries(),
            db.get_proxies(),
            db_admin_target=env.get_db_admin_target(),
        )

        # Keep the LAN DNS server records in step (no-op unless DNS_SERVER=true)
        dns_server.sync(docker.project_dir, env, db)

        # Restart the reverse proxy to pick up the new routes
        from ..core.docker_manager import REVERSE_PROXY_CONTAINER  # noqa: PLC0415

        log_info("Restarting reverse proxy...")
        docker.restart_container(REVERSE_PROXY_CONTAINER)

        print()
        log_success("New proxy host(s) added successfully!")
        log_info("Added domains:")
        for domain in new_domains:
            log_info(f"  - https://{domain} -> {target}")

    except Exception as e:
        # Cleanup on failure
        log_error(f"An error occurred: {e}")
        log_error("Cleaning up...")

        for domain in hosts_added:
            try:
                hosts.remove_entry("127.0.0.1", domain)
            except Exception:
                pass

        try:
            db.remove_proxies(new_domains)
        except Exception:
            pass

        log_error("Cleanup complete. The proxy host(s) were not added.")
        raise
