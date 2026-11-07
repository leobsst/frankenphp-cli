"""Unit tests for the exec command helpers."""

from pathlib import Path, PurePosixPath, PureWindowsPath

import pytest
from frankenmanager.commands.exec_cmd import (
    build_run_command,
    get_mounts,
    resolve_php_version,
    to_container_path,
)
from frankenmanager.core.database import DatabaseManager
from frankenmanager.exceptions import ValidationError


@pytest.fixture
def db(tmp_path: Path) -> DatabaseManager:
    db = DatabaseManager(tmp_path / "db.sqlite")
    db.add_domains(["app.test"], "8.4")
    db.add_aliases(["alt.test"], "app.test")
    db.add_proxies(["proxy.test"], "127.0.0.1:8006")
    return db


class TestResolvePhpVersion:
    def test_domain(self, db):
        assert resolve_php_version(db, "app.test") == "8.4"

    def test_php_version_given_directly(self, db):
        assert resolve_php_version(db, "8.2") == "8.2"

    def test_alias_follows_target(self, db):
        assert resolve_php_version(db, "alt.test") == "8.4"

    def test_proxy_host_has_no_php_version(self, db):
        with pytest.raises(ValidationError, match="proxy host"):
            resolve_php_version(db, "proxy.test")

    def test_unknown_domain(self, db):
        with pytest.raises(ValidationError, match="not configured"):
            resolve_php_version(db, "nope.test")


class TestToContainerPath:
    def test_posix_path_kept(self):
        assert to_container_path(PurePosixPath("/home/me/app")) == "/home/me/app"

    def test_windows_path_mapped_to_drive_dir(self):
        assert to_container_path(PureWindowsPath(r"C:\Users\me\app")) == "/c/Users/me/app"


class TestGetMounts:
    def test_cwd_outside_projects_root_is_mounted_too(self, tmp_path):
        root = tmp_path / "projects"
        root.mkdir()
        cwd = tmp_path / "elsewhere"
        cwd.mkdir()

        assert get_mounts(cwd, str(root)) == [root, cwd]

    def test_cwd_inside_projects_root_not_mounted_twice(self, tmp_path):
        root = tmp_path / "projects"
        cwd = root / "app"
        cwd.mkdir(parents=True)

        assert get_mounts(cwd, str(root)) == [root]

    def test_cwd_inside_symlinked_projects_root(self, tmp_path):
        real = tmp_path / "real"
        (real / "app").mkdir(parents=True)
        link = tmp_path / "link"
        link.symlink_to(real)

        assert get_mounts((real / "app").resolve(), str(link)) == [link]

    def test_unknown_projects_root(self, tmp_path):
        assert get_mounts(tmp_path, None) == [tmp_path]


class TestBuildRunCommand:
    def _build(self, command: list[str], tty: bool = False) -> list[str]:
        return build_run_command(
            "custom-frankenphp:8.4",
            "501:20",
            Path("/work/app"),
            [Path("/work")],
            [(Path("/data/php/8.4/php.ini"), "/usr/local/etc/php/php.ini")],
            command,
            tty,
        )

    def test_runs_as_user_in_cwd(self):
        cmd = self._build(["php", "-v"])

        assert cmd[:4] == ["docker", "run", "--rm", "-i"]
        assert "-t" not in cmd
        assert cmd[cmd.index("--user") + 1] == "501:20"
        assert cmd[cmd.index("-w") + 1] == "/work/app"
        assert "/work:/work" in cmd
        assert "/data/php/8.4/php.ini:/usr/local/etc/php/php.ini:ro" in cmd
        assert cmd[-3:] == ["custom-frankenphp:8.4", "php", "-v"]

    def test_tty(self):
        assert "-t" in self._build(["php", "-a"], tty=True)

    def test_single_argument_runs_through_shell(self):
        cmd = self._build(["php -v && php -m"])

        assert cmd[-4:] == ["custom-frankenphp:8.4", "sh", "-c", "php -v && php -m"]
