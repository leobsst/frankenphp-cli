"""List hosts command implementation."""

from rich.console import Console
from rich.table import Table

from ..core.database import DatabaseManager
from ..core.resources import get_project_dir

console = Console()


def list_hosts() -> None:
    """List all registered domains with their PHP versions."""
    project_dir = get_project_dir()

    db = DatabaseManager(project_dir / "db.sqlite")
    domains_with_versions = db.get_domains_with_versions()

    if not domains_with_versions:
        console.print("\nNo domains registered.")
        console.print("Use 'frankenmanager start \"myapp.test\"' to add domains.\n")
        return

    table = Table(title="Registered Domains", show_header=True, header_style="bold")
    table.add_column("Domain", style="cyan")
    table.add_column("PHP", style="green")

    for domain, php_version in domains_with_versions:
        table.add_row(f"https://{domain}", php_version)

    console.print()
    console.print(table)
    console.print(f"\n[dim]{len(domains_with_versions)} domain(s) registered[/dim]\n")
