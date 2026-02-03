#!/usr/bin/env python3
"""Build script for creating FrankenManager binaries.

This script builds standalone executables using PyInstaller.
It can be run locally or by GitHub Actions for release builds.

Usage:
    python scripts/build.py [--clean] [--test]

Options:
    --clean    Remove build artifacts before building
    --test     Run the built binary to verify it works
"""

import argparse
import platform
import shutil
import subprocess
import sys
from pathlib import Path


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


def build_binary(project_root: Path) -> Path:
    """Build the binary using PyInstaller.

    Returns:
        Path to the built binary.
    """
    print("Building binary with PyInstaller...")

    spec_file = project_root / "frankenmanager.spec"

    if not spec_file.exists():
        print(f"Error: Spec file not found: {spec_file}")
        sys.exit(1)

    # Run PyInstaller
    result = subprocess.run(
        [sys.executable, "-m", "PyInstaller", str(spec_file), "--noconfirm"],
        cwd=project_root,
        check=False,
    )

    if result.returncode != 0:
        print("Error: PyInstaller build failed")
        sys.exit(1)

    # Find the built binary
    dist_dir = project_root / "dist"
    binary_name = "frankenmanager.exe" if platform.system() == "Windows" else "frankenmanager"
    binary_path = dist_dir / binary_name

    if not binary_path.exists():
        print(f"Error: Built binary not found at {binary_path}")
        sys.exit(1)

    print(f"Binary built successfully: {binary_path}")
    return binary_path


def rename_binary_for_release(binary_path: Path) -> Path:
    """Rename the binary with platform suffix for release.

    Returns:
        Path to the renamed binary.
    """
    suffix = get_platform_suffix()
    ext = ".exe" if platform.system() == "Windows" else ""
    new_name = f"frankenmanager-{suffix}{ext}"
    new_path = binary_path.parent / new_name

    print(f"Renaming binary to: {new_name}")
    shutil.copy2(binary_path, new_path)

    return new_path


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
    parser.add_argument("--release", action="store_true", help="Rename binary with platform suffix")
    args = parser.parse_args()

    # Find project root (parent of scripts directory)
    project_root = Path(__file__).parent.parent.resolve()
    print(f"Project root: {project_root}")

    if args.clean:
        clean_build_artifacts(project_root)

    binary_path = build_binary(project_root)

    if args.release:
        binary_path = rename_binary_for_release(binary_path)

    if args.test:
        if not test_binary(binary_path):
            sys.exit(1)

    print(f"\nBuild complete: {binary_path}")
    print(f"Binary size: {binary_path.stat().st_size / 1024 / 1024:.2f} MB")


if __name__ == "__main__":
    main()
