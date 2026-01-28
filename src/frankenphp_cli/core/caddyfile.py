"""Caddyfile generation from templates."""

from pathlib import Path

from ..utils.logging import log_info, log_success


class CaddyfileGenerator:
    """Generates Caddyfile configurations from templates."""

    def __init__(self, project_dir: Path) -> None:
        """Initialize the Caddyfile generator.

        Args:
            project_dir: Path to the project directory.
        """
        self.project_dir = project_dir
        self.template_path = project_dir / "caddy" / "Caddyfile.template"
        self.custom_dir = project_dir / "caddy" / "sites" / "custom"

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
