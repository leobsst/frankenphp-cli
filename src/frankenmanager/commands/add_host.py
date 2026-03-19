"""Add host command implementation."""

from pathlib import Path
from typing import Optional

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
from ..utils.validation import validate_domain


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


def add_host(domains: list[str], force_ssl: bool, php_version: Optional[str] = None) -> None:
    """Add new host(s) to the running server.

    Args:
        domains: List of new domain names to add.
        force_ssl: Whether to force SSL certificate regeneration.
        php_version: PHP version for the new domains (None = use .env or fallback).
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

    # Validate new domains and filter out duplicates
    new_domains: list[str] = []
    for domain in domains:
        validate_domain(domain)
        if domain in existing_domains:
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
        caddyfile.generate_main_caddyfile(all_domains_versions, caddy_dir, env.is_production())

        if need_new_container:
            # Build and start a new container for this PHP version
            log_info(f"Building Docker image for PHP {php_version}...")
            custom_path = env.get("DEFAULT_PROJECT_PATH") or ""
            docker.build_image(custom_path, php_version, env.get("WWWGROUP") or "")

            # Regenerate compose file with the new version
            all_versions = active_versions | {php_version}
            docker.generate_compose_file(all_versions, {}, env.is_production())

            # Restart all via compose to start the new container
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
