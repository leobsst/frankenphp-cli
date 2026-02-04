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
    domains: str = typer.Argument(..., help="Space-separated domain names"),
    path: Optional[str] = typer.Option(
        None,
        "--path",
        "-p",
        help="Project root path (uses DEFAULT_PROJECT_PATH from .env if not set)",
    ),
    force_ssl: bool = typer.Option(False, "--force-ssl", help="Force SSL certificate regeneration"),
) -> None:
    """Start the FrankenPHP development server.

    If --path is not specified, uses DEFAULT_PROJECT_PATH from .env file.

    Examples:
        frankenmanager start "myapp.test"
        frankenmanager start "myapp.test api.test" --path /home/projects
    """
    from .commands.start import start_server

    domain_list = domains.split()
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
) -> None:
    """Restart the FrankenPHP development server."""
    from .commands.restart import restart_server

    restart_server(force_ssl)


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


if __name__ == "__main__":
    app()
