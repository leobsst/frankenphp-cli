"""Environment file (.env) management."""

import os
import secrets
import string
from pathlib import Path
from typing import Optional

from ..exceptions import ConfigurationError
from ..utils.logging import log_warning
from . import soketi
from .php_versions import resolve_image_distro


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

    def get_extra_packages(self, php_version: str, package_type: str) -> str:
        """Merge the version-agnostic and per-version extra package lists.

        Reads `EXTRA_{package_type}_PACKAGES` (applies to every PHP version)
        and `EXTRA_{package_type}_PACKAGES_<version without dot>` (e.g.
        `EXTRA_APT_PACKAGES_84` for PHP 8.4), then concatenates them.

        Args:
            php_version: PHP version string (e.g., "8.4").
            package_type: "APT", "APK" or "COMPOSER".

        Returns:
            Space-separated, de-duplicated package list (may be empty).
        """
        suffix = php_version.replace(".", "")
        global_packages = self.get(f"EXTRA_{package_type}_PACKAGES") or ""
        version_packages = self.get(f"EXTRA_{package_type}_PACKAGES_{suffix}") or ""

        seen: set[str] = set()
        packages: list[str] = []
        for pkg in f"{global_packages} {version_packages}".split():
            if pkg not in seen:
                seen.add(pkg)
                packages.append(pkg)
        return " ".join(packages)

    def get_image_distro(self) -> str:
        """Get the base image distribution from PHP_IMAGE_DISTRO.

        Returns:
            "alpine" (default) or "trixie".

        Raises:
            ConfigurationError: If PHP_IMAGE_DISTRO holds an unsupported value.
        """
        try:
            return resolve_image_distro(self.get("PHP_IMAGE_DISTRO"))
        except ValueError as e:
            raise ConfigurationError(str(e)) from e

    def get_extra_system_packages(self, php_version: str) -> str:
        """Get the extra system packages matching the configured distribution.

        Reads EXTRA_APK_PACKAGES* on Alpine and EXTRA_APT_PACKAGES* on Trixie,
        since package names differ between the two package managers.

        Args:
            php_version: PHP version string (e.g., "8.4").

        Returns:
            Space-separated, de-duplicated package list (may be empty).
        """
        if self.get_image_distro() == "alpine":
            if self.get_extra_packages(php_version, "APT"):
                log_warning(
                    "EXTRA_APT_PACKAGES is ignored on Alpine images: use EXTRA_APK_PACKAGES, "
                    "or set PHP_IMAGE_DISTRO=trixie."
                )
            return self.get_extra_packages(php_version, "APK")
        return self.get_extra_packages(php_version, "APT")

    def is_pdftk_enabled(self) -> bool:
        """Check if pdftk (and the Java runtime it needs) should be installed.

        Returns:
            True if INSTALL_PDFTK is 'true'.
        """
        return self.get("INSTALL_PDFTK") == "true"

    def is_production(self) -> bool:
        """Check if running in production mode.

        Returns:
            True if APP_ENV is 'prod' or 'production'.
        """
        env = self.get("APP_ENV", "dev")
        return env in ("prod", "production")

    def is_redis_enabled(self) -> bool:
        """Check if the Redis cache container should run.

        Returns:
            True if REDIS_ENABLED is 'true' (disabled by default).
        """
        return (self.get("REDIS_ENABLED") or "").strip().lower() == "true"

    def get_db_engines(self) -> list[str]:
        """Get the enabled database engines from DB_ENGINES.

        Returns:
            Valid engines in the configured order, ["mariadb"] when none is set.
        """
        from .docker_manager import parse_db_engines  # noqa: PLC0415

        return parse_db_engines(self.get("DB_ENGINES") or "mariadb") or ["mariadb"]

    def get_db_admin_target(self) -> Optional[str]:
        """Get the database admin UI upstream that https://localhost should proxy to.

        phpMyAdmin owns localhost whenever MariaDB or MySQL is enabled (a single
        container serves both servers). pgAdmin takes it over when PostgreSQL is
        the only engine; otherwise it stays reachable on PGADMIN_PORT.

        Returns:
            "127.0.0.1:<port>" in dev when an admin UI runs, None in production.
        """
        if self.is_production():
            return None
        db_engines = self.get_db_engines()
        if "mariadb" in db_engines or "mysql" in db_engines:
            return f"127.0.0.1:{self.get('PMA_PORT') or '8080'}"
        if "postgresql" in db_engines:
            return f"127.0.0.1:{self.get('PGADMIN_PORT') or '8081'}"
        return None

    def resolve_dir(self, key: str, default: str, project_dir: Path) -> Path:
        """Resolve a storage directory from .env, relative to the project directory.

        Args:
            key: Variable name (e.g. "CADDY_DIR").
            default: Default relative path when the variable is empty.
            project_dir: Base directory for relative paths.

        Returns:
            Absolute path.
        """
        path = Path(self.get(key) or default)
        return path if path.is_absolute() else project_dir / path

    def build_compose_env_vars(
        self, project_dir: Path, custom_path: str, db_engines: list[str]
    ) -> dict[str, str]:
        """Build every variable the generated docker-compose file expects.

        Args:
            project_dir: Project (data) directory.
            custom_path: Projects root mounted into the PHP containers.
            db_engines: Enabled database engines.

        Returns:
            Environment variables for `docker compose`.
        """
        localhost = "" if self.get("EXPOSE_SERVICES") == "true" else "127.0.0.1:"
        db_port = self.get("DB_PORT") or "3306"
        pma_port = self.get("PMA_PORT") or "8080"
        redis_port = self.get("REDIS_PORT") or "6379"
        return {
            "CUSTOM_PATH": custom_path,
            "UID": self.require("UID"),
            "GID": self.require("GID"),
            "DB_PORT": f"{localhost}{db_port}:3306",
            "PMA_PORT": f"{localhost}{pma_port}:80",
            "REDIS_PORT": f"{localhost}{redis_port}:6379",
            "WEB_HTTP_PORT": self.get("WEB_HTTP_PORT") or "80",
            "WEB_HTTPS_PORT": self.get("WEB_HTTPS_PORT") or "443",
            "MYSQL_MAX_ALLOWED_PACKET": self.get("MYSQL_MAX_ALLOWED_PACKET") or "512M",
            "PWD": str(project_dir),
            "CADDY_DIR": str(self.resolve_dir("CADDY_DIR", "./caddy", project_dir)),
            "DATABASE_DIR": str(self.resolve_dir("DATABASE_DIR", "./database", project_dir)),
            **self.build_db_env_vars(db_engines, localhost),
            **soketi.build_compose_env_vars(self, localhost),
        }

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

        if "postgresql" in db_engines:
            pg_port = self.get("POSTGRES_PORT") or "5432"
            env_vars["POSTGRES_PASSWORD"] = self.require("POSTGRES_PASSWORD")
            env_vars["POSTGRES_USER"] = self.get("POSTGRES_USER") or "postgres"
            env_vars["POSTGRES_VERSION"] = self.get("POSTGRES_VERSION") or "16"
            env_vars["POSTGRES_PORT"] = f"{localhost}{pg_port}:5432"
            pgadmin_port = self.get("PGADMIN_PORT") or "8081"
            env_vars["PGADMIN_PORT"] = f"{localhost}{pgadmin_port}:80"

        return env_vars
