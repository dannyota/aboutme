#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-codex-agents.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT
TREE="$WORK/tree with spaces"
mkdir -p "$TREE/scripts"
cp "$ROOT/scripts/generate-codex-agents.sh" "$TREE/scripts/"
cp -R "$ROOT/scripts/codex-agents" "$TREE/scripts/"

fail() {
  printf 'codex-agents-test: %s\n' "$*" >&2
  exit 1
}

generate() {
  bash "$TREE/scripts/generate-codex-agents.sh" "$@"
}

reject_drift() {
  if generate --check >"$WORK/drift" 2>&1; then
    fail "check accepted $1"
  fi
  grep -Fq "$2" "$WORK/drift" || fail "check did not identify $1"
  grep -Fq 'bash scripts/generate-codex-agents.sh' "$WORK/drift" ||
    fail 'drift error did not give the render command'
}

generate
generate --check
generate --output-dir "$WORK/rendered"
diff -ru "$TREE/.codex/agents" "$WORK/rendered"

python3 - "$ROOT" "$TREE" <<'PY'
from pathlib import Path
import sys
import tomllib

root, tree = map(Path, sys.argv[1:])
fields = ("name", "description", "model", "model_reasoning_effort", "sandbox_mode")
expected = {path.name for path in (root / ".codex/agents").glob("*.toml")}
actual = {path.name for path in (tree / ".codex/agents").glob("*.toml")}
assert len(expected) == 7 and actual == expected, (expected, actual)
common = (tree / "scripts/codex-agents/common.md").read_text().strip() + "\n\n"
for name in sorted(expected):
    original = tomllib.loads((root / ".codex/agents" / name).read_text())
    generated = tomllib.loads((tree / ".codex/agents" / name).read_text())
    assert {key: original[key] for key in fields} == {
        key: generated[key] for key in fields
    }, name
    assert generated["developer_instructions"].startswith(common), name
PY

printf '\n# Hand edit\n' >>"$TREE/.codex/agents/implementer.toml"
cp "$TREE/.codex/agents/implementer.toml" "$WORK/hand-edited"
reject_drift 'a hand edit' implementer.toml
cmp -s "$WORK/hand-edited" "$TREE/.codex/agents/implementer.toml" ||
  fail 'check changed a hand-edited file'
generate
generate --check

rm "$TREE/.codex/agents/qa.toml"
reject_drift 'a missing output' qa.toml
[[ ! -e $TREE/.codex/agents/qa.toml ]] || fail 'check wrote a missing output'
generate
printf 'unexpected = true\n' >"$TREE/.codex/agents/extra.toml"
reject_drift 'an extra output' extra.toml
rm "$TREE/.codex/agents/extra.toml"
generate --check

python3 - "$TREE" <<'PY'
from pathlib import Path
import sys

sources = Path(sys.argv[1]) / "scripts/codex-agents"
text = 'Quotes: "role", triple: """. Backslashes: \\path, \\n, \\u0041. Việt.\n'
text += 'Trailing slash: \\\n' + "Controls: " + "".join(chr(n) for n in range(32)) + chr(127)
for name in ("common.md", "implementer.toml.in"):
    with (sources / name).open("a", encoding="utf-8") as source:
        source.write("\n" + text + "\n")
PY
generate
generate --check
python3 - "$TREE" <<'PY'
from pathlib import Path
import sys
import tomllib

tree = Path(sys.argv[1])
sources = tree / "scripts/codex-agents"
common = (sources / "common.md").read_text().strip()
for fragment in sorted(sources.glob("*.toml.in")):
    metadata, _, role = fragment.read_text().partition("\n\n")
    path = tree / ".codex/agents" / fragment.name.removesuffix(".in")
    rendered = path.read_text()
    parsed = tomllib.loads(rendered)
    assert parsed["developer_instructions"] == common + "\n\n" + role.strip() + "\n\n"
    assert rendered.startswith(metadata + '\ndeveloper_instructions = """\n')
    assert '\\"' in rendered and "\\\\" in rendered, path
PY

bash "$ROOT/scripts/generate-codex-agents.sh" --check
printf 'codex-agents tests passed\n'
