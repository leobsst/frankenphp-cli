"""Start command implementation."""

import sys
from pathlib import Path
from typing import Optional

from ..core.caddyfile import CaddyfileGenerator
from ..core.database import DatabaseManager
from ..core.docker_manager import DockerManager, parse_db_engines
from ..core.environment import EnvironmentManager
from ..core.hosts_manager import HostsManager
from ..core.password_manager import PasswordManager
from ..core.php_versions import resolve_default_php_version, validate_php_version
from ..core.resources import ensure_php_version_config, get_project_dir
from ..core.ssl_manager import SSLManager
from ..exceptions import ServerStateError
from ..utils.logging import log_error, log_info, log_success
from ..utils.validation import validate_directory, validate_domain


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


def start_server(
    domains: Optional[list[str]],
    custom_path: Optional[Path],
    force_ssl: bool,
    php_version: Optional[str] = None,
) -> None:
    """Start the FrankenPHP server.

    Args:
        domains: List of domain names to serve (None to use registered domains from database).
        custom_path: Path to the project root (None to use DEFAULT_PROJECT_PATH).
        force_ssl: Whether to force SSL certificate regeneration.
        php_version: Default PHP version for new domains (None = use .env or fallback).
    """
    project_dir = get_project_dir()

    # Initialize managers
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")

    # Ensure .env exists
    if not env.ensure_env_exists():
        env_path = project_dir / ".env"
        log_info(f"Created .env file at {env_path}. Please review settings, then try again.")
        sys.exit(1)

    env.load()

    # Resolve PHP version from --php flag, .env, or fallback
    if php_version is None:
        php_version = resolve_default_php_version(env.get("DEFAULT_PHP_VERSION"))
    validate_php_version(php_version)

    # Check required environment variables
    try:
        env.require("UID")
        env.require("GID")
    except Exception as e:
        log_error(str(e))
        log_error("Please set UID and GID in .env file.")
        sys.exit(1)

    # Resolve project path
    if custom_path is None:
        default_path = env.get("DEFAULT_PROJECT_PATH")
        if not default_path:
            log_error("No project path specified and DEFAULT_PROJECT_PATH is not set in .env")
            sys.exit(1)
        custom_path = Path(default_path)

    # Parse DB engines configuration
    db_engines = parse_db_engines(env.get("DB_ENGINES") or "mariadb")
    if not db_engines:
        db_engines = ["mariadb"]

    # Generate passwords for enabled DB engines
    if "mariadb" in db_engines and not env.get("MARIADB_ROOT_PASSWORD"):
        password = env.generate_db_password()
        env.set("MARIADB_ROOT_PASSWORD", password)
        log_info("MariaDB password generated and saved to .env")

    if "mysql" in db_engines and not env.get("MYSQL_ROOT_PASSWORD"):
        password = env.generate_db_password()
        env.set("MYSQL_ROOT_PASSWORD", password)
        log_info("MySQL password generated and saved to .env")

    if "postgresql" in db_engines and not env.get("POSTGRES_PASSWORD"):
        password = env.generate_db_password()
        env.set("POSTGRES_PASSWORD", password)
        log_info("PostgreSQL password generated and saved to .env")

    env.load()  # Reload to get any new passwords

    # Resolve storage paths from environment
    caddy_dir = _resolve_path(env.get("CADDY_DIR"), "./caddy", project_dir)
    certs_dir = caddy_dir / "certs"
    sites_dir = caddy_dir / "sites"
    database_dir = _resolve_path(env.get("DATABASE_DIR"), "./database", project_dir)

    docker = DockerManager(project_dir)
    db = DatabaseManager(project_dir / "db.sqlite", docker)
    ssl = SSLManager(certs_dir)
    hosts = HostsManager()
    caddyfile = CaddyfileGenerator(project_dir, sites_dir)
    password_manager = PasswordManager(project_dir, docker)

    # Check state
    if db.is_running:
        raise ServerStateError("The server is already running.")

    # Load and merge domains
    registered = db.get_domains_with_versions()
    registered_domains = [d for d, _ in registered]
    registered_map = {d: v for d, v in registered}

    domains_with_versions: list[tuple[str, str]] = []

    if domains is None:
        # No domains provided, use registered ones
        if not registered:
            log_error("No domains provided and no domains registered in database.")
            log_error('Please provide domains: frankenmanager start "myapp.test"')
            sys.exit(1)
        domains_with_versions = list(registered)
        log_info(f"Using registered domains: {', '.join(registered_domains)}")
    else:
        # Merge provided domains with registered ones

        # Keep registered domains with their existing versions
        for d, v in registered:
            domains_with_versions.append((d, v))

        # Add new domains with the specified PHP version
        new_domains_list = [d for d in domains if d not in registered_map]
        for d in new_domains_list:
            domains_with_versions.append((d, php_version))

        if registered:
            log_info(f"Registered domains: {', '.join(registered_domains)}")
            if new_domains_list:
                log_info(f"New domains (PHP {php_version}): {', '.join(new_domains_list)}")

    # Extract flat domain list for validation
    all_domains = [d for d, _ in domains_with_versions]

    # Validate inputs
    validate_directory(custom_path)
    for domain in all_domains:
        validate_domain(domain)

    # Remove duplicates while preserving order
    seen: set[str] = set()
    unique_domains_versions: list[tuple[str, str]] = []
    for d, v in domains_with_versions:
        if d not in seen:
            seen.add(d)
            unique_domains_versions.append((d, v))
    domains_with_versions = unique_domains_versions
    all_domains = [d for d, _ in domains_with_versions]

    # Determine which PHP versions are needed
    active_versions = {v for _, v in domains_with_versions}

    # Ensure php.ini exists for each version
    for version in active_versions:
        ensure_php_version_config(project_dir, version)

    hosts_added: list[str] = []

    try:
        # Generate SSL certificates
        ssl.generate_all(all_domains, force_ssl, env.is_production())

        # Add hosts entries
        for domain in all_domains:
            hosts.add_entry("127.0.0.1", domain)
            hosts_added.append(domain)

        # Generate per-version Caddyfiles (new domains only)
        caddyfile.generate_all_for_versions(domains_with_versions)

        # Migrate existing Caddyfiles from TLS to HTTP-only worker format
        caddyfile.migrate_worker_caddyfiles()

        # Generate main reverse proxy Caddyfile
        caddyfile.generate_main_caddyfile(domains_with_versions, caddy_dir, env.is_production())

        # Save domains to database with versions
        db.set_domains_with_versions(domains_with_versions)

        # Build and start containers
        log_info("Building Docker images...")

        # Fix .my-healthcheck.cnf permissions if world-writable (MySQL ignores such files)
        healthcheck_cnf = database_dir / ".my-healthcheck.cnf"
        if healthcheck_cnf.exists() and (healthcheck_cnf.stat().st_mode & 0o022):
            healthcheck_cnf.chmod(0o640)

        docker.build_images(str(custom_path), active_versions, env.get("WWWGROUP") or "")
        docker.compose_down(env.is_production())

        # Generate docker-compose file
        docker.generate_compose_file(
            active_versions, {}, env.is_production(), db_engines
        )

        # Prepare environment variables for docker-compose
        expose = env.get("EXPOSE_SERVICES") == "true"
        localhost = "" if expose else "127.0.0.1:"

        # Get port configurations from .env (with defaults)
        db_port = env.get("DB_PORT") or "3306"
        pma_port = env.get("PMA_PORT") or "8080"
        redis_port = env.get("REDIS_PORT") or "6379"
        web_http_port = env.get("WEB_HTTP_PORT") or "80"
        web_https_port = env.get("WEB_HTTPS_PORT") or "443"

        env_vars = {
            "CUSTOM_PATH": str(custom_path),
            "UID": env.require("UID"),
            "GID": env.require("GID"),
            "SIMPLE_DB_PORT": db_port,
            "DB_PORT": f"{localhost}{db_port}:3306",
            "PMA_PORT": f"{localhost}{pma_port}:80",
            "REDIS_PORT": f"{localhost}{redis_port}:6379",
            "WEB_HTTP_PORT": web_http_port,
            "WEB_HTTPS_PORT": web_https_port,
            "MYSQL_MAX_ALLOWED_PACKET": env.get("MYSQL_MAX_ALLOWED_PACKET") or "512M",
            "PWD": str(project_dir),
            "CADDY_DIR": str(caddy_dir),
            "DATABASE_DIR": str(database_dir),
            **env.build_db_env_vars(db_engines, localhost),
        }
        docker.compose_up(env_vars, env.is_production())

        # Sync database passwords
        db_passwords = {}
        if "mariadb" in db_engines:
            db_passwords["mariadb"] = env.require("MARIADB_ROOT_PASSWORD")
        if "mysql" in db_engines:
            db_passwords["mysql"] = env.require("MYSQL_ROOT_PASSWORD")
        password_manager.sync_all_passwords(db_engines, db_passwords)

        print()
        log_success("Web server started!")

        # Show PHP version summary
        for version in sorted(active_versions):
            version_domains = [d for d, v in domains_with_versions if v == version]
            log_info(f"  PHP {version}: {', '.join(version_domains)}")

    except Exception as e:
        # Cleanup on failure
        log_error(f"An error occurred: {e}")
        log_error("Cleaning up...")

        for domain in hosts_added:
            try:
                hosts.remove_entry("127.0.0.1", domain)
            except Exception:
                pass

        db.reset()
        log_error("Cleanup complete. The server did not start.")
        raise
