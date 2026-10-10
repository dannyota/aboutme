#!/usr/bin/env bash
# Runs one pgBackRest operation as the postgres user for
# aboutme-backup@<kind>.service (docs/design/vietnam-production.md,
# "PostgreSQL"). Accepts only full, diff, or verify.
set -euo pipefail

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
