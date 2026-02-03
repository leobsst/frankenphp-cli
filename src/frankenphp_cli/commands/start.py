"""Start command implementation."""

import sys
from pathlib import Path

from ..core.caddyfile import CaddyfileGenerator
from ..core.config import ConfigManager
from ..core.docker_manager import DockerManager
from ..core.environment import EnvironmentManager
from ..core.hosts_manager import HostsManager
from ..core.password_manager import PasswordManager
from ..core.resources import get_project_dir
from ..core.ssl_manager import SSLManager
from ..exceptions import ServerStateError
from ..utils.logging import log_error, log_info, log_success
from ..utils.validation import validate_directory, validate_domain


def start_server(domains: list[str], custom_path: Path, force_ssl: bool) -> None:
    """Start the FrankenPHP server.

    Args:
        domains: List of domain names to serve.
        custom_path: Path to the project root.
        force_ssl: Whether to force SSL certificate regeneration.
    """
    project_dir = get_project_dir()

    # Initialize managers
    config = ConfigManager(project_dir / ".config")
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")

    # Ensure .env exists
    if not env.ensure_env_exists():
        log_error("Created .env file. Please review the settings, then try again.")
        sys.exit(1)

    env.load()

    # Check required environment variables
    try:
        env.require("UID")
        env.require("GID")
    except Exception as e:
        log_error(str(e))
        log_error("Please set UID and GID in .env file.")
        sys.exit(1)

    # Generate password if not set
    if not env.get("MARIADB_ROOT_PASSWORD"):
        password = env.generate_mariadb_password()
        env.set("MARIADB_ROOT_PASSWORD", password)
        log_info("MariaDB password generated and saved to .env")
        env.load()  # Reload to get the new password

    docker = DockerManager(project_dir)
    certs_dir = project_dir / (env.get("CERTS_DIR") or "./caddy/certs")
    ssl = SSLManager(certs_dir)
    hosts = HostsManager()
    caddyfile = CaddyfileGenerator(project_dir)
    password_manager = PasswordManager(project_dir, docker)

    # Check state
    if config.is_running:
        raise ServerStateError("The server is already running.")

    # Validate inputs
    validate_directory(custom_path)
    for domain in domains:
        validate_domain(domain)

    # Remove duplicates while preserving order
    domains = list(dict.fromkeys(domains))

    hosts_added: list[str] = []

    try:
        # Generate SSL certificates
        ssl.generate_all(domains, force_ssl, env.is_production())

        # Add hosts entries
        for domain in domains:
            hosts.add_entry("127.0.0.1", domain)
            hosts_added.append(domain)

        # Generate Caddyfiles
        caddyfile.generate(domains)

        # Update config to running
        config.set_running(domains)

        # Build and start containers
        log_info("Starting web server...")
        docker.build_image(str(custom_path), env.get("WWWGROUP") or "")
        docker.compose_down(env.is_production())

        # Prepare environment variables for docker-compose
        expose = env.get("EXPOSE_SERVICES") == "true"
        localhost = "" if expose else "127.0.0.1:"

        # Get port configurations from .env (with defaults)
        db_port = env.get("DB_PORT") or "3306"
        pma_port = env.get("PMA_PORT") or "8080"
        redis_port = env.get("REDIS_PORT") or "6379"

        env_vars = {
            "CUSTOM_PATH": str(custom_path),
            "UID": env.require("UID"),
            "GID": env.require("GID"),
            "DB_PORT": f"{localhost}{db_port}:3306",
            "PMA_PORT": f"{localhost}{pma_port}:80",
            "REDIS_PORT": f"{localhost}{redis_port}:6379",
            "MARIADB_ROOT_PASSWORD": env.require("MARIADB_ROOT_PASSWORD"),
            "MYSQL_MAX_ALLOWED_PACKET": env.get("MYSQL_MAX_ALLOWED_PACKET") or "512M",
            "PWD": str(project_dir),
        }
        docker.compose_up(env_vars, env.is_production())

        # Sync database password
        password_manager.sync_password(env.require("MARIADB_ROOT_PASSWORD"))

        print()
        log_success("Web server started!")

    except Exception as e:
        # Cleanup on failure
        log_error(f"An error occurred: {e}")
        log_error("Cleaning up...")

        for domain in hosts_added:
            try:
                hosts.remove_entry("127.0.0.1", domain)
            except Exception:
                pass

        config.reset()
        log_error("Cleanup complete. The server did not start.")
        raise
