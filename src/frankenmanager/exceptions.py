"""Custom exceptions for FrankenPHP Manager."""


class FrankenPHPError(Exception):
    """Base exception for FrankenPHP Manager."""

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
