"""Unit tests for DatabaseManager."""

import sqlite3
import tempfile
from pathlib import Path

import pytest

from frankenmanager.core.database import DatabaseManager


class TestDatabaseManager:
    """Test database manager functionality."""

    @pytest.fixture
    def db_path(self):
        """Create temporary database path."""
        with tempfile.TemporaryDirectory() as tmpdir:
            yield Path(tmpdir) / "test.db"

    def test_init_database_creates_tables(self, db_path):
        """Test that database initialization creates required tables."""
        db = DatabaseManager(db_path)

        # Check that database file was created
        assert db_path.exists()

        # Check that domains table exists
        with sqlite3.connect(db_path) as conn:
            cursor = conn.cursor()
            cursor.execute(
                "SELECT name FROM sqlite_master WHERE type='table' AND name='domains'"
            )
            assert cursor.fetchone() is not None

    def test_default_is_running_is_false(self, db_path):
        """Test that default is_running is False without Docker manager."""
        db = DatabaseManager(db_path)
        assert db.is_running is False

    def test_set_domains(self, db_path):
        """Test setting domains."""
        db = DatabaseManager(db_path)

        domains = ["example.test", "test.local"]
        db.set_domains(domains)

        assert db.get_domains() == domains

    def test_clear_domains(self, db_path):
        """Test clearing domains."""
        db = DatabaseManager(db_path)

        # First set domains
        db.set_domains(["example.test"])
        assert len(db.get_domains()) > 0

        # Then clear
        db.clear_domains()
        assert db.get_domains() == []

    def test_add_domains(self, db_path):
        """Test adding domains."""
        db = DatabaseManager(db_path)

        db.add_domains(["domain1.test", "domain2.test"])
        assert "domain1.test" in db.get_domains()
        assert "domain2.test" in db.get_domains()

        # Add more domains
        db.add_domains(["domain3.test"])
        domains = db.get_domains()
        assert len(domains) == 3

    def test_add_domains_ignores_duplicates(self, db_path):
        """Test that adding duplicate domains is ignored."""
        db = DatabaseManager(db_path)

        db.add_domains(["domain.test"])
        db.add_domains(["domain.test"])  # Add same domain again

        domains = db.get_domains()
        assert domains.count("domain.test") == 1

    def test_remove_domains(self, db_path):
        """Test removing domains."""
        db = DatabaseManager(db_path)

        # Add domains
        db.add_domains(["domain1.test", "domain2.test", "domain3.test"])
        assert len(db.get_domains()) == 3

        # Remove one domain
        db.remove_domains(["domain2.test"])
        domains = db.get_domains()
        assert len(domains) == 2
        assert "domain2.test" not in domains

    def test_reset(self, db_path):
        """Test reset functionality."""
        db = DatabaseManager(db_path)

        # Set up some state
        db.set_domains(["domain.test"])
        assert len(db.get_domains()) > 0

        # Reset
        db.reset()
        assert db.get_domains() == []

    def test_get_domains_preserves_order(self, db_path):
        """Test that get_domains preserves insertion order."""
        db = DatabaseManager(db_path)

        domains = ["zebra.test", "apple.test", "monkey.test"]
        db.add_domains(domains)

        retrieved = db.get_domains()
        assert retrieved == domains
