#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
host=$repo_root/deploy/vn/host
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/feed" "$work/bin"
failures=0

check() {
  local label=$1
  shift
  if "$@" >/dev/null 2>&1; then
    printf 'PASS %s\n' "$label"
  else
    printf 'FAIL %s\n' "$label"
    failures=$((failures + 1))
  fi
}

check postgresql-retention-admission python3 - "$host/crowdsec-retention.sh" <<'PY'
import pathlib
import re
import sys

source = pathlib.Path(sys.argv[1]).read_text()
assert "crowdsec-db.sh" in source and "preflight-ready" in source
assert not re.search(r"\bsqlite3?\b|wal_checkpoint|pg_switch_wal|pgbackrest|VACUUM|ANALYZE", source, re.I)
assert re.search(r"\bpsql\b", source)
assert re.search(r"\bflock\b", source)
assert "UTC" in source
assert re.search(r"statement_timeout\s*=\s*'10s'", source)
assert re.search(r"lock_timeout\s*=\s*'2s'", source)
assert re.search(r"LIMIT\s+1000\b", source, re.I)
assert re.search(r"23\s+hours", source)
assert re.search(r"30\s+minutes", source)
assert "isfinite(" in source
for relation, columns in {
    "alerts": ("created_at", "updated_at", "started_at", "stopped_at"),
    "decisions": ("created_at", "updated_at", "until"),
    "events": ("created_at", "updated_at", "time"),
    "meta": ("created_at", "updated_at"),
}.items():
    assert relation in source
    for column in columns:
        assert column in source
assert re.search(r"22\s+hours", source)
PY

check stopped-consumer-reconciliation python3 - "$host/crowdsec-retention.sh" <<'PY'
import pathlib
import re
import sys

source = pathlib.Path(sys.argv[1]).read_text()
for unit in ("aboutme-caddy.service", "aboutme-maintenance.service",
             "crowdsec-firewall-bouncer.service", "crowdsec.service"):
    assert unit in source
assert "last_pull" not in source
assert not re.search(r"decisions\s+stream|alerts\s+flush", source)
assert "crowdsec-chain" in source and "crowdsec-blacklists" in source
assert "crowdsec6-chain" in source and "crowdsec6-blacklists" in source
assert not re.search(r"flush\s+ruleset|delete\s+table\s+inet|stop\s+postgresql", source)
assert re.search(r"(?:is-active|ActiveState)", source)
assert re.search(r"(?:MainPID|ControlGroup|cgroup)", source)
assert re.search(r"\b20(?:s|\b)", source)
PY

cat >"$work/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$ABOUTME_TEST_SYSTEMCTL"
if [[ ${1-} == is-active && ${ABOUTME_TEST_STILL_ACTIVE:-0} == 1 ]]; then
  exit 0
fi
[[ ${1-} != is-active ]]
STUB
cat >"$work/bin/alert.sh" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$ABOUTME_TEST_ALERT"
STUB
chmod +x "$work/bin/systemctl" "$work/bin/alert.sh"
export ABOUTME_RETENTION_SYSTEMCTL=$work/bin/systemctl
export ABOUTME_RETENTION_ALERT=$work/bin/alert.sh
export ABOUTME_RETENTION_FAILURE_MARKER=$work/failure
export ABOUTME_RETENTION_FEED_DIR=$work/feed
export ABOUTME_RETENTION_SUCCESS_MARKER=$work/retention-ok
export ABOUTME_TEST_SYSTEMCTL=$work/systemctl.log
export ABOUTME_TEST_ALERT=$work/alert.log

: >"$work/retention-ok"
"$host/crowdsec-retention-fail.sh"
grep -qxF 'stop aboutme-caddy.service aboutme-maintenance.service crowdsec-firewall-bouncer.service crowdsec.service aboutme-crowdsec-retention-startup.service' \
  "$work/systemctl.log" || { echo "FAIL retention-stop-consumers" >&2; exit 1; }
[[ $(cat "$work/failure") == retention-failed ]] || { echo "FAIL retention-fixed-marker" >&2; exit 1; }
grep -qxF 'crowdsec-retention CrowdSec retention failed' "$work/alert.log" || {
  echo "FAIL retention-fixed-alarm" >&2
  exit 1
}
[[ ! -e $work/feed && ! -e $work/retention-ok ]] || {
  echo "FAIL retention-failure-removes-feed-and-lease" >&2; exit 1;
}
if grep -qE '(stop|kill).*postgresql' "$work/systemctl.log"; then
  echo "FAIL retention-preserves-application-database" >&2; exit 1
fi

mkdir -p "$work/feed"
printf '%s\n' retained >"$work/feed/serving.json"
if ABOUTME_TEST_STILL_ACTIVE=1 "$host/crowdsec-retention-fail.sh"; then
  echo "FAIL retention-refuses-active-consumer" >&2
  exit 1
fi
grep -qF 'kill --kill-whom=all --signal=SIGKILL' "$work/systemctl.log" || {
  echo "FAIL retention-kills-active-consumer" >&2; exit 1;
}
[[ -s $work/feed/serving.json ]] || {
  echo "FAIL retention-confirms-quiescence-before-feed-removal" >&2; exit 1;
}

((failures == 0)) || exit 1
echo "CrowdSec retention tests passed"
