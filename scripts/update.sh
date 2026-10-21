#!/usr/bin/env bash
set -e

REPO="GordenArcher/ramseyer-finance"
APP="ramseyer-finance"
HOST="127.0.0.1"
PORT="47823"

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin) OS_NAME="darwin" ;;
  Linux) OS_NAME="linux" ;;
  MINGW*|MSYS*|CYGWIN*) OS_NAME="windows" ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH_NAME="amd64" ;;
  arm64|aarch64) ARCH_NAME="arm64" ;;
  *) echo "Unsupported arch: $ARCH"; exit 1 ;;
esac

BIN_NAME="$APP"
[ "$OS_NAME" = "windows" ] && BIN_NAME="${APP}.exe"
EXT="tar.gz"
[ "$OS_NAME" = "windows" ] && EXT="zip"

LATEST=$(curl -sL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
echo "Latest: $LATEST"

ASSET="${APP}-${OS_NAME}-${ARCH_NAME}.${EXT}"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST}/${ASSET}"

TMP_DIR=$(mktemp -d)
cd "$TMP_DIR"
curl -sL -o "$ASSET" "$DOWNLOAD_URL" 2>/dev/null || {
  if [ "$OS_NAME" = "windows" ]; then
    ASSET="Ramseyer.Finance-windows.zip"
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST}/${ASSET}"
    curl -sL -o "$ASSET" "$DOWNLOAD_URL"
  fi
}

if [ "$EXT" = "zip" ]; then
  unzip -q "$ASSET"
else
  tar -xzf "$ASSET"
fi

INSTALL_DIR="/usr/local/bin"
[ ! -w "$INSTALL_DIR" ] && [ -d "$HOME/.local/bin" ] && INSTALL_DIR="$HOME/.local/bin"

find . -name "$BIN_NAME" -type f -exec cp {} "$INSTALL_DIR/$BIN_NAME" \; 2>/dev/null
chmod +x "$INSTALL_DIR/$BIN_NAME" 2>/dev/null || true

cd - >/dev/null
rm -rf "$TMP_DIR"

if pgrep -f "$APP" >/dev/null 2>&1; then
  pkill -f "$APP" || true
  sleep 1
  "$INSTALL_DIR/$BIN_NAME" &
  echo "Updated and restarted"
else
  echo "Updated"
fi
