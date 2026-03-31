"""Database password management and synchronization."""

import time
from pathlib import Path
from typing import TYPE_CHECKING, Optional

from ..utils.logging import log_error, log_info, log_success

if TYPE_CHECKING:
    from .docker_manager import DockerManager


class PasswordManager:
    """Manages database root password history and synchronization."""

    def __init__(self, project_dir: Path, docker_manager: "DockerManager") -> None:
        """Initialize the password manager.

        Args:
            project_dir: Path to the project directory.
            docker_manager: Docker manager instance.
        """
        self.project_dir = project_dir
        self.docker = docker_manager

    def _history_file(self, engine: str) -> Path:
        """Get the password history file for a given engine."""
        return self.project_dir / f".db_password_history_{engine}"

    def _load_history(self, engine: str) -> list[str]:
        """Load password history from file.

        Args:
            engine: Database engine name.

        Returns:
            List of previously used passwords.
        """
        history_file = self._history_file(engine)
        if not history_file.exists():
            # Migrate from legacy file for mariadb
            if engine == "mariadb":
                legacy = self.project_dir / ".db_password_history"
                if legacy.exists():
                    return [p for p in legacy.read_text().splitlines() if p.strip()]
            return []
        return [p for p in history_file.read_text().splitlines() if p.strip()]

    def _save_to_history(self, engine: str, password: str) -> None:
        """Add a password to the history file if not already present.

        Args:
            engine: Database engine name.
            password: The password to save.
        """
        history = self._load_history(engine)
        if password not in history:
            history_file = self._history_file(engine)
            with history_file.open("a") as f:
                f.write(password + "\n")
            history_file.chmod(0o660)

    def _test_password_mariadb(self, container_name: str, password: str) -> bool:
        """Test if a password works for MariaDB root."""
        exit_code, _ = self.docker.exec_in_container(
            container_name,
            ["mariadb", "-u", "root", f"-p{password}", "-e", "SELECT 1"],
        )
        return exit_code == 0

    def _test_password_mysql(self, container_name: str, password: str) -> bool:
        """Test if a password works for MySQL root."""
        exit_code, _ = self.docker.exec_in_container(
            container_name,
            ["mysql", "-u", "root", f"-p{password}", "-e", "SELECT 1"],
        )
        return exit_code == 0

    def _find_working_password(
        self, engine: str, container_name: str, new_password: str
    ) -> Optional[str]:
        """Find a working password, trying new first then history.

        Args:
            engine: Database engine name.
            container_name: Container to test against.
            new_password: The new password to try first.

        Returns:
            A working password, or None if none work.
        """
        test_fn = self._test_password_mariadb if engine == "mariadb" else self._test_password_mysql

        if test_fn(container_name, new_password):
            return new_password

        for old_password in reversed(self._load_history(engine)):
            if test_fn(container_name, old_password):
                return old_password

        return None

    def _sync_mariadb_or_mysql(
        self, engine: str, container_name: str, new_password: str, max_retries: int = 30
    ) -> bool:
        """Synchronize password for MariaDB or MySQL.

        Args:
            engine: "mariadb" or "mysql".
            container_name: Container name.
            new_password: The password from .env to sync.
            max_retries: Maximum number of connection attempts.

        Returns:
            True if sync succeeded, False otherwise.
        """
        label = "MariaDB" if engine == "mariadb" else "MySQL"
        cli_cmd = "mariadb" if engine == "mariadb" else "mysql"

        working_password = None
        for _ in range(max_retries):
            working_password = self._find_working_password(engine, container_name, new_password)
            if working_password:
                break
            time.sleep(1)

        if not working_password:
            log_info(f"{label} password sync skipped (could not connect)")
            return False

        if working_password == new_password:
            self._save_to_history(engine, new_password)
            log_info(f"{label} password already in sync")
            return True

        sql = (
            f"ALTER USER 'root'@'%' IDENTIFIED BY '{new_password}'; "
            f"ALTER USER 'root'@'localhost' IDENTIFIED BY '{new_password}'; "
            f"FLUSH PRIVILEGES;"
        )
        exit_code, _ = self.docker.exec_in_container(
            container_name,
            [cli_cmd, "-u", "root", f"-p{working_password}", "-e", sql],
        )

        if exit_code == 0:
            self._save_to_history(engine, new_password)
            log_success(f"{label} password updated to match .env")
            return True

        log_error(f"Failed to sync {label} password")
        return False

    def sync_password(self, new_password: str, max_retries: int = 30) -> bool:
        """Synchronize the .env password to the running MariaDB instance.

        Legacy method for backward compatibility.

        Args:
            new_password: The password from .env to sync.
            max_retries: Maximum number of connection attempts.

        Returns:
            True if sync succeeded, False otherwise.
        """
        return self._sync_mariadb_or_mysql(
            "mariadb", "franken_mariadb", new_password, max_retries
        )

    def sync_all_passwords(
        self, db_engines: list[str], passwords: dict[str, str], max_retries: int = 30
    ) -> None:
        """Synchronize passwords for all enabled database engines.

        Args:
            db_engines: List of enabled database engines.
            passwords: Dict mapping engine name to password.
            max_retries: Maximum number of connection attempts.
        """
        from .docker_manager import DB_CONTAINERS

        for engine in db_engines:
            container_name = DB_CONTAINERS.get(engine)
            password = passwords.get(engine)
            if not container_name or not password:
                continue

            if engine in ("mariadb", "mysql"):
                self._sync_mariadb_or_mysql(engine, container_name, password, max_retries)
            # PostgreSQL handles passwords via POSTGRES_PASSWORD env var at startup,
            # no runtime sync needed
