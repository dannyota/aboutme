#!/usr/bin/env bash
# Ordinary maintenance keeps the pinned catalog free of planner samples.
set -euo pipefail
umask 077

vacuum_main() {
  [[ $EUID == 0 && $# == 0 ]] || return 1
  [[ -d /run/aboutme && ! -L /run/aboutme && $(stat -c '%u:%g:%a' /run/aboutme) == 0:0:755 ]] || return 1
  [[ ! -L /run/aboutme/crowdsec-vacuum.lock ]] || return 1
  exec {vacuum_lock}>/run/aboutme/crowdsec-vacuum.lock
  [[ $(stat -c '%u:%g:%a:%h' /run/aboutme/crowdsec-vacuum.lock) == 0:0:600:1 ]] || return 1
  flock -n "$vacuum_lock" || return 1
  local work relation
  work=$(mktemp -d /run/aboutme/crowdsec-vacuum.XXXXXX)
  # Capture the fixed-prefix temporary path before this local leaves scope.
  # shellcheck disable=SC2064
  trap "rm -rf -- '$work'" EXIT
  /usr/local/lib/aboutme/crowdsec-db.sh preflight-ready </dev/null \
    3>"$work/epoch" >/dev/null 2>&1 || return 1
  python3 -I - /usr/local/lib/aboutme/crowdsec-schema-v1.8.1.tsv <<'PY' >"$work/tables" || return 1
import pathlib,re,sys
rows=[row.split('\t') for row in pathlib.Path(sys.argv[1]).read_text().splitlines()]
tables=sorted(row[1] for row in rows if row[0]=='relation' and row[2]=='r')
assert tables and len(tables)==len(set(tables))
assert all(re.fullmatch(r'[a-z][a-z0-9_]{0,62}',table) for table in tables)
print('\n'.join(tables))
PY
  while IFS= read -r relation; do
    /usr/local/lib/aboutme/crowdsec-db.sh preflight-ready <"$work/epoch" >/dev/null 2>&1 || return 1
    printf 'SET statement_timeout = '\''10s'\'';\nSET lock_timeout = '\''2s'\'';\nVACUUM (ANALYZE FALSE) public."%s";\n' "$relation" |
      timeout 12 env -i PATH=/usr/bin:/bin PGPASSFILE=/dev/null PGSSLMODE=disable \
        /usr/lib/postgresql/18/bin/psql -X -qAt -w -v ON_ERROR_STOP=1 \
        -h /run/postgresql -U crowdsec -d crowdsec >/dev/null 2>&1 || return 1
  done <"$work/tables"
  /usr/local/lib/aboutme/crowdsec-db.sh preflight-ready <"$work/epoch" >/dev/null 2>&1
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  [[ $# == 0 && $EUID == 0 ]] || exit 2
  if ! timeout --signal=TERM --kill-after=2s 300 env -i PATH=/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    /usr/bin/bash -c 'source /usr/local/lib/aboutme/crowdsec-vacuum.sh; vacuum_main' \
    >/dev/null 2>&1; then
    /usr/local/lib/aboutme/alert.sh crowdsec-vacuum 'CrowdSec vacuum failed' >/dev/null 2>&1 || true
    exit 1
  fi
fi
