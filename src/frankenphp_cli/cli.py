"""Main CLI application using Typer."""

from pathlib import Path
from typing import Optional

import typer
from rich.console import Console

from . import __version__

app = typer.Typer(
    name="frankenphp",
    help="FrankenPHP Docker development environment manager",
    add_completion=False,
)

console = Console()


def version_callback(value: bool) -> None:
    """Print version and exit."""
    if value:
        console.print(f"frankenphp-cli version {__version__}")
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
    """FrankenPHP Docker development environment manager."""
    pass


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


if __name__ == "__main__":
    app()
