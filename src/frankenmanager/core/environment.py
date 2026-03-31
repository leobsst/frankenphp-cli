"""Environment file (.env) management."""

import os
import secrets
import string
from pathlib import Path
from typing import Optional

from ..exceptions import ConfigurationError


class EnvironmentManager:
    """Manages the .env file for environment variables."""

    def __init__(self, env_path: Path, env_example_path: Path) -> None:
        """Initialize the environment manager.

        Args:
            env_path: Path to the .env file.
            env_example_path: Path to the .env.example file.
        """
        self.env_path = env_path
        self.env_example_path = env_example_path
        self._env_vars: dict[str, str] = {}

    def ensure_env_exists(self) -> bool:
        """Create .env from .env.example if it doesn't exist.

        Automatically fills UID and GID with the current user's values.

        Returns:
            True if the file already existed, False if newly created.

        Raises:
            ConfigurationError: If .env.example doesn't exist.
        """
        if not self.env_path.exists():
            if not self.env_example_path.exists():
                raise ConfigurationError(f"Missing {self.env_example_path}")

            content = self.env_example_path.read_text()

            # Auto-fill UID and GID with current user's values
            uid = os.getuid()
            gid = os.getgid()
            content = content.replace("UID=", f"UID={uid}", 1)
            content = content.replace("GID=", f"GID={gid}", 1)

            self.env_path.write_text(content)
            self.env_path.chmod(0o660)
            return False
        return True

    def load(self) -> dict[str, str]:
        """Load environment variables from .env file.

        Returns:
            Dictionary of environment variables.
        """
        self._env_vars = {}
        if not self.env_path.exists():
            return self._env_vars

        for line in self.env_path.read_text().splitlines():
            line = line.strip()
            if line and not line.startswith("#") and "=" in line:
                key, value = line.split("=", 1)
                # Remove quotes from value
                value = value.strip().strip("\"'")
                self._env_vars[key.strip()] = value
        return self._env_vars

    def get(self, key: str, default: Optional[str] = None) -> Optional[str]:
        """Get an environment variable value.

        Args:
            key: The variable name.
            default: Default value if not found.

        Returns:
            The variable value or default.
        """
        return self._env_vars.get(key, default)

    def require(self, key: str) -> str:
        """Get a required environment variable.

        Args:
            key: The variable name.

        Returns:
            The variable value.

        Raises:
            ConfigurationError: If the variable is not set.
        """
        value = self.get(key)
        if not value:
            raise ConfigurationError(f"Required environment variable '{key}' is not set")
        return value

    def set(self, key: str, value: str) -> None:
        """Update a value in the .env file.

        Args:
            key: The variable name.
            value: The new value.
        """
        lines = self.env_path.read_text().splitlines() if self.env_path.exists() else []
        found = False

        for i, line in enumerate(lines):
            if line.strip().startswith(f"{key}="):
                lines[i] = f"{key}={value}"
                found = True
                break

        if not found:
            lines.append(f"{key}={value}")

        self.env_path.write_text("\n".join(lines) + "\n")
        self._env_vars[key] = value

    def generate_db_password(self) -> str:
        """Generate a secure random password for a database.

        Returns:
            A 32-character random password.
        """
        alphabet = string.ascii_letters + string.digits
        return "".join(secrets.choice(alphabet) for _ in range(32))

    def generate_mariadb_password(self) -> str:
        """Generate a secure random password for MariaDB.

        Deprecated: Use generate_db_password() instead.

        Returns:
            A 32-character random password.
        """
        return self.generate_db_password()

    def is_production(self) -> bool:
        """Check if running in production mode.

        Returns:
            True if APP_ENV is 'prod' or 'production'.
        """
        env = self.get("APP_ENV", "dev")
        return env in ("prod", "production")

    def get_certs_dir(self) -> Path:
        """Get the configured certificates directory.

        Returns:
            Path to the certificates directory.
        """
        certs_dir = self.get("CERTS_DIR") or "./caddy/certs"
        return Path(certs_dir)

    def build_db_env_vars(self, db_engines: list[str], localhost: str) -> dict[str, str]:
        """Build environment variables for database engines.

        Args:
            db_engines: List of enabled database engines.
            localhost: Localhost prefix for port binding (e.g. "127.0.0.1:" or "").

        Returns:
            Dictionary of environment variables for docker-compose.
        """
        env_vars: dict[str, str] = {}

        if "mariadb" in db_engines:
            env_vars["MARIADB_ROOT_PASSWORD"] = self.require("MARIADB_ROOT_PASSWORD")
            env_vars["MARIADB_VERSION"] = self.get("MARIADB_VERSION") or "10.11.9"

        if "mysql" in db_engines:
            mysql_port = self.get("MYSQL_PORT") or "3307"
            env_vars["MYSQL_ROOT_PASSWORD"] = self.require("MYSQL_ROOT_PASSWORD")
            env_vars["MYSQL_VERSION"] = self.get("MYSQL_VERSION") or "8.0"
            env_vars["MYSQL_PORT"] = f"{localhost}{mysql_port}:3306"
            pma_mysql_port = self.get("PMA_MYSQL_PORT") or "8082"
            env_vars["PMA_MYSQL_PORT"] = f"{localhost}{pma_mysql_port}:80"

        if "postgresql" in db_engines:
            pg_port = self.get("POSTGRES_PORT") or "5432"
            env_vars["POSTGRES_PASSWORD"] = self.require("POSTGRES_PASSWORD")
            env_vars["POSTGRES_USER"] = self.get("POSTGRES_USER") or "postgres"
            env_vars["POSTGRES_VERSION"] = self.get("POSTGRES_VERSION") or "16"
            env_vars["POSTGRES_PORT"] = f"{localhost}{pg_port}:5432"
            pgadmin_port = self.get("PGADMIN_PORT") or "8081"
            env_vars["PGADMIN_PORT"] = f"{localhost}{pgadmin_port}:80"

        return env_vars
