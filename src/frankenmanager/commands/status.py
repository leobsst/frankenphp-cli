"""Status command implementation."""

from rich.console import Console
from rich.table import Table

from ..core.database import DatabaseManager
from ..core.docker_manager import DockerManager
from ..core.resources import get_project_dir

console = Console()


def show_status() -> None:
    """Show the current server status."""
    project_dir = get_project_dir()

    docker = DockerManager(project_dir)
    db = DatabaseManager(project_dir / "db.sqlite", docker)

    # Get real-time status from Docker containers
    is_running = db.is_running
    domains = db.get_domains()

    console.print("\n[bold]=== FrankenPHP Server Status ===[/bold]\n")

    status = "running" if is_running else "stopped"
    status_color = "green" if is_running else "red"
    console.print(f"Status: [{status_color}]{status}[/]")

    # Show domains if configured
    if domains:
        console.print("\n[bold]Domains:[/bold]")
        for domain in domains:
            console.print(f"  - https://{domain}")

    # Always show container status
    console.print("\n[bold]Containers:[/bold]")
    table = Table(show_header=True, header_style="bold")
    table.add_column("Container", width=25)
    table.add_column("Status", width=12)
    table.add_column("Health", width=12)

    for container in DockerManager.CONTAINERS:
        status_info = docker.get_container_status(container)
        status = status_info["status"]
        health = status_info["health"]

        # Color coding
        if status == "running":
            status_str = f"[green]{status}[/]"
        elif status == "not found":
            status_str = f"[dim]{status}[/]"
        else:
            status_str = f"[red]{status}[/]"

        if health == "healthy":
            health_str = f"[green]{health}[/]"
        elif health == "N/A":
            health_str = f"[dim]{health}[/]"
        else:
            health_str = f"[yellow]{health}[/]"

        table.add_row(container, status_str, health_str)

    console.print(table)

    console.print()
