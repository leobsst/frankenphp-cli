"""Self-update functionality for FrankenManager binary."""

import json
import os
import platform
import shutil
import ssl
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any, Optional

import certifi
from packaging.version import Version

from .. import __version__
from ..utils.logging import log_error, log_info, log_success, log_warning
from .resources import get_app_data_dir

# GitHub repository for releases
GITHUB_REPO = "leobsst/frankenphp-cli"
GITHUB_API_URL = f"https://api.github.com/repos/{GITHUB_REPO}/releases/latest"

# Name of the bundle directory inside release archives, and of the
# executable inside that directory.
BUNDLE_NAME = "frankenmanager"

# Startup update check: a successful GitHub lookup is cached for
# UPDATE_CACHE_TTL. While the cache is stale, a failed lookup (offline,
# rate-limited...) is retried at most once per UPDATE_RETRY_DELAY.
UPDATE_CACHE_FILE = "update_check.json"
UPDATE_CACHE_TTL = 6 * 60 * 60
UPDATE_RETRY_DELAY = 10 * 60

# Hidden CLI command run in a detached process to refresh the cache.
REFRESH_COMMAND = "_refresh-update-cache"


class UpdateCheckError(Exception):
    """Raised when release information cannot be fetched or parsed."""


def _ssl_context() -> ssl.SSLContext:
    """Build an SSL context backed by certifi's CA bundle.

    A frozen PyInstaller binary doesn't reliably inherit the host's
    system CA trust store, so relying on ssl's default lookup can
    fail with an opaque certificate error on machines other than the
    one it was built on.
    """
    return ssl.create_default_context(cafile=certifi.where())


def get_platform_binary_name() -> str:
    """Get the binary name for the current platform.

    Returns:
        The binary filename for the current OS and architecture.
    """
    system = platform.system().lower()
    machine = platform.machine().lower()

    if system == "darwin":
        if machine == "arm64":
            return "frankenmanager-macos-arm64"
        return "frankenmanager-macos-x86_64"
    elif system == "linux":
        if machine == "aarch64":
            return "frankenmanager-linux-arm64"
        return "frankenmanager-linux-x86_64"
    elif system == "windows":
        return "frankenmanager-windows-x86_64.exe"
    else:
        raise RuntimeError(f"Unsupported platform: {system}-{machine}")


def get_platform_archive_name() -> str:
    """Get the release archive name for the current platform.

    The archive holds the fast-starting onedir bundle (executable +
    _internal/). The bare binary asset (see get_platform_binary_name) is
    only still published so that older single-file installs can update.

    Returns:
        The archive filename for the current OS and architecture.
    """
    return f"{get_platform_binary_name().removesuffix('.exe')}.tar.gz"


def get_current_version() -> str:
    """Get the current installed version.

    Returns:
        The current version string.
    """
    return str(__version__)


def fetch_latest_release() -> dict[str, str]:
    """Fetch the latest release information from GitHub.

    Returns:
        Dictionary with 'version' and 'download_url' keys.

    Raises:
        UpdateCheckError: If the release information can't be fetched,
            parsed, or doesn't include an asset for this platform.
    """
    try:
        req = urllib.request.Request(
            GITHUB_API_URL,
            headers={"Accept": "application/vnd.github.v3+json", "User-Agent": "FrankenManager"},
        )
        with urllib.request.urlopen(req, timeout=10, context=_ssl_context()) as response:
            data = json.loads(response.read().decode())
    except urllib.error.URLError as e:
        raise UpdateCheckError(f"Could not reach GitHub ({e.reason}).") from e
    except json.JSONDecodeError as e:
        raise UpdateCheckError(f"GitHub returned an unreadable response ({e}).") from e

    try:
        version = data["tag_name"].lstrip("v")
    except KeyError as e:
        raise UpdateCheckError(f"Unexpected release format from GitHub (missing {e}).") from e

    archive_name = get_platform_archive_name()

    # Find the download URL for our platform
    download_url = None
    for asset in data.get("assets", []):
        if asset["name"] == archive_name:
            download_url = asset["browser_download_url"]
            break

    if not download_url:
        raise UpdateCheckError(
            f"The latest release ({version}) has no build for this platform ({archive_name})."
        )

    return {"version": version, "download_url": download_url, "name": data["name"]}


def _is_dev_version(version: str) -> bool:
    """Check whether a version string is an unreleased/dev build.

    Returns:
        True if the version looks like a dev build (e.g. "0.0.0.dev" or a
        setuptools_scm "X.Y.Z.devN" version between tags).
    """
    return version == "0.0.0.dev" or "dev" in version


def _release_tuple(version: str) -> tuple[int, ...]:
    """Get the major.minor.micro release segment of a version string.

    Pre-release/dev suffixes (e.g. the ".devN" in "1.2.4.dev5") are
    ignored so that a dev build is only ever considered newer or older
    based on the release it's building towards, never on its dev number.
    """
    return Version(version).release[:3]


def _is_newer_than_current(latest: dict[str, str]) -> bool:
    """Check whether a release is newer than the running version."""
    current = get_current_version()

    # Outside of a compiled binary, __version__ can't be trusted for
    # comparison (e.g. the "0.0.0.dev" fallback for editable installs),
    # so just surface the latest release instead of comparing versions.
    if not is_running_from_binary() and _is_dev_version(current):
        return True

    try:
        return _release_tuple(latest["version"]) > _release_tuple(current)
    except Exception:
        # Version parsing failed, skip update check
        return False


def _cache_path() -> Path:
    return get_app_data_dir() / UPDATE_CACHE_FILE


def _read_cache() -> dict[str, Any]:
    try:
        data = json.loads(_cache_path().read_text())
    except (OSError, ValueError):
        return {}
    return data if isinstance(data, dict) else {}


def _write_cache(data: dict[str, Any]) -> None:
    """Write the cache atomically, so a concurrent reader never sees half a file."""
    path = _cache_path()
    tmp_path = path.with_name(f"{path.name}.{os.getpid()}.tmp")
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        tmp_path.write_text(json.dumps(data))
        os.replace(tmp_path, path)
    except OSError:
        tmp_path.unlink(missing_ok=True)


def refresh_update_cache() -> dict[str, str]:
    """Fetch the latest release from GitHub and cache it on success.

    Returns:
        The latest release information.

    Raises:
        UpdateCheckError: If release information can't be fetched.
    """
    latest = fetch_latest_release()
    cache = _read_cache()
    cache.update({"checked_at": time.time(), "latest": latest})
    _write_cache(cache)
    return latest


def check_for_updates() -> Optional[dict[str, str]]:
    """Check if a new version is available.

    Returns:
        Dictionary with update info if available, None otherwise.

    Raises:
        UpdateCheckError: If release information can't be fetched.
    """
    latest = refresh_update_cache()
    return latest if _is_newer_than_current(latest) else None


def is_running_from_binary() -> bool:
    """Check if running from a PyInstaller binary.

    Returns:
        True if running from a frozen binary.
    """
    return getattr(sys, "frozen", False)


def _is_onedir_bundle() -> bool:
    """Check if running from an onedir bundle rather than a single-file binary.

    A onedir bundle keeps its _internal/ directory next to the executable,
    whereas a single-file binary unpacks itself to a temporary directory.
    """
    bundle_dir = getattr(sys, "_MEIPASS", None)
    if bundle_dir is None:
        return False
    return Path(bundle_dir).resolve().parent == Path(sys.executable).resolve().parent


def get_executable_path() -> Path:
    """Get the path to the current executable.

    Returns:
        Path to the current executable file.
    """
    if is_running_from_binary():
        return Path(sys.executable)
    else:
        # Running from Python, return the script path
        return Path(sys.argv[0]).resolve()


def download_asset(url: str, dest: Path) -> bool:
    """Download a release asset from URL to destination.

    Args:
        url: The download URL.
        dest: Destination path for the asset.

    Returns:
        True if download succeeded.
    """
    try:
        log_info(f"Downloading from {url}...")

        req = urllib.request.Request(url, headers={"User-Agent": "FrankenManager"})
        with urllib.request.urlopen(req, timeout=60, context=_ssl_context()) as response:
            with open(dest, "wb") as f:
                shutil.copyfileobj(response, f)

        return True

    except urllib.error.URLError as e:
        log_error(f"Download failed: {e}")
        return False


def _extract_bundle(archive: Path, dest_dir: Path) -> Path:
    """Extract a release archive and return the bundle directory it contains."""
    with tarfile.open(archive, "r:gz") as tar:
        if hasattr(tarfile, "data_filter"):
            # Rejects absolute paths and links escaping dest_dir
            tar.extractall(dest_dir, filter="data")
        else:
            tar.extractall(dest_dir)  # noqa: S202

    bundle = dest_dir / BUNDLE_NAME
    if not (bundle / BUNDLE_NAME).is_file():
        raise OSError(f"Release archive does not contain {BUNDLE_NAME}/{BUNDLE_NAME}")
    return bundle


def _install_bundle(new_bundle: Path, install_dir: Path) -> None:
    """Swap the installed bundle directory with a freshly extracted one.

    The previous bundle is moved aside first and put back if the swap
    fails, so a failed update never leaves a half-installed bundle.
    """
    staging_dir = install_dir.with_name(f"{install_dir.name}.new")
    backup_dir = install_dir.with_name(f"{install_dir.name}.old")
    shutil.rmtree(staging_dir, ignore_errors=True)
    shutil.rmtree(backup_dir, ignore_errors=True)

    install_dir.parent.mkdir(parents=True, exist_ok=True)
    # Copy next to the install dir first so the final swap is a same-filesystem rename
    shutil.move(str(new_bundle), str(staging_dir))

    if install_dir.exists():
        os.rename(install_dir, backup_dir)
    try:
        os.rename(staging_dir, install_dir)
    except OSError:
        if backup_dir.exists():
            os.rename(backup_dir, install_dir)
        raise

    shutil.rmtree(backup_dir, ignore_errors=True)


def update_binary(force: bool = False) -> bool:
    """Update the binary to the latest version.

    The update installs the onedir bundle shipped in the release archive.
    A legacy single-file install (e.g. /usr/local/bin/frankenmanager) is
    migrated: the bundle goes to <prefix>/lib/frankenmanager and the old
    binary is replaced by a symlink to the bundle's executable.

    Args:
        force: If True, update even if already on latest version.

    Returns:
        True if update succeeded or no update needed.
    """
    if not is_running_from_binary():
        log_warning("Self-update is only available for pre-compiled binaries.")
        log_info("Download the latest binary from GitHub releases.")
        return False

    log_info("Checking for updates...")
    try:
        latest = refresh_update_cache()
    except UpdateCheckError as e:
        log_error(str(e))
        return False

    current = get_current_version()
    log_info(f"Current version: {current}")
    log_info(f"Latest version:  {latest['version']}")

    # Check if update is needed. We're always running as a compiled
    # binary here (checked above), so even a dev-tagged current version
    # (e.g. "1.2.4.dev5" built ahead of the "1.2.4" tag) is compared on
    # its release number rather than always forcing an update.
    needs_update = force
    if not force:
        try:
            needs_update = _release_tuple(latest["version"]) > _release_tuple(current)
        except Exception:
            needs_update = False

    if not needs_update:
        log_success("Already on the latest version!")
        return True

    log_info(f"Updating to version {latest['version']}...")

    exe_path = get_executable_path()
    onedir = _is_onedir_bundle()
    if onedir:
        install_dir = exe_path.resolve().parent
    else:
        install_dir = exe_path.parent.parent / "lib" / BUNDLE_NAME

    with tempfile.TemporaryDirectory() as tmp:
        archive_path = Path(tmp) / get_platform_archive_name()
        if not download_asset(latest["download_url"], archive_path):
            return False

        try:
            new_bundle = _extract_bundle(archive_path, Path(tmp) / "extracted")
            _install_bundle(new_bundle, install_dir)

            if not onedir:
                # Replace the legacy single-file binary with a symlink to the bundle
                tmp_link = exe_path.with_name(f".{exe_path.name}.link")
                tmp_link.unlink(missing_ok=True)
                tmp_link.symlink_to(install_dir / BUNDLE_NAME)
                os.replace(tmp_link, exe_path)
        except PermissionError:
            log_error("Permission denied. Try running with sudo/administrator privileges.")
            return False
        except (OSError, tarfile.TarError) as e:
            log_error(f"Update failed: {e}")
            return False

    log_success(f"Successfully updated to version {latest['version']}!")
    log_info("Please restart FrankenManager to use the new version.")
    return True


def _spawn_cache_refresh() -> None:
    """Refresh the update cache in a detached background process.

    The process outlives the current command, so the user never waits on
    GitHub; its result is picked up by the next command.
    """
    subprocess.Popen(
        [sys.executable, "--no-update-check", REFRESH_COMMAND],
        stdin=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )


def notify_if_update_available() -> None:
    """Print a notification if a newer version is known to be available.

    Never touches the network itself: it reads the cached result of the
    last GitHub lookup, and refreshes a stale cache in a detached process
    (see _spawn_cache_refresh). A newly published release is therefore
    announced from the following command on.
    """
    if not is_running_from_binary():
        return

    try:
        cache = _read_cache()
        now = time.time()
        if (
            now - cache.get("checked_at", 0) >= UPDATE_CACHE_TTL
            and now - cache.get("attempted_at", 0) >= UPDATE_RETRY_DELAY
        ):
            cache["attempted_at"] = now
            _write_cache(cache)
            _spawn_cache_refresh()

        latest = cache.get("latest")
        if latest and _is_newer_than_current(latest):
            log_warning(
                f"A new version is available: {latest['version']} "
                f"(current: {get_current_version()})"
            )
            log_info("Run 'sudo frankenmanager update' to update.")
    except Exception:
        # Silently ignore errors during startup check
        pass
