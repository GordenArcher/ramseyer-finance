#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_NAME="${APP_NAME:-Ramseyer Finance}"
BINARY_NAME="${BINARY_NAME:-ramseyer-finance}"
GOARCH_VALUE="${GOARCH:-$(go env GOARCH)}"
DIST_DIR="$ROOT_DIR/dist"
APP_DIR="$DIST_DIR/$APP_NAME.app"
CONTENTS_DIR="$APP_DIR/Contents"
MACOS_DIR="$CONTENTS_DIR/MacOS"
RESOURCES_DIR="$CONTENTS_DIR/Resources"
PLIST_PATH="$CONTENTS_DIR/Info.plist"
ZIP_PATH="$DIST_DIR/$APP_NAME-macos.zip"
ICON_SOURCE="$ROOT_DIR/static/app-icon.icns"
SIGN_IDENTITY="${SIGN_IDENTITY:-}"

rm -rf "$APP_DIR" "$ZIP_PATH"
mkdir -p "$MACOS_DIR" "$RESOURCES_DIR"

CGO_ENABLED=1 GOOS=darwin GOARCH="$GOARCH_VALUE" go build -o "$MACOS_DIR/$BINARY_NAME" "$ROOT_DIR/main.go"
chmod +x "$MACOS_DIR/$BINARY_NAME"

cat > "$PLIST_PATH" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>en</string>
	<key>CFBundleExecutable</key>
	<string>$BINARY_NAME</string>
	<key>CFBundleIdentifier</key>
	<string>com.ramseyer.finance</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>$APP_NAME</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>1.0.0</string>
	<key>CFBundleVersion</key>
	<string>1.0.0</string>
	<key>LSMinimumSystemVersion</key>
	<string>12.0</string>
	<key>NSHighResolutionCapable</key>
	<true/>
</dict>
</plist>
EOF

if [[ -f "$ICON_SOURCE" ]]; then
	cp "$ICON_SOURCE" "$RESOURCES_DIR/AppIcon.icns"
	/usr/libexec/PlistBuddy -c "Add :CFBundleIconFile string AppIcon" "$PLIST_PATH" >/dev/null 2>&1 || true
fi

if [[ -n "$SIGN_IDENTITY" ]]; then
	codesign --force --deep --sign "$SIGN_IDENTITY" "$APP_DIR"
fi

ditto -c -k --sequesterRsrc --keepParent "$APP_DIR" "$ZIP_PATH"

echo "Built app bundle: $APP_DIR"
echo "Built zip archive: $ZIP_PATH"

