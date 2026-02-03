"""Platform detection and cross-platform utilities."""

import os
import platform
from enum import Enum
from pathlib import Path


class Platform(Enum):
    """Supported platforms."""

    MACOS = "darwin"
    LINUX = "linux"
    WINDOWS = "windows"


def get_platform() -> Platform:
    """Detect the current platform.

    Returns:
        Platform enum value for the current system.

    Raises:
        RuntimeError: If the platform is not supported.
    """
    system = platform.system().lower()
    if system == "darwin":
        return Platform.MACOS
    elif system == "linux":
        return Platform.LINUX
    elif system == "windows":
        return Platform.WINDOWS
    raise RuntimeError(f"Unsupported platform: {system}")


def is_macos() -> bool:
    """Check if running on macOS."""
    return get_platform() == Platform.MACOS


def is_linux() -> bool:
    """Check if running on Linux."""
    return get_platform() == Platform.LINUX


def is_windows() -> bool:
    """Check if running on Windows."""
    return get_platform() == Platform.WINDOWS


def get_hosts_file_path() -> Path:
    """Get the path to the hosts file for the current platform.

    Returns:
        Path to the hosts file.
    """
    if get_platform() == Platform.WINDOWS:
        return Path(r"C:\Windows\System32\drivers\etc\hosts")
    return Path("/etc/hosts")


def is_admin() -> bool:
    """Check if running with administrator/root privileges.

    Returns:
        True if running with elevated privileges, False otherwise.
    """
    plat = get_platform()
    if plat == Platform.WINDOWS:
        try:
            import ctypes

            return bool(ctypes.windll.shell32.IsUserAnAdmin() != 0)  # type: ignore[attr-defined]
        except (AttributeError, OSError):
            return False
    else:
        return os.geteuid() == 0
