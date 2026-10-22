"""Unit tests for DockerManager container status lookups."""

from pathlib import Path
from unittest.mock import MagicMock

from frankenmanager.core.docker_manager import DockerManager


def _manager_with_containers(summaries):
    manager = DockerManager(Path("/tmp"))
    manager._client = MagicMock()
    manager._client.api.containers.return_value = summaries
    return manager


class TestGetContainersStatus:
    """Test the single-call container status snapshot."""

    def test_parses_state_and_health_from_list_summaries(self):
        """Health is read from the human-readable Status of the list endpoint."""
        manager = _manager_with_containers(
            [
                {"Names": ["/franken_redis"], "State": "running", "Status": "Up 2 hours (healthy)"},
                {
                    "Names": ["/franken_mariadb"],
                    "State": "running",
                    "Status": "Up 1 second (health: starting)",
                },
                {
                    "Names": ["/franken-caddy-proxy"],
                    "State": "running",
                    "Status": "Up 3 minutes (unhealthy)",
                },
                {
                    "Names": ["/franken_phpmyadmin"],
                    "State": "exited",
                    "Status": "Exited (0) 1 hour ago",
                },
            ]
        )

        statuses = manager.get_containers_status()

        assert statuses == {
            "franken_redis": {"status": "running", "health": "healthy"},
            "franken_mariadb": {"status": "running", "health": "starting"},
            "franken-caddy-proxy": {"status": "running", "health": "unhealthy"},
            "franken_phpmyadmin": {"status": "exited", "health": "N/A"},
        }
        manager._client.api.containers.assert_called_once_with(all=True)

    def test_returns_empty_mapping_when_docker_is_unreachable(self):
        """An unreachable daemon reads as "no container", not as a crash."""
        manager = DockerManager(Path("/tmp"))
        manager._client = MagicMock()
        manager._client.api.containers.side_effect = Exception("Cannot connect")

        assert manager.get_containers_status() == {}
