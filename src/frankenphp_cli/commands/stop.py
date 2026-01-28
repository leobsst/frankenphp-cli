"""Stop command implementation."""

from pathlib import Path

from ..core.config import ConfigManager
from ..core.docker_manager import DockerManager
from ..core.environment import EnvironmentManager
from ..core.hosts_manager import HostsManager
from ..exceptions import ServerStateError
from ..utils.logging import log_info, log_success


def get_project_dir() -> Path:
    """Get the project directory."""
    return Path.cwd()


def stop_server() -> None:
    """Stop the FrankenPHP server."""
    project_dir = get_project_dir()

    config = ConfigManager(project_dir / ".config")
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")

    if env.env_path.exists():
        env.load()

    docker = DockerManager(project_dir)
    hosts = HostsManager()

    # Check state
    if not config.is_running:
        raise ServerStateError("The server is already stopped.")

    log_info("Stopping web server...")

    # Stop containers
    docker.compose_down(env.is_production() if env.env_path.exists() else False)

    # Remove hosts entries
    for domain in config.get_domains():
        try:
            hosts.remove_entry("127.0.0.1", domain)
        except Exception:
            pass  # Ignore errors when removing hosts entries

    # Reset config
    config.reset()

    print()
    log_success("Web server stopped!")
