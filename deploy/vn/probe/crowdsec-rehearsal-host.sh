#!/usr/bin/env bash
# Fixed private-context entry point for docs/design/crowdsec-rehearsal.md.
set -euo pipefail
case "$#:${1:-}" in
  1:bootstrap | 1:seed | 1:sample | 1:measure | 1:verify) ;;
  *) printf 'INVALID arguments\n' >&2; exit 2 ;;
esac
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
exec python3 -Es "$here/../rehearsal/host_deadline.py" "$1"
