"""Add alias host command implementation."""

from pathlib import Path
from typing import Optional

from ..core.caddyfile import CaddyfileGenerator
from ..core.database import DatabaseManager
from ..core.docker_manager import DockerManager
from ..core.environment import EnvironmentManager
from ..core.hosts_manager import HostsManager
from ..core.resources import get_project_dir
from ..core.ssl_manager import SSLManager
from ..exceptions import ServerStateError, ValidationError
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


def add_alias(target_domain: str, alias_domains: list[str], force_ssl: bool) -> None:
    """Add alternate host(s) that reverse-proxy to an already-configured domain.

    Unlike `add_host`, this does not create a per-site Caddyfile or a new PHP
    container: each alias gets its own SSL certificate and hosts entry, and is
    added to the main reverse-proxy Caddyfile, forwarding to the target
    domain's existing site.

    Args:
        target_domain: Existing domain to attach the alias(es) to.
        alias_domains: List of new alias domain names to add.
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

    # The target must be a real, already-registered domain (not itself an alias)
    existing_domains = db.get_domains()
    if target_domain not in existing_domains:
        raise ValidationError(
            f"Domain '{target_domain}' is not configured. Add it first with 'add-host'."
        )

    php_version = db.get_domain_php_version(target_domain)
    existing_aliases = {alias for alias, _ in db.get_aliases()}

    # Validate new aliases and filter out duplicates
    new_aliases: list[str] = []
    for domain in alias_domains:
        validate_domain(domain)
        if domain == target_domain:
            log_info(f"'{domain}' is the target domain itself, skipping.")
        elif domain in existing_domains:
            log_info(f"Domain '{domain}' is already a registered host, skipping.")
        elif domain in existing_aliases:
            log_info(f"'{domain}' is already configured as an alias, skipping.")
        elif domain in new_aliases:
            log_info(f"'{domain}' is duplicated in input, skipping.")
        else:
            new_aliases.append(domain)

    if not new_aliases:
        log_info("No new alternate hosts to add.")
        return

    # Resolve storage paths from environment
    caddy_dir = _resolve_path(env.get("CADDY_DIR"), "./caddy", project_dir)

    ssl = SSLManager(caddy_dir / "certs")
    hosts = HostsManager()
    caddyfile = CaddyfileGenerator(project_dir, caddy_dir / "sites")

    hosts_added: list[str] = []

    try:
        log_info(f"Adding {len(new_aliases)} alternate host(s) for '{target_domain}'...")

        # Generate SSL certificates for the alias domains
        log_info("Generating SSL certificates...")
        ssl.generate_all(new_aliases, force_ssl, env.is_production())

        # Add hosts entries for the alias domains
        log_info("Adding hosts entries...")
        for domain in new_aliases:
            hosts.add_entry("127.0.0.1", domain)
            hosts_added.append(domain)

        # Record the aliases (no per-site Caddyfile, no new PHP container)
        db.add_aliases(new_aliases, target_domain)

        # Regenerate main reverse proxy Caddyfile with the new aliases included
        all_domains_versions = db.get_domains_with_versions()
        caddyfile.generate_main_caddyfile(
            all_domains_versions,
            caddy_dir,
            env.is_production(),
            db.get_alias_entries(),
            db.get_proxies(),
        )

        # Restart the reverse proxy to pick up the new routes
        from ..core.docker_manager import REVERSE_PROXY_CONTAINER  # noqa: PLC0415

        log_info("Restarting reverse proxy...")
        docker.restart_container(REVERSE_PROXY_CONTAINER)

        print()
        log_success("Alternate host(s) added successfully!")
        log_info("Added aliases:")
        for domain in new_aliases:
            log_info(f"  - https://{domain} -> {target_domain} (PHP {php_version})")

    except Exception as e:
        # Cleanup on failure
        log_error(f"An error occurred: {e}")
        log_error("Cleaning up...")

        for domain in hosts_added:
            try:
                hosts.remove_entry("127.0.0.1", domain)
            except Exception:
                pass

        try:
            db.remove_aliases(new_aliases)
        except Exception:
            pass

        log_error("Cleanup complete. The alternate host(s) were not added.")
        raise
