#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
watch_source=$repo_root/deploy/vn/host/bin/watch.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/lib"
# Only the canonical lock path and wait duration are redirected in this fixture.
sed -e "s|/var/lib/aboutme/fence.lock|$work/fence.lock|g" \
  -e "s|/run/aboutme/crowdsec-retention.lock|$work/retention.lock|g" \
  -e 's/flock -s -w 30 /flock -s -w 0 /' \
  -e 's/timeout --kill-after=1s 5 cscli/timeout --kill-after=1s 0.2 cscli/' \
  "$watch_source" >"$work/watch.sh"
chmod +x "$work/watch.sh"
watch=$work/watch.sh
: >"$work/fence.lock"
: >"$work/retention.lock"
chmod 0600 "$work/retention.lock"
grep -qF 'flock -s -n' "$watch_source"
cat >"$work/lib/crowdsec-db.sh" <<'STUB'
crowdsec_db_held_lock() { return 0; }
crowdsec_db_ready() {
  if [[ ${WATCH_TEST_IGNORE_TERM_PREFLIGHT:-0} == 1 ]]; then
    timeout 15 bash -c 'trap "" TERM; : >"$WATCH_TEST_PREFLIGHT_ENTERED"; while :; do sleep 1; done'
  fi
  [[ ${WATCH_TEST_DATABASE_CHANGED:-0} == 0 && ${WATCH_TEST_DATABASE_RECOVERY:-0} == 0 ]]
}
STUB

cat >"$work/lib/alert.sh" <<'STUB'
#!/usr/bin/env bash
printf '%s|%s\n' "$1" "$2" >>"$WATCH_TEST_ALERTS"
cat >/dev/null
STUB
cat >"$work/lib/crowdsec-retention-fail.sh" <<'STUB'
#!/usr/bin/env bash
printf 'retention failure\n' >>"$WATCH_TEST_RETENTION_FAILURES"
STUB
cat >"$work/lib/crowdsec-retention-lease-check.sh" <<'STUB'
#!/usr/bin/env bash
crowdsec_lease_check() {
for timer in aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer; do
  systemctl is-enabled --quiet "$timer" || return 1
  systemctl is-active --quiet "$timer" || return 1
done
lease=${ABOUTME_RETENTION_SUCCESS_MARKER:?}
[[ -f $lease && ! -L $lease ]] || return 1
modified=$(stat -c %Y "$lease")
((modified <= EPOCHSECONDS && EPOCHSECONDS - modified <= 360)) || return 1
[[ ${WATCH_TEST_DATABASE_CHANGED:-0} == 0 && ${WATCH_TEST_DATABASE_RECOVERY:-0} == 0 ]]
}
STUB
cat >"$work/bin/df" <<'STUB'
#!/usr/bin/env bash
printf 'Use%%\n10%%\n'
STUB
cat >"$work/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
if [[ ${1-} == is-enabled && ${WATCH_TEST_TIMER_DISABLED:-0} == 1 ]]; then
  exit 1
fi
if [[ ${1-} == is-active && ${WATCH_TEST_TIMER_INACTIVE:-0} == 1 ]]; then
  exit 1
fi
if [[ $* == *vacuum.timer* ]]; then
  [[ ${1-} != is-enabled || ${WATCH_TEST_VACUUM_TIMER_DISABLED:-0} == 0 ]] || exit 1
  [[ ${1-} != is-active || ${WATCH_TEST_VACUUM_TIMER_INACTIVE:-0} == 0 ]] || exit 1
fi
exit 0
STUB
cat >"$work/bin/runuser" <<'STUB'
#!/usr/bin/env bash
case " $* " in
  *' pg_isready '*) exit 0 ;;
  *' psql '*) printf 'f\n' ;;
  *' pgbackrest '*)
    if [[ ${WATCH_TEST_HUNG_BACKUP:-0} == 1 ]]; then
      : >"$WATCH_TEST_BACKUP_ENTERED"
      while [[ ! -e $WATCH_TEST_BACKUP_RELEASE ]]; do sleep 0.01; done
    fi
    printf '[{"backup":[{"timestamp":{"stop":2000000000}}]}]\n' ;;
  *) exit 2 ;;
esac
STUB
cat >"$work/bin/date" <<'STUB'
#!/usr/bin/env bash
[[ ${1-} == +%s ]] && printf '2000000000\n'
STUB
cat >"$work/bin/cscli" <<'STUB'
#!/usr/bin/env bash
if [[ ${WATCH_TEST_HUNG_CSCLI:-0} == 1 ]]; then
  : >"$WATCH_TEST_CSCLI_ENTERED"
  sleep 30
fi
printf '[]\n'
STUB
cat >"$work/bin/find" <<'STUB'
#!/usr/bin/env bash
if [[ ${WATCH_TEST_NO_WAF_LOGS:-0} == 0 && $* == *serving-match.log* ]]; then
  printf '2000000000 %s\n' "$WATCH_TEST_FIXTURE"
fi
STUB
cat >"$work/bin/journalctl" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
chmod +x "$work/bin/"* "$work/lib/"*.sh
export PATH=$work/bin:$PATH ABOUTME_WATCH_TEST_LIB=$work/lib
export ABOUTME_RETENTION_SUCCESS_MARKER=$work/retention-ok
: >"$ABOUTME_RETENTION_SUCCESS_MARKER"

run_watch() { # fixture alerts
  export WATCH_TEST_FIXTURE=$1 WATCH_TEST_ALERTS=$2 WATCH_TEST_RETENTION_FAILURES=$work/retention-failures
  : >"$WATCH_TEST_ALERTS"
  : >"$WATCH_TEST_RETENTION_FAILURES"
  "$watch" >"$WATCH_TEST_ALERTS.journal"
  [[ ! -s $WATCH_TEST_RETENTION_FAILURES ]] || { echo "fresh retention lease failed" >&2; exit 1; }
  sort -o "$WATCH_TEST_ALERTS" "$WATCH_TEST_ALERTS"
}

base=$work/base.log
with_diagnostic=$work/with-diagnostic.log
cat >"$base" <<'EOF'
{"ts":1999999999,"level":"warn","logger":"http.handlers.waf","msg":"waf_rule_match","rule_id":1001}
{"ts":1999999999,"level":"warn","logger":"http.handlers.waf","msg":"waf_rule_match_unparsed"}
{"ts":1999999999,"level":"warn","logger":"http.handlers.waf","msg":"unexpected"}
{"ts":1999996399,"level":"error","logger":"http.handlers.waf","msg":"waf_rule_match","rule_id":1002}
{"ts":1999996399,"level":"error","logger":"http.handlers.waf","msg":"waf_rule_match_unparsed"}
EOF
{
  cat "$base"
  printf '%s\n' '{"ts":1999999999,"level":"error","logger":"http.handlers.waf","msg":"waf_engine_diagnostic"}'
} >"$with_diagnostic"

run_watch "$base" "$work/base.alerts"

# A hung LAPI call must release both locks without waiting for the unit deadline.
WATCH_TEST_HUNG_CSCLI=1 WATCH_TEST_CSCLI_ENTERED=$work/cscli-entered \
  WATCH_TEST_FIXTURE=$base WATCH_TEST_ALERTS=$work/hung.alerts \
  WATCH_TEST_RETENTION_FAILURES=$work/hung.failures "$watch" >"$work/hung.journal" &
watch_pid=$!
for ((i=0; i<100; i++)); do
  [[ ! -e $work/cscli-entered ]] || break
  sleep 0.01
done
[[ -e $work/cscli-entered ]] || { echo 'hung cscli fixture did not start' >&2; exit 1; }
if flock -n "$work/retention.lock" true; then
  echo 'watch query overlapped an edge transition' >&2; exit 1
fi
timeout 3 flock -x "$work/fence.lock" true || {
  echo 'hung watch retained the fence lock' >&2; exit 1;
}
wait "$watch_pid"
flock -n "$work/retention.lock" true

# A nested preflight that ignores TERM must not escape the query's process group
# and retain either inherited safety descriptor after the whole query deadline.
sed 's/timeout --kill-after=1s 20 /timeout --kill-after=0.1s 0.5 /' "$watch" >"$work/watch-preflight.sh"
WATCH_TEST_IGNORE_TERM_PREFLIGHT=1 WATCH_TEST_PREFLIGHT_ENTERED=$work/preflight-entered \
  WATCH_TEST_FIXTURE=$base WATCH_TEST_ALERTS=$work/preflight.alerts \
  WATCH_TEST_RETENTION_FAILURES=$work/preflight.failures \
  timeout 3 bash "$work/watch-preflight.sh" >"$work/preflight.journal"
[[ -e $work/preflight-entered ]] || { echo 'TERM-ignoring preflight did not run' >&2; exit 1; }
grep -qxF 'retention failure' "$work/preflight.failures"
flock -n "$work/fence.lock" true
flock -n "$work/retention.lock" true

# An unrelated blocked backup check must not retain either safety lock.
WATCH_TEST_HUNG_BACKUP=1 WATCH_TEST_BACKUP_ENTERED=$work/backup-entered \
  WATCH_TEST_BACKUP_RELEASE=$work/backup-release WATCH_TEST_FIXTURE=$base \
  WATCH_TEST_ALERTS=$work/backup.alerts WATCH_TEST_RETENTION_FAILURES=$work/backup.failures \
  "$watch" >"$work/backup.journal" &
watch_pid=$!
for ((i=0; i<100; i++)); do
  [[ ! -e $work/backup-entered ]] || break
  sleep 0.01
done
[[ -e $work/backup-entered ]] || { echo 'blocked backup fixture did not start' >&2; exit 1; }
flock -n "$work/fence.lock" true
flock -n "$work/retention.lock" true
touch "$work/backup-release"
wait "$watch_pid"

run_watch "$with_diagnostic" "$work/diagnostic.alerts"
cmp "$work/base.alerts" "$work/diagnostic.alerts"
grep -qxF 'coraza-log-invalid|the Coraza match log has 1 unknown records in the last hour' "$work/diagnostic.alerts"
[[ $(wc -l <"$work/diagnostic.alerts") == 1 ]] || {
  echo "the watcher raised an unexpected alert" >&2
  exit 1
}
cmp "$work/base.alerts.journal" "$work/diagnostic.alerts.journal"
grep -qxF 'coraza counts in the last hour: matches=1 unparsed=1' "$work/diagnostic.alerts.journal"
[[ $(wc -l <"$work/diagnostic.alerts.journal") == 1 ]] || {
  echo "the watcher did not write exactly one Coraza count line" >&2
  exit 1
}

noise=$work/noise.log
for ((i=0; i<280; i++)); do
  printf '%s\n' '{"ts":1999999999,"level":"error","logger":"http.handlers.waf","msg":"waf_rule_match","rule_id":1001}'
done >"$noise"
printf '%s\n' '{"ts":1999999999,"level":"error","logger":"http.handlers.waf","msg":"waf_rule_match_unparsed"}' >>"$noise"
run_watch "$noise" "$work/noise.alerts"
[[ ! -s $work/noise.alerts ]] || { echo "Coraza noise triggered mail" >&2; exit 1; }
grep -qxF 'coraza counts in the last hour: matches=280 unparsed=1' "$work/noise.alerts.journal"

: >"$work/empty.log"
run_watch "$work/empty.log" "$work/empty.alerts"
[[ ! -s $work/empty.alerts ]] || { echo "an empty Coraza log triggered mail" >&2; exit 1; }
grep -qxF 'coraza counts in the last hour: matches=0 unparsed=0' "$work/empty.alerts.journal"

WATCH_TEST_NO_WAF_LOGS=1 run_watch "$base" "$work/missing.alerts"
[[ ! -s $work/missing.alerts ]] || { echo "absent Coraza logs triggered mail" >&2; exit 1; }
grep -qxF 'coraza counts in the last hour: matches=0 unparsed=0' "$work/missing.alerts.journal"

printf '%s\n' 'not-json' >"$work/malformed.log"
run_watch "$work/malformed.log" "$work/malformed.alerts"
grep -qxF 'coraza-log-invalid|the Coraza match log is malformed' "$work/malformed.alerts"
[[ $(wc -l <"$work/malformed.alerts") == 1 ]] || { echo "malformed Coraza log alerts changed" >&2; exit 1; }

touch -d '7 minutes ago' "$ABOUTME_RETENTION_SUCCESS_MARKER"
WATCH_TEST_FIXTURE=$base \
  WATCH_TEST_ALERTS=$work/stale.alerts WATCH_TEST_RETENTION_FAILURES=$work/stale.failures \
  "$watch"
grep -qxF 'retention failure' "$work/stale.failures" || {
  echo "stale retention lease did not fail closed" >&2
  exit 1
}
: >"$ABOUTME_RETENTION_SUCCESS_MARKER"

WATCH_TEST_TIMER_DISABLED=1 WATCH_TEST_FIXTURE=$base \
  WATCH_TEST_ALERTS=$work/disabled.alerts WATCH_TEST_RETENTION_FAILURES=$work/disabled.failures \
  "$watch"
grep -qxF 'retention failure' "$work/disabled.failures" || {
  echo "disabled retention timer did not fail closed" >&2
  exit 1
}

WATCH_TEST_TIMER_INACTIVE=1 WATCH_TEST_FIXTURE=$base \
  WATCH_TEST_ALERTS=$work/inactive.alerts WATCH_TEST_RETENTION_FAILURES=$work/inactive.failures \
  "$watch"
grep -qxF 'retention failure' "$work/inactive.failures" || {
  echo "inactive retention timer did not fail closed" >&2
  exit 1
}

for state in disabled inactive; do
  if [[ $state == disabled ]]; then
    export WATCH_TEST_VACUUM_TIMER_DISABLED=1 WATCH_TEST_VACUUM_TIMER_INACTIVE=0
  else
    export WATCH_TEST_VACUUM_TIMER_DISABLED=0 WATCH_TEST_VACUUM_TIMER_INACTIVE=1
  fi
  WATCH_TEST_FIXTURE=$base \
    WATCH_TEST_ALERTS=$work/vacuum-$state.alerts WATCH_TEST_RETENTION_FAILURES=$work/vacuum-$state.failures \
    "$watch"
  grep -qxF 'retention failure' "$work/vacuum-$state.failures" || {
    echo "vacuum safety timer did not invalidate the watcher lease" >&2
    exit 1
  }
done
unset WATCH_TEST_VACUUM_TIMER_DISABLED WATCH_TEST_VACUUM_TIMER_INACTIVE

for state in changed recovery; do
  if [[ $state == changed ]]; then
    export WATCH_TEST_DATABASE_CHANGED=1 WATCH_TEST_DATABASE_RECOVERY=0
  else
    export WATCH_TEST_DATABASE_CHANGED=0 WATCH_TEST_DATABASE_RECOVERY=1
  fi
  WATCH_TEST_FIXTURE=$base \
    WATCH_TEST_ALERTS=$work/database-$state.alerts WATCH_TEST_RETENTION_FAILURES=$work/database-$state.failures \
    "$watch"
  grep -qxF 'retention failure' "$work/database-$state.failures" || {
    echo "database epoch or recovery did not invalidate the watcher lease" >&2
    exit 1
  }
done
unset WATCH_TEST_DATABASE_CHANGED WATCH_TEST_DATABASE_RECOVERY

# A held completion lock refuses the scheduled tick before any failure body.
: >"$work/admission.failures"
exec {guard}>"$work/fence.lock"
flock -x "$guard"
if WATCH_TEST_ALERTS=$work/admission.alerts WATCH_TEST_RETENTION_FAILURES=$work/admission.failures "$watch" >"$work/admission.out" 2>&1; then
  echo 'FAIL watchdog-lock-admission'; exit 1
fi
[[ ! -s $work/admission.failures ]]
flock -u "$guard"

# A tick during any edge mutation waits before its safety or failure body.
for transition in maintenance-up app-down render restore-previous app-up maintenance-down fail-closed; do
  : >"$work/transition.failures"
  exec {edge_guard}>"$work/retention.lock"
  flock -x "$edge_guard"
  WATCH_TEST_FIXTURE=$base WATCH_TEST_ALERTS=$work/transition.alerts \
    WATCH_TEST_RETENTION_FAILURES=$work/transition.failures "$watch" >"$work/transition.out" 2>&1
  [[ ! -s $work/transition.failures ]] || { echo "fresh lease failed during $transition" >&2; exit 1; }
  flock -u "$edge_guard"
  exec {edge_guard}>&-
done

exec {stuck_guard}>"$work/retention.lock"
flock -x "$stuck_guard"
touch -d '7 minutes ago' "$ABOUTME_RETENTION_SUCCESS_MARKER"
WATCH_TEST_FIXTURE=$base WATCH_TEST_ALERTS=$work/stuck.alerts \
  WATCH_TEST_RETENTION_FAILURES=$work/stuck.failures "$watch" >"$work/stuck.out" 2>&1
grep -qxF 'retention failure' "$work/stuck.failures" || {
  echo 'stuck retention lock suppressed the expired-lease watchdog' >&2; exit 1;
}
flock -u "$stuck_guard"
exec {stuck_guard}>&-
echo "watch tests passed"
