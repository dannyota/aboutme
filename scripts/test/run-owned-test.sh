#!/usr/bin/env bash
# Real process tests, run by hosted Linux and macOS jobs.
set -Eeuo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-run-owned.XXXXXX")
trap 'rm -rf "$WORK"' EXIT
fail() { printf 'run-owned-test: %s\n' "$*" >&2; cat "$WORK/out" >&2; exit 1; }
check() {
  local want=$1 rc=0
  shift
  "$@" >"$WORK/out" 2>&1 || rc=$?
  [ "$rc" -eq "$want" ] || fail "exit $rc, want $want"
}
python3 -B - "$ROOT/scripts/lib/run-owned.py" <<'IDENTITY'
import importlib.util
import sys
from unittest.mock import call, patch
spec = importlib.util.spec_from_file_location("watchdog", sys.argv[1])
watchdog = importlib.util.module_from_spec(spec)
spec.loader.exec_module(watchdog)
old = (20, "Mon Oct 26 10:00:00 2026")
new = (20, "Mon Oct 26 10:01:00 2026")
rows = [(20, 1, 99, new[1], "S"), (21, 20, 99, new[1], "S")]
assert not watchdog.members(rows, 10, {old}, None)
assert watchdog.members([(20, 1, 99, old[1], "S")], 10, {old}, None) == {old}
for owned in (False, True):
    assert not watchdog.members([(10, 1, 10, new[1], "S")], 10,
                                set(), (10, old[1]), group_owned=owned)
replacement = (22, new[1])
assert watchdog.members([(22, 1, 10, new[1], "S")], 10,
                        {old}, (10, old[1]), group_owned=True) == {replacement}
for malformed in ("1 0 1", "1 0 1 invalid start time fields here S"):
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
with patch.object(watchdog, "snapshot", side_effect=[[(20, 1, 10, old[1], "S")], rows]), patch.object(watchdog.os, "kill") as kill:
    watchdog.signal_members({old, (21, old[1])}, 9)
    assert kill.call_args_list == [call(20, 9)]
with patch.object(watchdog.subprocess, "check_output", return_value="10 1 10 Mon Oct 26 10:00:00 2026 S") as observe:
    watchdog.snapshot()
    assert "rss" not in " ".join(observe.call_args.args[0]).lower()
IDENTITY
# Legacy memory settings cannot refuse, cap, report, or change priority.
export RUN_CAPPED_TOTAL_KIB=invalid RUN_CAPPED_RESERVE_KIB=999999999 RUN_CAPPED_REPORT=1
for entry in run-owned run-capped; do
  check 17 bash "$ROOT/scripts/$entry.sh" -- bash -c 'exit 17'
  [[ ! -s $WORK/out ]] || fail 'unexpected memory report'
  check 124 bash "$ROOT/scripts/$entry.sh" --timeout 1 -- sleep 30
  check 0 bash "$ROOT/scripts/$entry.sh" -- python3 -c 'x=bytearray(2*1024*1024)'
done
# Launch observes the inherited priority without adding nice.
priority=$(ps -o nice= -p "$$" | tr -d ' ')
check 0 bash "$ROOT/scripts/run-owned.sh" -- bash -c 'ps -o nice= -p "$$"' > /dev/null
[[ $(tr -d '[:space:]' <"$WORK/out") == "$priority" ]] || fail 'priority changed'
for timeout in 0 -1 nan inf bad; do
  check 125 bash "$ROOT/scripts/run-owned.sh" --timeout "$timeout" -- touch "$WORK/started"
  [[ ! -e $WORK/started ]] || fail 'invalid timeout launched a command'
done
check 127 bash "$ROOT/scripts/run-owned.sh" -- "$WORK/missing"
(cd "$WORK" && python3 -B "$ROOT/scripts/test/test_run_owned_lifecycle.py")
printf 'run-owned tests passed\n'
