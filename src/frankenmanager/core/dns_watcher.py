"""Keep the LAN DNS records pointed at this host's current LAN IP.

The DNS container can't detect the host's IP itself: on macOS and Windows,
Docker runs in a VM and containers only ever see the VM's address. So a small
detached process on the host polls the LAN IP and, when it changes (Wi-Fi to
Ethernet, new DHCP lease...), rewrites the records file. CoreDNS reloads that
file on its own within seconds: nothing has to be restarted.

Only runs while the server runs, with DNS_SERVER=true and DNS_IP unset (a
fixed DNS_IP never changes, so there is nothing to watch).
"""

import os
import signal
import subprocess
import sys
import time
from datetime import datetime
from pathlib import Path
from typing import TYPE_CHECKING, Optional

from . import dns_server
from .ca_server import get_lan_ip

if TYPE_CHECKING:
    from .environment import EnvironmentManager

COMMAND = "_watch-dns-internal"
POLL_INTERVAL = 5.0


def _state_file(project_dir: Path) -> Path:
    return project_dir / ".dns_watcher.pid"


def _log_file(project_dir: Path) -> Path:
    return project_dir / ".dns_watcher.log"


def is_needed(env: "EnvironmentManager") -> bool:
    """Whether the records follow the auto-detected LAN IP."""
    return dns_server.is_enabled(env) and not env.get("DNS_IP")


def update(project_dir: Path, env: "EnvironmentManager") -> bool:
    """Restart the watcher if the current settings need it, stop it otherwise.

    Returns:
        True if the watcher is running afterwards.
    """
    if not is_needed(env):
        stop(project_dir)
        return False
    start(project_dir)
    return True


def get_running_pid(project_dir: Path) -> Optional[int]:
    """PID of the running watcher, if any. Cleans up a stale state file."""
    state_file = _state_file(project_dir)
    if not state_file.exists():
        return None
    try:
        pid = int(state_file.read_text().strip())
        os.kill(pid, 0)
    except (ValueError, OSError):
        state_file.unlink(missing_ok=True)
        return None
    return pid


def start(project_dir: Path) -> int:
    """(Re)start the watcher as a detached background process.

    Returns:
        The PID of the spawned watcher.
    """
    stop(project_dir)

    # --no-update-check is a top-level option and must precede the subcommand
    if getattr(sys, "frozen", False):
        cmd = [sys.executable, "--no-update-check", COMMAND]
    else:
        cmd = [sys.executable, "-m", "frankenmanager", "--no-update-check", COMMAND]

    # stderr goes to a file, not a pipe: the launcher exits right away and the
    # watcher would otherwise die on its first write to a closed pipe
    with open(_log_file(project_dir), "wb") as log:
        process = subprocess.Popen(
            cmd,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=log,
            start_new_session=True,
        )
    _state_file(project_dir).write_text(str(process.pid))
    return process.pid


def stop(project_dir: Path) -> bool:
    """Stop the watcher. Returns False if it wasn't running."""
    pid = get_running_pid(project_dir)
    if pid is None:
        return False
    try:
        os.kill(pid, signal.SIGTERM)
    except OSError:
        pass
    _state_file(project_dir).unlink(missing_ok=True)
    return True


def check_once(project_dir: Path, last_ip: Optional[str]) -> Optional[str]:
    """Rewrite the records if the LAN IP changed since `last_ip`.

    A loopback address means no network right now (e.g. mid-switch between
    Wi-Fi and Ethernet): the records are left as they are until a real LAN
    IP comes back, rather than pointing every device at itself.

    Returns:
        The IP the records now point to (`last_ip` when left unchanged).
    """
    ip = get_lan_ip()
    if ip == last_ip or ip.startswith("127."):
        return last_ip

    # Imported here: the database and env are re-read on every change so the
    # records include domains added since the watcher started
    from .database import DatabaseManager  # noqa: PLC0415
    from .environment import EnvironmentManager  # noqa: PLC0415

    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    env.load()
    if not is_needed(env):
        return last_ip
    dns_server.sync(project_dir, env, DatabaseManager(project_dir / "db.sqlite"))
    stamp = datetime.now().isoformat(timespec="seconds")
    print(f"{stamp} LAN IP changed: {last_ip} -> {ip}, DNS records updated", file=sys.stderr)
    sys.stderr.flush()
    return ip


def run(project_dir: Path) -> None:
    """Blocking poll loop. Meant to run as its own subprocess (see `start`)."""
    last_ip: Optional[str] = None
    while True:
        try:
            last_ip = check_once(project_dir, last_ip)
        except Exception as e:  # keep watching: a transient error must not stop it
            print(f"DNS watcher error: {e}", file=sys.stderr)
            sys.stderr.flush()
        time.sleep(POLL_INTERVAL)
