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
printf '%s %s\n' "$(basename "$0")" "$*" >>"$CALLS"
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

run_case usage 2
echo "deploy-script-test: ok"
