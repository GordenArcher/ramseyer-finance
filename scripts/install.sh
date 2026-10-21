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

if [ "$OS_NAME" = "windows" ]; then
  BIN_NAME="${APP}.exe"
  EXT="zip"
else
  BIN_NAME="$APP"
  EXT="tar.gz"
fi

echo "==> Fetching latest release..."
LATEST=$(curl -sL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
if [ -z "$LATEST" ]; then
  echo "Failed to fetch latest release"
  exit 1
fi
echo "    Latest: $LATEST"

ASSET="${APP}-${OS_NAME}-${ARCH_NAME}.${EXT}"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST}/${ASSET}"

echo "==> Downloading ${ASSET}..."
TMP_DIR=$(mktemp -d)
cd "$TMP_DIR"
curl -sL -o "$ASSET" "$DOWNLOAD_URL" || {
  echo "Asset not found, trying alternative naming..."
  # Try common patterns
  if [ "$OS_NAME" = "windows" ]; then
    ASSET="Ramseyer.Finance-windows.zip"
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST}/${ASSET}"
    curl -sL -o "$ASSET" "$DOWNLOAD_URL"
  fi
}

echo "==> Extracting..."
if [ "$EXT" = "zip" ]; then
  unzip -q "$ASSET"
else
  tar -xzf "$ASSET"
fi

INSTALL_DIR="/usr/local/bin"
if [ ! -w "$INSTALL_DIR" ] && [ "$OS_NAME" != "windows" ]; then
  if [ -d "$HOME/.local/bin" ] || mkdir -p "$HOME/.local/bin" 2>/dev/null; then
    INSTALL_DIR="$HOME/.local/bin"
    if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
      echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$HOME/.bashrc" 2>/dev/null || true
      echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$HOME/.zshrc" 2>/dev/null || true
    fi
  fi
fi

echo "==> Installing to $INSTALL_DIR..."
find . -name "$BIN_NAME" -type f -exec cp {} "$INSTALL_DIR/$BIN_NAME" \;
chmod +x "$INSTALL_DIR/$BIN_NAME" 2>/dev/null || true

cd - >/dev/null
rm -rf "$TMP_DIR"

# Setup service
if [ "$OS_NAME" = "darwin" ]; then
  PLIST="$HOME/Library/LaunchAgents/com.ramseyer.finance.plist"
  mkdir -p "$HOME/Library/LaunchAgents"
  cat > "$PLIST" << PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.ramseyer.finance</string>
  <key>ProgramArguments</key>
  <array>
    <string>$INSTALL_DIR/$BIN_NAME</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOST</key>
    <string>$HOST</string>
    <key>PORT</key>
    <string>$PORT</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
</dict>
</plist>
PLIST
  launchctl bootout gui/$(id -u)/com.ramseyer.finance 2>/dev/null || true
  launchctl bootstrap gui/$(id -u) "$PLIST" 2>/dev/null || launchctl load "$PLIST"
  echo "==> Service installed and started"
elif [ "$OS_NAME" = "linux" ]; then
  mkdir -p "$HOME/.config/systemd/user"
  cat > "$HOME/.config/systemd/user/ramseyer-finance.service" << SERVICE
[Unit]
Description=Ramseyer Finance
After=network.target

[Service]
Environment=HOST=$HOST
Environment=PORT=$PORT
ExecStart=$INSTALL_DIR/$BIN_NAME
Restart=always

[Install]
WantedBy=default.target
SERVICE
  systemctl --user daemon-reload
  systemctl --user enable --now ramseyer-finance.service
  echo "==> Service installed and started"
fi

echo ""
echo "==> Done!"
echo "    Open http://$HOST:$PORT in your browser"
