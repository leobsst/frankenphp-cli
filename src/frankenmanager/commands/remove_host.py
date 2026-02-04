"""Remove host command implementation."""

from pathlib import Path
from typing import Optional

from ..core.caddyfile import CaddyfileGenerator
from ..core.config import ConfigManager
from ..core.docker_manager import DockerManager
from ..core.environment import EnvironmentManager
from ..core.hosts_manager import HostsManager
from ..core.resources import get_project_dir
from ..exceptions import ServerStateError
from ..utils.logging import log_error, log_info, log_success, log_warning


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


def remove_host(domains: list[str]) -> None:
    """Remove host(s) from the running server.

    Args:
        domains: List of domain names to remove.
    """
    project_dir = get_project_dir()

    # Initialize managers
    config = ConfigManager(project_dir / ".config")
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    env.load()

    docker = DockerManager(project_dir)

    # Check state - server must be running
    if not config.is_running:
        raise ServerStateError("The server is not running.")

    # Get existing domains
    existing_domains = config.get_domains()

    # Validate domains to remove
    domains_to_remove: list[str] = []
    for domain in domains:
        if domain not in existing_domains:
            log_warning(f"Domain '{domain}' is not configured, skipping.")
        elif domain in domains_to_remove:
            log_info(f"Domain '{domain}' is duplicated in input, skipping.")
        else:
            domains_to_remove.append(domain)

    if not domains_to_remove:
        log_info("No domains to remove.")
        return

    # Check if we're trying to remove all domains
    remaining_after_removal = [d for d in existing_domains if d not in domains_to_remove]
    if not remaining_after_removal:
        log_error("Cannot remove all domains. Use 'stop' command to stop the server entirely.")
        return

    # Resolve storage paths from environment
    caddy_dir = _resolve_path(env.get("CADDY_DIR"), "./caddy", project_dir)

    hosts = HostsManager()
    caddyfile = CaddyfileGenerator(project_dir, caddy_dir / "sites" / "custom")

    try:
        log_info(f"Removing {len(domains_to_remove)} domain(s)...")

        # Remove hosts entries
        log_info("Removing hosts entries...")
        for domain in domains_to_remove:
            try:
                hosts.remove_entry("127.0.0.1", domain)
            except Exception as e:
                log_warning(f"Failed to remove hosts entry for {domain}: {e}")

        # Archive Caddyfiles
        log_info("Archiving Caddyfiles...")
        caddyfile.archive(domains_to_remove)

        # Update config to remove domains
        config.remove_domains(domains_to_remove)

        # Restart only the webserver container to apply changes
        log_info("Restarting FrankenPHP container...")
        if docker.restart_container("webserver-and-caddy"):
            print()
            log_success("Host(s) removed successfully!")
            log_info("Removed domains:")
            for domain in domains_to_remove:
                log_info(f"  - {domain}")
            log_info(f"\nCaddyfiles archived to: {caddyfile.archive_dir}")
        else:
            raise ServerStateError("Failed to restart webserver container.")

    except Exception as e:
        log_error(f"An error occurred: {e}")
        raise
