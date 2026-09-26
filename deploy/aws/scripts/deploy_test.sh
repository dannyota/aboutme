#!/usr/bin/env bash
# Checks deploy.sh step order and failure recovery against stubbed commands.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Real certificates for edge_origin_cert_check (edge.sh), which runs real
# openssl against whatever the stubbed ssm get-parameter prints: one far from
# expiry (the default answer, so every case not testing this check still
# passes) and one inside the 21-day guard window.
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 100 \
  -subj /CN=aboutme.vn -keyout "$work/origin-cert-100.key" -out "$work/origin-cert-100.pem" 2>/dev/null
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 10 \
  -subj /CN=aboutme.vn -keyout "$work/origin-cert-10.key" -out "$work/origin-cert-10.pem" 2>/dev/null
# The stored origin key fingerprint the stub returns by default: the SHA-256
# of the 100-day certificate's public key, as tls.sh writes it.
openssl x509 -in "$work/origin-cert-100.pem" -noout -pubkey | openssl pkey -pubin -outform DER |
  openssl dgst -sha256 -r | cut -d' ' -f1 >"$work/origin-cert-100.sha256"

mkdir -p "$work/bin"
for cmd in aws gh git curl date; do
  cat >"$work/bin/$cmd" <<'STUB'
#!/usr/bin/env bash
printf '[%s] %s %s\n' "${AWS_PROFILE:-none}" "$(basename "$0")" "$*" >>"$CALLS"
exec bash "$STUB_DIR/respond" "$(basename "$0")" "$@"
STUB
  chmod +x "$work/bin/$cmd"
done
cp "$here/testdata/respond" "$work/respond"

run_case() { # name expected-exit|fail args...
  local name=$1 want=$2 got
  shift 2
  : >"$work/$name.calls"
  set +e
  # Backgrounded so its own PID (not a subshell bash forks internally for a
  # command substitution) lands in $name.pid: a stub that signals deploy.sh
  # from inside its own cleanup wait needs that exact PID, since a signal to
  # the wrong process only kills a disposable subshell there.
  CALLS="$work/$name.calls" STUB_DIR="$work" STUB_CASE="$name" PATH="$work/bin:$PATH" \
    AWS_CONFIG_FILE="$work/no-such-aws-config" AWS_PROFILE=test-base \
    DEPLOY_SMOKE_TIMEOUT=1 DEPLOY_SMOKE_DELAY=0 \
    DEPLOY_ALARM_WAIT="${DEPLOY_ALARM_WAIT:-3}" DEPLOY_ALARM_POLL="${DEPLOY_ALARM_POLL:-0}" \
    DEPLOY_WARM_ATTEMPTS="${DEPLOY_WARM_ATTEMPTS:-8}" DEPLOY_HANDOFF_HOLD=0 \
    bash "$here/deploy.sh" "$@" >"$work/$name.out" 2>&1 &
  echo $! >"$work/$name.pid"
  wait $!
  got=$?
  set -e
  if [[ $want == fail ]] && ((got != 0 && got != 2)); then
    return
  fi
  if [[ $want == fail ]] || ((got != want)); then
    echo "$name: exit $got, want $want" >&2
    cat "$work/$name.out" >&2
    exit 1
  fi
}

line() { { grep -n -m1 -F -- "$2" "$1" || true; } | cut -d: -f1; }

before() { # file first second
  local a b
  a=$(line "$1" "$2")
  b=$(line "$1" "$3")
  if [[ -z $a || -z $b || $a -ge $b ]]; then
    echo "$(basename "$1"): '$2' must precede '$3'" >&2
    exit 1
  fi
}

absent() { # file text
  if grep -q -F -- "$2" "$1"; then
    echo "$(basename "$1"): unexpected '$2'" >&2
    exit 1
  fi
}

# Regex-anchored variants of line/before, for a call whose URL is a substring
# of another call's URL (the homepage warm probe "https://aboutme.vn/" is a
# prefix of the resume-page probe "https://aboutme.vn/danny").
line_re() { { grep -n -m1 -E -- "$2" "$1" || true; } | cut -d: -f1; }
before_re() { # file first-regex second-regex
  local a b
  a=$(line_re "$1" "$2")
  b=$(line_re "$1" "$3")
  if [[ -z $a || -z $b || $a -ge $b ]]; then
    echo "$(basename "$1"): '$2' must precede '$3'" >&2
    exit 1
  fi
}

count() { grep -c -F -- "$2" "$1" || true; }

# nth prints the line number of the N'th match of TEXT in FILE, or empty.
nth() { { grep -n -F -- "$2" "$1" || true; } | sed -n "${3}p" | cut -d: -f1; }

stop_app="--service aboutme-prod-app --desired-count 0"
app_point="--service aboutme-prod-app --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/new:4"
app_up1="--service aboutme-prod-app --desired-count 1"
app_confirm="--tasks arn:aws:ecs:ap-southeast-1:1:task/aboutme-prod/task-aboutme-prod-app --output json"
start_web="--service aboutme-prod-web --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/new:4 --desired-count 1"
maint_up1="--service aboutme-prod-maintenance --desired-count 1"
down_maintenance="--service aboutme-prod-maintenance --desired-count 0"
# A stopped service pointed at a new revision and scaled up in the same call:
# the old-revision regression (a bare "update-service --task-definition X
# --desired-count 1" starts a task of the previous revision first, see
# handoff.sh's start_service). Neither app nor maintenance ever starts this
# way from a stop.
combined_app_start="--service aboutme-prod-app --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/new:4 --desired-count 1"
combined_maint_start="--service aboutme-prod-maintenance --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/new:4 --desired-count 1"
# The previous app is already the app's own running (or last-set) revision in
# every restore scenario here, so restoring it only ever asks for
# desired-count 1: the same literal a fresh app start's own final call uses.
# The two never appear in the same run (see the case comments below).
prev_app_up="$app_up1"
# Aliases for call sites below that only care whether the service reached its
# target count, not whether that came with a revision change in the same
# call.
start_app="$app_up1"
up_maintenance="$maint_up1"
warm_danny_re='-w %\{http_code\} %\{time_total\} https://aboutme\.vn/danny$'
warm_home_re='-w %\{http_code\} %\{time_total\} https://aboutme\.vn/$'

before_ok_epoch=$(date -u +%s)
run_case ok 0 v0.1.0
f=$work/ok.calls
before "$f" "rds create-db-snapshot" "$stop_app"
before "$f" "scheduler update-schedule" "$stop_app"
before "$f" "$stop_app" "--started-by deploy-migrate"
before "$f" "--started-by deploy-migrate" "$app_point"
before "$f" "$start_web" "$app_point"
# The app is pointed at the new revision before it is scaled up: a combined
# point-and-scale call from a stop would risk starting a task of the previous
# revision first (handoff.sh's start_service).
before "$f" "$app_point" "$app_up1"
# Both Caddy services use host networking with no ECS port mapping and bind
# 443 with SO_REUSEPORT (docs/design/single-host-production.md, "Deploy"), so
# each handoff starts the incoming service, beside the outgoing one, and
# confirms it before the outgoing one stops: maintenance up before the app
# stops, and the new app confirmed running before maintenance stops.
before "$f" "$maint_up1" "$stop_app"
before "$f" "$maint_up1" "--started-by deploy-migrate"
before "$f" "$app_up1" "$down_maintenance"
before "$f" "$app_confirm" "$down_maintenance"
absent "$f" "deploy-db-setup"
# The CloudFront distribution check (edge.sh) runs on every deploy, as the
# base caller.
grep -qF "[test-base] aws cloudfront list-distributions --output json" "$f" ||
  { echo "ok: the distribution check did not run as the base caller" >&2; exit 1; }
absent "$f" "$combined_app_start"
absent "$f" "$combined_maint_start"
[[ $(count "$f" "scheduler update-schedule") == 8 ]] || { echo "ok: want 8 schedule updates" >&2; exit 1; }
# The 4 disables run before either service changes; the 4 enables run only
# after maintenance stops.
down_line=$(nth "$f" "$down_maintenance" 1)
enable_line=$(nth "$f" "scheduler update-schedule" 5)
[[ -n $down_line && -n $enable_line && $down_line -lt $enable_line ]] ||
  { echo "ok: maintenance did not stop before the schedules were re-enabled" >&2; exit 1; }
grep -q '"State": "ENABLED"' "$work/last-schedule.json" || { echo "ok: schedules not enabled at the end" >&2; exit 1; }
jq -e '.Target.EcsParameters.TaskDefinitionArn == "arn:aws:ecs:ap-southeast-1:1:task-definition/new:4"' \
  "$work/last-schedule.json" >/dev/null || { echo "ok: schedules not pinned to the released revision" >&2; exit 1; }

# Planned service handoffs suppress only the site-down alarm actions and the
# stopped-task EventBridge notification rule. Other alarms stay live.
before "$f" "events disable-rule --name aboutme-prod-task-stopped" "$stop_app"
before "$f" "cloudwatch disable-alarm-actions --alarm-names aboutme-prod-site-down" "$stop_app"
before "$f" "$start_app" "events enable-rule --name aboutme-prod-task-stopped"
before "$f" "$start_app" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down"
[[ $(count "$f" "events disable-rule --name aboutme-prod-task-stopped") == 1 ]] ||
  { echo "ok: task-stopped rule was not disabled once" >&2; exit 1; }
[[ $(count "$f" "cloudwatch disable-alarm-actions --alarm-names aboutme-prod-site-down") == 1 ]] ||
  { echo "ok: site-down actions were not disabled once" >&2; exit 1; }
[[ $(count "$f" "host-recover") == 0 ]] || { echo "ok: touched host recovery" >&2; exit 1; }

# The operation lock releases only after notifications are restored, and only
# once, with the exact operation ID this run acquired.
before "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down" "REMOVE operation_id"
[[ $(count "$f" "REMOVE operation_id") == 1 ]] || { echo "ok: lock released more than once" >&2; exit 1; }
grep -qF ":o\":{\"S\":\"$(cat "$work/lock-oid")\"" "$f" ||
  { echo "ok: lock was not released with its own operation ID" >&2; exit 1; }

# A checkpoint runs before every mutating call that starts an image: the
# snapshot, the register loop, both notification-suppression calls, 4
# schedule disables, the migration, the web and app starts, and 4 schedule
# enables. The maintenance start is never checkpointed (deploy.sh's own
# comment on maintenance_up explains why).
# "SET operation_checked_at=:s --condition-expression" is unique to a
# checkpoint call; the lock acquisition's SET clause also touches that
# attribute but continues with more fields before its own condition.
[[ $(count "$f" "SET operation_checked_at=:s --condition-expression") == 15 ]] ||
  { echo "ok: want 15 fence checkpoints, one per mutating image start" >&2; exit 1; }

# Every mutation runs as the deploy role; only the fence's own strong read
# runs as the operator role.
absent "$f" "[fence-operator] aws --region ap-southeast-1 ecs"
absent "$f" "[fence-operator] aws --region ap-southeast-1 dynamodb update-item"
absent "$f" "[fence-operator] aws --region us-east-1"
grep -qF "[fence-deploy] aws --region ap-southeast-1 ecs register-task-definition" "$f" ||
  { echo "ok: task registration did not run as the deploy role" >&2; exit 1; }
grep -F -- "$start_app" "$f" | grep -qF "[fence-deploy]" ||
  { echo "ok: the app start did not run as the deploy role" >&2; exit 1; }
grep -qF "[fence-operator] aws --region ap-southeast-1 dynamodb get-item" "$f" ||
  { echo "ok: the fence read did not run as the operator role" >&2; exit 1; }

# The site-down alarm's actions come back only once the task-stopped rule is
# already re-enabled, the alarm itself reads OK, and Route 53 has reported
# enough healthy post-up minutes; the metric read runs as the base caller,
# not a role, because the deploy role's closed CloudWatch list has no
# GetMetricStatistics (deploy/aws/modules/identity/main.tf).
before "$f" "events enable-rule --name aboutme-prod-task-stopped" "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json"
before "$f" "cloudwatch get-metric-statistics" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down"
before "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down" "REMOVE operation_id"
grep -qF "[test-base] aws --region us-east-1 cloudwatch get-metric-statistics" "$f" ||
  { echo "ok: the alarm metric read did not run as the base caller" >&2; exit 1; }
absent "$f" "[fence-deploy] aws --region us-east-1 cloudwatch get-metric-statistics"
start_time=$(sed -E 's/.*--start-time ([0-9TZ:-]+).*/\1/' < <(grep -m1 -F "cloudwatch get-metric-statistics" "$f"))
[[ $start_time =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:00Z$ ]] ||
  { echo "ok: the metric read's start time '$start_time' is not rounded to a whole minute" >&2; exit 1; }
start_epoch=$(date -u -d "$start_time" +%s)
# Rounding up to the next whole minute only ever moves the start time later
# than the moment the site came up, never earlier, so it is always at or
# after this run's own start.
((start_epoch >= before_ok_epoch && start_epoch <= before_ok_epoch + 120)) ||
  { echo "ok: the metric read's start time is not close to the recorded site-up minute" >&2; exit 1; }
logged_start=$(sed -nE 's/.*healthy minutes since ([0-9TZ:-]+)\).*/\1/p' "$work/ok.out" | head -n1)
[[ $logged_start == "$start_time" ]] ||
  { echo "ok: the logged wait start '$logged_start' does not match the metric call's start-time '$start_time'" >&2; exit 1; }

# A local timezone must never change the metric read's UTC start time: it is
# computed with TZ=UTC regardless of the caller's environment. The comparison
# is against this run's own real-clock start, not the earlier "ok" run's
# start time, so the check never flakes across a minute boundary between them.
before_tz_epoch=$(date -u +%s)
( export TZ=Asia/Ho_Chi_Minh; run_case ok_tz 0 v0.1.0 )
f=$work/ok_tz.calls
start_time_tz=$(sed -E 's/.*--start-time ([0-9TZ:-]+).*/\1/' < <(grep -m1 -F "cloudwatch get-metric-statistics" "$f"))
[[ $start_time_tz =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:00Z$ ]] ||
  { echo "ok_tz: the metric read's start time '$start_time_tz' is not UTC" >&2; exit 1; }
logged_start_tz=$(sed -nE 's/.*healthy minutes since ([0-9TZ:-]+)\).*/\1/p' "$work/ok_tz.out" | head -n1)
[[ $logged_start_tz == "$start_time_tz" ]] ||
  { echo "ok_tz: the logged wait start does not match the metric call's start-time" >&2; exit 1; }
start_epoch_tz=$(date -u -d "$start_time_tz" +%s)
((start_epoch_tz >= before_tz_epoch && start_epoch_tz <= before_tz_epoch + 120)) ||
  { echo "ok_tz: the metric read's start time is not close to this run's own site-up minute" >&2; exit 1; }

# Both warm paths are requested through CloudFront after the existing smoke
# checks and before the site-down alarm wait, each in one attempt.
f=$work/ok.calls
before_re "$f" "https://aboutme\.vn/readyz" "$warm_danny_re"
before_re "$f" "curl -fsSI https://aboutme\.vn/" "$warm_danny_re"
before_re "$f" "$warm_danny_re" "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json"
before_re "$f" "$warm_home_re" "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json"
[[ $(count "$f" "https://aboutme.vn/danny") == 1 ]] || { echo "ok: want one warm attempt for /danny" >&2; exit 1; }

# Existing disabled states must stay disabled and need no mutation.
run_case alerts_initially_disabled 0 v0.1.0
f=$work/alerts_initially_disabled.calls
absent "$f" "events disable-rule --name aboutme-prod-task-stopped"
absent "$f" "events enable-rule --name aboutme-prod-task-stopped"
absent "$f" "cloudwatch disable-alarm-actions --alarm-names aboutme-prod-site-down"
absent "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down"
absent "$f" "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json"
absent "$f" "cloudwatch get-metric-statistics"

# The alarm reads OK throughout, but Route 53 has not yet reported enough
# healthy post-up minutes on the first polls; the actions come back only once
# it has, and every poll is counted. DEPLOY_ALARM_WAIT is generous so a slow
# runner's per-attempt overhead cannot hit the wall-clock deadline before the
# 4th poll succeeds; the attempt count below still bounds it.
DEPLOY_ALARM_WAIT=60 run_case alarm_ok_stale 0 v0.1.0
f=$work/alarm_ok_stale.calls
[[ $(count "$f" "cloudwatch get-metric-statistics") == 4 ]] ||
  { echo "alarm_ok_stale: want 4 metric polls before the healthy count catches up" >&2; exit 1; }
before "$f" "cloudwatch get-metric-statistics" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down"

# Healthy minutes count only at the newest end of the series: an unhealthy
# newest minute after three healthy ones keeps the actions off. A generous
# DEPLOY_ALARM_WAIT keeps a slow runner's per-attempt overhead from hitting
# the wall-clock deadline before the 2nd poll succeeds.
DEPLOY_ALARM_WAIT=60 run_case alarm_flap 0 v0.1.0
f=$work/alarm_flap.calls
[[ $(count "$f" "cloudwatch get-metric-statistics") == 2 ]] ||
  { echo "alarm_flap: want a second metric poll after an unhealthy newest minute" >&2; exit 1; }
before "$f" "cloudwatch get-metric-statistics" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down"

# The alarm itself still reads ALARM for the first polls (evaluating the
# outage), then OK with a fully healthy metric; the actions come back only
# after it turns OK. A generous DEPLOY_ALARM_WAIT keeps a slow runner's
# per-attempt overhead from hitting the wall-clock deadline before the 3rd
# poll turns OK.
DEPLOY_ALARM_WAIT=60 run_case alarm_late_alarm 0 v0.1.0
f=$work/alarm_late_alarm.calls
[[ $(count "$f" "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json") == 3 ]] ||
  { echo "alarm_late_alarm: want 3 alarm-state polls before it turns OK" >&2; exit 1; }
[[ $(count "$f" "cloudwatch get-metric-statistics") == 1 ]] ||
  { echo "alarm_late_alarm: the metric should be read only once the alarm is OK" >&2; exit 1; }
before "$f" "events enable-rule --name aboutme-prod-task-stopped" "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json"

# An alarm shape that no longer matches OpenTofu's definition (someone edited
# it by hand) must never be treated as healthy, however long the wait runs.
run_case alarm_shape_changed fail v0.1.0
f=$work/alarm_shape_changed.calls
absent "$f" "cloudwatch get-metric-statistics"
[[ $(count "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down") == 1 ]] ||
  { echo "alarm_shape_changed: site-down actions were not re-enabled exactly once" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$f" || { echo "alarm_shape_changed: lock was not released" >&2; exit 1; }
grep -qF "unexpected alarm shape: LessThanOrEqualToThreshold/Minimum" "$work/alarm_shape_changed.out" ||
  { echo "alarm_shape_changed: no shape-mismatch message" >&2; exit 1; }

# A metric read that always fails must name its last error in the timeout
# message, without ever failing the wait outright.
run_case alarm_metric_error fail v0.1.0
f=$work/alarm_metric_error.calls
[[ $(count "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down") == 1 ]] ||
  { echo "alarm_metric_error: site-down actions were not re-enabled exactly once" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$f" || { echo "alarm_metric_error: lock was not released" >&2; exit 1; }
grep -qF "last read error: AccessDeniedException: user is not authorized to perform this action" \
  "$work/alarm_metric_error.out" ||
  { echo "alarm_metric_error: no last-read-error message" >&2; exit 1; }

# Route 53 never reports enough healthy minutes: the deploy fails with the
# timeout message, but the actions still come back once, the lock is still
# released, and the app is never stopped a second time (a warm-up or alarm
# timeout after phase=finished never triggers service recovery).
run_case alarm_never_ok fail v0.1.0
f=$work/alarm_never_ok.calls
[[ $(count "$f" "$stop_app") == 1 ]] || { echo "alarm_never_ok: app was stopped again" >&2; exit 1; }
[[ $(count "$f" "$app_up1") == 1 ]] || { echo "alarm_never_ok: the app was started again" >&2; exit 1; }
[[ $(count "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down") == 1 ]] ||
  { echo "alarm_never_ok: site-down actions were not re-enabled exactly once" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$f" || { echo "alarm_never_ok: lock was not released" >&2; exit 1; }
grep -qF "site-down alarm is 'OK' and Route 53 reported 1 of 4 healthy minutes since the site came up after 3 s; re-enabling its actions; check the site" \
  "$work/alarm_never_ok.out" ||
  { echo "alarm_never_ok: no timeout message" >&2; exit 1; }

# A timed-out wait and a first enable-alarm-actions failure must not trigger a
# second wait: on_exit's own cleanup retries only the enable call once the
# wait already ran, and the operation lock still releases.
run_case alarm_timeout_enable_retry fail v0.1.0
f=$work/alarm_timeout_enable_retry.calls
(($(count "$f" "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json") <= 3)) ||
  { echo "alarm_timeout_enable_retry: want at most 3 alarm-state polls, from a single wait" >&2; exit 1; }
[[ $(count "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down") == 2 ]] ||
  { echo "alarm_timeout_enable_retry: the failed enable was not retried in cleanup" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$f" ||
  { echo "alarm_timeout_enable_retry: lock was not released" >&2; exit 1; }

# An ambiguous or mismatched suppression response must restore what could have
# changed and fail before the app is stopped.
for alert_failure in alerts_site_lost alerts_site_mismatch alerts_rule_lost; do
  run_case "$alert_failure" fail v0.1.0
  f=$work/$alert_failure.calls
  absent "$f" "$stop_app"
  grep -qF -- "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down" "$f" ||
    { echo "$alert_failure: site-down actions were not restored" >&2; exit 1; }
done
f=$work/alerts_rule_lost.calls
grep -qF -- "events enable-rule --name aboutme-prod-task-stopped" "$f" ||
  { echo "alerts_rule_lost: task-stopped rule was not restored" >&2; exit 1; }

# A notification restore failure is an error, but does not invoke service
# recovery after the new app is already healthy.
run_case alerts_restore_fails fail v0.1.0
f=$work/alerts_restore_fails.calls
[[ $(count "$f" "$stop_app") == 1 ]] || { echo "alerts_restore_fails: app was stopped again" >&2; exit 1; }
[[ $(count "$f" "$app_up1") == 1 ]] || { echo "alerts_restore_fails: app start changed" >&2; exit 1; }

# A failed service-recovery step must not bypass notification cleanup. The
# original deployment failure remains the process result, and no unsafe ECS
# start follows the failed recovery state check.
run_case alerts_recovery_fails fail v0.1.0
f=$work/alerts_recovery_fails.calls
grep -qF -- "events enable-rule --name aboutme-prod-task-stopped" "$f" ||
  { echo "alerts_recovery_fails: task-stopped rule was not restored" >&2; exit 1; }
grep -qF -- "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down" "$f" ||
  { echo "alerts_recovery_fails: site-down actions were not restored" >&2; exit 1; }
[[ $(count "$f" "$app_up1") == 1 ]] || { echo "alerts_recovery_fails: the app was started again" >&2; exit 1; }
grep -q "service recovery did not complete; restoring deployment notifications" "$work/alerts_recovery_fails.out" ||
  { echo "alerts_recovery_fails: recovery failure was not reported" >&2; exit 1; }

# The suppression boundary applies to first deploy and rollback too.
for alert_success in alerts_first alerts_rollback; do
  if [[ $alert_success == alerts_first ]]; then run_case "$alert_success" 0 v0.1.0 --first-deploy
  else run_case "$alert_success" 0 --rollback v0.0.9
  fi
  f=$work/$alert_success.calls
  grep -qF -- "events disable-rule --name aboutme-prod-task-stopped" "$f" ||
    { echo "$alert_success: task-stopped rule was not disabled" >&2; exit 1; }
  grep -qF -- "cloudwatch disable-alarm-actions --alarm-names aboutme-prod-site-down" "$f" ||
    { echo "$alert_success: site-down actions were not disabled" >&2; exit 1; }
done

# HUP, INT and TERM exit through cleanup. The stub sends TERM while the rule
# suppression call is in flight, before a service can be changed.
run_case alerts_signal fail v0.1.0
f=$work/alerts_signal.calls
absent "$f" "$stop_app"
grep -qF -- "events enable-rule --name aboutme-prod-task-stopped" "$f" ||
  { echo "alerts_signal: task-stopped rule was not restored" >&2; exit 1; }

# A second signal arriving during cleanup itself must not re-enter on_signal
# and race the restore/release sequence already in progress; on_exit ignores
# it and finishes with the original failure's status.
run_case signal_during_cleanup fail v0.1.0
grep -q "received TERM" "$work/signal_during_cleanup.out" &&
  { echo "signal_during_cleanup: a signal during cleanup was not ignored" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$work/signal_during_cleanup.calls" ||
  { echo "signal_during_cleanup: lock was not released" >&2; exit 1; }

# Every task secret is checked by name, once, before anything changes, and no
# value is ever read.
before "$f" "ssm describe-parameters" "rds create-db-snapshot"
before "$f" "secretsmanager describe-secret" "rds create-db-snapshot"
before "$f" "ssm describe-parameters" "ecs register-task-definition"
for want in "Key=Name,Values=/aboutme/prod/db/app-password " "Key=Name,Values=/aboutme/prod/oauth/google-client-id "; do
  [[ $(count "$f" "$want") == 1 ]] || { echo "ok: want one describe-parameters for '$want'" >&2; exit 1; }
done
[[ $(count "$f" "--secret-id arn:aws:secretsmanager:ap-southeast-1:1:secret:rds!db-abc-XyZ12 ") == 1 ]] ||
  { echo "ok: want one describe-secret for the Secrets Manager ARN without its JSON key" >&2; exit 1; }
absent "$f" "get-secret-value"
# The only SSM value reads are edge_origin_cert_check's two, as the base
# caller: the public origin certificate and the origin key's public
# fingerprint. Never a task secret, never through the deploy role.
[[ $(count "$f" "ssm get-parameter") == 2 ]] ||
  { echo "ok: want exactly two ssm get-parameter(s) calls" >&2; exit 1; }
grep -qF "[test-base] aws --region ap-southeast-1 ssm get-parameter --name /aboutme/prod/tls/origin-cert " "$f" ||
  { echo "ok: the origin certificate read did not run as the base caller" >&2; exit 1; }
grep -qF "[test-base] aws --region ap-southeast-1 ssm get-parameters --names /aboutme/prod/tls/origin-key-sha256 " "$f" ||
  { echo "ok: the origin key fingerprint read did not run as the base caller" >&2; exit 1; }

for missing in secret_missing secretsmanager_missing; do
  run_case "$missing" fail v0.1.0
  f=$work/$missing.calls
  absent "$f" "create-db-snapshot"
  absent "$f" "register-task-definition"
  absent "$f" "update-service"
  absent "$f" "scheduler update-schedule"
done
grep -q -F "missing secret /aboutme/prod/oauth/google-client-id" "$work/secret_missing.out" ||
  { echo "secret_missing: the missing parameter is not named" >&2; exit 1; }
grep -q -F "missing secret arn:aws:secretsmanager:ap-southeast-1:1:secret:rds!db-abc-XyZ12" "$work/secretsmanager_missing.out" ||
  { echo "secretsmanager_missing: the missing secret is not named" >&2; exit 1; }

# The origin-certificate guard (edge.sh) refuses before any mutation: a
# certificate inside the 21-day window, or one that cannot be read at all.
run_case origin_cert_expiring fail v0.1.0
f=$work/origin_cert_expiring.calls
absent "$f" "create-db-snapshot"
absent "$f" "register-task-definition"
absent "$f" "update-service"
absent "$f" "operation_id=:o, operation_kind=:k"
grep -qF "the origin certificate expires in fewer than 21 days; run tls.sh export, then deploy" \
  "$work/origin_cert_expiring.out" ||
  { echo "origin_cert_expiring: no expiry message" >&2; exit 1; }

run_case origin_cert_unreadable fail v0.1.0
f=$work/origin_cert_unreadable.calls
absent "$f" "create-db-snapshot"
absent "$f" "register-task-definition"
absent "$f" "update-service"
absent "$f" "operation_id=:o, operation_kind=:k"
grep -qF "could not read the origin certificate" "$work/origin_cert_unreadable.out" ||
  { echo "origin_cert_unreadable: no unreadable message" >&2; exit 1; }

# A stored value that is not a certificate reads as unreadable.
run_case origin_cert_not_pem fail v0.1.0
absent "$work/origin_cert_not_pem.calls" "register-task-definition"
grep -qF "could not read the origin certificate" "$work/origin_cert_not_pem.out" ||
  { echo "origin_cert_not_pem: no unreadable message" >&2; exit 1; }

# A certificate that does not match the stored origin key refuses before any
# mutation: every Caddy task started from them would fail its TLS setup.
run_case origin_key_mismatch fail v0.1.0
f=$work/origin_key_mismatch.calls
absent "$f" "create-db-snapshot"
absent "$f" "register-task-definition"
absent "$f" "update-service"
absent "$f" "operation_id=:o, operation_kind=:k"
grep -qF "the origin certificate does not match the stored origin key; run tls.sh export, then deploy" \
  "$work/origin_key_mismatch.out" ||
  { echo "origin_key_mismatch: no mismatch message" >&2; exit 1; }

# Before the first export no fingerprint exists; the deploy goes ahead.
run_case origin_key_fingerprint_absent 0 v0.1.0
grep -qF "no origin key fingerprint stored yet; skipping the key match check" \
  "$work/origin_key_fingerprint_absent.out" ||
  { echo "origin_key_fingerprint_absent: no skip message" >&2; exit 1; }

# The same guard runs on --rollback, not only a normal deploy.
run_case origin_cert_expiring_rollback fail --rollback v0.0.9
f=$work/origin_cert_expiring_rollback.calls
absent "$f" "create-db-snapshot"
absent "$f" "register-task-definition"
absent "$f" "update-service"
absent "$f" "operation_id=:o, operation_kind=:k"
grep -qF "the origin certificate expires in fewer than 21 days; run tls.sh export, then deploy" \
  "$work/origin_cert_expiring_rollback.out" ||
  { echo "origin_cert_expiring_rollback: no expiry message" >&2; exit 1; }

# A task definition that still maps host port 443 (tofu apply not run yet)
# refuses before any mutation: the app and maintenance handoff needs both
# services free of a port mapping to run side by side.
run_case port443_mapped fail v0.1.0
f=$work/port443_mapped.calls
absent "$f" "ecs update-service"
absent "$f" "rds create-db-snapshot"
absent "$f" "scheduler update-schedule"
grep -qF "still maps host port 443 or 8443; run tofu apply first" "$work/port443_mapped.out" ||
  { echo "port443_mapped: no port-mapping message" >&2; exit 1; }

# A mapping that names only containerPort, with no explicit hostPort, still
# reserves host port 443 under host networking (ECS defaults hostPort to
# containerPort), so the guard must catch it too, before any mutation.
run_case port443_container_only fail v0.1.0
f=$work/port443_container_only.calls
absent "$f" "ecs update-service"
absent "$f" "rds create-db-snapshot"
absent "$f" "scheduler update-schedule"
grep -qF "still maps host port 443 or 8443; run tofu apply first" "$work/port443_container_only.out" ||
  { echo "port443_container_only: no port-mapping message" >&2; exit 1; }

# The same guard covers the CloudFront listener's host port 8443.
run_case port8443_mapped fail v0.1.0
f=$work/port8443_mapped.calls
absent "$f" "ecs update-service"
absent "$f" "rds create-db-snapshot"
absent "$f" "scheduler update-schedule"
grep -qF "still maps host port 443 or 8443; run tofu apply first" "$work/port8443_mapped.out" ||
  { echo "port8443_mapped: no port-mapping message" >&2; exit 1; }

# The CloudFront edge distribution check (edge.sh) runs on every deploy, and,
# like the port-mapping check beside it, refuses before any mutation.
run_case edge_ok 0 v0.1.0
f=$work/edge_ok.calls
grep -qF "[test-base] aws cloudfront list-distributions --output json" "$f" ||
  { echo "edge_ok: the distribution check did not run as the base caller" >&2; exit 1; }

run_case edge_no_distribution fail v0.1.0
f=$work/edge_no_distribution.calls
absent "$f" "ecs update-service"
absent "$f" "rds create-db-snapshot"
absent "$f" "scheduler update-schedule"
grep -qF "expected exactly one CloudFront distribution for aboutme.vn; found 0; run tofu apply first" \
  "$work/edge_no_distribution.out" ||
  { echo "edge_no_distribution: no distribution-count message" >&2; exit 1; }

run_case edge_wrong_origin fail v0.1.0
f=$work/edge_wrong_origin.calls
absent "$f" "ecs update-service"
absent "$f" "rds create-db-snapshot"
absent "$f" "scheduler update-schedule"
grep -qF "the distribution's origin is ec2-198-51-100-1.ap-southeast-1.compute.amazonaws.com" \
  "$work/edge_wrong_origin.out" ||
  { echo "edge_wrong_origin: no wrong-origin message" >&2; exit 1; }

run_case edge_wrong_port fail v0.1.0
f=$work/edge_wrong_port.calls
absent "$f" "ecs update-service"
absent "$f" "rds create-db-snapshot"
absent "$f" "scheduler update-schedule"
grep -qF "the distribution's origin HTTPS port is 443, not 8443; run tofu apply first" \
  "$work/edge_wrong_port.out" ||
  { echo "edge_wrong_port: no wrong-port message" >&2; exit 1; }

run_case edge_no_mtls fail v0.1.0
f=$work/edge_no_mtls.calls
absent "$f" "ecs update-service"
absent "$f" "rds create-db-snapshot"
absent "$f" "scheduler update-schedule"
grep -qF "the distribution's origin has no mTLS client certificate; run tofu apply first" \
  "$work/edge_no_mtls.out" ||
  { echo "edge_no_mtls: no missing-mTLS message" >&2; exit 1; }

run_case edge_wrong_protocol fail v0.1.0
f=$work/edge_wrong_protocol.calls
absent "$f" "ecs update-service"
absent "$f" "rds create-db-snapshot"
absent "$f" "scheduler update-schedule"
grep -qF "the distribution's origin protocol policy is match-viewer, not https-only; run tofu apply first" \
  "$work/edge_wrong_protocol.out" ||
  { echo "edge_wrong_protocol: no wrong-protocol message" >&2; exit 1; }

# An unknown origin address stops the deploy
# before any mutation, not only at the final smoke check.
run_case edge_no_address fail v0.1.0
f=$work/edge_no_address.calls
absent "$f" "ecs update-service"
absent "$f" "rds create-db-snapshot"
absent "$f" "cloudfront list-distributions"
grep -qF "could not resolve the origin address" "$work/edge_no_address.out" ||
  { echo "edge_no_address: no unresolved-address message" >&2; exit 1; }

# Each release snapshot carries the tag release-snapshot-sweep deletes by.
f=$work/ok.calls
grep -q -F -- "rds create-db-snapshot --db-instance-identifier aboutme-prod --db-snapshot-identifier aboutme-prod-v0-1-0-202610200000 --tags Key=aboutme:created-by,Value=deploy.sh" "$f" ||
  { echo "ok: snapshot not created with the deploy.sh tag" >&2; exit 1; }

# Smoke checks retry transient edge failures after the restart.
run_case smoke_flaky 0 v0.1.0
f=$work/smoke_flaky.calls
[[ $(count "$f" "https://aboutme.vn/healthz") == 2 ]] || { echo "smoke_flaky: want one /healthz retry" >&2; exit 1; }
[[ $(count "$f" "curl -fsSI https://aboutme.vn/") == 2 ]] || { echo "smoke_flaky: want one HSTS retry" >&2; exit 1; }
grep -q "deployed v0.1.0" "$work/smoke_flaky.out" || { echo "smoke_flaky: deploy did not finish" >&2; exit 1; }

# A check that never passes fails after the bounded attempts.
run_case smoke_down fail v0.1.0
[[ $(count "$work/smoke_down.calls" "https://aboutme.vn/healthz") == 5 ]] ||
  { echo "smoke_down: want exactly 5 /healthz attempts" >&2; exit 1; }
grep -q "smoke: /healthz returned 502" "$work/smoke_down.out" || { echo "smoke_down: no failure message" >&2; exit 1; }

# Headers with HSTS but no CloudFront Via (as if DNS still pointed elsewhere)
# fail the deploy after the bounded attempts, once the /healthz and /readyz
# smoke already ran.
run_case via_missing fail v0.1.0
f=$work/via_missing.calls
before "$f" "https://aboutme.vn/readyz" "curl -fsSI https://aboutme.vn/"
[[ $(count "$f" "curl -fsSI https://aboutme.vn/") == 5 ]] ||
  { echo "via_missing: want exactly 5 header-probe attempts" >&2; exit 1; }
grep -qF "smoke: Via does not name CloudFront; check the DNS records for aboutme.vn" "$work/via_missing.out" ||
  { echo "via_missing: no Via failure message" >&2; exit 1; }

# Cloudflare's proxy in front of CloudFront passes the Via check alone; its
# CF-Ray fails the deploy.
run_case cloudflare_proxied fail v0.1.0
[[ $(count "$work/cloudflare_proxied.calls" "curl -fsSI https://aboutme.vn/") == 5 ]] ||
  { echo "cloudflare_proxied: want exactly 5 header-probe attempts" >&2; exit 1; }
grep -qF "smoke: Cloudflare proxies the response; check the DNS records for aboutme.vn" \
  "$work/cloudflare_proxied.out" || { echo "cloudflare_proxied: no proxy failure message" >&2; exit 1; }

# A header request that keeps failing is reported as such, not as a missing
# header.
run_case headers_down fail v0.1.0
grep -qF "smoke: the header request failed" "$work/headers_down.out" ||
  { echo "headers_down: no request failure message" >&2; exit 1; }

# The direct-origin check fails closed: one answer fails the deploy, and an
# unknown origin address is a failure, never a skip. It covers both 443 and
# 8443 (docs/design/cloudfront-edge.md, "Deploys and monitoring").
run_case origin_open fail v0.1.0
[[ $(count "$work/origin_open.calls" "https://192.0.2.10:443/") == 1 ]] ||
  { echo "origin_open: an answering origin must fail on the first probe" >&2; exit 1; }
grep -q "the origin is reachable directly on port 443 (curl exit 0)" "$work/origin_open.out" ||
  { echo "origin_open: no failure message" >&2; exit 1; }

# The 443 probe passes (no answer); the 8443 probe still runs and fails the
# deploy on its own.
run_case origin_open_8443 fail v0.1.0
[[ $(count "$work/origin_open_8443.calls" "https://192.0.2.10:443/") == 1 ]] ||
  { echo "origin_open_8443: the 443 probe must still run once" >&2; exit 1; }
[[ $(count "$work/origin_open_8443.calls" "https://192.0.2.10:8443/") == 1 ]] ||
  { echo "origin_open_8443: an answering origin must fail on the first 8443 probe" >&2; exit 1; }
grep -q "the origin is reachable directly on port 8443 (curl exit 0)" "$work/origin_open_8443.out" ||
  { echo "origin_open_8443: no failure message" >&2; exit 1; }
# A refused connection or a TLS rejection is not a closed port: only a
# timeout passes.
run_case origin_refused_8443 fail v0.1.0
grep -q "the origin is reachable directly on port 8443 (curl exit 7)" "$work/origin_refused_8443.out" ||
  { echo "origin_refused_8443: a refused connection must fail the deploy" >&2; exit 1; }
run_case origin_tls_rejected fail v0.1.0
grep -q "the origin is reachable directly on port 443 (curl exit 56)" "$work/origin_tls_rejected.out" ||
  { echo "origin_tls_rejected: a TLS rejection must fail the deploy" >&2; exit 1; }
# The preflight resolved the address, but the smoke check cannot: a failure,
# never a skip.
run_case origin_unknown fail v0.1.0
grep -q "smoke: could not resolve the origin address" "$work/origin_unknown.out" ||
  { echo "origin_unknown: no failure message" >&2; exit 1; }

# A path that answers slow or with a 5xx on its first attempts, then fast and
# 200, still succeeds; every attempt is counted.
DEPLOY_WARM_ATTEMPTS=5 run_case warm_slow_then_fast 0 v0.1.0
f=$work/warm_slow_then_fast.calls
[[ $(count "$f" "https://aboutme.vn/danny") == 3 ]] ||
  { echo "warm_slow_then_fast: want 3 warm attempts for /danny" >&2; exit 1; }
grep -q "deployed v0.1.0" "$work/warm_slow_then_fast.out" ||
  { echo "warm_slow_then_fast: deploy did not finish" >&2; exit 1; }

# A path that never answers fast enough fails the deploy after the bounded
# attempts, without touching service recovery: phase=finished by then, so a
# warm-up failure never restores the previous app.
DEPLOY_WARM_ATTEMPTS=3 run_case warm_never_fast fail v0.1.0
f=$work/warm_never_fast.calls
[[ $(count "$f" "https://aboutme.vn/danny") == 3 ]] ||
  { echo "warm_never_fast: want exactly 3 warm attempts for /danny" >&2; exit 1; }
[[ $(count "$f" "$app_up1") == 1 ]] || { echo "warm_never_fast: the app was started again" >&2; exit 1; }
grep -qF "warm-up: /danny did not answer 200 within 3 s in 3 attempts (last: 200 in 5.0 s)" \
  "$work/warm_never_fast.out" ||
  { echo "warm_never_fast: no warm-up failure message" >&2; exit 1; }
# A warm-up failure after the handoff finished still waits for the site-down
# alarm's post-recovery health, the same bounded way a clean finish does,
# before its actions come back; it does not re-enable them right away.
grep -qF "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json" "$f" ||
  { echo "warm_never_fast: the alarm wait did not run" >&2; exit 1; }
grep -qF "cloudwatch get-metric-statistics" "$f" ||
  { echo "warm_never_fast: the alarm metric read did not run" >&2; exit 1; }
before "$f" "cloudwatch get-metric-statistics" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down"
[[ $(count "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down") == 1 ]] ||
  { echo "warm_never_fast: site-down actions were not re-enabled exactly once" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$f" || { echo "warm_never_fast: lock was not released" >&2; exit 1; }

# A signal during the post-finish alarm wait skips the wait and restores at
# once, instead of leaving the alarm suppressed until a bounded wait finishes.
run_case alarm_signal_during_wait fail v0.1.0
f=$work/alarm_signal_during_wait.calls
absent "$f" "cloudwatch get-metric-statistics"
grep -qF -- "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down" "$f" ||
  { echo "alarm_signal_during_wait: site-down actions were not restored" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$f" || { echo "alarm_signal_during_wait: lock was not released" >&2; exit 1; }

# A signal during on_exit's own cleanup wait (a warm-up failure here, the same
# as warm_never_fast) must stop that wait early too, instead of pushing the
# operator to SIGKILL for up to DEPLOY_ALARM_WAIT seconds.
DEPLOY_WARM_ATTEMPTS=3 run_case cleanup_wait_signal fail v0.1.0
f=$work/cleanup_wait_signal.calls
grep -qF "waiting up to 3 s for the site-down alarm before re-enabling its actions; press Ctrl-C to skip the wait" \
  "$work/cleanup_wait_signal.out" ||
  { echo "cleanup_wait_signal: no wait-start message" >&2; exit 1; }
[[ $(count "$f" "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json") -lt 3 ]] ||
  { echo "cleanup_wait_signal: the wait was not stopped by the signal" >&2; exit 1; }
[[ $(count "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down") == 1 ]] ||
  { echo "cleanup_wait_signal: site-down actions were not re-enabled exactly once" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$f" || { echo "cleanup_wait_signal: lock was not released" >&2; exit 1; }
grep -qF "skipping the site-down alarm wait; re-enabling its actions now" "$work/cleanup_wait_signal.out" ||
  { echo "cleanup_wait_signal: no skip message" >&2; exit 1; }

# A signal during cleanup that also kills the task-stopped rule's enable call
# must not leave the rule disabled: cleanup retries it with signals ignored.
DEPLOY_WARM_ATTEMPTS=3 run_case cleanup_rule_killed fail v0.1.0
f=$work/cleanup_rule_killed.calls
[[ $(count "$f" "events enable-rule --name aboutme-prod-task-stopped") == 2 ]] ||
  { echo "cleanup_rule_killed: the killed rule enable was not retried" >&2; exit 1; }
[[ $(count "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down") == 1 ]] ||
  { echo "cleanup_rule_killed: site-down actions were not re-enabled exactly once" >&2; exit 1; }
absent "$f" "cloudwatch get-metric-statistics"
grep -qF "REMOVE operation_id" "$f" || { echo "cleanup_rule_killed: lock was not released" >&2; exit 1; }

# A curl timeout on one attempt counts as a failed attempt, not a crash.
run_case warm_timeout 0 v0.1.0
f=$work/warm_timeout.calls
[[ $(count "$f" "https://aboutme.vn/danny") == 2 ]] ||
  { echo "warm_timeout: want the timed-out attempt plus one retry" >&2; exit 1; }
grep -q "deployed v0.1.0" "$work/warm_timeout.out" || { echo "warm_timeout: deploy did not finish" >&2; exit 1; }

run_case first 0 v0.1.0 --first-deploy
f=$work/first.calls
before "$f" "--started-by deploy-db-setup" "--started-by deploy-migrate"
grep -q '"State": "ENABLED"' "$work/last-schedule.json" || { echo "first: schedules not enabled at the end" >&2; exit 1; }

# ListTasks failures must stop the deploy before a database task. Maintenance
# is already up by then (it starts before the app stops), so recovery brings
# the previous app back and stops maintenance, in that order.
for list_failure in list_running_fails list_stopped_fails; do
  run_case "$list_failure" fail v0.1.0
  f=$work/$list_failure.calls
  absent "$f" "deploy-migrate"
  absent "$f" "$app_point"
  grep -qF -- "$prev_app_up" "$f" || { echo "$list_failure: previous app not restored" >&2; exit 1; }
  grep -qF -- "$down_maintenance" "$f" || { echo "$list_failure: maintenance not stopped" >&2; exit 1; }
  before "$f" "$prev_app_up" "$down_maintenance"
  [[ $(count "$f" "scheduler update-schedule") == 8 ]] || { echo "$list_failure: schedules not restored" >&2; exit 1; }
done

# The post-scale STOPPED query includes a task that was already stopping. The
# task-stopped waiter must include it before another service may claim :443.
run_case stopping_task 0 v0.1.0
f=$work/stopping_task.calls
grep -q -- "ecs wait tasks-stopped.*running-task.*stopping-task" "$f" || { echo "stopping_task: not all tasks were waited" >&2; exit 1; }

# On the first deploy, maintenance starts, then an uncertain app stop must be
# retried and confirmed before a database task can run. Job schedules remain
# disabled because db-setup may not have created usable application state.
run_case first_app_down_fails fail v0.1.0 --first-deploy
f=$work/first_app_down_fails.calls
before "$f" "$maint_up1" "$stop_app"
[[ $(count "$f" "$stop_app") == 2 ]] || { echo "first_app_down_fails: app stop was not retried" >&2; exit 1; }
[[ $(count "$f" "$maint_up1") == 2 ]] ||
  { echo "first_app_down_fails: maintenance was not reconfirmed during recovery" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "first_app_down_fails: schedules must stay disabled" >&2; exit 1; }
absent "$f" "deploy-migrate"

# The maintenance page must prove it is live through CloudFront before a
# database task can start. Otherwise the rollback path restores the app and
# schedules without any migration risk.
run_case maintenance_smoke_fails fail v0.1.0
f=$work/maintenance_smoke_fails.calls
absent "$f" "deploy-migrate"
grep -qF -- "$prev_app_up" "$f" || { echo "maintenance_smoke_fails: previous app not restored" >&2; exit 1; }
grep -qF -- "$down_maintenance" "$f" || { echo "maintenance_smoke_fails: maintenance page not turned off" >&2; exit 1; }
before "$f" "$prev_app_up" "$down_maintenance"
[[ $(count "$f" "https://aboutme.vn/") == 5 ]] || { echo "maintenance_smoke_fails: want 5 checks" >&2; exit 1; }

run_case migrate_fails fail v0.1.0
f=$work/migrate_fails.calls
absent "$f" "$start_app"
# A failed Goose run can have committed earlier migrations. Keep the
# maintenance page, app, and schedules stopped until an operator fixes forward
# or restores the snapshot.
absent "$f" "$prev_app_up"
absent "$f" "$down_maintenance"
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "migrate_fails: schedules must stay disabled" >&2; exit 1; }
grep -q "migration may have been applied" "$work/migrate_fails.out" || { echo "migrate_fails: no migration warning" >&2; exit 1; }

run_case first_migrate_fails fail v0.1.0 --first-deploy
f=$work/first_migrate_fails.calls
absent "$f" "$start_app"
absent "$f" "$prev_app_up"
# There is no previous release on a first deploy, so restore leaves the
# maintenance page up rather than leaving nothing answering port 8443.
grep -qF -- "$up_maintenance" "$f" || { echo "first_migrate_fails: maintenance page not left up" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "first_migrate_fails: schedules must stay disabled" >&2; exit 1; }

run_case first_db_setup_fails fail v0.1.0 --first-deploy
f=$work/first_db_setup_fails.calls
absent "$f" "deploy-migrate"
absent "$f" "$start_app"
absent "$f" "$prev_app_up"
grep -qF -- "$up_maintenance" "$f" || { echo "first_db_setup_fails: maintenance page not left up" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "first_db_setup_fails: schedules must stay disabled" >&2; exit 1; }

# The first-deploy variant of the same gating: db-setup fails before any
# migration risk, so restore() takes its first-deploy branch; there,
# restore()'s own maintenance_up cannot be confirmed either, so the app is
# left as it is (never scaled to 0) rather than risk port 8443 with no
# listener at all.
run_case restore_first_maint_fails fail v0.1.0 --first-deploy
f=$work/restore_first_maint_fails.calls
absent "$f" "deploy-migrate"
[[ $(count "$f" "$stop_app") == 1 ]] ||
  { echo "restore_first_maint_fails: the app was scaled to 0 after the failed restart" >&2; exit 1; }
grep -qF "the app stays as it is so port 8443 keeps a listener" \
  "$work/restore_first_maint_fails.out" ||
  { echo "restore_first_maint_fails: no keeps-a-listener message" >&2; exit 1; }

run_case runtask_fails fail v0.1.0
f=$work/runtask_fails.calls
# A lost RunTask response does not prove the migration failed to start. Keep
# the maintenance page up until an operator resolves the database state.
absent "$f" "$prev_app_up"
absent "$f" "$down_maintenance"
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "runtask_fails: schedules must stay disabled" >&2; exit 1; }
grep -q "migration may have been applied" "$work/runtask_fails.out" || { echo "runtask_fails: no migration warning" >&2; exit 1; }

run_case wait_timeout fail v0.1.0
f=$work/wait_timeout.calls
absent "$f" "$start_app"
absent "$f" "$prev_app_up"
# A database task may still be running: leave it alone, including the
# maintenance page, which is already up and must not be touched again.
absent "$f" "$down_maintenance"
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "wait_timeout: schedules must stay disabled" >&2; exit 1; }
grep -q "may still be running" "$work/wait_timeout.out" || { echo "wait_timeout: no warning about the running task" >&2; exit 1; }
grep -q "maintenance page stays up" "$work/wait_timeout.out" || { echo "wait_timeout: no mention that maintenance stays up" >&2; exit 1; }

run_case start_fails fail v0.1.0
f=$work/start_fails.calls
absent "$f" "$prev_app_up"
absent "$f" "$start_app"
# Migrations were applied: the maintenance page must still be up (or come
# back up) rather than leaving nothing answering port 8443.
grep -qF -- "$up_maintenance" "$f" || { echo "start_fails: maintenance page not left up" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "start_fails: schedules must stay disabled after a migration" >&2; exit 1; }
grep -q "migration may have been applied" "$work/start_fails.out" || { echo "start_fails: no migration warning" >&2; exit 1; }

# An update-service response can be lost after ECS accepted it. The deploy
# marks the app start before the request, reconfirms maintenance runs, then
# scales the app back to zero. It never assumes port 8443 stayed free.
run_case app_start_update_fails fail v0.1.0
f=$work/app_start_update_fails.calls
grep -qF -- "$stop_app" "$f" || { echo "app_start_update_fails: app was not scaled to zero" >&2; exit 1; }
[[ $(count "$f" "$up_maintenance") == 2 ]] || { echo "app_start_update_fails: maintenance was not restored" >&2; exit 1; }
# The app was pointed at the new release (its response was lost, not
# refused) but never got a second desired-count-1 request: recovery scales it
# back down rather than retrying the request.
[[ $(count "$f" "$app_up1") == 1 ]] || { echo "app_start_update_fails: the app start was retried" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "app_start_update_fails: schedules must stay disabled" >&2; exit 1; }
# restore()'s migration branch starts (or reconfirms) maintenance before it
# stops an app whose health was never confirmed, so port 8443 always keeps a
# listener; the 2nd occurrence of each pattern is the restore-time one, since
# the forward path already sent one of each before the app start failed.
restore_maint=$(nth "$f" "$maint_up1" 2)
restore_stop=$(nth "$f" "$stop_app" 2)
[[ -n $restore_maint && -n $restore_stop && $restore_maint -lt $restore_stop ]] ||
  { echo "app_start_update_fails: restore-time maintenance start did not precede the app stop" >&2; exit 1; }

# Same failure, but restore()'s own maintenance_up cannot be confirmed either:
# the new app that may have started beside it runs the migrated schema, so it
# is left as it is (never scaled to 0) rather than risk leaving port 8443 with
# no listener.
run_case restore_migration_maint_fails fail v0.1.0
f=$work/restore_migration_maint_fails.calls
[[ $(count "$f" "$stop_app") == 1 ]] ||
  { echo "restore_migration_maint_fails: the new app was scaled to 0 after its own start" >&2; exit 1; }
grep -qF "could not confirm that the maintenance page runs; check both services before retrying" \
  "$work/restore_migration_maint_fails.out" ||
  { echo "restore_migration_maint_fails: no maintenance warning" >&2; exit 1; }
grep -qF "the new app stays as it is so port 8443 keeps a listener; check aboutme-prod-app by hand" \
  "$work/restore_migration_maint_fails.out" ||
  { echo "restore_migration_maint_fails: no keeps-a-listener message" >&2; exit 1; }

# If maintenance cannot be confirmed stopped once the new app is healthy, do
# not leave it looking like recovery is still needed: the app stays up and the
# gap is named for an operator, but the app is never touched again.
run_case maintenance_down_fails fail v0.1.0
f=$work/maintenance_down_fails.calls
[[ $(count "$f" "$app_up1") == 1 ]] || { echo "maintenance_down_fails: the app was scaled again" >&2; exit 1; }
grep -q "could not confirm that maintenance stopped; scale aboutme-prod-maintenance to 0 by hand" \
  "$work/maintenance_down_fails.out" || { echo "maintenance_down_fails: no maintenance warning" >&2; exit 1; }

# The forward handoff's own maintenance-down wait can fail once even though
# the app is already up and healthy (an ECS wait timeout, not a real
# failure): restore() retries the scale-down and, once it confirms
# maintenance stopped, that counts as a finished handoff for the bounded
# site-down alarm wait, the same as a clean finish.
DEPLOY_ALARM_WAIT=3 run_case maint_down_wait_once_fails fail v0.1.0
f=$work/maint_down_wait_once_fails.calls
[[ $(count "$f" "$down_maintenance") == 2 ]] ||
  { echo "maint_down_wait_once_fails: maintenance scale-down was not retried in restore()" >&2; exit 1; }
stop_line=$(nth "$f" "$stop_app" 1)
confirm_line=$(line "$f" "$app_confirm")
[[ -n $stop_line && -n $confirm_line && $stop_line -lt $confirm_line && $(count "$f" "$stop_app") == 1 ]] ||
  { echo "maint_down_wait_once_fails: the app was scaled to 0 after its own start" >&2; exit 1; }
grep -qF "waiting up to 3 s for the site-down alarm before re-enabling its actions" \
  "$work/maint_down_wait_once_fails.out" ||
  { echo "maint_down_wait_once_fails: no bounded alarm-wait message" >&2; exit 1; }

run_case bad_flag 2 v0.1.0 --first_deploy
[[ ! -s $work/bad_flag.calls ]] || { echo "bad_flag: commands ran" >&2; exit 1; }

run_case extra_arg 2 --rollback v0.0.9 extra
[[ ! -s $work/extra_arg.calls ]] || { echo "extra_arg: commands ran" >&2; exit 1; }

run_case rollback 0 --rollback v0.0.9
f=$work/rollback.calls
absent "$f" "deploy-migrate"
absent "$f" "create-db-snapshot"
before "$f" "ssm describe-parameters" "ecs register-task-definition"
# A rollback still starts each incoming service beside the outgoing one.
before "$f" "$maint_up1" "$stop_app"
before "$f" "$app_up1" "$down_maintenance"
# Same point-before-scale ordering as the forward deploy above.
before "$f" "$app_point" "$app_up1"

# A rollback runs through the same fence-aware script and is rejected the
# same way a forward deploy is when the target is below the fence minimum.
run_case rollback_below_fence fail --rollback v0.0.9
absent "$work/rollback_below_fence.calls" "ecs register-task-definition"
grep -qF "below the release fence minimum" "$work/rollback_below_fence.out" ||
  { echo "rollback_below_fence: no fence-minimum message" >&2; exit 1; }

# A rollback target equal to the fence minimum is accepted.
run_case rollback_equal_fence 0 --rollback v0.0.9

# A rollback has no migration, so restore()'s app_stable branch (not its
# migration branch) must be the one that keeps a confirmed app up when only
# the schedule-enable loop fails afterward: it never restarts the previous
# (pre-rollback) app over an unrelated Scheduler API error, and it stops
# maintenance rather than leaving it up beside the now-healthy app.
run_case rollback_schedule_enable_fails fail --rollback v0.0.9
f=$work/rollback_schedule_enable_fails.calls
absent "$f" "--service aboutme-prod-app --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/aboutme-prod-app:3"
[[ $(count "$f" "$stop_app") == 1 ]] ||
  { echo "rollback_schedule_enable_fails: the app was scaled to 0 after its own start" >&2; exit 1; }
grep -q "the app started this release and is healthy; it stays up" \
  "$work/rollback_schedule_enable_fails.out" ||
  { echo "rollback_schedule_enable_fails: no stays-up message" >&2; exit 1; }
grep -q "fix by hand, or rerun deploy.sh to retry the whole release" \
  "$work/rollback_schedule_enable_fails.out" ||
  { echo "rollback_schedule_enable_fails: no schedule-gap message" >&2; exit 1; }

# restore() refuses a previous_app below the current fence minimum instead of
# restarting a release the fence no longer allows; maintenance stays up.
run_case restore_below_fence fail --rollback v0.5.0
f=$work/restore_below_fence.calls
absent "$f" "$prev_app_up"
absent "$f" "$down_maintenance"
grep -qF "is below the fence minimum" "$work/restore_below_fence.out" ||
  { echo "restore_below_fence: no below-fence message" >&2; exit 1; }

# Bringing maintenance up can lose its steady-state confirmation before the
# app ever stops: the app is never touched, and recovery still reconfirms the
# previous app and tries to stop maintenance, naming the gap if that also
# cannot be confirmed.
run_case maint_up_fails fail v0.1.0
f=$work/maint_up_fails.calls
absent "$f" "$stop_app"
absent "$f" "$app_point"
grep -qF -- "$prev_app_up" "$f" || { echo "maint_up_fails: previous app not reconfirmed" >&2; exit 1; }
grep -qF -- "$down_maintenance" "$f" || { echo "maint_up_fails: maintenance was not scaled to zero" >&2; exit 1; }
grep -q "could not confirm that maintenance stopped; scale aboutme-prod-maintenance to 0 by hand" \
  "$work/maint_up_fails.out" || { echo "maint_up_fails: no maintenance warning" >&2; exit 1; }
absent "$f" "deploy-migrate"
[[ $(count "$f" "scheduler update-schedule") == 8 ]] ||
  { echo "maint_up_fails: schedules were not disabled and re-enabled" >&2; exit 1; }

# The maintenance smoke check fails before any migration risk, so restore()
# takes its previous-release branch; there, restoring the previous app itself
# cannot be confirmed (its task still names a revision confirm_running never
# expects). Once maintenance is confirmed, the unconfirmed app is scaled back
# to 0 rather than left running beside a confirmed maintenance page.
run_case restore_prev_confirm_fails fail v0.1.0
f=$work/restore_prev_confirm_fails.calls
[[ $(count "$f" "$stop_app") == 2 ]] ||
  { echo "restore_prev_confirm_fails: the unconfirmed previous app was not scaled to 0" >&2; exit 1; }
second_stop=$(nth "$f" "$stop_app" 2)
up1_line=$(line "$f" "$app_up1")
[[ -n $second_stop && -n $up1_line && $up1_line -lt $second_stop ]] ||
  { echo "restore_prev_confirm_fails: the app was scaled to 0 before its own restart attempt" >&2; exit 1; }
absent "$f" "$down_maintenance"
grep -qF "the maintenance page stays up; scaling the unconfirmed app back to 0" \
  "$work/restore_prev_confirm_fails.out" ||
  { echo "restore_prev_confirm_fails: no scale-back message" >&2; exit 1; }

# Same as above, but maintenance_up also cannot be confirmed in restore(): with
# no confirmed maintenance page, the unconfirmed app is left as the only
# possible listener on port 8443 rather than scaled to 0.
run_case restore_prev_and_maint_fail fail v0.1.0
f=$work/restore_prev_and_maint_fail.calls
[[ $(count "$f" "$stop_app") == 1 ]] ||
  { echo "restore_prev_and_maint_fail: the app was scaled to 0 with no confirmed maintenance page" >&2; exit 1; }
grep -qF "could not confirm that the maintenance page runs either; both services stay as they are; check them by hand" \
  "$work/restore_prev_and_maint_fail.out" ||
  { echo "restore_prev_and_maint_fail: no both-services-stay message" >&2; exit 1; }

# confirm_running rejects a task of the wrong revision: the app was pointed at
# the new release and scaled up, but the running task still names the
# previous one. Recovery scales the app back down and reconfirms maintenance,
# the same as an unconfirmed wait.
run_case confirm_wrong_revision fail v0.1.0
f=$work/confirm_wrong_revision.calls
grep -qF -- "$app_point" "$f" || { echo "confirm_wrong_revision: app was never pointed at the new release" >&2; exit 1; }
[[ $(count "$f" "$app_up1") == 1 ]] || { echo "confirm_wrong_revision: the app start was retried" >&2; exit 1; }
[[ $(count "$f" "$stop_app") == 2 ]] || { echo "confirm_wrong_revision: app was not scaled back down" >&2; exit 1; }
[[ $(count "$f" "$maint_up1") == 2 ]] ||
  { echo "confirm_wrong_revision: maintenance was not reconfirmed" >&2; exit 1; }
grep -qF "is not running its task of arn:aws:ecs:ap-southeast-1:1:task-definition/new:4 with every container up" \
  "$work/confirm_wrong_revision.out" || { echo "confirm_wrong_revision: no confirmation-failure message" >&2; exit 1; }
grep -q "failed while the new app was starting; its health was never confirmed" \
  "$work/confirm_wrong_revision.out" || { echo "confirm_wrong_revision: no health-unconfirmed message" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] ||
  { echo "confirm_wrong_revision: schedules must stay disabled" >&2; exit 1; }

# confirm_running rejects a task whose containers are not all RUNNING (the
# app's Caddy has not yet seen the server container turn healthy). Same
# recovery as a wrong revision.
run_case confirm_container_not_running fail v0.1.0
f=$work/confirm_container_not_running.calls
grep -qF -- "$app_point" "$f" || { echo "confirm_container_not_running: app was never pointed at the new release" >&2; exit 1; }
[[ $(count "$f" "$stop_app") == 2 ]] ||
  { echo "confirm_container_not_running: app was not scaled back down" >&2; exit 1; }
grep -qF "is not running its task of arn:aws:ecs:ap-southeast-1:1:task-definition/new:4 with every container up" \
  "$work/confirm_container_not_running.out" ||
  { echo "confirm_container_not_running: no confirmation-failure message" >&2; exit 1; }

# confirm_running rejects a service reporting two RUNNING tasks for the app
# (a placement race, or the previous revision not yet fully stopped), not
# just a wrong revision or an unready container. Same recovery.
run_case confirm_two_running_tasks fail v0.1.0
f=$work/confirm_two_running_tasks.calls
grep -qF -- "$app_point" "$f" || { echo "confirm_two_running_tasks: app was never pointed at the new release" >&2; exit 1; }
[[ $(count "$f" "$stop_app") == 2 ]] ||
  { echo "confirm_two_running_tasks: app was not scaled back down" >&2; exit 1; }
[[ $(count "$f" "$maint_up1") == 2 ]] ||
  { echo "confirm_two_running_tasks: maintenance was not reconfirmed" >&2; exit 1; }
grep -qF "aboutme-prod-app has 2 tasks wanting to run, want 1" "$work/confirm_two_running_tasks.out" ||
  { echo "confirm_two_running_tasks: no confirmation-failure message" >&2; exit 1; }
grep -q "failed while the new app was starting; its health was never confirmed" \
  "$work/confirm_two_running_tasks.out" ||
  { echo "confirm_two_running_tasks: no health-unconfirmed message" >&2; exit 1; }

# deploy.sh never deletes a snapshot; release-snapshot-sweep owns that.
for calls in "$work"/*.calls; do
  absent "$calls" "delete-db-snapshot"
done

# The release fence blocks a target below its minimum before any mutation.
run_case fence_lower fail v0.1.0
f=$work/fence_lower.calls
absent "$f" "rds create-db-snapshot"
absent "$f" "ecs register-task-definition"
absent "$f" "ecs update-service"
absent "$f" "scheduler update-schedule"
grep -qF "below the release fence minimum" "$work/fence_lower.out" ||
  { echo "fence_lower: no fence-minimum message" >&2; exit 1; }

# An equal target is accepted without lowering or rewriting the fence: a
# normal deploy never raises it.
run_case fence_equal 0 v0.1.0
absent "$work/fence_equal.calls" "minimum_release=:c, minimum_tag=:t"

# A present but malformed fence item blocks before any mutation.
run_case fence_malformed fail v0.1.0
absent "$work/fence_malformed.calls" "ecs register-task-definition"
grep -qF "release fence item is malformed" "$work/fence_malformed.out" ||
  { echo "fence_malformed: no malformed-item message" >&2; exit 1; }

# A missing fence item blocks unless the running app proves enrollment off.
run_case fence_missing_enrolled fail v0.1.0
absent "$work/fence_missing_enrolled.calls" "ecs register-task-definition"
grep -qF "does not prove passkey enrollment off" "$work/fence_missing_enrolled.out" ||
  { echo "fence_missing_enrolled: no enrollment message" >&2; exit 1; }

# OpenTofu creates aboutme-prod-app with its own placeholder task definition
# at desired count 0, so a fresh host never reports an absent service; the
# plain "first" case above already exercises that real, untouched-placeholder
# shape and proves the true-first-deploy path still succeeds. This case is
# the other shape at the same zero counts: a real prior deploy stamp, proving
# --first-deploy checks the stamp, not just running and desired counts.
run_case fence_first_already_running fail v0.1.0 --first-deploy
absent "$work/fence_first_already_running.calls" "ecs register-task-definition"
grep -qF "already runs" "$work/fence_first_already_running.out" ||
  { echo "fence_first_already_running: no already-runs message" >&2; exit 1; }

# A read error on the missing-item check fails closed, not open.
run_case fence_missing_describe_error fail v0.1.0
absent "$work/fence_missing_describe_error.calls" "ecs register-task-definition"
grep -qF "could not read the running app service" "$work/fence_missing_describe_error.out" ||
  { echo "fence_missing_describe_error: no read-failure message" >&2; exit 1; }

# A running app task definition that jq cannot parse fails closed, not open:
# it must not read as an empty (so "off") enrollment value.
run_case fence_running_def_malformed fail v0.1.0
absent "$work/fence_running_def_malformed.calls" "ecs register-task-definition"
grep -qF "could not read the running app task definition" "$work/fence_running_def_malformed.out" ||
  { echo "fence_running_def_malformed: no read-failure message" >&2; exit 1; }

# fence_read checks the service's actual running revision, not a family's
# latest, so the running app proves enrollment off. The revision this deploy
# would register still carries enrollment on below the fence, so it stops
# before the lock or any registration.
run_case fence_running_revision_differs fail v0.1.0
f=$work/fence_running_revision_differs.calls
absent "$f" "SET operation_id=:o, operation_kind=:k"
absent "$f" "ecs register-task-definition"
grep -qF "run --activate first" "$work/fence_running_revision_differs.out" ||
  { echo "fence_running_revision_differs: no enrollment message" >&2; exit 1; }

# An existing operation fails lock acquisition before any other mutation.
run_case fence_locked fail v0.1.0
f=$work/fence_locked.calls
absent "$f" "rds create-db-snapshot"
absent "$f" "ecs register-task-definition"

# An uncertain lock result that a strong read proves this process owns
# proceeds normally; one that proves a foreign owner stops without a retry.
run_case fence_uncertain_owned 0 v0.1.0
run_case fence_uncertain_foreign fail v0.1.0
f=$work/fence_uncertain_foreign.calls
absent "$f" "ecs register-task-definition"
[[ $(count "$f" "SET operation_id=:o, operation_kind=:k") == 1 ]] ||
  { echo "fence_uncertain_foreign: retried lock acquisition" >&2; exit 1; }

# A ConditionalCheckFailedException on lock acquisition can be an SDK retry
# of this exact request seeing its own success, not a foreign owner. The
# same strong read that resolves an uncertain result also resolves this one.
run_case fence_lock_retry_success 0 v0.1.0
[[ $(count "$work/fence_lock_retry_success.calls" "SET operation_id=:o, operation_kind=:k") == 1 ]] ||
  { echo "fence_lock_retry_success: retried lock acquisition" >&2; exit 1; }

# A mid-deploy checkpoint failure stops before the guarded mutation, and the
# lock this process holds is still released.
run_case fence_checkpoint_fails fail v0.1.0
f=$work/fence_checkpoint_fails.calls
absent "$f" "rds create-db-snapshot"
grep -qF "REMOVE operation_id" "$f" ||
  { echo "fence_checkpoint_fails: lock was not released" >&2; exit 1; }

# A checkpoint failure right after web comes up (section 8, just before the
# app start it guards) must not leave the app stopped with no recovery: the
# app is confirmed stopped (it already was) and maintenance is reconfirmed,
# rather than attempting an unguarded app start.
run_case checkpoint_fails_before_app_start fail v0.1.0
f=$work/checkpoint_fails_before_app_start.calls
grep -qF -- "$start_web" "$f" ||
  { echo "checkpoint_fails_before_app_start: web never started" >&2; exit 1; }
[[ $(count "$f" "$up_maintenance") == 2 ]] ||
  { echo "checkpoint_fails_before_app_start: maintenance was not reconfirmed" >&2; exit 1; }
absent "$f" "$app_point"
grep -qF "REMOVE operation_id" "$f" ||
  { echo "checkpoint_fails_before_app_start: lock was not released" >&2; exit 1; }

# A checkpoint failure right after a migration leaves the maintenance page up
# rather than attempting an unproved app start.
run_case checkpoint_fails_after_migration fail v0.1.0
f=$work/checkpoint_fails_after_migration.calls
grep -qF -- "$up_maintenance" "$f" ||
  { echo "checkpoint_fails_after_migration: maintenance page not left up" >&2; exit 1; }
absent "$f" "$start_web"
grep -qF "REMOVE operation_id" "$f" ||
  { echo "checkpoint_fails_after_migration: lock was not released" >&2; exit 1; }

# --activate only raises the fence: no image, ECS, snapshot, or notification
# mutation, whether the raise succeeds or a race fails it after the lock.
run_case fence_activate 0 --activate v0.1.0
f=$work/fence_activate.calls
absent "$f" "ecs register-task-definition"
absent "$f" "rds create-db-snapshot"
absent "$f" "cloudwatch disable-alarm-actions"
grep -qF "raised the release fence to v0.1.0" "$work/fence_activate.out" ||
  { echo "fence_activate: no raise message" >&2; exit 1; }

run_case fence_raise_race fail --activate v0.1.0
f=$work/fence_raise_race.calls
absent "$f" "ecs register-task-definition"
grep -qF "REMOVE operation_id" "$f" ||
  { echo "fence_raise_race: lock was not released after a failed raise" >&2; exit 1; }

# Activation requires the running app service to already be the exact
# candidate; a tag that is not what the service runs is refused before the
# lock is ever acquired.
run_case fence_activate_not_running fail --activate v0.1.0
f=$work/fence_activate_not_running.calls
absent "$f" "SET operation_id=:o, operation_kind=:k"
grep -qF "not the activation candidate" "$work/fence_activate_not_running.out" ||
  { echo "fence_activate_not_running: no release-mismatch message" >&2; exit 1; }

# The generic serialized activation accepts the real v0.4.7 floor once v0.4.2
# already raised it, and never lowers the stored minimum.
run_case fence_activate_4002_then_4007 0 --activate v0.4.7
f=$work/fence_activate_4002_then_4007.calls
grep -qF "raised the release fence to v0.4.7" "$work/fence_activate_4002_then_4007.out" ||
  { echo "fence_activate_4002_then_4007: no raise message" >&2; exit 1; }
grep -qF ':c":{"N":"4007"}' "$f" || { echo "fence_activate_4002_then_4007: raise did not use 4007" >&2; exit 1; }

# A revision that turns TOTP enrollment on is refused below the v0.4.7 floor,
# the same way passkey enrollment is refused below v0.4.2.
run_case fence_missing_totp_enrolled fail v0.1.0
absent "$work/fence_missing_totp_enrolled.calls" "ecs register-task-definition"
grep -qF "does not prove TOTP enrollment off" "$work/fence_missing_totp_enrolled.out" ||
  { echo "fence_missing_totp_enrolled: no TOTP enrollment message" >&2; exit 1; }

run_case fence_new_revision_totp_enrolled fail v0.1.0
f=$work/fence_new_revision_totp_enrolled.calls
absent "$f" "SET operation_id=:o, operation_kind=:k"
absent "$f" "ecs register-task-definition"
grep -qF "TOTP enrollment on while the release fence is below v0.4.7" \
  "$work/fence_new_revision_totp_enrolled.out" ||
  { echo "fence_new_revision_totp_enrolled: no TOTP enrollment message" >&2; exit 1; }

# --totp-key-reencrypt builds and registers its own totp-reencrypt revision
# from the tag's server image, the same way the other one-shot families are
# built and registered, with no service, snapshot, or schedule mutation, and
# requires the running app to be the exact tag requested.
run_case totp_reencrypt_ok 0 --totp-key-reencrypt v0.4.7
f=$work/totp_reencrypt_ok.calls
absent "$f" "rds create-db-snapshot"
absent "$f" "scheduler update-schedule"
absent "$f" "ecs update-service"
grep -qF "ecs register-task-definition" "$f" ||
  { echo "totp_reencrypt_ok: did not register a revision" >&2; exit 1; }
grep -qF -- "--task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/new:4 --started-by deploy-totp-reencrypt" "$f" ||
  { echo "totp_reencrypt_ok: did not run its own registered revision" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$f" || { echo "totp_reencrypt_ok: lock was not released" >&2; exit 1; }
grep -qF "totp-key-reencrypt done for v0.4.7" "$work/totp_reencrypt_ok.out" ||
  { echo "totp_reencrypt_ok: no completion message" >&2; exit 1; }
# Registered from current_def totp-reencrypt, stamped with the tag's server
# image and DEPLOY_RELEASE_*, not var.image_server's untouched registration.
jq -e '.containerDefinitions[0].image == "ghcr.io/dannyota/aboutme-server@sha256:'"$(printf '%064d' 1)"'"
  and (.containerDefinitions[0].environment | any(.name == "DEPLOY_RELEASE_TAG" and .value == "v0.4.7"))' \
  "$work/last-registered.json" >/dev/null ||
  { echo "totp_reencrypt_ok: registered revision is not stamped with the tag's server image" >&2; exit 1; }

# A run still going after every wait keeps the operation lock, so no deploy
# can start beside it (docs/design/passkey-release-fence.md).
run_case totp_reencrypt_wait_timeout fail --totp-key-reencrypt v0.4.7
f=$work/totp_reencrypt_wait_timeout.calls
[[ $(grep -c "ecs wait tasks-stopped" "$f") -eq 4 ]] ||
  { echo "totp_reencrypt_wait_timeout: did not wait four times" >&2; exit 1; }
absent "$f" "REMOVE operation_id"
grep -qF "operation lock stays held" "$work/totp_reencrypt_wait_timeout.out" ||
  { echo "totp_reencrypt_wait_timeout: no held-lock message" >&2; exit 1; }

# The stored minimum must itself already be at v0.4.7; a lower stored
# minimum refuses the one-shot before the lock, even though a normal deploy
# floor check alone would accept this candidate.
run_case totp_reencrypt_below_floor fail --totp-key-reencrypt v0.4.7
f=$work/totp_reencrypt_below_floor.calls
absent "$f" "SET operation_id=:o, operation_kind=:k"
absent "$f" "ecs run-task"
grep -qF "release fence is below v0.4.7" "$work/totp_reencrypt_below_floor.out" ||
  { echo "totp_reencrypt_below_floor: no floor message" >&2; exit 1; }

# The requested tag must equal the running app's exact release: the same-tag
# rule the rotation runbook step relies on. This check runs after the lock is
# acquired, not before, so it always reads the running app under the lock.
run_case totp_reencrypt_tag_mismatch fail --totp-key-reencrypt v0.4.7
f=$work/totp_reencrypt_tag_mismatch.calls
absent "$f" "ecs register-task-definition"
absent "$f" "ecs run-task"
grep -qF "REMOVE operation_id" "$f" ||
  { echo "totp_reencrypt_tag_mismatch: lock was not released" >&2; exit 1; }
grep -qF "is release 4006, not v0.4.7" "$work/totp_reencrypt_tag_mismatch.out" ||
  { echo "totp_reencrypt_tag_mismatch: no release-mismatch message" >&2; exit 1; }

# Before registering, the new revision's TOTP_ACTIVE_KEY/TOTP_PREVIOUS_KEY
# slots must match the running app's; a mismatch would seal the task under a
# key the running app cannot read, so it is refused instead.
run_case totp_reencrypt_slot_mismatch fail --totp-key-reencrypt v0.4.7
f=$work/totp_reencrypt_slot_mismatch.calls
absent "$f" "ecs register-task-definition"
absent "$f" "ecs run-task"
grep -qF "REMOVE operation_id" "$f" ||
  { echo "totp_reencrypt_slot_mismatch: lock was not released" >&2; exit 1; }
grep -qF "run tofu apply first" "$work/totp_reencrypt_slot_mismatch.out" ||
  { echo "totp_reencrypt_slot_mismatch: no slot-mismatch message" >&2; exit 1; }

run_case totp_reencrypt_register_fails fail --totp-key-reencrypt v0.4.7
f=$work/totp_reencrypt_register_fails.calls
absent "$f" "ecs run-task"
grep -qF "REMOVE operation_id" "$f" ||
  { echo "totp_reencrypt_register_fails: lock was not released" >&2; exit 1; }

run_case totp_reencrypt_runtask_fails fail --totp-key-reencrypt v0.4.7
f=$work/totp_reencrypt_runtask_fails.calls
grep -qF "REMOVE operation_id" "$f" ||
  { echo "totp_reencrypt_runtask_fails: lock was not released" >&2; exit 1; }
grep -qF "totp-key-reencrypt did not start" "$work/totp_reencrypt_runtask_fails.out" ||
  { echo "totp_reencrypt_runtask_fails: no start-failure message" >&2; exit 1; }

run_case totp_reencrypt_task_fails fail --totp-key-reencrypt v0.4.7
f=$work/totp_reencrypt_task_fails.calls
grep -qF "REMOVE operation_id" "$f" ||
  { echo "totp_reencrypt_task_fails: lock was not released" >&2; exit 1; }
grep -qF "totp-key-reencrypt exited with 1" "$work/totp_reencrypt_task_fails.out" ||
  { echo "totp_reencrypt_task_fails: no exit-code message" >&2; exit 1; }

# A release at or above v0.4.7 must name TOTP_ACTIVE_KEY in the app's server
# secrets, or the app would start and fail at TOTP use after migrate has
# already run.
run_case totp_key_secret_missing fail v0.4.7
f=$work/totp_key_secret_missing.calls
absent "$f" "SET operation_id=:o, operation_kind=:k"
absent "$f" "ecs register-task-definition"
grep -qF "run tofu apply for the TOTP key before deploying this release" \
  "$work/totp_key_secret_missing.out" ||
  { echo "totp_key_secret_missing: no TOTP key message" >&2; exit 1; }

# A candidate that never reaches the fence (CI is not green) never acquires
# or releases the lock: a crash before the lock exists leaves nothing to
# clear, unlike a crash after acquisition, which the runbook's manual clear
# then owns.
run_case fence_ci_fails fail v0.1.0
f=$work/fence_ci_fails.calls
absent "$f" "SET operation_id=:o, operation_kind=:k"
absent "$f" "REMOVE operation_id"

# The CI gate accepts only a green push run of ci.yml on main for the tag's
# exact commit: not a workflow_dispatch run, an older run hidden behind a
# newer unrelated run, a run on another branch, a run for a different commit,
# or a run still in progress.
grep -qF -- "--event push --branch main" "$f" ||
  { echo "fence_ci_fails: gh run list did not filter by push event and main branch" >&2; exit 1; }

run_case ci_dispatch_only fail v0.1.0
f=$work/ci_dispatch_only.calls
absent "$f" "SET operation_id=:o, operation_kind=:k"
absent "$f" "ecs register-task-definition"
grep -qF "no successful push run of ci.yml on main for v0.1.0" "$work/ci_dispatch_only.out" ||
  { echo "ci_dispatch_only: no CI-gate message" >&2; exit 1; }
grep -qF "latest: 'none'" "$work/ci_dispatch_only.out" ||
  { echo "ci_dispatch_only: a dispatch-only run was not treated as none" >&2; exit 1; }

run_case ci_dispatch_newer fail v0.1.0
f=$work/ci_dispatch_newer.calls
absent "$f" "ecs register-task-definition"
grep -qF "latest: 'failure'" "$work/ci_dispatch_newer.out" ||
  { echo "ci_dispatch_newer: did not report the older push run's own conclusion" >&2; exit 1; }

run_case ci_push_other_branch fail v0.1.0
f=$work/ci_push_other_branch.calls
absent "$f" "ecs register-task-definition"
grep -qF "latest: 'none'" "$work/ci_push_other_branch.out" ||
  { echo "ci_push_other_branch: a run on another branch was not treated as none" >&2; exit 1; }

run_case ci_wrong_sha fail v0.1.0
f=$work/ci_wrong_sha.calls
absent "$f" "ecs register-task-definition"
grep -qF "latest: 'none'" "$work/ci_wrong_sha.out" ||
  { echo "ci_wrong_sha: a run for a different commit was not treated as none" >&2; exit 1; }

run_case ci_push_in_progress fail v0.1.0
f=$work/ci_push_in_progress.calls
absent "$f" "ecs register-task-definition"
grep -qF "latest: 'in_progress'" "$work/ci_push_in_progress.out" ||
  { echo "ci_push_in_progress: an in-progress run's own status was not reported" >&2; exit 1; }

# An early verify_role failure, well before on_exit's full trap replaces the
# minimal one installed right after mktemp, must still not leave deploy.sh's
# own temp directory behind in TMPDIR.
tmproot=$work/verify_role_fails.tmproot
mkdir -p "$tmproot"
set +e
CALLS="$work/verify_role_fails.calls" STUB_DIR="$work" STUB_CASE="verify_role_fails" PATH="$work/bin:$PATH" \
  AWS_CONFIG_FILE="$work/no-such-aws-config" AWS_PROFILE=test-base TMPDIR="$tmproot" \
  DEPLOY_SMOKE_TIMEOUT=1 DEPLOY_SMOKE_DELAY=0 bash "$here/deploy.sh" v0.1.0 >"$work/verify_role_fails.out" 2>&1
got=$?
set -e
((got != 0)) || { echo "verify_role_fails: exit 0, want failure" >&2; exit 1; }
grep -qF "not an assumed-role session" "$work/verify_role_fails.out" ||
  { echo "verify_role_fails: no identity-mismatch message" >&2; exit 1; }
[ -z "$(ls -A "$tmproot")" ] ||
  { echo "verify_role_fails: left a temp directory behind in TMPDIR" >&2; exit 1; }

run_case usage 2

# The pre-switch DNS comparison script (docs/runbooks/dns.md).
bash "$here/dns-check_test.sh"
echo "deploy-script-test: ok"
