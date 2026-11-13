#!/usr/bin/env bash
# FrankenManager installer (macOS, Linux)
# Usage: curl -fsSL https://raw.githubusercontent.com/leobsst/frankenphp-cli/refs/heads/main/install.sh | bash
#
# Environment:
#   VERSION        version to install (default: the latest release)
#   PREFIX         install prefix (default: /usr/local): the bundle goes to
#                  $PREFIX/lib/frankenmanager, the command to $PREFIX/bin
#   DOWNLOAD_BASE  where the release assets are (default: the GitHub release
#                  of VERSION); a file:// URL works, for tests

set -euo pipefail

REPO="leobsst/frankenphp-cli"
BINARY_NAME="frankenmanager"
PREFIX="${PREFIX:-/usr/local}"
INSTALL_DIR="${PREFIX}/bin"
# The bundle (executable, plus FrankenManager.app on macOS) lives here, at
# the path the DNS watcher's LaunchAgent or systemd unit points to;
# INSTALL_DIR gets a symlink to it
LIB_DIR="${PREFIX}/lib/${BINARY_NAME}"

# Detect the platform
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin)
    # A shell under Rosetta reports x86_64 on Apple Silicon: take arm64 anyway
    if [ "$(sysctl -n hw.optional.arm64 2>/dev/null || echo 0)" = "1" ]; then
      ARCH="arm64"
    fi
    case "$ARCH" in
      arm64)  PLATFORM="macos-arm64" ;;
      x86_64) PLATFORM="macos-x86_64" ;;
      *)      echo "Unsupported architecture: $ARCH"; exit 2 ;;
    esac
    ;;
  Linux)
    case "$ARCH" in
      x86_64|amd64)  PLATFORM="linux-x86_64" ;;
      aarch64|arm64) PLATFORM="linux-arm64" ;;
      armv7l|armv7*) PLATFORM="linux-armv7" ;;
      *)             echo "Unsupported architecture: $ARCH"; exit 2 ;;
    esac
    ;;
  *)
    echo "Unsupported OS: $OS (on Windows, use install.ps1)"
    exit 2
    ;;
esac
ASSET="${BINARY_NAME}-${PLATFORM}.tar.gz"

# Resolve the latest version if not specified
if [ -z "${VERSION:-}" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | sed 's/.*"tag_name": *"v\([^"]*\)".*/\1/')
  if [ -z "$VERSION" ]; then
    echo "Could not determine latest version. Set VERSION env var to override."
    exit 1
  fi
fi
VERSION="${VERSION#v}"
DOWNLOAD_BASE="${DOWNLOAD_BASE:-https://github.com/${REPO}/releases/download/v${VERSION}}"

echo "Installing ${BINARY_NAME} v${VERSION} (${ASSET})..."

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

curl -fsSL "${DOWNLOAD_BASE}/${ASSET}" -o "${TMP_DIR}/${ASSET}"

# Check the archive against the published checksums before installing it
if curl -fsSL "${DOWNLOAD_BASE}/SHA256SUMS.txt" -o "${TMP_DIR}/SHA256SUMS.txt" 2>/dev/null; then
  EXPECTED=$(awk -v name="$ASSET" '$2 == name { print $1 }' "${TMP_DIR}/SHA256SUMS.txt")
  if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL=$(sha256sum "${TMP_DIR}/${ASSET}" | awk '{ print $1 }')
  else
    ACTUAL=$(shasum -a 256 "${TMP_DIR}/${ASSET}" | awk '{ print $1 }')
  fi
  if [ -z "$EXPECTED" ] || [ "$EXPECTED" != "$ACTUAL" ]; then
    echo "Checksum mismatch for ${ASSET}: the download is corrupted or was tampered with."
    exit 1
  fi
else
  echo "Warning: no SHA256SUMS.txt for v${VERSION}, the archive is not verified."
fi

tar -xzf "${TMP_DIR}/${ASSET}" -C "$TMP_DIR"
if [ ! -f "${TMP_DIR}/${BINARY_NAME}/${BINARY_NAME}" ]; then
  echo "The archive does not contain ${BINARY_NAME}/${BINARY_NAME}."
  exit 1
fi

# Install — use sudo only if we can't write to the target dirs
LIB_PARENT="$(dirname "$LIB_DIR")"
mkdir -p "$LIB_PARENT" "$INSTALL_DIR" 2>/dev/null || true
SUDO=""
if [ ! -w "$INSTALL_DIR" ] || [ ! -w "$LIB_PARENT" ]; then
  echo "Writing to ${INSTALL_DIR} and ${LIB_PARENT} requires elevated privileges."
  SUDO="sudo"
fi

$SUDO mkdir -p "$INSTALL_DIR" "$LIB_PARENT"
# Swap the bundle in, then point the command at it. This also removes the
# _internal/ directory of an earlier install, and replaces a single-file
# binary left by an older one.
$SUDO rm -rf "${LIB_DIR}.new"
$SUDO mv "${TMP_DIR}/${BINARY_NAME}" "${LIB_DIR}.new"
$SUDO rm -rf "$LIB_DIR"
$SUDO mv "${LIB_DIR}.new" "$LIB_DIR"
$SUDO ln -sfn "${LIB_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"

# The short alias (fm by default, CLI_ALIAS setting) next to the command. The
# user's HOME is kept so that a configured alias is read from their data
# directory (read-only: nothing is created there).
$SUDO env HOME="$HOME" FRANKENMANAGER_DATA_DIR="${FRANKENMANAGER_DATA_DIR:-}" \
  "${INSTALL_DIR}/${BINARY_NAME}" --no-update-check _install-alias-internal --bin-dir "$INSTALL_DIR" || true

echo ""
echo "${BINARY_NAME} v${VERSION} installed to ${LIB_DIR} (command: ${INSTALL_DIR}/${BINARY_NAME})"
echo "Run '${BINARY_NAME} --version' to verify, then 'sudo ${BINARY_NAME} setup --install-mkcert' on a first install."
