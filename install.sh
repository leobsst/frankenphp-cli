#!/usr/bin/env bash
# FrankenManager installer
# Usage: curl -fsSL https://raw.githubusercontent.com/leobsst/frankenphp-cli/refs/heads/main/install.sh | bash

set -e

REPO="leobsst/frankenphp-cli"
BINARY_NAME="frankenmanager"
INSTALL_DIR="/usr/local/bin"
# The bundle (executable + _internal/) lives here; INSTALL_DIR gets a symlink to it
LIB_DIR="/usr/local/lib/${BINARY_NAME}"

# Detect OS
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin)
    case "$ARCH" in
      arm64)   ASSET="${BINARY_NAME}-macos-arm64.tar.gz" ;;
      x86_64)  ASSET="${BINARY_NAME}-macos-x86_64.tar.gz" ;;
      *)       echo "Unsupported architecture: $ARCH"; exit 2 ;;
    esac
    ;;
  Linux)
    case "$ARCH" in
      x86_64|amd64) ASSET="${BINARY_NAME}-linux-x86_64.tar.gz" ;;
      *)             echo "Unsupported architecture: $ARCH"; exit 2 ;;
    esac
    ;;
  *)
    echo "Unsupported OS: $OS"
    exit 2
    ;;
esac

# Resolve latest version if not specified
if [ -z "$VERSION" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | sed 's/.*"tag_name": *"v\([^"]*\)".*/\1/')
  if [ -z "$VERSION" ]; then
    echo "Could not determine latest version. Set VERSION env var to override."
    exit 1
  fi
fi

DOWNLOAD_URL="https://github.com/${REPO}/releases/download/v${VERSION}/${ASSET}"

echo "Installing ${BINARY_NAME} v${VERSION} (${ASSET})..."

# Download and extract to a temp dir
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

curl -fsSL "$DOWNLOAD_URL" -o "${TMP_DIR}/${ASSET}"
tar -xzf "${TMP_DIR}/${ASSET}" -C "$TMP_DIR"

# Install — use sudo only if we can't write to the target dirs
LIB_PARENT="$(dirname "$LIB_DIR")"
mkdir -p "$LIB_PARENT" 2>/dev/null || true
SUDO=""
if [ ! -w "$INSTALL_DIR" ] || [ ! -w "$LIB_PARENT" ]; then
  echo "Writing to ${INSTALL_DIR} and ${LIB_PARENT} requires elevated privileges."
  SUDO="sudo"
fi

$SUDO mkdir -p "$INSTALL_DIR" "$LIB_PARENT"
# Swap the bundle in, then point the command at it (this also replaces a
# single-file binary left by an older install)
$SUDO rm -rf "${LIB_DIR}.new"
$SUDO mv "${TMP_DIR}/${BINARY_NAME}" "${LIB_DIR}.new"
$SUDO rm -rf "$LIB_DIR"
$SUDO mv "${LIB_DIR}.new" "$LIB_DIR"
$SUDO ln -sfn "${LIB_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"

echo ""
echo "${BINARY_NAME} v${VERSION} installed to ${LIB_DIR} (command: ${INSTALL_DIR}/${BINARY_NAME})"
echo "Run '${BINARY_NAME} --version' to verify."
