"""Unit tests for the static resources resync run at the end of `update`.

The old binary runs `update`, so the newly installed one resyncs the static
templates (as the real user under sudo) instead of waiting for the next
start/status/restart.
"""

import os
import subprocess
import sys
from pathlib import Path

import pytest
from frankenmanager import __version__
from frankenmanager.core import resources, updater


@pytest.fixture
def installed(tmp_path, monkeypatch):
    """An initialized data directory synced with an older version, and a bundle."""
    app_dir = tmp_path / "data"
    app_dir.mkdir()
    (app_dir / ".initialized").write_text("1")
    (app_dir / resources.RESOURCES_VERSION_MARKER).write_text("0.9.0")
    (app_dir / "Dockerfile").write_text("old")

    bundle = tmp_path / "bundle"
    bundle.mkdir()
    (bundle / "Dockerfile").write_text("new")

    monkeypatch.setenv("FRANKENMANAGER_DATA_DIR", str(app_dir))
    monkeypatch.delenv("SUDO_UID", raising=False)
    monkeypatch.setattr(resources, "_is_development_mode", lambda: False)
    monkeypatch.setattr(resources, "get_bundled_resources_dir", lambda: bundle)
    return app_dir


def test_resyncs_static_files_of_an_older_version(installed):
    assert resources.sync_installed_resources() is True

    assert (installed / "Dockerfile").read_text() == "new"
    assert (installed / resources.RESOURCES_VERSION_MARKER).read_text() == __version__


def test_same_version_keeps_static_files(installed):
    (installed / resources.RESOURCES_VERSION_MARKER).write_text(__version__)

    assert resources.sync_installed_resources() is True

    assert (installed / "Dockerfile").read_text() == "old"


def test_leaves_an_uninitialized_data_dir_alone(installed):
    (installed / ".initialized").unlink()

    assert resources.sync_installed_resources() is False

    assert (installed / "Dockerfile").read_text() == "old"


@pytest.mark.skipif(sys.platform == "win32", reason="sudo is Unix only")
class TestRealUserOptions:
    """Under sudo, the new binary runs as the user owning the data directory."""

    def test_runs_as_the_sudo_user_with_their_home(self, monkeypatch):
        import pwd

        user = pwd.getpwuid(os.getuid())
        monkeypatch.setattr(updater.os, "geteuid", lambda: 0)
        env = {"SUDO_UID": str(user.pw_uid), "SUDO_GID": "20", "HOME": "/root"}

        options = updater._real_user_options(env)

        assert options == {"user": user.pw_uid, "group": 20, "extra_groups": []}
        assert env["HOME"] == user.pw_dir
        assert "SUDO_UID" not in env

    def test_nothing_to_change_outside_sudo(self):
        env = {"HOME": "/home/me"}

        assert updater._real_user_options(env) == {}
        assert env == {"HOME": "/home/me"}


class TestSyncResourcesWith:
    """`update` hands the resync over to the newly installed binary."""

    @pytest.fixture
    def executable(self, tmp_path):
        exe = tmp_path / "frankenmanager"
        exe.write_text("")
        return exe

    def test_runs_the_new_binary_internal_command(self, executable, mocker, capsys):
        run = mocker.patch.object(
            updater.subprocess, "run", return_value=subprocess.CompletedProcess([], 0)
        )

        updater._sync_resources_with(executable)

        assert run.call_args.args[0] == [
            str(executable),
            "--no-update-check",
            updater.SYNC_RESOURCES_COMMAND,
        ]
        assert "Static files" in capsys.readouterr().out

    def test_failure_falls_back_to_the_next_command(self, executable, mocker, capsys):
        mocker.patch.object(
            updater.subprocess, "run", side_effect=subprocess.TimeoutExpired("x", 60)
        )

        updater._sync_resources_with(executable)

        assert "next command" in capsys.readouterr().out

    def test_silent_when_there_is_nothing_to_sync(self, executable, mocker, capsys):
        mocker.patch.object(
            updater.subprocess,
            "run",
            return_value=subprocess.CompletedProcess([], updater.SYNC_RESOURCES_NOTHING_TO_DO),
        )

        updater._sync_resources_with(executable)

        assert capsys.readouterr().out == ""

    def test_missing_executable_is_not_run(self, mocker):
        run = mocker.patch.object(updater.subprocess, "run")

        updater._sync_resources_with(Path("/nonexistent/frankenmanager"))

        run.assert_not_called()
