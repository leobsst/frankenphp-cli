"""Logging utilities with Rich console output."""

from rich.console import Console

# Global console instance for output
console = Console()


def log_info(message: str) -> None:
    """Log an informational message.

    Args:
        message: The message to log.
    """
    console.print(f"[blue]--[/blue] {message}")


def log_success(message: str) -> None:
    """Log a success message.

    Args:
        message: The message to log.
    """
    console.print(f"[green]--[/green] {message}")


def log_error(message: str) -> None:
    """Log an error message to stderr.

    Args:
        message: The error message to log.
    """
    console.print(f"[red]ERROR:[/red] {message}", style="red")


def log_warning(message: str) -> None:
    """Log a warning message.

    Args:
        message: The warning message to log.
    """
    console.print(f"[yellow]WARNING:[/yellow] {message}", style="yellow")
