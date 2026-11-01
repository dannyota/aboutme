#!/usr/bin/env bash
# Run local CI under the shared lock and memory watchdog.
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
MAIN=$(git worktree list --porcelain | sed -n '1s/^worktree //p')
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
  exec env RUN_CAPPED_REPORT=1 bash "$ROOT/scripts/run-capped.sh" \
    --timeout "$TIMEOUT_SECONDS" -- bash "$SCRIPT" --capped
}

declare -a CREATED=()
LOG=
cleanup() {
  local path
  for path in "${CREATED[@]}"; do rm -rf -- "$path"; done
  [[ -z $LOG ]] || rm -f -- "$LOG"
}

# Check the same installed lint packages and lockfile drift as pre-push.
modules_for() {
  local dir=$1 modules=$ROOT/$1/node_modules drift
  shift
  if [[ ! -d $modules ]]; then
    modules=$MAIN/$dir/node_modules
    [[ -d $modules ]] || { say "refused: no node_modules in $ROOT/$dir or $MAIN/$dir"; return 1; }
  fi
  if [[ $ROOT != "$MAIN" && ( $modules == "$MAIN/$dir/node_modules" ||
    ( -L $ROOT/$dir/node_modules && $(readlink "$ROOT/$dir/node_modules") == "$MAIN/$dir/node_modules" ) ) ]]; then
    if ! cmp -s "$ROOT/$dir/package.json" "$MAIN/$dir/package.json" ||
      ! cmp -s "$ROOT/$dir/package-lock.json" "$MAIN/$dir/package-lock.json"; then
      say "refused: $dir manifest or lockfile differs from the main checkout"
      return 1
    fi
  fi
  command -v node >/dev/null || { say 'refused: node is not installed'; return 1; }
  drift=$(node - "$ROOT/$dir/package-lock.json" "$modules" "$@" <<'NODE'
const fs = require('node:fs');
const [lockPath, modules, ...names] = process.argv.slice(2);
const lock = JSON.parse(fs.readFileSync(lockPath, 'utf8')).packages;
for (const name of names) {
  const want = lock[`node_modules/${name}`]?.version ?? 'absent';
  let have = 'absent';
  try { have = JSON.parse(fs.readFileSync(`${modules}/${name}/package.json`, 'utf8')).version; } catch {}
  if (have !== want) console.log(`${name} installed ${have}, lockfile ${want}`);
}
const installed = JSON.parse(fs.readFileSync(`${modules}/.package-lock.json`, 'utf8')).packages;
for (const [path, entry] of Object.entries(installed)) {
  if (entry.version && lock[path]?.version !== entry.version)
    console.log(`${path} installed ${entry.version}, lockfile ${lock[path]?.version ?? 'absent'}`);
}
NODE
  ) || { say "refused: cannot inspect $dir/package-lock.json"; return 1; }
  if [[ -n $drift ]]; then
    say "refused: stale $modules; run npm ci in $(dirname "$modules")"
    printf '%s\n' "$drift"
    return 1
  fi
  if [[ $modules != "$ROOT/$dir/node_modules" ]]; then
    ln -s -- "$modules" "$ROOT/$dir/node_modules" || return 1
    CREATED+=("$ROOT/$dir/node_modules")
  fi
}

schema_matches_main() {
  local path
  local -a paths=()
  mapfile -t paths < <(
    { git -C "$ROOT" ls-files --cached --others --exclude-standard -- packages/schema
      git -C "$MAIN" ls-files --cached --others --exclude-standard -- packages/schema; } | sort -u
  )
  for path in "${paths[@]}"; do
    if [[ ! -f $ROOT/$path && ! -f $MAIN/$path ]]; then continue; fi
    if ! cmp -s "$ROOT/$path" "$MAIN/$path"; then
      say 'refused: borrowed web dependencies link to different main checkout schema sources; install web dependencies in this worktree'
      return 1
    fi
  done
}

borrow() {
  local web=$ROOT/apps/web
  # Reclaim only paths marked by an interrupted run of this wrapper.
  if [[ $ROOT != "$MAIN" ]]; then
    [[ -L $ROOT/node_modules && $(readlink "$ROOT/node_modules") == "$MAIN/./node_modules" ]] && CREATED+=("$ROOT/node_modules")
    [[ -L $web/node_modules && $(readlink "$web/node_modules") == "$MAIN/apps/web/node_modules" ]] && CREATED+=("$web/node_modules")
    [[ ! -f $web/.nuxt/.ci-local-copy ]] || rm -rf -- "$web/.nuxt"
  fi
  modules_for . prettier markdownlint-cli2 markdownlint || return 1
  if [[ $ROOT != "$MAIN" && -L $MAIN/apps/web/node_modules/@aboutme/schema &&
    ( ! -d $web/node_modules || ( -L $web/node_modules &&
      $(readlink "$web/node_modules") == "$MAIN/apps/web/node_modules" ) ) ]]; then
    schema_matches_main || return 1
  fi
  modules_for apps/web eslint @nuxt/eslint @nuxt/eslint-config \
    @typescript-eslint/parser eslint-plugin-vue vue-eslint-parser @stylistic/eslint-plugin || return 1
  if [[ ! -e $web/.nuxt ]]; then
    [[ -f $MAIN/apps/web/.nuxt/eslint.config.mjs ]] || {
      say "refused: no generated $MAIN/apps/web/.nuxt; generate it in the main checkout"
      return 1
    }
    mkdir "$web/.nuxt" || return 1
    CREATED+=("$web/.nuxt")
    : >"$web/.nuxt/.ci-local-copy" || return 1
    cp -R -p -P "$MAIN/apps/web/.nuxt/." "$web/.nuxt/" || return 1
  fi
}

finish() {
  local status=$?
  say 'summary'
  if [[ -n $LOG && -f $LOG ]]; then
    awk '/^--- .* OK \(/ { print "  PASS " $0 }
         /^--- .* FAILED \(/ { print "  FAIL " $0 }
         /^Skipping .*CI covers them\./ { print "  SKIP " $0 }' "$LOG"
  fi
  if ((status)); then say "FAIL: local CI exited $status"; else say 'PASS: local CI'; fi
  cleanup
}

capped() {
  trap finish EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  if ! borrow; then say 'refused: dependency preparation failed; CI did not run'; exit 2; fi
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
