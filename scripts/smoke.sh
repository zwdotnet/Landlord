#!/usr/bin/env bash
# Portable HTTP smoke checks: version, removed endpoints and SQLite backups.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec python3 "$ROOT/scripts/smoke.py" "$@"
