"""Cross-platform /etc/hosts file management."""

import re
import subprocess

from ..exceptions import HostsFileError
from ..utils.logging import log_info, log_success
from ..utils.platform import Platform, get_hosts_file_path, get_platform


class HostsManager:
    """Manages entries in the system hosts file."""

    def __init__(self) -> None:
        """Initialize the hosts manager."""
        self.hosts_path = get_hosts_file_path()

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
                self.hosts_path.write_text(content)
            except PermissionError as e:
                raise HostsFileError(f"Failed to write hosts file. Run as Administrator: {e}")
        else:
            # Use sudo tee to write with privileges
            process = subprocess.run(
                ["sudo", "tee", str(self.hosts_path)],
                input=content.encode(),
                capture_output=True,
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
        pattern = rf"^{re.escape(ip)}\s+{re.escape(hostname)}"
        return bool(re.search(pattern, content, re.MULTILINE))

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

        content = self._read_hosts()
        lines = content.splitlines()

        # Filter out matching lines (both IPv4 and IPv6)
        pattern_v4 = rf"^{re.escape(ip)}\s+{re.escape(hostname)}$"
        pattern_v6 = rf"^::1\s+{re.escape(hostname)}$"

        new_lines = [
            line
            for line in lines
            if not re.match(pattern_v4, line.strip()) and not re.match(pattern_v6, line.strip())
        ]

        if len(new_lines) != len(lines):
            self._write_hosts("\n".join(new_lines) + "\n")
            log_success(f"Removed {hostname} from hosts file")
            return True

        log_info(f"{hostname} not found in hosts file")
        return False
