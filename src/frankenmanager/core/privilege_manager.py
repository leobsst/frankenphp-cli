"""Cross-platform privilege management for elevated operations.

This module provides a secure way to configure passwordless sudo access
for specific operations like modifying /etc/hosts, without requiring
sudo for every command.
"""

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
from pathlib import Path
from typing import Union

from ..exceptions import ConfigurationError
from ..utils.logging import log_error, log_info, log_success, log_warning
from ..utils.platform import Platform, get_hosts_file_path, get_platform, is_admin


class PrivilegeManager:
    """Manages privilege escalation configuration across platforms."""

    # Name of the helper script that will be allowed passwordless sudo
    HELPER_SCRIPT_NAME = "frankenmanager-hosts-helper"

    # Sudoers file name in /etc/sudoers.d/
    SUDOERS_FILE_NAME = "frankenmanager"

    # Windows PowerShell helper script name
    WINDOWS_HELPER_NAME = "FrankenManagerHostsHelper.ps1"

    def __init__(self) -> None:
        """Initialize the privilege manager."""
        self.platform = get_platform()
        self.hosts_path = get_hosts_file_path()

    def get_helper_script_path(self) -> Path:
        """Get the path where the helper script should be installed.

        Returns:
            Path to the helper script location.
        """
        if self.platform == Platform.WINDOWS:
            # On Windows, store in user's AppData
            appdata = os.environ.get("LOCALAPPDATA", os.path.expanduser("~"))
            return Path(appdata) / "FrankenManager" / self.WINDOWS_HELPER_NAME

        # Use /usr/local/bin for the helper script
        return Path("/usr/local/bin") / self.HELPER_SCRIPT_NAME

    def get_sudoers_path(self) -> Path:
        """Get the path to the sudoers configuration file.

        Returns:
            Path to the sudoers.d file.
        """
        return Path("/etc/sudoers.d") / self.SUDOERS_FILE_NAME

    def is_configured(self) -> bool:
        """Check if privilege escalation is already configured.

        Returns:
            True if configured, False otherwise.
        """
        if self.platform == Platform.WINDOWS:
            return self._is_windows_configured()

        # On Unix, check for helper script and sudoers entry
        helper_path = self.get_helper_script_path()
        sudoers_path = self.get_sudoers_path()

        return helper_path.exists() and sudoers_path.exists()

    def _is_windows_configured(self) -> bool:
        """Check Windows privilege configuration.

        Returns:
            True if configured for Windows.
        """
        # Check if helper script exists and we can write to hosts
        helper_path = self.get_helper_script_path()
        if not helper_path.exists():
            return False

        try:
            with open(self.hosts_path, "a", encoding="utf-8"):
                pass
            return True
        except PermissionError:
            return False

    def get_helper_script_content(self) -> str:
        """Generate the content of the helper script.

        Returns:
            Shell script content for the hosts helper.
        """
        return f"""#!/bin/bash
# FrankenManager Hosts Helper
# This script is allowed to run with passwordless sudo to modify /etc/hosts
# It only allows adding/removing entries matching 127.0.0.1 or ::1

set -euo pipefail

HOSTS_FILE="{self.hosts_path}"
ACTION="${{1:-}}"
IP="${{2:-}}"
HOSTNAME="${{3:-}}"

# Validate action
if [[ "$ACTION" != "add" && "$ACTION" != "remove" ]]; then
    echo "Error: Action must be 'add' or 'remove'" >&2
    exit 1
fi

# Validate IP (only allow localhost IPs)
if [[ "$IP" != "127.0.0.1" && "$IP" != "::1" ]]; then
    echo "Error: Only 127.0.0.1 and ::1 are allowed" >&2
    exit 1
fi

# Validate hostname (basic validation)
if [[ -z "$HOSTNAME" || "$HOSTNAME" == "localhost" ]]; then
    echo "Error: Invalid hostname" >&2
    exit 1
fi

# Validate hostname format (alphanumeric, dots, hyphens only)
if ! [[ "$HOSTNAME" =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]$ ]]; then
    echo "Error: Hostname contains invalid characters" >&2
    exit 1
fi

if [[ "$ACTION" == "add" ]]; then
    # Check if entry already exists (use -E for extended regex on macOS)
    # Allow trailing whitespace in the pattern
    if grep -Eq "^$IP[[:space:]]+$HOSTNAME[[:space:]]*$" "$HOSTS_FILE" 2>/dev/null; then
        echo "Entry already exists"
        exit 0
    fi

    # Add entry
    echo -e "$IP\\t$HOSTNAME" >> "$HOSTS_FILE"
    echo "Added $IP $HOSTNAME"

elif [[ "$ACTION" == "remove" ]]; then
    # Remove entry using sed (use -E for extended regex on macOS)
    # Allow trailing whitespace in the pattern
    if grep -Eq "^$IP[[:space:]]+$HOSTNAME[[:space:]]*$" "$HOSTS_FILE" 2>/dev/null; then
        sed -E -i '' "/^$IP[[:space:]]+$HOSTNAME[[:space:]]*$/d" "$HOSTS_FILE"
        echo "Removed $IP $HOSTNAME"
        exit 0
    else
        echo "Entry not found"
        exit 1
    fi
fi
"""

    def get_windows_helper_script_content(self) -> str:
        """Generate the Windows PowerShell helper script content.

        Returns:
            PowerShell script content for the hosts helper.
        """
        return f"""# FrankenManager Hosts Helper for Windows
# This script modifies the hosts file

param(
    [Parameter(Mandatory=$true)]
    [ValidateSet("add", "remove")]
    [string]$Action,

    [Parameter(Mandatory=$true)]
    [ValidateSet("127.0.0.1", "::1")]
    [string]$IP,

    [Parameter(Mandatory=$true)]
    [ValidatePattern("^[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]$")]
    [string]$Hostname
)

$hostsPath = "{self.hosts_path}"

if ($Hostname -eq "localhost") {{
    Write-Error "Cannot modify localhost entry"
    exit 1
}}

$content = Get-Content $hostsPath -Raw
$pattern = "^$($IP -replace '\\.','\\.')[\\s]+$([regex]::Escape($Hostname))$"

if ($Action -eq "add") {{
    if ($content -match $pattern) {{
        Write-Output "Entry already exists"
        exit 0
    }}

    Add-Content -Path $hostsPath -Value "$IP`t$Hostname"
    Write-Output "Added $IP $Hostname"
}}
elseif ($Action -eq "remove") {{
    $lines = Get-Content $hostsPath
    $newLines = $lines | Where-Object {{ $_ -notmatch $pattern }}

    if ($lines.Count -ne $newLines.Count) {{
        $newLines | Set-Content $hostsPath
        Write-Output "Removed $IP $Hostname"
    }} else {{
        Write-Output "Entry not found"
    }}
}}
"""

    def get_sudoers_content(self) -> str:
        """Generate the content for the sudoers file.

        Returns:
            Sudoers configuration content.
        """
        import pwd

        helper_path = self.get_helper_script_path()
        # When running under sudo, os.getuid() returns 0 (root).
        # Use SUDO_USER to get the real user who invoked sudo.
        sudo_user = os.environ.get("SUDO_USER")
        if sudo_user:
            username = sudo_user
        else:
            username = pwd.getpwuid(os.getuid()).pw_name

        return f"""# FrankenManager - Allow passwordless sudo for hosts helper
# This file was automatically generated by frankenmanager setup
# To remove: sudo rm {self.get_sudoers_path()}

{username} ALL=(ALL) NOPASSWD: {helper_path}
"""

    def setup_unix_privileges(self) -> bool:
        """Set up privilege escalation on Unix (macOS/Linux).

        Returns:
            True if setup was successful.

        Raises:
            ConfigurationError: If setup fails.
        """
        if not is_admin():
            raise ConfigurationError(
                "Setup requires root privileges. Please run with: sudo frankenmanager setup"
            )

        helper_path = self.get_helper_script_path()
        sudoers_path = self.get_sudoers_path()

        # Create helper script
        log_info(f"Creating helper script at {helper_path}")
        helper_content = self.get_helper_script_content()
        helper_path.write_text(helper_content)
        helper_path.chmod(0o755)

        # Set ownership to root
        os.chown(helper_path, 0, 0)

        # Validate sudoers content before installing
        sudoers_content = self.get_sudoers_content()

        # Write to temp file and validate with visudo
        with tempfile.NamedTemporaryFile(mode="w", suffix=".sudoers", delete=False) as f:
            f.write(sudoers_content)
            temp_path = f.name

        try:
            # Validate syntax
            result = subprocess.run(
                ["visudo", "-c", "-f", temp_path],
                capture_output=True,
                text=True,
                check=False,
            )
            if result.returncode != 0:
                raise ConfigurationError(f"Invalid sudoers syntax: {result.stderr}")

            # Install sudoers file
            log_info(f"Installing sudoers configuration at {sudoers_path}")
            shutil.move(temp_path, str(sudoers_path))
            sudoers_path.chmod(0o440)
            os.chown(sudoers_path, 0, 0)

        except Exception as e:
            # Clean up temp file
            Path(temp_path).unlink(missing_ok=True)
            raise ConfigurationError(f"Failed to configure sudoers: {e}") from e

        log_success("Privilege configuration complete!")
        log_info("You can now run frankenmanager start/stop without sudo prompts")
        return True

    def setup_windows_privileges(self) -> bool:
        """Set up privilege handling on Windows.

        Creates a PowerShell helper script and provides instructions.

        Returns:
            True if setup was successful.
        """
        if not is_admin():
            log_error("Setup requires Administrator privileges.")
            log_info("Please run this command in an Administrator terminal:")
            log_info("  1. Right-click on PowerShell or Command Prompt")
            log_info("  2. Select 'Run as administrator'")
            log_info("  3. Run: frankenmanager setup")
            raise ConfigurationError("Administrator privileges required")

        helper_path = self.get_helper_script_path()

        # Create helper directory
        helper_path.parent.mkdir(parents=True, exist_ok=True)

        # Create PowerShell helper script
        log_info(f"Creating helper script at {helper_path}")
        helper_content = self.get_windows_helper_script_content()
        helper_path.write_text(helper_content, encoding="utf-8")

        log_success("Windows helper script installed!")
        log_info("")
        log_info("The hosts file can now be modified when running as Administrator.")
        log_info("")
        log_info("For passwordless operation, you have two options:")
        log_info("")
        log_info("Option 1: Always run FrankenManager as Administrator")
        log_info("  Right-click your terminal and select 'Run as administrator'")
        log_info("")
        log_info("Option 2: Grant write access to hosts file (less secure)")
        log_info(f"  1. Right-click on {self.hosts_path}")
        log_info("  2. Go to Properties -> Security -> Edit")
        log_info("  3. Add your user with 'Modify' permission")
        log_info("")
        log_warning("Option 2 reduces security - use with caution on shared machines")

        return True

    def setup(self) -> bool:
        """Set up privilege escalation for the current platform.

        Returns:
            True if setup was successful.
        """
        if self.platform == Platform.WINDOWS:
            return self.setup_windows_privileges()
        else:
            return self.setup_unix_privileges()

    def remove_unix_privileges(self) -> bool:
        """Remove privilege configuration on Unix.

        Returns:
            True if removal was successful.
        """
        if not is_admin():
            raise ConfigurationError(
                "Removal requires root privileges. "
                "Please run with: sudo frankenmanager setup --remove"
            )

        helper_path = self.get_helper_script_path()
        sudoers_path = self.get_sudoers_path()

        removed = False

        if helper_path.exists():
            log_info(f"Removing helper script: {helper_path}")
            helper_path.unlink()
            removed = True

        if sudoers_path.exists():
            log_info(f"Removing sudoers configuration: {sudoers_path}")
            sudoers_path.unlink()
            removed = True

        if removed:
            log_success("Privilege configuration removed")
        else:
            log_info("No privilege configuration found")

        return True

    def remove_windows_privileges(self) -> bool:
        """Remove privilege configuration on Windows.

        Returns:
            True if removal was successful.
        """
        helper_path = self.get_helper_script_path()

        if helper_path.exists():
            log_info(f"Removing helper script: {helper_path}")
            helper_path.unlink()
            # Remove parent directory if empty
            try:
                helper_path.parent.rmdir()
            except OSError:
                pass
            log_success("Helper script removed")
        else:
            log_info("No helper script found")

        log_info("")
        log_info("To restore default hosts file permissions:")
        log_info(f"  1. Right-click on {self.hosts_path}")
        log_info("  2. Go to Properties -> Security")
        log_info("  3. Remove any custom user permissions")

        return True

    def remove(self) -> bool:
        """Remove privilege configuration for the current platform.

        Returns:
            True if removal was successful.
        """
        if self.platform == Platform.WINDOWS:
            return self.remove_windows_privileges()
        else:
            return self.remove_unix_privileges()

    def execute_hosts_helper(self, action: str, ip: str, hostname: str) -> tuple[bool, str]:
        """Execute the hosts helper script with sudo (if configured).

        Args:
            action: Either 'add' or 'remove'.
            ip: IP address (127.0.0.1 or ::1).
            hostname: The hostname to add/remove.

        Returns:
            Tuple of (success, message).
        """
        helper_path = self.get_helper_script_path()

        if not helper_path.exists():
            return False, "Helper script not installed. Run 'frankenmanager setup' first."

        try:
            if self.platform == Platform.WINDOWS:
                # Use PowerShell to execute the helper
                result = subprocess.run(
                    [
                        "powershell",
                        "-ExecutionPolicy",
                        "Bypass",
                        "-File",
                        str(helper_path),
                        "-Action",
                        action,
                        "-IP",
                        ip,
                        "-Hostname",
                        hostname,
                    ],
                    capture_output=True,
                    text=True,
                    timeout=10,
                    check=False,
                )
            else:
                result = subprocess.run(
                    ["sudo", str(helper_path), action, ip, hostname],
                    capture_output=True,
                    text=True,
                    timeout=10,
                    check=False,
                )

            if result.returncode == 0:
                return True, result.stdout.strip()
            else:
                return False, result.stderr.strip() or result.stdout.strip()
        except subprocess.TimeoutExpired:
            return False, "Helper script timed out"
        except Exception as e:
            return False, str(e)

    def can_use_helper(self) -> bool:
        """Check if the helper script can be used without password.

        Returns:
            True if helper is configured and works without password.
        """
        if not self.is_configured():
            return False

        if self.platform == Platform.WINDOWS:
            # On Windows, check if we can write to hosts file
            try:
                with open(self.hosts_path, "a", encoding="utf-8"):
                    pass
                return True
            except PermissionError:
                return False

        # On Unix, test if sudo works without password for our helper
        helper_path = self.get_helper_script_path()
        try:
            result = subprocess.run(
                ["sudo", "-n", str(helper_path), "--help"],
                capture_output=True,
                timeout=5,
                check=False,
            )
            # -n flag makes sudo print "password is required" to stderr
            # if password would be needed
            if b"password is required" in result.stderr:
                return False
            return True
        except subprocess.TimeoutExpired:
            return False
        except Exception:
            return False

    def get_status(self) -> dict[str, Union[bool, str]]:
        """Get the current privilege configuration status.

        Returns:
            Dictionary with status information.
        """
        status: dict[str, Union[bool, str]] = {
            "platform": self.platform.value,
            "configured": self.is_configured(),
            "is_admin": is_admin(),
            "helper_installed": self.get_helper_script_path().exists(),
        }

        if self.platform != Platform.WINDOWS:
            status["sudoers_configured"] = self.get_sudoers_path().exists()

        status["passwordless_available"] = self.can_use_helper()

        return status


class MkcertInstaller:
    """Cross-platform mkcert installation helper."""

    def __init__(self) -> None:
        """Initialize the mkcert installer."""
        self.platform = get_platform()

    def is_installed(self) -> bool:
        """Check if mkcert is installed.

        Returns:
            True if mkcert is available in PATH.
        """
        return shutil.which("mkcert") is not None

    def get_install_instructions(self) -> str:
        """Get platform-specific installation instructions.

        Returns:
            Instructions for installing mkcert.
        """
        if self.platform == Platform.MACOS:
            return """To install mkcert on macOS:

    brew install mkcert
    brew install nss  # if using Firefox

Or download from: https://github.com/FiloSottile/mkcert/releases"""

        elif self.platform == Platform.LINUX:
            return """To install mkcert on Linux:

    # Ubuntu/Debian
    sudo apt install libnss3-tools
    # Download from GitHub releases and install to /usr/local/bin
    # See: https://github.com/FiloSottile/mkcert/releases

    # Arch Linux
    sudo pacman -S mkcert

    # Fedora
    sudo dnf install mkcert"""

        else:  # Windows
            return """To install mkcert on Windows:

    # Using Chocolatey
    choco install mkcert

    # Using Scoop
    scoop install mkcert

    # Or download from: https://github.com/FiloSottile/mkcert/releases"""

    def install(self) -> bool:
        """Attempt to install mkcert automatically.

        Returns:
            True if installation was successful.
        """
        if self.is_installed():
            log_info("mkcert is already installed")
            return True

        log_info("Attempting to install mkcert...")

        try:
            if self.platform == Platform.MACOS:
                return self._install_macos()
            elif self.platform == Platform.LINUX:
                return self._install_linux()
            else:
                return self._install_windows()
        except Exception as e:
            log_error(f"Failed to install mkcert: {e}")
            log_info("")
            log_info(self.get_install_instructions())
            return False

    def _install_macos(self) -> bool:
        """Install mkcert on macOS using Homebrew."""
        # Check if Homebrew is installed
        if not shutil.which("brew"):
            log_warning("Homebrew not found. Please install mkcert manually:")
            log_info(self.get_install_instructions())
            return False

        log_info("Installing mkcert via Homebrew...")
        result = subprocess.run(
            ["brew", "install", "mkcert"],
            capture_output=True,
            text=True,
            check=False,
        )

        if result.returncode != 0:
            log_error(f"Homebrew installation failed: {result.stderr}")
            return False

        log_success("mkcert installed successfully!")
        return True

    def _install_linux(self) -> bool:
        """Install mkcert on Linux."""
        import platform
        import urllib.request

        # Determine architecture
        machine = platform.machine().lower()
        if machine in ("x86_64", "amd64"):
            arch = "amd64"
        elif machine in ("aarch64", "arm64"):
            arch = "arm64"
        else:
            log_warning(f"Unsupported architecture: {machine}")
            log_info(self.get_install_instructions())
            return False

        # Download mkcert binary
        version = "v1.4.4"
        url = (
            f"https://github.com/FiloSottile/mkcert/releases/download/"
            f"{version}/mkcert-{version}-linux-{arch}"
        )

        log_info(f"Downloading mkcert from {url}...")

        temp_path = Path(tempfile.gettempdir()) / "mkcert"
        try:
            urllib.request.urlretrieve(url, temp_path)
        except Exception as e:
            log_error(f"Download failed: {e}")
            return False

        # Make executable and move to /usr/local/bin
        temp_path.chmod(0o755)

        if is_admin():
            dest = Path("/usr/local/bin/mkcert")
            shutil.move(str(temp_path), str(dest))
            dest.chmod(0o755)
        else:
            # Move to user's local bin
            local_bin = Path.home() / ".local" / "bin"
            local_bin.mkdir(parents=True, exist_ok=True)
            dest = local_bin / "mkcert"
            shutil.move(str(temp_path), str(dest))
            dest.chmod(0o755)
            log_warning(f"Installed to {dest}")
            log_info("Make sure ~/.local/bin is in your PATH")

        log_success("mkcert installed successfully!")
        return True

    def _install_windows(self) -> bool:
        """Install mkcert on Windows."""
        # Try Chocolatey first
        if shutil.which("choco"):
            log_info("Installing mkcert via Chocolatey...")
            result = subprocess.run(
                ["choco", "install", "mkcert", "-y"],
                capture_output=True,
                text=True,
                check=False,
            )
            if result.returncode == 0:
                log_success("mkcert installed successfully!")
                return True

        # Try Scoop
        if shutil.which("scoop"):
            log_info("Installing mkcert via Scoop...")
            result = subprocess.run(
                ["scoop", "install", "mkcert"],
                capture_output=True,
                text=True,
                check=False,
            )
            if result.returncode == 0:
                log_success("mkcert installed successfully!")
                return True

        log_warning("Neither Chocolatey nor Scoop found.")
        log_info(self.get_install_instructions())
        return False

    def setup_ca(self) -> bool:
        """Install the mkcert Certificate Authority.

        Returns:
            True if successful.
        """
        if not self.is_installed():
            log_error("mkcert is not installed")
            return False

        log_info("Installing mkcert CA (may require admin password)...")

        mkcert = shutil.which("mkcert")
        if not mkcert:
            return False

        if self.platform != Platform.WINDOWS and not is_admin():
            # On Unix, mkcert -install may need sudo for system trust store
            result = subprocess.run(
                ["sudo", mkcert, "-install"],
                capture_output=True,
                text=True,
                check=False,
            )
        else:
            result = subprocess.run(
                [mkcert, "-install"],
                capture_output=True,
                text=True,
                check=False,
            )

        if result.returncode != 0:
            log_error(f"Failed to install CA: {result.stderr}")
            return False

        log_success("mkcert CA installed successfully!")
        return True
