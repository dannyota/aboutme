#!/usr/bin/env bash
# Checks deploy.sh step order and failure recovery against stubbed commands.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/bin"
for cmd in aws gh git curl; do
  cat >"$work/bin/$cmd" <<'STUB'
#!/usr/bin/env bash
printf '%s %s\n' "$(basename "$0")" "$*" >>"$CALLS"
exec bash "$STUB_DIR/respond" "$(basename "$0")" "$@"
STUB
  chmod +x "$work/bin/$cmd"
done
cp "$here/testdata/respond" "$work/respond"

run_case() { # name expected-exit args...
  local name=$1 want=$2 got
  shift 2
  : >"$work/$name.calls"
  set +e
  CALLS="$work/$name.calls" STUB_DIR="$work" STUB_CASE="$name" PATH="$work/bin:$PATH" \
    DEPLOY_SMOKE_TIMEOUT=1 bash "$here/deploy.sh" "$@" >"$work/$name.out" 2>&1
  got=$?
  set -e
  if ((got != want)); then
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
absent "$f" "deploy-db-bootstrap"
[[ $(count "$f" "scheduler update-schedule") == 8 ]] || { echo "ok: want 8 schedule updates" >&2; exit 1; }
grep -q '"State": "ENABLED"' "$work/last-schedule.json" || { echo "ok: schedules not enabled at the end" >&2; exit 1; }

run_case first 0 v0.1.0 --first-deploy
f=$work/first.calls
before "$f" "--started-by deploy-db-bootstrap" "--started-by deploy-db-provision"
before "$f" "--started-by deploy-db-provision" "--started-by deploy-db-set-login"
before "$f" "--started-by deploy-db-set-login" "--started-by deploy-migrate"
grep -q '"State": "ENABLED"' "$work/last-schedule.json" || { echo "first: schedules not enabled at the end" >&2; exit 1; }

run_case migrate_fails 1 v0.1.0
f=$work/migrate_fails.calls
grep -q -F -- "--service aboutme-prod-app --task-definition arn:aws:ecs:ap-southeast-1:1:task-definition/aboutme-prod-app:3 --desired-count 1" "$f" ||
  { echo "migrate_fails: previous app not restored" >&2; exit 1; }
absent "$f" "$start_app"
[[ $(count "$f" "scheduler update-schedule") == 8 ]] || { echo "migrate_fails: schedules not restored" >&2; exit 1; }
grep -q '"State": "ENABLED"' "$work/last-schedule.json" || { echo "migrate_fails: schedules left disabled" >&2; exit 1; }

run_case first_migrate_fails 1 v0.1.0 --first-deploy
f=$work/first_migrate_fails.calls
absent "$f" "--desired-count 1"
[[ $(count "$f" "scheduler update-schedule") == 4 ]] || { echo "first_migrate_fails: schedules must stay disabled" >&2; exit 1; }

run_case rollback 0 --rollback v0.0.9
f=$work/rollback.calls
absent "$f" "deploy-migrate"
absent "$f" "create-db-snapshot"

run_case usage 2
echo "deploy-script-test: ok"
