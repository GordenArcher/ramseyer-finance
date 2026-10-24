#!/usr/bin/env bash
# Install the current release as ~/.local/bin/ramseyer-finance. It detects the current
# macOS/Linux machine and CPU automatically, configures PATH, and opens the app when done.
set -euo pipefail

repo="GordenArcher/ramseyer-finance"
app="ramseyer-finance"
app_port=47823

# Printed so a support session can confirm which copy actually executed. The raw URL is CDN
# cached, and a stale copy was previously mistaken for an unfixed script.
installer_revision="2026-10-24-checksum-stop"
echo "$app installer ($installer_revision)"

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

# GitHub publishes a sha256 digest per asset. Verifying it stops a truncated or swapped archive
# from being installed. The awk window is bounded because the digest appears a few lines after
# the asset name in the pretty-printed release JSON, and older releases may have no digest.
digest="$(printf '%s\n' "$release_json" |
  grep -A 20 "\"name\": *\"$asset\"" |
  sed -n 's/.*"digest": *"\([^"]*\)".*/\1/p' |
  head -n 1)"

install_dir="$HOME/.local/bin"
mkdir -p "$install_dir"
staging_dir="$(mktemp -d)"
cleanup() { rm -rf "$staging_dir"; }
trap cleanup EXIT

echo "Downloading $asset..."
curl --fail --silent --show-error --location "$download_url" -o "$staging_dir/$asset"

if [[ "$digest" == sha256:* ]]; then
  expected_digest="${digest#sha256:}"
  if command -v shasum >/dev/null 2>&1; then
    actual_digest="$(shasum -a 256 "$staging_dir/$asset" | awk '{print $1}')"
  else
    actual_digest="$(sha256sum "$staging_dir/$asset" | awk '{print $1}')"
  fi
  if [ "$actual_digest" != "$expected_digest" ]; then
    echo "The downloaded archive failed checksum verification. Check your connection and run the installer again." >&2
    exit 1
  fi
fi

tar -xzf "$staging_dir/$asset" -C "$staging_dir"
test -f "$staging_dir/$app"

# Stop an existing install before replacing the binary. The service manager is asked first so it
# does not restart the old process mid-copy, then anything still holding the port is stopped
# because a manual serve or development build is unknown to the manager. The installed binary's
# own stop command is not used: the version being replaced may contain the broken stop path.
echo "Stopping any running $app instance..."
case "$os" in
  darwin) launchctl bootout "gui/$(id -u)/$app" >/dev/null 2>&1 || true ;;
  linux) systemctl --user stop "$app.service" >/dev/null 2>&1 || true ;;
esac
if command -v lsof >/dev/null 2>&1; then
  running_pids="$(lsof -ti "tcp:$app_port" -sTCP:LISTEN 2>/dev/null || true)"
  if [ -n "$running_pids" ]; then
    # Word splitting is intentional here because lsof prints one PID per line.
    # shellcheck disable=SC2086
    kill $running_pids 2>/dev/null || true
    sleep 1
    # shellcheck disable=SC2086
    kill -9 $running_pids 2>/dev/null || true
  fi
fi

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

echo "Starting $app..."
"$install_dir/$app" open

# A start can return before the HTTP listener is ready, and a failure used to print success with
# nothing listening. Polling the health endpoint makes the installer report the real outcome.
healthy=0
for _ in $(seq 1 40); do
  if curl --fail --silent --show-error --max-time 2 "http://127.0.0.1:$app_port/healthz" >/dev/null 2>&1; then
    healthy=1
    break
  fi
  sleep 0.5
done

if [ "$healthy" -ne 1 ]; then
  if [ "$os" = "darwin" ]; then
    config_dir="$HOME/Library/Application Support"
  else
    config_dir="${XDG_CONFIG_HOME:-$HOME/.config}"
  fi
  echo "$app did not respond on http://127.0.0.1:$app_port. Start it with \"$install_dir/$app\" serve and check $config_dir/ramseyer-financial-manager/logs/server.log." >&2
  exit 1
fi

echo "$app is running at http://127.0.0.1:$app_port"
