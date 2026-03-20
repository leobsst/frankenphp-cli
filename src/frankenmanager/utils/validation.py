"""Validation utilities for domains, files, and commands."""

import re
import shutil
from pathlib import Path

from ..exceptions import ValidationError

# Domain name regex pattern (e.g., myapp.test, example.com)
DOMAIN_REGEX = re.compile(r"^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z]{2,})+$")

# Reserved local hostnames allowed without a TLD
LOCAL_HOSTNAMES = {"localhost"}


def validate_domain(domain: str) -> None:
    """Validate a domain name format.

    Args:
        domain: The domain name to validate (e.g., myapp.test, localhost).

    Raises:
        ValidationError: If the domain name is invalid.
    """
    if domain in LOCAL_HOSTNAMES:
        return
    if not DOMAIN_REGEX.match(domain):
        raise ValidationError(f"Invalid domain name: {domain}")


def validate_directory(path: Path) -> None:
    """Validate that a directory exists.

    Args:
        path: The path to validate.

    Raises:
        ValidationError: If the directory does not exist.
    """
    if not path.is_dir():
        raise ValidationError(f"Directory does not exist: {path}")


def validate_file(path: Path) -> None:
    """Validate that a file exists.

    Args:
        path: The path to validate.

    Raises:
        ValidationError: If the file does not exist.
    """
    if not path.is_file():
        raise ValidationError(f"File does not exist: {path}")


def require_command(cmd: str) -> None:
    """Validate that a command is available in PATH.

    Args:
        cmd: The command name to check.

    Raises:
        ValidationError: If the command is not found.
    """
    if not shutil.which(cmd):
        raise ValidationError(f"Required command not found: {cmd}")


def require_file(path: Path) -> None:
    """Alias for validate_file for consistency with bash scripts.

    Args:
        path: The path to validate.

    Raises:
        ValidationError: If the file does not exist.
    """
    validate_file(path)


def require_directory(path: Path) -> None:
    """Alias for validate_directory for consistency with bash scripts.

    Args:
        path: The path to validate.

    Raises:
        ValidationError: If the directory does not exist.
    """
    validate_directory(path)
