"""Unit tests for DockerManager container status lookups."""

from pathlib import Path
from unittest.mock import MagicMock

from frankenmanager.core.docker_manager import BUILD_HASH_LABEL, DockerManager


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


class TestComposeContent:
    """Test the generated docker-compose services."""

    def test_single_phpmyadmin_serves_mariadb_and_mysql(self, tmp_path):
        """Both servers are listed in one phpMyAdmin, MariaDB first (the default)."""
        manager = DockerManager(tmp_path)

        content = manager.generate_compose_file(
            set(), {}, db_engines=["mysql", "mariadb"]
        ).read_text()

        assert content.count("container_name: franken_phpmyadmin") == 1
        assert "PMA_HOSTS: franken_mariadb,franken_mysql" in content
        assert "FRANKEN_PASSWORD_FRANKEN_MARIADB: ${MARIADB_ROOT_PASSWORD}" in content
        assert "FRANKEN_PASSWORD_FRANKEN_MYSQL: ${MYSQL_ROOT_PASSWORD}" in content
        assert (tmp_path / "phpmyadmin" / "config.user.inc.php").exists()

    def test_no_phpmyadmin_for_postgresql_only(self, tmp_path):
        content = (
            DockerManager(tmp_path)
            .generate_compose_file(set(), {}, db_engines=["postgresql"])
            .read_text()
        )

        assert "franken_phpmyadmin" not in content
        assert "franken_pgadmin" in content

    def test_redis_is_opt_in(self, tmp_path):
        manager = DockerManager(tmp_path)

        without = manager.generate_compose_file({"8.4"}, {}).read_text()
        with_redis = manager.generate_compose_file({"8.4"}, {}, redis=True).read_text()

        assert "franken_redis" not in without
        assert "      cache:" not in without
        assert "franken_redis" in with_redis
        assert "      cache:" in with_redis


class TestConditionalBuild:
    """Images are only rebuilt when missing, outdated or forced."""

    def _manager(self, tmp_path, labels):
        (tmp_path / "Dockerfile").write_text("FROM scratch\n")
        manager = DockerManager(tmp_path)
        manager._client = MagicMock()
        manager._client.images.get.return_value.labels = labels
        return manager

    def test_up_to_date_image_is_not_rebuilt(self, tmp_path):
        manager = self._manager(tmp_path, {})
        build_args = {"CUSTOM_PATH": "/projects", "PHP_IMAGE_TAG": "1-php8.4-alpine"}
        manager._client.images.get.return_value.labels = {
            BUILD_HASH_LABEL: manager.compute_build_hash(build_args)
        }

        assert manager.build_image("/projects", "8.4", force=False) is False
        manager._client.images.build.assert_not_called()

    def test_changed_inputs_trigger_a_rebuild(self, tmp_path):
        manager = self._manager(tmp_path, {BUILD_HASH_LABEL: "stale"})

        assert manager.build_image("/projects", "8.4", force=False) is True
        manager._client.images.build.assert_called_once()

    def test_force_rebuilds_an_up_to_date_image(self, tmp_path):
        manager = self._manager(tmp_path, {})
        build_args = {"CUSTOM_PATH": "/projects", "PHP_IMAGE_TAG": "1-php8.4-alpine"}
        manager._client.images.get.return_value.labels = {
            BUILD_HASH_LABEL: manager.compute_build_hash(build_args)
        }

        assert manager.build_image("/projects", "8.4", force=True) is True
