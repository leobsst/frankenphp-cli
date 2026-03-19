"""PHP version management and Docker image tag mapping."""

from typing import Optional

# Mapping of PHP versions to their FrankenPHP Docker image tags
PHP_IMAGE_TAGS: dict[str, str] = {
    "8.2": "latest-php8.2",
    "8.3": "latest-php8.3",
    "8.4": "1.12-php8.4",
    "8.5": "1.12-php8.5",
}

# Supported PHP versions (ordered)
SUPPORTED_VERSIONS = sorted(PHP_IMAGE_TAGS.keys())

# Internal HTTP/HTTPS ports for each PHP version container
# These are only used internally; the main Caddy proxy listens on 80/443
PHP_INTERNAL_PORTS: dict[str, tuple[int, int]] = {
    "8.2": (8280, 8243),
    "8.3": (8380, 8343),
    "8.4": (8480, 8443),
    "8.5": (8580, 8543),
}

# Fallback PHP version when not configured in .env
DEFAULT_PHP_VERSION = "8.3"


def resolve_default_php_version(env_value: Optional[str]) -> str:
    """Resolve the default PHP version from .env or fallback.

    Args:
        env_value: Value of DEFAULT_PHP_VERSION from .env (may be None/empty).

    Returns:
        A valid PHP version string.
    """
    if env_value and env_value in PHP_IMAGE_TAGS:
        return env_value
    return DEFAULT_PHP_VERSION


def get_image_tag(php_version: str) -> str:
    """Get the FrankenPHP Docker image tag for a PHP version.

    Args:
        php_version: PHP version string (e.g., "8.3").

    Returns:
        The Docker image tag.

    Raises:
        ValueError: If the version is not supported.
    """
    tag = PHP_IMAGE_TAGS.get(php_version)
    if not tag:
        supported = ", ".join(SUPPORTED_VERSIONS)
        raise ValueError(f"Unsupported PHP version '{php_version}'. Supported: {supported}")
    return tag


def get_container_name(php_version: str) -> str:
    """Get the Docker container name for a PHP version.

    Args:
        php_version: PHP version string (e.g., "8.3").

    Returns:
        Container name like "frankenphp-8.3".
    """
    return f"frankenphp-{php_version}"


def get_image_name(php_version: str) -> str:
    """Get the custom Docker image name for a PHP version.

    Args:
        php_version: PHP version string (e.g., "8.3").

    Returns:
        Image name like "custom-frankenphp:8.3".
    """
    return f"custom-frankenphp:{php_version}"


def get_internal_ports(php_version: str) -> tuple[int, int]:
    """Get the internal HTTP/HTTPS ports for a PHP version container.

    Args:
        php_version: PHP version string (e.g., "8.3").

    Returns:
        Tuple of (http_port, https_port).

    Raises:
        ValueError: If the version is not supported.
    """
    ports = PHP_INTERNAL_PORTS.get(php_version)
    if not ports:
        supported = ", ".join(SUPPORTED_VERSIONS)
        raise ValueError(f"Unsupported PHP version '{php_version}'. Supported: {supported}")
    return ports


def validate_php_version(php_version: str) -> str:
    """Validate and normalize a PHP version string.

    Args:
        php_version: PHP version string to validate.

    Returns:
        The normalized version string.

    Raises:
        ValueError: If the version is not supported.
    """
    if php_version not in PHP_IMAGE_TAGS:
        supported = ", ".join(SUPPORTED_VERSIONS)
        raise ValueError(f"Unsupported PHP version '{php_version}'. Supported: {supported}")
    return php_version
