"""Optional Soketi WebSocket server (Pusher protocol) for the apps that need one.

Off by default: set SOKETI_ENABLED=true in .env to turn it on. The server runs
in its own container and is served over TLS by the reverse proxy at
wss://ws.frankenmanager.test, while PHP apps (host network) reach its HTTP API
at 127.0.0.1:SOKETI_PORT. Its default app is configured with
SOKETI_DEFAULT_APP_ID, SOKETI_DEFAULT_APP_KEY and SOKETI_DEFAULT_APP_SECRET,
generated on start when left empty.
"""

import secrets
import string
from typing import TYPE_CHECKING, Optional

if TYPE_CHECKING:
    from .environment import EnvironmentManager

CONTAINER_NAME = "franken_soketi"
IMAGE = "quay.io/soketi/soketi:1.6-16-alpine"
# Public WebSocket host, served over TLS by the reverse proxy
DOMAIN = "ws.frankenmanager.test"
DEFAULT_PORT = "6001"
DEFAULT_APP_ID = "frankenmanager"

APP_ID_KEY = "SOKETI_DEFAULT_APP_ID"
APP_KEY_KEY = "SOKETI_DEFAULT_APP_KEY"
APP_SECRET_KEY = "SOKETI_DEFAULT_APP_SECRET"
CREDENTIAL_KEYS = (APP_ID_KEY, APP_KEY_KEY, APP_SECRET_KEY)


def is_enabled(env: "EnvironmentManager") -> bool:
    """Check if the Soketi container should run.

    Args:
        env: Environment manager.

    Returns:
        True if SOKETI_ENABLED is 'true' (disabled by default).
    """
    return (env.get("SOKETI_ENABLED") or "").strip().lower() == "true"


def get_port(env: "EnvironmentManager") -> str:
    """Get the host port Soketi listens on.

    Args:
        env: Environment manager.

    Returns:
        SOKETI_PORT, or the default port.
    """
    return (env.get("SOKETI_PORT") or "").strip() or DEFAULT_PORT


def get_target(env: "EnvironmentManager") -> Optional[str]:
    """Get the upstream the reverse proxy forwards ws.frankenmanager.test to.

    Args:
        env: Environment manager.

    Returns:
        "127.0.0.1:<port>" when Soketi is enabled, None otherwise.
    """
    return f"127.0.0.1:{get_port(env)}" if is_enabled(env) else None


def get_domains(env: "EnvironmentManager") -> list[str]:
    """Get the domains Soketi needs a certificate, hosts entry and DNS record for.

    Args:
        env: Environment manager.

    Returns:
        [DOMAIN] when Soketi is enabled, an empty list otherwise.
    """
    return [DOMAIN] if is_enabled(env) else []


def _generate_secret() -> str:
    """Generate a random 32-character alphanumeric secret."""
    alphabet = string.ascii_lowercase + string.digits
    return "".join(secrets.choice(alphabet) for _ in range(32))


def ensure_credentials(env: "EnvironmentManager") -> list[str]:
    """Fill in the default app credentials left empty in .env.

    Does nothing when Soketi is disabled.

    Args:
        env: Environment manager (the generated values are saved to .env).

    Returns:
        Names of the variables that were generated.
    """
    if not is_enabled(env):
        return []

    generated: list[str] = []
    for key in CREDENTIAL_KEYS:
        if (env.get(key) or "").strip():
            continue
        env.set(key, DEFAULT_APP_ID if key == APP_ID_KEY else _generate_secret())
        generated.append(key)
    return generated


def build_compose_env_vars(env: "EnvironmentManager", localhost: str) -> dict[str, str]:
    """Build the variables the Soketi compose service expects.

    Args:
        env: Environment manager.
        localhost: Port binding prefix ("127.0.0.1:" or "" when exposed).

    Returns:
        Compose variables, empty when Soketi is disabled.
    """
    if not is_enabled(env):
        return {}

    port = get_port(env)
    return {
        "SOKETI_PORT": port,
        "SOKETI_PUBLISH": f"{localhost}{port}:{DEFAULT_PORT}",
        **{key: env.require(key) for key in CREDENTIAL_KEYS},
    }


def build_service(production: bool) -> list[str]:
    """Build the Soketi docker-compose service.

    No env_file on purpose: Soketi reads its own SOKETI_* variables, and our
    SOKETI_PORT (host port) must not leak into the container as-is.

    Args:
        production: Host network in production, published port in dev.

    Returns:
        YAML lines of the service.
    """
    lines = [
        "  soketi:",
        f"    container_name: {CONTAINER_NAME}",
        f"    image: {IMAGE}",
        "    restart: always",
        "    environment:",
        f"      {APP_ID_KEY}: ${{{APP_ID_KEY}}}",
        f"      {APP_KEY_KEY}: ${{{APP_KEY_KEY}}}",
        f"      {APP_SECRET_KEY}: ${{{APP_SECRET_KEY}}}",
    ]
    if production:
        # Host network: listen straight on the configured port
        lines.extend(
            [
                f"      SOKETI_PORT: '${{SOKETI_PORT:-{DEFAULT_PORT}}}'",
                "    network_mode: host",
            ]
        )
    else:
        lines.extend(
            [
                f"      SOKETI_PORT: '{DEFAULT_PORT}'",
                "    ports:",
                f"      - ${{SOKETI_PUBLISH:-127.0.0.1:{DEFAULT_PORT}:{DEFAULT_PORT}}}",
                "    networks:",
                "      - franken_network",
            ]
        )
    lines.extend(
        [
            "    healthcheck:",
            '      test: ["CMD-SHELL", "wget -q -O /dev/null http://127.0.0.1:$${SOKETI_PORT}/ready"]',
            "      interval: 10s",
            "      timeout: 5s",
            "      retries: 5",
            "      start_period: 10s",
            "",
        ]
    )
    return lines
