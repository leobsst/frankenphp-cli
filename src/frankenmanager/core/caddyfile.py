"""Caddyfile generation from templates."""

import shutil
from pathlib import Path
from typing import Optional

from ..utils.logging import log_info, log_success, log_warning


class CaddyfileGenerator:
    """Generates Caddyfile configurations from templates."""

    def __init__(self, project_dir: Path, sites_dir: Optional[Path] = None) -> None:
        """Initialize the Caddyfile generator.

        Args:
            project_dir: Path to the project directory.
            sites_dir: Optional custom directory for site configs.
        """
        self.project_dir = project_dir
        self.template_path = project_dir / "caddy" / "Caddyfile.template"
        self.custom_dir = sites_dir if sites_dir else (project_dir / "caddy" / "sites" / "custom")
        self.archive_dir = self.custom_dir.parent / "archive"

    def ensure_custom_dir(self) -> None:
        """Create the custom Caddyfile directory if it doesn't exist."""
        self.custom_dir.mkdir(parents=True, exist_ok=True)
        self.custom_dir.chmod(0o750)

    def generate(self, domains: list[str]) -> None:
        """Generate Caddyfile configurations for all domains.

        Args:
            domains: List of domain names.
        """
        self.ensure_custom_dir()

        if not self.template_path.exists():
            log_info(f"Template not found at {self.template_path}, skipping Caddyfile generation")
            return

        template = self.template_path.read_text()

        for domain in domains:
            # Extract simple domain (remove TLD)
            simple_domain = domain.rsplit(".", 1)[0]
            caddyfile_path = self.custom_dir / f"{simple_domain}_Caddyfile"

            if not caddyfile_path.exists():
                log_info(f"Creating {simple_domain}_Caddyfile...")

                # Replace placeholders in template
                content = template.replace("full_domain", domain)
                content = content.replace("custom_domain", simple_domain)

                caddyfile_path.write_text(content)

        log_success("Caddyfile configurations generated")

    def ensure_archive_dir(self) -> None:
        """Create the archive Caddyfile directory if it doesn't exist."""
        self.archive_dir.mkdir(parents=True, exist_ok=True)
        self.archive_dir.chmod(0o750)

    def archive(self, domains: list[str]) -> None:
        """Archive Caddyfile configurations for the given domains.

        Moves the Caddyfile from custom/ to archive/ directory.

        Args:
            domains: List of domain names to archive.
        """
        self.ensure_archive_dir()

        for domain in domains:
            # Extract simple domain (remove TLD)
            simple_domain = domain.rsplit(".", 1)[0]
            caddyfile_path = self.custom_dir / f"{simple_domain}_Caddyfile"

            if caddyfile_path.exists():
                archive_path = self.archive_dir / f"{simple_domain}_Caddyfile"

                # If archive already exists, remove it first
                if archive_path.exists():
                    archive_path.unlink()

                shutil.move(str(caddyfile_path), str(archive_path))
                log_info(f"Archived {simple_domain}_Caddyfile")
            else:
                log_warning(f"Caddyfile for {domain} not found, skipping archive")

    def list_archived(self) -> list[dict[str, str]]:
        """List all archived Caddyfile configurations.

        Returns:
            List of dictionaries with 'simple_domain' and 'full_domain' keys.
        """
        archived: list[dict[str, str]] = []

        if not self.archive_dir.exists():
            return archived

        for caddyfile_path in self.archive_dir.glob("*_Caddyfile"):
            simple_domain = caddyfile_path.stem.replace("_Caddyfile", "")
            # Try to extract full domain from Caddyfile content
            full_domain = self._extract_domain_from_caddyfile(caddyfile_path)
            archived.append(
                {
                    "simple_domain": simple_domain,
                    "full_domain": full_domain or f"{simple_domain}.test",
                }
            )

        return archived

    def _extract_domain_from_caddyfile(self, caddyfile_path: Path) -> Optional[str]:
        """Extract the full domain from a Caddyfile.

        Args:
            caddyfile_path: Path to the Caddyfile.

        Returns:
            The full domain name or None if not found.
        """
        try:
            content = caddyfile_path.read_text()
            # First line should be the domain (e.g., "myapp.test {")
            first_line = content.split("\n")[0].strip()
            if first_line.endswith("{"):
                return first_line[:-1].strip()
        except Exception:
            pass
        return None

    def restore(self, domains: list[str]) -> list[str]:
        """Restore Caddyfile configurations from archive.

        Moves the Caddyfile from archive/ to custom/ directory.

        Args:
            domains: List of domain names to restore (can be simple or full domain).

        Returns:
            List of full domain names that were restored.
        """
        self.ensure_custom_dir()
        restored_domains: list[str] = []

        for domain in domains:
            # Extract simple domain (remove TLD if present)
            simple_domain = domain.rsplit(".", 1)[0] if "." in domain else domain
            archive_path = self.archive_dir / f"{simple_domain}_Caddyfile"

            if archive_path.exists():
                # Extract full domain from archived file
                full_domain = self._extract_domain_from_caddyfile(archive_path)
                if not full_domain:
                    full_domain = f"{simple_domain}.test"

                custom_path = self.custom_dir / f"{simple_domain}_Caddyfile"

                # If custom already exists, remove it first
                if custom_path.exists():
                    custom_path.unlink()

                shutil.move(str(archive_path), str(custom_path))
                log_info(f"Restored {simple_domain}_Caddyfile")
                restored_domains.append(full_domain)
            else:
                log_warning(f"Archived Caddyfile for '{domain}' not found, skipping")

        return restored_domains
