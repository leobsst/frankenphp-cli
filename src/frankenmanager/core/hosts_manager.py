"""Cross-platform /etc/hosts file management."""

from __future__ import annotations

import re
import subprocess
from typing import TYPE_CHECKING

from ..exceptions import HostsFileError
from ..utils.logging import log_info, log_success
from ..utils.platform import Platform, get_hosts_file_path, get_platform

if TYPE_CHECKING:
    from .privilege_manager import PrivilegeManager


class HostsManager:
    """Manages entries in the system hosts file."""

    def __init__(self, use_privilege_helper: bool = True) -> None:
        """Initialize the hosts manager.

        Args:
            use_privilege_helper: If True, try to use the privilege helper
                                  for passwordless sudo operations.
        """
        self.hosts_path = get_hosts_file_path()
        self._use_privilege_helper = use_privilege_helper
        self._privilege_manager: PrivilegeManager | None = None

    @property
    def privilege_manager(self) -> PrivilegeManager:
        """Lazy-load the privilege manager."""
        if self._privilege_manager is None:
            from .privilege_manager import PrivilegeManager

            self._privilege_manager = PrivilegeManager()
        return self._privilege_manager

    def _can_use_helper(self) -> bool:
        """Check if we can use the privilege helper.

        Returns:
            True if helper is available and configured.
        """
        if not self._use_privilege_helper:
            return False
        try:
            return self.privilege_manager.can_use_helper()
        except Exception:
            return False

    def _read_hosts(self) -> str:
        """Read the hosts file content.

        Returns:
            The hosts file content.
        """
        return self.hosts_path.read_text()

    def _write_hosts(self, content: str) -> None:
        """Write content to the hosts file (requires privileges).

        Args:
            content: The new hosts file content.

        Raises:
            HostsFileError: If writing fails.
        """
        if get_platform() == Platform.WINDOWS:
            try:
                self.hosts_path.write_text(content, encoding="utf-8")
            except PermissionError as e:
                raise HostsFileError(
                    f"Failed to write hosts file. Run as Administrator: {e}"
                ) from e
        else:
            # Use sudo tee to write with privileges
            process = subprocess.run(
                ["sudo", "tee", str(self.hosts_path)],
                input=content.encode(),
                capture_output=True,
                check=False,
            )
            if process.returncode != 0:
                raise HostsFileError(f"Failed to write hosts file: {process.stderr.decode()}")

    def has_entry(self, ip: str, hostname: str) -> bool:
        """Check if an entry exists in the hosts file.

        Args:
            ip: IP address.
            hostname: Hostname.

        Returns:
            True if entry exists, False otherwise.
        """
        content = self._read_hosts()
        pattern = rf"^{re.escape(ip)}\s+{re.escape(hostname)}\s*$"
        return bool(re.search(pattern, content, re.MULTILINE))

    def _add_entry_via_helper(self, ip: str, hostname: str) -> bool:
        """Add entry using the privilege helper script.

        Args:
            ip: IP address.
            hostname: Hostname.

        Returns:
            True if successful, False otherwise.
        """
        success, message = self.privilege_manager.execute_hosts_helper("add", ip, hostname)
        if success:
            log_success(f"Added {ip}\t{hostname} to hosts file")
            # Also add IPv6 if IPv4
            if ip == "127.0.0.1":
                success_v6, _ = self.privilege_manager.execute_hosts_helper("add", "::1", hostname)
                if success_v6:
                    log_success(f"Added ::1\t{hostname} to hosts file")
        return success

    def _remove_entry_via_helper(self, ip: str, hostname: str) -> bool:
        """Remove entry using the privilege helper script.

        Args:
            ip: IP address.
            hostname: Hostname.

        Returns:
            True if successful, False otherwise.
        """
        success, message = self.privilege_manager.execute_hosts_helper("remove", ip, hostname)
        if success:
            log_success(f"Removed {ip}\t{hostname} from hosts file")
            # Also remove IPv6 if IPv4
            if ip == "127.0.0.1":
                self.privilege_manager.execute_hosts_helper("remove", "::1", hostname)
        return success

    def add_entry(self, ip: str, hostname: str) -> bool:
        """Add an entry to the hosts file.

        Args:
            ip: IP address (e.g., '127.0.0.1').
            hostname: Hostname (e.g., 'myapp.test').

        Returns:
            True if entry was added, False if it already existed.
        """
        if hostname == "localhost":
            return False

        if self.has_entry(ip, hostname):
            log_info(f"{ip}\t{hostname} already exists")
            return False

        # Try to use privilege helper if available (passwordless sudo)
        if self._can_use_helper():
            return self._add_entry_via_helper(ip, hostname)

        # Fall back to direct write with sudo prompt
        content = self._read_hosts()
        new_entry = f"{ip}\t{hostname}\n"

        # Also add IPv6 entry if IPv4
        if ip == "127.0.0.1":
            new_entry += f"::1\t\t{hostname}\n"

        self._write_hosts(content + new_entry)
        log_success(f"Added {ip}\t{hostname} to hosts file")
        return True

    def remove_entry(self, ip: str, hostname: str) -> bool:
        """Remove an entry from the hosts file.

        Args:
            ip: IP address.
            hostname: Hostname.

        Returns:
            True if entry was removed, False if it wasn't found.
        """
        if hostname == "localhost":
            return False

        # Check if entry exists first
        if not self.has_entry(ip, hostname):
            log_info(f"{hostname} not found in hosts file")
            return False

        # Try to use privilege helper if available (passwordless sudo)
        if self._can_use_helper():
            return self._remove_entry_via_helper(ip, hostname)

        # Fall back to direct write with sudo prompt
        content = self._read_hosts()
        lines = content.splitlines()

        # Filter out matching lines (both IPv4 and IPv6)
        # Allow trailing whitespace in the pattern
        pattern_v4 = rf"^{re.escape(ip)}\s+{re.escape(hostname)}\s*$"
        pattern_v6 = rf"^::1\s+{re.escape(hostname)}\s*$"

        new_lines = [
            line
            for line in lines
            if not re.match(pattern_v4, line) and not re.match(pattern_v6, line)
        ]

        if len(new_lines) != len(lines):
            self._write_hosts("\n".join(new_lines) + "\n")
            log_success(f"Removed {hostname} from hosts file")
            return True

        return False
