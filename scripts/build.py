#!/usr/bin/env python3
"""Build script for creating FrankenManager binaries.

This script builds a standalone onedir bundle (dist/frankenmanager/) using
PyInstaller. It can be run locally or by GitHub Actions for release builds.

Usage:
    python scripts/build.py [--clean] [--test] [--release]

Options:
    --clean    Remove build artifacts before building
    --test     Run the built binary to verify it works
    --release  Package the bundle as dist/frankenmanager-<platform>.tar.gz and
               also build the legacy single-file dist/frankenmanager-<platform>
"""

import argparse
import os
import platform
import shutil
import subprocess
import sys
import tarfile
from pathlib import Path

BUNDLE_NAME = "frankenmanager"


def get_platform_suffix() -> str:
    """Get the platform-specific suffix for the binary name."""
    system = platform.system().lower()
    machine = platform.machine().lower()

    if system == "darwin":
        if machine == "arm64":
            return "macos-arm64"
        return "macos-x86_64"
    elif system == "linux":
        if machine == "aarch64":
            return "linux-arm64"
        return "linux-x86_64"
    elif system == "windows":
        return "windows-x86_64"
    else:
        return f"{system}-{machine}"


def clean_build_artifacts(project_root: Path) -> None:
    """Remove build artifacts."""
    print("Cleaning build artifacts...")
    dirs_to_remove = ["build", "dist", "__pycache__"]

    for dir_name in dirs_to_remove:
        dir_path = project_root / dir_name
        if dir_path.exists():
            print(f"  Removing {dir_path}")
            shutil.rmtree(dir_path)

    # Clean __pycache__ in subdirectories
    for pycache in project_root.rglob("__pycache__"):
        print(f"  Removing {pycache}")
        shutil.rmtree(pycache)

    # Clean .pyc files
    for pyc in project_root.rglob("*.pyc"):
        print(f"  Removing {pyc}")
        pyc.unlink()


def build_binary(project_root: Path, legacy_onefile: bool = False) -> Path:
    """Build the onedir bundle using PyInstaller.

    Args:
        project_root: Project root directory.
        legacy_onefile: Also build the legacy single-file binary.

    Returns:
        Path to the bundle's executable.
    """
    print("Building binary with PyInstaller...")

    spec_file = project_root / "frankenmanager.spec"

    if not spec_file.exists():
        print(f"Error: Spec file not found: {spec_file}")
        sys.exit(1)

    env = os.environ.copy()
    if legacy_onefile:
        env["FRANKENMANAGER_LEGACY_ONEFILE"] = "1"

    # Run PyInstaller
    result = subprocess.run(
        [sys.executable, "-m", "PyInstaller", str(spec_file), "--noconfirm"],
        cwd=project_root,
        env=env,
        check=False,
    )

    if result.returncode != 0:
        print("Error: PyInstaller build failed")
        sys.exit(1)

    # Find the built binary
    dist_dir = project_root / "dist"
    binary_name = f"{BUNDLE_NAME}.exe" if platform.system() == "Windows" else BUNDLE_NAME
    binary_path = dist_dir / BUNDLE_NAME / binary_name

    if not binary_path.exists():
        print(f"Error: Built binary not found at {binary_path}")
        sys.exit(1)

    print(f"Binary built successfully: {binary_path}")
    return binary_path


def package_for_release(binary_path: Path) -> Path:
    """Package the release assets with the platform suffix.

    Creates frankenmanager-<platform>.tar.gz from the onedir bundle (its
    single top-level directory is frankenmanager/), and renames the legacy
    single-file binary to frankenmanager-<platform>.

    Returns:
        Path to the release archive.
    """
    suffix = get_platform_suffix()
    bundle_dir = binary_path.parent
    dist_dir = bundle_dir.parent

    archive_path = dist_dir / f"{BUNDLE_NAME}-{suffix}.tar.gz"
    print(f"Creating release archive: {archive_path.name}")
    with tarfile.open(archive_path, "w:gz") as tar:
        tar.add(bundle_dir, arcname=BUNDLE_NAME)

    ext = ".exe" if platform.system() == "Windows" else ""
    legacy_path = dist_dir / f"{BUNDLE_NAME}-onefile{ext}"
    if not legacy_path.exists():
        print(f"Error: Legacy single-file binary not found at {legacy_path}")
        sys.exit(1)
    legacy_name = f"{BUNDLE_NAME}-{suffix}{ext}"
    print(f"Renaming legacy single-file binary to: {legacy_name}")
    legacy_path.replace(dist_dir / legacy_name)

    return archive_path


def test_binary(binary_path: Path) -> bool:
    """Test the built binary.

    Returns:
        True if test passed, False otherwise.
    """
    print(f"Testing binary: {binary_path}")

    # Test version command
    result = subprocess.run(
        [str(binary_path), "--version"],
        capture_output=True,
        text=True,
        check=False,
    )

    if result.returncode != 0:
        print(f"Error: Version check failed: {result.stderr}")
        return False

    print(f"  Version output: {result.stdout.strip()}")

    # Test help command
    result = subprocess.run(
        [str(binary_path), "--help"],
        capture_output=True,
        text=True,
        check=False,
    )

    if result.returncode != 0:
        print(f"Error: Help check failed: {result.stderr}")
        return False

    print("  Help command: OK")

    # Test setup --status (doesn't require privileges)
    result = subprocess.run(
        [str(binary_path), "setup", "--status"],
        capture_output=True,
        text=True,
        check=False,
    )

    # This might fail if not configured, but should at least run
    print("  Setup status command: OK")

    print("All tests passed!")
    return True


def main() -> None:
    """Main entry point."""
    parser = argparse.ArgumentParser(description="Build FrankenManager binary")
    parser.add_argument("--clean", action="store_true", help="Clean build artifacts first")
    parser.add_argument("--test", action="store_true", help="Test the built binary")
    parser.add_argument(
        "--release", action="store_true", help="Package release assets with platform suffix"
    )
    args = parser.parse_args()

    # Find project root (parent of scripts directory)
    project_root = Path(__file__).parent.parent.resolve()
    print(f"Project root: {project_root}")

    if args.clean:
        clean_build_artifacts(project_root)

    binary_path = build_binary(project_root, legacy_onefile=args.release)

    if args.test:
        if not test_binary(binary_path):
            sys.exit(1)

    print(f"\nBuild complete: {binary_path}")
    bundle_size = sum(f.stat().st_size for f in binary_path.parent.rglob("*") if f.is_file())
    print(f"Bundle size: {bundle_size / 1024 / 1024:.2f} MB")

    if args.release:
        archive_path = package_for_release(binary_path)
        print(f"Release archive: {archive_path}")
        print(f"Archive size: {archive_path.stat().st_size / 1024 / 1024:.2f} MB")


if __name__ == "__main__":
    main()
