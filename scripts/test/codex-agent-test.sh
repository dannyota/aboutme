#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-codex-agent.XXXXXX")
WORK=$(cd "$WORK" && pwd -P)
trap 'rm -rf -- "$WORK"' EXIT
MAIN=$WORK/main
TREE="$WORK/tree with spaces"
mkdir -p "$WORK/bin" "$WORK/caches" "$WORK/tmp"
git init -q "$MAIN"
git -C "$MAIN" -c user.name=Test -c user.email=test@example.invalid commit -q --allow-empty -m fixture
git -C "$MAIN" worktree add -q -b fixture "$TREE"
mkdir -p "$TREE/.codex/agents"
export CALL_LOG="$WORK/calls" STDIN_LOG="$WORK/stdin" ENV_LOG="$WORK/env.json"
export CACHES="$WORK/caches" PATH="$WORK/bin:$PATH"

cat >"$WORK/bin/codex" <<'SH'
#!/usr/bin/env bash
printf '%s\0' "$@" >"$CALL_LOG"
python3 -c 'import json, os, sys; json.dump(dict(os.environ), open(sys.argv[1], "w"))' "$ENV_LOG"
cat >"$STDIN_LOG"
exit "${CODEX_STATUS:-0}"
SH
# The launcher resolves cache roots with the same scrubbed environment Codex gets.
cat >"$WORK/bin/go" <<'SH'
#!/usr/bin/env bash
[ -z "${GH_TOKEN+set}" ] || exit 65
[ "$*" = 'env GOCACHE GOMODCACHE' ] || exit 64
printf '%s\n' "${FAKE_GOCACHE:-$CACHES/go build}" "$CACHES/go mod"
SH
cat >"$WORK/bin/npm" <<'SH'
#!/usr/bin/env bash
[ -z "${GH_TOKEN+set}" ] || exit 65
[ "$*" = 'config get cache' ] || exit 64
printf '%s\n' "$CACHES/npm"
SH
chmod +x "$WORK/bin/codex" "$WORK/bin/go" "$WORK/bin/npm"

cat >"$TREE/.codex/agents/implementer.toml" <<'TOML'
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
  python3 - "$WORK" "$MAIN" "$TREE" "$@" <<'PY'
import json
from pathlib import Path
import sys
import tomllib

work, main, tree = (Path(arg) for arg in sys.argv[1:4])
model, effort, search = sys.argv[4:]
role = tomllib.loads((tree / ".codex/agents/implementer.toml").read_text())
writes = role["sandbox_mode"] == "workspace-write"
caches = work / "caches"
roots = [str(caches / "go build"), str(caches / "go mod"), str(caches / "golangci-lint"),
         str(caches / "npm"), str(work / "tmp"), str(main / ".dev/locks")]
expected = (["--search"] if search == "yes" else []) + [
    "exec", "-m", model, "-c", f"model_reasoning_effort={effort}",
    "-c", None, "-c", "features.shell_snapshot=false", "-s", role["sandbox_mode"],
] + ([
    "-c", "sandbox_workspace_write.network_access=true",
    "-c", "sandbox_workspace_write.exclude_slash_tmp=true", "-c", None,
] if writes else []) + [
    "-C", str(tree), "--ephemeral", "-o", str(work / "report with spaces.md"),
    "--", (work / "brief").read_text(),
]
actual = (work / "calls").read_bytes().split(b"\0")[:-1]
actual = [arg.decode() for arg in actual]
index = expected.index("-s") - 3
assert actual[index].startswith("developer_instructions=")
encoded = actual[index].split("=", 1)[1]
assert tomllib.loads("x = " + encoded)["x"] == role["developer_instructions"]
assert not any(ord(char) < 32 or ord(char) == 127 for char in encoded)
for char, escape in (("\b", "\\b"), ("\t", "\\t"), ("\n", "\\n"), ("\f", "\\f"), ("\r", "\\r")):
    if char in role["developer_instructions"]:
        assert escape in encoded, escape
assert all(char in encoded for char in role["developer_instructions"] if ord(char) > 127)
expected[index] = actual[index]
if writes:
    index = expected.index("sandbox_workspace_write.exclude_slash_tmp=true") + 2
    key, separator, value = actual[index].partition("=")
    assert key == "sandbox_workspace_write.writable_roots" and separator, actual[index]
    assert tomllib.loads("x = " + value)["x"] == roots, value
    assert all(Path(root).is_dir() for root in roots), "a writable root was not created"
    assert (main / ".dev").stat().st_mode & 0o777 == 0o700, "a new .dev is not private"
    expected[index] = actual[index]
assert actual == expected, actual
assert (work / "stdin").read_bytes() == b"", "codex received stdin"

environment = json.loads((work / "env.json").read_text())
for name in ("GH_TOKEN", "GITHUB_TOKEN", "GH_HOST", "AWS_PROFILE", "AWS_SECRET_ACCESS_KEY",
             "VNGCLOUD_CLIENT_ID", "REGISTRY_AUTH_FILE", "NPM_TOKEN", "NODE_AUTH_TOKEN",
             "npm_config__authToken", "ANTHROPIC_API_KEY_OWNER", "OPENAI_API_KEY",
             "SSH_AUTH_SOCK", "CLAUDE_CODE_MESSAGING_TOKEN", "OP_SERVICE_ACCOUNT_TOKEN",
             "GIT_ASKPASS", "GOOGLE_APPLICATION_CREDENTIALS", "DOCKER_CONFIG", "KUBECONFIG",
             "DEPLOY_PASSWORD", "SESSION_SECRET", "GOAUTH"):
    assert name not in environment, name
for name, value in (("GOFLAGS", "-mod=readonly"), ("NODE_OPTIONS", "--no-warnings"),
                    ("LANG", "C.UTF-8"), ("CODEX_HOME", str(work / "codex home"))):
    assert environment.get(name) == value, name
assert environment["PATH"].startswith(str(work / "bin")) and environment["HOME"]
for name, value in (("GIT_CONFIG_COUNT", "1"), ("GIT_CONFIG_KEY_0", "credential.helper"),
                    ("GIT_CONFIG_VALUE_0", ""), ("GIT_TERMINAL_PROMPT", "0")):
    assert environment.get(name) == value, name
if writes:
    for name, root in zip(("GOCACHE", "GOMODCACHE", "GOLANGCI_LINT_CACHE", "npm_config_cache",
                           "TMPDIR"), roots):
        assert environment.get(name) == root, name
    assert environment.get("TMPPREFIX") == str(work / "tmp/zsh")
PY
}

run() {
  env GH_TOKEN=ghp_UNTRUSTED GITHUB_TOKEN=UNTRUSTED GH_HOST=github.com AWS_PROFILE=prod \
    AWS_SECRET_ACCESS_KEY=UNTRUSTED VNGCLOUD_CLIENT_ID=UNTRUSTED REGISTRY_AUTH_FILE=/auth.json \
    NPM_TOKEN=UNTRUSTED NODE_AUTH_TOKEN=UNTRUSTED npm_config__authToken=UNTRUSTED \
    ANTHROPIC_API_KEY_OWNER=UNTRUSTED OPENAI_API_KEY=UNTRUSTED SSH_AUTH_SOCK=/agent.sock \
    CLAUDE_CODE_MESSAGING_TOKEN=UNTRUSTED OP_SERVICE_ACCOUNT_TOKEN=UNTRUSTED \
    GIT_ASKPASS=/askpass GIT_CONFIG_COUNT=2 GOOGLE_APPLICATION_CREDENTIALS=/key.json DOCKER_CONFIG=/docker \
    KUBECONFIG=/kube DEPLOY_PASSWORD=UNTRUSTED SESSION_SECRET=UNTRUSTED GOAUTH=netrc \
    GOFLAGS=-mod=readonly NODE_OPTIONS=--no-warnings LANG=C.UTF-8 \
    CODEX_HOME="$WORK/codex home" GOLANGCI_LINT_CACHE="$CACHES/golangci-lint" TMPDIR="$WORK/tmp" \
    bash "$ROOT/scripts/codex-agent.sh" "$@" "$TREE" "$WORK/brief" "$WORK/report with spaces.md" \
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

cp "$TREE/.codex/agents/implementer.toml" "$WORK/valid-role"
python3 - "$WORK" "$TREE" <<'PY'
import json
from pathlib import Path
import sys
import tomllib

work, tree = Path(sys.argv[1]), Path(sys.argv[2])
role = tomllib.loads((work / "valid-role").read_text())
role["developer_instructions"] = 'DEL:\x7f tab:\t quote:" slash:\\ letter:ệ ' + "".join(
    chr(code) for code in range(32)
)
(tree / ".codex/agents/implementer.toml").write_text(
    "".join(f"{key} = {json.dumps(value, ensure_ascii=False)}\n" for key, value in role.items())
    .replace("\x7f", "\\u007f")
)
PY
run implementer
check_args gpt-6.1-sol medium no
cp "$WORK/valid-role" "$TREE/.codex/agents/implementer.toml"

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
    python3 - "$WORK" "$TREE" "$field" "$value" <<'PY'
import json
from pathlib import Path
import sys
import tomllib

work, tree = Path(sys.argv[1]), Path(sys.argv[2])
role = tomllib.loads((work / "valid-role").read_text())
value = sys.argv[4]
role[sys.argv[3]] = {"42": 42, "true": True}.get(value, value)
(tree / ".codex/agents/implementer.toml").write_text(
    "".join(f"{key} = {json.dumps(value, ensure_ascii=False)}\n" for key, value in role.items())
)
PY
    reject_setting "$field" implementer
  done
done
cp "$WORK/valid-role" "$TREE/.codex/agents/implementer.toml"

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

# Writable roots never reach the Git directory, and the lock root cannot be
# redirected through a link a previous worker left behind.
for gocache in "$MAIN/.git/go-build" "$MAIN" "$WORK" "$MAIN/go-build"; do
  rm -f "$CALL_LOG"
  if FAKE_GOCACHE=$gocache run implementer >"$WORK/error" 2>&1; then
    printf 'codex-agent-test: accepted a writable root overlapping the checkout: %s\n' "$gocache" >&2
    exit 1
  fi
  [[ ! -e $CALL_LOG ]] || { echo 'codex-agent-test: called codex with .git writable' >&2; exit 1; }
  grep -Eq 'GOCACHE overlaps the (Git directory|main checkout)' "$WORK/error" ||
    { cat "$WORK/error" >&2; exit 1; }
  [[ ! -e $MAIN/.git/go-build && ! -e $MAIN/go-build ]] ||
    { echo 'codex-agent-test: created a refused root' >&2; exit 1; }
done
mv "$MAIN/.dev/locks" "$WORK/locks-real"
ln -s "$MAIN/.git" "$MAIN/.dev/locks"
rm -f "$CALL_LOG"
if run implementer >"$WORK/error" 2>&1; then
  echo 'codex-agent-test: accepted a linked lock directory' >&2
  exit 1
fi
[[ ! -e $CALL_LOG ]] || { echo 'codex-agent-test: called codex with a linked lock directory' >&2; exit 1; }
grep -q 'lock directory' "$WORK/error" || { cat "$WORK/error" >&2; exit 1; }
rm "$MAIN/.dev/locks"
mv "$WORK/locks-real" "$MAIN/.dev/locks"

# A workspace-write role outside a Git checkout has no shared lock root.
mkdir -p "$WORK/plain/.codex/agents"
cp "$WORK/valid-role" "$WORK/plain/.codex/agents/implementer.toml"
rm -f "$CALL_LOG"
if bash "$ROOT/scripts/codex-agent.sh" implementer "$WORK/plain" "$WORK/brief" "$WORK/report" \
  >"$WORK/error" 2>&1 </dev/null; then
  echo 'codex-agent-test: accepted a workspace-write role outside Git' >&2
  exit 1
fi
[[ ! -e $CALL_LOG ]] || { echo 'codex-agent-test: called codex outside Git' >&2; exit 1; }

python3 - "$WORK" "$TREE" <<'PY'
from pathlib import Path
import sys

work, tree = Path(sys.argv[1]), Path(sys.argv[2])
(tree / ".codex/agents/implementer.toml").write_text(
    (work / "valid-role").read_text().replace('"workspace-write"', '"read-only"')
)
PY
run implementer
check_args gpt-6.1-sol medium no
cp "$WORK/valid-role" "$TREE/.codex/agents/implementer.toml"

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
