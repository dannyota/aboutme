#!/usr/bin/env bash
# Fixed private-context entry point for docs/design/crowdsec-rehearsal.md.
set -euo pipefail
case "$#:${1:-}" in
  1:bootstrap | 1:seed | 1:sample | 1:measure | 1:verify) ;;
  *) printf 'INVALID arguments\n' >&2; exit 2 ;;
esac
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
exec timeout --signal=TERM --kill-after=10s 3600s \
  python3 -Es "$here/../rehearsal/host.py" "$1"
