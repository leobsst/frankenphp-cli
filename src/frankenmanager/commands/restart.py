"""Restart command implementation."""

from typing import Optional

from ..core.database import DatabaseManager
from ..core.docker_manager import DockerManager
from ..core.environment import EnvironmentManager
from ..core.password_manager import PasswordManager
from ..core.php_versions import get_container_name
from ..core.resources import get_project_dir
from ..core.ssl_manager import SSLManager
from ..exceptions import ServerStateError
from ..utils.logging import log_info, log_success

# Map friendly names to actual container names
CONTAINER_MAP = {
    "database": "franken_mariadb",
    "cache": "franken_redis",
    "phpmyadmin": "franken_phpmyadmin",
}


def restart_server(force_ssl: bool, containers: Optional[list[str]] = None) -> None:
    """Restart the FrankenPHP server or specific containers.

    Args:
        force_ssl: Whether to force SSL certificate regeneration.
        containers: List of container names to restart (None = restart all).
                   Valid values: "caddy", "database", "cache", "phpmyadmin"
    """
    project_dir = get_project_dir()

    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    env.load()

    docker = DockerManager(project_dir)
    db = DatabaseManager(project_dir / "db.sqlite", docker)
    certs_dir = project_dir / (env.get("CERTS_DIR") or "./caddy/certs")
    ssl = SSLManager(certs_dir)
    password_manager = PasswordManager(project_dir, docker)

    # Check state
    if not db.is_running:
        raise ServerStateError("The server is not running.")

    # Get active PHP versions
    active_versions = db.get_active_php_versions()

    # Determine if we're restarting all or specific containers
    restart_all = containers is None or len(containers) == 0
    restart_caddy = restart_all or (containers is not None and "caddy" in containers)
    restart_db = restart_all or (containers is not None and "database" in containers)

    if restart_all:
        log_info("Restarting web server...")
    else:
        container_names = ", ".join(containers) if containers else ""
        log_info(f"Restarting {container_names}...")

    # Regenerate SSL if requested and restarting Caddy
    if restart_caddy:
        domains = db.get_domains()
        print()
        log_info("Generating SSL certificates...")
        ssl.generate_all(domains, force_ssl, env.is_production())

    # Restart containers
    if restart_all:
        docker.restart_all(active_versions)
    else:
        if containers:
            for container_name in containers:
                if container_name == "caddy":
                    # Restart all FrankenPHP containers and the reverse proxy
                    for version in sorted(active_versions):
                        docker.restart_container(get_container_name(version))
                    from ..core.docker_manager import REVERSE_PROXY_CONTAINER  # noqa: PLC0415

                    docker.restart_container(REVERSE_PROXY_CONTAINER)
                else:
                    actual_name = CONTAINER_MAP.get(container_name)
                    if actual_name:
                        docker.restart_container(actual_name)

    # Sync password if restarting database
    if restart_db:
        password_manager.sync_password(env.require("MARIADB_ROOT_PASSWORD"))

    print()
    if restart_all:
        log_success("Web server restarted!")
    else:
        container_list = ", ".join(containers) if containers else ""
        log_success(f"Container(s) restarted: {container_list}")
