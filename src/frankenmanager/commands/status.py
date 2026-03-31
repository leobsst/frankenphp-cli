"""Status command implementation."""

from rich.console import Console
from rich.table import Table

from ..core.database import DatabaseManager
from ..core.docker_manager import DockerManager, parse_db_engines
from ..core.environment import EnvironmentManager
from ..core.php_versions import get_container_name
from ..core.resources import get_project_dir

console = Console()


def show_status() -> None:
    """Show the current server status."""
    project_dir = get_project_dir()

    docker = DockerManager(project_dir)
    db = DatabaseManager(project_dir / "db.sqlite", docker)

    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    if env.env_path.exists():
        env.load()
    db_engines = parse_db_engines(env.get("DB_ENGINES") or "mariadb") or ["mariadb"]

    # Get real-time status from Docker containers
    is_running = db.is_running
    domains_with_versions = db.get_domains_with_versions()
    active_versions = db.get_active_php_versions()

    console.print("\n[bold]=== FrankenPHP Server Status ===[/bold]\n")

    status = "running" if is_running else "stopped"
    status_color = "green" if is_running else "red"
    console.print(f"Status: [{status_color}]{status}[/]")

    # Show domains with PHP versions
    if domains_with_versions:
        console.print("\n[bold]Domains:[/bold]")
        domain_table = Table(show_header=True, header_style="bold")
        domain_table.add_column("Domain", width=30)
        domain_table.add_column("PHP Version", width=12)

        for domain, php_version in domains_with_versions:
            domain_table.add_row(f"https://{domain}", f"PHP {php_version}")

        console.print(domain_table)

    # Show container status
    console.print("\n[bold]Containers:[/bold]")
    table = Table(show_header=True, header_style="bold")
    table.add_column("Container", width=25)
    table.add_column("Status", width=12)
    table.add_column("Health", width=12)

    # FrankenPHP containers (one per active version)
    for version in sorted(active_versions):
        container_name = get_container_name(version)
        _add_container_row(table, docker, container_name)

    # Infrastructure containers
    for container in docker.get_all_containers(
        db_engines=db_engines, production=env.is_production()
    ):
        # Skip PHP containers (already shown above)
        if container.startswith("frankenphp-"):
            continue
        _add_container_row(table, docker, container)

    console.print(table)
    console.print()


def _add_container_row(table: Table, docker: DockerManager, container: str) -> None:
    """Add a container status row to the table."""
    status_info = docker.get_container_status(container)
    status = status_info["status"]
    health = status_info["health"]

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
