#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  printf '%s\n' "Usage: ./scripts/bump_version.sh v1.2.1" >&2
  exit 1
fi

NEW_VERSION="$1"

if [[ ! "$NEW_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  printf '%s\n' "Version must match v<major>.<minor>.<patch>, for example v1.2.1" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
META_FILE="$ROOT_DIR/internal/appmeta/meta.go"

if [[ ! -f "$META_FILE" ]]; then
  printf '%s\n' "Could not find $META_FILE" >&2
  exit 1
fi

CURRENT_VERSION="$(sed -n 's/.*CurrentVersion = "\(v[0-9][^"]*\)".*/\1/p' "$META_FILE")"

if [[ -z "$CURRENT_VERSION" ]]; then
  printf '%s\n' "Could not read CurrentVersion from $META_FILE" >&2
  exit 1
fi

perl -0pi -e 's/CurrentVersion = "v[0-9]+\.[0-9]+\.[0-9]+"/CurrentVersion = "'"$NEW_VERSION"'"/' "$META_FILE"

printf '%s\n' "Updated version:"
printf '  %s -> %s\n' "$CURRENT_VERSION" "$NEW_VERSION"
printf '%s\n' ""
printf '%s\n' "Next steps:"
printf '  1. Review the change: git diff -- %s\n' "$META_FILE"
printf '  2. Commit it: git add %s && git commit -m "chore: bump version to %s"\n' "$META_FILE" "$NEW_VERSION"
printf '  3. Tag it: git tag %s\n' "$NEW_VERSION"
printf '  4. Build the Windows package from that commit/tag\n'
