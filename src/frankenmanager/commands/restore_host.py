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
from ..core.resources import get_project_dir
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
    caddyfile = CaddyfileGenerator(project_dir, caddy_dir / "sites" / "custom")

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


def restore_host(domains: list[str], force_ssl: bool) -> None:
    """Restore host(s) from archive to the running server.

    Args:
        domains: List of domain names to restore.
        force_ssl: Whether to force SSL certificate regeneration.
    """
    project_dir = get_project_dir()

    # Initialize managers
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    env.load()

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
    caddyfile = CaddyfileGenerator(project_dir, caddy_dir / "sites" / "custom")

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

    hosts_added: list[str] = []

    try:
        log_info(f"Restoring {len(domains_to_restore)} domain(s)...")

        # Restore Caddyfiles from archive
        log_info("Restoring Caddyfiles...")
        restored_domains = caddyfile.restore(domains_to_restore)

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
        db.add_domains(restored_domains)

        # Restart only the webserver container to apply changes
        log_info("Restarting FrankenPHP container...")
        if docker.restart_container("webserver-and-caddy"):
            print()
            log_success("Host(s) restored successfully!")
            log_info("Restored domains:")
            for domain in restored_domains:
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

        log_error("Cleanup complete. The host(s) were not restored.")
        raise
