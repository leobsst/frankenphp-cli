#!/usr/bin/env bash
# FrankenManager installer
# Usage: curl -fsSL https://raw.githubusercontent.com/leobsst/frankenphp-cli/main/install.sh | bash

set -e

REPO="leobsst/frankenphp-cli"
BINARY_NAME="frankenmanager"
INSTALL_DIR="/usr/local/bin"

# Detect OS
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin)
    case "$ARCH" in
      arm64)   ASSET="${BINARY_NAME}-macos-arm64" ;;
      x86_64)  ASSET="${BINARY_NAME}-macos-x86_64" ;;
      *)       echo "Unsupported architecture: $ARCH"; exit 2 ;;
    esac
    ;;
  Linux)
    case "$ARCH" in
      x86_64|amd64) ASSET="${BINARY_NAME}-linux-x86_64" ;;
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

# Download to a temp file
TMP_FILE=$(mktemp)
trap 'rm -f "$TMP_FILE"' EXIT

curl -fsSL "$DOWNLOAD_URL" -o "$TMP_FILE"
chmod +x "$TMP_FILE"

# Install — use sudo only if we can't write to the target dir
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP_FILE" "${INSTALL_DIR}/${BINARY_NAME}"
else
  echo "Writing to ${INSTALL_DIR} requires elevated privileges."
  sudo mv "$TMP_FILE" "${INSTALL_DIR}/${BINARY_NAME}"
  sudo chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
fi

echo ""
echo "${BINARY_NAME} v${VERSION} installed to ${INSTALL_DIR}/${BINARY_NAME}"
echo "Run '${BINARY_NAME} --version' to verify."
