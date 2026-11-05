"""Unit tests for the automatic https://localhost -> phpMyAdmin block."""

import pytest
from frankenmanager.core.caddyfile import CaddyfileGenerator
from frankenmanager.core.environment import EnvironmentManager


def _generate(tmp_path, **kwargs):
    kwargs.setdefault("domains_versions", [("app.test", "8.4")])
    CaddyfileGenerator(tmp_path).generate_main_caddyfile(caddy_dir=tmp_path, **kwargs)
    return (tmp_path / "Caddyfile").read_text()


def test_localhost_proxies_to_phpmyadmin(tmp_path):
    content = _generate(tmp_path, phpmyadmin_target="127.0.0.1:8081")

    assert "localhost {" in content
    assert "tls /certs/localhost.pem /certs/localhost-key.pem" in content
    assert "reverse_proxy 127.0.0.1:8081" in content


def test_no_localhost_block_without_phpmyadmin(tmp_path):
    content = _generate(tmp_path)

    assert "localhost {" not in content


def test_user_registered_localhost_wins(tmp_path):
    content = _generate(
        tmp_path,
        raw_proxies=[("localhost", "127.0.0.1:3000")],
        phpmyadmin_target="127.0.0.1:8080",
    )

    assert content.count("localhost {") == 1
    assert "reverse_proxy 127.0.0.1:3000" in content
    assert "127.0.0.1:8080" not in content


@pytest.mark.parametrize(
    ("env_content", "expected"),
    [
        ("", "127.0.0.1:8080"),
        ("DB_ENGINES=mariadb,mysql\nPMA_PORT=9000", "127.0.0.1:9000"),
        ("DB_ENGINES=mysql", "127.0.0.1:8082"),
        ("DB_ENGINES=mysql\nPMA_MYSQL_PORT=9002", "127.0.0.1:9002"),
        ("DB_ENGINES=postgresql", None),
        ("APP_ENV=prod", None),
    ],
)
def test_phpmyadmin_target(tmp_path, env_content, expected):
    env_path = tmp_path / ".env"
    env_path.write_text(env_content)
    env = EnvironmentManager(env_path, tmp_path / ".env.example")
    env.load()

    assert env.get_phpmyadmin_target() == expected
