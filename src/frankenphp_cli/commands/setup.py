"""Setup command for configuring privilege escalation and dependencies."""

from rich.console import Console
from rich.table import Table

from ..core.privilege_manager import MkcertInstaller, PrivilegeManager
from ..core.resources import get_data_dir_info
from ..exceptions import ConfigurationError
from ..utils.logging import log_error, log_info, log_success, log_warning

console = Console()


def setup_privileges(
    remove: bool = False,
    show_status: bool = False,
    install_mkcert: bool = False,
) -> None:
    """Configure privilege escalation for passwordless hosts file modifications.

    Args:
        remove: If True, remove the privilege configuration.
        show_status: If True, only show current status.
        install_mkcert: If True, also install mkcert.
    """
    manager = PrivilegeManager()
    mkcert = MkcertInstaller()

    if show_status:
        _show_status(manager, mkcert)
        return

    if remove:
        _remove_privileges(manager)
    else:
        _setup_all(manager, mkcert, install_mkcert)


def _show_status(manager: PrivilegeManager, mkcert: MkcertInstaller) -> None:
    """Display the current configuration status."""
    status = manager.get_status()
    data_info = get_data_dir_info()

    # Data directory table
    data_table = Table(title="Data Directory")
    data_table.add_column("Setting", style="cyan")
    data_table.add_column("Value", style="green")

    data_table.add_row("Location", data_info["data_dir"])
    data_table.add_row("Mode", data_info["mode"].capitalize())
    data_table.add_row("Initialized", "Yes" if data_info["initialized"] == "True" else "No")

    console.print(data_table)
    console.print("")

    # Privilege status table
    priv_table = Table(title="Privilege Configuration Status")
    priv_table.add_column("Setting", style="cyan")
    priv_table.add_column("Value", style="green")

    priv_table.add_row("Platform", str(status.get("platform", "unknown")))
    priv_table.add_row("Running as Admin/Root", "Yes" if status.get("is_admin") else "No")
    priv_table.add_row("Helper Script Installed", "Yes" if status.get("helper_installed") else "No")

    if "sudoers_configured" in status:
        priv_table.add_row(
            "Sudoers Configured", "Yes" if status.get("sudoers_configured") else "No"
        )

    priv_table.add_row(
        "Passwordless Available", "Yes" if status.get("passwordless_available") else "No"
    )

    console.print(priv_table)
    console.print("")

    # Dependencies table
    dep_table = Table(title="Dependencies Status")
    dep_table.add_column("Dependency", style="cyan")
    dep_table.add_column("Status", style="green")

    mkcert_status = "Installed" if mkcert.is_installed() else "Not installed"
    dep_table.add_row("mkcert", mkcert_status)

    console.print(dep_table)

    if not status.get("configured") or not mkcert.is_installed():
        log_info("")
        log_info("To complete setup, run:")
        log_info("  sudo frankenmanager setup")


def _setup_all(
    manager: PrivilegeManager,
    mkcert: MkcertInstaller,
    install_mkcert: bool,
) -> None:
    """Set up privilege escalation and optionally install mkcert."""
    log_info("Setting up FrankenManager...")
    log_info("")

    # Step 1: Privilege escalation
    if manager.is_configured():
        log_info("Privilege escalation is already configured.")
    else:
        try:
            manager.setup()
        except ConfigurationError as e:
            log_error(str(e))
            raise SystemExit(1) from e

    log_info("")

    # Step 2: Check/install mkcert
    if mkcert.is_installed():
        log_info("mkcert is already installed.")
        # Install CA if not already done
        mkcert.setup_ca()
    elif install_mkcert:
        log_info("Installing mkcert...")
        if mkcert.install():
            mkcert.setup_ca()
        else:
            log_warning("mkcert installation failed. You can install it manually:")
            log_info(mkcert.get_install_instructions())
    else:
        log_info("mkcert is not installed.")
        log_info("To install mkcert automatically, run:")
        log_info("  sudo frankenmanager setup --install-mkcert")
        log_info("")
        log_info("Or install manually:")
        log_info(mkcert.get_install_instructions())

    log_info("")
    log_success("Setup complete!")


def _remove_privileges(manager: PrivilegeManager) -> None:
    """Remove privilege escalation configuration."""
    log_info("Removing privilege escalation configuration...")
    log_info("")

    if not manager.is_configured():
        log_info("Privilege escalation is not configured. Nothing to remove.")
        return

    try:
        manager.remove()
    except ConfigurationError as e:
        log_error(str(e))
        raise SystemExit(1) from e
