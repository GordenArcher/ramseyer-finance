#!/usr/bin/env bash
set -euo pipefail

if ! command -v ramseyer-finance >/dev/null 2>&1; then
  echo "ramseyer-finance is not installed. Run scripts/install.sh first." >&2
  exit 1
fi

exec ramseyer-finance update
