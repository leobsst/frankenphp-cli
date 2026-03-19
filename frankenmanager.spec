# -*- mode: python ; coding: utf-8 -*-
"""PyInstaller spec file for FrankenManager.

This spec file creates a single executable file that includes all dependencies.
The binary can be distributed without requiring Python to be installed.

Build commands:
    pyinstaller frankenmanager.spec

Or using the build script:
    python scripts/build.py
"""

import os
import sys
from pathlib import Path

block_cipher = None

# Read code signing identity from environment variable (set during CI builds)
codesign_id = os.environ.get("CODESIGN_IDENTITY")
entitlements = os.environ.get("ENTITLEMENTS_FILE")

# Get the source directory
src_path = Path("src/frankenmanager")

# Bundle resources directly from project root (no duplication needed)
resource_files = [
    (".env.example", "resources"),
    ("docker-compose.yml", "resources"),
    ("docker-compose-prod.yml", "resources"),
    ("Dockerfile", "resources"),
    ("caddy/Caddyfile", "resources/caddy"),
    ("caddy/Caddyfile.template", "resources/caddy"),
    ("php/php.ini", "resources/php"),
    ("php/php-prod.ini", "resources/php"),
]

a = Analysis(
    ["src/frankenmanager/__main__.py"],
    pathex=[],
    binaries=[],
    datas=resource_files,
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
        "frankenmanager",
        "frankenmanager.cli",
        "frankenmanager.commands",
        "frankenmanager.commands.start",
        "frankenmanager.commands.stop",
        "frankenmanager.commands.restart",
        "frankenmanager.commands.status",
        "frankenmanager.commands.setup",
        "frankenmanager.core",
        "frankenmanager.core.config",
        "frankenmanager.core.docker_manager",
        "frankenmanager.core.environment",
        "frankenmanager.core.hosts_manager",
        "frankenmanager.core.ssl_manager",
        "frankenmanager.core.caddyfile",
        "frankenmanager.core.password_manager",
        "frankenmanager.core.privilege_manager",
        "frankenmanager.core.resources",
        "frankenmanager.utils",
        "frankenmanager.utils.logging",
        "frankenmanager.utils.platform",
        "frankenmanager.utils.validation",
        "frankenmanager.exceptions",
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
    codesign_identity=codesign_id,
    entitlements_file=entitlements,
    icon=None,  # Add icon path here if desired: icon="assets/icon.ico"
)
