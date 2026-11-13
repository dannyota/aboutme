#!/usr/bin/env bash
set -euo pipefail

if [[ ${1-} != --locked ]]; then
  exec /usr/local/lib/aboutme/workload-lock.sh 21600 "$0" --locked "$@"
fi
shift

kind=${1:-}
case "$kind" in
full | diff)
  exec /usr/bin/pgbackrest --stanza=aboutme --type="$kind" backup
  ;;
verify)
  exec /usr/bin/pgbackrest --stanza=aboutme verify
  ;;
*)
  echo "usage: backup-run.sh full|diff|verify" >&2
  exit 2
  ;;
esac
