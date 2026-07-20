"""Toggle sharing the local mkcert root CA over the LAN."""

from pathlib import Path

from ..core.ca_server import (
    DEFAULT_PORT,
    get_lan_ip,
    get_running_state,
    start_sharing,
    stop_sharing,
)
from ..core.environment import EnvironmentManager
from ..core.resources import get_project_dir
from ..exceptions import ServerStateError
from ..utils.logging import log_info, log_success, log_warning


def _root_ca_path() -> Path:
    project_dir = get_project_dir()
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    if env.env_path.exists():
        env.load()

    caddy_dir_value = env.get("CADDY_DIR")
    caddy_dir = Path(caddy_dir_value) if caddy_dir_value else project_dir / "caddy"
    if not caddy_dir.is_absolute():
        caddy_dir = project_dir / caddy_dir

    return caddy_dir / "certs" / "rootCA.pem"


def trust_ca_on(port: int = DEFAULT_PORT) -> None:
    """Start serving the local mkcert root CA on the host's LAN IP."""
    project_dir = get_project_dir()
    cert_file = _root_ca_path()

    start_sharing(project_dir, cert_file, port)

    ip = get_lan_ip()
    log_success(f"Root CA is now shared at http://{ip}:{port}/")
    log_info("Open that URL in a browser on the other device to download and install rootCA.pem.")
    log_warning(
        "Anyone on your network can reach this port while sharing is on. "
        "Run `frankenmanager trust-ca off` once other devices have installed it."
    )


def trust_ca_off() -> None:
    """Stop serving the local mkcert root CA."""
    project_dir = get_project_dir()
    if not stop_sharing(project_dir):
        raise ServerStateError("Root CA sharing is not currently running.")
    log_success("Root CA sharing stopped.")


def trust_ca_status() -> None:
    """Print whether the root CA is currently being shared."""
    project_dir = get_project_dir()
    state = get_running_state(project_dir)
    if state is None:
        log_info("Root CA sharing is currently OFF.")
        return

    _pid, port = state
    ip = get_lan_ip()
    log_info(f"Root CA sharing is ON at http://{ip}:{port}/")
