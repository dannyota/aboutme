#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-codex-agent.XXXXXX")
WORK=$(cd "$WORK" && pwd -P)
trap 'rm -rf -- "$WORK"' EXIT
# The worktree, brief, and report share a session scratchpad, like the
# primary's; the main checkout and $HOME sit outside it.
MAIN=$WORK/main
SESSION=$WORK/session/scratchpad
TREE="$SESSION/wt/tree with spaces"
SCRATCH=$SESSION/sdd
FAKE_HOME=$WORK/home
mkdir -p "$WORK/bin" "$SCRATCH" "$SESSION/sandbox-probe" "$FAKE_HOME/.ssh" "$FAKE_HOME/.config" \
  "$FAKE_HOME/.docker" "$FAKE_HOME/gopath" "$FAKE_HOME/other-caches" "$WORK/zdot"
git init -q "$MAIN"
git -C "$MAIN" -c user.name=Test -c user.email=test@example.invalid commit -q --allow-empty -m fixture
git -C "$MAIN" worktree add -q -b fixture "$TREE"
mkdir -p "$TREE/.codex/agents"
export CALL_LOG="$WORK/calls" STDIN_LOG="$WORK/stdin" ENV_LOG="$WORK/env.json" TOOL_LOG="$WORK/tools"
export PATH="$WORK/bin:$PATH"

cat >"$WORK/bin/codex" <<'SH'
#!/usr/bin/env bash
printf '%s\0' "$@" >"$CALL_LOG"
python3 -c 'import json, os, sys; json.dump(dict(os.environ), open(sys.argv[1], "w"))' "$ENV_LOG"
cat >"$STDIN_LOG"
exit "${CODEX_STATUS:-0}"
SH
# Checkout configuration never chooses a writable root, so go and npm never run.
for tool in go npm; do
  printf '#!/usr/bin/env bash\nprintf "%%s\\n" "$0 $*" >>"$TOOL_LOG"\nprintf "%%s\\n" "%s"\n' \
    "$FAKE_HOME/.ssh" >"$WORK/bin/$tool"
done
chmod +x "$WORK/bin/codex" "$WORK/bin/go" "$WORK/bin/npm"
# Hostile configuration that once redirected a root: project and user .npmrc
# files, inherited cache variables, and shell startup files.
printf 'cache=${HOME}/.ssh\n' | tee "$TREE/.npmrc" >"$FAKE_HOME/.npmrc"
printf 'export GH_TOKEN=fixture-zshenv\n' >"$WORK/zdot/.zshenv"
printf 'export GH_TOKEN=fixture-zprofile\n' >"$WORK/zdot/.zprofile"

cat >"$TREE/.codex/agents/implementer.toml" <<'TOML'
model = "gpt-6.1-sol"
model_reasoning_effort = "medium"
sandbox_mode = "workspace-write"
developer_instructions = '''Read the "role".
Keep literal \paths and $variables; use Unicode: Việt.
'''
TOML
printf 'A brief with "quotes", $variables and \\paths.\nSecond line.\n' >"$SCRATCH/brief"
python3 - "$WORK/caller-stdin" <<'PY'
from pathlib import Path
import sys

Path(sys.argv[1]).write_bytes(b"stdin must not reach codex\n" * 65536)
PY

# remove_run_temp deletes the per-run temp directory the last launch created.
remove_run_temp() {
  python3 -c 'import json, shutil, sys; shutil.rmtree(json.load(open(sys.argv[1]))["TMPDIR"])' "$ENV_LOG"
}

check_args() {
  python3 - "$WORK" "$MAIN" "$TREE" "$@" <<'PY'
import json
import os
from pathlib import Path
import subprocess
import sys
import tomllib

work, main, tree = (Path(arg) for arg in sys.argv[1:4])
model, effort, search = sys.argv[4:]
home, scratch = work / "home", work / "session/scratchpad/sdd"
role = tomllib.loads((tree / ".codex/agents/implementer.toml").read_text())
writes = role["sandbox_mode"] == "workspace-write"
environment = json.loads((work / "env.json").read_text())

# Every launch gets a new private temp directory in the per-user system temp
# directory, never the caller's TMPDIR.
if sys.platform == "darwin":
    base = subprocess.run(["getconf", "DARWIN_USER_TEMP_DIR"], capture_output=True, text=True,
                          check=True).stdout.strip()
else:
    base = "/tmp"
temp = Path(environment["TMPDIR"])
assert temp.parent == Path(base).resolve() and temp.name.startswith("aboutme-codex."), temp
assert temp.stat().st_mode & 0o777 == 0o700, "the run temp directory is not private"
seen = work / "seen-temps"
assert str(temp) not in (seen.read_text().splitlines() if seen.exists() else []), "temp reused"
with seen.open("a") as record:
    record.write(f"{temp}\n")
assert environment["TMPPREFIX"] == str(temp / "zsh")
assert environment["ZDOTDIR"] == str(temp / "zdotdir") and not os.listdir(temp / "zdotdir")
for name in ("BASH_ENV", "ENV"):
    assert name not in environment, name

cache = home / ("Library/Caches" if sys.platform == "darwin" else ".cache")
roots = [str(cache / "go-build"), str(home / "go/pkg/mod"), str(cache / "golangci-lint"),
         str(home / ".npm"), str(temp), str(main / ".dev/locks")]
expected = (["--search"] if search == "yes" else []) + [
    "exec", "-m", model, "-c", f"model_reasoning_effort={effort}",
    "-c", None, "-c", "features.shell_snapshot=false", "-c", "allow_login_shell=false",
    "-s", role["sandbox_mode"],
] + ([
    "-c", "sandbox_workspace_write.network_access=true",
    "-c", "sandbox_workspace_write.exclude_slash_tmp=true", "-c", None,
] if writes else []) + [
    "-C", str(tree), "--ephemeral", "-o", str(scratch / "report with spaces.md"),
    "--", (scratch / "brief").read_text(),
]
actual = (work / "calls").read_bytes().split(b"\0")[:-1]
actual = [arg.decode() for arg in actual]
index = expected.index("-s") - 5
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
    pre_move = main / ".git/aboutme-local-check.lock"
    assert pre_move.is_file() and pre_move.stat().st_mode & 0o777 == 0o600, "no pre-move lock"
    expected[index] = actual[index]
assert actual == expected, actual
assert (work / "stdin").read_bytes() == b"", "codex received stdin"
assert not (work / "tools").exists(), "the launcher ran go or npm"

for name in ("GH_TOKEN", "GITHUB_TOKEN", "GH_HOST", "AWS_PROFILE", "AWS_SECRET_ACCESS_KEY",
             "VNGCLOUD_CLIENT_ID", "REGISTRY_AUTH_FILE", "NPM_TOKEN", "NODE_AUTH_TOKEN",
             "npm_config__authToken", "ANTHROPIC_API_KEY_OWNER", "OPENAI_API_KEY",
             "SSH_AUTH_SOCK", "CLAUDE_CODE_MESSAGING_TOKEN", "OP_SERVICE_ACCOUNT_TOKEN",
             "GIT_ASKPASS", "GOOGLE_APPLICATION_CREDENTIALS", "DOCKER_CONFIG", "KUBECONFIG",
             "DEPLOY_PASSWORD", "SESSION_SECRET", "GOAUTH"):
    assert name not in environment, name
for name, value in (("GOFLAGS", "-mod=readonly"), ("NODE_OPTIONS", "--no-warnings"),
                    ("LANG", "C.UTF-8"), ("CODEX_HOME", str(work / "codex home")),
                    ("HOME", str(home))):
    assert environment.get(name) == value, name
assert environment["PATH"].startswith(str(work / "bin"))
for name, value in (("GIT_CONFIG_COUNT", "1"), ("GIT_CONFIG_KEY_0", "credential.helper"),
                    ("GIT_CONFIG_VALUE_0", ""), ("GIT_TERMINAL_PROMPT", "0")):
    assert environment.get(name) == value, name
if writes:
    for name, root in zip(("GOCACHE", "GOMODCACHE", "GOLANGCI_LINT_CACHE", "npm_config_cache"),
                          roots):
        assert environment.get(name) == root, name
PY
  remove_run_temp
}

# run [launcher args...] starts the launcher with credentials, hostile cache
# and temp settings, and shell startup hooks in its environment. TMPDIR_MODE=unset
# leaves TMPDIR unset instead of pointing it at the scratchpad; HOME_DIR replaces
# the fixture $HOME.
run() {
  local -a settings=(GH_TOKEN=ghp_UNTRUSTED GITHUB_TOKEN=UNTRUSTED GH_HOST=github.com
    AWS_PROFILE=prod AWS_SECRET_ACCESS_KEY=UNTRUSTED VNGCLOUD_CLIENT_ID=UNTRUSTED
    REGISTRY_AUTH_FILE=/auth.json NPM_TOKEN=UNTRUSTED NODE_AUTH_TOKEN=UNTRUSTED
    npm_config__authToken=UNTRUSTED ANTHROPIC_API_KEY_OWNER=UNTRUSTED OPENAI_API_KEY=UNTRUSTED
    SSH_AUTH_SOCK=/agent.sock CLAUDE_CODE_MESSAGING_TOKEN=UNTRUSTED
    OP_SERVICE_ACCOUNT_TOKEN=UNTRUSTED GIT_ASKPASS=/askpass GIT_CONFIG_COUNT=2
    GOOGLE_APPLICATION_CREDENTIALS=/key.json DOCKER_CONFIG=/docker KUBECONFIG=/kube
    DEPLOY_PASSWORD=UNTRUSTED SESSION_SECRET=UNTRUSTED GOAUTH=netrc
    GOFLAGS=-mod=readonly NODE_OPTIONS=--no-warnings LANG=C.UTF-8
    "CODEX_HOME=$WORK/codex home" "HOME=${HOME_DIR:-$FAKE_HOME}"
    "GOCACHE=$FAKE_HOME/.ssh" "GOMODCACHE=$MAIN" "GOLANGCI_LINT_CACHE=$SCRATCH"
    "npm_config_cache=$FAKE_HOME/.config" "ZDOTDIR=$WORK/zdot" BASH_ENV=/hostile ENV=/hostile)
  if [[ ${TMPDIR_MODE:-} == unset ]]; then
    env -u TMPDIR "${settings[@]}" bash "$ROOT/scripts/codex-agent.sh" "$@" "$TREE" \
      "$SCRATCH/brief" "$SCRATCH/report with spaces.md" <"$WORK/caller-stdin"
  else
    env "${settings[@]}" "TMPDIR=$SCRATCH" bash "$ROOT/scripts/codex-agent.sh" "$@" "$TREE" \
      "$SCRATCH/brief" "$SCRATCH/report with spaces.md" <"$WORK/caller-stdin"
  fi
}

run implementer
check_args gpt-6.1-sol medium no
TMPDIR_MODE='unset' run implementer
check_args gpt-6.1-sol medium no
run --model gpt-6-astra --effort xhigh --search implementer
check_args gpt-6-astra xhigh yes

for brief in '- Review this' '--help'; do
  printf '%s' "$brief" >"$SCRATCH/brief"
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

# refuse_root <link> <target> <pattern>: a writable root linked to <target> is
# refused before anything is created there.
refuse_root() {
  local link=$1 target=$2 pattern=$3 before after
  [[ ! -e $link ]] || mv "$link" "$WORK/link-saved"
  mkdir -p "$(dirname "$link")"
  ln -s "$target" "$link"
  rm -f "$CALL_LOG"
  before=$({ find "$target" -print 2>/dev/null || true; } | wc -l)
  if run implementer >"$WORK/error" 2>&1; then
    printf 'codex-agent-test: accepted %s linked to %s\n' "$link" "$target" >&2
    exit 1
  fi
  [[ ! -e $CALL_LOG ]] || { echo 'codex-agent-test: called codex with a refused root' >&2; exit 1; }
  grep -Eq "$pattern" "$WORK/error" || { cat "$WORK/error" >&2; exit 1; }
  after=$({ find "$target" -print 2>/dev/null || true; } | wc -l)
  [[ $before == "$after" ]] || { echo 'codex-agent-test: created a refused root' >&2; exit 1; }
  rm "$link"
  [[ ! -e $WORK/link-saved ]] || mv "$WORK/link-saved" "$link"
}
if [[ $(uname -s) == Darwin ]]; then cache=$FAKE_HOME/Library/Caches; else cache=$FAKE_HOME/.cache; fi
# A root that is, or sits under, a symbolic link is refused wherever the link
# points, so no list of protected paths has to be complete.
refuse_root "$FAKE_HOME/.npm" "$FAKE_HOME/.docker" 'npm_config_cache: .*/\.npm is a symbolic link'
refuse_root "$FAKE_HOME/.npm" "$SESSION/sandbox-probe" 'npm_config_cache: .*/\.npm is a symbolic link'
refuse_root "$FAKE_HOME/.npm" "$FAKE_HOME/.ssh" 'npm_config_cache: .*/\.npm is a symbolic link'
refuse_root "$FAKE_HOME/go" "$FAKE_HOME/gopath" 'GOMODCACHE: .*/go is a symbolic link'
refuse_root "$FAKE_HOME/go" "$MAIN/.git" 'GOMODCACHE: .*/go is a symbolic link'
refuse_root "$cache" "$FAKE_HOME/other-caches" 'GOCACHE: .*/[.]?[Cc]aches? is a symbolic link'
refuse_root "$cache/golangci-lint" "$SESSION/wt" 'GOLANGCI_LINT_CACHE: .* is a symbolic link'
refuse_root "$MAIN/.dev/locks" "$MAIN/.git" 'lock directory'

# refuse_home <home> <pattern>: a $HOME that puts the fixed roots in Git
# metadata, the main checkout, or the session scratchpad is refused, and
# nothing is created there.
refuse_home() {
  rm -f "$CALL_LOG"
  if HOME_DIR=$1 run implementer >"$WORK/error" 2>&1; then
    printf 'codex-agent-test: accepted HOME=%s\n' "$1" >&2
    exit 1
  fi
  [[ ! -e $CALL_LOG ]] || { echo 'codex-agent-test: called codex with a refused home' >&2; exit 1; }
  grep -Eq "$2" "$WORK/error" || { cat "$WORK/error" >&2; exit 1; }
  [[ ! -e $1 ]] || { echo 'codex-agent-test: created a refused home' >&2; exit 1; }
}
refuse_home "$MAIN/.git/home" 'GOCACHE overlaps the Git directory'
refuse_home "$MAIN/home" "GOCACHE overlaps a protected path: $MAIN\$"
refuse_home "$SESSION/home" "GOCACHE overlaps a protected path: $SESSION\$"

# A workspace-write role outside a Git checkout has no shared lock root.
mkdir -p "$WORK/plain/.codex/agents"
cp "$WORK/valid-role" "$WORK/plain/.codex/agents/implementer.toml"
rm -f "$CALL_LOG"
if HOME=$FAKE_HOME bash "$ROOT/scripts/codex-agent.sh" implementer "$WORK/plain" \
  "$SCRATCH/brief" "$SCRATCH/report" >"$WORK/error" 2>&1 </dev/null; then
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
remove_run_temp
[[ $status == 23 ]] || { echo "codex-agent-test: exit $status, expected 23" >&2; exit 1; }
printf 'codex-agent tests passed\n'
