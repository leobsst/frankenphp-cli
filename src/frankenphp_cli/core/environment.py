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

    def generate_mariadb_password(self) -> str:
        """Generate a secure random password for MariaDB.

        Returns:
            A 32-character random password.
        """
        alphabet = string.ascii_letters + string.digits
        return "".join(secrets.choice(alphabet) for _ in range(32))

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
