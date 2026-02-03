"""Configuration management for the .config JSON file."""

import json
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Optional


@dataclass
class ServerConfig:
    """Server configuration data structure."""

    status: str = "stopped"
    domains: list[str] = field(default_factory=list)


class ConfigManager:
    """Manages the .config JSON file for server state."""

    def __init__(self, config_path: Path) -> None:
        """Initialize the config manager.

        Args:
            config_path: Path to the .config file.
        """
        self.config_path = config_path
        self._config: Optional[ServerConfig] = None

    def load(self) -> ServerConfig:
        """Load config from file, creating default if needed.

        Returns:
            The loaded or created ServerConfig.
        """
        if not self.config_path.exists():
            self._config = ServerConfig()
            self.save()
        else:
            try:
                data = json.loads(self.config_path.read_text())
                self._config = ServerConfig(**data)
            except (json.JSONDecodeError, TypeError, KeyError):
                self._config = ServerConfig()
                self.save()
        return self._config

    def save(self) -> None:
        """Save current config to file."""
        if self._config is None:
            self._config = ServerConfig()
        self.config_path.write_text(json.dumps(asdict(self._config), indent=2))

    def reset(self) -> None:
        """Reset config to default state."""
        self._config = ServerConfig()
        self.save()

    @property
    def is_running(self) -> bool:
        """Check if the server is currently running.

        Returns:
            True if status is 'running', False otherwise.
        """
        return self.load().status == "running"

    def set_running(self, domains: list[str]) -> None:
        """Set the server status to running with the given domains.

        Args:
            domains: List of domain names being served.
        """
        self._config = ServerConfig(status="running", domains=domains)
        self.save()

    def set_stopped(self) -> None:
        """Set the server status to stopped."""
        self._config = ServerConfig(status="stopped", domains=[])
        self.save()

    def get_domains(self) -> list[str]:
        """Get the list of configured domains.

        Returns:
            List of domain names.
        """
        return self.load().domains

    def get_status(self) -> str:
        """Get the current server status.

        Returns:
            The status string ('running' or 'stopped').
        """
        return self.load().status
