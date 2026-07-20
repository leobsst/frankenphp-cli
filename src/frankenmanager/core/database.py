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
            cursor.execute(
                """
                CREATE TABLE IF NOT EXISTS domains (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    domain TEXT NOT NULL UNIQUE,
                    php_version TEXT NOT NULL DEFAULT '8.3',
                    added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
                )
            """
            )

            # Migration: add php_version column if missing (existing installs)
            cursor.execute("PRAGMA table_info(domains)")
            columns = [row[1] for row in cursor.fetchall()]
            if "php_version" not in columns:
                cursor.execute(
                    "ALTER TABLE domains ADD COLUMN "
                    f"php_version TEXT NOT NULL DEFAULT '{DEFAULT_PHP_VERSION}'"
                )

            # Create aliases table: alternate hosts that reverse-proxy to an
            # existing domain's site instead of owning their own Caddyfile.
            cursor.execute(
                """
                CREATE TABLE IF NOT EXISTS aliases (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    alias_domain TEXT NOT NULL UNIQUE,
                    target_domain TEXT NOT NULL,
                    added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
                )
            """
            )

            # Create proxies table: domains that reverse-proxy straight to a
            # raw upstream address (e.g. "127.0.0.1:8006") instead of a PHP
            # container. No php_version/Caddyfile of their own.
            cursor.execute(
                """
                CREATE TABLE IF NOT EXISTS proxies (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    domain TEXT NOT NULL UNIQUE,
                    target TEXT NOT NULL,
                    added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
                )
            """
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

    def add_aliases(self, alias_domains: list[str], target_domain: str) -> None:
        """Add alternate host(s) that reverse-proxy to an existing domain.

        Args:
            alias_domains: List of new alias domain names.
            target_domain: The existing domain these aliases should proxy to.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()

            for alias_domain in alias_domains:
                cursor.execute(
                    "INSERT OR IGNORE INTO aliases (alias_domain, target_domain) VALUES (?, ?)",
                    (alias_domain, target_domain),
                )

            conn.commit()

    def remove_aliases(self, alias_domains: list[str]) -> None:
        """Remove alias domains.

        Args:
            alias_domains: List of alias domain names to remove.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()

            for alias_domain in alias_domains:
                cursor.execute("DELETE FROM aliases WHERE alias_domain = ?", (alias_domain,))

            conn.commit()

    def remove_aliases_for_targets(self, target_domains: list[str]) -> list[str]:
        """Remove all aliases pointing to any of the given target domains.

        Used when the target domain itself is removed, to avoid leaving
        aliases that proxy to a domain that no longer exists.

        Args:
            target_domains: List of target domain names being removed.

        Returns:
            List of alias domain names that were removed.
        """
        if not target_domains:
            return []

        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            placeholders = ",".join("?" * len(target_domains))
            cursor.execute(
                f"SELECT alias_domain FROM aliases WHERE target_domain IN ({placeholders})",
                target_domains,
            )
            removed = [row[0] for row in cursor.fetchall()]
            cursor.execute(
                f"DELETE FROM aliases WHERE target_domain IN ({placeholders})",
                target_domains,
            )
            conn.commit()
            return removed

    def get_aliases(self) -> list[tuple[str, str]]:
        """Get all configured aliases.

        Returns:
            List of (alias_domain, target_domain) tuples.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            cursor.execute("SELECT alias_domain, target_domain FROM aliases ORDER BY id")
            return [(row[0], row[1]) for row in cursor.fetchall()]

    def get_alias_target(self, alias_domain: str) -> Optional[str]:
        """Get the target domain for an alias.

        Args:
            alias_domain: The alias domain name.

        Returns:
            The target domain name, or None if the domain is not an alias.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            cursor.execute(
                "SELECT target_domain FROM aliases WHERE alias_domain = ?", (alias_domain,)
            )
            row = cursor.fetchone()
            return row[0] if row else None

    def get_alias_entries(self) -> list[tuple[str, str, str]]:
        """Get all aliases together with their target's current PHP version.

        Resolving the PHP version live (rather than storing it on the alias)
        keeps aliases in sync automatically when the target's version changes.

        Returns:
            List of (alias_domain, php_version, target_domain) tuples.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            cursor.execute(
                """
                SELECT a.alias_domain, d.php_version, a.target_domain
                FROM aliases a
                JOIN domains d ON d.domain = a.target_domain
                ORDER BY a.id
            """
            )
            return [(row[0], row[1], row[2]) for row in cursor.fetchall()]

    def clear_aliases(self) -> None:
        """Clear all configured aliases."""
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            cursor.execute("DELETE FROM aliases")
            conn.commit()

    def add_proxies(self, domains: list[str], target: str) -> None:
        """Add domain(s) that reverse-proxy straight to a raw upstream address.

        Args:
            domains: List of new domain names.
            target: Raw upstream address (e.g. "127.0.0.1:8006", "http://host:port").
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()

            for domain in domains:
                cursor.execute(
                    "INSERT OR IGNORE INTO proxies (domain, target) VALUES (?, ?)",
                    (domain, target),
                )

            conn.commit()

    def remove_proxies(self, domains: list[str]) -> None:
        """Remove proxy domains.

        Args:
            domains: List of proxy domain names to remove.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()

            for domain in domains:
                cursor.execute("DELETE FROM proxies WHERE domain = ?", (domain,))

            conn.commit()

    def get_proxies(self) -> list[tuple[str, str]]:
        """Get all configured proxy hosts.

        Returns:
            List of (domain, target) tuples.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            cursor.execute("SELECT domain, target FROM proxies ORDER BY id")
            return [(row[0], row[1]) for row in cursor.fetchall()]

    def get_proxy_target(self, domain: str) -> Optional[str]:
        """Get the upstream target for a proxy domain.

        Args:
            domain: The proxy domain name.

        Returns:
            The raw upstream address, or None if the domain is not a proxy.
        """
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            cursor.execute("SELECT target FROM proxies WHERE domain = ?", (domain,))
            row = cursor.fetchone()
            return row[0] if row else None

    def clear_proxies(self) -> None:
        """Clear all configured proxy hosts."""
        with sqlite3.connect(self.db_path) as conn:
            cursor = conn.cursor()
            cursor.execute("DELETE FROM proxies")
            conn.commit()

    def reset(self) -> None:
        """Reset database to default state."""
        self.clear_domains()
        self.clear_aliases()
        self.clear_proxies()
