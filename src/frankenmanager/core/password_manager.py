"""MariaDB password management and synchronization."""

import time
from pathlib import Path
from typing import TYPE_CHECKING, Optional

from ..utils.logging import log_error, log_info, log_success

if TYPE_CHECKING:
    from .docker_manager import DockerManager


class PasswordManager:
    """Manages MariaDB root password history and synchronization."""

    def __init__(self, project_dir: Path, docker_manager: "DockerManager") -> None:
        """Initialize the password manager.

        Args:
            project_dir: Path to the project directory.
            docker_manager: Docker manager instance.
        """
        self.history_file = project_dir / ".db_password_history"
        self.docker = docker_manager
        self.container_name = "franken_mariadb"

    def _load_history(self) -> list[str]:
        """Load password history from file.

        Returns:
            List of previously used passwords.
        """
        if not self.history_file.exists():
            return []
        return [p for p in self.history_file.read_text().splitlines() if p.strip()]

    def _save_to_history(self, password: str) -> None:
        """Add a password to the history file if not already present.

        Args:
            password: The password to save.
        """
        history = self._load_history()
        if password not in history:
            with self.history_file.open("a") as f:
                f.write(password + "\n")
            self.history_file.chmod(0o660)

    def _test_password(self, password: str) -> bool:
        """Test if a password works for MariaDB root.

        Args:
            password: The password to test.

        Returns:
            True if the password works, False otherwise.
        """
        exit_code, _ = self.docker.exec_in_container(
            self.container_name,
            ["mariadb", "-u", "root", f"-p{password}", "-e", "SELECT 1"],
        )
        return exit_code == 0

    def find_working_password(self, new_password: str) -> Optional[str]:
        """Find a working password, trying new first then history.

        Args:
            new_password: The new password to try first.

        Returns:
            A working password, or None if none work.
        """
        # Try new password first
        if self._test_password(new_password):
            return new_password

        # Try history (newest first)
        for old_password in reversed(self._load_history()):
            if self._test_password(old_password):
                return old_password

        return None

    def sync_password(self, new_password: str, max_retries: int = 30) -> bool:
        """Synchronize the .env password to the running MariaDB instance.

        Args:
            new_password: The password from .env to sync.
            max_retries: Maximum number of connection attempts.

        Returns:
            True if sync succeeded, False otherwise.
        """
        # Wait for MariaDB to be ready
        working_password = None
        for _ in range(max_retries):
            working_password = self.find_working_password(new_password)
            if working_password:
                break
            time.sleep(1)

        if not working_password:
            log_info("MariaDB password sync skipped (could not connect)")
            return False

        # Already in sync
        if working_password == new_password:
            self._save_to_history(new_password)
            log_info("MariaDB password already in sync")
            return True

        # Update password using the old working password
        sql = (
            f"ALTER USER 'root'@'%' IDENTIFIED BY '{new_password}'; "
            f"ALTER USER 'root'@'localhost' IDENTIFIED BY '{new_password}'; "
            f"FLUSH PRIVILEGES;"
        )
        exit_code, _ = self.docker.exec_in_container(
            self.container_name,
            ["mariadb", "-u", "root", f"-p{working_password}", "-e", sql],
        )

        if exit_code == 0:
            self._save_to_history(new_password)
            log_success("MariaDB password updated to match .env")
            return True

        log_error("Failed to sync MariaDB password")
        return False
