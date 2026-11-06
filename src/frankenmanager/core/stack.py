"""Docker compose stack helpers shared by the server commands."""

from pathlib import Path

from . import dns_server
from .database import DatabaseManager
from .docker_manager import DockerManager
from .environment import EnvironmentManager

# Settings key holding the projects root mounted into the PHP containers
PROJECT_PATH_SETTING = "project_path"


def get_project_path(env: EnvironmentManager, db: DatabaseManager) -> str:
    """Get the projects root the server was started with.

    Falls back to DEFAULT_PROJECT_PATH for servers started before it was stored.

    Args:
        env: Environment manager.
        db: Database manager.

    Returns:
        The projects root, empty if unknown.
    """
    return db.get_setting(PROJECT_PATH_SETTING) or env.get("DEFAULT_PROJECT_PATH") or ""


def generate_compose(
    docker: DockerManager, env: EnvironmentManager, php_versions: set[str]
) -> None:
    """Regenerate the compose file from the .env settings.

    Args:
        docker: Docker manager.
        env: Environment manager.
        php_versions: PHP versions that need a container.
    """
    docker.generate_compose_file(
        php_versions,
        {},
        env.is_production(),
        env.get_db_engines(),
        dns_server.get_listen_address(env),
        env.is_redis_enabled(),
    )


def compose_up(
    project_dir: Path,
    docker: DockerManager,
    env: EnvironmentManager,
    php_versions: set[str],
    custom_path: str,
) -> None:
    """Regenerate the compose file and bring the stack up to date.

    Args:
        project_dir: Project (data) directory.
        docker: Docker manager.
        env: Environment manager.
        php_versions: PHP versions that need a container.
        custom_path: Projects root mounted into the PHP containers.
    """
    generate_compose(docker, env, php_versions)
    env_vars = env.build_compose_env_vars(project_dir, custom_path, env.get_db_engines())
    docker.compose_up(env_vars, env.is_production())
