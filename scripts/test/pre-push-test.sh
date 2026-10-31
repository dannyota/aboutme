#!/usr/bin/env bash

# Tests scripts/pre-push.sh in temporary repositories with stub tools: change
# detection, per-check file selection, refusals, the lock and watchdog refusals,
# the watchdog arguments, and worktree borrowing of node_modules.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-pre-push.XXXXXX")
WORK=$(cd "$WORK" && pwd -P)
trap 'rm -rf -- "$WORK"' EXIT

BIN=$WORK/bin
CALLS=$WORK/calls
mkdir -p "$BIN"

fail() {
  printf 'pre-push-test: %s\n' "$*" >&2
  [[ -f $WORK/out ]] && sed 's/^/  | /' "$WORK/out" >&2
  exit 1
}

stub() { # stub <name> <body>
  printf '#!/usr/bin/env bash\n%s\n' "$2" >"$BIN/$1"
  chmod +x "$BIN/$1"
}

# fails <name>: true when STUB_FAIL lists <name>.
stub_lib='log() { printf "%s\n" "$*" >>"$CALL_LOG"; }
fails() { [[ " ${STUB_FAIL:-} " == *" $1 "* ]]; }'

stub gofmt "$stub_lib"'
log "gofmt $*"
fails gofmt && printf "%s\n" "${@: -1}"
exit 0'
stub golangci-lint "$stub_lib"'
if [[ $1 == version ]]; then echo "${STUB_GOLANGCI_VERSION:-2.15.0}"; exit 0; fi
log "golangci-lint $* (in ${PWD##*/}, GOWORK=${GOWORK:-unset})"
! fails golangci-lint'
stub shellcheck "$stub_lib"'
log "shellcheck $*"
# Model a warning-only diagnostic when the fixture asks for one.
if [[ -n ${STUB_SHELLCHECK_WARNING:-} && " $* " == *" -S warning "* ]]; then
  exit 1
fi
! fails shellcheck'

node_tool() { # node_tool <node_modules> <bin name> <package> <version>
  local modules=$1 name=$2 package=$3 version=$4
  mkdir -p "$modules/.bin" "$modules/$package"
  printf '{"version":"%s"}\n' "$version" >"$modules/$package/package.json"
  [[ -n $name ]] || return 0
  printf '#!/usr/bin/env bash\n%s\nlog "%s $* (in ${PWD##*/}, modules $(readlink node_modules || echo real))"\n! fails %s\n' \
    "$stub_lib" "$name" "$name" >"$modules/.bin/$name"
  chmod +x "$modules/.bin/$name"
}

web_packages=(eslint @nuxt/eslint @nuxt/eslint-config @typescript-eslint/parser
  eslint-plugin-vue vue-eslint-parser @stylistic/eslint-plugin)
root_packages=(prettier markdownlint-cli2 markdownlint)

lockfile() { # lockfile <file> <package...>: every package at 1.0.0
  local file=$1 package
  shift
  {
    printf '{"packages":{"":{}'
    for package in "$@"; do printf ',"node_modules/%s":{"version":"1.0.0"}' "$package"; done
    printf '}}\n'
  } >"$file"
}

new_repo() { # new_repo <dir>: a committed repository whose origin/main is HEAD
  local repo=$1 package
  mkdir -p "$repo/scripts" "$repo/apps/web/app" "$repo/apps/server/internal/foo" \
    "$repo/apps/server/internal/gone" "$repo/docs" \
    "$repo/deploy/observer/internal/foo" "$repo/packages/schema/gen/go/internal/bar"
  cp "$ROOT/scripts/pre-push.sh" "$ROOT/scripts/check-lengths.sh" \
    "$ROOT/scripts/check-path-case.sh" "$repo/scripts/"
  mkdir -p "$repo/scripts/lib"
  cp "$ROOT/scripts/lib/platform.sh" "$ROOT/scripts/lib/local-check.py" "$repo/scripts/lib/"
  cat >"$repo/scripts/run-capped.sh" <<'STUB'
#!/usr/bin/env bash
printf 'run-capped %s report=%s\n' "$*" "$RUN_CAPPED_REPORT" >>"$CALL_LOG"
[ -z "${STUB_CAPPED_STATUS:-}" ] || exit "$STUB_CAPPED_STATUS"
shift 3
exec "$@"
STUB

  printf 'golangci-lint 2.15.0\n' >"$repo/.tool-versions"
  printf '{}\n' >"$repo/package.json"
  printf '{}\n' >"$repo/apps/web/package.json"
  lockfile "$repo/package-lock.json" "${root_packages[@]}"
  lockfile "$repo/apps/web/package-lock.json" "${web_packages[@]}"
  printf 'export const a = 1;\n' >"$repo/apps/web/app/a.ts"
  printf 'package foo\n' >"$repo/apps/server/internal/foo/foo.go"
  printf 'package gone\n' >"$repo/apps/server/internal/gone/gone.go"
  printf 'module test/server\n\ngo 1.21\n' >"$repo/apps/server/go.mod"
  printf 'test config\n' >"$repo/apps/server/.golangci.yml"
  # go.work puts apps/server and the generated schema module in the
  # workspace; deploy/observer stays outside it, like the real repo.
  printf 'go 1.21\n\nuse (\n\t./apps/server\n\t./packages/schema/gen/go\n)\n' \
    >"$repo/go.work"
  printf 'package foo\n' >"$repo/deploy/observer/internal/foo/foo.go"
  printf 'module test/observer\n\ngo 1.21\n' >"$repo/deploy/observer/go.mod"
  printf 'test config\n' >"$repo/deploy/observer/.golangci.yml"
  # packages/schema/gen/go is in the workspace but carries no .golangci.yml,
  # so it stays gofmt-only, like the real generated schema module.
  printf 'package bar\n' >"$repo/packages/schema/gen/go/internal/bar/bar.go"
  printf 'module test/schema\n\ngo 1.21\n' >"$repo/packages/schema/gen/go/go.mod"
  printf '# Doc\n' >"$repo/docs/a.md"
  git -C "$repo" init -q -b main
  git -C "$repo" config user.email test@example.invalid
  git -C "$repo" config user.name "pre-push test"
  printf 'node_modules\n.nuxt/\n' >"$repo/.git/info/exclude"
  git -C "$repo" add -A
  git -C "$repo" commit -qm "test: base"
  git -C "$repo" update-ref refs/remotes/origin/main HEAD
  for package in "${root_packages[@]}"; do
    node_tool "$repo/node_modules" "$package" "$package" 1.0.0
  done
  for package in "${web_packages[@]}"; do
    node_tool "$repo/apps/web/node_modules" \
      "$([[ $package == eslint ]] && echo eslint)" "$package" 1.0.0
  done
  mkdir -p "$repo/apps/web/.nuxt"
  printf 'export default [];\n' >"$repo/apps/web/.nuxt/eslint.config.mjs"
}

# check <dir> <want exit> [env...]: runs the script and saves its output.
check() {
  local dir=$1 want=$2 status=0
  shift 2
  : >"$CALLS"
  (
    cd "$dir"
    env PATH="$BIN:$PATH" CALL_LOG="$CALLS" \
      PRE_PUSH_LOCK_WAIT=1 "$@" bash scripts/pre-push.sh
  ) >"$WORK/out" 2>&1 || status=$?
  ((status == want)) || fail "$dir: exit $status, want $want"
}

has_call() { grep -Fq -- "$1" "$CALLS" || fail "missing call: $1"; }
no_call() { ! grep -Fq -- "$1" "$CALLS" || fail "unexpected call: $1"; }
has_line() { grep -Eq -- "$1" "$WORK/out" || fail "missing output: $1"; }

REPO=$WORK/repo
new_repo "$REPO"

# No changes: every check skips, lengths runs, under the lock and the cap.
check "$REPO" 0

has_call "run-capped --timeout 600 -- bash $REPO/scripts/pre-push.sh --capped report=1"
has_line 'eslint +skip'
has_line 'lengths +pass'
has_line 'path-case +pass'
no_call eslint

# A watchdog refusal means nothing ran. Runtime failures are check failures.
check "$REPO" 2 STUB_CAPPED_STATUS=125
has_line 'refused: watchdog could not start'
no_call gofmt
for code in 123 124 126 127 130 137; do
  check "$REPO" 1 STUB_CAPPED_STATUS=$code
  has_line 'FAIL: watchdog stopped'
done

# A real advisory lock held by another process refuses before the watchdog.
python3 - "$REPO/.git/aboutme-local-check.lock" "$WORK/ready" <<'LOCK' &
import fcntl, sys, time
with open(sys.argv[1], "w") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    open(sys.argv[2], "w").close()
    time.sleep(30)
LOCK
holder=$!
for ((i=0; i<100; i++)); do
  [ ! -f "$WORK/ready" ] || break
  sleep 0.05
done
check "$REPO" 2
kill "$holder"
wait "$holder" || true
has_line 'another local check held'
no_call run-capped

# Committed, uncommitted, and untracked web sources all reach ESLint; other
# files under apps/web do not.
printf 'export const a = 2;\n' >"$REPO/apps/web/app/a.ts"
git -C "$REPO" commit -qam "test: web change"
printf '<template />\n' >"$REPO/apps/web/app/b.vue"
printf 'export default 1;\n' >"$REPO/apps/web/app/c.mjs"
printf 'x\n' >"$REPO/apps/web/app/notes.txt"
check "$REPO" 0
has_call "eslint --no-warn-ignored -- app/a.ts app/b.vue app/c.mjs (in web, modules real)"
has_line 'eslint +pass +3 file'

check "$REPO" 1 STUB_FAIL=eslint
has_line 'eslint +FAIL'

# A stale install is refused, not trusted.
node_tool "$REPO/apps/web/node_modules" eslint eslint 0.9.0
check "$REPO" 2
has_line 'eslint installed 0.9.0, lockfile 1.0.0'
has_line 'eslint +REFUSED +stale'
no_call "eslint --no-warn-ignored"
node_tool "$REPO/apps/web/node_modules" eslint eslint 1.0.0

# A manifest change needs a real install, so the web check is refused.
printf '{"private":true}\n' >"$REPO/apps/web/package.json"
check "$REPO" 2
has_line 'eslint +REFUSED +the branch changed apps/web/package.json'
no_call "eslint --no-warn-ignored"
git -C "$REPO" checkout -q -- apps/web/package.json
rm -f "$REPO/apps/web/app/b.vue" "$REPO/apps/web/app/c.mjs" "$REPO/apps/web/app/notes.txt"

# Markdown people read goes to Prettier and markdownlint; agent-only paths do not.
mkdir -p "$REPO/instructions"
printf '# Changed\n' >"$REPO/docs/a.md"
printf '# Agent\n' >"$REPO/instructions/x.md"
printf '# Agents\n' >"$REPO/AGENTS.md"
printf '@AGENTS.md\n' >"$REPO/CLAUDE.md"
printf '# Nested agents\n' >"$REPO/apps/web/AGENTS.md"
printf '@AGENTS.md\n' >"$REPO/apps/web/CLAUDE.md"
mkdir -p "$REPO/docs/component"
printf '# Nested agents\n' >"$REPO/docs/component/AGENTS.md"
printf '@AGENTS.md\n' >"$REPO/docs/component/CLAUDE.md"
check "$REPO" 0
has_call "prettier --check -- docs/a.md (in repo"
has_call "markdownlint-cli2 docs/a.md (in repo"
no_call "instructions/x.md"
no_call "AGENTS.md"
no_call "CLAUDE.md"
check "$REPO" 1 STUB_FAIL=markdownlint-cli2
has_line 'markdownlint +FAIL'
rm -rf "$REPO/instructions" "$REPO/AGENTS.md" "$REPO/CLAUDE.md" \
  "$REPO/apps/web/AGENTS.md" "$REPO/apps/web/CLAUDE.md" "$REPO/docs/component"
git -C "$REPO" checkout -q -- docs/a.md

# Go: gofmt sees changed files across every Go module (found from go.mod,
# not a hardcoded list); golangci-lint lints only a module with its own
# .golangci.yml, from that module's directory, with GOWORK=off outside
# go.work's use list (deploy/observer) and unset inside it (apps/server).
# packages/schema/gen/go is in the workspace but has no .golangci.yml, so it
# reaches gofmt only, like the real generated schema module.
printf 'package foo\n\nvar X = 1\n' >"$REPO/apps/server/internal/foo/foo.go"
git -C "$REPO" rm -q apps/server/internal/gone/gone.go
printf 'package foo\n\nvar Y = 1\n' >"$REPO/deploy/observer/internal/foo/foo.go"
printf 'package bar\n\nvar Z = 1\n' >"$REPO/packages/schema/gen/go/internal/bar/bar.go"
check "$REPO" 0
has_call "gofmt -l apps/server/internal/foo/foo.go deploy/observer/internal/foo/foo.go packages/schema/gen/go/internal/bar/bar.go"
has_call "golangci-lint run --concurrency=2 ./internal/foo (in server, GOWORK=unset)"
has_call "golangci-lint run --concurrency=2 ./internal/foo (in observer, GOWORK=off)"
has_line 'golangci-lint +pass +apps/server: 1 package'
has_line 'golangci-lint +pass +deploy/observer: 1 package'
no_call "./internal/gone"
no_call "golangci-lint run --concurrency=2 ./internal/bar"
check "$REPO" 1 STUB_FAIL=gofmt
has_line 'gofmt +FAIL'
check "$REPO" 1 STUB_FAIL=golangci-lint
has_line 'golangci-lint +FAIL'
check "$REPO" 2 STUB_GOLANGCI_VERSION=2.13.0
has_line 'golangci-lint +REFUSED +installed 2.13.0'
git -C "$REPO" reset -q --hard

# Shell: bash -n catches syntax errors; other scripts use error severity.
printf 'if then\n' >"$REPO/scripts/bad.sh"
check "$REPO" 1
has_line 'bash-n +FAIL'
has_call "shellcheck -S error -- scripts/bad.sh"
rm -f "$REPO/scripts/bad.sh"

# SC2043 is a warning: the same script passes outside deploy/vn and fails
# under deploy/vn, including nested paths, with CI's shellcheck flags.
printf '#!/usr/bin/env bash\nfor value in one; do echo "$value"; done\n' \
  >"$REPO/scripts/warning.sh"
check "$REPO" 0 STUB_SHELLCHECK_WARNING=1
has_call "shellcheck -S error -- scripts/warning.sh"
has_line 'shellcheck +pass +outside deploy/vn: 1 file\(s\), errors only'

mkdir -p "$REPO/deploy/vn/tests"
cp "$REPO/scripts/warning.sh" "$REPO/deploy/vn/warning.sh"
check "$REPO" 0
has_call "shellcheck -x -S warning -- deploy/vn/warning.sh"
has_call "shellcheck -S error -- scripts/warning.sh"
has_line 'shellcheck +pass +deploy/vn: 1 file\(s\), warnings and errors'
check "$REPO" 1 STUB_SHELLCHECK_WARNING=1
has_line 'bash-n +pass +2 file'
has_line 'shellcheck +FAIL +deploy/vn: 1 file\(s\), warnings and errors'
has_line 'shellcheck +pass +outside deploy/vn: 1 file\(s\), errors only'

mv "$REPO/deploy/vn/warning.sh" "$REPO/deploy/vn/tests/warning.sh"
check "$REPO" 1 STUB_SHELLCHECK_WARNING=1
has_call "shellcheck -x -S warning -- deploy/vn/tests/warning.sh"
has_call "shellcheck -S error -- scripts/warning.sh"
rm -rf "$REPO/deploy/vn"
rm -f "$REPO/scripts/warning.sh"

# check-lengths.sh runs over the whole tree.
seq 451 | sed 's/^/line /' >"$REPO/docs/long.md"
git -C "$REPO" add docs/long.md
check "$REPO" 1
has_line 'lengths +FAIL'
git -C "$REPO" rm -q -f docs/long.md

# A worktree borrows the main checkout's install and .nuxt, then removes both.
TREE=$WORK/tree
git -C "$REPO" worktree add -q -b topic "$TREE" HEAD
printf 'export const a = 3;\n' >"$TREE/apps/web/app/a.ts"
check "$TREE" 0
has_call "eslint --no-warn-ignored -- app/a.ts (in web, modules $REPO/apps/web/node_modules)"

[[ ! -e $TREE/apps/web/node_modules && ! -e $TREE/apps/web/.nuxt ]] ||
  fail "worktree kept the borrowed node_modules or .nuxt"
[[ -d $REPO/apps/web/node_modules && ! -L $REPO/apps/web/node_modules ]] ||
  fail "main checkout lost its node_modules"

# Leftovers from a killed run are reclaimed and removed.
ln -s "$REPO/apps/web/node_modules" "$TREE/apps/web/node_modules"
mkdir -p "$TREE/apps/web/.nuxt"
: >"$TREE/apps/web/.nuxt/.pre-push-copy"
check "$TREE" 0
[[ ! -e $TREE/apps/web/node_modules && ! -e $TREE/apps/web/.nuxt ]] ||
  fail "worktree kept leftovers from a killed run"

# A worktree branch that changed the web lockfile is refused.
node -e 'const fs=require("fs"); const p=process.argv[1]; fs.writeFileSync(p,fs.readFileSync(p,"utf8").replace("1.0.0", "1.1.0"));' "$TREE/apps/web/package-lock.json"
check "$TREE" 2
has_line 'eslint +REFUSED +the branch changed apps/web/package.json or its lockfile'

# Interrupt the actual lock -> pre-push -> watchdog -> check chain.
REAL=$WORK/real
new_repo "$REAL"
cp "$ROOT/scripts/run-capped.sh" "$REAL/scripts/"
cp "$ROOT/scripts/lib/run-capped.py" "$REAL/scripts/lib/"
cat >"$REAL/scripts/check-lengths.sh" <<'CHECK'
#!/usr/bin/env bash
exec python3 -c '
import os, signal, time
signal.signal(signal.SIGTERM, signal.SIG_IGN)
signal.signal(signal.SIGINT, signal.SIG_IGN)
open(os.environ["OWNED_PID"], "w").write(str(os.getpid()))
time.sleep(25)'
CHECK
python3 - "$REAL" "$BIN" "$CALLS" "$WORK" <<'INTERRUPT'
import fcntl
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
repo, binary, calls, work = sys.argv[1:]
for number in (signal.SIGTERM, signal.SIGINT):
    owned = Path(work) / "owned.pid"
    owned.unlink(missing_ok=True)
    environment = {**os.environ, "PATH": binary + os.pathsep + os.environ["PATH"],
                   "CALL_LOG": calls, "OWNED_PID": str(owned),
                   "RUN_CAPPED_TOTAL_KIB": "262144", "RUN_CAPPED_RESERVE_KIB": "0"}
    with open(Path(work) / "out", "w") as output:
        child = subprocess.Popen(["bash", "scripts/pre-push.sh"], cwd=repo,
                                 env=environment, stdout=output, stderr=output,
                                 start_new_session=True,
                                 preexec_fn=lambda: signal.signal(signal.SIGINT, signal.SIG_DFL))
        try:
            deadline = time.monotonic() + 15
            while not owned.exists() or not owned.stat().st_size:
                assert child.poll() is None, "wrapper exited before check started"
                assert time.monotonic() < deadline, "check never started"
                time.sleep(0.05)
            pid = owned.read_text()
            child.send_signal(number)
            with open(Path(repo) / ".git/aboutme-local-check.lock", "w") as lock:
                deadline = time.monotonic() + 20
                while True:
                    try:
                        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                        break
                    except BlockingIOError:
                        assert time.monotonic() < deadline, "lock never released"
                        time.sleep(0.05)
                result = subprocess.run(["ps", "-o", "rss=", "-p", pid], capture_output=True, text=True)
                assert not result.stdout.strip() or int(result.stdout) == 0, "lock released before child ended"
            assert child.wait(timeout=10) == 1, "interrupted pre-push did not report failure"
        finally:
            if child.poll() is None:
                child.kill()
                child.wait()
INTERRUPT

printf 'pre-push tests passed\n'
