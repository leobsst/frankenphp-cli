"""Remove host command implementation."""

from pathlib import Path
from typing import Optional

from ..core.caddyfile import CaddyfileGenerator
from ..core.database import DatabaseManager
from ..core.docker_manager import DockerManager
from ..core.environment import EnvironmentManager
from ..core.hosts_manager import HostsManager
from ..core.php_versions import get_container_name
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
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    env.load()

    docker = DockerManager(project_dir)
    db = DatabaseManager(project_dir / "db.sqlite", docker)

    # Check state - server must be running
    if not db.is_running:
        raise ServerStateError("The server is not running.")

    # Get existing domains with versions
    existing = db.get_domains_with_versions()
    existing_domains = [d for d, _ in existing]
    domain_version_map = {d: v for d, v in existing}

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
    caddyfile = CaddyfileGenerator(project_dir, caddy_dir / "sites")

    # Determine which PHP versions will lose all their domains
    versions_before = db.get_active_php_versions()
    versions_to_remove_from: dict[str, list[str]] = {}
    for domain in domains_to_remove:
        version = domain_version_map[domain]
        versions_to_remove_from.setdefault(version, []).append(domain)

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

        # Update database to remove domains
        db.remove_domains(domains_to_remove)

        # Check which versions still have domains after removal
        versions_after = db.get_active_php_versions()
        orphaned_versions = versions_before - versions_after

        # Stop containers for versions that have no more domains
        for version in orphaned_versions:
            container_name = get_container_name(version)
            log_info(f"Stopping {container_name} (no more domains)...")
            docker.stop_container(container_name)

        # Restart remaining FrankenPHP containers to apply Caddyfile changes
        affected_versions = {v for v in versions_to_remove_from if v in versions_after}
        for version in affected_versions:
            container_name = get_container_name(version)
            log_info(f"Restarting {container_name}...")
            docker.restart_container(container_name)

        # Regenerate compose file without orphaned versions
        if orphaned_versions:
            docker.generate_compose_file(versions_after, {}, env.is_production())

        # Regenerate main reverse proxy Caddyfile
        remaining_domains_versions = db.get_domains_with_versions()
        caddyfile.generate_main_caddyfile(
            remaining_domains_versions, caddy_dir, env.is_production()
        )

        # Restart the reverse proxy
        from ..core.docker_manager import REVERSE_PROXY_CONTAINER  # noqa: PLC0415

        log_info("Restarting reverse proxy...")
        docker.restart_container(REVERSE_PROXY_CONTAINER)

        print()
        log_success("Host(s) removed successfully!")
        log_info("Removed domains:")
        for domain in domains_to_remove:
            log_info(f"  - {domain} (PHP {domain_version_map[domain]})")
        log_info(f"\nCaddyfiles archived to: {caddyfile.archive_dir}")

    except Exception as e:
        log_error(f"An error occurred: {e}")
        raise
