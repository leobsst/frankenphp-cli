"""Reset command implementation."""

from pathlib import Path
from typing import Optional

import typer
from rich.console import Console

from ..core.database import DatabaseManager
from ..core.docker_manager import DockerManager
from ..core.environment import EnvironmentManager
from ..core.resources import get_project_dir
from ..exceptions import ServerStateError
from ..utils.logging import log_error, log_info, log_success, log_warning

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


def reset_data(reset_db: bool, reset_caddyfiles: bool) -> None:
    """Reset FrankenManager configuration and/or Caddyfiles with user confirmation.

    Args:
        reset_db: Whether to reset the FrankenManager configuration (domain list).
        reset_caddyfiles: Whether to reset custom Caddyfiles.
    """
    if not reset_db and not reset_caddyfiles:
        log_error("You must specify at least one option: --db or --caddyfiles")
        raise typer.Exit(1)

    project_dir = get_project_dir()

    # Initialize managers
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    if env.env_path.exists():
        env.load()

    docker = DockerManager(project_dir)
    db = DatabaseManager(project_dir / "db.sqlite", docker)

    # Check if server is running
    if db.is_running:
        raise ServerStateError(
            "The server is currently running. "
            "Please stop the server first with 'frankenmanager stop'."
        )

    # Build confirmation message
    items_to_reset = []
    if reset_db:
        items_to_reset.append("FrankenManager configuration (domain list)")
    if reset_caddyfiles:
        items_to_reset.append("custom Caddyfiles")

    console.print("\n[bold yellow]⚠️  Warning: You are about to reset:[/bold yellow]")
    for item in items_to_reset:
        console.print(f"  • {item}")

    if reset_db:
        console.print(
            "\n[dim]Configuration reset will clear all registered domains from db.sqlite[/dim]"
        )
        console.print("[dim]Note: This does NOT affect your MariaDB database data[/dim]")
    if reset_caddyfiles:
        caddy_dir = _resolve_path(env.get("CADDY_DIR"), "./caddy", project_dir)
        custom_dir = caddy_dir / "sites" / "custom"
        console.print(f"\n[dim]Caddyfiles reset will delete all files in: {custom_dir}[/dim]")

    console.print("\n[bold red]This action cannot be undone![/bold red]\n")

    # Ask for confirmation
    confirm = typer.confirm("Do you want to proceed?")
    if not confirm:
        log_info("Reset cancelled.")
        raise typer.Exit(0)

    # Perform reset operations
    try:
        if reset_db:
            log_info("Resetting FrankenManager configuration...")
            db.reset()
            log_success("Configuration reset successfully!")

        if reset_caddyfiles:
            log_info("Resetting custom Caddyfiles...")
            caddy_dir = _resolve_path(env.get("CADDY_DIR"), "./caddy", project_dir)
            sites_dir = caddy_dir / "sites"
            total_deleted = 0

            if sites_dir.exists():
                # Delete from legacy custom dir and all version dirs
                for subdir in sites_dir.iterdir():
                    if subdir.is_dir() and (
                        subdir.name == "custom" or subdir.name.startswith("php-")
                    ):
                        caddyfiles = list(subdir.glob("*_Caddyfile"))
                        for cf in caddyfiles:
                            cf.unlink()
                            total_deleted += 1

            if total_deleted > 0:
                log_success(f"Deleted {total_deleted} Caddyfile(s)")
            else:
                log_info("No custom Caddyfiles found to delete")

        print()
        log_success("Reset completed successfully!")

    except Exception as e:
        log_error(f"An error occurred during reset: {e}")
        raise
