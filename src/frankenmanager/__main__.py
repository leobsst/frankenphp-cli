"""Entry point for python -m frankenmanager."""

import sys
from pathlib import Path

# Ensure the package is importable when run as a script
if __name__ == "__main__":
    # Add src directory to path for PyInstaller compatibility
    src_path = Path(__file__).parent.parent
    if str(src_path) not in sys.path:
        sys.path.insert(0, str(src_path))

    from frankenmanager.cli import app

    app()
else:
    from .cli import app
