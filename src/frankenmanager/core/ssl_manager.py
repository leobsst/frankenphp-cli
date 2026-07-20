"""SSL certificate generation using mkcert."""

import shutil
import subprocess
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Optional

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import NameOID

from ..exceptions import SSLError
from ..utils.logging import log_info, log_success
from ..utils.platform import Platform, get_platform

# Fixed identity baked into every root CA this tool generates, in place of
# mkcert's own default ("mkcert development CA" / user@host). Not user
# configurable on purpose: every machine running frankenmanager should trust
# a CA that is recognizable as belonging to this tool, not to a random user.
CA_ORGANIZATION = "FrankenManager"
CA_ORGANIZATIONAL_UNIT = "Local Development CA"
CA_COMMON_NAME = "FrankenManager Local Development CA"


def get_ca_root(mkcert: str) -> Optional[Path]:
    """Resolve mkcert's CAROOT directory, or None if mkcert can't report it."""
    ca_check = subprocess.run([mkcert, "-CAROOT"], capture_output=True, text=True)
    if ca_check.returncode != 0:
        return None
    return Path(ca_check.stdout.strip())


def generate_custom_ca(ca_root: Path) -> None:
    """Generate a root CA under CAROOT with the fixed FrankenManager identity.

    mkcert only generates its own CA when rootCA.pem is absent from CAROOT
    (see its loadCA()), so writing ours there first makes a later
    `mkcert -install` register this CA in the system trust store instead of
    generating one with mkcert's default identity.
    """
    ca_root.mkdir(parents=True, exist_ok=True)

    key = rsa.generate_private_key(public_exponent=65537, key_size=3072)

    name = x509.Name(
        [
            x509.NameAttribute(NameOID.ORGANIZATION_NAME, CA_ORGANIZATION),
            x509.NameAttribute(NameOID.ORGANIZATIONAL_UNIT_NAME, CA_ORGANIZATIONAL_UNIT),
            x509.NameAttribute(NameOID.COMMON_NAME, CA_COMMON_NAME),
        ]
    )

    now = datetime.now(timezone.utc)
    cert = (
        x509.CertificateBuilder()
        .subject_name(name)
        .issuer_name(name)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now)
        .not_valid_after(now + timedelta(days=3650))
        .add_extension(x509.BasicConstraints(ca=True, path_length=0), critical=True)
        .add_extension(
            x509.KeyUsage(
                digital_signature=False,
                content_commitment=False,
                key_encipherment=False,
                data_encipherment=False,
                key_agreement=False,
                key_cert_sign=True,
                crl_sign=False,
                encipher_only=False,
                decipher_only=False,
            ),
            critical=True,
        )
        .add_extension(
            x509.SubjectKeyIdentifier.from_public_key(key.public_key()),
            critical=False,
        )
        .sign(key, hashes.SHA256())
    )

    key_path = ca_root / "rootCA-key.pem"
    key_path.write_bytes(
        key.private_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PrivateFormat.PKCS8,
            encryption_algorithm=serialization.NoEncryption(),
        )
    )
    key_path.chmod(0o600)

    cert_path = ca_root / "rootCA.pem"
    cert_path.write_bytes(cert.public_bytes(serialization.Encoding.PEM))
    cert_path.chmod(0o644)


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
        """Install the FrankenManager CA (requires elevated privileges on Unix)."""
        mkcert = self._find_mkcert()
        ca_root = get_ca_root(mkcert)

        if ca_root is not None:
            ca_cert = ca_root / "rootCA.pem"
            ca_key = ca_root / "rootCA-key.pem"

            # If both CA files already exist, it's already installed - skip.
            if ca_cert.exists() and ca_key.exists():
                self._sync_root_ca(ca_root)
                return

            generate_custom_ca(ca_root)

        ca_root = self._install_ca(mkcert)
        self._sync_root_ca(ca_root)

    def _install_ca(self, mkcert: str) -> Path:
        """Run `mkcert -install` and return the resulting CA root directory."""
        log_info("Installing FrankenManager local CA...")

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

        ca_check = subprocess.run([mkcert, "-CAROOT"], capture_output=True, text=True)
        return Path(ca_check.stdout.strip())

    def _sync_root_ca(self, ca_root: Optional[Path]) -> None:
        """Copy the mkcert root CA into certs_dir so containers can trust it.

        The certs directory is already bind-mounted into the FrankenPHP
        container as /certs; the container entrypoint installs this file
        into the container's system trust store on startup so that
        outgoing PHP/curl calls to other local domains (e.g. cross-project
        API calls) validate correctly, not just requests from the host
        browser.
        """
        if ca_root is None:
            return

        ca_cert = ca_root / "rootCA.pem"
        if not ca_cert.exists():
            return

        shutil.copy(ca_cert, self.certs_dir / "rootCA.pem")

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
