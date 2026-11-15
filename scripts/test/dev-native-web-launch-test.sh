#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-dev-native-web.XXXXXX")
WORK=$(cd "$WORK" && pwd -P)
REPO=$WORK/repo

cleanup() {
  if [[ -s $REPO/.dev/web.pid ]]; then kill -TERM "$(cat "$REPO/.dev/web.pid")" 2>/dev/null || true; fi
  rm -rf -- "$WORK"
}
trap cleanup EXIT

fail() {
  printf 'dev-native-web-launch-test: %s\n' "$*" >&2
  [[ -f $WORK/out ]] && sed 's/^/  | /' "$WORK/out" >&2
  [[ -f $REPO/.dev/web.log ]] && sed 's/^/  log | /' "$REPO/.dev/web.log" >&2
  exit 1
}

mkdir -p "$REPO/scripts/lib" "$REPO/apps/web/node_modules" "$REPO/.dev" "$WORK/bin"
chmod 0700 "$REPO/.dev"
awk '$0 != "main \"$@\""' "$ROOT/scripts/dev-native.sh" >"$REPO/scripts/dev-native.sh"
cp "$ROOT/scripts/lib/platform.sh" "$ROOT/scripts/lib/run-owned.py" "$REPO/scripts/lib/"
cp "$ROOT/scripts/run-owned.sh" "$REPO/scripts/"

# `npm install --global --ignore-scripts pnpm` installs a launcher without a shebang (pnpm package file `pnpm`).
printf '%s\n' 'printf "%s\n" "$PWD" "$NUXT_PRINT_ORIGIN" "$*" >"$FAKE_PNPM_CALL"' 'exec sleep 30' \
  >"$WORK/bin/pnpm"
chmod 0700 "$WORK/bin/pnpm"
[[ $(head -c 2 "$WORK/bin/pnpm") != '#!' ]] || fail 'the fixture launcher must not have a shebang'

status=0
(
  cd "$REPO"
  PATH="$WORK/bin:$PATH" FAKE_PNPM_CALL=$WORK/call bash -c '
    source scripts/dev-native.sh
    port_blocked_by_stranger() { return 1; }
    wait_http() {
      local _
      for _ in $(seq 1 200); do
        [ -s "$FAKE_PNPM_CALL" ] && return 0
        is_ours web "$(read_pid web)" || return 1
        sleep 0.05
      done
      return 1
    }
    start_web
    stop_service web
  '
) >"$WORK/out" 2>&1 || status=$?
((status == 0)) || fail "start_web exited $status"
[[ -s $WORK/call ]] || fail 'the shebang-less pnpm launcher never ran'
mapfile -t call <"$WORK/call"
[[ ${call[0]} == "$REPO/apps/web" ]] || fail "pnpm ran in ${call[0]}"
[[ ${call[1]} == http://127.0.0.1:20082 ]] || fail "NUXT_PRINT_ORIGIN was ${call[1]}"
[[ ${call[2]} == 'run dev --port 20030 --host 127.0.0.1' ]] || fail "pnpm arguments were ${call[2]}"
[[ ! -e $REPO/.dev/web.pid ]] || fail 'stop_service left the web pid file'

printf 'dev-native-web-launch-test: passed\n'
