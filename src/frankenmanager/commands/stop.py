"""Stop command implementation."""

from ..core.database import DatabaseManager
from ..core.docker_manager import DockerManager
from ..core.environment import EnvironmentManager
from ..core.hosts_manager import HostsManager
from ..core.resources import get_project_dir
from ..exceptions import ServerStateError
from ..utils.logging import log_info, log_success


def stop_server() -> None:
    """Stop the FrankenPHP server."""
    project_dir = get_project_dir()

    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")

    if env.env_path.exists():
        env.load()

    docker = DockerManager(project_dir)
    db = DatabaseManager(project_dir / "db.sqlite", docker)
    hosts = HostsManager()

    # Check state
    if not db.is_running:
        raise ServerStateError("The server is already stopped.")

    log_info("Stopping web server...")

    # Stop containers
    docker.compose_down(env.is_production() if env.env_path.exists() else False)

    # Remove hosts entries
    for domain in db.get_domains():
        try:
            hosts.remove_entry("127.0.0.1", domain)
        except Exception:
            pass  # Ignore errors when removing hosts entries

    # Reset database
    db.reset()

    print()
    log_success("Web server stopped!")
