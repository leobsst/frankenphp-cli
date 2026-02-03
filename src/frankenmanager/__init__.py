"""FrankenPHP Manager - Docker development environment manager."""

try:
    from ._version import __version__
except ImportError:
    # Development install without version file
    __version__ = "0.0.0.dev"

__all__ = ["__version__"]
