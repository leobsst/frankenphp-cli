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
) -> None:
    """FrankenManager - FrankenPHP Docker development environment manager."""


@app.command()
def start(
    domains: str = typer.Argument(..., help="Space-separated domain names"),
    path: str = typer.Argument("/home", help="Custom path for project root"),
    force_ssl: bool = typer.Option(
        False, "--force-ssl", help="Force SSL certificate regeneration"
    ),
) -> None:
    """Start the FrankenPHP development server."""
    from .commands.start import start_server

    domain_list = domains.split()
    start_server(domain_list, Path(path), force_ssl)


@app.command()
def stop() -> None:
    """Stop the FrankenPHP development server."""
    from .commands.stop import stop_server

    stop_server()


@app.command()
def restart(
    force_ssl: bool = typer.Option(
        False, "--force-ssl", help="Force SSL certificate regeneration"
    ),
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
    remove: bool = typer.Option(
        False, "--remove", "-r", help="Remove privilege configuration"
    ),
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


if __name__ == "__main__":
    app()
