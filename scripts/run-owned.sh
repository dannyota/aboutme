#!/usr/bin/env bash
# Own a command's lifetime and descendants without changing its memory or priority.
# Exit codes: 124 timeout, 125 refused before launch, 126 ownership inspection
# failed; otherwise the command status (128 + signal if killed).
set -Eeuo pipefail
command -v python3 >/dev/null || {
  printf 'run-owned: refused: python3 is required\n' >&2
  exit 125
}
exec "$(command -v python3)" -B "$(dirname "${BASH_SOURCE[0]}")/lib/run-owned.py" "$@"
