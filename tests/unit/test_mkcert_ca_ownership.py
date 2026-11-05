"""Unit tests for mkcert CAROOT ownership after `sudo frankenmanager setup --install-mkcert`.

Regression: the setup wrote rootCA-key.pem (0600) as root into the user's
CAROOT, so the next non-sudo `frankenmanager start` failed with
"failed to read the CA key: permission denied".
"""

import pytest
from frankenmanager.core import privilege_manager, resources
from frankenmanager.core.privilege_manager import MkcertInstaller


@pytest.fixture
def ca_root(tmp_path):
    root = tmp_path / "mkcert"
    root.mkdir()
    (root / "rootCA.pem").write_text("cert")
    (root / "rootCA-key.pem").write_text("key")
    return root


@pytest.fixture
def installer(mocker, ca_root):
    """MkcertInstaller that never runs mkcert or touches the trust store."""
    mocker.patch.object(privilege_manager.shutil, "which", return_value="/usr/bin/mkcert")
    mocker.patch.object(MkcertInstaller, "_get_ca_root", return_value=ca_root)
    mocker.patch.object(MkcertInstaller, "_is_frankenmanager_ca", return_value=True)
    return MkcertInstaller()


def test_setup_ca_under_sudo_hands_existing_ca_back_to_user(
    installer, ca_root, monkeypatch, mocker
):
    """Re-running setup also repairs a CA left root-owned by an older version."""
    monkeypatch.setenv("SUDO_UID", "501")
    monkeypatch.setenv("SUDO_GID", "20")
    chown = mocker.patch.object(resources.os, "chown")
    mocker.patch.object(MkcertInstaller, "_run_mkcert_install", return_value=True)

    assert installer.setup_ca() is True

    chowned = {call.args[0] for call in chown.call_args_list}
    assert chowned == {ca_root, ca_root / "rootCA.pem", ca_root / "rootCA-key.pem"}


def test_setup_ca_hands_ca_back_even_if_mkcert_install_fails(
    installer, ca_root, monkeypatch, mocker
):
    monkeypatch.setenv("SUDO_UID", "501")
    chown = mocker.patch.object(resources.os, "chown")
    mocker.patch.object(MkcertInstaller, "_run_mkcert_install", return_value=False)

    assert installer.setup_ca() is False

    chown.assert_any_call(ca_root / "rootCA-key.pem", 501, -1)
