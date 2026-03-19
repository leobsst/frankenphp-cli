"""Restore host command implementation."""

from pathlib import Path
from typing import Optional

from rich.console import Console
from rich.table import Table

from ..core.caddyfile import CaddyfileGenerator
from ..core.database import DatabaseManager
from ..core.docker_manager import DockerManager
from ..core.environment import EnvironmentManager
from ..core.hosts_manager import HostsManager
from ..core.php_versions import (
    get_container_name,
    resolve_default_php_version,
    validate_php_version,
)
from ..core.resources import ensure_php_version_config, get_project_dir
from ..core.ssl_manager import SSLManager
from ..exceptions import ServerStateError
from ..utils.logging import log_error, log_info, log_success

console = Console()


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


def list_archived_hosts() -> None:
    """List all archived hosts."""
    project_dir = get_project_dir()
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    env.load()

    caddy_dir = _resolve_path(env.get("CADDY_DIR"), "./caddy", project_dir)
    caddyfile = CaddyfileGenerator(project_dir, caddy_dir / "sites")

    archived = caddyfile.list_archived()

    if not archived:
        log_info("No archived hosts found.")
        log_info(f"Archive directory: {caddyfile.archive_dir}")
        return

    table = Table(title="Archived Hosts")
    table.add_column("Domain", style="cyan")
    table.add_column("Caddyfile", style="dim")

    for item in archived:
        table.add_row(
            item["full_domain"],
            f"{item['simple_domain']}_Caddyfile",
        )

    console.print(table)
    log_info(f"\nArchive directory: {caddyfile.archive_dir}")
    log_info("Use 'frankenmanager restore-host \"domain.test\"' to restore a host.")


def restore_host(domains: list[str], force_ssl: bool, php_version: Optional[str] = None) -> None:
    """Restore host(s) from archive to the running server.

    Args:
        domains: List of domain names to restore.
        force_ssl: Whether to force SSL certificate regeneration.
        php_version: PHP version for the restored domains (None = use .env or fallback).
    """
    project_dir = get_project_dir()

    # Initialize managers
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    env.load()

    # Resolve PHP version from --php flag, .env, or fallback
    if php_version is None:
        php_version = resolve_default_php_version(env.get("DEFAULT_PHP_VERSION"))
    validate_php_version(php_version)

    docker = DockerManager(project_dir)
    db = DatabaseManager(project_dir / "db.sqlite", docker)

    # Check state - server must be running
    if not db.is_running:
        raise ServerStateError("The server is not running. Use 'start' command instead.")

    # Get existing domains
    existing_domains = db.get_domains()

    # Resolve storage paths from environment
    caddy_dir = _resolve_path(env.get("CADDY_DIR"), "./caddy", project_dir)

    ssl = SSLManager(caddy_dir / "certs")
    hosts = HostsManager()
    caddyfile = CaddyfileGenerator(project_dir, caddy_dir / "sites")

    # Filter out domains that are already active
    domains_to_restore: list[str] = []
    for domain in domains:
        # Check if already active (by simple domain name)
        simple_domain = domain.rsplit(".", 1)[0] if "." in domain else domain
        is_active = any(d.rsplit(".", 1)[0] == simple_domain for d in existing_domains)

        if is_active:
            log_info(f"Domain '{domain}' is already active, skipping.")
        elif domain in domains_to_restore:
            log_info(f"Domain '{domain}' is duplicated in input, skipping.")
        else:
            domains_to_restore.append(domain)

    if not domains_to_restore:
        log_info("No domains to restore.")
        return

    # Check if we need a new PHP version container
    active_versions = db.get_active_php_versions()
    need_new_container = php_version not in active_versions

    # Ensure php.ini exists for this version
    ensure_php_version_config(project_dir, php_version)

    hosts_added: list[str] = []

    try:
        log_info(f"Restoring {len(domains_to_restore)} domain(s) with PHP {php_version}...")

        # Restore Caddyfiles from archive into version directory
        log_info("Restoring Caddyfiles...")
        restored_domains = caddyfile.restore_to_version(domains_to_restore, php_version)

        if not restored_domains:
            log_info("No Caddyfiles were restored.")
            return

        # Generate SSL certificates for restored domains
        log_info("Generating SSL certificates...")
        ssl.generate_all(restored_domains, force_ssl, env.is_production())

        # Add hosts entries for restored domains
        log_info("Adding hosts entries...")
        for domain in restored_domains:
            hosts.add_entry("127.0.0.1", domain)
            hosts_added.append(domain)

        # Update database with restored domains
        db.add_domains(restored_domains, php_version)

        if need_new_container:
            # Build and start a new container for this PHP version
            log_info(f"Building Docker image for PHP {php_version}...")
            custom_path = env.get("DEFAULT_PROJECT_PATH") or ""
            docker.build_image(custom_path, php_version, env.get("WWWGROUP") or "")

            # Regenerate compose file with the new version
            all_versions = active_versions | {php_version}
            docker.generate_compose_file(all_versions, {}, env.is_production())

            log_info(f"Starting PHP {php_version} container...")
            expose = env.get("EXPOSE_SERVICES") == "true"
            localhost = "" if expose else "127.0.0.1:"
            db_port = env.get("DB_PORT") or "3306"
            pma_port = env.get("PMA_PORT") or "8080"
            redis_port = env.get("REDIS_PORT") or "6379"
            database_dir = _resolve_path(env.get("DATABASE_DIR"), "./database", project_dir)

            env_vars = {
                "CUSTOM_PATH": custom_path,
                "UID": env.require("UID"),
                "GID": env.require("GID"),
                "SIMPLE_DB_PORT": db_port,
                "DB_PORT": f"{localhost}{db_port}:3306",
                "PMA_PORT": f"{localhost}{pma_port}:80",
                "REDIS_PORT": f"{localhost}{redis_port}:6379",
                "WEB_HTTP_PORT": env.get("WEB_HTTP_PORT") or "80",
                "WEB_HTTPS_PORT": env.get("WEB_HTTPS_PORT") or "443",
                "MARIADB_ROOT_PASSWORD": env.require("MARIADB_ROOT_PASSWORD"),
                "MYSQL_MAX_ALLOWED_PACKET": env.get("MYSQL_MAX_ALLOWED_PACKET") or "512M",
                "PWD": str(project_dir),
                "CADDY_DIR": str(caddy_dir),
                "DATABASE_DIR": str(database_dir),
            }
            docker.compose_up(env_vars, env.is_production())
        else:
            # Just restart the existing container
            container_name = get_container_name(php_version)
            log_info(f"Restarting {container_name}...")
            docker.restart_container(container_name)

        # Regenerate main reverse proxy Caddyfile and restart proxy
        all_domains_versions = db.get_domains_with_versions()
        caddyfile.generate_main_caddyfile(all_domains_versions, caddy_dir, env.is_production())

        from ..core.docker_manager import REVERSE_PROXY_CONTAINER  # noqa: PLC0415

        log_info("Restarting reverse proxy...")
        docker.restart_container(REVERSE_PROXY_CONTAINER)

        print()
        log_success("Host(s) restored successfully!")
        log_info("Restored domains:")
        for domain in restored_domains:
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

        log_error("Cleanup complete. The host(s) were not restored.")
        raise
