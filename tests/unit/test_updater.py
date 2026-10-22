"""Unit tests for the cached, non-blocking startup update check."""

import json
import time

import pytest

from frankenmanager.core import updater


@pytest.fixture
def app_dir(tmp_path, monkeypatch):
    """Isolate the update cache and pretend to run from a compiled binary."""
    monkeypatch.setenv("FRANKENMANAGER_DATA_DIR", str(tmp_path))
    monkeypatch.setattr(updater, "is_running_from_binary", lambda: True)
    monkeypatch.setattr(updater, "get_current_version", lambda: "1.0.0")
    return tmp_path


@pytest.fixture
def spawn(mocker):
    """Never spawn a real process (it would hit GitHub)."""
    return mocker.patch.object(updater, "_spawn_cache_refresh")


@pytest.fixture
def fetch(mocker):
    """Never call GitHub for real."""
    return mocker.patch.object(updater, "fetch_latest_release")


def _write_cache(app_dir, **data):
    (app_dir / updater.UPDATE_CACHE_FILE).write_text(json.dumps(data))


def _read_cache(app_dir):
    return json.loads((app_dir / updater.UPDATE_CACHE_FILE).read_text())


LATEST = {"version": "2.0.0", "download_url": "https://example.test/a.tar.gz", "name": "v2.0.0"}


class TestNotifyIfUpdateAvailable:
    """The startup check reads the cache and refreshes it in the background only."""

    def test_fresh_cache_is_used_without_refresh(self, app_dir, spawn, fetch, capsys):
        _write_cache(app_dir, checked_at=time.time(), latest=LATEST)

        updater.notify_if_update_available()

        spawn.assert_not_called()
        fetch.assert_not_called()
        assert "2.0.0" in capsys.readouterr().out

    def test_stale_cache_spawns_refresh_and_still_notifies(self, app_dir, spawn, fetch, capsys):
        stale = time.time() - updater.UPDATE_CACHE_TTL - 1
        _write_cache(app_dir, checked_at=stale, latest=LATEST)

        updater.notify_if_update_available()

        spawn.assert_called_once()
        fetch.assert_not_called()
        assert "2.0.0" in capsys.readouterr().out

    def test_failed_refresh_is_not_retried_before_delay(self, app_dir, spawn, fetch):
        # No successful check yet, but a refresh was just attempted
        _write_cache(app_dir, attempted_at=time.time())

        updater.notify_if_update_available()

        spawn.assert_not_called()

    def test_no_notification_when_up_to_date(self, app_dir, spawn, fetch, capsys):
        _write_cache(app_dir, checked_at=time.time(), latest={**LATEST, "version": "1.0.0"})

        updater.notify_if_update_available()

        assert capsys.readouterr().out == ""


class TestRefreshUpdateCache:
    """Only a successful GitHub lookup is cached."""

    def test_success_is_cached(self, app_dir, fetch):
        fetch.return_value = LATEST

        updater.refresh_update_cache()

        cache = _read_cache(app_dir)
        assert cache["latest"] == LATEST
        assert time.time() - cache["checked_at"] < 60

    def test_failure_is_not_cached(self, app_dir, fetch):
        fetch.side_effect = updater.UpdateCheckError("offline")

        with pytest.raises(updater.UpdateCheckError):
            updater.refresh_update_cache()

        assert not (app_dir / updater.UPDATE_CACHE_FILE).exists()
