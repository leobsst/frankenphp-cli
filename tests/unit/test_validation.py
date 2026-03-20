"""Tests for validation utilities."""

from pathlib import Path

import pytest
from frankenmanager.exceptions import ValidationError
from frankenmanager.utils.validation import validate_directory, validate_domain


class TestValidateDomain:
    """Tests for domain validation."""

    def test_valid_domain_with_test_tld(self):
        """Valid .test domain should pass."""
        validate_domain("myapp.test")

    def test_valid_domain_with_com_tld(self):
        """Valid .com domain should pass."""
        validate_domain("example.com")

    def test_valid_subdomain(self):
        """Valid subdomain should pass."""
        validate_domain("api.example.com")

    def test_valid_domain_with_hyphen(self):
        """Domain with hyphen should pass."""
        validate_domain("my-app.test")

    def test_invalid_domain_no_tld(self):
        """Domain without TLD should fail."""
        with pytest.raises(ValidationError):
            validate_domain("myapp")

    def test_valid_localhost(self):
        """localhost should be accepted as a reserved local hostname."""
        validate_domain("localhost")

    def test_invalid_domain_starts_with_hyphen(self):
        """Domain starting with hyphen should fail."""
        with pytest.raises(ValidationError):
            validate_domain("-myapp.test")

    def test_invalid_domain_empty(self):
        """Empty domain should fail."""
        with pytest.raises(ValidationError):
            validate_domain("")


class TestValidateDirectory:
    """Tests for directory validation."""

    def test_valid_directory(self, tmp_path):
        """Existing directory should pass."""
        validate_directory(tmp_path)

    def test_invalid_directory_not_exists(self):
        """Non-existent directory should fail."""
        with pytest.raises(ValidationError):
            validate_directory(Path("/nonexistent/path"))

    def test_invalid_directory_is_file(self, tmp_path):
        """File path should fail."""
        test_file = tmp_path / "test.txt"
        test_file.write_text("test")
        with pytest.raises(ValidationError):
            validate_directory(test_file)
