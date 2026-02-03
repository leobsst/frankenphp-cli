"""Self-update functionality for FrankenManager binary."""

import json
import os
import platform
import shutil
import stat
import sys
import tempfile
import urllib.error
import urllib.request
from pathlib import Path
from typing import Optional

from packaging.version import Version

from .. import __version__
from ..utils.logging import log_error, log_info, log_success, log_warning

# GitHub repository for releases
GITHUB_REPO = "leobsst/frankenphp-cli"
GITHUB_API_URL = f"https://api.github.com/repos/{GITHUB_REPO}/releases/latest"


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


def get_current_version() -> str:
    """Get the current installed version.

    Returns:
        The current version string.
    """
    return str(__version__)


def fetch_latest_release() -> Optional[dict[str, str]]:
    """Fetch the latest release information from GitHub.

    Returns:
        Dictionary with 'version' and 'download_url' keys, or None on error.
    """
    try:
        req = urllib.request.Request(
            GITHUB_API_URL,
            headers={"Accept": "application/vnd.github.v3+json", "User-Agent": "FrankenManager"},
        )
        with urllib.request.urlopen(req, timeout=10) as response:
            data = json.loads(response.read().decode())

        version = data["tag_name"].lstrip("v")
        binary_name = get_platform_binary_name()

        # Find the download URL for our platform
        download_url = None
        for asset in data.get("assets", []):
            if asset["name"] == binary_name:
                download_url = asset["browser_download_url"]
                break

        if not download_url:
            return None

        return {"version": version, "download_url": download_url, "name": data["name"]}

    except (urllib.error.URLError, json.JSONDecodeError, KeyError):
        return None


def check_for_updates() -> Optional[dict[str, str]]:
    """Check if a new version is available.

    Returns:
        Dictionary with update info if available, None otherwise.
    """
    latest = fetch_latest_release()
    if not latest:
        return None

    current = get_current_version()

    # Handle development versions
    if current == "0.0.0.dev" or "dev" in current:
        return latest

    try:
        if Version(latest["version"]) > Version(current):
            return latest
    except Exception:
        # Version parsing failed, skip update check
        pass

    return None


def is_running_from_binary() -> bool:
    """Check if running from a PyInstaller binary.

    Returns:
        True if running from a frozen binary.
    """
    return getattr(sys, "frozen", False)


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


def download_binary(url: str, dest: Path) -> bool:
    """Download a binary from URL to destination.

    Args:
        url: The download URL.
        dest: Destination path for the binary.

    Returns:
        True if download succeeded.
    """
    try:
        log_info(f"Downloading from {url}...")

        req = urllib.request.Request(url, headers={"User-Agent": "FrankenManager"})
        with urllib.request.urlopen(req, timeout=60) as response:
            with open(dest, "wb") as f:
                shutil.copyfileobj(response, f)

        return True

    except urllib.error.URLError as e:
        log_error(f"Download failed: {e}")
        return False


def update_binary(force: bool = False) -> bool:
    """Update the binary to the latest version.

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
    latest = fetch_latest_release()

    if not latest:
        log_error("Could not fetch release information from GitHub.")
        return False

    current = get_current_version()
    log_info(f"Current version: {current}")
    log_info(f"Latest version:  {latest['version']}")

    # Check if update is needed
    needs_update = force
    if not force:
        if current == "0.0.0.dev" or "dev" in current:
            needs_update = True
        else:
            try:
                needs_update = Version(latest["version"]) > Version(current)
            except Exception:
                needs_update = False

    if not needs_update:
        log_success("Already on the latest version!")
        return True

    log_info(f"Updating to version {latest['version']}...")

    # Get current executable path
    exe_path = get_executable_path()

    # Create temp file for download
    with tempfile.NamedTemporaryFile(delete=False, suffix=exe_path.suffix) as tmp:
        tmp_path = Path(tmp.name)

    try:
        # Download new binary
        if not download_binary(latest["download_url"], tmp_path):
            tmp_path.unlink(missing_ok=True)
            return False

        # Make executable on Unix
        if platform.system() != "Windows":
            tmp_path.chmod(tmp_path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

        # Replace current binary
        backup_path = exe_path.with_suffix(exe_path.suffix + ".backup")

        try:
            # Backup current binary
            shutil.copy2(exe_path, backup_path)

            # Replace with new binary
            if platform.system() == "Windows":
                # On Windows, rename current exe first
                old_path = exe_path.with_suffix(exe_path.suffix + ".old")
                os.rename(exe_path, old_path)
                shutil.move(str(tmp_path), str(exe_path))
                old_path.unlink(missing_ok=True)
            else:
                shutil.move(str(tmp_path), str(exe_path))

            # Remove backup on success
            backup_path.unlink(missing_ok=True)

            log_success(f"Successfully updated to version {latest['version']}!")
            log_info("Please restart FrankenManager to use the new version.")
            return True

        except PermissionError:
            log_error("Permission denied. Try running with sudo/administrator privileges.")
            # Restore backup if it exists
            if backup_path.exists():
                shutil.move(str(backup_path), str(exe_path))
            return False

    finally:
        # Clean up temp file if it still exists
        tmp_path.unlink(missing_ok=True)


def notify_if_update_available() -> None:
    """Check for updates and print a notification if available.

    This is meant to be called at startup and should be non-blocking.
    """
    if not is_running_from_binary():
        return

    try:
        update_info = check_for_updates()
        if update_info:
            log_warning(
                f"A new version is available: {update_info['version']} "
                f"(current: {get_current_version()})"
            )
            log_info("Run 'frankenmanager update' to update.")
    except Exception:
        # Silently ignore errors during startup check
        pass
