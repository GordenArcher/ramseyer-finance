#!/usr/bin/env bash
set -euo pipefail

# The guard and replacement logic lives in the binary so every platform follows the same
# checksum, stop, and start sequence. Arguments are forwarded so `update.sh --force` can
# reinstall the current version on a broken install.
if ! command -v ramseyer-finance >/dev/null 2>&1; then
  echo "ramseyer-finance is not installed. Run scripts/install.sh first." >&2
  exit 1
fi

exec ramseyer-finance update "$@"
