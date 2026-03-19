"""Caddyfile generation from templates."""

import shutil
from pathlib import Path
from typing import Optional

from ..utils.logging import log_info, log_success, log_warning
from .php_versions import get_internal_ports


class CaddyfileGenerator:
    """Generates Caddyfile configurations from templates."""

    def __init__(self, project_dir: Path, sites_dir: Optional[Path] = None) -> None:
        """Initialize the Caddyfile generator.

        Args:
            project_dir: Path to the project directory.
            sites_dir: Optional custom base directory for site configs.
        """
        self.project_dir = project_dir
        self.template_path = project_dir / "caddy" / "Caddyfile.template"
        self.sites_base = sites_dir if sites_dir else (project_dir / "caddy" / "sites")
        self.custom_dir = self.sites_base / "custom"
        self.archive_dir = self.sites_base / "archive"

    def _version_dir(self, php_version: str) -> Path:
        """Get the site directory for a specific PHP version."""
        return self.sites_base / f"php-{php_version}"

    def _ensure_dir(self, path: Path) -> None:
        """Create a directory if it doesn't exist."""
        path.mkdir(parents=True, exist_ok=True)

    def generate_for_version(self, domains: list[str], php_version: str) -> None:
        """Generate Caddyfile configurations for domains into the version-specific directory.

        Args:
            domains: List of domain names.
            php_version: PHP version these domains belong to.
        """
        version_dir = self._version_dir(php_version)
        self._ensure_dir(version_dir)

        if not self.template_path.exists():
            log_info(f"Template not found at {self.template_path}, skipping Caddyfile generation")
            return

        template = self.template_path.read_text()

        for domain in domains:
            simple_domain = domain.rsplit(".", 1)[0]
            caddyfile_path = version_dir / f"{simple_domain}_Caddyfile"

            if not caddyfile_path.exists():
                log_info(f"Creating {simple_domain}_Caddyfile (PHP {php_version})...")

                content = template.replace("full_domain", domain)
                content = content.replace("custom_domain", simple_domain)

                caddyfile_path.write_text(content)

        log_success("Caddyfile configurations generated")

    def generate(self, domains: list[str]) -> None:
        """Generate Caddyfile configurations into the legacy custom directory.

        Args:
            domains: List of domain names.
        """
        self._ensure_dir(self.custom_dir)

        if not self.template_path.exists():
            log_info(f"Template not found at {self.template_path}, skipping Caddyfile generation")
            return

        template = self.template_path.read_text()

        for domain in domains:
            simple_domain = domain.rsplit(".", 1)[0]
            caddyfile_path = self.custom_dir / f"{simple_domain}_Caddyfile"

            if not caddyfile_path.exists():
                log_info(f"Creating {simple_domain}_Caddyfile...")

                content = template.replace("full_domain", domain)
                content = content.replace("custom_domain", simple_domain)

                caddyfile_path.write_text(content)

        log_success("Caddyfile configurations generated")

    def generate_all_for_versions(self, domains_versions: list[tuple[str, str]]) -> None:
        """Generate Caddyfile configs for all domains, grouped by PHP version.

        Args:
            domains_versions: List of (domain, php_version) tuples.
        """
        # Group domains by version
        by_version: dict[str, list[str]] = {}
        for domain, version in domains_versions:
            by_version.setdefault(version, []).append(domain)

        for version, domains in by_version.items():
            self.generate_for_version(domains, version)

    def move_to_version(self, domain: str, from_version: str, to_version: str) -> None:
        """Move a domain's Caddyfile from one version directory to another.

        Args:
            domain: The domain name.
            from_version: Source PHP version.
            to_version: Target PHP version.
        """
        simple_domain = domain.rsplit(".", 1)[0]
        filename = f"{simple_domain}_Caddyfile"

        src_dir = self._version_dir(from_version)
        dst_dir = self._version_dir(to_version)
        self._ensure_dir(dst_dir)

        src_path = src_dir / filename
        dst_path = dst_dir / filename

        if src_path.exists():
            if dst_path.exists():
                dst_path.unlink()
            shutil.move(str(src_path), str(dst_path))
            log_info(f"Moved {filename} from PHP {from_version} to PHP {to_version}")
        else:
            # File might be in legacy custom dir
            legacy_path = self.custom_dir / filename
            if legacy_path.exists():
                if dst_path.exists():
                    dst_path.unlink()
                shutil.move(str(legacy_path), str(dst_path))
                log_info(f"Moved {filename} to PHP {to_version}")
            else:
                log_warning(f"Caddyfile for {domain} not found")

    def archive_from_version(self, domains: list[str], php_version: str) -> None:
        """Archive Caddyfile configurations from a version directory.

        Args:
            domains: List of domain names to archive.
            php_version: PHP version directory to archive from.
        """
        self._ensure_dir(self.archive_dir)
        version_dir = self._version_dir(php_version)

        for domain in domains:
            simple_domain = domain.rsplit(".", 1)[0]
            filename = f"{simple_domain}_Caddyfile"

            # Try version dir first, then legacy custom dir
            caddyfile_path = version_dir / filename
            if not caddyfile_path.exists():
                caddyfile_path = self.custom_dir / filename

            if caddyfile_path.exists():
                archive_path = self.archive_dir / filename
                if archive_path.exists():
                    archive_path.unlink()
                shutil.move(str(caddyfile_path), str(archive_path))
                log_info(f"Archived {filename}")
            else:
                log_warning(f"Caddyfile for {domain} not found, skipping archive")

    def archive(self, domains: list[str]) -> None:
        """Archive Caddyfile configurations from any location.

        Searches in all version directories and the custom directory.

        Args:
            domains: List of domain names to archive.
        """
        self._ensure_dir(self.archive_dir)

        for domain in domains:
            simple_domain = domain.rsplit(".", 1)[0]
            filename = f"{simple_domain}_Caddyfile"
            archived = False

            # Search in all version directories
            if self.sites_base.exists():
                for version_dir in self.sites_base.glob("php-*"):
                    caddyfile_path = version_dir / filename
                    if caddyfile_path.exists():
                        archive_path = self.archive_dir / filename
                        if archive_path.exists():
                            archive_path.unlink()
                        shutil.move(str(caddyfile_path), str(archive_path))
                        log_info(f"Archived {filename}")
                        archived = True
                        break

            # Fall back to legacy custom dir
            if not archived:
                caddyfile_path = self.custom_dir / filename
                if caddyfile_path.exists():
                    archive_path = self.archive_dir / filename
                    if archive_path.exists():
                        archive_path.unlink()
                    shutil.move(str(caddyfile_path), str(archive_path))
                    log_info(f"Archived {filename}")
                else:
                    log_warning(f"Caddyfile for {domain} not found, skipping archive")

    def restore_to_version(self, domains: list[str], php_version: str) -> list[str]:
        """Restore Caddyfile configurations from archive into a version directory.

        Args:
            domains: List of domain names to restore.
            php_version: PHP version directory to restore into.

        Returns:
            List of full domain names that were restored.
        """
        version_dir = self._version_dir(php_version)
        self._ensure_dir(version_dir)
        restored_domains: list[str] = []

        for domain in domains:
            simple_domain = domain.rsplit(".", 1)[0] if "." in domain else domain
            archive_path = self.archive_dir / f"{simple_domain}_Caddyfile"

            if archive_path.exists():
                full_domain = self._extract_domain_from_caddyfile(archive_path)
                if not full_domain:
                    full_domain = f"{simple_domain}.test"

                target_path = version_dir / f"{simple_domain}_Caddyfile"
                if target_path.exists():
                    target_path.unlink()

                shutil.move(str(archive_path), str(target_path))
                log_info(f"Restored {simple_domain}_Caddyfile (PHP {php_version})")
                restored_domains.append(full_domain)
            else:
                log_warning(f"Archived Caddyfile for '{domain}' not found, skipping")

        return restored_domains

    def restore(self, domains: list[str]) -> list[str]:
        """Restore Caddyfile configurations from archive to legacy custom dir.

        Args:
            domains: List of domain names to restore.

        Returns:
            List of full domain names that were restored.
        """
        self._ensure_dir(self.custom_dir)
        restored_domains: list[str] = []

        for domain in domains:
            simple_domain = domain.rsplit(".", 1)[0] if "." in domain else domain
            archive_path = self.archive_dir / f"{simple_domain}_Caddyfile"

            if archive_path.exists():
                full_domain = self._extract_domain_from_caddyfile(archive_path)
                if not full_domain:
                    full_domain = f"{simple_domain}.test"

                custom_path = self.custom_dir / f"{simple_domain}_Caddyfile"
                if custom_path.exists():
                    custom_path.unlink()

                shutil.move(str(archive_path), str(custom_path))
                log_info(f"Restored {simple_domain}_Caddyfile")
                restored_domains.append(full_domain)
            else:
                log_warning(f"Archived Caddyfile for '{domain}' not found, skipping")

        return restored_domains

    def generate_main_caddyfile(
        self,
        domains_versions: list[tuple[str, str]],
        caddy_dir: Path,
        production: bool = False,
    ) -> None:
        """Generate the main Caddyfile that reverse-proxies to FrankenPHP containers.

        All containers use host network, so 127.0.0.1 is used in both dev and prod.

        Args:
            domains_versions: List of (domain, php_version) tuples.
            caddy_dir: Path to the caddy directory.
            production: Kept for API compatibility (unused, all modes use host network).
        """
        lines: list[str] = [
            "{",
            "\thttp_port {$WEB_HTTP_PORT:80}",
            "\thttps_port {$WEB_HTTPS_PORT:443}",
            "}",
            "",
        ]

        for domain, php_version in domains_versions:
            _, https_port = get_internal_ports(php_version)
            simple_domain = domain.rsplit(".", 1)[0]

            # All containers use host network, so 127.0.0.1 works in both dev and prod
            upstream = f"127.0.0.1:{https_port}"

            lines.extend(
                [
                    f"{domain} {{",
                    f"\ttls /certs/{domain}.pem /certs/{domain}-key.pem",
                    f"\treverse_proxy https://{upstream} {{",
                    "\t\ttransport http {",
                    "\t\t\ttls_insecure_skip_verify",
                    "\t\t}",
                    f'\t\theader_up Host "{domain}"',
                    "\t}",
                    "\tlog {",
                    f"\t\toutput file /var/log/caddy/{simple_domain}_access.log {{",
                    "\t\t\troll_size 10mb",
                    "\t\t\troll_keep 5",
                    "\t\t\troll_keep_for 720h",
                    "\t\t}",
                    "\t\tformat console",
                    "\t}",
                    "}",
                    "",
                ]
            )

        main_caddyfile = caddy_dir / "Caddyfile"
        main_caddyfile.write_text("\n".join(lines) + "\n")

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
            first_line = content.split("\n")[0].strip()
            if first_line.endswith("{"):
                return first_line[:-1].strip()
        except Exception:
            pass
        return None
