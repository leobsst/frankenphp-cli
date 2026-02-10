"""SSL certificate generation using mkcert."""

import shutil
import subprocess
from datetime import datetime, timedelta
from pathlib import Path

from ..exceptions import SSLError
from ..utils.logging import log_info, log_success
from ..utils.platform import Platform, get_platform


class SSLManager:
    """Manages SSL certificate generation using mkcert."""

    def __init__(self, certs_dir: Path) -> None:
        """Initialize the SSL manager.

        Args:
            certs_dir: Directory to store certificates.
        """
        self.certs_dir = certs_dir
        self.certs_dir.mkdir(parents=True, exist_ok=True)

    def _find_mkcert(self) -> str:
        """Find the mkcert executable.

        Returns:
            Path to mkcert.

        Raises:
            SSLError: If mkcert is not found.
        """
        mkcert = shutil.which("mkcert")
        if not mkcert:
            raise SSLError("mkcert not found. Please install mkcert first.")
        return mkcert

    def install_ca(self) -> None:
        """Install the mkcert CA (requires elevated privileges on Unix)."""
        mkcert = self._find_mkcert()

        # Check if CA is already installed by checking for CA root files
        ca_check = subprocess.run(
            [mkcert, "-CAROOT"],
            capture_output=True,
            text=True,
        )

        if ca_check.returncode == 0:
            ca_root = Path(ca_check.stdout.strip())
            ca_cert = ca_root / "rootCA.pem"
            ca_key = ca_root / "rootCA-key.pem"

            # If both CA files exist, skip installation
            if ca_cert.exists() and ca_key.exists():
                return

        log_info("Installing mkcert CA...")

        if get_platform() != Platform.WINDOWS:
            # On Unix, mkcert -install requires sudo for system trust store
            result = subprocess.run(
                ["sudo", mkcert, "-install"],
                capture_output=True,
                text=True,
            )
        else:
            result = subprocess.run(
                [mkcert, "-install"],
                capture_output=True,
                text=True,
            )

        if result.returncode != 0:
            raise SSLError(f"Failed to install mkcert CA: {result.stderr}")

    def generate_localhost_cert(self) -> None:
        """Generate a certificate for localhost."""
        mkcert = self._find_mkcert()
        cert_file = self.certs_dir / "localhost.pem"
        key_file = self.certs_dir / "localhost-key.pem"

        result = subprocess.run(
            [
                mkcert,
                "-cert-file",
                str(cert_file),
                "-key-file",
                str(key_file),
                "localhost",
            ],
            capture_output=True,
            text=True,
        )

        if result.returncode != 0:
            raise SSLError(f"Failed to generate localhost certificate: {result.stderr}")

    def generate_domain_cert(self, domain: str, force: bool = False) -> bool:
        """Generate a certificate for a domain.

        Args:
            domain: The domain name.
            force: Force regeneration even if certificate is recent.

        Returns:
            True if certificate was generated, False if skipped.
        """
        mkcert = self._find_mkcert()
        cert_file = self.certs_dir / f"{domain}.pem"
        key_file = self.certs_dir / f"{domain}-key.pem"

        # Check if cert exists and is recent (less than 30 days old)
        if not force and cert_file.exists() and key_file.exists():
            cert_age = datetime.now() - datetime.fromtimestamp(cert_file.stat().st_mtime)
            if cert_age < timedelta(days=30):
                return False  # Skip, still valid

        result = subprocess.run(
            [
                mkcert,
                "-cert-file",
                str(cert_file),
                "-key-file",
                str(key_file),
                domain,
            ],
            capture_output=True,
            text=True,
        )

        if result.returncode != 0:
            raise SSLError(f"Failed to generate certificate for {domain}: {result.stderr}")

        return True

    def generate_all(
        self, domains: list[str], force: bool = False, is_production: bool = False
    ) -> None:
        """Generate all required certificates.

        Args:
            domains: List of domain names.
            force: Force regeneration of all certificates.
            is_production: Whether running in production mode.
        """
        self.install_ca()
        self.generate_localhost_cert()

        if not is_production:
            log_info(f"Creating SSL certificates for: {', '.join(domains)}")
            for domain in domains:
                generated = self.generate_domain_cert(domain, force)
                if not generated:
                    age = self._get_cert_age(domain)
                    log_info(f"Certificate for {domain} still valid ({age} days old), skipping")

            log_success("SSL certificates generated!")

        # Set permissions on all certificates
        for cert_file in self.certs_dir.iterdir():
            if cert_file.is_file():
                cert_file.chmod(0o750)

    def _get_cert_age(self, domain: str) -> int:
        """Get the age of a domain certificate in days.

        Args:
            domain: The domain name.

        Returns:
            Age in days, or -1 if certificate doesn't exist.
        """
        cert_file = self.certs_dir / f"{domain}.pem"
        if not cert_file.exists():
            return -1
        cert_age = datetime.now() - datetime.fromtimestamp(cert_file.stat().st_mtime)
        return cert_age.days
