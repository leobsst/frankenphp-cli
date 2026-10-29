"""Unit tests for the optional LAN DNS server."""

from pathlib import Path

import pytest

from frankenmanager.core import dns_server
from frankenmanager.core.database import DatabaseManager
from frankenmanager.core.docker_manager import DockerManager
from frankenmanager.core.environment import EnvironmentManager
from frankenmanager.exceptions import ConfigurationError


def _env(tmp_path: Path, content: str) -> EnvironmentManager:
    env = EnvironmentManager(tmp_path / ".env", tmp_path / ".env.example")
    env.env_path.write_text(content)
    env.load()
    return env


class TestComposeService:
    """The DNS service only appears in the compose file when enabled."""

    def test_dns_service_published_on_listen_address(self):
        content = DockerManager(Path("/tmp"))._build_compose_content(
            {"8.3"}, production=False, dns_listen="192.168.1.42:53"
        )

        assert f"container_name: {dns_server.CONTAINER_NAME}" in content
        assert '      - "192.168.1.42:53:53/udp"' in content
        assert '      - "192.168.1.42:53:53/tcp"' in content
        assert "      - ./dns:/etc/coredns:ro" in content

    def test_no_dns_service_when_disabled(self):
        content = DockerManager(Path("/tmp"))._build_compose_content({"8.3"}, production=False)

        assert dns_server.CONTAINER_NAME not in content


class TestListenAddress:
    """Listen address resolution from .env."""

    def test_disabled_by_default(self, tmp_path):
        assert dns_server.get_listen_address(_env(tmp_path, "")) is None

    def test_uses_configured_ip_and_port(self, tmp_path):
        env = _env(tmp_path, "DNS_SERVER=true\nDNS_IP=10.0.0.5\nDNS_PORT=5353\n")

        assert dns_server.get_listen_address(env) == "10.0.0.5:5353"

    def test_rejects_invalid_ip(self, tmp_path):
        env = _env(tmp_path, "DNS_SERVER=true\nDNS_IP=not-an-ip\n")

        with pytest.raises(ConfigurationError):
            dns_server.get_listen_address(env)


class TestSync:
    """Records cover every kind of registered domain."""

    def test_writes_records_for_domains_aliases_and_proxies(self, tmp_path):
        env = _env(tmp_path, "DNS_SERVER=true\nDNS_IP=10.0.0.5\n")
        db = DatabaseManager(tmp_path / "db.sqlite")
        db.set_domains_with_versions([("shop.test", "8.3")])
        db.add_aliases(["shop-alt.test"], "shop.test")
        db.add_proxies(["api.test"], "127.0.0.1:8006")

        dns_server.sync(tmp_path, env, db)

        records = (tmp_path / "dns" / "hosts").read_text().splitlines()
        assert records[1:] == [
            "10.0.0.5 api.test",
            "10.0.0.5 shop-alt.test",
            "10.0.0.5 shop.test",
        ]
        assert "forward . 1.1.1.1 9.9.9.9" in (tmp_path / "dns" / "Corefile").read_text()

    def test_does_nothing_when_disabled(self, tmp_path):
        env = _env(tmp_path, "DNS_SERVER=false\n")
        db = DatabaseManager(tmp_path / "db.sqlite")

        dns_server.sync(tmp_path, env, db)

        assert not (tmp_path / "dns").exists()
