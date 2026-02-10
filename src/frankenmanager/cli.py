"""Main CLI application using Typer."""

from pathlib import Path
from typing import Optional

import typer
from rich.console import Console

from . import __version__

app = typer.Typer(
    name="frankenmanager",
    help="FrankenPHP Docker development environment manager",
    add_completion=False,
)

console = Console()


def version_callback(value: bool) -> None:
    """Print version and exit."""
    if value:
        console.print(f"FrankenManager version {__version__}")
        raise typer.Exit()


@app.callback()
def main(
    version: Optional[bool] = typer.Option(
        None,
        "--version",
        "-v",
        help="Show version and exit",
        callback=version_callback,
        is_eager=True,
    ),
    no_update_check: bool = typer.Option(
        False,
        "--no-update-check",
        help="Skip checking for updates on startup",
        hidden=True,
    ),
) -> None:
    """FrankenManager - FrankenPHP Docker development environment manager."""
    if not no_update_check:
        from .core.updater import notify_if_update_available

        notify_if_update_available()


@app.command()
def start(
    domains: Optional[str] = typer.Argument(
        None, help="Space-separated domain names (uses registered domains if not provided)"
    ),
    path: Optional[str] = typer.Option(
        None,
        "--path",
        "-p",
        help="Project root path (uses DEFAULT_PROJECT_PATH from .env if not set)",
    ),
    force_ssl: bool = typer.Option(False, "--force-ssl", help="Force SSL certificate regeneration"),
) -> None:
    """Start the FrankenPHP development server.

    If domains are not provided, uses domains registered in the database.
    If --path is not specified, uses DEFAULT_PROJECT_PATH from .env file.

    Examples:
        frankenmanager start "myapp.test"
        frankenmanager start "myapp.test api.test" --path /home/projects
        frankenmanager start  # Uses registered domains from database
    """
    from .commands.start import start_server

    domain_list = domains.split() if domains else None
    custom_path = Path(path) if path else None
    start_server(domain_list, custom_path, force_ssl)


@app.command()
def stop() -> None:
    """Stop the FrankenPHP development server."""
    from .commands.stop import stop_server

    stop_server()


@app.command()
def restart(
    force_ssl: bool = typer.Option(False, "--force-ssl", help="Force SSL certificate regeneration"),
    caddy: bool = typer.Option(
        False, "--caddy", help="Restart only the web server / Caddy container"
    ),
    database: bool = typer.Option(
        False, "--database", "--db", help="Restart only the database container"
    ),
    cache: bool = typer.Option(False, "--cache", help="Restart only the Redis cache container"),
    phpmyadmin: bool = typer.Option(
        False, "--phpmyadmin", "--pma", help="Restart only the phpMyAdmin container"
    ),
) -> None:
    """Restart the FrankenPHP development server or specific containers.

    Examples:
        frankenmanager restart                    # Restart all containers
        frankenmanager restart --caddy            # Restart only web server/Caddy
        frankenmanager restart --database         # Restart only database
        frankenmanager restart --cache --pma      # Restart Redis and phpMyAdmin
    """
    from .commands.restart import restart_server

    # Collect which containers to restart
    containers = []
    if caddy:
        containers.append("caddy")
    if database:
        containers.append("database")
    if cache:
        containers.append("cache")
    if phpmyadmin:
        containers.append("phpmyadmin")

    restart_server(force_ssl, containers if containers else None)


@app.command(name="add-host")
def add_host_cmd(
    domains: str = typer.Argument(..., help="Space-separated domain names to add"),
    force_ssl: bool = typer.Option(False, "--force-ssl", help="Force SSL certificate regeneration"),
) -> None:
    """Add new host(s) to the running server.

    This command allows you to add new domains while the server is running,
    without having to stop and restart the entire environment.

    Examples:
        frankenmanager add-host "newapp.test"
        frankenmanager add-host "app1.test app2.test" --force-ssl
    """
    from .commands.add_host import add_host

    domain_list = domains.split()
    add_host(domain_list, force_ssl)


@app.command(name="remove-host")
def remove_host_cmd(
    domains: str = typer.Argument(..., help="Space-separated domain names to remove"),
) -> None:
    """Remove host(s) from the running server.

    This command allows you to remove domains while the server is running,
    without having to stop and restart the entire environment.

    The Caddyfile configurations will be archived (not deleted) in case
    you need to restore them later.

    Examples:
        frankenmanager remove-host "oldapp.test"
        frankenmanager remove-host "app1.test app2.test"
    """
    from .commands.remove_host import remove_host

    domain_list = domains.split()
    remove_host(domain_list)


@app.command(name="restore-host")
def restore_host_cmd(
    domains: Optional[str] = typer.Argument(None, help="Space-separated domain names to restore"),
    list_archived: bool = typer.Option(False, "--list", "-l", help="List all archived hosts"),
    force_ssl: bool = typer.Option(False, "--force-ssl", help="Force SSL certificate regeneration"),
) -> None:
    """Restore archived host(s) to the running server.

    This command allows you to restore previously removed domains from the archive.
    Use --list to see all available archived hosts.

    Examples:
        frankenmanager restore-host --list
        frankenmanager restore-host "myapp.test"
        frankenmanager restore-host "app1.test app2.test" --force-ssl
    """
    from .commands.restore_host import list_archived_hosts, restore_host

    if list_archived:
        list_archived_hosts()
        return

    if not domains:
        console.print(
            "[yellow]Specify domains to restore or use --list to see archived hosts.[/yellow]"
        )
        raise typer.Exit(1)

    domain_list = domains.split()
    restore_host(domain_list, force_ssl)


@app.command()
def status() -> None:
    """Show the current server status."""
    from .commands.status import show_status

    show_status()


@app.command()
def setup(
    remove: bool = typer.Option(False, "--remove", "-r", help="Remove privilege configuration"),
    show_status: bool = typer.Option(
        False, "--status", "-s", help="Show current configuration status"
    ),
    install_mkcert: bool = typer.Option(
        False, "--install-mkcert", "-m", help="Install mkcert for SSL certificates"
    ),
) -> None:
    """Configure FrankenManager for passwordless operation.

    This command sets up the system to allow FrankenManager to:
    - Modify /etc/hosts without requiring a password each time
    - Optionally install mkcert for SSL certificate generation

    Run with sudo for initial setup:
        sudo frankenmanager setup

    To also install mkcert:
        sudo frankenmanager setup --install-mkcert

    To check current status:
        frankenmanager setup --status

    To remove configuration:
        sudo frankenmanager setup --remove
    """
    from .commands.setup import setup_privileges

    setup_privileges(remove=remove, show_status=show_status, install_mkcert=install_mkcert)


@app.command()
def update(
    force: bool = typer.Option(
        False, "--force", "-f", help="Force update even if on latest version"
    ),
    check_only: bool = typer.Option(
        False, "--check", "-c", help="Only check for updates, don't install"
    ),
) -> None:
    """Update FrankenManager to the latest version.

    This command downloads and installs the latest release from GitHub.

    Examples:
        frankenmanager update          # Update to latest version
        frankenmanager update --check  # Check for updates without installing
        frankenmanager update --force  # Force reinstall latest version
    """
    from .core.updater import check_for_updates, get_current_version, update_binary
    from .utils.logging import log_info, log_success

    if check_only:
        log_info(f"Current version: {get_current_version()}")
        update_info = check_for_updates()
        if update_info:
            log_info(f"New version available: {update_info['version']}")
            log_info(f"Release: {update_info.get('name', 'N/A')}")
        else:
            log_success("You are on the latest version!")
    else:
        success = update_binary(force=force)
        if not success:
            raise typer.Exit(1)


@app.command()
def reset(
    db: bool = typer.Option(
        False, "--db", "--database", help="Reset FrankenManager configuration (domain list)"
    ),
    caddyfiles: bool = typer.Option(
        False, "--caddyfiles", "--caddy", help="Reset custom Caddyfiles"
    ),
) -> None:
    """Reset FrankenManager configuration and/or custom Caddyfiles.

    This command allows you to reset specific components of FrankenManager.
    The server must be stopped before running this command.

    NOTE: This does NOT affect your MariaDB database data.

    ⚠️  WARNING: This action cannot be undone!

    Examples:
        frankenmanager reset --db                 # Reset domain list configuration
        frankenmanager reset --caddyfiles         # Reset only custom Caddyfiles
        frankenmanager reset --db --caddyfiles    # Reset both
    """
    from .commands.reset import reset_data

    reset_data(db, caddyfiles)


if __name__ == "__main__":
    app()
