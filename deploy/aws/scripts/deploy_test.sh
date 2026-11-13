#!/usr/bin/env bash
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 100 \
  -subj /CN=aboutme.vn -keyout "$work/origin-cert-100.key" -out "$work/origin-cert-100.pem" 2>/dev/null
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 10 \
  -subj /CN=aboutme.vn -keyout "$work/origin-cert-10.key" -out "$work/origin-cert-10.pem" 2>/dev/null
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

mkdir -p "$work/root-bin"
cat >"$work/root-bin/aws" <<'STUB'
#!/usr/bin/env bash
args="$*"
if [[ ${ROOT_CHECK_ONLY:-0} == 1 && $args == *"ecs describe-task-definition"* && $args == *"aboutme-prod-jobs"* ]]; then
  printf '[%s] %s %s\n' "${AWS_PROFILE:-none}" "$(basename "$0")" "$args" >>"$CALLS"
  printf '%s\n' '{"family":"aboutme-prod-jobs","networkMode":"bridge","requiresCompatibilities":["EC2"],"containerDefinitions":[{"name":"jobs","image":"unchanged","memory":256,"entryPoint":["/usr/local/bin/server"],"command":["idempotency-expiry-sweep"],"environment":[{"name":"DATABASE_URL","value":"postgres://aboutme_app@db/aboutme"},{"name":"DEPLOY_RELEASE_TAG","value":"old"},{"name":"DEPLOY_RELEASE_NUMBER","value":"1"}],"secrets":[{"name":"PGPASSWORD","valueFrom":"arn:aws:ssm:ap-southeast-1:1:parameter/aboutme/prod/db/app-password"}]}]}'
  exit 0
fi
if [[ $args == *"ecs run-task"* && $args == *"--started-by root-"* ]]; then
  printf '[%s] %s %s\n' "${AWS_PROFILE:-none}" "$(basename "$0")" "$args" >>"$CALLS"
  n=$(($(cat "$STUB_DIR/root-check-count.$STUB_CASE" 2>/dev/null || echo 0) + 1))
  echo "$n" >"$STUB_DIR/root-check-count.$STUB_CASE"
  action=$(cut -d, -f"$n" <<<"${ROOT_CHECK_RUN_TASK_SEQUENCE:-ok}")
  [[ $action == lost ]] && exit 255
  printf 'arn:aws:ecs:ap-southeast-1:1:task/aboutme-prod/deploy-public-root-check-%s\n' "$n"
  exit 0
fi
if [[ $args == *"ecs wait tasks-stopped"* && $args == *"deploy-public-root-check-"* ]]; then
  printf '[%s] %s %s\n' "${AWS_PROFILE:-none}" "$(basename "$0")" "$args" >>"$CALLS"
  n=${args##*deploy-public-root-check-}
  n=${n%% *}
  action=$(cut -d, -f"$n" <<<"${ROOT_CHECK_WAIT_SEQUENCE:-ok}")
  [[ $action == fail ]] && exit 255
  exit 0
fi
if [[ $args == *"ecs describe-tasks"* && $args == *"deploy-public-root-check-"* ]]; then
  printf '[%s] %s %s\n' "${AWS_PROFILE:-none}" "$(basename "$0")" "$args" >>"$CALLS"
  n=${args##*deploy-public-root-check-}
  n=${n%% *}
  if [[ $args == *"lastStatus"* ]]; then
    action=$(cut -d, -f"$n" <<<"${ROOT_CHECK_WAIT_SEQUENCE:-ok}")
    [[ $action == fail ]] && printf 'RUNNING\n' || printf 'STOPPED\n'
  else
    code=$(cut -d, -f"$n" <<<"${ROOT_CHECK_EXIT_SEQUENCE:-${ROOT_CHECK_EXIT:-0}}")
    printf '%s\n' "${code:-0}"
  fi
  exit 0
fi
if [[ ${AD_ONLY:-0} == 1 ]]; then
  log() { printf '[%s] %s %s\n' "${AWS_PROFILE:-none}" "$(basename "$0")" "$args" >>"$CALLS"; }
  case $args in
    *"ecs describe-task-definition"*aboutme-prod-jobs*)
      log
      printf '%s\n' '{"family":"aboutme-prod-jobs","networkMode":"bridge","requiresCompatibilities":["EC2"],"containerDefinitions":[{"name":"jobs","image":"unchanged","memory":256,"entryPoint":["/usr/local/bin/server"],"command":["idempotency-expiry-sweep"],"environment":[{"name":"DATABASE_URL","value":"postgres://aboutme_app@db/aboutme"}],"secrets":[{"name":"PGPASSWORD","valueFrom":"arn:aws:ssm:ap-southeast-1:1:parameter/aboutme/prod/db/app-password"}]}]}'
      exit 0 ;;
    *"ecs describe-task-definition"*"--task-definition arn:"*)
      log
      printf '{"family":"f","containerDefinitions":[{"name":"server","environment":[{"name":"DEPLOY_RELEASE_NUMBER","value":"%s"}]}]}\n' "${AD_APP_RELEASE:-1000}"
      exit 0 ;;
    *"ecs run-task"*"--started-by acctdel-"*)
      log
      [[ ${AD_RUN:-ok} == lost ]] && exit 255
      echo "arn:aws:ecs:ap-southeast-1:1:task/aboutme-prod/adtask1"
      exit 0 ;;
    *"ecs wait tasks-stopped"*adtask1*)
      log
      [[ ${AD_WAIT:-ok} == fail ]] && exit 255
      exit 0 ;;
    *"ecs describe-tasks"*adtask1*)
      log
      if [[ $args == *lastStatus* ]]; then
        [[ ${AD_WAIT:-ok} == fail ]] && echo RUNNING || echo STOPPED
      else
        echo "${AD_EXIT:-0}"
      fi
      exit 0 ;;
    *"logs get-log-events"*)
      log
      tok=$(sed -nE 's/.*--next-token ([^ ]+).*/\1/p' <<<"$args")
      first='{"message":"account user=0b1c2d3e-4f50-4617-8899-aabbccddeeff slugs=alpha-cv,beta-cv"},{"message":"esc\u001b[31mred"}'
      end='{"message":"account-delete: end"}'
      case "${AD_LOG:-ok}:$tok" in
        ok:) printf '{"events":[%s,%s],"nextForwardToken":"f/1"}\n' "$first" "$end" ;;
        nosentinel:) printf '{"events":[%s],"nextForwardToken":"f/1"}\n' "$first" ;;
        split:) printf '{"events":[%s],"nextForwardToken":"f/1"}\n' "$first" ;;
        split:f/1) printf '{"events":[%s],"nextForwardToken":"f/2"}\n' "$end" ;;
        split:f/2) printf '{"events":[],"nextForwardToken":"f/2"}\n' ;;
        ok:f/1 | nosentinel:f/1) printf '{"events":[],"nextForwardToken":"f/1"}\n' ;;
        empty:*) printf '%s\n' '{"events":[]}' ;;
        *) exit 255 ;;
      esac
      exit 0 ;;
  esac
fi
exec "$STUB_DIR/bin/aws" "$@"
STUB
chmod +x "$work/root-bin/aws"

run_case() {
  local name=$1 want=$2 got
  shift 2
  : >"$work/$name.calls"
  set +e
  CALLS="$work/$name.calls" STUB_DIR="$work" STUB_CASE="$name" ROOT_CHECK_ONLY="${ROOT_CHECK_ONLY:-0}" \
    ROOT_CHECK_EXIT="${ROOT_CHECK_EXIT:-0}" ROOT_CHECK_EXIT_SEQUENCE="${ROOT_CHECK_EXIT_SEQUENCE:-}" \
    ROOT_CHECK_RUN_TASK_SEQUENCE="${ROOT_CHECK_RUN_TASK_SEQUENCE:-}" ROOT_CHECK_WAIT_SEQUENCE="${ROOT_CHECK_WAIT_SEQUENCE:-}" \
    PATH="$work/root-bin:$work/bin:$PATH" \
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

before() {
  local a b
  a=$(line "$1" "$2")
  b=$(line "$1" "$3")
  if [[ -z $a || -z $b || $a -ge $b ]]; then
    echo "$(basename "$1"): '$2' must precede '$3'" >&2
    exit 1
  fi
}

absent() {
  if grep -q -F -- "$2" "$1"; then
    echo "$(basename "$1"): unexpected '$2'" >&2
    exit 1
  fi
}

line_re() { { grep -n -m1 -E -- "$2" "$1" || true; } | cut -d: -f1; }
before_re() {
  local a b
  a=$(line_re "$1" "$2")
  b=$(line_re "$1" "$3")
  if [[ -z $a || -z $b || $a -ge $b ]]; then
    echo "$(basename "$1"): '$2' must precede '$3'" >&2
    exit 1
  fi
}

count() { grep -c -F -- "$2" "$1" || true; }

nth() { { grep -n -F -- "$2" "$1" || true; } | sed -n "${3}p" | cut -d: -f1; }

stop_app="--service aboutme-prod-app --desired-count 0"
app_point="--service aboutme-prod-app --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/new:4"
app_up1="--service aboutme-prod-app --desired-count 1"
app_confirm="--tasks arn:aws:ecs:ap-southeast-1:1:task/aboutme-prod/task-aboutme-prod-app --output json"
start_web="--service aboutme-prod-web --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/new:4 --desired-count 1"
maint_up1="--service aboutme-prod-maintenance --desired-count 1"
down_maintenance="--service aboutme-prod-maintenance --desired-count 0"
combined_app_start="--service aboutme-prod-app --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/new:4 --desired-count 1"
combined_maint_start="--service aboutme-prod-maintenance --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/new:4 --desired-count 1"
prev_app_up="$app_up1"
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
before "$f" "$app_point" "$app_up1"
before "$f" "$maint_up1" "$stop_app"
before "$f" "$maint_up1" "--started-by deploy-migrate"
before "$f" "$app_up1" "$down_maintenance"
before "$f" "$app_confirm" "$down_maintenance"
absent "$f" "deploy-db-setup"
grep -qF "[test-base] aws cloudfront list-distributions --output json" "$f" ||
  { echo "ok: the distribution check did not run as the base caller" >&2; exit 1; }
absent "$f" "$combined_app_start"
absent "$f" "$combined_maint_start"
[[ $(count "$f" "scheduler update-schedule") == 8 ]] || { echo "ok: want 8 schedule updates" >&2; exit 1; }
down_line=$(nth "$f" "$down_maintenance" 1)
enable_line=$(nth "$f" "scheduler update-schedule" 5)
[[ -n $down_line && -n $enable_line && $down_line -lt $enable_line ]] ||
  { echo "ok: maintenance did not stop before the schedules were re-enabled" >&2; exit 1; }
grep -q '"State": "ENABLED"' "$work/last-schedule.json" || { echo "ok: schedules not enabled at the end" >&2; exit 1; }
jq -e '.Target.EcsParameters.TaskDefinitionArn == "arn:aws:ecs:ap-southeast-1:1:task-definition/new:4"' \
  "$work/last-schedule.json" >/dev/null || { echo "ok: schedules not pinned to the released revision" >&2; exit 1; }

before "$f" "events disable-rule --name aboutme-prod-task-stopped" "$stop_app"
before "$f" "cloudwatch disable-alarm-actions --alarm-names aboutme-prod-site-down" "$stop_app"
before "$f" "$start_app" "events enable-rule --name aboutme-prod-task-stopped"
before "$f" "$start_app" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down"
[[ $(count "$f" "events disable-rule --name aboutme-prod-task-stopped") == 1 ]] ||
  { echo "ok: task-stopped rule was not disabled once" >&2; exit 1; }
[[ $(count "$f" "cloudwatch disable-alarm-actions --alarm-names aboutme-prod-site-down") == 1 ]] ||
  { echo "ok: site-down actions were not disabled once" >&2; exit 1; }
[[ $(count "$f" "host-recover") == 0 ]] || { echo "ok: touched host recovery" >&2; exit 1; }

before "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down" "REMOVE operation_id"
[[ $(count "$f" "REMOVE operation_id") == 1 ]] || { echo "ok: lock released more than once" >&2; exit 1; }
grep -qF ":o\":{\"S\":\"$(cat "$work/lock-oid")\"" "$f" ||
  { echo "ok: lock was not released with its own operation ID" >&2; exit 1; }

[[ $(count "$f" "SET operation_checked_at=:s --condition-expression") == 15 ]] ||
  { echo "ok: want 15 fence checkpoints, one per mutating image start" >&2; exit 1; }

absent "$f" "[fence-operator] aws --region ap-southeast-1 ecs"
absent "$f" "[fence-operator] aws --region ap-southeast-1 dynamodb update-item"
absent "$f" "[fence-operator] aws --region us-east-1"
grep -qF "[fence-deploy] aws --region ap-southeast-1 ecs register-task-definition" "$f" ||
  { echo "ok: task registration did not run as the deploy role" >&2; exit 1; }
grep -F -- "$start_app" "$f" | grep -qF "[fence-deploy]" ||
  { echo "ok: the app start did not run as the deploy role" >&2; exit 1; }
grep -qF "[fence-operator] aws --region ap-southeast-1 dynamodb get-item" "$f" ||
  { echo "ok: the fence read did not run as the operator role" >&2; exit 1; }

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
((start_epoch >= before_ok_epoch && start_epoch <= before_ok_epoch + 120)) ||
  { echo "ok: the metric read's start time is not close to the recorded site-up minute" >&2; exit 1; }
logged_start=$(sed -nE 's/.*healthy minutes since ([0-9TZ:-]+)\).*/\1/p' "$work/ok.out" | head -n1)
[[ $logged_start == "$start_time" ]] ||
  { echo "ok: the logged wait start '$logged_start' does not match the metric call's start-time '$start_time'" >&2; exit 1; }

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

f=$work/ok.calls
before_re "$f" "https://aboutme\.vn/readyz" "$warm_danny_re"
before_re "$f" "curl -fsSI https://aboutme\.vn/" "$warm_danny_re"
before_re "$f" "$warm_danny_re" "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json"
before_re "$f" "$warm_home_re" "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json"
[[ $(count "$f" "https://aboutme.vn/danny") == 1 ]] || { echo "ok: want one warm attempt for /danny" >&2; exit 1; }

run_case alerts_initially_disabled 0 v0.1.0
f=$work/alerts_initially_disabled.calls
absent "$f" "events disable-rule --name aboutme-prod-task-stopped"
absent "$f" "events enable-rule --name aboutme-prod-task-stopped"
absent "$f" "cloudwatch disable-alarm-actions --alarm-names aboutme-prod-site-down"
absent "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down"
absent "$f" "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json"
absent "$f" "cloudwatch get-metric-statistics"

DEPLOY_ALARM_WAIT=60 run_case alarm_ok_stale 0 v0.1.0
f=$work/alarm_ok_stale.calls
[[ $(count "$f" "cloudwatch get-metric-statistics") == 4 ]] ||
  { echo "alarm_ok_stale: want 4 metric polls before the healthy count catches up" >&2; exit 1; }
before "$f" "cloudwatch get-metric-statistics" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down"

DEPLOY_ALARM_WAIT=60 run_case alarm_flap 0 v0.1.0
f=$work/alarm_flap.calls
[[ $(count "$f" "cloudwatch get-metric-statistics") == 2 ]] ||
  { echo "alarm_flap: want a second metric poll after an unhealthy newest minute" >&2; exit 1; }
before "$f" "cloudwatch get-metric-statistics" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down"

DEPLOY_ALARM_WAIT=60 run_case alarm_late_alarm 0 v0.1.0
f=$work/alarm_late_alarm.calls
[[ $(count "$f" "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json") == 3 ]] ||
  { echo "alarm_late_alarm: want 3 alarm-state polls before it turns OK" >&2; exit 1; }
[[ $(count "$f" "cloudwatch get-metric-statistics") == 1 ]] ||
  { echo "alarm_late_alarm: the metric should be read only once the alarm is OK" >&2; exit 1; }
before "$f" "events enable-rule --name aboutme-prod-task-stopped" "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json"

run_case alarm_shape_changed fail v0.1.0
f=$work/alarm_shape_changed.calls
absent "$f" "cloudwatch get-metric-statistics"
[[ $(count "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down") == 1 ]] ||
  { echo "alarm_shape_changed: site-down actions were not re-enabled exactly once" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$f" || { echo "alarm_shape_changed: lock was not released" >&2; exit 1; }
grep -qF "unexpected alarm shape: LessThanOrEqualToThreshold/Minimum" "$work/alarm_shape_changed.out" ||
  { echo "alarm_shape_changed: no shape-mismatch message" >&2; exit 1; }

run_case alarm_metric_error fail v0.1.0
f=$work/alarm_metric_error.calls
[[ $(count "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down") == 1 ]] ||
  { echo "alarm_metric_error: site-down actions were not re-enabled exactly once" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$f" || { echo "alarm_metric_error: lock was not released" >&2; exit 1; }
grep -qF "last read error: AccessDeniedException: user is not authorized to perform this action" \
  "$work/alarm_metric_error.out" ||
  { echo "alarm_metric_error: no last-read-error message" >&2; exit 1; }

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

run_case alarm_timeout_enable_retry fail v0.1.0
f=$work/alarm_timeout_enable_retry.calls
(($(count "$f" "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json") <= 3)) ||
  { echo "alarm_timeout_enable_retry: want at most 3 alarm-state polls, from a single wait" >&2; exit 1; }
[[ $(count "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down") == 2 ]] ||
  { echo "alarm_timeout_enable_retry: the failed enable was not retried in cleanup" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$f" ||
  { echo "alarm_timeout_enable_retry: lock was not released" >&2; exit 1; }

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

run_case alerts_restore_fails fail v0.1.0
f=$work/alerts_restore_fails.calls
[[ $(count "$f" "$stop_app") == 1 ]] || { echo "alerts_restore_fails: app was stopped again" >&2; exit 1; }
[[ $(count "$f" "$app_up1") == 1 ]] || { echo "alerts_restore_fails: app start changed" >&2; exit 1; }

run_case alerts_recovery_fails fail v0.1.0
f=$work/alerts_recovery_fails.calls
grep -qF -- "events enable-rule --name aboutme-prod-task-stopped" "$f" ||
  { echo "alerts_recovery_fails: task-stopped rule was not restored" >&2; exit 1; }
grep -qF -- "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down" "$f" ||
  { echo "alerts_recovery_fails: site-down actions were not restored" >&2; exit 1; }
[[ $(count "$f" "$app_up1") == 1 ]] || { echo "alerts_recovery_fails: the app was started again" >&2; exit 1; }
grep -q "service recovery did not complete; restoring deployment notifications" "$work/alerts_recovery_fails.out" ||
  { echo "alerts_recovery_fails: recovery failure was not reported" >&2; exit 1; }

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

run_case alerts_signal fail v0.1.0
f=$work/alerts_signal.calls
absent "$f" "$stop_app"
grep -qF -- "events enable-rule --name aboutme-prod-task-stopped" "$f" ||
  { echo "alerts_signal: task-stopped rule was not restored" >&2; exit 1; }

run_case signal_during_cleanup fail v0.1.0
grep -q "received TERM" "$work/signal_during_cleanup.out" &&
  { echo "signal_during_cleanup: a signal during cleanup was not ignored" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$work/signal_during_cleanup.calls" ||
  { echo "signal_during_cleanup: lock was not released" >&2; exit 1; }

before "$f" "ssm describe-parameters" "rds create-db-snapshot"
before "$f" "secretsmanager describe-secret" "rds create-db-snapshot"
before "$f" "ssm describe-parameters" "ecs register-task-definition"
for want in "Key=Name,Values=/aboutme/prod/db/app-password " "Key=Name,Values=/aboutme/prod/oauth/google-client-id "; do
  [[ $(count "$f" "$want") == 1 ]] || { echo "ok: want one describe-parameters for '$want'" >&2; exit 1; }
done
[[ $(count "$f" "--secret-id arn:aws:secretsmanager:ap-southeast-1:1:secret:rds!db-abc-XyZ12 ") == 1 ]] ||
  { echo "ok: want one describe-secret for the Secrets Manager ARN without its JSON key" >&2; exit 1; }
absent "$f" "get-secret-value"
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

run_case origin_cert_not_pem fail v0.1.0
absent "$work/origin_cert_not_pem.calls" "register-task-definition"
grep -qF "could not read the origin certificate" "$work/origin_cert_not_pem.out" ||
  { echo "origin_cert_not_pem: no unreadable message" >&2; exit 1; }

run_case origin_key_mismatch fail v0.1.0
f=$work/origin_key_mismatch.calls
absent "$f" "create-db-snapshot"
absent "$f" "register-task-definition"
absent "$f" "update-service"
absent "$f" "operation_id=:o, operation_kind=:k"
grep -qF "the origin certificate does not match the stored origin key; run tls.sh export, then deploy" \
  "$work/origin_key_mismatch.out" ||
  { echo "origin_key_mismatch: no mismatch message" >&2; exit 1; }

run_case origin_key_fingerprint_absent 0 v0.1.0
grep -qF "no origin key fingerprint stored yet; skipping the key match check" \
  "$work/origin_key_fingerprint_absent.out" ||
  { echo "origin_key_fingerprint_absent: no skip message" >&2; exit 1; }

run_case origin_cert_expiring_rollback fail --rollback v0.0.9
f=$work/origin_cert_expiring_rollback.calls
absent "$f" "create-db-snapshot"
absent "$f" "register-task-definition"
absent "$f" "update-service"
absent "$f" "operation_id=:o, operation_kind=:k"
grep -qF "the origin certificate expires in fewer than 21 days; run tls.sh export, then deploy" \
  "$work/origin_cert_expiring_rollback.out" ||
  { echo "origin_cert_expiring_rollback: no expiry message" >&2; exit 1; }

run_case port443_mapped fail v0.1.0
f=$work/port443_mapped.calls
absent "$f" "ecs update-service"
absent "$f" "rds create-db-snapshot"
absent "$f" "scheduler update-schedule"
grep -qF "still maps host port 443 or 8443; run tofu apply first" "$work/port443_mapped.out" ||
  { echo "port443_mapped: no port-mapping message" >&2; exit 1; }

# Under host networking ECS defaults hostPort to containerPort (ECS task definition reference).
run_case port443_container_only fail v0.1.0
f=$work/port443_container_only.calls
absent "$f" "ecs update-service"
absent "$f" "rds create-db-snapshot"
absent "$f" "scheduler update-schedule"
grep -qF "still maps host port 443 or 8443; run tofu apply first" "$work/port443_container_only.out" ||
  { echo "port443_container_only: no port-mapping message" >&2; exit 1; }

run_case port8443_mapped fail v0.1.0
f=$work/port8443_mapped.calls
absent "$f" "ecs update-service"
absent "$f" "rds create-db-snapshot"
absent "$f" "scheduler update-schedule"
grep -qF "still maps host port 443 or 8443; run tofu apply first" "$work/port8443_mapped.out" ||
  { echo "port8443_mapped: no port-mapping message" >&2; exit 1; }

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

run_case edge_no_address fail v0.1.0
f=$work/edge_no_address.calls
absent "$f" "ecs update-service"
absent "$f" "rds create-db-snapshot"
absent "$f" "cloudfront list-distributions"
grep -qF "could not resolve the origin address" "$work/edge_no_address.out" ||
  { echo "edge_no_address: no unresolved-address message" >&2; exit 1; }

f=$work/ok.calls
grep -q -F -- "rds create-db-snapshot --db-instance-identifier aboutme-prod --db-snapshot-identifier aboutme-prod-v0-1-0-202610200000 --tags Key=aboutme:created-by,Value=deploy.sh" "$f" ||
  { echo "ok: snapshot not created with the deploy.sh tag" >&2; exit 1; }

run_case smoke_flaky 0 v0.1.0
f=$work/smoke_flaky.calls
[[ $(count "$f" "https://aboutme.vn/healthz") == 2 ]] || { echo "smoke_flaky: want one /healthz retry" >&2; exit 1; }
[[ $(count "$f" "curl -fsSI https://aboutme.vn/") == 2 ]] || { echo "smoke_flaky: want one HSTS retry" >&2; exit 1; }
grep -q "deployed v0.1.0" "$work/smoke_flaky.out" || { echo "smoke_flaky: deploy did not finish" >&2; exit 1; }

run_case smoke_down fail v0.1.0
[[ $(count "$work/smoke_down.calls" "https://aboutme.vn/healthz") == 5 ]] ||
  { echo "smoke_down: want exactly 5 /healthz attempts" >&2; exit 1; }
grep -q "smoke: /healthz returned 502" "$work/smoke_down.out" || { echo "smoke_down: no failure message" >&2; exit 1; }

run_case via_missing fail v0.1.0
f=$work/via_missing.calls
before "$f" "https://aboutme.vn/readyz" "curl -fsSI https://aboutme.vn/"
[[ $(count "$f" "curl -fsSI https://aboutme.vn/") == 5 ]] ||
  { echo "via_missing: want exactly 5 header-probe attempts" >&2; exit 1; }
grep -qF "smoke: Via does not name CloudFront; check the DNS records for aboutme.vn" "$work/via_missing.out" ||
  { echo "via_missing: no Via failure message" >&2; exit 1; }

run_case cloudflare_proxied fail v0.1.0
[[ $(count "$work/cloudflare_proxied.calls" "curl -fsSI https://aboutme.vn/") == 5 ]] ||
  { echo "cloudflare_proxied: want exactly 5 header-probe attempts" >&2; exit 1; }
grep -qF "smoke: Cloudflare proxies the response; check the DNS records for aboutme.vn" \
  "$work/cloudflare_proxied.out" || { echo "cloudflare_proxied: no proxy failure message" >&2; exit 1; }

run_case headers_down fail v0.1.0
grep -qF "smoke: the header request failed" "$work/headers_down.out" ||
  { echo "headers_down: no request failure message" >&2; exit 1; }

run_case origin_open fail v0.1.0
[[ $(count "$work/origin_open.calls" "https://192.0.2.10:443/") == 1 ]] ||
  { echo "origin_open: an answering origin must fail on the first probe" >&2; exit 1; }
grep -q "the origin is reachable directly on port 443 (curl exit 0)" "$work/origin_open.out" ||
  { echo "origin_open: no failure message" >&2; exit 1; }

run_case origin_open_8443 fail v0.1.0
[[ $(count "$work/origin_open_8443.calls" "https://192.0.2.10:443/") == 1 ]] ||
  { echo "origin_open_8443: the 443 probe must still run once" >&2; exit 1; }
[[ $(count "$work/origin_open_8443.calls" "https://192.0.2.10:8443/") == 1 ]] ||
  { echo "origin_open_8443: an answering origin must fail on the first 8443 probe" >&2; exit 1; }
grep -q "the origin is reachable directly on port 8443 (curl exit 0)" "$work/origin_open_8443.out" ||
  { echo "origin_open_8443: no failure message" >&2; exit 1; }
run_case origin_refused_8443 fail v0.1.0
grep -q "the origin is reachable directly on port 8443 (curl exit 7)" "$work/origin_refused_8443.out" ||
  { echo "origin_refused_8443: a refused connection must fail the deploy" >&2; exit 1; }
run_case origin_tls_rejected fail v0.1.0
grep -q "the origin is reachable directly on port 443 (curl exit 56)" "$work/origin_tls_rejected.out" ||
  { echo "origin_tls_rejected: a TLS rejection must fail the deploy" >&2; exit 1; }
run_case origin_unknown fail v0.1.0
grep -q "smoke: could not resolve the origin address" "$work/origin_unknown.out" ||
  { echo "origin_unknown: no failure message" >&2; exit 1; }

DEPLOY_WARM_ATTEMPTS=5 run_case warm_slow_then_fast 0 v0.1.0
f=$work/warm_slow_then_fast.calls
[[ $(count "$f" "https://aboutme.vn/danny") == 3 ]] ||
  { echo "warm_slow_then_fast: want 3 warm attempts for /danny" >&2; exit 1; }
grep -q "deployed v0.1.0" "$work/warm_slow_then_fast.out" ||
  { echo "warm_slow_then_fast: deploy did not finish" >&2; exit 1; }

DEPLOY_WARM_ATTEMPTS=3 run_case warm_never_fast fail v0.1.0
f=$work/warm_never_fast.calls
[[ $(count "$f" "https://aboutme.vn/danny") == 3 ]] ||
  { echo "warm_never_fast: want exactly 3 warm attempts for /danny" >&2; exit 1; }
[[ $(count "$f" "$app_up1") == 1 ]] || { echo "warm_never_fast: the app was started again" >&2; exit 1; }
grep -qF "warm-up: /danny did not answer 200 within 3 s in 3 attempts (last: 200 in 5.0 s)" \
  "$work/warm_never_fast.out" ||
  { echo "warm_never_fast: no warm-up failure message" >&2; exit 1; }
grep -qF "cloudwatch describe-alarms --alarm-names aboutme-prod-site-down --output json" "$f" ||
  { echo "warm_never_fast: the alarm wait did not run" >&2; exit 1; }
grep -qF "cloudwatch get-metric-statistics" "$f" ||
  { echo "warm_never_fast: the alarm metric read did not run" >&2; exit 1; }
before "$f" "cloudwatch get-metric-statistics" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down"
[[ $(count "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down") == 1 ]] ||
  { echo "warm_never_fast: site-down actions were not re-enabled exactly once" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$f" || { echo "warm_never_fast: lock was not released" >&2; exit 1; }

run_case alarm_signal_during_wait fail v0.1.0
f=$work/alarm_signal_during_wait.calls
absent "$f" "cloudwatch get-metric-statistics"
grep -qF -- "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down" "$f" ||
  { echo "alarm_signal_during_wait: site-down actions were not restored" >&2; exit 1; }
grep -qF "REMOVE operation_id" "$f" || { echo "alarm_signal_during_wait: lock was not released" >&2; exit 1; }

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

DEPLOY_WARM_ATTEMPTS=3 run_case cleanup_rule_killed fail v0.1.0
f=$work/cleanup_rule_killed.calls
[[ $(count "$f" "events enable-rule --name aboutme-prod-task-stopped") == 2 ]] ||
  { echo "cleanup_rule_killed: the killed rule enable was not retried" >&2; exit 1; }
[[ $(count "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down") == 1 ]] ||
  { echo "cleanup_rule_killed: site-down actions were not re-enabled exactly once" >&2; exit 1; }
absent "$f" "cloudwatch get-metric-statistics"
grep -qF "REMOVE operation_id" "$f" || { echo "cleanup_rule_killed: lock was not released" >&2; exit 1; }

run_case warm_timeout 0 v0.1.0
f=$work/warm_timeout.calls
[[ $(count "$f" "https://aboutme.vn/danny") == 2 ]] ||
  { echo "warm_timeout: want the timed-out attempt plus one retry" >&2; exit 1; }
grep -q "deployed v0.1.0" "$work/warm_timeout.out" || { echo "warm_timeout: deploy did not finish" >&2; exit 1; }

run_case warm_page_404 0 v0.1.0
f=$work/warm_page_404.calls
[[ $(count "$f" "https://aboutme.vn/danny") == 1 ]] ||
  { echo "warm_page_404: want one warm attempt for /danny" >&2; exit 1; }
grep -q "deployed v0.1.0" "$work/warm_page_404.out" ||
  { echo "warm_page_404: deploy did not finish" >&2; exit 1; }
DEPLOY_WARM_ATTEMPTS=2 run_case warm_home_404 fail v0.1.0
grep -qF "warm-up: / did not answer 200" "$work/warm_home_404.out" ||
  { echo "warm_home_404: no warm-up failure message" >&2; exit 1; }

run_case first 0 v0.1.0 --first-deploy
f=$work/first.calls
before "$f" "--started-by deploy-db-setup" "--started-by deploy-migrate"
grep -q '"State": "ENABLED"' "$work/last-schedule.json" || { echo "first: schedules not enabled at the end" >&2; exit 1; }

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

run_case stopping_task 0 v0.1.0
f=$work/stopping_task.calls
grep -q -- "ecs wait tasks-stopped.*running-task.*stopping-task" "$f" || { echo "stopping_task: not all tasks were waited" >&2; exit 1; }

run_case first_app_down_fails fail v0.1.0 --first-deploy
f=$work/first_app_down_fails.calls
before "$f" "$maint_up1" "$stop_app"
[[ $(count "$f" "$stop_app") == 2 ]] || { echo "first_app_down_fails: app stop was not retried" >&2; exit 1; }
[[ $(count "$f" "$maint_up1") == 2 ]] ||
  { echo "first_app_down_fails: maintenance was not reconfirmed during recovery" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "first_app_down_fails: schedules must stay disabled" >&2; exit 1; }
absent "$f" "deploy-migrate"

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
absent "$f" "$prev_app_up"
absent "$f" "$down_maintenance"
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "migrate_fails: schedules must stay disabled" >&2; exit 1; }
grep -q "migration may have been applied" "$work/migrate_fails.out" || { echo "migrate_fails: no migration warning" >&2; exit 1; }

run_case first_migrate_fails fail v0.1.0 --first-deploy
f=$work/first_migrate_fails.calls
absent "$f" "$start_app"
absent "$f" "$prev_app_up"
grep -qF -- "$up_maintenance" "$f" || { echo "first_migrate_fails: maintenance page not left up" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "first_migrate_fails: schedules must stay disabled" >&2; exit 1; }

run_case first_db_setup_fails fail v0.1.0 --first-deploy
f=$work/first_db_setup_fails.calls
absent "$f" "deploy-migrate"
absent "$f" "$start_app"
absent "$f" "$prev_app_up"
grep -qF -- "$up_maintenance" "$f" || { echo "first_db_setup_fails: maintenance page not left up" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "first_db_setup_fails: schedules must stay disabled" >&2; exit 1; }

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
absent "$f" "$prev_app_up"
absent "$f" "$down_maintenance"
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "runtask_fails: schedules must stay disabled" >&2; exit 1; }
grep -q "migration may have been applied" "$work/runtask_fails.out" || { echo "runtask_fails: no migration warning" >&2; exit 1; }

run_case wait_timeout fail v0.1.0
f=$work/wait_timeout.calls
absent "$f" "$start_app"
absent "$f" "$prev_app_up"
absent "$f" "$down_maintenance"
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "wait_timeout: schedules must stay disabled" >&2; exit 1; }
grep -q "may still be running" "$work/wait_timeout.out" || { echo "wait_timeout: no warning about the running task" >&2; exit 1; }
grep -q "maintenance page stays up" "$work/wait_timeout.out" || { echo "wait_timeout: no mention that maintenance stays up" >&2; exit 1; }

run_case start_fails fail v0.1.0
f=$work/start_fails.calls
absent "$f" "$prev_app_up"
absent "$f" "$start_app"
grep -qF -- "$up_maintenance" "$f" || { echo "start_fails: maintenance page not left up" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "start_fails: schedules must stay disabled after a migration" >&2; exit 1; }
grep -q "migration may have been applied" "$work/start_fails.out" || { echo "start_fails: no migration warning" >&2; exit 1; }

run_case app_start_update_fails fail v0.1.0
f=$work/app_start_update_fails.calls
grep -qF -- "$stop_app" "$f" || { echo "app_start_update_fails: app was not scaled to zero" >&2; exit 1; }
[[ $(count "$f" "$up_maintenance") == 2 ]] || { echo "app_start_update_fails: maintenance was not restored" >&2; exit 1; }
[[ $(count "$f" "$app_up1") == 1 ]] || { echo "app_start_update_fails: the app start was retried" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "app_start_update_fails: schedules must stay disabled" >&2; exit 1; }
restore_maint=$(nth "$f" "$maint_up1" 2)
restore_stop=$(nth "$f" "$stop_app" 2)
[[ -n $restore_maint && -n $restore_stop && $restore_maint -lt $restore_stop ]] ||
  { echo "app_start_update_fails: restore-time maintenance start did not precede the app stop" >&2; exit 1; }

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

run_case maintenance_down_fails fail v0.1.0
f=$work/maintenance_down_fails.calls
[[ $(count "$f" "$app_up1") == 1 ]] || { echo "maintenance_down_fails: the app was scaled again" >&2; exit 1; }
grep -q "could not confirm that maintenance stopped; scale aboutme-prod-maintenance to 0 by hand" \
  "$work/maintenance_down_fails.out" || { echo "maintenance_down_fails: no maintenance warning" >&2; exit 1; }

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
before "$f" "$maint_up1" "$stop_app"
before "$f" "$app_up1" "$down_maintenance"
before "$f" "$app_point" "$app_up1"

run_case rollback_below_fence fail --rollback v0.0.9
absent "$work/rollback_below_fence.calls" "ecs register-task-definition"
grep -qF "below the release fence minimum" "$work/rollback_below_fence.out" ||
  { echo "rollback_below_fence: no fence-minimum message" >&2; exit 1; }

run_case rollback_equal_fence 0 --rollback v0.0.9

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

run_case restore_below_fence fail --rollback v0.5.0
f=$work/restore_below_fence.calls
absent "$f" "$prev_app_up"
absent "$f" "$down_maintenance"
grep -qF "is below the fence minimum" "$work/restore_below_fence.out" ||
  { echo "restore_below_fence: no below-fence message" >&2; exit 1; }

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

run_case restore_prev_and_maint_fail fail v0.1.0
f=$work/restore_prev_and_maint_fail.calls
[[ $(count "$f" "$stop_app") == 1 ]] ||
  { echo "restore_prev_and_maint_fail: the app was scaled to 0 with no confirmed maintenance page" >&2; exit 1; }
grep -qF "could not confirm that the maintenance page runs either; both services stay as they are; check them by hand" \
  "$work/restore_prev_and_maint_fail.out" ||
  { echo "restore_prev_and_maint_fail: no both-services-stay message" >&2; exit 1; }

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

run_case confirm_container_not_running fail v0.1.0
f=$work/confirm_container_not_running.calls
grep -qF -- "$app_point" "$f" || { echo "confirm_container_not_running: app was never pointed at the new release" >&2; exit 1; }
[[ $(count "$f" "$stop_app") == 2 ]] ||
  { echo "confirm_container_not_running: app was not scaled back down" >&2; exit 1; }
grep -qF "is not running its task of arn:aws:ecs:ap-southeast-1:1:task-definition/new:4 with every container up" \
  "$work/confirm_container_not_running.out" ||
  { echo "confirm_container_not_running: no confirmation-failure message" >&2; exit 1; }

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

for calls in "$work"/*.calls; do
  absent "$calls" "delete-db-snapshot"
done

run_case fence_lower fail v0.1.0
f=$work/fence_lower.calls
absent "$f" "rds create-db-snapshot"
absent "$f" "ecs register-task-definition"
absent "$f" "ecs update-service"
absent "$f" "scheduler update-schedule"
grep -qF "below the release fence minimum" "$work/fence_lower.out" ||
  { echo "fence_lower: no fence-minimum message" >&2; exit 1; }

run_case fence_equal 0 v0.1.0
absent "$work/fence_equal.calls" "minimum_release=:c, minimum_tag=:t"

run_case fence_malformed fail v0.1.0
absent "$work/fence_malformed.calls" "ecs register-task-definition"
grep -qF "release fence item is malformed" "$work/fence_malformed.out" ||
  { echo "fence_malformed: no malformed-item message" >&2; exit 1; }

run_case fence_missing_enrolled fail v0.1.0
absent "$work/fence_missing_enrolled.calls" "ecs register-task-definition"
grep -qF "does not prove passkey enrollment off" "$work/fence_missing_enrolled.out" ||
  { echo "fence_missing_enrolled: no enrollment message" >&2; exit 1; }

run_case fence_first_already_running fail v0.1.0 --first-deploy
absent "$work/fence_first_already_running.calls" "ecs register-task-definition"
grep -qF "already runs" "$work/fence_first_already_running.out" ||
  { echo "fence_first_already_running: no already-runs message" >&2; exit 1; }

run_case fence_missing_describe_error fail v0.1.0
absent "$work/fence_missing_describe_error.calls" "ecs register-task-definition"
grep -qF "could not read the running app service" "$work/fence_missing_describe_error.out" ||
  { echo "fence_missing_describe_error: no read-failure message" >&2; exit 1; }

run_case fence_running_def_malformed fail v0.1.0
absent "$work/fence_running_def_malformed.calls" "ecs register-task-definition"
grep -qF "could not read the running app task definition" "$work/fence_running_def_malformed.out" ||
  { echo "fence_running_def_malformed: no read-failure message" >&2; exit 1; }

run_case fence_running_revision_differs fail v0.1.0
f=$work/fence_running_revision_differs.calls
absent "$f" "SET operation_id=:o, operation_kind=:k"
absent "$f" "ecs register-task-definition"
grep -qF "run --activate first" "$work/fence_running_revision_differs.out" ||
  { echo "fence_running_revision_differs: no enrollment message" >&2; exit 1; }

run_case fence_locked fail v0.1.0
f=$work/fence_locked.calls
absent "$f" "rds create-db-snapshot"
absent "$f" "ecs register-task-definition"

run_case fence_uncertain_owned 0 v0.1.0
run_case fence_uncertain_foreign fail v0.1.0
f=$work/fence_uncertain_foreign.calls
absent "$f" "ecs register-task-definition"
[[ $(count "$f" "SET operation_id=:o, operation_kind=:k") == 1 ]] ||
  { echo "fence_uncertain_foreign: retried lock acquisition" >&2; exit 1; }

run_case fence_lock_retry_success 0 v0.1.0
[[ $(count "$work/fence_lock_retry_success.calls" "SET operation_id=:o, operation_kind=:k") == 1 ]] ||
  { echo "fence_lock_retry_success: retried lock acquisition" >&2; exit 1; }

run_case fence_checkpoint_fails fail v0.1.0
f=$work/fence_checkpoint_fails.calls
absent "$f" "rds create-db-snapshot"
grep -qF "REMOVE operation_id" "$f" ||
  { echo "fence_checkpoint_fails: lock was not released" >&2; exit 1; }

run_case checkpoint_fails_before_app_start fail v0.1.0
f=$work/checkpoint_fails_before_app_start.calls
grep -qF -- "$start_web" "$f" ||
  { echo "checkpoint_fails_before_app_start: web never started" >&2; exit 1; }
[[ $(count "$f" "$up_maintenance") == 2 ]] ||
  { echo "checkpoint_fails_before_app_start: maintenance was not reconfirmed" >&2; exit 1; }
absent "$f" "$app_point"
grep -qF "REMOVE operation_id" "$f" ||
  { echo "checkpoint_fails_before_app_start: lock was not released" >&2; exit 1; }

run_case checkpoint_fails_after_migration fail v0.1.0
f=$work/checkpoint_fails_after_migration.calls
grep -qF -- "$up_maintenance" "$f" ||
  { echo "checkpoint_fails_after_migration: maintenance page not left up" >&2; exit 1; }
absent "$f" "$start_web"
grep -qF "REMOVE operation_id" "$f" ||
  { echo "checkpoint_fails_after_migration: lock was not released" >&2; exit 1; }

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

run_case fence_activate_not_running fail --activate v0.1.0
f=$work/fence_activate_not_running.calls
absent "$f" "SET operation_id=:o, operation_kind=:k"
grep -qF "not the activation candidate" "$work/fence_activate_not_running.out" ||
  { echo "fence_activate_not_running: no release-mismatch message" >&2; exit 1; }

run_case fence_activate_4002_then_4007 0 --activate v0.4.7
f=$work/fence_activate_4002_then_4007.calls
grep -qF "raised the release fence to v0.4.7" "$work/fence_activate_4002_then_4007.out" ||
  { echo "fence_activate_4002_then_4007: no raise message" >&2; exit 1; }
grep -qF ':c":{"N":"4007"}' "$f" || { echo "fence_activate_4002_then_4007: raise did not use 4007" >&2; exit 1; }

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
jq -e '.containerDefinitions[0].image == "ghcr.io/dannyota/aboutme-server@sha256:'"$(printf '%064d' 1)"'"
  and (.containerDefinitions[0].environment | any(.name == "DEPLOY_RELEASE_TAG" and .value == "v0.4.7"))' \
  "$work/last-registered.json" >/dev/null ||
  { echo "totp_reencrypt_ok: registered revision is not stamped with the tag's server image" >&2; exit 1; }

run_case totp_reencrypt_wait_timeout fail --totp-key-reencrypt v0.4.7
f=$work/totp_reencrypt_wait_timeout.calls
[[ $(grep -c "ecs wait tasks-stopped" "$f") -eq 4 ]] ||
  { echo "totp_reencrypt_wait_timeout: did not wait four times" >&2; exit 1; }
absent "$f" "REMOVE operation_id"
grep -qF "operation lock stays held" "$work/totp_reencrypt_wait_timeout.out" ||
  { echo "totp_reencrypt_wait_timeout: no held-lock message" >&2; exit 1; }

run_case totp_reencrypt_below_floor fail --totp-key-reencrypt v0.4.7
f=$work/totp_reencrypt_below_floor.calls
absent "$f" "SET operation_id=:o, operation_kind=:k"
absent "$f" "ecs run-task"
grep -qF "release fence is below v0.4.7" "$work/totp_reencrypt_below_floor.out" ||
  { echo "totp_reencrypt_below_floor: no floor message" >&2; exit 1; }

run_case totp_reencrypt_tag_mismatch fail --totp-key-reencrypt v0.4.7
f=$work/totp_reencrypt_tag_mismatch.calls
absent "$f" "ecs register-task-definition"
absent "$f" "ecs run-task"
grep -qF "REMOVE operation_id" "$f" ||
  { echo "totp_reencrypt_tag_mismatch: lock was not released" >&2; exit 1; }
grep -qF "is release 4006, not v0.4.7" "$work/totp_reencrypt_tag_mismatch.out" ||
  { echo "totp_reencrypt_tag_mismatch: no release-mismatch message" >&2; exit 1; }

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

run_case totp_key_secret_missing fail v0.4.7
f=$work/totp_key_secret_missing.calls
absent "$f" "SET operation_id=:o, operation_kind=:k"
absent "$f" "ecs register-task-definition"
grep -qF "run tofu apply for the TOTP key before deploying this release" \
  "$work/totp_key_secret_missing.out" ||
  { echo "totp_key_secret_missing: no TOTP key message" >&2; exit 1; }

run_case view_pass_key_secret_missing fail v0.6.26
f=$work/view_pass_key_secret_missing.calls
absent "$f" "SET operation_id=:o, operation_kind=:k"
absent "$f" "ecs register-task-definition"
grep -qF "for the view pass key before deploying this release" \
  "$work/view_pass_key_secret_missing.out" ||
  { echo "view_pass_key_secret_missing: no view pass key message" >&2; exit 1; }

run_case view_pass_key_present 0 v0.6.26
grep -qF "ecs register-task-definition" "$work/view_pass_key_present.calls" ||
  { echo "view_pass_key_present: did not register revisions" >&2; exit 1; }
absent "$work/view_pass_key_present.out" "view pass key"

run_case view_pass_key_below_floor 0 v0.6.25
grep -qF "ecs register-task-definition" "$work/view_pass_key_below_floor.calls" ||
  { echo "view_pass_key_below_floor: did not register revisions" >&2; exit 1; }
absent "$work/view_pass_key_below_floor.out" "view pass key"

run_case fence_missing_signin_enrolled fail v0.1.0
absent "$work/fence_missing_signin_enrolled.calls" "ecs register-task-definition"
grep -qF "does not prove sign in to view off" "$work/fence_missing_signin_enrolled.out" ||
  { echo "fence_missing_signin_enrolled: no sign-in-to-view message" >&2; exit 1; }

run_case fence_new_revision_signin_enrolled fail v0.1.0
f=$work/fence_new_revision_signin_enrolled.calls
absent "$f" "SET operation_id=:o, operation_kind=:k"
absent "$f" "ecs register-task-definition"
grep -qF "sign in to view on while the release fence is below v0.6.26" \
  "$work/fence_new_revision_signin_enrolled.out" ||
  { echo "fence_new_revision_signin_enrolled: no sign-in-to-view message" >&2; exit 1; }

run_case signin_enabled_at_floor 0 v0.6.26
grep -qF "ecs register-task-definition" "$work/signin_enabled_at_floor.calls" ||
  { echo "signin_enabled_at_floor: did not register revisions" >&2; exit 1; }
absent "$work/signin_enabled_at_floor.out" "run --activate first"

run_case signin_disabled_below_floor 0 v0.1.0
grep -qF "ecs register-task-definition" "$work/signin_disabled_below_floor.calls" ||
  { echo "signin_disabled_below_floor: did not register revisions" >&2; exit 1; }
absent "$work/signin_disabled_below_floor.out" "sign in to view"

run_case signin_rollback_below_floor fail --rollback v0.0.9
absent "$work/signin_rollback_below_floor.calls" "ecs register-task-definition"
grep -qF "below the release fence minimum" "$work/signin_rollback_below_floor.out" ||
  { echo "signin_rollback_below_floor: no fence-minimum message" >&2; exit 1; }

run_case fence_ci_fails fail v0.1.0
f=$work/fence_ci_fails.calls
absent "$f" "SET operation_id=:o, operation_kind=:k"
absent "$f" "REMOVE operation_id"

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

f=$work/ok.calls
sha=0123456789abcdef0123456789abcdef01234567
for name in server web caddy; do
  want="gh attestation verify oci://ghcr.io/dannyota/aboutme-$name@sha256:$(printf '%064d' 1) --repo dannyota/aboutme"
  want+=" --cert-identity https://github.com/dannyota/aboutme/.github/workflows/release-images.yml@refs/tags/v0.1.0"
  want+=" --source-ref refs/tags/v0.1.0 --source-digest $sha --deny-self-hosted-runners --format json"
  [[ $(count "$f" "$want") == 1 ]] || { echo "ok: $name provenance was not verified once with the full policy" >&2; exit 1; }
  before "$f" "$want" "operation_id=:o"
  before "$f" "$want" "rds create-db-snapshot"
  before "$f" "$want" "ecs register-task-definition"
done
policy() {
  printf 'gh attestation verify oci://ghcr.io/dannyota/aboutme-%s@sha256:%064d --repo dannyota/aboutme' "$1" 1
  printf ' --cert-identity https://github.com/dannyota/aboutme/.github/workflows/release-images.yml@refs/tags/%s' "$2"
  printf ' --source-ref refs/tags/%s --source-digest %s --deny-self-hosted-runners --format json' "$2" "$sha"
}
for name in server web caddy; do
  [[ $(count "$work/rollback.calls" "$(policy "$name" v0.0.9)") == 1 ]] ||
    { echo "rollback: $name provenance was not verified once for the rollback tag" >&2; exit 1; }
done
f=$work/totp_reencrypt_ok.calls
[[ $(count "$f" "$(policy server v0.4.7)") == 1 ]] ||
  { echo "totp_reencrypt_ok: server provenance was not verified once" >&2; exit 1; }
before "$f" "$(policy server v0.4.7)" "operation_id=:o"
before "$f" "$(policy server v0.4.7)" "ecs register-task-definition"
run_case totp_reencrypt_provenance_fails fail --totp-key-reencrypt v0.4.7
f=$work/totp_reencrypt_provenance_fails.calls
absent "$f" "operation_id=:o"
absent "$f" "ecs register-task-definition"
absent "$f" "ecs run-task"
grep -qF "provenance: no valid build provenance for server@" "$work/totp_reencrypt_provenance_fails.out" ||
  { echo "totp_reencrypt_provenance_fails: did not stop on the provenance check" >&2; exit 1; }

for case_ in provenance_fails provenance_wrong_subject; do
  run_case "$case_" fail v0.1.0
  f=$work/$case_.calls
  absent "$f" "operation_id=:o"
  absent "$f" "rds create-db-snapshot"
  absent "$f" "ecs register-task-definition"
  absent "$f" "ecs update-service"
  absent "$f" "gh attestation verify oci://ghcr.io/dannyota/aboutme-web@"
  grep -qF "provenance:" "$work/$case_.out" || { echo "$case_: no provenance message" >&2; exit 1; }
done

run_case index_tag 0 v0.1.0
f=$work/index_tag.calls
arm64=sha256:$(printf '%064d' 2)
for name in server web caddy; do
  grep -qF "gh attestation verify oci://ghcr.io/dannyota/aboutme-$name@$arm64 " "$f" ||
    { echo "index_tag: $name arm64 provenance was not verified" >&2; exit 1; }
  for other in 3 9; do
    absent "$f" "gh attestation verify oci://ghcr.io/dannyota/aboutme-$name@sha256:$(printf '%064d' "$other")"
  done
done
jq -e --arg d "$arm64" '[.containerDefinitions[].image | endswith("@" + $d)] | length > 0 and all' \
  "$work/last-registered.json" >/dev/null ||
  { echo "index_tag: a task definition does not pin the arm64 digest" >&2; exit 1; }
run_case index_no_arm64 fail v0.1.0
f=$work/index_no_arm64.calls
absent "$f" "gh attestation verify"
absent "$f" "operation_id=:o"
absent "$f" "ecs register-task-definition"
grep -qF "no server image for v0.1.0" "$work/index_no_arm64.out" ||
  { echo "index_no_arm64: did not stop on the missing arm64 image" >&2; exit 1; }

ROOT_CHECK_ONLY=1 ROOT_CHECK_EXIT=0 run_case public_root_clear 0 --check-public-root v0.1.0 guide
f=$work/public_root_clear.calls
grep -qF '{"resumeOccupied":false,"tombstoneOccupied":false}' "$work/public_root_clear.out" ||
  { echo "public_root_clear: missing clear result" >&2; exit 1; }
[[ $(count "$f" "$(policy server v0.1.0)") == 1 ]] ||
  { echo "public_root_clear: candidate server provenance was not verified" >&2; exit 1; }
before "$f" "operation_checked_at=:s" "ecs register-task-definition"
before "$f" "ecs register-task-definition" "--started-by root-"
absent "$f" "rds create-db-snapshot"
absent "$f" "ecs update-service"
absent "$f" "scheduler update-schedule"
absent "$f" "cloudwatch"
jq -e '.containerDefinitions[] | select(.name == "jobs")
  | .image == "ghcr.io/dannyota/aboutme-server@sha256:'"$(printf '%064d' 1)"'"
    and .entryPoint == ["/usr/local/bin/server"]
    and .command == ["idempotency-expiry-sweep"]
    and .secrets == [{"name":"PGPASSWORD","valueFrom":"arn:aws:ssm:ap-southeast-1:1:parameter/aboutme/prod/db/app-password"}]
    and (.environment | any(.name == "DEPLOY_RELEASE_TAG" and .value == "v0.1.0"))
    and (.environment | any(.name == "DEPLOY_RELEASE_NUMBER" and .value == "1000"))' \
  "$work/last-registered.json" >/dev/null ||
  { echo "public_root_clear: candidate task did not preserve jobs configuration" >&2; exit 1; }
grep -qF -- '--overrides {"containerOverrides":[{"name":"jobs","command":["check-public-root","guide"]}]}' "$f" ||
  { echo "public_root_clear: missing jobs command override" >&2; exit 1; }
grep -qF "public-root check task: arn:aws:ecs:" "$work/public_root_clear.out" ||
  { echo "public_root_clear: missing task correlation" >&2; exit 1; }

ROOT_CHECK_ONLY=1 ROOT_CHECK_EXIT=10 run_case public_root_resume 10 --check-public-root v0.1.0 guide
grep -qF '{"resumeOccupied":true,"tombstoneOccupied":false}' "$work/public_root_resume.out" ||
  { echo "public_root_resume: wrong result" >&2; exit 1; }
ROOT_CHECK_ONLY=1 ROOT_CHECK_EXIT=11 run_case public_root_tombstone 11 --check-public-root v0.1.0 guide
grep -qF '{"resumeOccupied":false,"tombstoneOccupied":true}' "$work/public_root_tombstone.out" ||
  { echo "public_root_tombstone: wrong result" >&2; exit 1; }
ROOT_CHECK_ONLY=1 ROOT_CHECK_EXIT=12 run_case public_root_both 12 --check-public-root v0.1.0 guide
grep -qF '{"resumeOccupied":true,"tombstoneOccupied":true}' "$work/public_root_both.out" ||
  { echo "public_root_both: wrong result" >&2; exit 1; }
ROOT_CHECK_ONLY=1 ROOT_CHECK_EXIT=1 run_case public_root_error fail --check-public-root v0.1.0 guide
grep -qF "public-root check exited with 1" "$work/public_root_error.out" ||
  { echo "public_root_error: missing fatal error" >&2; exit 1; }
for invalid_root in bad guide- a--b; do
  run_case "public_root_invalid_$invalid_root" fail --check-public-root v0.1.0 "$invalid_root"
  absent "$work/public_root_invalid_$invalid_root.calls" "aws "
done

ROOT_CHECK_ONLY=1 ROOT_CHECK_RUN_TASK_SEQUENCE=lost run_case public_root_lost_task fail --check-public-root v0.1.0 guide
f=$work/public_root_lost_task.calls
absent "$f" "REMOVE operation_id"
grep -qF "public-root check may still run; leave the operation lock closed; list tasks with started-by root-" "$work/public_root_lost_task.out" ||
  { echo "public_root_lost_task: missing lock-recovery direction" >&2; exit 1; }
ROOT_CHECK_ONLY=1 ROOT_CHECK_WAIT_SEQUENCE=fail run_case public_root_wait_pending fail --check-public-root v0.1.0 guide
f=$work/public_root_wait_pending.calls
absent "$f" "REMOVE operation_id"
grep -qF "public-root check may still run; leave the operation lock closed; list tasks with started-by root-" "$work/public_root_wait_pending.out" ||
  { echo "public_root_wait_pending: missing lock-recovery direction" >&2; exit 1; }

ROOT_CHECK_ONLY=1 ROOT_CHECK_EXIT_SEQUENCE=0,0 run_case require_free_root 0 v0.1.0 --require-free-root guide
f=$work/require_free_root.calls
[[ $(count "$f" "--started-by root-") == 2 ]] ||
  { echo "require_free_root: want two checks" >&2; exit 1; }
mapfile -t root_started_by < <(grep -oE -- '--started-by root-[^ ]+' "$f" | cut -d' ' -f2)
mapfile -t root_tokens < <(grep -oE -- '--client-token root-[^ ]+' "$f" | cut -d' ' -f2)
[[ ${#root_started_by[@]} == 2 && ${#root_tokens[@]} == 2 && ${root_started_by[0]} != "${root_started_by[1]}" && ${root_tokens[0]} != "${root_tokens[1]}" && ${root_started_by[0]%-*} == "${root_started_by[1]%-*}" && ${root_tokens[0]%-*} == "${root_tokens[1]%-*}" ]] ||
  { echo "require_free_root: audits did not use distinct operation-correlated tokens" >&2; exit 1; }
first_root=$(nth "$f" "--started-by root-" 1)
second_root=$(nth "$f" "--started-by root-" 2)
snapshot_line=$(line "$f" "rds create-db-snapshot")
stop_line=$(line "$f" "$stop_app")
maintenance_line=$(line "$f" "curl -s -m 10")
migrate_line=$(line "$f" "--started-by deploy-migrate")
[[ $first_root -lt $snapshot_line && $stop_line -lt $maintenance_line && $maintenance_line -lt $second_root && $second_root -lt $migrate_line ]] ||
  { echo "require_free_root: checks ran in the wrong order" >&2; exit 1; }

ROOT_CHECK_ONLY=1 ROOT_CHECK_EXIT_SEQUENCE=0,10 run_case require_free_root_raced fail v0.1.0 --require-free-root guide
f=$work/require_free_root_raced.calls
[[ $(count "$f" "--started-by root-") == 2 ]] ||
  { echo "require_free_root_raced: want two checks" >&2; exit 1; }
absent "$f" "--started-by deploy-migrate"
grep -qF -- "$prev_app_up" "$f" ||
  { echo "require_free_root_raced: previous app was not restored" >&2; exit 1; }
grep -qF -- "$down_maintenance" "$f" ||
  { echo "require_free_root_raced: maintenance was not stopped" >&2; exit 1; }
before "$f" "$prev_app_up" "$down_maintenance"
[[ $(count "$f" "scheduler update-schedule") == 8 ]] ||
  { echo "require_free_root_raced: schedules were not restored" >&2; exit 1; }

ROOT_CHECK_ONLY=1 ROOT_CHECK_EXIT_SEQUENCE=0,0 ROOT_CHECK_WAIT_SEQUENCE=ok,fail \
  run_case require_free_root_pending fail v0.1.0 --require-free-root guide
f=$work/require_free_root_pending.calls
absent "$f" "REMOVE operation_id"
grep -qF "public-root check may still run; leave the operation lock closed; list tasks with started-by root-" "$work/require_free_root_pending.out" ||
  { echo "require_free_root_pending: missing lock-recovery direction" >&2; exit 1; }
grep -qF -- "$maint_up1" "$f" ||
  { echo "require_free_root_pending: maintenance was not left up" >&2; exit 1; }
absent "$f" "--started-by deploy-migrate"

ROOT_CHECK_ONLY=1 ROOT_CHECK_EXIT_SEQUENCE=0,0 ROOT_CHECK_RUN_TASK_SEQUENCE=ok,lost \
  run_case require_free_root_lost_second fail v0.1.0 --require-free-root guide
f=$work/require_free_root_lost_second.calls
[[ $(count "$f" "--started-by root-") == 2 ]] ||
  { echo "require_free_root_lost_second: want two distinct audit requests" >&2; exit 1; }
absent "$f" "REMOVE operation_id"
absent "$f" "--started-by deploy-migrate"
grep -qF "public-root check may still run; leave the operation lock closed; list tasks with started-by root-" "$work/require_free_root_lost_second.out" ||
  { echo "require_free_root_lost_second: missing lock-recovery direction" >&2; exit 1; }

for bad in "" "bogus" "show" "show alpha-cv extra" "show Bad_Slug" "show a" "show -bad-" "confirm alpha-cv" \
  "confirm alpha-cv 0b1c2d3e-4f50-4617-8899-AABBCCDDEEFF" "confirm alpha-cv 0b1c2d3e-4f50-4617-8899-aabbccddeef" \
  "confirm alpha-cv 0b1c2d3e4f5046178899aabbccddeeff" "confirm alpha-cv xyz" "confirm Bad_Slug 0b1c2d3e-4f50-4617-8899-aabbccddeeff" \
  "confirm alpha-cv 0b1c2d3e-4f50-4617-8899-aabbccddeeff extra" "pending"; do
  name="account_delete_invalid_${bad//[^a-z0-9A-Z]/_}"
  # shellcheck disable=SC2086
  AD_ONLY=1 run_case "$name" 2 --account-delete v0.1.0 $bad
  absent "$work/$name.calls" "aws "
done
AD_ONLY=1 run_case account_delete_bad_tag fail --account-delete main show alpha-cv
absent "$work/account_delete_bad_tag.calls" "aws "

AD_ONLY=1 run_case account_delete_show 0 --account-delete v0.1.0 show alpha-cv
f=$work/account_delete_show.calls
grep -qF "account user=0b1c2d3e-4f50-4617-8899-aabbccddeeff slugs=alpha-cv,beta-cv" "$work/account_delete_show.out" ||
  { echo "account_delete_show: log lines were not printed" >&2; exit 1; }
absent "$work/account_delete_show.out" $'\033'
absent "$work/account_delete_show.out" "account-delete: end"
[[ $(count "$f" "$(policy server v0.1.0)") == 1 ]] ||
  { echo "account_delete_show: server provenance was not verified" >&2; exit 1; }
before "$f" "operation_checked_at=:s" "ecs register-task-definition"
before "$f" "ecs register-task-definition" "--started-by acctdel-"
before "$f" "--started-by acctdel-" "logs get-log-events"
before "$f" "logs get-log-events" "REMOVE operation_id"
absent "$f" "rds create-db-snapshot"
absent "$f" "ecs update-service"
absent "$f" "scheduler update-schedule"
grep -qF -- '--overrides {"containerOverrides":[{"name":"jobs","command":["account-delete","show","alpha-cv"]}]}' "$f" ||
  { echo "account_delete_show: missing jobs command override" >&2; exit 1; }
grep -qF "[test-base] aws --region ap-southeast-1 logs get-log-events --log-group-name /aboutme/prod --log-stream-name jobs/jobs/adtask1" "$f" ||
  { echo "account_delete_show: log was not read with the caller's login" >&2; exit 1; }
absent "$f" "[fence-deploy] aws --region ap-southeast-1 logs"
jq -e '.containerDefinitions[] | select(.name == "jobs")
  | .image == "ghcr.io/dannyota/aboutme-server@sha256:'"$(printf '%064d' 1)"'"
    and .command == ["idempotency-expiry-sweep"]
    and (.environment | any(.name == "DEPLOY_RELEASE_TAG" and .value == "v0.1.0"))' \
  "$work/last-registered.json" >/dev/null ||
  { echo "account_delete_show: task did not keep the jobs configuration" >&2; exit 1; }

AD_ONLY=1 run_case account_delete_confirm 0 --account-delete v0.1.0 confirm alpha-cv 0b1c2d3e-4f50-4617-8899-aabbccddeeff
grep -qF -- '"command":["account-delete","confirm","alpha-cv","0b1c2d3e-4f50-4617-8899-aabbccddeeff"]' "$work/account_delete_confirm.calls" ||
  { echo "account_delete_confirm: wrong command array" >&2; exit 1; }
grep -qF "account-delete confirm done for v0.1.0" "$work/account_delete_confirm.out" ||
  { echo "account_delete_confirm: no done message" >&2; exit 1; }

AD_ONLY=1 AD_EXIT=1 run_case account_delete_task_fails fail --account-delete v0.1.0 confirm alpha-cv 0b1c2d3e-4f50-4617-8899-aabbccddeeff
grep -qF "account user=0b1c2d3e" "$work/account_delete_task_fails.out" ||
  { echo "account_delete_task_fails: log was not printed" >&2; exit 1; }
grep -qF "account-delete exited with 1" "$work/account_delete_task_fails.out" ||
  { echo "account_delete_task_fails: no exit-code message" >&2; exit 1; }
absent "$work/account_delete_task_fails.out" "done for"
grep -qF "REMOVE operation_id" "$work/account_delete_task_fails.calls" ||
  { echo "account_delete_task_fails: lock was not released" >&2; exit 1; }

for mode in empty fail nosentinel; do
  AD_ONLY=1 AD_LOG=$mode DEPLOY_SMOKE_DELAY=0 run_case "account_delete_log_$mode" fail --account-delete v0.1.0 show alpha-cv
  grep -qF "no end marker, so the result is incomplete; run show" "$work/account_delete_log_$mode.out" ||
    { echo "account_delete_log_$mode: no incomplete-result message" >&2; exit 1; }
  grep -qF "REMOVE operation_id" "$work/account_delete_log_$mode.calls" ||
    { echo "account_delete_log_$mode: lock was not released" >&2; exit 1; }
  absent "$work/account_delete_log_$mode.out" "done for"
done
grep -qF "account user=0b1c2d3e" "$work/account_delete_log_nosentinel.out" ||
  { echo "account_delete_log_nosentinel: lines read were not printed" >&2; exit 1; }
[[ $(count "$work/account_delete_log_nosentinel.calls" "logs get-log-events") == 10 ]] ||
  { echo "account_delete_log_nosentinel: want five attempts of two pages" >&2; exit 1; }

AD_ONLY=1 AD_LOG=split run_case account_delete_log_split 0 --account-delete v0.1.0 show alpha-cv
[[ $(count "$work/account_delete_log_split.calls" "logs get-log-events") == 3 ]] ||
  { echo "account_delete_log_split: want three pages" >&2; exit 1; }
[[ $(count "$work/account_delete_log_split.calls" "--next-token f/1") == 1 ]] ||
  { echo "account_delete_log_split: did not follow the first token" >&2; exit 1; }
grep -qF "account user=0b1c2d3e" "$work/account_delete_log_split.out" ||
  { echo "account_delete_log_split: first page was not printed" >&2; exit 1; }
absent "$work/account_delete_log_split.out" "account-delete: end"

for pair in "4:not found:" "5:mismatch:"; do
  code=${pair%%:*} text=${pair#*:} text=${text%:}
  AD_ONLY=1 AD_EXIT=$code run_case "account_delete_exit_$code" "$code" --account-delete v0.1.0 confirm alpha-cv 0b1c2d3e-4f50-4617-8899-aabbccddeeff
  grep -qF "account user=0b1c2d3e" "$work/account_delete_exit_$code.out" ||
    { echo "account_delete_exit_$code: log was not printed" >&2; exit 1; }
  grep -qF "$text" "$work/account_delete_exit_$code.out" ||
    { echo "account_delete_exit_$code: no '$text' message" >&2; exit 1; }
  grep -qF "nothing deleted" "$work/account_delete_exit_$code.out" ||
    { echo "account_delete_exit_$code: no nothing-deleted message" >&2; exit 1; }
  absent "$work/account_delete_exit_$code.out" "done for"
  grep -qF "REMOVE operation_id" "$work/account_delete_exit_$code.calls" ||
    { echo "account_delete_exit_$code: lock was not released" >&2; exit 1; }
done

AD_ONLY=1 AD_EXIT=3 run_case account_delete_exit_3 3 --account-delete v0.1.0 confirm alpha-cv 0b1c2d3e-4f50-4617-8899-aabbccddeeff
AD_ONLY=1 AD_EXIT=1 run_case account_delete_exit_1 1 --account-delete v0.1.0 confirm alpha-cv 0b1c2d3e-4f50-4617-8899-aabbccddeeff
for code in 1 3; do
  f=$work/account_delete_exit_$code
  grep -qF "account user=0b1c2d3e" "$f.out" ||
    { echo "account_delete_exit_$code: log was not printed" >&2; exit 1; }
  absent "$f.out" "done for"
  absent "$f.out" "no end marker"
  [[ $(count "$f.calls" "logs get-log-events") == 2 ]] ||
    { echo "account_delete_exit_$code: want one pass of two pages" >&2; exit 1; }
  grep -qF "REMOVE operation_id" "$f.calls" ||
    { echo "account_delete_exit_$code: lock was not released" >&2; exit 1; }
done
grep -qF "result unknown: run show <slug> to confirm" "$work/account_delete_exit_3.out" ||
  { echo "account_delete_exit_3: no unknown-result message" >&2; exit 1; }

AD_ONLY=1 AD_APP_RELEASE=999 run_case account_delete_tag_mismatch fail --account-delete v0.1.0 show alpha-cv
f=$work/account_delete_tag_mismatch.calls
absent "$f" "ecs register-task-definition"
absent "$f" "ecs run-task"
grep -qF "REMOVE operation_id" "$f" ||
  { echo "account_delete_tag_mismatch: lock was not released" >&2; exit 1; }
grep -qF "is release 999, not v0.1.0" "$work/account_delete_tag_mismatch.out" ||
  { echo "account_delete_tag_mismatch: no release-mismatch message" >&2; exit 1; }

AD_ONLY=1 AD_RUN=lost run_case account_delete_lost_task fail --account-delete v0.1.0 confirm alpha-cv 0b1c2d3e-4f50-4617-8899-aabbccddeeff
AD_ONLY=1 AD_WAIT=fail run_case account_delete_wait_fails fail --account-delete v0.1.0 confirm alpha-cv 0b1c2d3e-4f50-4617-8899-aabbccddeeff
for case_ in account_delete_lost_task account_delete_wait_fails; do
  absent "$work/$case_.calls" "REMOVE operation_id"
  absent "$work/$case_.calls" "logs get-log-events"
  grep -qF "the operation lock stays held until the task stops" "$work/$case_.out" ||
    { echo "$case_: no held-lock message" >&2; exit 1; }
done

run_case usage 2

bash "$here/dns-check_test.sh"
bash "$here/provenance_test.sh"
echo "deploy-script-test: ok"
