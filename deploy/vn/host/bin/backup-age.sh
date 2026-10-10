#!/usr/bin/env bash
# Checks the age of the newest pgBackRest backup and prints the fixed marker
# a vMonitor log alarm matches when it is over 26 hours or absent
# (docs/design/vietnam-production.md, "PostgreSQL" and "Logs, metrics, and
# alarms"). Runs as the postgres user.
set -euo pipefail

max_hours=26

info=$(/usr/bin/pgbackrest --stanza=aboutme info --output=json) || {
  echo "aboutme-backup-stale age_hours=unknown"
  exit 1
}
newest=$(jq -r '[.[0].backup[]?.timestamp.stop] | max // empty' <<<"$info")
if [ -z "$newest" ]; then
  echo "aboutme-backup-stale age_hours=none"
  exit 1
fi
age_hours=$((($(date +%s) - newest) / 3600))
if [ "$age_hours" -gt "$max_hours" ]; then
  echo "aboutme-backup-stale age_hours=$age_hours"
  exit 1
fi
echo "aboutme-backup-fresh age_hours=$age_hours"
