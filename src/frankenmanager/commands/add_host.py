"""Add host command implementation."""

from pathlib import Path
from typing import Optional

from ..core.caddyfile import CaddyfileGenerator
from ..core.config import ConfigManager
from ..core.docker_manager import DockerManager
from ..core.environment import EnvironmentManager
from ..core.hosts_manager import HostsManager
from ..core.resources import get_project_dir
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


def add_host(domains: list[str], force_ssl: bool) -> None:
    """Add new host(s) to the running server.

    Args:
        domains: List of new domain names to add.
        force_ssl: Whether to force SSL certificate regeneration.
    """
    project_dir = get_project_dir()

    # Initialize managers
    config = ConfigManager(project_dir / ".config")
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    env.load()

    docker = DockerManager(project_dir)

    # Check state - server must be running
    if not config.is_running:
        raise ServerStateError("The server is not running. Use 'start' command instead.")

    # Get existing domains
    existing_domains = config.get_domains()

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
    caddyfile = CaddyfileGenerator(project_dir, caddy_dir / "sites" / "custom")

    hosts_added: list[str] = []

    try:
        log_info(f"Adding {len(new_domains)} new domain(s)...")

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
        caddyfile.generate(new_domains)

        # Update config with new domains
        config.add_domains(new_domains)

        # Restart only the webserver container to apply changes
        log_info("Restarting FrankenPHP container...")
        if docker.restart_container("webserver-and-caddy"):
            print()
            log_success("New host(s) added successfully!")
            log_info("Added domains:")
            for domain in new_domains:
                log_info(f"  - https://{domain}")
        else:
            raise ServerStateError("Failed to restart webserver container.")

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
