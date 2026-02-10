"""Tests for reset command."""

from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest
import typer

from frankenmanager.commands.reset import reset_data
from frankenmanager.exceptions import ServerStateError


def test_reset_requires_at_least_one_option(tmp_path: Path) -> None:
    """Test that reset requires at least one option."""
    with (
        patch("frankenmanager.commands.reset.get_project_dir", return_value=tmp_path),
        pytest.raises(typer.Exit) as exc_info,
    ):
        reset_data(reset_db=False, reset_caddyfiles=False)

    assert exc_info.value.exit_code == 1


def test_reset_fails_when_server_running(tmp_path: Path) -> None:
    """Test that reset fails when server is running."""
    env_file = tmp_path / ".env"
    env_file.write_text("DEFAULT_PROJECT_PATH=/test\n")

    db_file = tmp_path / "db.sqlite"
    db_file.touch()

    with (
        patch("frankenmanager.commands.reset.get_project_dir", return_value=tmp_path),
        patch("frankenmanager.commands.reset.DatabaseManager") as mock_db,
        patch("frankenmanager.commands.reset.DockerManager"),
    ):
        # Mock server as running
        mock_db.return_value.is_running = True

        with pytest.raises(ServerStateError) as exc_info:
            reset_data(reset_db=True, reset_caddyfiles=False)

        assert "server is currently running" in str(exc_info.value).lower()


def test_reset_db_with_confirmation(tmp_path: Path) -> None:
    """Test resetting FrankenManager configuration with user confirmation."""
    env_file = tmp_path / ".env"
    env_file.write_text("DEFAULT_PROJECT_PATH=/test\n")

    db_file = tmp_path / "db.sqlite"
    db_file.touch()

    with (
        patch("frankenmanager.commands.reset.get_project_dir", return_value=tmp_path),
        patch("frankenmanager.commands.reset.DatabaseManager") as mock_db_class,
        patch("frankenmanager.commands.reset.DockerManager"),
        patch("frankenmanager.commands.reset.typer.confirm", return_value=True),
        patch("frankenmanager.commands.reset.console") as mock_console,
    ):
        mock_db = MagicMock()
        mock_db.is_running = False
        mock_db_class.return_value = mock_db

        reset_data(reset_db=True, reset_caddyfiles=False)

        # Verify confirmation was shown
        assert mock_console.print.called
        # Verify database reset was called
        mock_db.reset.assert_called_once()


def test_reset_caddyfiles_with_confirmation(tmp_path: Path) -> None:
    """Test resetting Caddyfiles with user confirmation."""
    env_file = tmp_path / ".env"
    env_file.write_text("DEFAULT_PROJECT_PATH=/test\n")

    # Create custom Caddyfiles
    custom_dir = tmp_path / "caddy" / "sites" / "custom"
    custom_dir.mkdir(parents=True)
    (custom_dir / "test_Caddyfile").write_text("test content")
    (custom_dir / "example_Caddyfile").write_text("example content")

    with (
        patch("frankenmanager.commands.reset.get_project_dir", return_value=tmp_path),
        patch("frankenmanager.commands.reset.DatabaseManager") as mock_db_class,
        patch("frankenmanager.commands.reset.DockerManager"),
        patch("frankenmanager.commands.reset.typer.confirm", return_value=True),
        patch("frankenmanager.commands.reset.console") as mock_console,
    ):
        mock_db = MagicMock()
        mock_db.is_running = False
        mock_db_class.return_value = mock_db

        reset_data(reset_db=False, reset_caddyfiles=True)

        # Verify confirmation was shown
        assert mock_console.print.called
        # Verify Caddyfiles were deleted
        assert not (custom_dir / "test_Caddyfile").exists()
        assert not (custom_dir / "example_Caddyfile").exists()


def test_reset_both_with_confirmation(tmp_path: Path) -> None:
    """Test resetting both FrankenManager configuration and Caddyfiles."""
    env_file = tmp_path / ".env"
    env_file.write_text("DEFAULT_PROJECT_PATH=/test\n")

    db_file = tmp_path / "db.sqlite"
    db_file.touch()

    # Create custom Caddyfiles
    custom_dir = tmp_path / "caddy" / "sites" / "custom"
    custom_dir.mkdir(parents=True)
    (custom_dir / "test_Caddyfile").write_text("test content")

    with (
        patch("frankenmanager.commands.reset.get_project_dir", return_value=tmp_path),
        patch("frankenmanager.commands.reset.DatabaseManager") as mock_db_class,
        patch("frankenmanager.commands.reset.DockerManager"),
        patch("frankenmanager.commands.reset.typer.confirm", return_value=True),
        patch("frankenmanager.commands.reset.console") as mock_console,
    ):
        mock_db = MagicMock()
        mock_db.is_running = False
        mock_db_class.return_value = mock_db

        reset_data(reset_db=True, reset_caddyfiles=True)

        # Verify confirmation was shown
        assert mock_console.print.called
        # Verify database reset was called
        mock_db.reset.assert_called_once()
        # Verify Caddyfiles were deleted
        assert not (custom_dir / "test_Caddyfile").exists()


def test_reset_cancelled_by_user(tmp_path: Path) -> None:
    """Test that reset is cancelled when user declines confirmation."""
    env_file = tmp_path / ".env"
    env_file.write_text("DEFAULT_PROJECT_PATH=/test\n")

    db_file = tmp_path / "db.sqlite"
    db_file.touch()

    with (
        patch("frankenmanager.commands.reset.get_project_dir", return_value=tmp_path),
        patch("frankenmanager.commands.reset.DatabaseManager") as mock_db_class,
        patch("frankenmanager.commands.reset.DockerManager"),
        patch("frankenmanager.commands.reset.typer.confirm", return_value=False),
        pytest.raises(typer.Exit) as exc_info,
    ):
        mock_db = MagicMock()
        mock_db.is_running = False
        mock_db_class.return_value = mock_db

        reset_data(reset_db=True, reset_caddyfiles=False)

    assert exc_info.value.exit_code == 0


def test_reset_caddyfiles_empty_directory(tmp_path: Path) -> None:
    """Test resetting Caddyfiles when directory is empty."""
    env_file = tmp_path / ".env"
    env_file.write_text("DEFAULT_PROJECT_PATH=/test\n")

    # Create empty custom directory
    custom_dir = tmp_path / "caddy" / "sites" / "custom"
    custom_dir.mkdir(parents=True)

    with (
        patch("frankenmanager.commands.reset.get_project_dir", return_value=tmp_path),
        patch("frankenmanager.commands.reset.DatabaseManager") as mock_db_class,
        patch("frankenmanager.commands.reset.DockerManager"),
        patch("frankenmanager.commands.reset.typer.confirm", return_value=True),
        patch("frankenmanager.commands.reset.console"),
    ):
        mock_db = MagicMock()
        mock_db.is_running = False
        mock_db_class.return_value = mock_db

        # Should not raise an error
        reset_data(reset_db=False, reset_caddyfiles=True)


def test_reset_caddyfiles_directory_not_exists(tmp_path: Path) -> None:
    """Test resetting Caddyfiles when directory doesn't exist."""
    env_file = tmp_path / ".env"
    env_file.write_text("DEFAULT_PROJECT_PATH=/test\n")

    with (
        patch("frankenmanager.commands.reset.get_project_dir", return_value=tmp_path),
        patch("frankenmanager.commands.reset.DatabaseManager") as mock_db_class,
        patch("frankenmanager.commands.reset.DockerManager"),
        patch("frankenmanager.commands.reset.typer.confirm", return_value=True),
        patch("frankenmanager.commands.reset.console"),
    ):
        mock_db = MagicMock()
        mock_db.is_running = False
        mock_db_class.return_value = mock_db

        # Should not raise an error
        reset_data(reset_db=False, reset_caddyfiles=True)
