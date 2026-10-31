#!/usr/bin/env bash
# Sample whole-tree RSS every 0.5 seconds with a total RAM minus 10 GiB limit.
# Sampling can miss a burst shorter than 0.5 seconds. A process that leaves
# both the process group and the descendant tree escapes this watchdog.
# Exit codes: 123 memory exceeded, 124 timeout, 125 refused before launch,
# 126 sampling failed; otherwise the command status (128 + signal if killed).
set -Eeuo pipefail
command -v python3 >/dev/null || {
  printf 'run-capped: refused: python3 is required\n' >&2
  exit 125
}
exec python3 "$(dirname "${BASH_SOURCE[0]}")/lib/run-capped.py" "$@"
