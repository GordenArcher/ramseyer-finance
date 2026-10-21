#!/usr/bin/env bash
# Install the current release as ~/.local/bin/ramseyer-finance. It detects the current
# macOS/Linux machine and CPU automatically, configures PATH, and opens the app when done.
set -euo pipefail

repo="GordenArcher/ramseyer-finance"
app="ramseyer-finance"

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *) echo "This installer supports macOS and Linux. Use scripts/install.ps1 on Windows." >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "Unsupported CPU architecture: $(uname -m)" >&2; exit 1 ;;
esac

for command in curl tar; do
  command -v "$command" >/dev/null || { echo "Missing required command: $command" >&2; exit 1; }
done

asset="$app-$os-$arch.tar.gz"
release_json="$(curl --fail --silent --show-error -L "https://api.github.com/repos/$repo/releases/latest")"
download_url="$(printf '%s' "$release_json" | sed -n 's#.*"browser_download_url": *"\([^"]*'"$asset"'\)".*#\1#p' | head -n 1)"
if [ -z "$download_url" ]; then
  echo "The latest release does not include $asset." >&2
  exit 1
fi

install_dir="$HOME/.local/bin"
mkdir -p "$install_dir"
staging_dir="$(mktemp -d)"
cleanup() { rm -rf "$staging_dir"; }
trap cleanup EXIT

echo "Downloading $asset..."
curl --fail --silent --show-error --location "$download_url" -o "$staging_dir/$asset"
tar -xzf "$staging_dir/$asset" -C "$staging_dir"
test -f "$staging_dir/$app"
install -m 0755 "$staging_dir/$app" "$install_dir/$app"

case ":$PATH:" in
  *":$install_dir:"*) ;;
  *)
    echo "Installed $app in $install_dir. Add this to your shell profile, then open a new terminal:"
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
    ;;
esac
echo "Installed. Enabling automatic start..."
"$install_dir/$app" enable
echo "Opening Ramseyer Finance for the first time..."
"$install_dir/$app" open
