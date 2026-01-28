"""Docker container and compose management."""

import os
import subprocess
from pathlib import Path
from typing import TYPE_CHECKING, Optional

from ..exceptions import DockerError

if TYPE_CHECKING:
    import docker


class DockerManager:
    """Manages Docker containers and compose operations."""

    CONTAINERS = [
        "webserver-and-caddy",
        "franken_mariadb",
        "franken_phpmyadmin",
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

    def build_image(self, custom_path: str, wwwgroup: str = "") -> None:
        """Build the custom FrankenPHP Docker image.

        Args:
            custom_path: The custom path build argument.
            wwwgroup: The www group build argument.

        Raises:
            DockerError: If the build fails.
        """
        build_args = {"CUSTOM_PATH": custom_path}
        if wwwgroup:
            build_args["WWWGROUP"] = wwwgroup

        try:
            self.client.images.build(
                path=str(self.project_dir),
                tag="custom-frankenphp:latest",
                buildargs=build_args,
                rm=True,
            )
        except Exception as e:
            raise DockerError(f"Failed to build image: {e}")

    def compose_up(self, env_vars: dict[str, str], production: bool = False) -> None:
        """Start containers using docker-compose.

        Args:
            env_vars: Environment variables for compose.
            production: Whether to use production compose file.

        Raises:
            DockerError: If compose up fails.
        """
        compose_file = "docker-compose-prod.yml" if production else "docker-compose.yml"
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
        compose_file = "docker-compose-prod.yml" if production else "docker-compose.yml"
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

    def restart_all(self) -> None:
        """Restart all FrankenPHP containers."""
        for name in self.CONTAINERS:
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
