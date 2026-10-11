#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-codex-agent.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT
mkdir -p "$WORK/bin" "$WORK/tree with spaces/.codex/agents"
export CALL_LOG="$WORK/calls" STDIN_LOG="$WORK/stdin"
export PATH="$WORK/bin:$PATH"

cat >"$WORK/bin/codex" <<'SH'
#!/usr/bin/env bash
printf '%s\0' "$@" >"$CALL_LOG"
cat >"$STDIN_LOG"
exit "${CODEX_STATUS:-0}"
SH
chmod +x "$WORK/bin/codex"

cat >"$WORK/tree with spaces/.codex/agents/implementer.toml" <<'TOML'
model = "role-model"
model_reasoning_effort = "medium"
sandbox_mode = "workspace-write"
developer_instructions = '''Read the "role".
Keep literal \paths and $variables; use Unicode: Việt.
'''
TOML
printf 'A brief with "quotes", $variables and \\paths.\nSecond line.\n' >"$WORK/brief"

check_args() {
  python3 - "$WORK" "$@" <<'PY'
import json
from pathlib import Path
import sys
import tomllib

work = Path(sys.argv[1])
model, effort, search = sys.argv[2:]
role = tomllib.loads((work / "tree with spaces/.codex/agents/implementer.toml").read_text())
expected = (["--search"] if search == "yes" else []) + [
    "exec", "-m", model, "-c", f"model_reasoning_effort={effort}",
    "-c", "developer_instructions=" + json.dumps(role["developer_instructions"], ensure_ascii=False),
    "-s", role["sandbox_mode"], "-C", str(work / "tree with spaces"),
    "--ephemeral", "-o", str(work / "report with spaces.md"),
    (work / "brief").read_text(),
]
actual = (work / "calls").read_bytes().split(b"\0")[:-1]
assert [arg.decode() for arg in actual] == expected, actual
encoded = expected[expected.index("-s") - 1].split("=", 1)[1]
assert tomllib.loads("instructions = " + encoded)["instructions"] == role["developer_instructions"]
assert (work / "stdin").read_bytes() == b"", "codex received stdin"
PY
}

run() {
  printf 'stdin must not reach codex\n' | bash "$ROOT/scripts/codex-agent.sh" "$@" \
    "$WORK/tree with spaces" "$WORK/brief" "$WORK/report with spaces.md"
}

run implementer
check_args role-model medium no
run --model override-model --effort xhigh --search implementer
check_args override-model xhigh yes

for role in '../implementer' 'bad-role' missing; do
  rm -f "$CALL_LOG"
  if run "$role" >"$WORK/error" 2>&1; then
    printf 'codex-agent-test: accepted invalid or missing role: %s\n' "$role" >&2
    exit 1
  fi
  [[ ! -e $CALL_LOG ]] || { echo 'codex-agent-test: called codex for an invalid role' >&2; exit 1; }
done

status=0
CODEX_STATUS=23 run implementer || status=$?
[[ $status == 23 ]] || { echo "codex-agent-test: exit $status, expected 23" >&2; exit 1; }
printf 'codex-agent tests passed\n'
