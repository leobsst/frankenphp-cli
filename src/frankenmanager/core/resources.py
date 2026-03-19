"""Resource management for bundled configuration files.

This module handles extraction and management of bundled configuration files
that are embedded in the PyInstaller binary or installed with the package.

All generated files and data are stored in ~/.frankenmanager/ (or equivalent
on Windows), ensuring the binary can run from any directory.
"""

import os
import shutil
import sys
from pathlib import Path
from typing import Optional

from ..utils.logging import log_info, log_warning
from .php_versions import SUPPORTED_VERSIONS


def get_app_data_dir() -> Path:
    """Get the application data directory.

    All FrankenManager data (configs, certificates, database, logs) is stored here.

    Returns:
        Path to ~/.frankenmanager/ on Unix or %LOCALAPPDATA%/frankenmanager/ on Windows.
    """
    # Allow override via environment variable
    if env_dir := os.environ.get("FRANKENMANAGER_DATA_DIR"):
        return Path(env_dir)

    if sys.platform == "win32":
        base = Path.home() / "AppData" / "Local"
        return base / "frankenmanager"
    else:
        return Path.home() / ".frankenmanager"


def get_bundled_resources_dir() -> Optional[Path]:
    """Get the directory containing bundled resources.

    For PyInstaller builds, this is the _MEIPASS directory.
    For pip installs, this is the package's resources directory.
    For development, this is the project root.

    Returns:
        Path to the resources directory, or None if not found.
    """
    # Check if running from PyInstaller bundle
    if getattr(sys, "frozen", False):
        bundle_dir = Path(sys._MEIPASS)  # type: ignore[attr-defined]
        resources_dir = bundle_dir / "resources"
        if resources_dir.exists():
            return resources_dir

    # Running from source or pip install - look for resources in package
    package_dir = Path(__file__).parent.parent
    resources_dir = package_dir / "resources"
    if resources_dir.exists():
        return resources_dir

    # Fall back to project root (development)
    project_root = package_dir.parent.parent
    if (project_root / ".env.example").exists():
        return project_root

    return None


def ensure_resources_extracted() -> Path:
    """Ensure all required resources are extracted to the app data directory.

    This function:
    1. Creates the app data directory if it doesn't exist
    2. Copies bundled resources if they haven't been extracted yet
    3. Creates all necessary subdirectories for generated files
    4. Returns the path to the app data directory

    Returns:
        Path to the app data directory containing all resources.
    """
    app_dir = get_app_data_dir()

    # Always ensure the directory structure exists
    _ensure_directory_structure(app_dir)

    # Check if already initialized (resources copied)
    marker_file = app_dir / ".initialized"
    if marker_file.exists():
        return app_dir

    # Get bundled resources
    resources_dir = get_bundled_resources_dir()
    if resources_dir is None:
        log_warning("No bundled resources found. Using app data directory as-is.")
        marker_file.write_text("1")
        return app_dir

    log_info(f"Initializing FrankenManager data directory: {app_dir}")

    # Clean up any files that were incorrectly created as directories by Docker
    # Docker creates empty directories when mounting a non-existent file
    _cleanup_incorrect_directories(app_dir)

    # List of files/directories to copy from bundled resources
    items_to_copy = [
        ".env.example",
        "docker-compose.yml",
        "docker-compose-prod.yml",
        "Dockerfile",
        "caddy",
        "php",
    ]

    for item in items_to_copy:
        src = resources_dir / item
        dst = app_dir / item

        if not src.exists():
            continue

        if src.is_file():
            if not dst.exists():
                shutil.copy2(src, dst)
        elif src.is_dir():
            # Use dirs_exist_ok=True to merge contents if directory already exists
            # This ensures files like Caddyfile are copied even if _ensure_directory_structure
            # already created parent directories
            shutil.copytree(src, dst, dirs_exist_ok=True)

    # Ensure directory structure again after copying (in case caddy wasn't in resources)
    _ensure_directory_structure(app_dir)

    # Distribute php.ini files to versioned directories
    _distribute_php_configs(app_dir)

    # Create marker file
    marker_file.write_text("1")

    log_info("FrankenManager initialized successfully.")
    return app_dir


def _ensure_directory_structure(app_dir: Path) -> None:
    """Create all necessary subdirectories for FrankenManager operation.

    Args:
        app_dir: The application data directory.
    """
    # Create app directory if it doesn't exist
    app_dir.mkdir(parents=True, exist_ok=True)

    # Caddy directories
    (app_dir / "caddy" / "sites" / "custom").mkdir(parents=True, exist_ok=True)
    (app_dir / "caddy" / "sites" / "default").mkdir(parents=True, exist_ok=True)
    (app_dir / "caddy" / "sites" / "archive").mkdir(parents=True, exist_ok=True)

    # Per-version site directories
    for version in SUPPORTED_VERSIONS:
        (app_dir / "caddy" / "sites" / f"php-{version}").mkdir(parents=True, exist_ok=True)
    (app_dir / "caddy" / "certs").mkdir(parents=True, exist_ok=True)
    (app_dir / "caddy" / "log" / "caddy").mkdir(parents=True, exist_ok=True)
    (app_dir / "caddy" / "log" / "php").mkdir(parents=True, exist_ok=True)
    (app_dir / "caddy" / "data").mkdir(parents=True, exist_ok=True)
    (app_dir / "caddy" / "config").mkdir(parents=True, exist_ok=True)

    # PHP config directories (one per supported version)
    for version in SUPPORTED_VERSIONS:
        (app_dir / "php" / version).mkdir(parents=True, exist_ok=True)

    # Database directory (MariaDB data)
    (app_dir / "database").mkdir(parents=True, exist_ok=True)


def _cleanup_incorrect_directories(app_dir: Path) -> None:
    """Remove directories that should be files.

    Docker creates empty directories when mounting a non-existent file path.
    This causes issues when we later try to copy the actual files.

    Args:
        app_dir: The application data directory.
    """
    # List of paths that should be files, not directories
    should_be_files = [
        app_dir / "caddy" / "Caddyfile",
        app_dir / "caddy" / "Caddyfile.template",
        app_dir / "php" / "php.ini",
        app_dir / "php" / "php-prod.ini",
    ]

    # Also check versioned php directories
    for version in SUPPORTED_VERSIONS:
        should_be_files.append(app_dir / "php" / version / "php.ini")
        should_be_files.append(app_dir / "php" / version / "php-prod.ini")

    for path in should_be_files:
        if path.exists() and path.is_dir():
            # This is a directory but should be a file - remove it
            shutil.rmtree(path)
            log_warning(f"Removed incorrectly created directory: {path}")


def _distribute_php_configs(app_dir: Path) -> None:
    """Copy base php.ini and php-prod.ini into each versioned php directory.

    If php/php.ini exists at the root level (legacy layout), it is used as the source.
    Otherwise, the files from the bundled resources are used.

    Args:
        app_dir: The application data directory.
    """
    base_php_ini = app_dir / "php" / "php.ini"
    base_php_prod_ini = app_dir / "php" / "php-prod.ini"

    for version in SUPPORTED_VERSIONS:
        version_dir = app_dir / "php" / version
        version_dir.mkdir(parents=True, exist_ok=True)

        target_ini = version_dir / "php.ini"
        target_prod_ini = version_dir / "php-prod.ini"

        if not target_ini.exists() and base_php_ini.exists() and base_php_ini.is_file():
            shutil.copy2(base_php_ini, target_ini)

        if not target_prod_ini.exists() and base_php_prod_ini.exists() and base_php_prod_ini.is_file():
            shutil.copy2(base_php_prod_ini, target_prod_ini)


def ensure_php_version_config(app_dir: Path, php_version: str) -> None:
    """Ensure php.ini files exist for a specific PHP version.

    If the versioned directory doesn't have config files, copies from base php/ directory.

    Args:
        app_dir: The application data directory.
        php_version: The PHP version string (e.g., "8.4").
    """
    version_dir = app_dir / "php" / php_version
    version_dir.mkdir(parents=True, exist_ok=True)

    base_php_ini = app_dir / "php" / "php.ini"
    base_php_prod_ini = app_dir / "php" / "php-prod.ini"

    target_ini = version_dir / "php.ini"
    target_prod_ini = version_dir / "php-prod.ini"

    if not target_ini.exists() and base_php_ini.exists() and base_php_ini.is_file():
        shutil.copy2(base_php_ini, target_ini)

    if not target_prod_ini.exists() and base_php_prod_ini.exists() and base_php_prod_ini.is_file():
        shutil.copy2(base_php_prod_ini, target_prod_ini)


def get_project_dir() -> Path:
    """Get the project directory for FrankenManager.

    For production (PyInstaller binary or pip install), this returns
    ~/.frankenmanager/ where all configs and generated files are stored.

    For development (editable install from source), this returns the
    project root directory.

    Returns:
        Path to the project directory.
    """
    # Development mode - use the project root for easier testing
    if _is_development_mode():
        package_dir = Path(__file__).parent.parent
        project_root = package_dir.parent.parent
        if (project_root / ".env.example").exists():
            return project_root

    # Production mode - always use ~/.frankenmanager/
    return ensure_resources_extracted()


def _is_development_mode() -> bool:
    """Check if running in development mode (editable install or direct source).

    Development mode is detected by:
    1. Not running from a frozen PyInstaller bundle
    2. Having pyproject.toml and src/ in a parent directory

    Returns:
        True if running in development mode.
    """
    # PyInstaller bundles are never development mode
    if getattr(sys, "frozen", False):
        return False

    package_dir = Path(__file__).parent.parent

    # Check if this is an editable install or running from source
    # by looking for pyproject.toml in parent directories
    for parent in package_dir.parents:
        if (parent / "pyproject.toml").exists() and (parent / "src").exists():
            return True
        if parent == Path.home():
            break

    return False


def reset_resources() -> None:
    """Reset the app data directory by removing the initialization marker.

    This will cause resources to be re-extracted on next run.
    Useful for updating bundled resources after a version upgrade.
    """
    app_dir = get_app_data_dir()
    marker_file = app_dir / ".initialized"
    if marker_file.exists():
        marker_file.unlink()
        log_info("FrankenManager resources will be re-initialized on next run.")


def get_data_dir_info() -> dict[str, str]:
    """Get information about the data directory for display purposes.

    Returns:
        Dictionary with data directory information.
    """
    app_dir = get_app_data_dir()
    return {
        "data_dir": str(app_dir),
        "initialized": str((app_dir / ".initialized").exists()),
        "mode": "development" if _is_development_mode() else "production",
    }
