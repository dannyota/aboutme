#!/usr/bin/env bash
# Real process tests, run by hosted Linux and macOS jobs.
set -Eeuo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-run-capped.XXXXXX")
trap 'rm -rf "$WORK"' EXIT
fail() { printf 'run-capped-test: %s\n' "$*" >&2; cat "$WORK/out" >&2; exit 1; }
check() {
  local want=$1 rc=0
  shift
  "$@" >"$WORK/out" 2>&1 || rc=$?
  [ "$rc" -eq "$want" ] || fail "exit $rc, want $want"
}
# Synthetic samples exercise reuse without risking a real unrelated process.
python3 - "$ROOT/scripts/lib/run-capped.py" <<'IDENTITY'
import importlib.util
import sys
from unittest.mock import call, patch
spec = importlib.util.spec_from_file_location("watchdog", sys.argv[1])
watchdog = importlib.util.module_from_spec(spec)
spec.loader.exec_module(watchdog)
old = (20, "Mon Oct 26 10:00:00 2026")
new = (20, "Mon Oct 26 10:01:00 2026")
rows = [(20, 1, 99, 500, new[1], "S"), (21, 20, 99, 500, new[1], "S")]
assert not watchdog.members(rows, 10, {old}, None)
assert watchdog.members([(20, 1, 99, 500, old[1], "S")], 10, {old}, None) == {old}
assert not watchdog.members([(10, 1, 10, 500, new[1], "S")], 10, set(), (10, old[1]))
assert not watchdog.members([(10, 1, 10, 500, new[1], "S")], 10,
                            set(), (10, old[1]), group_owned=True)
# The leader was reaped and the observed descendant forked and exited.
replacement = (22, new[1])
assert watchdog.members([(22, 1, 10, 500, new[1], "S")], 10,
                        {old}, (10, old[1]), group_owned=True) == {replacement}
for malformed in ("1 0 1 10", "1 0 1 10 invalid start time fields here S"):
    with patch.object(watchdog.subprocess, "check_output", return_value=malformed):
        try:
            watchdog.snapshot()
        except ValueError:
            pass
        else:
            raise AssertionError("malformed identity sample accepted")
with patch.object(watchdog, "snapshot", return_value=rows), patch.object(watchdog.os, "kill") as kill:
    watchdog.signal_members({old}, 15)
    kill.assert_not_called()
with patch.object(watchdog, "snapshot", side_effect=[[(20, 1, 10, 1, old[1], "S")], rows]), patch.object(watchdog.os, "kill") as kill:
    watchdog.signal_members({old, (21, old[1])}, 9)
    assert kill.call_args_list == [call(20, 9)]
IDENTITY
export RUN_CAPPED_TOTAL_KIB=262144 RUN_CAPPED_RESERVE_KIB=0 RUN_CAPPED_REPORT=1
check 17 bash "$ROOT/scripts/run-capped.sh" -- bash -c 'exit 17'
grep -q 'peak RSS' "$WORK/out" || fail 'peak missing'
check 124 bash "$ROOT/scripts/run-capped.sh" --timeout 1 -- sleep 30
# Native memory discovery and the ps format must work on each hosted OS.
check 19 env -u RUN_CAPPED_TOTAL_KIB RUN_CAPPED_RESERVE_KIB=0 bash "$ROOT/scripts/run-capped.sh" -- bash -c 'exit 19'
export RUN_CAPPED_TOTAL_KIB=73728
check 123 bash "$ROOT/scripts/run-capped.sh" -- python3 -c 'import time; x=bytearray(96*1024*1024); time.sleep(30)'
grep -q 'memory limit exceeded' "$WORK/out" || fail 'memory diagnostic missing'
export PID_FILE=$WORK/child.pid
cat >"$WORK/descendant.py" <<'CHILD'
import os
import time
if os.environ["LEAVE_GROUP"] == "1":
    os.setsid()
with open(os.environ["PID_FILE"], "w") as target:
    target.write(str(os.getpid()))
x = bytearray(96 * 1024 * 1024)
time.sleep(30)
CHILD
# bash -> python: the allocator is a grandchild of the watchdog. Exercise
# both the command group and a descendant in its own session.
for leave in 0 1; do
  check 123 env LEAVE_GROUP=$leave bash "$ROOT/scripts/run-capped.sh" -- \
    bash -c 'python3 "$1" & wait' child "$WORK/descendant.py"
  [ -s "$PID_FILE" ] || fail 'descendant never started'
  rss=$(ps -o rss= -p "$(cat "$PID_FILE")" 2>/dev/null || true)
  [ -z "${rss//[[:space:]0]/}" ] || fail 'descendant survived'
  rm "$PID_FILE"
done
# No one process is above the cap; the sum of the two grandchildren is.
check 123 bash "$ROOT/scripts/run-capped.sh" -- bash -c '
  python3 -c "import time; x=bytearray(40*1024*1024); time.sleep(30)" &
  python3 -c "import time; x=bytearray(40*1024*1024); time.sleep(30)" &
  wait'
# Handshakes and independent outer deadlines bound process lifecycle cases.
python3 "$ROOT/scripts/test/run-capped-lifecycle-test.py" "$ROOT/scripts/lib/run-capped.py"
check 125 env RUN_CAPPED_TOTAL_KIB=unknown bash "$ROOT/scripts/run-capped.sh" -- touch "$WORK/started"
[ ! -e "$WORK/started" ] || fail 'command started without total memory'
check 125 env RUN_CAPPED_TOTAL_KIB=10 RUN_CAPPED_RESERVE_KIB=10 bash "$ROOT/scripts/run-capped.sh" -- true
printf 'run-capped tests passed\n'
