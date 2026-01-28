# Python Package Conversion Plan - FrankenPHP CLI

## Overview

Convert 11 bash scripts to a proper Python 3 package (`frankenphp-cli`) with cross-platform support, better maintainability, and extensibility.

## Package Structure

```
frankenphp/
├── pyproject.toml
├── src/
│   └── frankenphp_cli/
│       ├── __init__.py
│       ├── __main__.py              # python -m frankenphp_cli
│       ├── cli.py                   # Typer CLI app
│       ├── exceptions.py            # Custom exceptions
│       ├── commands/
│       │   ├── __init__.py
│       │   ├── start.py
│       │   ├── stop.py
│       │   ├── restart.py
│       │   └── status.py
│       ├── core/
│       │   ├── __init__.py
│       │   ├── config.py            # JSON .config management
│       │   ├── environment.py       # .env loading
│       │   ├── docker_manager.py    # Docker SDK integration
│       │   ├── ssl_manager.py       # mkcert wrapper
│       │   ├── hosts_manager.py     # /etc/hosts management
│       │   ├── caddyfile.py         # Caddyfile generation
│       │   └── password_manager.py  # MariaDB password sync
│       └── utils/
│           ├── __init__.py
│           ├── platform.py          # OS detection
│           ├── logging.py           # Rich logging
│           └── validation.py        # Domain/file validation
└── tests/
    ├── conftest.py
    ├── unit/
    └── integration/
```

## Module Mapping

| Bash Script | Python Module |
|-------------|---------------|
| server.sh | cli.py, __main__.py |
| utils.sh | utils/* (split by concern) |
| actions/start.sh | commands/start.py |
| actions/stop.sh | commands/stop.py |
| actions/status.sh | commands/status.py |
| generate_ssl.sh | core/ssl_manager.py |
| generate_caddyfile.sh | core/caddyfile.py |
| manage_hosts.sh | core/hosts_manager.py |
| check_config.sh | core/config.py |
| check_files.sh | utils/validation.py |
| restart_server.sh | commands/restart.py |

## CLI Interface (Typer)

```bash
# Usage examples (after pip install -e .)
frankenphp start "domain.test" /path/to/project
frankenphp start "domain1.test domain2.test" /path --force-ssl
frankenphp stop
frankenphp restart --force-ssl
frankenphp status
```

## Key Dependencies

```toml
dependencies = [
    "typer>=0.9.0",      # CLI framework
    "rich>=13.0.0",      # Pretty output
    "docker>=7.0.0",     # Docker SDK
    "python-dotenv>=1.0.0",  # .env loading
]
```

## Cross-Platform Support

| Feature | macOS/Linux | Windows |
|---------|-------------|---------|
| Hosts file | /etc/hosts | C:\Windows\System32\drivers\etc\hosts |
| Privileges | sudo | Run as Administrator |
| mkcert | PATH lookup | where mkcert |
| File paths | pathlib.Path everywhere | pathlib.Path everywhere |

## Implementation Phases

### Phase 1: Foundation
- [ ] Create pyproject.toml
- [ ] Create package structure
- [ ] Implement utils/platform.py
- [ ] Implement utils/logging.py
- [ ] Implement utils/validation.py
- [ ] Implement exceptions.py

### Phase 2: Core Configuration
- [ ] Implement core/config.py
- [ ] Implement core/environment.py

### Phase 3: Service Managers
- [ ] Implement core/docker_manager.py
- [ ] Implement core/ssl_manager.py
- [ ] Implement core/hosts_manager.py
- [ ] Implement core/caddyfile.py
- [ ] Implement core/password_manager.py

### Phase 4: CLI Commands
- [ ] Implement cli.py
- [ ] Implement commands/start.py
- [ ] Implement commands/stop.py
- [ ] Implement commands/restart.py
- [ ] Implement commands/status.py
- [ ] Implement __main__.py

### Phase 5: Testing & Polish
- [ ] Write unit tests
- [ ] Write integration tests
- [ ] Add type hints (mypy)
- [ ] Run linter (ruff)

## Critical Files to Create

1. **pyproject.toml** - Package configuration with Hatch build system
2. **src/frankenphp_cli/cli.py** - Main Typer app with 4 commands
3. **src/frankenphp_cli/core/docker_manager.py** - Docker SDK + subprocess for compose
4. **src/frankenphp_cli/core/hosts_manager.py** - Cross-platform hosts file editing
5. **src/frankenphp_cli/commands/start.py** - Most complex, orchestrates all services

## Bash Scripts to Keep

The original bash scripts will remain alongside the Python package for backwards compatibility. Users can choose either approach.

## Detailed Implementation

### pyproject.toml

```toml
[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"

[project]
name = "frankenphp-cli"
version = "1.0.0"
description = "FrankenPHP Docker development environment manager"
readme = "README.md"
license = "MIT"
requires-python = ">=3.9"
dependencies = [
    "typer>=0.9.0",
    "rich>=13.0.0",
    "docker>=7.0.0",
    "python-dotenv>=1.0.0",
]

[project.optional-dependencies]
dev = [
    "pytest>=8.0.0",
    "pytest-cov>=4.0.0",
    "pytest-mock>=3.12.0",
    "mypy>=1.8.0",
    "ruff>=0.2.0",
]

[project.scripts]
frankenphp = "frankenphp_cli.cli:app"

[tool.hatch.build.targets.wheel]
packages = ["src/frankenphp_cli"]
```

### utils/platform.py

```python
import platform
from enum import Enum
from pathlib import Path

class Platform(Enum):
    MACOS = "darwin"
    LINUX = "linux"
    WINDOWS = "windows"

def get_platform() -> Platform:
    system = platform.system().lower()
    if system == "darwin":
        return Platform.MACOS
    elif system == "linux":
        return Platform.LINUX
    elif system == "windows":
        return Platform.WINDOWS
    raise RuntimeError(f"Unsupported platform: {system}")

def get_hosts_file_path() -> Path:
    if get_platform() == Platform.WINDOWS:
        return Path(r"C:\Windows\System32\drivers\etc\hosts")
    return Path("/etc/hosts")

def is_admin() -> bool:
    if get_platform() == Platform.WINDOWS:
        import ctypes
        return ctypes.windll.shell32.IsUserAnAdmin() != 0
    import os
    return os.geteuid() == 0
```

### utils/logging.py

```python
from rich.console import Console

console = Console()

def log_info(message: str):
    console.print(f"[blue]--[/blue] {message}")

def log_success(message: str):
    console.print(f"[green]--[/green] {message}")

def log_error(message: str):
    console.print(f"[red]ERROR:[/red] {message}", style="red")
```

### utils/validation.py

```python
import re
from pathlib import Path
from ..exceptions import ValidationError

DOMAIN_REGEX = re.compile(r'^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z]{2,})+$')

def validate_domain(domain: str) -> None:
    """Validate a domain name format (e.g. myapp.test)."""
    if not DOMAIN_REGEX.match(domain):
        raise ValidationError(f"Invalid domain name: {domain}")

def validate_directory(path: Path) -> None:
    """Validate that a directory exists."""
    if not path.is_dir():
        raise ValidationError(f"Directory does not exist: {path}")

def validate_file(path: Path) -> None:
    """Validate that a file exists."""
    if not path.is_file():
        raise ValidationError(f"File does not exist: {path}")

def require_command(cmd: str) -> None:
    """Validate that a command is available in PATH."""
    import shutil
    if not shutil.which(cmd):
        raise ValidationError(f"Required command not found: {cmd}")
```

### exceptions.py

```python
class FrankenPHPError(Exception):
    """Base exception for FrankenPHP CLI."""
    pass

class ConfigurationError(FrankenPHPError):
    """Raised when configuration is invalid or missing."""
    pass

class DockerError(FrankenPHPError):
    """Raised when Docker operations fail."""
    pass

class SSLError(FrankenPHPError):
    """Raised when SSL certificate operations fail."""
    pass

class HostsFileError(FrankenPHPError):
    """Raised when /etc/hosts operations fail."""
    pass

class ValidationError(FrankenPHPError):
    """Raised when validation fails (domain, path, etc.)."""
    pass

class ServerStateError(FrankenPHPError):
    """Raised when server is in unexpected state."""
    pass
```

### core/config.py

```python
import json
from pathlib import Path
from dataclasses import dataclass, field, asdict
from typing import List, Optional

@dataclass
class ServerConfig:
    status: str = "stopped"
    domains: List[str] = field(default_factory=list)

class ConfigManager:
    def __init__(self, config_path: Path):
        self.config_path = config_path
        self._config: Optional[ServerConfig] = None

    def load(self) -> ServerConfig:
        if not self.config_path.exists():
            self._config = ServerConfig()
            self.save()
        else:
            try:
                data = json.loads(self.config_path.read_text())
                self._config = ServerConfig(**data)
            except (json.JSONDecodeError, KeyError):
                self._config = ServerConfig()
                self.save()
        return self._config

    def save(self):
        self.config_path.write_text(json.dumps(asdict(self._config), indent=2))

    def reset(self):
        self._config = ServerConfig()
        self.save()

    @property
    def is_running(self) -> bool:
        return self.load().status == "running"

    def set_running(self, domains: List[str]):
        self._config = ServerConfig(status="running", domains=domains)
        self.save()

    def set_stopped(self):
        self._config = ServerConfig(status="stopped", domains=[])
        self.save()

    def get_domains(self) -> List[str]:
        return self.load().domains
```

### core/environment.py

```python
import os
import secrets
import string
from pathlib import Path
from typing import Dict, Optional
from ..exceptions import ConfigurationError

class EnvironmentManager:
    def __init__(self, env_path: Path, env_example_path: Path):
        self.env_path = env_path
        self.env_example_path = env_example_path
        self._env_vars: Dict[str, str] = {}

    def ensure_env_exists(self) -> bool:
        """Create .env from .env.example if it doesn't exist. Returns True if existed."""
        if not self.env_path.exists():
            if not self.env_example_path.exists():
                raise ConfigurationError(f"Missing {self.env_example_path}")
            self.env_path.write_text(self.env_example_path.read_text())
            self.env_path.chmod(0o660)
            return False
        return True

    def load(self) -> Dict[str, str]:
        """Load environment variables from .env file."""
        self._env_vars = {}
        for line in self.env_path.read_text().splitlines():
            line = line.strip()
            if line and not line.startswith('#') and '=' in line:
                key, value = line.split('=', 1)
                self._env_vars[key.strip()] = value.strip().strip('"\'')
        return self._env_vars

    def get(self, key: str, default: Optional[str] = None) -> Optional[str]:
        return self._env_vars.get(key, default)

    def require(self, key: str) -> str:
        value = self.get(key)
        if not value:
            raise ConfigurationError(f"Required environment variable '{key}' is not set")
        return value

    def set(self, key: str, value: str):
        """Update a value in the .env file."""
        lines = self.env_path.read_text().splitlines()
        found = False
        for i, line in enumerate(lines):
            if line.strip().startswith(f"{key}="):
                lines[i] = f"{key}={value}"
                found = True
                break
        if not found:
            lines.append(f"{key}={value}")
        self.env_path.write_text('\n'.join(lines) + '\n')
        self._env_vars[key] = value

    def generate_mariadb_password(self) -> str:
        """Generate a secure random password for MariaDB."""
        alphabet = string.ascii_letters + string.digits
        return ''.join(secrets.choice(alphabet) for _ in range(32))

    def is_production(self) -> bool:
        env = self.get("APP_ENV", "dev")
        return env in ("prod", "production")
```

### core/docker_manager.py

```python
import docker
import subprocess
import os
from pathlib import Path
from typing import Dict, List, Optional, Tuple
from ..exceptions import DockerError

class DockerManager:
    CONTAINERS = ["webserver-and-caddy", "franken_mariadb", "franken_phpmyadmin", "franken_redis"]

    def __init__(self, project_dir: Path):
        self.project_dir = project_dir
        self._client: Optional[docker.DockerClient] = None

    @property
    def client(self) -> docker.DockerClient:
        if self._client is None:
            try:
                self._client = docker.from_env()
            except docker.errors.DockerException as e:
                raise DockerError(f"Cannot connect to Docker: {e}")
        return self._client

    def build_image(self, custom_path: str, wwwgroup: str = "") -> None:
        """Build the custom FrankenPHP image."""
        build_args = {"CUSTOM_PATH": custom_path}
        if wwwgroup:
            build_args["WWWGROUP"] = wwwgroup
        try:
            self.client.images.build(
                path=str(self.project_dir),
                tag="custom-frankenphp:latest",
                buildargs=build_args,
                rm=True
            )
        except docker.errors.BuildError as e:
            raise DockerError(f"Failed to build image: {e}")

    def compose_up(self, env_vars: Dict[str, str], production: bool = False) -> None:
        """Start containers using docker-compose."""
        compose_file = "docker-compose-prod.yml" if production else "docker-compose.yml"
        result = subprocess.run(
            ["docker", "--log-level", "error", "compose", "-f", compose_file, "up", "-d"],
            cwd=self.project_dir,
            env={**os.environ, **env_vars},
            capture_output=True,
            text=True
        )
        if result.returncode != 0:
            raise DockerError(f"Failed to start containers: {result.stderr}")

    def compose_down(self, production: bool = False) -> None:
        """Stop containers using docker-compose."""
        compose_file = "docker-compose-prod.yml" if production else "docker-compose.yml"
        subprocess.run(
            ["docker", "--log-level", "error", "compose", "-f", compose_file, "down"],
            cwd=self.project_dir,
            capture_output=True
        )

    def restart_container(self, name: str) -> bool:
        """Restart a single container by name."""
        try:
            self.client.containers.get(name).restart()
            return True
        except docker.errors.NotFound:
            return False

    def restart_all(self) -> None:
        """Restart all FrankenPHP containers."""
        for name in self.CONTAINERS:
            self.restart_container(name)

    def get_container_status(self, name: str) -> Dict[str, str]:
        """Get status info for a container."""
        try:
            container = self.client.containers.get(name)
            health = "N/A"
            if container.attrs.get("State", {}).get("Health"):
                health = container.attrs["State"]["Health"].get("Status", "N/A")
            return {"status": container.status, "health": health}
        except docker.errors.NotFound:
            return {"status": "not found", "health": "N/A"}

    def exec_in_container(self, container_name: str, command: List[str]) -> Tuple[int, str]:
        """Execute a command inside a container."""
        try:
            container = self.client.containers.get(container_name)
            result = container.exec_run(command)
            return result.exit_code, result.output.decode()
        except Exception as e:
            return -1, str(e)
```

### core/ssl_manager.py

```python
import subprocess
import shutil
from pathlib import Path
from typing import List
from datetime import datetime, timedelta
from ..utils.logging import log_info, log_success
from ..utils.platform import get_platform, Platform
from ..exceptions import SSLError

class SSLManager:
    def __init__(self, certs_dir: Path):
        self.certs_dir = certs_dir
        self.certs_dir.mkdir(parents=True, exist_ok=True)

    def _find_mkcert(self) -> str:
        """Find mkcert executable."""
        mkcert = shutil.which("mkcert")
        if not mkcert:
            raise SSLError("mkcert not found. Please install mkcert first.")
        return mkcert

    def install_ca(self) -> None:
        """Install mkcert CA (requires sudo on Unix)."""
        mkcert = self._find_mkcert()
        log_info("Installing mkcert...")
        if get_platform() != Platform.WINDOWS:
            subprocess.run(["sudo", mkcert, "-install"], check=True)
        else:
            subprocess.run([mkcert, "-install"], check=True)

    def generate_localhost_cert(self) -> None:
        """Generate certificate for localhost."""
        mkcert = self._find_mkcert()
        cert_file = self.certs_dir / "localhost.pem"
        key_file = self.certs_dir / "localhost-key.pem"

        subprocess.run([
            mkcert,
            "-cert-file", str(cert_file),
            "-key-file", str(key_file),
            "localhost"
        ], check=True)

    def generate_domain_cert(self, domain: str, force: bool = False) -> bool:
        """Generate certificate for a domain. Returns True if generated."""
        mkcert = self._find_mkcert()
        cert_file = self.certs_dir / f"{domain}.pem"
        key_file = self.certs_dir / f"{domain}-key.pem"

        # Check if cert exists and is recent (less than 30 days old)
        if not force and cert_file.exists() and key_file.exists():
            cert_age = datetime.now() - datetime.fromtimestamp(cert_file.stat().st_mtime)
            if cert_age < timedelta(days=30):
                return False  # Skip, still valid

        subprocess.run([
            mkcert,
            "-cert-file", str(cert_file),
            "-key-file", str(key_file),
            domain
        ], check=True)
        return True

    def generate_all(self, domains: List[str], force: bool = False,
                     is_production: bool = False) -> None:
        """Generate all required certificates."""
        self.install_ca()
        self.generate_localhost_cert()

        if not is_production:
            log_info(f"Creating SSL certificates for: {', '.join(domains)}")
            for domain in domains:
                generated = self.generate_domain_cert(domain, force)
                if not generated:
                    log_info(f"Certificate for {domain} still valid, skipping")

            log_success("SSL certificates generated!")

        # Set permissions
        for cert_file in self.certs_dir.iterdir():
            cert_file.chmod(0o750)
```

### core/hosts_manager.py

```python
import re
import subprocess
from pathlib import Path
from ..utils.platform import get_platform, Platform, get_hosts_file_path
from ..utils.logging import log_info, log_success
from ..exceptions import HostsFileError

class HostsManager:
    def __init__(self):
        self.hosts_path = get_hosts_file_path()

    def _read_hosts(self) -> str:
        return self.hosts_path.read_text()

    def _write_hosts(self, content: str) -> None:
        """Write to hosts file (requires privileges)."""
        if get_platform() == Platform.WINDOWS:
            self.hosts_path.write_text(content)
        else:
            process = subprocess.run(
                ["sudo", "tee", str(self.hosts_path)],
                input=content.encode(),
                capture_output=True
            )
            if process.returncode != 0:
                raise HostsFileError(f"Failed to write hosts file: {process.stderr.decode()}")

    def has_entry(self, ip: str, hostname: str) -> bool:
        """Check if an entry exists in hosts file."""
        content = self._read_hosts()
        pattern = rf"^{re.escape(ip)}\s+{re.escape(hostname)}"
        return bool(re.search(pattern, content, re.MULTILINE))

    def add_entry(self, ip: str, hostname: str) -> bool:
        """Add an entry to the hosts file."""
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
        log_success(f"Added {ip}\t{hostname}")
        return True

    def remove_entry(self, ip: str, hostname: str) -> bool:
        """Remove an entry from the hosts file."""
        if hostname == "localhost":
            return False

        content = self._read_hosts()
        lines = content.splitlines()

        # Filter out matching lines
        pattern_v4 = rf"^{re.escape(ip)}\s+{re.escape(hostname)}$"
        pattern_v6 = rf"^::1\s+{re.escape(hostname)}$"

        new_lines = [
            line for line in lines
            if not re.match(pattern_v4, line) and not re.match(pattern_v6, line)
        ]

        if len(new_lines) != len(lines):
            self._write_hosts('\n'.join(new_lines) + '\n')
            log_success(f"Removed {hostname}")
            return True

        log_info(f"{hostname} not found in hosts file")
        return False
```

### core/caddyfile.py

```python
from pathlib import Path
from typing import List
from ..utils.logging import log_info, log_success

class CaddyfileGenerator:
    def __init__(self, project_dir: Path):
        self.project_dir = project_dir
        self.template_path = project_dir / "caddy" / "Caddyfile.template"
        self.custom_dir = project_dir / "caddy" / "sites" / "custom"

    def ensure_custom_dir(self) -> None:
        """Create custom Caddyfile directory if needed."""
        self.custom_dir.mkdir(parents=True, exist_ok=True)
        self.custom_dir.chmod(0o750)

    def generate(self, domains: List[str]) -> None:
        """Generate Caddyfile configs for all domains."""
        self.ensure_custom_dir()
        template = self.template_path.read_text()

        for domain in domains:
            simple_domain = domain.rsplit('.', 1)[0]  # Remove TLD
            caddyfile_path = self.custom_dir / f"{simple_domain}_Caddyfile"

            if not caddyfile_path.exists():
                log_info(f"Creating {simple_domain}_Caddyfile...")
                content = template.replace("full_domain", domain)
                content = content.replace("custom_domain", simple_domain)
                caddyfile_path.write_text(content)

        log_success("Caddyfile configurations generated")
```

### core/password_manager.py

```python
import time
from pathlib import Path
from typing import Optional, List
from ..utils.logging import log_info, log_success, log_error

class PasswordManager:
    def __init__(self, project_dir: Path, docker_manager):
        self.history_file = project_dir / ".db_password_history"
        self.docker = docker_manager
        self.container_name = "franken_mariadb"

    def _load_history(self) -> List[str]:
        """Load password history from file."""
        if not self.history_file.exists():
            return []
        return [p for p in self.history_file.read_text().splitlines() if p.strip()]

    def _save_to_history(self, password: str) -> None:
        """Add password to history if not already present."""
        history = self._load_history()
        if password not in history:
            with self.history_file.open('a') as f:
                f.write(password + '\n')
            self.history_file.chmod(0o660)

    def _test_password(self, password: str) -> bool:
        """Test if a password works for MariaDB root."""
        exit_code, _ = self.docker.exec_in_container(
            self.container_name,
            ["mariadb", "-u", "root", f"-p{password}", "-e", "SELECT 1"]
        )
        return exit_code == 0

    def find_working_password(self, new_password: str) -> Optional[str]:
        """Find a working password, trying new first then history."""
        # Try new password first
        if self._test_password(new_password):
            return new_password

        # Try history (newest first)
        for old_password in reversed(self._load_history()):
            if self._test_password(old_password):
                return old_password

        return None

    def sync_password(self, new_password: str, max_retries: int = 30) -> bool:
        """Sync the .env password to MariaDB."""
        # Wait for MariaDB to be ready
        working_password = None
        for _ in range(max_retries):
            working_password = self.find_working_password(new_password)
            if working_password:
                break
            time.sleep(1)

        if not working_password:
            log_info("MariaDB password sync skipped (could not connect)")
            return False

        # Already in sync
        if working_password == new_password:
            self._save_to_history(new_password)
            log_info("MariaDB password already in sync")
            return True

        # Update password
        sql = (
            f"ALTER USER 'root'@'%' IDENTIFIED BY '{new_password}'; "
            f"ALTER USER 'root'@'localhost' IDENTIFIED BY '{new_password}'; "
            f"FLUSH PRIVILEGES;"
        )
        exit_code, _ = self.docker.exec_in_container(
            self.container_name,
            ["mariadb", "-u", "root", f"-p{working_password}", "-e", sql]
        )

        if exit_code == 0:
            self._save_to_history(new_password)
            log_success("MariaDB password updated to match .env")
            return True

        log_error("Failed to sync MariaDB password")
        return False
```

### cli.py

```python
import typer
from pathlib import Path
from typing import Optional

app = typer.Typer(
    name="frankenphp",
    help="FrankenPHP Docker development environment manager",
    add_completion=False
)

@app.command()
def start(
    domains: str = typer.Argument(..., help="Space-separated domain names"),
    path: str = typer.Argument("/home", help="Custom path for project root"),
    force_ssl: bool = typer.Option(False, "--force-ssl", help="Force SSL regeneration")
):
    """Start the FrankenPHP development server."""
    from .commands.start import start_server
    domain_list = domains.split()
    start_server(domain_list, Path(path), force_ssl)

@app.command()
def stop():
    """Stop the FrankenPHP development server."""
    from .commands.stop import stop_server
    stop_server()

@app.command()
def restart(
    force_ssl: bool = typer.Option(False, "--force-ssl", help="Force SSL regeneration")
):
    """Restart the FrankenPHP development server."""
    from .commands.restart import restart_server
    restart_server(force_ssl)

@app.command()
def status():
    """Show the current server status."""
    from .commands.status import show_status
    show_status()

if __name__ == "__main__":
    app()
```

### commands/start.py

```python
from pathlib import Path
from typing import List
from ..core.config import ConfigManager
from ..core.environment import EnvironmentManager
from ..core.docker_manager import DockerManager
from ..core.ssl_manager import SSLManager
from ..core.hosts_manager import HostsManager
from ..core.caddyfile import CaddyfileGenerator
from ..core.password_manager import PasswordManager
from ..utils.logging import log_info, log_success, log_error
from ..utils.validation import validate_domain, validate_directory
from ..exceptions import ServerStateError

def get_project_dir() -> Path:
    """Get the project directory (where pyproject.toml is)."""
    return Path(__file__).parent.parent.parent.parent

def start_server(domains: List[str], custom_path: Path, force_ssl: bool) -> None:
    """Start the FrankenPHP server."""
    project_dir = get_project_dir()

    # Initialize managers
    config = ConfigManager(project_dir / ".config")
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")

    # Ensure .env exists
    if not env.ensure_env_exists():
        log_error("Created .env file. Please set USER and GROUP, then try again.")
        raise SystemExit(1)

    env.load()

    # Generate password if not set
    if not env.get("MARIADB_ROOT_PASSWORD"):
        password = env.generate_mariadb_password()
        env.set("MARIADB_ROOT_PASSWORD", password)
        log_info("MariaDB password generated and saved to .env")

    docker = DockerManager(project_dir)
    ssl = SSLManager(Path(env.get("CERTS_DIR", "./caddy/certs")))
    hosts = HostsManager()
    caddyfile = CaddyfileGenerator(project_dir)
    password = PasswordManager(project_dir, docker)

    # Check state
    if config.is_running:
        raise ServerStateError("The server is already running.")

    # Validate inputs
    validate_directory(custom_path)
    for domain in domains:
        validate_domain(domain)

    # Remove duplicates while preserving order
    domains = list(dict.fromkeys(domains))

    hosts_added = []
    try:
        # Generate SSL certificates
        ssl.generate_all(domains, force_ssl, env.is_production())

        # Add hosts entries
        for domain in domains:
            hosts.add_entry("127.0.0.1", domain)
            hosts_added.append(domain)

        # Generate Caddyfiles
        caddyfile.generate(domains)

        # Update config to running
        config.set_running(domains)

        # Build and start containers
        log_info("Starting web server...")
        docker.build_image(str(custom_path), env.get("WWWGROUP", ""))
        docker.compose_down(env.is_production())

        # Prepare environment variables
        expose = env.get("EXPOSE_SERVICES") == "true"
        env_vars = {
            "CUSTOM_PATH": str(custom_path),
            "DB_PORT": "3306:3306" if expose else "127.0.0.1:3306:3306",
            "PMA_PORT": "8080:80" if expose else "127.0.0.1:8080:80",
            "REDIS_PORT": "6379:6379" if expose else "127.0.0.1:6379:6379",
            "MARIADB_ROOT_PASSWORD": env.require("MARIADB_ROOT_PASSWORD"),
            "PWD": str(project_dir),
        }
        docker.compose_up(env_vars, env.is_production())

        # Sync database password
        password.sync_password(env.require("MARIADB_ROOT_PASSWORD"))

        log_success("Web server started!")

    except Exception as e:
        # Cleanup on failure
        log_error(f"An error occurred: {e}")
        for domain in hosts_added:
            hosts.remove_entry("127.0.0.1", domain)
        config.reset()
        raise
```

### commands/stop.py

```python
from pathlib import Path
from ..core.config import ConfigManager
from ..core.environment import EnvironmentManager
from ..core.docker_manager import DockerManager
from ..core.hosts_manager import HostsManager
from ..utils.logging import log_info, log_success
from ..exceptions import ServerStateError

def get_project_dir() -> Path:
    return Path(__file__).parent.parent.parent.parent

def stop_server() -> None:
    """Stop the FrankenPHP server."""
    project_dir = get_project_dir()

    config = ConfigManager(project_dir / ".config")
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    env.load()
    docker = DockerManager(project_dir)
    hosts = HostsManager()

    if not config.is_running:
        raise ServerStateError("The server is already stopped.")

    log_info("Stopping web server...")

    # Stop containers
    docker.compose_down(env.is_production())

    # Remove hosts entries
    for domain in config.get_domains():
        hosts.remove_entry("127.0.0.1", domain)

    # Reset config
    config.reset()

    log_success("Web server stopped!")
```

### commands/restart.py

```python
from pathlib import Path
from ..core.config import ConfigManager
from ..core.environment import EnvironmentManager
from ..core.docker_manager import DockerManager
from ..core.ssl_manager import SSLManager
from ..core.password_manager import PasswordManager
from ..utils.logging import log_info, log_success
from ..exceptions import ServerStateError

def get_project_dir() -> Path:
    return Path(__file__).parent.parent.parent.parent

def restart_server(force_ssl: bool) -> None:
    """Restart the FrankenPHP server."""
    project_dir = get_project_dir()

    config = ConfigManager(project_dir / ".config")
    env = EnvironmentManager(project_dir / ".env", project_dir / ".env.example")
    env.load()
    docker = DockerManager(project_dir)
    ssl = SSLManager(Path(env.get("CERTS_DIR", "./caddy/certs")))
    password = PasswordManager(project_dir, docker)

    if not config.is_running:
        raise ServerStateError("The server is not running.")

    log_info("Restarting web server...")

    # Regenerate SSL if requested
    domains = config.get_domains()
    ssl.generate_all(domains, force_ssl, env.is_production())

    # Restart containers
    docker.restart_all()

    # Sync password
    password.sync_password(env.require("MARIADB_ROOT_PASSWORD"))

    log_success("Web server restarted!")
```

### commands/status.py

```python
from pathlib import Path
from rich.console import Console
from rich.table import Table
from ..core.config import ConfigManager
from ..core.docker_manager import DockerManager

console = Console()

def get_project_dir() -> Path:
    return Path(__file__).parent.parent.parent.parent

def show_status() -> None:
    """Show the current server status."""
    project_dir = get_project_dir()

    config = ConfigManager(project_dir / ".config")
    docker = DockerManager(project_dir)

    server_config = config.load()

    console.print("\n[bold]=== FrankenPHP Server Status ===[/bold]\n")
    console.print(f"Status: [{'green' if server_config.status == 'running' else 'red'}]{server_config.status}[/]")

    if server_config.status == "running":
        console.print("\n[bold]Domains:[/bold]")
        for domain in server_config.domains:
            console.print(f"  - {domain}")

        console.print("\n[bold]Containers:[/bold]")
        table = Table(show_header=True, header_style="bold")
        table.add_column("Container", width=25)
        table.add_column("Status")
        table.add_column("Health")

        for container in DockerManager.CONTAINERS:
            status_info = docker.get_container_status(container)
            status = status_info["status"]
            health = status_info["health"]

            status_color = "green" if status == "running" else "red"
            health_color = "green" if health == "healthy" else ("yellow" if health == "N/A" else "red")

            table.add_row(
                container,
                f"[{status_color}]{status}[/]",
                f"[{health_color}]{health}[/]"
            )

        console.print(table)

    console.print()
```

### __main__.py

```python
from .cli import app

if __name__ == "__main__":
    app()
```

### __init__.py

```python
__version__ = "1.0.0"
__all__ = ["__version__"]
```

## Verification

After implementation:
1. Run `pip install -e .` to install in dev mode
2. Test `frankenphp start domain.test /home`
3. Test `frankenphp status`
4. Test `frankenphp stop`
5. Test `frankenphp restart`
6. Verify cross-platform by testing on macOS and Linux
7. Run `pytest` for unit/integration tests
8. Run `mypy src/` for type checking
9. Run `ruff check src/` for linting
