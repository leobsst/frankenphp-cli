"""Serve the local mkcert root CA over the LAN so other devices can trust it.

Off by default. The certificate served here is the public mkcert root CA
(rootCA.pem) - never the private key - so exposing it on the network only
lets other devices choose to trust your local dev domains; it grants no
access to anything else.
"""

import http.client
import http.server
import os
import signal
import socket
import subprocess
import sys
import time
from pathlib import Path
from typing import Optional

from ..exceptions import SSLError

DEFAULT_PORT = 9080
STARTUP_TIMEOUT = 3.0


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
            f"Root CA not found at {cert_file}. "
            "Run `sudo frankenmanager setup --install-mkcert` first."
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
        stderr=subprocess.PIPE,
        start_new_session=True,
    )
    _wait_until_listening(process, port, cert_file.read_bytes())
    _state_file(app_dir).write_text(f"{process.pid} {port}")
    return process.pid


def _wait_until_listening(process: subprocess.Popen, port: int, cert_bytes: bytes) -> None:
    """Block until the spawned server is actually serving our root CA cert.

    Popen returns as soon as the child is forked, long before it has bound
    the port - without this, a child that crashes on startup (port already
    in use, permission denied, missing cert) would still leave `start_sharing`
    reporting success and writing a state file for a process that's already
    dead. A bare TCP connect isn't enough either: if some *other* process is
    already squatting the port, the connect succeeds but it isn't our server
    - so the response body is checked against the actual cert bytes.
    """
    deadline = time.monotonic() + STARTUP_TIMEOUT
    while time.monotonic() < deadline:
        if process.poll() is not None:
            stderr = process.stderr.read().decode(errors="replace").strip() if process.stderr else ""
            detail = stderr.splitlines()[-1] if stderr else f"exited with code {process.returncode}"
            raise SSLError(f"Root CA server failed to start: {detail}")

        conn = http.client.HTTPConnection("127.0.0.1", port, timeout=0.3)
        try:
            conn.request("GET", "/")
            body = conn.getresponse().read()
        except (OSError, http.client.HTTPException):
            time.sleep(0.1)
            continue
        finally:
            conn.close()

        if body == cert_bytes:
            return

        process.kill()
        process.wait(timeout=2)
        raise SSLError(
            f"Port {port} is already in use by another process (it responded, "
            "but not with the expected root CA certificate). "
            "Pick a different port with --port."
        )

    process.kill()
    process.wait(timeout=2)
    raise SSLError(
        f"Root CA server did not start listening on port {port} within "
        f"{STARTUP_TIMEOUT:.0f}s. It may be blocked by a firewall."
    )


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
