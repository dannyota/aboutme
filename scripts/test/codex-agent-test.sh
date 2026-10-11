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
model = "gpt-6.1-sol"
model_reasoning_effort = "medium"
sandbox_mode = "workspace-write"
developer_instructions = '''Read the "role".
Keep literal \paths and $variables; use Unicode: Việt.
'''
TOML
printf 'A brief with "quotes", $variables and \\paths.\nSecond line.\n' >"$WORK/brief"
python3 - "$WORK/caller-stdin" <<'PY'
from pathlib import Path
import sys

Path(sys.argv[1]).write_bytes(b"stdin must not reach codex\n" * 65536)
PY

check_args() {
  python3 - "$WORK" "$@" <<'PY'
from pathlib import Path
import sys
import tomllib

work = Path(sys.argv[1])
model, effort, search = sys.argv[2:]
role = tomllib.loads((work / "tree with spaces/.codex/agents/implementer.toml").read_text())
expected = (["--search"] if search == "yes" else []) + [
    "exec", "-m", model, "-c", f"model_reasoning_effort={effort}",
    "-c", None,
    "-s", role["sandbox_mode"], "-C", str(work / "tree with spaces"),
    "--ephemeral", "-o", str(work / "report with spaces.md"),
    "--", (work / "brief").read_text(),
]
actual = (work / "calls").read_bytes().split(b"\0")[:-1]
actual = [arg.decode() for arg in actual]
index = expected.index("-s") - 1
assert actual[index].startswith("developer_instructions=")
encoded = actual[index].split("=", 1)[1]
assert tomllib.loads("x = " + encoded)["x"] == role["developer_instructions"]
assert not any(ord(char) < 32 or ord(char) == 127 for char in encoded)
for char, escape in (("\b", "\\b"), ("\t", "\\t"), ("\n", "\\n"), ("\f", "\\f"), ("\r", "\\r")):
    if char in role["developer_instructions"]:
        assert escape in encoded, escape
assert all(char in encoded for char in role["developer_instructions"] if ord(char) > 127)
expected[index] = actual[index]
assert actual == expected, actual
assert (work / "stdin").read_bytes() == b"", "codex received stdin"
PY
}

run() {
  bash "$ROOT/scripts/codex-agent.sh" "$@" \
    "$WORK/tree with spaces" "$WORK/brief" "$WORK/report with spaces.md" \
    <"$WORK/caller-stdin"
}

run implementer
check_args gpt-6.1-sol medium no
run --model gpt-6-astra --effort xhigh --search implementer
check_args gpt-6-astra xhigh yes

for brief in '- Review this' '--help'; do
  printf '%s' "$brief" >"$WORK/brief"
  run implementer
  check_args gpt-6.1-sol medium no
done

cp "$WORK/tree with spaces/.codex/agents/implementer.toml" "$WORK/valid-role"
python3 - "$WORK" <<'PY'
import json
from pathlib import Path
import sys
import tomllib

work = Path(sys.argv[1])
role = tomllib.loads((work / "valid-role").read_text())
role["developer_instructions"] = 'DEL:\x7f tab:\t quote:" slash:\\ letter:ệ ' + "".join(
    chr(code) for code in range(32)
)
(work / "tree with spaces/.codex/agents/implementer.toml").write_text(
    "".join(f"{key} = {json.dumps(value, ensure_ascii=False)}\n" for key, value in role.items())
    .replace("\x7f", "\\u007f")
)
PY
run implementer
check_args gpt-6.1-sol medium no
cp "$WORK/valid-role" "$WORK/tree with spaces/.codex/agents/implementer.toml"

reject_setting() {
  local field=$1
  shift
  rm -f "$CALL_LOG"
  if run "$@" >"$WORK/error" 2>&1; then
    echo "codex-agent-test: accepted invalid $field" >&2
    exit 1
  fi
  [[ ! -e $CALL_LOG ]] || { echo 'codex-agent-test: called codex for invalid settings' >&2; exit 1; }
  python3 - "$WORK/error" "$field" <<'PY'
from pathlib import Path
import sys

error = Path(sys.argv[1]).read_text()
assert sys.argv[2] in error, error
assert "UNTRUSTED" not in error, "diagnostic echoed the rejected value"
PY
}

for field in sandbox_mode model_reasoning_effort model; do
  for value in 'UNTRUSTED' 'UNTRUSTED"\nextra=true' '' '42' 'true' 'danger-full-access' 'minimal' 'gpt-6X1-sol'; do
    python3 - "$WORK" "$field" "$value" <<'PY'
import json
from pathlib import Path
import sys
import tomllib

work = Path(sys.argv[1])
role = tomllib.loads((work / "valid-role").read_text())
value = sys.argv[3]
role[sys.argv[2]] = {"42": 42, "true": True}.get(value, value)
(work / "tree with spaces/.codex/agents/implementer.toml").write_text(
    "".join(f"{key} = {json.dumps(value, ensure_ascii=False)}\n" for key, value in role.items())
)
PY
    reject_setting "$field" implementer
  done
done
cp "$WORK/valid-role" "$WORK/tree with spaces/.codex/agents/implementer.toml"

for value in 'UNTRUSTED' '' 'gpt-6-sol-UNTRUSTED' 'gpt-6X1-sol-UNTRUSTED'; do
  reject_setting model --model "$value" implementer
done
for value in 'UNTRUSTED' '' 'minimal' 'maximum'; do
  reject_setting model_reasoning_effort --effort "$value" implementer
done
for effort in low medium high xhigh; do
  run --effort "$effort" implementer
  check_args gpt-6.1-sol "$effort" no
done
python3 - "$WORK" <<'PY'
from pathlib import Path
import sys

work = Path(sys.argv[1])
(work / "tree with spaces/.codex/agents/implementer.toml").write_text(
    (work / "valid-role").read_text().replace('"workspace-write"', '"read-only"')
)
PY
run implementer
check_args gpt-6.1-sol medium no

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
