"""Restart command implementation."""

from ..core.config import ConfigManager
from ..core.docker_manager import DockerManager
from ..core.environment import EnvironmentManager
from ..core.password_manager import PasswordManager
from ..core.resources import get_project_dir
from ..core.ssl_manager import SSLManager
from ..exceptions import ServerStateError
from ..utils.logging import log_info, log_success


def restart_server(force_ssl: bool) -> None:
    """Restart the FrankenPHP server.

    Args:
        force_ssl: Whether to force SSL certificate regeneration.
    """
    project_dir = get_project_dir()

    config = ConfigManager(project_dir / ".config")
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    env.load()

    docker = DockerManager(project_dir)
    certs_dir = project_dir / (env.get("CERTS_DIR") or "./caddy/certs")
    ssl = SSLManager(certs_dir)
    password_manager = PasswordManager(project_dir, docker)

    # Check state
    if not config.is_running:
        raise ServerStateError("The server is not running.")

    log_info("Restarting web server...")

    # Regenerate SSL if requested
    domains = config.get_domains()
    print()
    log_info("Generating SSL certificates...")
    ssl.generate_all(domains, force_ssl, env.is_production())

    # Restart containers
    docker.restart_all()

    # Sync password
    password_manager.sync_password(env.require("MARIADB_ROOT_PASSWORD"))

    print()
    log_success("Web server restarted!")
