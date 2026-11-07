"""Run a command with a domain's PHP version, from the current directory."""

import os
import subprocess
import sys
from pathlib import Path, PurePath, PureWindowsPath
from typing import Optional

from ..core.database import DatabaseManager
from ..core.docker_manager import DockerManager
from ..core.environment import EnvironmentManager
from ..core.php_versions import get_image_name
from ..core.resources import ensure_php_version_config, get_project_dir
from ..core.stack import get_project_path
from ..exceptions import DockerError, ValidationError

# Fallback user when the host has no Unix IDs (Windows) and .env sets none:
# the default user of WSL2 distributions and most Linux desktops.
DEFAULT_UID = "1000"
DEFAULT_GID = "1000"


def resolve_php_version(db: DatabaseManager, domain: str) -> str:
    """Get the PHP version serving a domain, following aliases.

    Args:
        db: Database manager.
        domain: Registered domain or alternate host.

    Returns:
        The PHP version of the domain.

    Raises:
        ValidationError: If the domain is unknown or has no PHP version.
    """
    version = db.get_domain_php_version(domain)
    if version:
        return version

    target = db.get_alias_target(domain)
    if target:
        version = db.get_domain_php_version(target)
        if version:
            return version

    if db.get_proxy_target(domain):
        raise ValidationError(f"'{domain}' is a proxy host: it has no PHP version.")
    raise ValidationError(f"Domain '{domain}' is not configured.")


def to_container_path(path: PurePath) -> str:
    """Get the path a host directory is mounted at inside the container.

    POSIX paths are kept as-is; Windows paths become ``/c/Users/...``.

    Args:
        path: Absolute host path.

    Returns:
        The absolute POSIX path inside the container.
    """
    windows = PureWindowsPath(str(path))
    if windows.drive and ":" in windows.drive:
        return "/" + "/".join([windows.drive[0].lower(), *windows.parts[1:]])
    return path.as_posix()


def get_user(env: EnvironmentManager) -> str:
    """Get the current user as a Docker ``uid:gid``.

    Args:
        env: Environment manager (UID/GID fallback on Windows).

    Returns:
        The ``uid:gid`` to run the container as.
    """
    if hasattr(os, "getuid") and hasattr(os, "getgid"):
        return f"{os.getuid()}:{os.getgid()}"
    return f"{env.get('UID') or DEFAULT_UID}:{env.get('GID') or DEFAULT_GID}"


def get_mounts(cwd: Path, project_path: Optional[str]) -> list[Path]:
    """Get the host directories to mount, each at the same path in the container.

    The projects root is mounted so apps reaching sibling projects (e.g.
    Composer path repositories) keep working; the current directory is added
    unless it is already inside it.

    Args:
        cwd: Directory the command is run from.
        project_path: Projects root, if known.

    Returns:
        Directories to mount.
    """
    mounts: list[Path] = []
    if project_path:
        root = Path(project_path).expanduser()
        if root.is_dir():
            mounts.append(root)
            for candidate in (root, root.resolve()):
                if cwd == candidate or candidate in cwd.parents:
                    return mounts
    mounts.append(cwd)
    return mounts


def build_run_command(
    image: str,
    user: str,
    workdir: Path,
    mounts: list[Path],
    php_ini_mounts: list[tuple[Path, str]],
    command: list[str],
    tty: bool,
) -> list[str]:
    """Build the docker run command line.

    A single argument is run through a shell, so a quoted command line
    (``"php artisan migrate --force"``, pipes, ``&&``...) works as typed.
    Several arguments are run as-is, preserving the user's own quoting.

    Args:
        image: PHP image of the domain's version.
        user: ``uid:gid`` to run as.
        workdir: Directory the command is run from.
        mounts: Host directories mounted at the same path.
        php_ini_mounts: (host file, container path) pairs of PHP config.
        command: Command to run, as given on the command line.
        tty: Whether to allocate a TTY.

    Returns:
        The docker command line.
    """
    cmd = ["docker", "run", "--rm", "-i"]
    if tty:
        cmd.append("-t")
    cmd.extend(
        [
            # Same network as the PHP containers: databases, Redis and the
            # other domains are reachable as from the served sites.
            "--network",
            "host",
            "--user",
            user,
            # No entrypoint: it updates the CA store, which needs root.
            "--entrypoint",
            "",
            # The host user has no home in the image
            "-e",
            "HOME=/tmp",
            "-e",
            "COMPOSER_CACHE_DIR=/tmp/composer-cache",
            # Mounted files may be owned by another UID than the user's
            "-e",
            "GIT_CONFIG_COUNT=1",
            "-e",
            "GIT_CONFIG_KEY_0=safe.directory",
            "-e",
            "GIT_CONFIG_VALUE_0=*",
        ]
    )
    for mount in mounts:
        cmd.extend(["-v", f"{mount}:{to_container_path(mount)}"])
    for host_file, container_file in php_ini_mounts:
        cmd.extend(["-v", f"{host_file}:{container_file}:ro"])
    cmd.extend(["-w", to_container_path(workdir), image])
    if len(command) == 1:
        cmd.extend(["sh", "-c", command[0]])
    else:
        cmd.extend(command)
    return cmd


def exec_command(domain: str, command: list[str]) -> None:
    """Run a command with the domain's PHP version, from the current directory.

    Runs as the current user in a one-off container of the domain's PHP image,
    and exits with the command's exit code.

    Args:
        domain: Registered domain or alternate host.
        command: Command to run.
    """
    if not command or not any(part.strip() for part in command):
        raise ValidationError("No command given.")

    project_dir = get_project_dir()
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    env.load()
    db = DatabaseManager(project_dir / "db.sqlite")

    php_version = resolve_php_version(db, domain)
    image = get_image_name(php_version)
    try:
        DockerManager(project_dir).client.images.get(image)
    except DockerError:
        raise
    except Exception:
        raise DockerError(
            f"Image '{image}' not found: start the server (or add a host) "
            f"with PHP {php_version} to build it."
        )

    ensure_php_version_config(project_dir, php_version)
    ini_dir = project_dir / "php" / php_version
    php_ini_mounts = [(ini_dir / "php.ini", "/usr/local/etc/php/php.ini")]
    if env.is_production():
        php_ini_mounts.append(
            (ini_dir / "php-prod.ini", "/usr/local/etc/php/conf.d/99-production.ini")
        )
    php_ini_mounts = [(host, target) for host, target in php_ini_mounts if host.is_file()]

    cwd = Path.cwd()
    cmd = build_run_command(
        image,
        get_user(env),
        cwd,
        get_mounts(cwd, get_project_path(env, db)),
        php_ini_mounts,
        command,
        sys.stdin.isatty() and sys.stdout.isatty(),
    )
    try:
        result = subprocess.run(cmd, check=False)
    except KeyboardInterrupt:
        sys.exit(130)
    sys.exit(result.returncode)
