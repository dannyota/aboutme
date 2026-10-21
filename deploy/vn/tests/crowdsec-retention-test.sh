#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
host=$repo_root/deploy/vn/host

if [[ ${1:-} == postgresql ]]; then
  # shellcheck source=crowdsec-retention-fixture.sh
  source "$repo_root/deploy/vn/tests/crowdsec-retention-fixture.sh"
  trap 'printf "FAIL retention-postgresql-command\n"' ERR
  retention_fixture=''
  retention_fixture_start "$repo_root"
  : "${retention_fixture:?fixture initialization failed}"
  run_retention() { /usr/local/lib/aboutme/crowdsec-retention.sh "$1"; }
  lease_check() { /usr/local/lib/aboutme/crowdsec-retention-lease-check.sh; }
  refused() { if "$@" >/dev/null 2>&1; then return 1; fi; }
  check_hosted() {
    local label=$1
    shift
    if "$@" >/dev/null 2>&1; then printf 'PASS %s\n' "$label"
    else printf 'FAIL %s\n' "$label"; exit 1; fi
  }

  check_hosted startup-empty-postgresql run_retention startup
  check_hosted valid-current-epoch-lease lease_check
  touch "$retention_fixture/timer-disabled"
  check_hosted valid-lease-disabled-timer refused lease_check
  rm "$retention_fixture/timer-disabled"
  check_hosted restored-enabled-timer lease_check
  touch "$retention_fixture/timer-inactive"
  check_hosted valid-lease-inactive-timer refused lease_check
  rm "$retention_fixture/timer-inactive"
  check_hosted restored-active-timer lease_check
  touch "$retention_fixture/vacuum-timer-disabled"
  check_hosted valid-lease-disabled-vacuum-timer refused lease_check
  rm "$retention_fixture/vacuum-timer-disabled"
  check_hosted restored-enabled-vacuum-timer lease_check
  touch "$retention_fixture/vacuum-timer-inactive"
  check_hosted valid-lease-inactive-vacuum-timer refused lease_check
  rm "$retention_fixture/vacuum-timer-inactive"
  check_hosted restored-active-vacuum-timer lease_check

  for fault in nft-malformed nft-query-failed nft-unexpected; do
    : >"$retention_fixture/control-log"
    touch "$retention_fixture/$fault"
    check_hosted "$fault-refuses-startup" refused run_retention startup
    if grep -qxF nft-init "$retention_fixture/control-log"; then
      echo 'FAIL invalid-firewall-metadata-triggered-initialization'; exit 1
    fi
    rm "$retention_fixture/$fault"
    check_hosted proven-firewall-absence-allows-startup run_retention startup
    check_hosted restored-firewall-current-lease lease_check
  done
  printf '22222222222222222222222222222222\n' >"$retention_fixture/invocation"
  check_hosted valid-lease-changed-epoch refused lease_check
  printf '11111111111111111111111111111111\n' >"$retention_fixture/invocation"
  check_hosted restored-current-epoch lease_check
  runuser -u postgres -- /usr/lib/postgresql/18/bin/pg_ctl -D "$retention_fixture/data" \
    -m fast -w restart >/dev/null 2>&1
  check_hosted real-postgresql-restart-invalidates-lease refused lease_check
  check_hosted startup-replaces-restarted-epoch run_retention startup
  check_hosted replacement-current-epoch-lease lease_check

  retention_fixture_sql >/dev/null <<'SQL'
INSERT INTO public.machines (created_at,updated_at,machine_id,password,ip_address)
VALUES (now(),now(),'retention-fixture','fixed-fixture','');
INSERT INTO public.bouncers (created_at,updated_at,name,api_key,revoked)
VALUES (now(),now(),'retention-fixture','fixed-fixture',false);
INSERT INTO public.alerts (id,created_at,updated_at,started_at,stopped_at,scenario)
SELECT id,now()-interval '23 hours 31 minutes',now(),now(),now(),'aboutme/http-status' FROM generate_series(1,1001) id;
INSERT INTO public.alerts (id,created_at,updated_at,started_at,stopped_at,scenario)
VALUES (2000,now(),now(),now(),now(),'aboutme/http-rate'),
       (3000,now(),now(),now(),now(),'crowdsecurity/ssh-bf');
INSERT INTO public.events (created_at,updated_at,time,serialized,alert_events)
VALUES (now(),now(),now()-interval '23 hours 31 minutes','{}',3000),
       (now(),now(),now(),'{}',2000),(now(),now(),now(),'{}',NULL);
INSERT INTO public.meta (created_at,updated_at,key,value,alert_metas)
VALUES (now(),now(),'fixed','fixed',2000),(now(),now(),'fixed','fixed',NULL);
SQL
  check_hosted bounded-postgresql-batch-removal run_retention startup
  [[ $(retention_fixture_sql <<<'SELECT count(*) FROM public.alerts;') == 1 ]]
  [[ $(retention_fixture_sql <<<'SELECT count(*) FROM public.events;') == 1 ]]
  [[ $(retention_fixture_sql <<<'SELECT count(*) FROM public.meta;') == 1 ]]
  [[ $(retention_fixture_sql <<<'SELECT count(*) FROM public.machines WHERE machine_id='\''retention-fixture'\'' AND password='\''fixed-fixture'\'';') == 1 ]]
  [[ $(retention_fixture_sql <<<'SELECT count(*) FROM public.bouncers WHERE name='\''retention-fixture'\'' AND api_key='\''fixed-fixture'\'';') == 1 ]]
  [[ $(sort -nr "$retention_fixture/batch-counts" | head -n1) == 1000 ]]
  [[ $(retention_fixture_admin aboutme <<<'SELECT count(*) FROM public.retention_sentinel;') == 1 ]]
  printf 'PASS stale-graphs-orphans-fresh-rows-and-application-preserved\n'

  retention_fixture_sql >/dev/null <<'SQL'
INSERT INTO public.alerts (id,created_at,updated_at,started_at,stopped_at,scenario)
VALUES (3000,now()-interval '23 hours 31 minutes',now(),now(),now(),'aboutme/http-status');
SQL
  printf 'active\n' >"$retention_fixture/consumer-state"
  : >"$retention_fixture/control-log"
  check_hosted no-decision-candidate-runtime run_retention runtime
  if grep -qE '^stop |^start ' "$retention_fixture/control-log"; then
    echo 'FAIL no-decision-candidate-restarted-consumer'; exit 1
  fi
  printf 'PASS no-decision-candidate-keeps-consumers\n'

  retention_fixture_sql >/dev/null <<'SQL'
INSERT INTO public.decisions (created_at,updated_at,until,scenario,type,scope,value,origin,alert_decisions)
VALUES (now()-interval '2 hours',now(),now()-interval '1 minute','aboutme/http-status','ban','Ip','192.0.2.20','crowdsec',2000);
SQL
  : >"$retention_fixture/control-log"
  check_hosted expired-decision-stopped-control-path run_retention runtime
  [[ $(retention_fixture_sql <<<'SELECT count(*) FROM public.decisions;') == 0 ]]
  stop_line=$(grep -n '^stop ' "$retention_fixture/control-log" | head -n1 | cut -d: -f1)
  delete_line=$(grep -n '^sql-delete$' "$retention_fixture/control-log" | head -n1 | cut -d: -f1)
  start_line=$(grep -n '^start ' "$retention_fixture/control-log" | head -n1 | cut -d: -f1)
  ((stop_line < delete_line && delete_line < start_line))
  printf 'PASS stopped-control-order-before-physical-deletion\n'

  for rebuild in accepted rejected; do
    touch "$retention_fixture/caddy-stopped"
    printf 'active\n' >"$retention_fixture/consumer-state"
    : >"$retention_fixture/control-log"
    retention_fixture_sql >/dev/null <<'SQL'
INSERT INTO public.decisions (created_at,updated_at,until,scenario,type,scope,value,origin,alert_decisions)
VALUES (now()-interval '2 hours',now(),now()-interval '1 minute','aboutme/http-status','ban','Ip','192.0.2.20','crowdsec',2000);
SQL
    check_hosted bouncer-only-current-lease lease_check
    if [[ $rebuild == accepted ]]; then
      check_hosted bouncer-only-valid-rebuild run_retention runtime
      check_hosted bouncer-only-valid-rebuild-renews lease_check
    else
      touch "$retention_fixture/nft-rebuild-mismatch"
      check_hosted bouncer-only-invalid-rebuild-refuses refused run_retention runtime
      [[ ! -e /run/aboutme/crowdsec-retention-ok ]]
      rm "$retention_fixture/nft-rebuild-mismatch"
    fi
    if grep -qE '^start aboutme-(caddy|maintenance)\.service$' "$retention_fixture/control-log"; then
      echo 'FAIL bouncer-only-path-started-caddy'; exit 1
    fi
    rm "$retention_fixture/caddy-stopped"
    printf 'inactive\n' >"$retention_fixture/consumer-state"
    check_hosted recover-after-bouncer-only-rebuild run_retention startup
  done
  printf 'active\n' >"$retention_fixture/consumer-state"

  for interleave in add replace; do
    retention_fixture_sql >/dev/null <<'SQL'
INSERT INTO public.alerts (id,created_at,updated_at,started_at,stopped_at,scenario)
VALUES (3000,now()-interval '23 hours 31 minutes',now(),now(),now(),'aboutme/http-status');
INSERT INTO public.decisions (id,created_at,updated_at,until,scenario,type,scope,value,origin,alert_decisions)
VALUES (9001,now(),now(),now()+interval '1 hour','aboutme/http-status','ban','Ip','192.0.2.20','crowdsec',3000);
SQL
    touch "$retention_fixture/$interleave-child-decision"
    check_hosted "$interleave-child-decision-refuses-deletion" refused run_retention runtime
    [[ $(retention_fixture_sql <<<'SELECT count(*) FROM public.alerts WHERE id=3000;') == 1 ]]
    [[ $(retention_fixture_sql <<<'SELECT count(*) FROM public.decisions WHERE id=9002;') == 1 ]]
    [[ ! -e /run/aboutme/crowdsec-retention-ok ]]
    retention_fixture_sql >/dev/null <<'SQL'
DELETE FROM public.alerts WHERE id=3000;
SQL
    check_hosted recover-after-child-decision-change run_retention startup
    printf 'active\n' >"$retention_fixture/consumer-state"
  done

  retention_fixture_sql >/dev/null <<'SQL'
INSERT INTO public.alerts (id,created_at,updated_at,started_at,stopped_at,scenario)
VALUES (3000,now()-interval '23 hours 31 minutes',now(),now(),now(),'aboutme/http-status');
INSERT INTO public.decisions (id,created_at,updated_at,until,scenario,type,scope,value,origin,alert_decisions)
SELECT id,now(),now(),now()+interval '1 hour','aboutme/http-status','ban','Ip','192.0.2.20','crowdsec',3000
FROM generate_series(9001,9999) id;
SQL
  check_hosted exact-thousand-graph-includes-every-decision run_retention runtime
  [[ $(retention_fixture_sql <<<'SELECT count(*) FROM public.decisions WHERE alert_decisions=3000;') == 0 ]]
  [[ $(retention_fixture_sql <<<'SELECT count(*) FROM public.alerts WHERE id=3000;') == 0 ]]
  [[ $(tail -n1 "$retention_fixture/batch-counts") == 1000 ]]

  retention_fixture_sql >/dev/null <<'SQL'
INSERT INTO public.alerts (id,created_at,updated_at,started_at,stopped_at,scenario)
VALUES (3000,now()-interval '23 hours 31 minutes',now(),now(),now(),'aboutme/http-status');
INSERT INTO public.decisions (id,created_at,updated_at,until,scenario,type,scope,value,origin,alert_decisions)
SELECT id,now(),now(),now()+interval '1 hour','aboutme/http-status','ban','Ip','192.0.2.20','crowdsec',3000
FROM generate_series(9001,10000) id;
SQL
  check_hosted oversized-decision-graph-refuses refused run_retention runtime
  [[ $(retention_fixture_sql <<<'SELECT count(*) FROM public.decisions WHERE alert_decisions=3000;') == 1000 ]]
  [[ $(retention_fixture_sql <<<'SELECT count(*) FROM public.alerts WHERE id=3000;') == 1 ]]
  retention_fixture_sql >/dev/null <<'SQL'
DELETE FROM public.alerts WHERE id=3000;
SQL
  check_hosted recover-after-oversized-decision-graph run_retention startup
  printf 'active\n' >"$retention_fixture/consumer-state"

  retention_fixture_sql >/dev/null <<'SQL'
INSERT INTO public.alerts (id,created_at,updated_at,started_at,stopped_at,scenario)
VALUES (3000,now()-interval '23 hours 31 minutes',now(),now(),now(),'aboutme/http-status');
SQL
  touch "$retention_fixture/change-candidate"
  check_hosted changed-candidate-refuses-deletion refused run_retention runtime
  [[ $(retention_fixture_sql <<<'SELECT count(*) FROM public.alerts WHERE id=3000;') == 1 ]]
  [[ ! -e /run/aboutme/crowdsec-retention-ok ]]
  check_hosted recover-after-candidate-change run_retention startup

  for column in created_at updated_at started_at stopped_at; do
    retention_fixture_sql >/dev/null <<<"UPDATE public.alerts SET $column='infinity' WHERE id=3000;"
    check_hosted nonfinite-alert-time-refuses refused run_retention runtime
    [[ ! -e /run/aboutme/crowdsec-retention-ok ]]
    retention_fixture_sql >/dev/null <<<"UPDATE public.alerts SET $column=now() WHERE id=3000;"
    check_hosted recover-after-nonfinite-time run_retention startup
  done
  retention_fixture_sql >/dev/null <<'SQL'
UPDATE public.alerts SET started_at=NULL WHERE id=3000;
SQL
  check_hosted missing-time-refuses refused run_retention runtime
  retention_fixture_sql >/dev/null <<'SQL'
UPDATE public.alerts SET started_at=now() WHERE id=3000;
SQL
  check_hosted recover-after-missing-time run_retention startup

  retention_fixture_sql >/dev/null <<'SQL'
INSERT INTO public.decisions (created_at,updated_at,until,scenario,type,scope,value,origin,alert_decisions)
VALUES (now(),now(),now()+interval '23 hours','aboutme/http-status','ban','Ip','192.0.2.20','crowdsec',2000);
SQL
  check_hosted excessive-decision-duration-refuses refused run_retention runtime
  retention_fixture_sql >/dev/null <<'SQL'
DELETE FROM public.decisions;
SQL
  check_hosted recover-after-invalid-duration run_retention startup
  retention_fixture_admin >/dev/null <<'SQL'
ALTER TABLE public.events DISABLE TRIGGER ALL;
INSERT INTO public.events (created_at,updated_at,time,serialized,alert_events)
VALUES (now(),now(),now(),'{}',999999);
ALTER TABLE public.events ENABLE TRIGGER ALL;
SQL
  check_hosted dangling-child-refuses refused run_retention runtime
  retention_fixture_sql >/dev/null <<'SQL'
DELETE FROM public.events WHERE alert_events=999999;
SQL
  check_hosted recover-after-dangling-child run_retention startup

  for relation in events meta; do
    columns=(created_at updated_at)
    [[ $relation != events ]] || columns+=(time)
    for column in "${columns[@]}"; do
      retention_fixture_sql >/dev/null <<<"UPDATE public.$relation SET $column='-infinity';"
      check_hosted "nonfinite-$relation-$column-refuses" refused run_retention runtime
      retention_fixture_sql >/dev/null <<<"UPDATE public.$relation SET $column=now();"
      check_hosted "recover-$relation-$column" run_retention startup
    done
  done
  retention_fixture_sql >/dev/null <<'SQL'
INSERT INTO public.decisions (created_at,updated_at,until,scenario,type,scope,value,origin,alert_decisions)
VALUES (now(),now(),now()+interval '1 hour','aboutme/http-status','ban','Ip','192.0.2.20','crowdsec',2000);
SQL
  for column in created_at updated_at until; do
    retention_fixture_sql >/dev/null <<<"UPDATE public.decisions SET $column='infinity';"
    check_hosted "nonfinite-decision-$column-refuses" refused run_retention runtime
    retention_fixture_sql >/dev/null <<'SQL'
UPDATE public.decisions SET created_at=now(),updated_at=now(),until=now()+interval '1 hour';
SQL
    check_hosted "recover-decision-$column" run_retention startup
  done
  retention_fixture_sql >/dev/null <<'SQL'
DELETE FROM public.decisions;
SQL

  retention_fixture_admin >/dev/null <<'SQL'
ALTER TABLE public.events RENAME COLUMN serialized TO unexpected;
SQL
  check_hosted manifest-mismatch-refuses-retention refused run_retention runtime
  [[ ! -e /run/aboutme/crowdsec-retention-ok ]]
  [[ $(retention_fixture_admin <<<'SELECT count(*) FROM information_schema.columns WHERE table_schema='\''public'\'' AND table_name='\''events'\'' AND column_name='\''unexpected'\'';') == 1 ]]
  retention_fixture_admin >/dev/null <<'SQL'
ALTER TABLE public.events RENAME COLUMN unexpected TO serialized;
SQL
  check_hosted recover-after-schema-mismatch run_retention startup

  retention_fixture_admin >"$retention_fixture/database-lock-ready" <<'SQL' &
BEGIN;
LOCK TABLE public.alerts IN ACCESS EXCLUSIVE MODE;
SELECT 'ready';
SELECT pg_sleep(8);
ROLLBACK;
SQL
  database_lock_holder=$!
  for _ in $(seq 50); do
    [[ -s $retention_fixture/database-lock-ready ]] && break
    sleep 0.1
  done
  [[ -s $retention_fixture/database-lock-ready ]]
  before_lock_check=$SECONDS
  check_hosted real-database-lock-timeout-refuses refused run_retention runtime
  ((SECONDS - before_lock_check < 8))
  wait "$database_lock_holder"
  check_hosted recover-after-database-lock run_retention startup

  check_hosted real-ordinary-vacuum /usr/local/lib/aboutme/crowdsec-vacuum.sh
  [[ $(retention_fixture_sql <<<'SELECT count(*) FROM pg_stats WHERE schemaname='\''public'\'';') == 0 ]]
  printf 'PASS ordinary-vacuum-keeps-planner-state-empty\n'
  exec {held_vacuum}>/run/aboutme/crowdsec-vacuum.lock
  flock -x "$held_vacuum"
  check_hosted vacuum-lock-refuses-overlap refused /usr/local/lib/aboutme/crowdsec-vacuum.sh
  flock -u "$held_vacuum"
  exec {held_vacuum}>&-
  exec {held_retention}>/run/aboutme/crowdsec-retention.lock
  flock -x "$held_retention"
  check_hosted retention-lock-contention-fails-closed refused run_retention runtime
  [[ ! -e /run/aboutme/crowdsec-retention-ok ]]
  flock -u "$held_retention"
  exec {held_retention}>&-
  check_hosted recover-after-lock-contention run_retention startup
  touch "$retention_fixture/clock-failed"
  check_hosted clock-failure-closes-gate refused run_retention runtime
  [[ ! -e /run/aboutme/crowdsec-retention-ok && ! -e /run/aboutme/caddy-log/crowdsec ]]
  rm "$retention_fixture/clock-failed"
  check_hosted recover-after-clock-failure run_retention startup

  runuser -u postgres -- /usr/lib/postgresql/18/bin/pg_ctl -D "$retention_fixture/data" \
    -m fast -w stop >/dev/null 2>&1
  touch "$retention_fixture/data/standby.signal"
  runuser -u postgres -- /usr/lib/postgresql/18/bin/pg_ctl -D "$retention_fixture/data" \
    -l "$retention_fixture/data/server.log" -w start >/dev/null 2>&1
  [[ $(retention_fixture_admin <<<'SELECT pg_is_in_recovery();') == t ]]
  check_hosted real-recovery-refuses-current-lease refused lease_check
  check_hosted real-recovery-refuses-retention refused run_retention startup
  [[ ! -e /run/aboutme/crowdsec-retention-ok ]]
  printf 'PASS retention-postgresql-host-control-boundaries\n'
  exit 0
fi

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
if [[ ${1-} == show ]]; then
  state=inactive
  [[ ${ABOUTME_TEST_STILL_ACTIVE:-0} != 1 ]] || state=active
  printf 'ActiveState=%s\nJob=\nMainPID=0\nControlPID=0\nControlGroup=\n' "$state"
  exit 0
fi
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
