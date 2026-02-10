"""Database management for server state using SQLite."""

import sqlite3
from pathlib import Path
from typing import TYPE_CHECKING, Optional

if TYPE_CHECKING:
    from .docker_manager import DockerManager


class DatabaseManager:
    """Manages server state in SQLite database."""

    def __init__(
        self, db_path: Path, docker_manager: Optional["DockerManager"] = None
    ) -> None:
        """Initialize the database manager.

        Args:
            db_path: Path to the SQLite database file.
            docker_manager: Optional DockerManager instance for real-time status checking.
        """
        self.db_path = db_path
        self.docker_manager = docker_manager
        self._init_database()

    def _init_database(self) -> None:
        """Initialize database schema if not exists."""
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()

            # Create domains table
            cursor.execute("""
                CREATE TABLE IF NOT EXISTS domains (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    domain TEXT NOT NULL UNIQUE,
                    added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
                )
            """)

            conn.commit()

    @property
    def is_running(self) -> bool:
        """Check if the server is currently running based on real-time Docker container status.

        Returns:
            True if all containers are running, False otherwise.
        """
        if not self.docker_manager:
            # No docker manager means we can't check status, assume stopped
            return False

        try:
            # Import here to avoid circular dependency
            from .docker_manager import DockerManager  # noqa: PLC0415

            # Check if all containers are running
            for container in DockerManager.CONTAINERS:
                status_info = self.docker_manager.get_container_status(container)
                if status_info["status"] != "running":
                    return False

            return True
        except (ImportError, AttributeError, KeyError):
            # If we can't check Docker, assume stopped
            return False

    def set_domains(self, domains: list[str]) -> None:
        """Set the list of configured domains.

        Args:
            domains: List of domain names being served.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()

            # Clear existing domains and insert new ones
            cursor.execute("DELETE FROM domains")
            for domain in domains:
                cursor.execute("""
                    INSERT INTO domains (domain) VALUES (?)
                """, (domain,))

            conn.commit()

    def clear_domains(self) -> None:
        """Clear all configured domains."""
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            cursor.execute("DELETE FROM domains")
            conn.commit()

    def get_domains(self) -> list[str]:
        """Get the list of configured domains.

        Returns:
            List of domain names.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            cursor.execute("SELECT domain FROM domains ORDER BY id")
            return [row[0] for row in cursor.fetchall()]

    def add_domains(self, new_domains: list[str]) -> None:
        """Add new domains to the existing configuration.

        Args:
            new_domains: List of new domain names to add.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()

            for domain in new_domains:
                cursor.execute("""
                    INSERT OR IGNORE INTO domains (domain) VALUES (?)
                """, (domain,))

            conn.commit()

    def remove_domains(self, domains_to_remove: list[str]) -> None:
        """Remove domains from the existing configuration.

        Args:
            domains_to_remove: List of domain names to remove.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()

            for domain in domains_to_remove:
                cursor.execute("""
                    DELETE FROM domains WHERE domain = ?
                """, (domain,))

            conn.commit()

    def reset(self) -> None:
        """Reset database to default state."""
        self.clear_domains()
