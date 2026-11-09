"""Unit tests for the optional LAN DNS server."""

from pathlib import Path

import pytest
from frankenmanager.core import dns_server, dns_watcher
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

    def test_listens_on_every_interface_by_default(self, tmp_path):
        env = _env(tmp_path, "DNS_SERVER=true\nDNS_IP=10.0.0.5\n")

        assert dns_server.get_listen_address(env) == "0.0.0.0:53"

    def test_uses_configured_listen_ip_and_port(self, tmp_path):
        env = _env(tmp_path, "DNS_SERVER=true\nDNS_LISTEN_IP=10.0.0.5\nDNS_PORT=5353\n")

        assert dns_server.get_listen_address(env) == "10.0.0.5:5353"

    def test_rejects_invalid_listen_ip(self, tmp_path):
        env = _env(tmp_path, "DNS_SERVER=true\nDNS_LISTEN_IP=not-an-ip\n")

        with pytest.raises(ConfigurationError):
            dns_server.get_listen_address(env)

    def test_rejects_invalid_records_ip(self, tmp_path):
        env = _env(tmp_path, "DNS_SERVER=true\nDNS_IP=not-an-ip\n")

        with pytest.raises(ConfigurationError):
            dns_server.resolve_ip(env)


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


class TestWatcher:
    """Records follow the host's LAN IP without restarting anything."""

    @pytest.fixture
    def project(self, tmp_path):
        (tmp_path / ".env").write_text("DNS_SERVER=true\n")
        DatabaseManager(tmp_path / "db.sqlite").set_domains_with_versions([("shop.test", "8.3")])
        return tmp_path

    def _records(self, project: Path) -> list[str]:
        return (project / "dns" / "hosts").read_text().splitlines()[1:]

    def test_rewrites_records_when_lan_ip_changes(self, project, monkeypatch):
        monkeypatch.setattr(dns_watcher, "get_lan_ip", lambda: "10.0.0.7")
        monkeypatch.setattr(dns_server, "get_lan_ip", lambda: "10.0.0.7")

        assert dns_watcher.check_once(project, "10.0.0.5") == "10.0.0.7"
        assert self._records(project) == ["10.0.0.7 shop.test"]

    def test_keeps_records_while_offline(self, project, monkeypatch):
        monkeypatch.setattr(dns_watcher, "get_lan_ip", lambda: "127.0.0.1")

        assert dns_watcher.check_once(project, "10.0.0.5") == "10.0.0.5"
        assert not (project / "dns").exists()

    def test_leaves_fixed_dns_ip_alone(self, project, monkeypatch):
        (project / ".env").write_text("DNS_SERVER=true\nDNS_IP=10.0.0.5\n")
        monkeypatch.setattr(dns_watcher, "get_lan_ip", lambda: "10.0.0.7")

        assert dns_watcher.check_once(project, "10.0.0.5") == "10.0.0.5"
        assert not (project / "dns").exists()


class TestServerAddresses:
    """Every LAN address the DNS server answers on, shown on start."""

    IFCONFIG = (
        "lo0: flags=8049<UP,LOOPBACK,RUNNING,MULTICAST> mtu 16384\n"
        "\tinet 127.0.0.1 netmask 0xff000000\n"
        "en0: flags=8863<UP,BROADCAST,SMART,RUNNING,SIMPLEX,MULTICAST> mtu 1500\n"
        "\tinet6 fe80::1%en0 prefixlen 64 scopeid 0x4\n"
        "\tinet 192.168.1.10 netmask 0xffffff00 broadcast 192.168.1.255\n"
        "en1: flags=8863<UP,BROADCAST,SMART,RUNNING,SIMPLEX,MULTICAST> mtu 1500\n"
        "\tinet 169.254.3.4 netmask 0xffff0000\n"
        "\tinet 192.168.1.11 netmask 0xffffff00 broadcast 192.168.1.255\n"
        "bridge100: flags=8863<UP,BROADCAST,SMART,RUNNING,SIMPLEX,MULTICAST> mtu 1500\n"
    )
    IP_ADDR = (
        "1: lo    inet 127.0.0.1/8 scope host lo\\       valid_lft forever\n"
        "2: eth0    inet 192.168.1.10/24 brd 192.168.1.255 scope global eth0\\       valid_lft\n"
        "3: wlan0    inet 192.168.1.11/24 brd 192.168.1.255 scope global wlan0\\       valid_lft\n"
        "4: docker0    inet 172.17.0.1/16 brd 172.17.255.255 scope global docker0\\       valid\n"
    )

    def test_parses_ifconfig(self):
        assert dns_server.parse_ifconfig(self.IFCONFIG) == [
            ("lo0", "127.0.0.1"),
            ("en0", "192.168.1.10"),
            ("en1", "169.254.3.4"),
            ("en1", "192.168.1.11"),
        ]

    def test_parses_ip_addr(self):
        assert dns_server.parse_ip_addr(self.IP_ADDR) == [
            ("lo", "127.0.0.1"),
            ("eth0", "192.168.1.10"),
            ("wlan0", "192.168.1.11"),
            ("docker0", "172.17.0.1"),
        ]

    def test_keeps_only_lan_reachable_addresses(self, monkeypatch):
        monkeypatch.setattr(
            dns_server, "_run", lambda cmd: self.IP_ADDR if cmd[0] == "ip" else None
        )

        assert dns_server.list_interface_ips() == [
            ("eth0", "192.168.1.10"),
            ("wlan0", "192.168.1.11"),
        ]

    def test_records_ip_comes_first(self, tmp_path, monkeypatch):
        monkeypatch.setattr(
            dns_server, "_run", lambda cmd: self.IFCONFIG if cmd[0] == "ifconfig" else None
        )
        env = _env(tmp_path, "DNS_SERVER=true\nDNS_IP=192.168.1.11\n")

        assert dns_server.get_server_addresses(env) == [
            ("en1", "192.168.1.11"),
            ("en0", "192.168.1.10"),
        ]

    def test_only_the_pinned_listen_ip(self, tmp_path, monkeypatch):
        monkeypatch.setattr(dns_server, "_run", lambda cmd: self.IP_ADDR)
        env = _env(tmp_path, "DNS_SERVER=true\nDNS_LISTEN_IP=192.168.1.11\n")

        assert dns_server.get_server_addresses(env) == [("", "192.168.1.11")]

    def test_describes_every_address_and_marks_the_records_one(self):
        lines = dns_server.describe_server_addresses(
            [("en0", "192.168.1.10"), ("en1", "192.168.1.11")], "192.168.1.10"
        )

        assert "    - 192.168.1.10 (en0, domains resolve to it)" in lines
        assert "    - 192.168.1.11 (en1)" in lines
        assert "primary and secondary" in lines[-1]

    def test_describes_a_single_address_in_one_line(self):
        assert dns_server.describe_server_addresses([("", "10.0.0.5")], "10.0.0.5") == [
            "  Set 10.0.0.5 as the DNS server of your other devices (or of your router)."
        ]
