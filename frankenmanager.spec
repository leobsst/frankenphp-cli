# -*- mode: python ; coding: utf-8 -*-
"""PyInstaller spec file for FrankenManager.

This spec file creates a single executable file that includes all dependencies.
The binary can be distributed without requiring Python to be installed.

Build commands:
    pyinstaller frankenmanager.spec

Or using the build script:
    python scripts/build.py
"""

import sys
from pathlib import Path

block_cipher = None

# Get the source directory
src_path = Path("src/frankenphp_cli")
resources_path = src_path / "resources"

a = Analysis(
    ["src/frankenphp_cli/__main__.py"],
    pathex=[],
    binaries=[],
    datas=[
        # Bundle all resources (config files, templates, etc.)
        (str(resources_path), "resources"),
    ],
    hiddenimports=[
        # Typer and Rich dependencies
        "typer",
        "typer.main",
        "typer.core",
        "rich",
        "rich.console",
        "rich.table",
        "rich.panel",
        "rich.progress",
        "rich.markup",
        "rich.text",
        # Docker SDK
        "docker",
        "docker.api",
        "docker.models",
        "docker.models.containers",
        # Python-dotenv
        "dotenv",
        # Our modules
        "frankenphp_cli",
        "frankenphp_cli.cli",
        "frankenphp_cli.commands",
        "frankenphp_cli.commands.start",
        "frankenphp_cli.commands.stop",
        "frankenphp_cli.commands.restart",
        "frankenphp_cli.commands.status",
        "frankenphp_cli.commands.setup",
        "frankenphp_cli.core",
        "frankenphp_cli.core.config",
        "frankenphp_cli.core.docker_manager",
        "frankenphp_cli.core.environment",
        "frankenphp_cli.core.hosts_manager",
        "frankenphp_cli.core.ssl_manager",
        "frankenphp_cli.core.caddyfile",
        "frankenphp_cli.core.password_manager",
        "frankenphp_cli.core.privilege_manager",
        "frankenphp_cli.core.resources",
        "frankenphp_cli.utils",
        "frankenphp_cli.utils.logging",
        "frankenphp_cli.utils.platform",
        "frankenphp_cli.utils.validation",
        "frankenphp_cli.exceptions",
    ],
    hookspath=[],
    hooksconfig={},
    runtime_hooks=[],
    excludes=[
        # Exclude test modules
        "pytest",
        "pytest_cov",
        "pytest_mock",
        # Exclude development tools
        "mypy",
        "ruff",
        # Exclude unnecessary modules
        "tkinter",
        "unittest",
    ],
    win_no_prefer_redirects=False,
    win_private_assemblies=False,
    cipher=block_cipher,
    noarchive=False,
)

pyz = PYZ(a.pure, a.zipped_data, cipher=block_cipher)

exe = EXE(
    pyz,
    a.scripts,
    a.binaries,
    a.zipfiles,
    a.datas,
    [],
    name="frankenmanager",
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    upx=True,
    upx_exclude=[],
    runtime_tmpdir=None,
    console=True,
    disable_windowed_traceback=False,
    argv_emulation=False,
    target_arch=None,
    codesign_identity=None,
    entitlements_file=None,
    icon=None,  # Add icon path here if desired: icon="assets/icon.ico"
)
