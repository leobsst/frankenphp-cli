"""Serve the local mkcert root CA over the LAN so other devices can trust it.

Off by default. The certificate served here is the public mkcert root CA
(rootCA.pem) - never the private key - so exposing it on the network only
lets other devices choose to trust your local dev domains; it grants no
access to anything else.
"""

import http.server
import os
import signal
import socket
import subprocess
import sys
from pathlib import Path
from typing import Optional

from ..exceptions import SSLError

DEFAULT_PORT = 9080


def _state_file(app_dir: Path) -> Path:
    return app_dir / ".ca_server.state"


def get_lan_ip() -> str:
    """Best-effort non-loopback IP of this host, for display purposes only."""
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        try:
            sock.connect(("8.8.8.8", 80))
            return str(sock.getsockname()[0])
        except OSError:
            return "127.0.0.1"


def get_running_state(app_dir: Path) -> Optional[tuple[int, int]]:
    """Return (pid, port) of the running CA-share server, if any.

    Cleans up the state file if the recorded process is no longer alive.
    """
    state_file = _state_file(app_dir)
    if not state_file.exists():
        return None

    try:
        pid_str, port_str = state_file.read_text().strip().split()
        pid, port = int(pid_str), int(port_str)
    except ValueError:
        state_file.unlink(missing_ok=True)
        return None

    try:
        os.kill(pid, 0)
    except OSError:
        state_file.unlink(missing_ok=True)
        return None

    return pid, port


def start_sharing(app_dir: Path, cert_file: Path, port: int) -> int:
    """Start serving rootCA.pem on 0.0.0.0:port as a detached background process.

    Returns:
        The PID of the spawned server process.
    """
    if get_running_state(app_dir) is not None:
        raise SSLError(
            "Root CA sharing is already running. Run `frankenmanager trust-ca off` first."
        )
    if not cert_file.exists():
        raise SSLError(
            f"Root CA not found at {cert_file}. Run `frankenmanager start` at least once first."
        )

    # --no-update-check is a top-level option and must precede the subcommand
    if getattr(sys, "frozen", False):
        cmd = [sys.executable, "--no-update-check", "_serve-ca-internal", str(cert_file), str(port)]
    else:
        cmd = [
            sys.executable,
            "-m",
            "frankenmanager",
            "--no-update-check",
            "_serve-ca-internal",
            str(cert_file),
            str(port),
        ]

    process = subprocess.Popen(
        cmd,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )
    _state_file(app_dir).write_text(f"{process.pid} {port}")
    return process.pid


def stop_sharing(app_dir: Path) -> bool:
    """Stop the CA-share server if running. Returns False if it wasn't running."""
    state = get_running_state(app_dir)
    if state is None:
        return False

    pid, _port = state
    try:
        os.kill(pid, signal.SIGTERM)
    except OSError:
        pass
    _state_file(app_dir).unlink(missing_ok=True)
    return True


class _RootCAHandler(http.server.BaseHTTPRequestHandler):
    """Serves the same rootCA.pem bytes for any request path - nothing else is reachable."""

    cert_bytes: bytes = b""

    def do_GET(self) -> None:
        """Respond with the root CA certificate bytes."""
        self._write_headers()
        self.wfile.write(self.cert_bytes)

    def do_HEAD(self) -> None:
        """Respond with headers only, same as GET."""
        self._write_headers()

    def _write_headers(self) -> None:
        self.send_response(200)
        self.send_header("Content-Type", "application/x-x509-ca-cert")
        self.send_header("Content-Disposition", 'attachment; filename="mkcert-rootCA.pem"')
        self.send_header("Content-Length", str(len(self.cert_bytes)))
        self.end_headers()

    def log_message(self, fmt: str, *args: object) -> None:
        """Silence request logging; keeps the parent CLI process quiet."""


def run_server(cert_file: Path, port: int) -> None:
    """Blocking single-file HTTP server. Meant to run as its own subprocess."""
    _RootCAHandler.cert_bytes = cert_file.read_bytes()
    server = http.server.HTTPServer(("0.0.0.0", port), _RootCAHandler)
    server.serve_forever()
