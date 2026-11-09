"""Restart command implementation."""

from pathlib import Path
from typing import Optional

from ..core import dns_server, dns_watcher, soketi
from ..core.database import DatabaseManager
from ..core.docker_manager import (
    DB_CONTAINERS,
    PHPMYADMIN_CONTAINER,
    REDIS_CONTAINER,
    DockerManager,
)
from ..core.environment import EnvironmentManager
from ..core.password_manager import PasswordManager
from ..core.php_versions import get_container_name
from ..core.resources import get_project_dir
from ..core.ssl_manager import SSLManager
from ..core.stack import compose_up, generate_compose, get_project_path
from ..exceptions import ServerStateError
from ..utils.logging import log_info, log_success, log_warning

# Map friendly names to actual container names (static ones)
CONTAINER_MAP = {
    "cache": REDIS_CONTAINER,
    "phpmyadmin": PHPMYADMIN_CONTAINER,
    "dns": dns_server.CONTAINER_NAME,
    "soketi": soketi.CONTAINER_NAME,
}


def restart_server(
    force_ssl: bool, containers: Optional[list[str]] = None, build: bool = False
) -> None:
    """Restart the FrankenPHP server or specific containers.

    Args:
        force_ssl: Whether to force SSL certificate regeneration.
        containers: List of container names to restart (None = restart all).
                   Valid values: "caddy", "database", "cache", "phpmyadmin", "dns",
                   "soketi"
        build: Rebuild the PHP images and recreate their containers first.
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
    restart_dns = restart_all or (containers is not None and "dns" in containers)

    if restart_all:
        log_info("Restarting web server...")
    else:
        container_names = ", ".join(containers) if containers else ""
        log_info(f"Restarting {container_names}...")

    # Regenerate SSL if requested and restarting Caddy
    if restart_caddy:
        domains = db.get_domains() + soketi.get_domains(env)
        print()
        log_info("Generating SSL certificates...")
        ssl.generate_all(domains, force_ssl, env.is_production())

    db_engines = env.get_db_engines()
    redis = env.is_redis_enabled()
    websocket = soketi.is_enabled(env)

    if build and active_versions:
        custom_path = get_project_path(env, db)
        if not custom_path:
            raise ServerStateError(
                "No project path known: set DEFAULT_PROJECT_PATH in .env "
                "or use 'stop' then 'start --build --path <path>'."
            )
        docker.build_images(custom_path, active_versions, env, force=True)
        # Recreate the PHP containers on the new images
        compose_up(project_dir, docker, env, active_versions, custom_path)
    elif build:
        log_info("No PHP domain registered: no image to build.")

    if restart_dns:
        if dns_server.is_enabled(env):
            _refresh_dns(project_dir, docker, env, db, active_versions)
        # Picks up DNS_SERVER / DNS_IP changes made in .env since the last start
        dns_watcher.update(project_dir, env)

    # Restart containers
    if restart_all:
        docker.restart_all(
            active_versions,
            db_engines,
            env.is_production(),
            dns_server.is_enabled(env),
            redis,
            websocket,
        )
    else:
        if containers:
            for container_name in containers:
                if container_name == "caddy":
                    # Restart all FrankenPHP containers and the reverse proxy
                    for version in sorted(active_versions):
                        docker.restart_container(get_container_name(version))
                    from ..core.docker_manager import REVERSE_PROXY_CONTAINER  # noqa: PLC0415

                    docker.restart_container(REVERSE_PROXY_CONTAINER)
                elif container_name == "database":
                    # Restart all DB engine containers
                    for engine in db_engines:
                        db_container = DB_CONTAINERS.get(engine)
                        if db_container:
                            docker.restart_container(db_container)
                elif container_name == "cache" and not redis:
                    log_warning("Redis is disabled: set REDIS_ENABLED=true in .env to use it.")
                elif container_name == "soketi" and not websocket:
                    log_warning("Soketi is disabled: set SOKETI_ENABLED=true in .env to use it.")
                elif container_name == "dns" and not dns_server.is_enabled(env):
                    log_warning("The LAN DNS server is disabled: set DNS_SERVER=true in .env.")
                else:
                    actual_name = CONTAINER_MAP.get(container_name)
                    if actual_name:
                        docker.restart_container(actual_name)

    # Sync passwords if restarting database
    if restart_db:
        db_passwords = {}
        if "mariadb" in db_engines:
            db_passwords["mariadb"] = env.require("MARIADB_ROOT_PASSWORD")
        if "mysql" in db_engines:
            db_passwords["mysql"] = env.require("MYSQL_ROOT_PASSWORD")
        password_manager.sync_all_passwords(db_engines, db_passwords)

    print()
    if restart_all:
        log_success("Web server restarted!")
    else:
        container_list = ", ".join(containers) if containers else ""
        log_success(f"Container(s) restarted: {container_list}")


def _refresh_dns(
    project_dir: Path,
    docker: DockerManager,
    env: EnvironmentManager,
    db: DatabaseManager,
    active_versions: set[str],
) -> None:
    """Bring the LAN DNS server in line with the current network and .env.

    Rewrites the records with the current LAN IP, then recreates the DNS
    container if its published address changed since the last start (e.g. an
    older install bound to one IP, or DNS_LISTEN_IP edited). LAN IP changes
    while the server runs are handled by dns_watcher, without any restart.
    """
    dns_server.sync(project_dir, env, db)
    generate_compose(docker, env, active_versions)
    env_vars = env.build_compose_env_vars(
        project_dir, get_project_path(env, db), env.get_db_engines()
    )
    docker.compose_up(env_vars, env.is_production(), services=["dns"])
    log_info(f"LAN DNS server: domains resolve to {dns_server.resolve_ip(env)}")
