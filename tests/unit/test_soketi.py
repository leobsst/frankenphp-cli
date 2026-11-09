"""Unit tests for the optional Soketi WebSocket server."""

from frankenmanager.core import soketi
from frankenmanager.core.caddyfile import CaddyfileGenerator
from frankenmanager.core.docker_manager import DockerManager
from frankenmanager.core.environment import EnvironmentManager


def _env(tmp_path, content):
    env_path = tmp_path / ".env"
    env_path.write_text(content)
    env = EnvironmentManager(env_path, tmp_path / ".env.example")
    env.load()
    return env


def _main_caddyfile(tmp_path, with_cert=True, **kwargs):
    if with_cert:
        (tmp_path / "certs").mkdir(exist_ok=True)
        (tmp_path / "certs" / f"{soketi.DOMAIN}.pem").write_text("")
    kwargs.setdefault("domains_versions", [("app.test", "8.4")])
    CaddyfileGenerator(tmp_path).generate_main_caddyfile(caddy_dir=tmp_path, **kwargs)
    return (tmp_path / "Caddyfile").read_text()


def test_soketi_is_opt_in(tmp_path):
    manager = DockerManager(tmp_path)

    without = manager.generate_compose_file({"8.4"}, {}).read_text()
    with_soketi = manager.generate_compose_file({"8.4"}, {}, websocket=True).read_text()

    assert soketi.CONTAINER_NAME not in without
    assert soketi.CONTAINER_NAME in with_soketi
    assert "${SOKETI_PUBLISH:-127.0.0.1:6001:6001}" in with_soketi
    # Never an env_file: our SOKETI_PORT is a host port, not Soketi's own setting
    service = with_soketi.split("  soketi:")[1].split("\n\n")[0]
    assert "env_file" not in service


def test_websocket_host_proxies_to_soketi(tmp_path):
    content = _main_caddyfile(tmp_path, websocket_target="127.0.0.1:6001")

    assert f"{soketi.DOMAIN} {{" in content
    assert f"tls /certs/{soketi.DOMAIN}.pem /certs/{soketi.DOMAIN}-key.pem" in content
    assert "reverse_proxy 127.0.0.1:6001" in content


def test_no_websocket_block_until_its_certificate_exists(tmp_path):
    # e.g. SOKETI_ENABLED turned on while running, then add-host: Caddy would
    # refuse the whole config over the missing certificate
    content = _main_caddyfile(tmp_path, with_cert=False, websocket_target="127.0.0.1:6001")

    assert soketi.DOMAIN not in content


def test_user_registered_websocket_host_wins(tmp_path):
    content = _main_caddyfile(
        tmp_path,
        raw_proxies=[(soketi.DOMAIN, "127.0.0.1:3000")],
        websocket_target="127.0.0.1:6001",
    )

    assert content.count(f"{soketi.DOMAIN} {{") == 1
    assert "127.0.0.1:6001" not in content


def test_disabled_soketi_needs_nothing(tmp_path):
    env = _env(tmp_path, "SOKETI_DEFAULT_APP_KEY=\n")

    assert soketi.get_target(env) is None
    assert soketi.get_domains(env) == []
    assert soketi.ensure_credentials(env) == []
    assert soketi.build_compose_env_vars(env, "127.0.0.1:") == {}


def test_credentials_generated_only_when_empty(tmp_path):
    env = _env(
        tmp_path,
        "SOKETI_ENABLED=true\nSOKETI_PORT=6002\nSOKETI_DEFAULT_APP_ID=my-app\n"
        "SOKETI_DEFAULT_APP_KEY=\n",
    )

    generated = soketi.ensure_credentials(env)

    assert generated == ["SOKETI_DEFAULT_APP_KEY", "SOKETI_DEFAULT_APP_SECRET"]
    saved = _env(tmp_path, (tmp_path / ".env").read_text())
    assert saved.get("SOKETI_DEFAULT_APP_ID") == "my-app"
    assert len(saved.get("SOKETI_DEFAULT_APP_KEY") or "") == 32
    assert len(saved.get("SOKETI_DEFAULT_APP_SECRET") or "") == 32
    assert soketi.get_target(saved) == "127.0.0.1:6002"
    assert soketi.build_compose_env_vars(saved, "127.0.0.1:")["SOKETI_PUBLISH"] == (
        "127.0.0.1:6002:6001"
    )
