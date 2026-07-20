"""Toggle sharing the local mkcert root CA over the LAN."""

import io
import shutil
import subprocess
from pathlib import Path

import qrcode

from ..core.ca_server import (
    DEFAULT_PORT,
    get_lan_ip,
    get_running_state,
    start_sharing,
    stop_sharing,
)
from ..core.resources import get_project_dir
from ..exceptions import ServerStateError, SSLError
from ..utils.logging import console, log_info, log_success, log_warning


def _render_qr_ascii(data: str) -> str:
    """Render a scannable QR code for `data` as terminal-printable ASCII art."""
    qr = qrcode.QRCode(border=1)
    qr.add_data(data)
    qr.make(fit=True)
    buf = io.StringIO()
    qr.print_ascii(out=buf)
    return buf.getvalue()


def _root_ca_path() -> Path:
    """Locate mkcert's actual rootCA.pem from its live CAROOT.

    This is the CA installed system-wide by
    `sudo frankenmanager setup --install-mkcert`, not any project's own
    copy under caddy/certs/, so what gets shared always matches what's
    actually trusted on this machine.
    """
    mkcert = shutil.which("mkcert")
    if not mkcert:
        raise SSLError("mkcert not found. Run `sudo frankenmanager setup --install-mkcert` first.")

    result = subprocess.run([mkcert, "-CAROOT"], capture_output=True, text=True)
    if result.returncode != 0:
        raise SSLError(f"Could not determine mkcert's CA root: {result.stderr.strip()}")

    return Path(result.stdout.strip()) / "rootCA.pem"


def trust_ca_on(port: int = DEFAULT_PORT) -> None:
    """Start serving the local mkcert root CA on the host's LAN IP."""
    project_dir = get_project_dir()
    cert_file = _root_ca_path()

    start_sharing(project_dir, cert_file, port)

    ip = get_lan_ip()
    url = f"http://{ip}:{port}/"
    log_success(f"Root CA is now shared at {url}")
    console.print(_render_qr_ascii(url), markup=False, highlight=False)
    log_info(
        "Scan the QR code (or open that URL in a browser) on the other device "
        "to download rootCA.pem."
    )
    log_info(
        'iOS: install the profile even though it shows "Not Verified" (normal for any '
        "manually-installed CA) - then you MUST also go to Settings > General > About > "
        'Certificate Trust Settings and enable full trust for "FrankenManager Local '
        'Development CA". Without that last toggle, HTTPS warnings keep appearing.'
    )
    log_info("Android: trust is granted automatically as soon as the certificate is installed.")
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
