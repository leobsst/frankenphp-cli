"""Switch PHP version command implementation."""

from pathlib import Path
from typing import Optional

from ..core.caddyfile import CaddyfileGenerator
from ..core.database import DatabaseManager
from ..core.docker_manager import REVERSE_PROXY_CONTAINER, DockerManager
from ..core.environment import EnvironmentManager
from ..core.php_versions import get_container_name, validate_php_version
from ..core.resources import ensure_php_version_config, get_project_dir
from ..exceptions import ServerStateError
from ..utils.logging import log_error, log_info, log_success


def _resolve_path(env_value: Optional[str], default: str, project_dir: Path) -> Path:
    """Resolve a path from environment variable or default."""
    path_str = env_value if env_value else default
    path = Path(path_str)
    if not path.is_absolute():
        path = project_dir / path
    return path


def switch_php(domain: str, php_version: str) -> None:
    """Switch the PHP version for a domain.

    Args:
        domain: The domain name to switch.
        php_version: The target PHP version.
    """
    validate_php_version(php_version)

    project_dir = get_project_dir()

    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    env.load()

    docker = DockerManager(project_dir)
    db = DatabaseManager(project_dir / "db.sqlite", docker)

    # Check state - server must be running
    if not db.is_running:
        raise ServerStateError("The server is not running.")

    # Check domain exists
    current_version = db.get_domain_php_version(domain)
    if current_version is None:
        log_error(f"Domain '{domain}' is not configured.")
        return

    if current_version == php_version:
        log_info(f"Domain '{domain}' is already using PHP {php_version}.")
        return

    log_info(f"Switching '{domain}' from PHP {current_version} to PHP {php_version}...")

    # Ensure php.ini exists for target version
    ensure_php_version_config(project_dir, php_version)

    # Resolve paths
    caddy_dir = _resolve_path(env.get("CADDY_DIR"), "./caddy", project_dir)
    caddyfile = CaddyfileGenerator(project_dir, caddy_dir / "sites")

    # Track versions before the switch
    versions_before = db.get_active_php_versions()
    need_new_container = php_version not in versions_before

    # Move Caddyfile from old version dir to new version dir
    caddyfile.move_to_version(domain, current_version, php_version)

    # Update the database
    db.update_domain_php_version(domain, php_version)

    # Check if old version still has domains
    versions_after = db.get_active_php_versions()
    orphaned_versions = versions_before - versions_after

    try:
        if need_new_container:
            # Build and start a new container for the target PHP version
            log_info(f"Building Docker image for PHP {php_version}...")
            custom_path = env.get("DEFAULT_PROJECT_PATH") or ""
            docker.build_image(custom_path, php_version, env.get("WWWGROUP") or "")

            # Regenerate compose file
            docker.generate_compose_file(versions_after, {}, env.is_production())

            # Start the new container via compose up
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
            # Target version container already exists, just restart it
            container_name = get_container_name(php_version)
            log_info(f"Restarting {container_name}...")
            docker.restart_container(container_name)

        # Restart old version container if it still has domains (removed a site from it)
        if current_version in versions_after:
            old_container = get_container_name(current_version)
            log_info(f"Restarting {old_container}...")
            docker.restart_container(old_container)

        # Stop containers for versions that no longer have domains
        for version in orphaned_versions:
            container_name = get_container_name(version)
            log_info(f"Stopping {container_name} (no more domains)...")
            docker.stop_container(container_name)

        # Update compose file if versions changed
        if orphaned_versions:
            docker.generate_compose_file(versions_after, {}, env.is_production())

        # Regenerate main reverse proxy Caddyfile and restart proxy
        all_domains_versions = db.get_domains_with_versions()
        caddyfile.generate_main_caddyfile(all_domains_versions, caddy_dir, env.is_production())

        log_info("Restarting reverse proxy...")
        docker.restart_container(REVERSE_PROXY_CONTAINER)

        print()
        log_success(f"Switched '{domain}' to PHP {php_version}!")

    except Exception as e:
        # Rollback database and caddyfile changes on failure
        log_error(f"An error occurred: {e}")
        caddyfile.move_to_version(domain, php_version, current_version)
        db.update_domain_php_version(domain, current_version)
        log_error("Rolled back version change.")
        raise
