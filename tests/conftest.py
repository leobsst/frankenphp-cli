"""Pytest fixtures for FrankenPHP Manager tests."""

import tempfile
from pathlib import Path

import pytest


@pytest.fixture
def temp_project_dir():
    """Create a temporary project directory with required files."""
    with tempfile.TemporaryDirectory() as tmpdir:
        project = Path(tmpdir)

        # Create .env.example
        (project / ".env.example").write_text(
            "APP_ENV=dev\nUSER=\nGROUP=\nCERTS_DIR=./caddy/certs\nMARIADB_ROOT_PASSWORD=\n"
        )

        # Create Caddyfile template directory
        caddy_dir = project / "caddy"
        caddy_dir.mkdir()
        (caddy_dir / "Caddyfile.template").write_text(
            "full_domain {\n  root * /custom_domain/public/\n}\n"
        )

        # Create docker-compose files (empty for testing)
        (project / "docker-compose.yml").write_text("")
        (project / "docker-compose-prod.yml").write_text("")

        yield project


@pytest.fixture
def mock_docker_client(mocker):
    """Mock Docker client for testing."""
    return mocker.patch("docker.from_env")
