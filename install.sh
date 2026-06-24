#!/bin/sh
set -e

# Configuration
REPO="RaquerLabs/xsmd"
INSTALL_DIR="$HOME/.local/bin"

# 1. Detect OS and Arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# 2. Get latest release version
LATEST_VERSION=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

# 3. Download the correct file
FILENAME="xsmd-lsp-$LATEST_VERSION-$OS-$ARCH"
EXT=".tar.gz"
[ "$OS" = "windows" ] && EXT=".zip"

URL="https://github.com/$REPO/releases/download/$LATEST_VERSION/$FILENAME$EXT"

echo "Downloading $URL..."
curl -LO "$URL"

# 4. Extract and Install
mkdir -p "$INSTALL_DIR"
if [ "$EXT" = ".zip" ]; then
    unzip -o "$FILENAME.zip" -d "$INSTALL_DIR"
else
    tar -xzf "$FILENAME.tar.gz" -C "$INSTALL_DIR"
fi

echo "Successfully installed to $INSTALL_DIR/xsmd"

