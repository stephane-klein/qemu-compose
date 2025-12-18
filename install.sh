#!/bin/bash

# Installation script for qemu-compose
# Downloads the correct binary for your architecture and installs it to ~/bin/

set -e

VERSION="v0.4.0"
BASE_URL="https://github.com/stephane-klein/qemu-compose/releases/download/${VERSION}"

# Create ~/bin/ directory if it doesn't exist
mkdir -p ~/bin/

# Detect architecture
ARCH=$(uname -m)
case $ARCH in
    x86_64|amd64)
        BINARY_NAME="qemu-compose-linux-amd64"
        ;;
    aarch64|arm64)
        BINARY_NAME="qemu-compose-linux-arm64"
        ;;
    *)
        echo "Unsupported architecture: $ARCH"
        echo "Supported architectures: x86_64 (amd64), aarch64 (arm64)"
        exit 1
        ;;
esac

DOWNLOAD_URL="${BASE_URL}/${BINARY_NAME}"
INSTALL_PATH="$HOME/bin/qemu-compose"

echo "Detected architecture: $ARCH"
echo "Downloading: $BINARY_NAME"
echo "From: $DOWNLOAD_URL"
echo "Installing to: $INSTALL_PATH"

# Download the binary
curl -L -o "$INSTALL_PATH" "$DOWNLOAD_URL"

# Make it executable
chmod ugo+x "$INSTALL_PATH"

# Set CAP_NET_ADMIN capability for network management
echo "Setting CAP_NET_ADMIN capability..."
if command -v sudo >/dev/null 2>&1; then
    sudo setcap cap_net_admin+ep "$INSTALL_PATH"
else
    echo "Warning: sudo not found. Trying to run setcap without sudo..."
    setcap cap_net_admin+ep "$INSTALL_PATH" 2>/dev/null || {
        echo "Failed to set capabilities. You may need to run:"
        echo "  sudo setcap cap_net_admin+ep $INSTALL_PATH"
    }
fi

echo ""
echo "✅ qemu-compose ${VERSION} has been installed successfully!"
echo ""
echo "Verify installation:"
echo ""
echo "  qemu-compose version"
