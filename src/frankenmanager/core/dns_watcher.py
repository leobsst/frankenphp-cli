"""Keep the LAN DNS records pointed at this host's current LAN IP.

The DNS container can't detect the host's IP itself: on macOS and Windows,
Docker runs in a VM and containers only ever see the VM's address. So a small
process on the host polls the LAN IP and, when it changes (Wi-Fi to
Ethernet, new DHCP lease...), rewrites the records file. CoreDNS reloads that
file on its own within seconds: nothing has to be restarted.

Only runs while the server runs, with DNS_SERVER=true and DNS_IP unset (a
fixed DNS_IP never changes, so there is nothing to watch).

The process is handed to the OS service manager when there is one (a
LaunchAgent on macOS, a systemd user unit on Linux): it then outlives the
terminal, comes back after a reboot or a login like the containers do
(`restart: always`), and is relaunched if it crashes. Elsewhere (Windows, or
no systemd user instance) it falls back to a detached background process.

On macOS, release builds ship FrankenManager.app inside the bundle (see
scripts/build.py): the LaunchAgent runs through it, so that Login Items show
"FrankenManager" rather than the name on the code signing certificate.
"""

import os
import plistlib
import shutil
import signal
import subprocess
import sys
import time
from datetime import datetime
from pathlib import Path
from types import FrameType
from typing import TYPE_CHECKING, Literal, Optional

from . import dns_server
from .ca_server import get_lan_ip

if TYPE_CHECKING:
    from .environment import EnvironmentManager

COMMAND = "_watch-dns-internal"
POLL_INTERVAL = 5.0

LAUNCHD_LABEL = "dev.frankenmanager.dns-watcher"
SYSTEMD_UNIT = "frankenmanager-dns-watcher.service"

# Keep in sync with scripts/build.py
MACOS_APP_NAME = "FrankenManager.app"
MACOS_APP_BUNDLE_ID = "dev.frankenmanager"
MACOS_APP_EXECUTABLE = "frankenmanager-agent"
LSREGISTER = Path(
    "/System/Library/Frameworks/CoreServices.framework/Frameworks/"
    "LaunchServices.framework/Support/lsregister"
)

Backend = Literal["launchd", "systemd"]


def _state_file(project_dir: Path) -> Path:
    return project_dir / ".dns_watcher.pid"


def _log_file(project_dir: Path) -> Path:
    return project_dir / ".dns_watcher.log"


def _launchd_plist() -> Path:
    return Path.home() / "Library" / "LaunchAgents" / f"{LAUNCHD_LABEL}.plist"


def _systemd_unit_file() -> Path:
    config_home = os.environ.get("XDG_CONFIG_HOME") or str(Path.home() / ".config")
    return Path(config_home) / "systemd" / "user" / SYSTEMD_UNIT


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


def _command() -> list[str]:
    # --no-update-check is a top-level option and must precede the subcommand
    if getattr(sys, "frozen", False):
        return [sys.executable, "--no-update-check", COMMAND]
    return [sys.executable, "-m", "frankenmanager", "--no-update-check", COMMAND]


def _service_backend() -> Optional[Backend]:
    """The OS service manager able to keep the watcher alive, if any."""
    if sys.platform == "darwin" and shutil.which("launchctl"):
        return "launchd"
    if sys.platform.startswith("linux") and shutil.which("systemctl"):
        return "systemd"
    return None


def start(project_dir: Path) -> None:
    """(Re)start the watcher, as an OS service when possible."""
    stop(project_dir)

    backend = _service_backend()
    if backend == "launchd" and _start_launchd(project_dir):
        return
    if backend == "systemd" and _start_systemd(project_dir):
        return
    _start_detached(project_dir)


def stop(project_dir: Path) -> bool:
    """Stop the watcher and remove its OS service. Returns False if it wasn't running."""
    stopped = _stop_launchd() | _stop_systemd()

    # Detached watcher (fallback, or started by a version without services)
    pid = get_running_pid(project_dir)
    if pid is not None:
        try:
            os.kill(pid, signal.SIGTERM)
        except OSError:
            pass
        stopped = True
    _state_file(project_dir).unlink(missing_ok=True)
    return stopped


# --- Detached process (fallback) ---------------------------------------------


def _start_detached(project_dir: Path) -> None:
    # stderr goes to a file, not a pipe: the launcher exits right away and the
    # watcher would otherwise die on its first write to a closed pipe
    with open(_log_file(project_dir), "wb") as log:
        process = subprocess.Popen(
            _command(),
            cwd=project_dir,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=log,
            start_new_session=True,
        )
    # Written here too (the watcher writes its own) so `status` sees it at once
    _state_file(project_dir).write_text(str(process.pid))


# --- macOS: LaunchAgent ------------------------------------------------------


def _macos_app() -> Optional[Path]:
    """FrankenManager.app shipped next to the running binary, if any.

    Absent from source installs and from builds predating the app.
    """
    if not getattr(sys, "frozen", False):
        return None
    app = Path(sys.executable).resolve().parent / MACOS_APP_NAME
    return app if (app / "Contents" / "MacOS" / MACOS_APP_EXECUTABLE).is_file() else None


def build_launchd_plist(project_dir: Path, command: list[str], app: Optional[Path] = None) -> bytes:
    """LaunchAgent starting the watcher at login and relaunching it if it dies.

    With `app`, the watcher is started through the app's launcher and the
    agent is tied to the app's bundle identifier: Login Items then show the
    app's name instead of the name on the signing certificate.
    """
    if app is not None:
        command = [str(app / "Contents" / "MacOS" / MACOS_APP_EXECUTABLE), *command[1:]]
    agent: dict[str, object] = {
        "Label": LAUNCHD_LABEL,
        "ProgramArguments": command,
        "WorkingDirectory": str(project_dir),
        "RunAtLoad": True,
        "KeepAlive": True,
        "ProcessType": "Background",
        "StandardOutPath": "/dev/null",
        "StandardErrorPath": str(_log_file(project_dir)),
    }
    if app is not None:
        agent["AssociatedBundleIdentifiers"] = [MACOS_APP_BUNDLE_ID]
    return plistlib.dumps(agent)


def _launchd_domain() -> str:
    return f"gui/{os.getuid()}"


def _launchctl(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(["launchctl", *args], capture_output=True, text=True)


def _register_app(app: Path) -> None:
    """Make the app known to Launch Services, outside /Applications.

    macOS resolves AssociatedBundleIdentifiers through Launch Services: an
    app it has never seen would leave the agent shown under the signer's
    name. Best effort: the agent works either way.
    """
    if LSREGISTER.is_file():
        subprocess.run([str(LSREGISTER), "-f", str(app)], capture_output=True)


def _start_launchd(project_dir: Path) -> bool:
    plist = _launchd_plist()
    app = _macos_app()
    if app is not None:
        _register_app(app)
    try:
        plist.parent.mkdir(parents=True, exist_ok=True)
        plist.write_bytes(build_launchd_plist(project_dir, _command(), app))
    except OSError:
        return False

    # Right after a bootout, launchd may still be tearing the old job down
    # and refuse the bootstrap for a moment
    for _ in range(5):
        if _launchctl("bootstrap", _launchd_domain(), str(plist)).returncode == 0:
            return True
        time.sleep(0.5)
    plist.unlink(missing_ok=True)
    return False


def _stop_launchd() -> bool:
    plist = _launchd_plist()
    if not plist.exists():
        return False
    if shutil.which("launchctl"):
        _launchctl("bootout", f"{_launchd_domain()}/{LAUNCHD_LABEL}")
    plist.unlink(missing_ok=True)
    return True


# --- Linux: systemd user unit ------------------------------------------------


def _systemd_quote(arg: str) -> str:
    return '"' + arg.replace("\\", "\\\\").replace('"', '\\"') + '"'


def build_systemd_unit(project_dir: Path, command: list[str]) -> str:
    """User unit starting the watcher at login and relaunching it if it dies."""
    return (
        "[Unit]\n"
        "Description=FrankenManager LAN DNS watcher\n"
        "\n"
        "[Service]\n"
        f"ExecStart={' '.join(_systemd_quote(arg) for arg in command)}\n"
        f"WorkingDirectory={project_dir}\n"
        "StandardOutput=null\n"
        f"StandardError=append:{_log_file(project_dir)}\n"
        "Restart=always\n"
        "RestartSec=5\n"
        "\n"
        "[Install]\n"
        "WantedBy=default.target\n"
    )


def _systemctl(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(["systemctl", "--user", *args], capture_output=True, text=True)


def _start_systemd(project_dir: Path) -> bool:
    unit = _systemd_unit_file()
    try:
        unit.parent.mkdir(parents=True, exist_ok=True)
        unit.write_text(build_systemd_unit(project_dir, _command()))
    except OSError:
        return False

    # Fails without a user systemd instance (WSL without systemd, containers...)
    if (
        _systemctl("daemon-reload").returncode == 0
        and _systemctl("enable", "--now", SYSTEMD_UNIT).returncode == 0
    ):
        return True
    unit.unlink(missing_ok=True)
    _systemctl("daemon-reload")
    return False


def _stop_systemd() -> bool:
    unit = _systemd_unit_file()
    if not unit.exists():
        return False
    if shutil.which("systemctl"):
        _systemctl("disable", "--now", SYSTEMD_UNIT)
    unit.unlink(missing_ok=True)
    if shutil.which("systemctl"):
        _systemctl("daemon-reload")
    return True


# --- Watcher process ---------------------------------------------------------


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


def _register_pid(project_dir: Path) -> None:
    """Record this process's PID, and forget it when stopped.

    The watcher writes it itself: when launched by launchd or systemd (at
    login, or after a crash), nobody else knows its PID.
    """
    state_file = _state_file(project_dir)
    pid = str(os.getpid())
    state_file.write_text(pid)

    def _on_sigterm(_signum: int, _frame: Optional[FrameType]) -> None:
        try:
            if state_file.read_text().strip() == pid:
                state_file.unlink()
        except OSError:
            pass
        sys.exit(0)

    signal.signal(signal.SIGTERM, _on_sigterm)


def run(project_dir: Path) -> None:
    """Blocking poll loop. Meant to run as its own process (see `start`)."""
    _register_pid(project_dir)
    last_ip: Optional[str] = None
    while True:
        try:
            last_ip = check_once(project_dir, last_ip)
        except Exception as e:  # keep watching: a transient error must not stop it
            print(f"DNS watcher error: {e}", file=sys.stderr)
            sys.stderr.flush()
        time.sleep(POLL_INTERVAL)
