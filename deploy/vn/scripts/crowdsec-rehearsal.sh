#!/usr/bin/env bash
# Runs only the fixed synthetic protocol in docs/design/crowdsec-rehearsal.md.
set -euo pipefail
case "$#:${1:-}" in
  2:plan | 2:run | 1:cleanup) ;;
  *) printf 'INVALID arguments\n' >&2; exit 2 ;;
esac
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
exec python3 -Es "$here/../rehearsal/coordinator.py" "$@"
