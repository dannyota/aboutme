#!/usr/bin/env bash
# Checks deploy.sh step order and failure recovery against stubbed commands.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

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
  CALLS="$work/$name.calls" STUB_DIR="$work" STUB_CASE="$name" PATH="$work/bin:$PATH" \
    AWS_CONFIG_FILE="$work/no-such-aws-config" AWS_PROFILE=test-base \
    DEPLOY_SMOKE_TIMEOUT=1 DEPLOY_SMOKE_DELAY=0 bash "$here/deploy.sh" "$@" >"$work/$name.out" 2>&1
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

count() { grep -c -F -- "$2" "$1" || true; }

stop_app="--service aboutme-prod-app --desired-count 0"
start_app="--service aboutme-prod-app --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/new:4 --desired-count 1"
start_web="--service aboutme-prod-web --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/new:4 --desired-count 1"
up_maintenance="--service aboutme-prod-maintenance --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/new:4 --desired-count 1"
down_maintenance="--service aboutme-prod-maintenance --desired-count 0"
prev_app_up="--service aboutme-prod-app --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/aboutme-prod-app:3 --desired-count 1"

run_case ok 0 v0.1.0
f=$work/ok.calls
before "$f" "rds create-db-snapshot" "$stop_app"
before "$f" "scheduler update-schedule" "$stop_app"
before "$f" "$stop_app" "--started-by deploy-migrate"
before "$f" "--started-by deploy-migrate" "$start_app"
before "$f" "$start_web" "$start_app"
# The maintenance page covers the window between the app stopping and
# starting again: up right after site-down, down only once web is back and
# right before app starts, since maintenance and app share host port 443.
before "$f" "$stop_app" "$up_maintenance"
before "$f" "$up_maintenance" "--started-by deploy-migrate"
before "$f" "$start_web" "$down_maintenance"
before "$f" "$down_maintenance" "$start_app"
absent "$f" "deploy-db-setup"
[[ $(count "$f" "scheduler update-schedule") == 8 ]] || { echo "ok: want 8 schedule updates" >&2; exit 1; }
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

# Existing disabled states must stay disabled and need no mutation.
run_case alerts_initially_disabled 0 v0.1.0
f=$work/alerts_initially_disabled.calls
absent "$f" "events disable-rule --name aboutme-prod-task-stopped"
absent "$f" "events enable-rule --name aboutme-prod-task-stopped"
absent "$f" "cloudwatch disable-alarm-actions --alarm-names aboutme-prod-site-down"
absent "$f" "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down"

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
[[ $(count "$f" "$start_app") == 1 ]] || { echo "alerts_restore_fails: app start changed" >&2; exit 1; }
absent "$f" "$prev_app_up"

# A failed service-recovery step must not bypass notification cleanup. The
# original deployment failure remains the process result, and no unsafe ECS
# start follows the failed recovery state check.
run_case alerts_recovery_fails fail v0.1.0
f=$work/alerts_recovery_fails.calls
grep -qF -- "events enable-rule --name aboutme-prod-task-stopped" "$f" ||
  { echo "alerts_recovery_fails: task-stopped rule was not restored" >&2; exit 1; }
grep -qF -- "cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down" "$f" ||
  { echo "alerts_recovery_fails: site-down actions were not restored" >&2; exit 1; }
absent "$f" "$prev_app_up"
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
absent "$f" "get-parameter"
absent "$f" "get-secret-value"

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

# The direct-origin check fails closed: one answer fails the deploy, and an
# unknown origin address is a failure, never a skip.
run_case origin_open fail v0.1.0
[[ $(count "$work/origin_open.calls" "https://192.0.2.10/") == 1 ]] ||
  { echo "origin_open: an answering origin must fail on the first probe" >&2; exit 1; }
grep -q "the origin answered a direct request" "$work/origin_open.out" || { echo "origin_open: no failure message" >&2; exit 1; }
run_case origin_unknown fail v0.1.0
grep -q "smoke: could not resolve the origin address" "$work/origin_unknown.out" ||
  { echo "origin_unknown: no failure message" >&2; exit 1; }

run_case first 0 v0.1.0 --first-deploy
f=$work/first.calls
before "$f" "--started-by deploy-db-setup" "--started-by deploy-migrate"
grep -q '"State": "ENABLED"' "$work/last-schedule.json" || { echo "first: schedules not enabled at the end" >&2; exit 1; }

# ListTasks failures must stop the deploy before a database task. The recovery
# path must not start maintenance when it cannot prove that app stopped.
for list_failure in list_running_fails list_stopped_fails; do
  run_case "$list_failure" fail v0.1.0
  f=$work/$list_failure.calls
  absent "$f" "deploy-migrate"
  absent "$f" "$up_maintenance"
  grep -qF -- "$prev_app_up" "$f" || { echo "$list_failure: previous app not restored" >&2; exit 1; }
  [[ $(count "$f" "scheduler update-schedule") == 8 ]] || { echo "$list_failure: schedules not restored" >&2; exit 1; }
done

# The post-scale STOPPED query includes a task that was already stopping. The
# task-stopped waiter must include it before another service may claim :443.
run_case stopping_task 0 v0.1.0
f=$work/stopping_task.calls
grep -q -- "ecs wait tasks-stopped.*running-task.*stopping-task" "$f" || { echo "stopping_task: not all tasks were waited" >&2; exit 1; }

# On the first deploy, an uncertain app stop must be retried and confirmed
# before maintenance starts. Job schedules remain disabled because db-setup
# may not have created usable application state.
run_case first_app_down_fails fail v0.1.0 --first-deploy
f=$work/first_app_down_fails.calls
before "$f" "$stop_app" "$up_maintenance"
[[ $(count "$f" "$stop_app") == 2 ]] || { echo "first_app_down_fails: app stop was not retried" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "first_app_down_fails: schedules must stay disabled" >&2; exit 1; }
absent "$f" "deploy-migrate"

# The maintenance page must prove it is live through Cloudflare before a
# database task can start. Otherwise the rollback path restores the app and
# schedules without any migration risk.
run_case maintenance_smoke_fails fail v0.1.0
f=$work/maintenance_smoke_fails.calls
absent "$f" "deploy-migrate"
grep -qF -- "$prev_app_up" "$f" || { echo "maintenance_smoke_fails: previous app not restored" >&2; exit 1; }
grep -qF -- "$down_maintenance" "$f" || { echo "maintenance_smoke_fails: maintenance page not turned off" >&2; exit 1; }
before "$f" "$down_maintenance" "$prev_app_up"
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
# maintenance page up rather than leaving nothing answering port 443.
grep -qF -- "$up_maintenance" "$f" || { echo "first_migrate_fails: maintenance page not left up" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "first_migrate_fails: schedules must stay disabled" >&2; exit 1; }

run_case first_db_setup_fails fail v0.1.0 --first-deploy
f=$work/first_db_setup_fails.calls
absent "$f" "deploy-migrate"
absent "$f" "$start_app"
absent "$f" "$prev_app_up"
grep -qF -- "$up_maintenance" "$f" || { echo "first_db_setup_fails: maintenance page not left up" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "first_db_setup_fails: schedules must stay disabled" >&2; exit 1; }

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
# back up) rather than leaving nothing answering port 443.
grep -qF -- "$up_maintenance" "$f" || { echo "start_fails: maintenance page not left up" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "start_fails: schedules must stay disabled after a migration" >&2; exit 1; }
grep -q "migration may have been applied" "$work/start_fails.out" || { echo "start_fails: no migration warning" >&2; exit 1; }

# An update-service response can be lost after ECS accepted it. The deploy
# marks the app start before the request, scales the app back to zero, then
# starts maintenance. It never assumes port 443 stayed free.
run_case app_start_update_fails fail v0.1.0
f=$work/app_start_update_fails.calls
grep -qF -- "$stop_app" "$f" || { echo "app_start_update_fails: app was not scaled to zero" >&2; exit 1; }
[[ $(count "$f" "$up_maintenance") == 2 ]] || { echo "app_start_update_fails: maintenance was not restored" >&2; exit 1; }
absent "$f" "$prev_app_up"
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "app_start_update_fails: schedules must stay disabled" >&2; exit 1; }

# If maintenance cannot be confirmed stopped during restoration, do not start
# the previous app and create a host-port collision.
run_case maintenance_down_fails fail v0.1.0
f=$work/maintenance_down_fails.calls
absent "$f" "$prev_app_up"
grep -q "maintenance released host port 443" "$work/maintenance_down_fails.out" || { echo "maintenance_down_fails: no port warning" >&2; exit 1; }

run_case bad_flag 2 v0.1.0 --first_deploy
[[ ! -s $work/bad_flag.calls ]] || { echo "bad_flag: commands ran" >&2; exit 1; }

run_case extra_arg 2 --rollback v0.0.9 extra
[[ ! -s $work/extra_arg.calls ]] || { echo "extra_arg: commands ran" >&2; exit 1; }

run_case rollback 0 --rollback v0.0.9
f=$work/rollback.calls
absent "$f" "deploy-migrate"
absent "$f" "create-db-snapshot"
before "$f" "ssm describe-parameters" "ecs register-task-definition"
# A rollback still swaps the maintenance page in and out around host port 443.
before "$f" "$stop_app" "$up_maintenance"
before "$f" "$down_maintenance" "$start_app"

# A rollback runs through the same fence-aware script and is rejected the
# same way a forward deploy is when the target is below the fence minimum.
run_case rollback_below_fence fail --rollback v0.0.9
absent "$work/rollback_below_fence.calls" "ecs register-task-definition"
grep -qF "below the release fence minimum" "$work/rollback_below_fence.out" ||
  { echo "rollback_below_fence: no fence-minimum message" >&2; exit 1; }

# A rollback target equal to the fence minimum is accepted.
run_case rollback_equal_fence 0 --rollback v0.0.9

# restore() refuses a previous_app below the current fence minimum instead of
# restarting a release the fence no longer allows; maintenance stays up.
run_case restore_below_fence fail --rollback v0.5.0
f=$work/restore_below_fence.calls
absent "$f" "$prev_app_up"
absent "$f" "$down_maintenance"
grep -qF "is below the fence minimum" "$work/restore_below_fence.out" ||
  { echo "restore_below_fence: no below-fence message" >&2; exit 1; }

# Bringing the maintenance page up can lose its steady-state confirmation. The
# recovery path must not start the previous app if it cannot prove that
# maintenance released host port 443.
run_case maint_up_fails fail v0.1.0
f=$work/maint_up_fails.calls
absent "$f" "$prev_app_up"
grep -qF -- "$down_maintenance" "$f" || { echo "maint_up_fails: maintenance was not stopped" >&2; exit 1; }
grep -q "maintenance released host port 443" "$work/maint_up_fails.out" || { echo "maint_up_fails: no port warning" >&2; exit 1; }
absent "$f" "deploy-migrate"
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "maint_up_fails: schedules must stay disabled" >&2; exit 1; }

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

# A checkpoint failure right after maintenance comes down (section 8, just
# before the app start it guards) must not leave the site fully dark:
# recovery brings maintenance back up rather than leaving both it and the
# app at zero.
run_case checkpoint_fails_after_maintenance_down fail v0.1.0
f=$work/checkpoint_fails_after_maintenance_down.calls
grep -qF -- "$down_maintenance" "$f" ||
  { echo "checkpoint_fails_after_maintenance_down: maintenance never came down" >&2; exit 1; }
[[ $(count "$f" "$up_maintenance") == 2 ]] ||
  { echo "checkpoint_fails_after_maintenance_down: maintenance was not restored after coming down" >&2; exit 1; }
absent "$f" "$start_app"
grep -qF "REMOVE operation_id" "$f" ||
  { echo "checkpoint_fails_after_maintenance_down: lock was not released" >&2; exit 1; }

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
echo "deploy-script-test: ok"
