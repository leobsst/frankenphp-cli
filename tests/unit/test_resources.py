"""Unit tests for data directory ownership when FrankenManager runs under sudo.

Regression: `sudo frankenmanager setup` wrote the update cache first, creating
~/.frankenmanager as root, so the next non-sudo `frankenmanager start` crashed
with PermissionError while creating caddy/sites/custom.
"""

import os

import pytest
from frankenmanager.core import resources, updater
from frankenmanager.exceptions import ConfigurationError


@pytest.fixture
def sudo(monkeypatch, mocker):
    """Pretend to run under sudo and record chown calls instead of applying them."""
    monkeypatch.setenv("SUDO_UID", "501")
    monkeypatch.setenv("SUDO_GID", "20")
    return mocker.patch.object(resources.os, "chown")


def _chowned(chown):
    return {call.args[0] for call in chown.call_args_list}


def test_update_cache_under_sudo_hands_new_data_dir_back_to_user(tmp_path, monkeypatch, sudo):
    app_dir = tmp_path / ".frankenmanager"
    monkeypatch.setenv("FRANKENMANAGER_DATA_DIR", str(app_dir))

    updater._write_cache({"attempted_at": 1})

    assert app_dir in _chowned(sudo)
    assert app_dir / updater.UPDATE_CACHE_FILE in _chowned(sudo)
    sudo.assert_any_call(app_dir, 501, 20)


def test_make_user_dir_only_chowns_directories_it_creates(tmp_path, sudo):
    existing = tmp_path / "database"
    existing.mkdir()

    resources.make_user_dir(existing / "a" / "b")

    assert _chowned(sudo) == {existing / "a", existing / "a" / "b"}


def test_make_user_dir_does_not_chown_outside_sudo(tmp_path, monkeypatch, mocker):
    monkeypatch.delenv("SUDO_UID", raising=False)
    chown = mocker.patch.object(resources.os, "chown")

    resources.make_user_dir(tmp_path / "x")

    chown.assert_not_called()


@pytest.mark.skipif(not hasattr(os, "getuid") or os.getuid() == 0, reason="needs non-root Unix")
def test_unwritable_data_dir_raises_actionable_error(tmp_path, monkeypatch):
    monkeypatch.delenv("SUDO_UID", raising=False)
    app_dir = tmp_path / ".frankenmanager"
    app_dir.mkdir()
    app_dir.chmod(0o500)  # simulate a root-owned directory left by a sudo run
    try:
        with pytest.raises(ConfigurationError, match="sudo chown -R"):
            resources._ensure_directory_structure(app_dir)
    finally:
        app_dir.chmod(0o700)
