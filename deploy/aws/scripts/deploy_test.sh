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

run_case ok 0 v0.1.0
f=$work/ok.calls
before "$f" "rds create-db-snapshot" "$stop_app"
before "$f" "scheduler update-schedule" "$stop_app"
before "$f" "$stop_app" "--started-by deploy-migrate"
before "$f" "--started-by deploy-migrate" "$start_app"
before "$f" "--service aboutme-prod-web --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/new:4 --desired-count 1" "$start_app"
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

run_case migrate_fails fail v0.1.0
f=$work/migrate_fails.calls
grep -q -F -- "--service aboutme-prod-app --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/aboutme-prod-app:3 --desired-count 1" "$f" ||
  { echo "migrate_fails: previous app not restored" >&2; exit 1; }
absent "$f" "$start_app"
[[ $(count "$f" "scheduler update-schedule") == 8 ]] || { echo "migrate_fails: schedules not restored" >&2; exit 1; }
grep -q '"State": "ENABLED"' "$work/last-schedule.json" || { echo "migrate_fails: schedules left disabled" >&2; exit 1; }

run_case first_migrate_fails fail v0.1.0 --first-deploy
f=$work/first_migrate_fails.calls
absent "$f" "--desired-count 1"
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "first_migrate_fails: schedules must stay disabled" >&2; exit 1; }

run_case first_db_setup_fails fail v0.1.0 --first-deploy
f=$work/first_db_setup_fails.calls
absent "$f" "deploy-migrate"
absent "$f" "--desired-count 1"
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "first_db_setup_fails: schedules must stay disabled" >&2; exit 1; }

run_case runtask_fails fail v0.1.0
f=$work/runtask_fails.calls
grep -q -F -- "--service aboutme-prod-app --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/aboutme-prod-app:3 --desired-count 1" "$f" ||
  { echo "runtask_fails: previous app not restored" >&2; exit 1; }
[[ $(count "$f" "scheduler update-schedule") == 8 ]] || { echo "runtask_fails: schedules not restored" >&2; exit 1; }

run_case wait_timeout fail v0.1.0
f=$work/wait_timeout.calls
absent "$f" "--desired-count 1"
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "wait_timeout: schedules must stay disabled" >&2; exit 1; }
grep -q "may still be running" "$work/wait_timeout.out" || { echo "wait_timeout: no warning about the running task" >&2; exit 1; }

run_case start_fails fail v0.1.0
f=$work/start_fails.calls
absent "$f" "task-definition/aboutme-prod-app:3 --desired-count 1"
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "start_fails: schedules must stay disabled after a migration" >&2; exit 1; }
grep -q "migrations were applied" "$work/start_fails.out" || { echo "start_fails: no warning about applied migrations" >&2; exit 1; }

run_case bad_flag 2 v0.1.0 --first_deploy
[[ ! -s $work/bad_flag.calls ]] || { echo "bad_flag: commands ran" >&2; exit 1; }

run_case extra_arg 2 --rollback v0.0.9 extra
[[ ! -s $work/extra_arg.calls ]] || { echo "extra_arg: commands ran" >&2; exit 1; }

run_case rollback 0 --rollback v0.0.9
f=$work/rollback.calls
absent "$f" "deploy-migrate"
absent "$f" "create-db-snapshot"
before "$f" "ssm describe-parameters" "ecs register-task-definition"

# deploy.sh never deletes a snapshot; release-snapshot-sweep owns that.
for calls in "$work"/*.calls; do
  absent "$calls" "delete-db-snapshot"
done

run_case usage 2
echo "deploy-script-test: ok"
