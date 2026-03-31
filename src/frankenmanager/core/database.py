"""Database management for server state using SQLite."""

import sqlite3
from pathlib import Path
from typing import TYPE_CHECKING, Optional

from .php_versions import DEFAULT_PHP_VERSION

if TYPE_CHECKING:
    from .docker_manager import DockerManager


class DatabaseManager:
    """Manages server state in SQLite database."""

    def __init__(self, db_path: Path, docker_manager: Optional["DockerManager"] = None) -> None:
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
                    php_version TEXT NOT NULL DEFAULT '8.3',
                    added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
                )
            """)

            # Migration: add php_version column if missing (existing installs)
            cursor.execute("PRAGMA table_info(domains)")
            columns = [row[1] for row in cursor.fetchall()]
            if "php_version" not in columns:
                cursor.execute(
                    "ALTER TABLE domains ADD COLUMN "
                    f"php_version TEXT NOT NULL DEFAULT '{DEFAULT_PHP_VERSION}'"
                )

            conn.commit()

    @property
    def is_running(self) -> bool:
        """Check if the server is currently running based on real-time Docker container status.

        Returns:
            True if at least one FrankenPHP container and infrastructure containers are running.
        """
        if not self.docker_manager:
            return False

        try:
            # Check infrastructure containers (including reverse proxy)
            from .docker_manager import REVERSE_PROXY_CONTAINER  # noqa: PLC0415

            for container in (REVERSE_PROXY_CONTAINER, "franken_redis"):
                status_info = self.docker_manager.get_container_status(container)
                if status_info["status"] != "running":
                    return False

            # Check if at least one frankenphp container is running
            domains_with_versions = self.get_domains_with_versions()
            if not domains_with_versions:
                return False

            active_versions = {v for _, v in domains_with_versions}
            for version in active_versions:
                from .php_versions import get_container_name  # noqa: PLC0415

                container_name = get_container_name(version)
                status_info = self.docker_manager.get_container_status(container_name)
                if status_info["status"] == "running":
                    return True

            return False
        except (ImportError, AttributeError, KeyError):
            return False

    def set_domains(self, domains: list[str], php_version: str = DEFAULT_PHP_VERSION) -> None:
        """Set the list of configured domains.

        Args:
            domains: List of domain names being served.
            php_version: PHP version for all domains.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()

            # Clear existing domains and insert new ones
            cursor.execute("DELETE FROM domains")
            for domain in domains:
                cursor.execute(
                    "INSERT INTO domains (domain, php_version) VALUES (?, ?)",
                    (domain, php_version),
                )

            conn.commit()

    def set_domains_with_versions(self, domains_versions: list[tuple[str, str]]) -> None:
        """Set the list of configured domains with their PHP versions.

        Args:
            domains_versions: List of (domain, php_version) tuples.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()

            cursor.execute("DELETE FROM domains")
            for domain, php_version in domains_versions:
                cursor.execute(
                    "INSERT INTO domains (domain, php_version) VALUES (?, ?)",
                    (domain, php_version),
                )

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

    def get_domains_with_versions(self) -> list[tuple[str, str]]:
        """Get domains with their PHP versions.

        Returns:
            List of (domain, php_version) tuples.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            cursor.execute("SELECT domain, php_version FROM domains ORDER BY id")
            return [(row[0], row[1]) for row in cursor.fetchall()]

    def get_domain_php_version(self, domain: str) -> Optional[str]:
        """Get the PHP version for a specific domain.

        Args:
            domain: The domain name.

        Returns:
            PHP version string or None if domain not found.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            cursor.execute("SELECT php_version FROM domains WHERE domain = ?", (domain,))
            row = cursor.fetchone()
            return row[0] if row else None

    def get_active_php_versions(self) -> set[str]:
        """Get the set of PHP versions currently in use.

        Returns:
            Set of PHP version strings.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            cursor.execute("SELECT DISTINCT php_version FROM domains")
            return {row[0] for row in cursor.fetchall()}

    def get_domains_by_version(self, php_version: str) -> list[str]:
        """Get all domains using a specific PHP version.

        Args:
            php_version: PHP version to filter by.

        Returns:
            List of domain names.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            cursor.execute(
                "SELECT domain FROM domains WHERE php_version = ? ORDER BY id",
                (php_version,),
            )
            return [row[0] for row in cursor.fetchall()]

    def add_domains(self, new_domains: list[str], php_version: str = DEFAULT_PHP_VERSION) -> None:
        """Add new domains to the existing configuration.

        Args:
            new_domains: List of new domain names to add.
            php_version: PHP version for the new domains.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()

            for domain in new_domains:
                cursor.execute(
                    "INSERT OR IGNORE INTO domains (domain, php_version) VALUES (?, ?)",
                    (domain, php_version),
                )

            conn.commit()

    def remove_domains(self, domains_to_remove: list[str]) -> None:
        """Remove domains from the existing configuration.

        Args:
            domains_to_remove: List of domain names to remove.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()

            for domain in domains_to_remove:
                cursor.execute(
                    "DELETE FROM domains WHERE domain = ?",
                    (domain,),
                )

            conn.commit()

    def update_domain_php_version(self, domain: str, php_version: str) -> bool:
        """Update the PHP version for a specific domain.

        Args:
            domain: The domain name.
            php_version: The new PHP version.

        Returns:
            True if the domain was found and updated.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            cursor.execute(
                "UPDATE domains SET php_version = ? WHERE domain = ?",
                (php_version, domain),
            )
            conn.commit()
            return cursor.rowcount > 0

    def reset(self) -> None:
        """Reset database to default state."""
        self.clear_domains()
