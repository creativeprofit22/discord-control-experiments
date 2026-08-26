#!/usr/bin/env bash
set -euo pipefail
if [[ $# -ne 1 ]]; then
  echo 'usage: scripts/drills.sh EVIDENCE.json' >&2
  exit 2
fi
exec go run ./tools/drills "$1"
