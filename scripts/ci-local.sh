#!/usr/bin/env bash
# Run local CI under the shared lock and owned-child deadline.
set -Eeuo pipefail

readonly TIMEOUT_SECONDS=3600
readonly LOCK_WAIT_SECONDS=${CI_LOCAL_LOCK_WAIT:-300}
SCRIPT=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/ci-local.sh
# shellcheck source=scripts/lib/platform.sh
source "$(dirname "$SCRIPT")/lib/platform.sh"
require_modern_bash || exit 2
command -v python3 >/dev/null || { printf 'ci-local: python3 is required\n' >&2; exit 2; }
ROOT=$(git rev-parse --show-toplevel)
COMMON=$(git rev-parse --path-format=absolute --git-common-dir)
LOCK=$COMMON/aboutme-local-check.lock
cd "$ROOT"
say() { printf 'ci-local: %s\n' "$*"; }

outer() {
  local status=0 lock_pid= pending=
  trap 'pending=INT; [ -z "$lock_pid" ] || kill -INT "$lock_pid" 2>/dev/null || true' INT
  trap 'pending=TERM; [ -z "$lock_pid" ] || kill -TERM "$lock_pid" 2>/dev/null || true' TERM
  python3 "$ROOT/scripts/lib/local-check.py" "$LOCK" "$LOCK_WAIT_SECONDS" \
    bash "$SCRIPT" --locked &
  lock_pid=$!
  [ -z "$pending" ] || kill -"$pending" "$lock_pid" 2>/dev/null || true
  while true; do
    status=0
    wait "$lock_pid" || status=$?
    kill -0 "$lock_pid" 2>/dev/null || break
  done
  trap - INT TERM
  case $status in
  0 | 1 | 2) exit "$status" ;;
  75) say "another local check held $LOCK or the lock could not be acquired; try again later"; say 'summary: FAIL lock acquisition; CI did not run'; exit 2 ;;
  125) say 'refused: watchdog could not start'; say 'summary: FAIL watchdog refusal; CI did not run'; exit 2 ;;
  *) say "FAIL: watchdog stopped the check (exit $status)"; exit 1 ;;
  esac
}

locked() {
  exec env PYTHONDONTWRITEBYTECODE=1 bash "$ROOT/scripts/run-owned.sh" \
    --timeout "$TIMEOUT_SECONDS" -- bash "$SCRIPT" --capped
}

LOG=

finish() {
  local status=$?
  say 'summary'
  if [[ -n $LOG && -f $LOG ]]; then
    awk '/^--- .* OK \(/ { print "  PASS " $0 }
         /^--- .* FAILED \(/ { print "  FAIL " $0 }
         /^Skipping .*CI covers them\./ { print "  SKIP " $0 }' "$LOG"
  fi
  if ((status)); then say "FAIL: local CI exited $status"; else say 'PASS: local CI'; fi
  [[ -z $LOG ]] || rm -f -- "$LOG"
}

capped() {
  trap finish EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  local dir tree_status
  bash "$ROOT/scripts/pre-push.sh" --check-dependencies \
    node_modules apps/web/node_modules apps/web/.nuxt apps/web/.output \
    packages/schema/node_modules deploy/dev-https-browser/node_modules || exit 2
  if ! tree_status=$(git status --porcelain --untracked-files=all); then
    say 'refused: could not check worktree status; CI did not run'
    exit 2
  fi
  if [[ -n $tree_status ]]; then
    say 'refused: worktree and index must be clean before running local CI'
    exit 2
  fi
  for dir in . apps/web packages/schema deploy/dev-https-browser; do
    if ! (cd "$ROOT/$dir" && npm ci --prefer-offline --no-audit --no-fund); then
      say "refused: dependency installation failed in $dir; CI did not run"
      exit 2
    fi
  done
  LOG=$(mktemp "${TMPDIR:-/tmp}/aboutme-ci-local.XXXXXX")
  local status=0
  local -a statuses
  bash "$ROOT/scripts/ci.sh" 2>&1 | tee "$LOG" || {
    statuses=("${PIPESTATUS[@]}")
    status=${statuses[0]}
    ((status)) || status=${statuses[1]}
  }
  return "$status"
}

case ${1:-} in
'') outer ;;
--locked) locked ;;
--capped) capped ;;
*) say 'usage: scripts/ci-local.sh'; exit 2 ;;
esac
