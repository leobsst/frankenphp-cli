"""Docker container and compose management."""

import os
import subprocess
from pathlib import Path
from typing import TYPE_CHECKING, Optional

from ..exceptions import DockerError
from .php_versions import get_container_name, get_image_name, get_image_tag, get_internal_ports

if TYPE_CHECKING:
    import docker


REVERSE_PROXY_CONTAINER = "franken-caddy-proxy"

# Valid database engine identifiers
VALID_DB_ENGINES = {"mariadb", "mysql", "postgresql"}

# Container names per DB engine
DB_CONTAINERS = {
    "mariadb": "franken_mariadb",
    "mysql": "franken_mysql",
    "postgresql": "franken_postgresql",
}

# Admin UI containers per DB engine
DB_ADMIN_CONTAINERS = {
    "mariadb": "franken_phpmyadmin",
    "mysql": "franken_phpmyadmin_mysql",
    "postgresql": "franken_pgadmin",
}


def parse_db_engines(db_engines_str: str) -> list[str]:
    """Parse the DB_ENGINES configuration string.

    Args:
        db_engines_str: Comma-separated list of database engines.

    Returns:
        List of valid database engine names.
    """
    engines = [e.strip().lower() for e in db_engines_str.split(",") if e.strip()]
    return [e for e in engines if e in VALID_DB_ENGINES]


class DockerManager:
    """Manages Docker containers and compose operations."""

    # Infrastructure containers (non-PHP) - base set without DB-specific ones
    BASE_INFRA_CONTAINERS = [
        REVERSE_PROXY_CONTAINER,
        "franken_redis",
    ]

    def __init__(self, project_dir: Path) -> None:
        """Initialize the Docker manager.

        Args:
            project_dir: Path to the project directory.
        """
        self.project_dir = project_dir
        self._client: Optional[docker.DockerClient] = None

    @property
    def client(self) -> "docker.DockerClient":
        """Get the Docker client, initializing if needed.

        Returns:
            Docker client instance.

        Raises:
            DockerError: If cannot connect to Docker.
        """
        if self._client is None:
            try:
                import docker

                self._client = docker.from_env()
            except Exception as e:
                raise DockerError(f"Cannot connect to Docker: {e}")
        return self._client

    def get_all_containers(
        self,
        php_versions: Optional[set[str]] = None,
        db_engines: Optional[list[str]] = None,
        production: bool = False,
    ) -> list[str]:
        """Get the list of all container names including PHP version containers.

        Args:
            php_versions: Set of active PHP versions. If None, returns only infra containers.
            db_engines: List of active database engines. If None, defaults to ["mariadb"].
            production: Whether in production mode (no admin UIs).

        Returns:
            List of container names.
        """
        if db_engines is None:
            db_engines = ["mariadb"]

        containers = list(self.BASE_INFRA_CONTAINERS)

        # Add DB containers
        for engine in db_engines:
            container = DB_CONTAINERS.get(engine)
            if container:
                containers.append(container)
            # Admin UIs (dev only)
            if not production:
                admin = DB_ADMIN_CONTAINERS.get(engine)
                if admin:
                    containers.append(admin)

        if php_versions:
            for version in sorted(php_versions):
                containers.insert(0, get_container_name(version))
        return containers

    def build_image(self, custom_path: str, php_version: str, wwwgroup: str = "") -> None:
        """Build the custom FrankenPHP Docker image for a specific PHP version.

        Args:
            custom_path: The custom path build argument.
            php_version: PHP version to build for.
            wwwgroup: The www group build argument.

        Raises:
            DockerError: If the build fails.
        """
        image_tag = get_image_tag(php_version)
        image_name = get_image_name(php_version)

        build_args = {
            "CUSTOM_PATH": custom_path,
            "PHP_IMAGE_TAG": image_tag,
        }
        if wwwgroup:
            build_args["WWWGROUP"] = wwwgroup

        try:
            self.client.images.build(
                path=str(self.project_dir),
                tag=image_name,
                buildargs=build_args,
                rm=True,
            )
        except Exception as e:
            raise DockerError(f"Failed to build image for PHP {php_version}: {e}")

    def build_images(self, custom_path: str, php_versions: set[str], wwwgroup: str = "") -> None:
        """Build Docker images for all required PHP versions.

        Args:
            custom_path: The custom path build argument.
            php_versions: Set of PHP versions to build.
            wwwgroup: The www group build argument.
        """
        for version in sorted(php_versions):
            self.build_image(custom_path, version, wwwgroup)

    def generate_compose_file(
        self,
        php_versions: set[str],
        env_vars: dict[str, str],
        production: bool = False,
        db_engines: Optional[list[str]] = None,
    ) -> Path:
        """Generate a docker-compose file with services for each PHP version.

        Args:
            php_versions: Set of PHP versions to create services for.
            env_vars: Environment variables for compose.
            production: Whether to generate production compose file.
            db_engines: List of database engines to include. Defaults to ["mariadb"].

        Returns:
            Path to the generated compose file.
        """
        if db_engines is None:
            db_engines = ["mariadb"]
        compose_path = self.project_dir / "docker-compose-generated.yml"
        content = self._build_compose_content(php_versions, production, db_engines)
        compose_path.write_text(content)
        return compose_path

    def _build_db_services(self, db_engines: list[str], production: bool) -> list[str]:
        """Build database service definitions for docker-compose.

        Args:
            db_engines: List of database engines to include.
            production: Whether this is for production.

        Returns:
            List of YAML lines.
        """
        lines: list[str] = []
        # Track which DB services are generated for depends_on in PHP services
        db_service_names: list[str] = []

        for engine in db_engines:
            if engine == "mariadb":
                db_service_names.append("db-mariadb")
                if production:
                    lines.extend(
                        [
                            "  db-mariadb:",
                            "    container_name: franken_mariadb",
                            "    image: mariadb:${MARIADB_VERSION:-10.11.9}",
                            "    restart: always",
                            "    command: --max-allowed-packet=${MYSQL_MAX_ALLOWED_PACKET:-512M}",
                            "    network_mode: host",
                            "    user: ${UID}:${GID}",
                            "    env_file: .env",
                            "    volumes:",
                            "      - ${DATABASE_DIR:-${PWD}/database}/mariadb:/var/lib/mysql",
                            "    environment:",
                            "      MARIADB_ROOT_PASSWORD: ${MARIADB_ROOT_PASSWORD}",
                            "    healthcheck:",
                            "      test:"
                            ' ["CMD", "healthcheck.sh", "--connect", "--innodb_initialized"]',
                            "      interval: 10s",
                            "      timeout: 5s",
                            "      retries: 5",
                            "      start_period: 30s",
                            "",
                        ]
                    )
                else:
                    lines.extend(
                        [
                            "  db-mariadb:",
                            "    container_name: franken_mariadb",
                            "    image: mariadb:${MARIADB_VERSION:-10.11.9}",
                            "    restart: always",
                            "    command: --max-allowed-packet=${MYSQL_MAX_ALLOWED_PACKET:-512M}",
                            "    user: ${UID}:${GID}",
                            "    env_file: .env",
                            "    volumes:",
                            "      - ${DATABASE_DIR:-./database}/mariadb:/var/lib/mysql",
                            "    ports:",
                            "      - ${DB_PORT:-127.0.0.1:3306:3306}",
                            "    environment:",
                            "      MARIADB_ROOT_PASSWORD: ${MARIADB_ROOT_PASSWORD}",
                            "    networks:",
                            "      - franken_network",
                            "    healthcheck:",
                            "      test:"
                            ' ["CMD", "healthcheck.sh", "--connect", "--innodb_initialized"]',
                            "      interval: 10s",
                            "      timeout: 5s",
                            "      retries: 5",
                            "      start_period: 30s",
                            "",
                        ]
                    )

                # PhpMyAdmin for MariaDB (dev only)
                if not production:
                    lines.extend(
                        [
                            "  phpmyadmin:",
                            "    container_name: franken_phpmyadmin",
                            "    image: phpmyadmin:latest",
                            "    restart: always",
                            "    env_file: .env",
                            "    environment:",
                            "      PMA_HOST: franken_mariadb",
                            "      PMA_PORT: ${SIMPLE_DB_PORT:-3306}",
                            "      PMA_USER: root",
                            "      PMA_PASSWORD: ${MARIADB_ROOT_PASSWORD}",
                            "    ports:",
                            "      - ${PMA_PORT:-127.0.0.1:8080:80}",
                            "    depends_on:",
                            "      db-mariadb:",
                            "        condition: service_healthy",
                            "    networks:",
                            "      - franken_network",
                            "",
                        ]
                    )

            elif engine == "mysql":
                db_service_names.append("db-mysql")
                if production:
                    lines.extend(
                        [
                            "  db-mysql:",
                            "    container_name: franken_mysql",
                            "    image: mysql:${MYSQL_VERSION:-8.0}",
                            "    restart: always",
                            "    command: --max-allowed-packet=${MYSQL_MAX_ALLOWED_PACKET:-512M}",
                            "    network_mode: host",
                            "    user: ${UID}:${GID}",
                            "    env_file: .env",
                            "    volumes:",
                            "      - ${DATABASE_DIR:-${PWD}/database}/mysql:/var/lib/mysql",
                            "    environment:",
                            "      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD}",
                            "    healthcheck:",
                            '      test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]',
                            "      interval: 10s",
                            "      timeout: 5s",
                            "      retries: 5",
                            "      start_period: 30s",
                            "",
                        ]
                    )
                else:
                    lines.extend(
                        [
                            "  db-mysql:",
                            "    container_name: franken_mysql",
                            "    image: mysql:${MYSQL_VERSION:-8.0}",
                            "    restart: always",
                            "    command: --max-allowed-packet=${MYSQL_MAX_ALLOWED_PACKET:-512M}",
                            "    user: ${UID}:${GID}",
                            "    env_file: .env",
                            "    volumes:",
                            "      - ${DATABASE_DIR:-./database}/mysql:/var/lib/mysql",
                            "    ports:",
                            "      - ${MYSQL_PORT:-127.0.0.1:3307:3306}",
                            "    environment:",
                            "      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD}",
                            "    networks:",
                            "      - franken_network",
                            "    healthcheck:",
                            '      test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]',
                            "      interval: 10s",
                            "      timeout: 5s",
                            "      retries: 5",
                            "      start_period: 30s",
                            "",
                        ]
                    )

                # PhpMyAdmin for MySQL (dev only)
                if not production:
                    lines.extend(
                        [
                            "  phpmyadmin-mysql:",
                            "    container_name: franken_phpmyadmin_mysql",
                            "    image: phpmyadmin:latest",
                            "    restart: always",
                            "    env_file: .env",
                            "    environment:",
                            "      PMA_HOST: franken_mysql",
                            "      PMA_PORT: 3306",
                            "      PMA_USER: root",
                            "      PMA_PASSWORD: ${MYSQL_ROOT_PASSWORD}",
                            "    ports:",
                            "      - ${PMA_MYSQL_PORT:-127.0.0.1:8082:80}",
                            "    depends_on:",
                            "      db-mysql:",
                            "        condition: service_healthy",
                            "    networks:",
                            "      - franken_network",
                            "",
                        ]
                    )

            elif engine == "postgresql":
                db_service_names.append("db-postgresql")
                if production:
                    lines.extend(
                        [
                            "  db-postgresql:",
                            "    container_name: franken_postgresql",
                            "    image: postgres:${POSTGRES_VERSION:-16}-alpine",
                            "    restart: always",
                            "    network_mode: host",
                            "    env_file: .env",
                            "    volumes:",
                            "      - ${DATABASE_DIR:-${PWD}/database}"
                            "/postgresql:/var/lib/postgresql/data",
                            "    environment:",
                            "      POSTGRES_USER: ${POSTGRES_USER:-postgres}",
                            "      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}",
                            "    healthcheck:",
                            '      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-postgres}"]',
                            "      interval: 10s",
                            "      timeout: 5s",
                            "      retries: 5",
                            "      start_period: 30s",
                            "",
                        ]
                    )
                else:
                    lines.extend(
                        [
                            "  db-postgresql:",
                            "    container_name: franken_postgresql",
                            "    image: postgres:${POSTGRES_VERSION:-16}-alpine",
                            "    restart: always",
                            "    env_file: .env",
                            "    volumes:",
                            "      - ${DATABASE_DIR:-./database}"
                            "/postgresql:/var/lib/postgresql/data",
                            "    ports:",
                            "      - ${POSTGRES_PORT:-127.0.0.1:5432:5432}",
                            "    environment:",
                            "      POSTGRES_USER: ${POSTGRES_USER:-postgres}",
                            "      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}",
                            "    networks:",
                            "      - franken_network",
                            "    healthcheck:",
                            '      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-postgres}"]',
                            "      interval: 10s",
                            "      timeout: 5s",
                            "      retries: 5",
                            "      start_period: 30s",
                            "",
                        ]
                    )

                # pgAdmin (dev only)
                if not production:
                    lines.extend(
                        [
                            "  pgadmin:",
                            "    container_name: franken_pgadmin",
                            "    image: dpage/pgadmin4:latest",
                            "    restart: always",
                            "    environment:",
                            "      PGADMIN_DEFAULT_EMAIL: ${PGADMIN_EMAIL:-admin@admin.com}",
                            "      PGADMIN_DEFAULT_PASSWORD: ${POSTGRES_PASSWORD}",
                            "      PGADMIN_CONFIG_SERVER_MODE: 'False'",
                            "    ports:",
                            "      - ${PGADMIN_PORT:-127.0.0.1:8081:80}",
                            "    depends_on:",
                            "      db-postgresql:",
                            "        condition: service_healthy",
                            "    networks:",
                            "      - franken_network",
                            "",
                        ]
                    )

        return lines

    def _build_compose_content(
        self, php_versions: set[str], production: bool, db_engines: Optional[list[str]] = None
    ) -> str:
        """Build the docker-compose YAML content.

        Args:
            php_versions: Set of PHP versions.
            production: Whether this is for production.
            db_engines: List of database engines. Defaults to ["mariadb"].

        Returns:
            YAML content as string.
        """
        if db_engines is None:
            db_engines = ["mariadb"]

        lines: list[str] = ["services:"]

        # Database services
        lines.extend(self._build_db_services(db_engines, production))

        # Redis
        if production:
            lines.extend(
                [
                    "  cache:",
                    "    container_name: franken_redis",
                    "    image: redis:alpine",
                    "    restart: always",
                    "    network_mode: host",
                    "    command: redis-server",
                    "    healthcheck:",
                    '      test: ["CMD", "redis-cli", "ping"]',
                    "      interval: 10s",
                    "      timeout: 5s",
                    "      retries: 5",
                    "      start_period: 10s",
                    "",
                ]
            )
        else:
            lines.extend(
                [
                    "  cache:",
                    "    container_name: franken_redis",
                    "    image: redis:alpine",
                    "    restart: always",
                    "    command: redis-server",
                    "    ports:",
                    "      - ${REDIS_PORT:-127.0.0.1:6379:6379}",
                    "    networks:",
                    "      - franken_network",
                    "    healthcheck:",
                    '      test: ["CMD", "redis-cli", "ping"]',
                    "      interval: 10s",
                    "      timeout: 5s",
                    "      retries: 5",
                    "      start_period: 10s",
                    "",
                ]
            )

        # Caddy reverse proxy container (listens on 80/443, proxies to FrankenPHP containers)
        depends_on_php = []
        for version in sorted(php_versions):
            service_name = f"franken-php-{version.replace('.', '')}"
            depends_on_php.append(service_name)

        # Caddy reverse proxy: host network in both dev and prod
        # Host network ensures PHP containers can reach other domains via /etc/hosts
        # and access DB at 127.0.0.1:3306 (inter-project calls)
        if production:
            lines.extend(
                [
                    "  caddy-proxy:",
                    f"    container_name: {REVERSE_PROXY_CONTAINER}",
                    "    image: caddy:2-alpine",
                    "    restart: always",
                    "    network_mode: host",
                    "    volumes:",
                    "      - ${CADDY_DIR:-${PWD}/caddy}/Caddyfile:/etc/caddy/Caddyfile",
                    "      - ${CADDY_DIR:-${PWD}/caddy}/certs:/certs",
                    "      - ${CADDY_DIR:-${PWD}/caddy}/data:/data",
                    "      - ${CADDY_DIR:-${PWD}/caddy}/config:/config",
                    "      - ${CADDY_DIR:-${PWD}/caddy}/log:/var/log",
                    "      - /etc/letsencrypt:/etc/letsencrypt",
                ]
            )
        else:
            lines.extend(
                [
                    "  caddy-proxy:",
                    f"    container_name: {REVERSE_PROXY_CONTAINER}",
                    "    image: caddy:2-alpine",
                    "    restart: always",
                    "    network_mode: host",
                    "    volumes:",
                    "      - ${CADDY_DIR:-./caddy}/Caddyfile:/etc/caddy/Caddyfile",
                    "      - ${CADDY_DIR:-./caddy}/certs:/certs",
                    "      - ${CADDY_DIR:-./caddy}/data:/data",
                    "      - ${CADDY_DIR:-./caddy}/config:/config",
                    "      - ${CADDY_DIR:-./caddy}/log:/var/log",
                ]
            )

        if depends_on_php:
            lines.append("    depends_on:")
            for svc in depends_on_php:
                lines.append(f"      {svc}:")
                lines.append("        condition: service_started")
        lines.append("")

        # Build depends_on for PHP services (all DB services + cache)
        db_service_names = [f"db-{engine}" for engine in db_engines]

        # FrankenPHP services - one per PHP version, each on its own internal ports
        for version in sorted(php_versions):
            container_name = get_container_name(version)
            image_name = get_image_name(version)
            service_name = f"franken-php-{version.replace('.', '')}"
            php_ini_dir = f"php/{version}"
            http_port, https_port = get_internal_ports(version)

            if production:
                # Production: host network
                lines.extend(
                    [
                        f"  {service_name}:",
                        f"    container_name: {container_name}",
                        f"    image: {image_name}",
                        "    restart: always",
                        "    network_mode: host",
                        "    user: ${UID}:${GID}",
                        "    environment:",
                        f"      WEB_HTTP_PORT: '{http_port}'",
                        f"      WEB_HTTPS_PORT: '{https_port}'",
                        "    volumes:",
                        "      - ${CUSTOM_PATH}:/${CUSTOM_PATH}",
                        "      - ${CADDY_DIR:-${PWD}/caddy}/Caddyfile.worker:/etc/caddy/Caddyfile",
                        f"      - ${{CADDY_DIR:-${{PWD}}/caddy}}/sites/php-{version}"
                        ":/etc/caddy/sites/custom",
                        "      - ${CADDY_DIR:-${PWD}/caddy}/certs:/certs",
                        "      - ${CADDY_DIR:-${PWD}/caddy}/log:/var/log",
                        f"      - ${{PWD}}/{php_ini_dir}/php.ini:/usr/local/etc/php/php.ini",
                        f"      - ${{PWD}}/{php_ini_dir}/php-prod.ini"
                        ":/usr/local/etc/php/conf.d/99-production.ini",
                        "    depends_on:",
                    ]
                )
            else:
                # Dev: host network so PHP apps can reach other domains
                # via /etc/hosts and access DB at 127.0.0.1:3306
                lines.extend(
                    [
                        f"  {service_name}:",
                        f"    container_name: {container_name}",
                        f"    image: {image_name}",
                        "    restart: always",
                        "    network_mode: host",
                        "    environment:",
                        f"      WEB_HTTP_PORT: '{http_port}'",
                        f"      WEB_HTTPS_PORT: '{https_port}'",
                        "    volumes:",
                        "      - ${CUSTOM_PATH}:/${CUSTOM_PATH}",
                        "      - ${CADDY_DIR:-./caddy}/Caddyfile.worker:/etc/caddy/Caddyfile",
                        f"      - ${{CADDY_DIR:-./caddy}}/sites/php-{version}"
                        ":/etc/caddy/sites/custom",
                        "      - ${CADDY_DIR:-./caddy}/certs:/certs",
                        "      - ${CADDY_DIR:-./caddy}/log:/var/log",
                        f"      - ./{php_ini_dir}/php.ini:/usr/local/etc/php/php.ini",
                        "    depends_on:",
                    ]
                )

            # Add depends_on for all DB services
            for db_svc in db_service_names:
                lines.append(f"      {db_svc}:")
                lines.append("        condition: service_healthy")
            lines.append("      cache:")
            lines.append("        condition: service_healthy")
            lines.append("")

        # Networks (dev only)
        if not production:
            lines.extend(
                [
                    "networks:",
                    "  franken_network:",
                    "    driver: bridge",
                ]
            )

        return "\n".join(lines) + "\n"

    def compose_up(self, env_vars: dict[str, str], production: bool = False) -> None:
        """Start containers using docker-compose.

        Args:
            env_vars: Environment variables for compose.
            production: Whether to use production compose file.

        Raises:
            DockerError: If compose up fails.
        """
        compose_file = "docker-compose-generated.yml"
        cmd = ["docker", "--log-level", "error", "compose", "-f", compose_file, "up", "-d"]

        result = subprocess.run(
            cmd,
            cwd=self.project_dir,
            env={**os.environ, **env_vars},
            capture_output=True,
            text=True,
        )

        if result.returncode != 0:
            raise DockerError(f"Failed to start containers: {result.stderr}")

    def compose_down(self, production: bool = False) -> None:
        """Stop containers using docker-compose.

        Args:
            production: Whether to use production compose file.
        """
        compose_file = "docker-compose-generated.yml"
        compose_path = self.project_dir / compose_file

        # If generated file doesn't exist, try legacy files
        if not compose_path.exists():
            legacy_file = "docker-compose-prod.yml" if production else "docker-compose.yml"
            legacy_path = self.project_dir / legacy_file
            if legacy_path.exists():
                compose_file = legacy_file

        cmd = ["docker", "--log-level", "error", "compose", "-f", compose_file, "down"]

        subprocess.run(
            cmd,
            cwd=self.project_dir,
            capture_output=True,
        )

    def restart_container(self, name: str) -> bool:
        """Restart a single container by name.

        Args:
            name: Container name.

        Returns:
            True if restarted, False if not found.
        """
        try:
            container = self.client.containers.get(name)
            container.restart()
            return True
        except Exception:
            return False

    def restart_all(
        self,
        php_versions: Optional[set[str]] = None,
        db_engines: Optional[list[str]] = None,
        production: bool = False,
    ) -> None:
        """Restart all containers.

        Args:
            php_versions: Set of active PHP versions.
            db_engines: List of active database engines.
            production: Whether in production mode.
        """
        for name in self.get_all_containers(php_versions, db_engines, production):
            self.restart_container(name)

    def get_container_status(self, name: str) -> dict[str, str]:
        """Get status info for a container.

        Args:
            name: Container name.

        Returns:
            Dictionary with 'status' and 'health' keys.
        """
        try:
            container = self.client.containers.get(name)
            health = "N/A"
            state = container.attrs.get("State", {})
            if state.get("Health"):
                health = state["Health"].get("Status", "N/A")
            return {"status": container.status, "health": health}
        except Exception:
            return {"status": "not found", "health": "N/A"}

    def stop_container(self, name: str) -> bool:
        """Stop a single container by name.

        Args:
            name: Container name.

        Returns:
            True if stopped, False if not found.
        """
        try:
            container = self.client.containers.get(name)
            container.stop()
            container.remove()
            return True
        except Exception:
            return False

    def exec_in_container(self, container_name: str, command: list[str]) -> tuple[int, str]:
        """Execute a command inside a container.

        Args:
            container_name: Name of the container.
            command: Command to execute as list of strings.

        Returns:
            Tuple of (exit_code, output).
        """
        try:
            container = self.client.containers.get(container_name)
            result = container.exec_run(command)
            return result.exit_code, result.output.decode()
        except Exception as e:
            return -1, str(e)
