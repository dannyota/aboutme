#!/usr/bin/env bash
# Static pre-push check over the files this branch changed since it left
# origin/main (committed, staged, unstaged, and untracked): ESLint on web
# sources, gofmt and golangci-lint on Go packages, Prettier and markdownlint on
# Markdown people read, check-lengths.sh, and bash -n plus shellcheck on shell
# scripts. It never runs tests, typechecks, builds, databases, stacks, or
# browsers; GitHub CI runs those (instructions/resources.md).
#
# The parent holds the shared local-check lock through owned-child cleanup and
# a 10-minute deadline. Selected lint tools and generated config must belong to
# this checkout and match its lockfile. Dependency preparation runs separately.
#
# Exit status: 0 every check that applies passed; 1 a check failed; 2 a check
# or the whole run could not run locally, so CI alone covers it.
set -Eeuo pipefail

readonly TIMEOUT_SECONDS=600
readonly LOCK_WAIT_SECONDS=${PRE_PUSH_LOCK_WAIT:-300}
readonly BASE_REF=origin/main

SCRIPT=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/pre-push.sh
# shellcheck source=scripts/lib/platform.sh
source "$(dirname "$SCRIPT")/lib/platform.sh"
require_modern_bash || exit 2
command -v python3 >/dev/null || { printf 'pre-push: python3 is required\n' >&2; exit 2; }
ROOT=$(git rev-parse --show-toplevel)
LOCK=$(local_check_lock_path) || exit 2
cd "$ROOT"

say() { printf 'pre-push: %s\n' "$*"; }

# The Python lock parent closes the descriptor in children on both systems.
outer() {
  local status=0 lock_pid= pending=
  # A trap interrupts wait, so keep waiting until the lock parent has reaped
  # the watchdog after its cleanup. Install traps before starting the parent.
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
  75) say "another local check held $LOCK or the lock could not be acquired; try again later"; exit 2 ;;
  125) say "refused: watchdog could not start"; exit 2 ;;
  *) say "FAIL: watchdog stopped the check (exit $status)"; exit 1 ;;
  esac
}

locked() {
  # The lock parent must wait on the watchdog itself, with no shell between.
  exec bash "$ROOT/scripts/run-owned.sh" \
    --timeout "$TIMEOUT_SECONDS" -- bash "$SCRIPT" --capped
}

# The static checks run inside the watchdog.

declare -a SUMMARY=()
FAILED=0
REFUSED=0

record() { # record <check> <pass|FAIL|skip|REFUSED> <detail>
  SUMMARY+=("$(printf '%-15s %-7s %s' "$1" "$2" "$3")")
  case $2 in
  FAIL) FAILED=1 ;;
  REFUSED) REFUSED=1 ;;
  esac
}

run() { # run <check> <detail> <command...>
  local name=$1 detail=$2
  shift 2
  say "running $name: $*"
  if "$@"; then record "$name" pass "$detail"; else record "$name" FAIL "$detail"; fi
}

filter() { # filter <extended regex> < list
  grep -E -- "$1" || true
}

declare -a CHANGED=() TOUCHED=()

collect_changes() {
  local base
  git rev-parse --verify --quiet "$BASE_REF^{commit}" >/dev/null || {
    say "refused: $BASE_REF does not exist; run git fetch origin"
    exit 2
  }
  base=$(git merge-base "$BASE_REF" HEAD)
  say "base $base ($BASE_REF merge-base)"
  # TOUCHED includes deletions; CHANGED holds only files that still exist.
  mapfile -t TOUCHED < <(
    {
      git diff --name-only "$base" --
      git ls-files --others --exclude-standard
    } | sort -u
  )
  local f
  for f in "${TOUCHED[@]}"; do
    [[ -f $f ]] && CHANGED+=("$f")
  done
  say "${#TOUCHED[@]} changed path(s), ${#CHANGED[@]} still present"
}

list() { printf '%s\n' "$@"; }

touched() { # touched <path>: true when the branch changed or deleted <path>
  local f
  for f in "${TOUCHED[@]}"; do [[ $f == "$1" ]] && return 0; done
  return 1
}

# Reject dependency links and active checkouts before linting or installation.
# ci-local uses the same guard before npm lifecycle hooks can write outputs.
dependency_paths() {
  python3 -B - "$ROOT" "$@" <<'GUARD'
import json
import os
from pathlib import Path
import stat
import sys

root = Path(sys.argv[1])

def refuse(message):
    print(f"refused: {message}; CI installs and checks dependencies", file=sys.stderr)
    sys.exit(2)

def ancestors(path):
    if not path.is_relative_to(root):
        refuse("dependency path leaves the checkout")
    for item in (path, *path.parents):
        if item.is_symlink():
            refuse(f"symlink at {item.relative_to(root) if item.is_relative_to(root) else item}")
        if item == root:
            break

def local_schema_link(item, modules):
    if modules != root / "apps/web/node_modules" or item != modules / "@aboutme/schema":
        return False
    target = root / "packages/schema"
    ancestors(target)
    manifest = json.loads((modules.parent / "package.json").read_text())
    lock = json.loads((modules.parent / "package-lock.json").read_text())
    # The declared source link must stay in this checkout with real ancestors.
    return (manifest.get("dependencies", {}).get("@aboutme/schema") == "file:../../packages/schema"
            and lock.get("packages", {}).get("node_modules/@aboutme/schema") == {
                "resolved": "../../packages/schema", "link": True}
            and item.resolve(strict=True) == target and target.is_dir())

try:
    for name in sys.argv[2:]:
        path = root / name
        ancestors(path)
        if not path.exists():
            continue
        if not path.is_dir():
            refuse(f"{name} must be a directory")
        for directory, dirs, files in os.walk(path, onerror=lambda error: refuse("cannot inspect dependency paths")):
            for entry in dirs + files:
                item = Path(directory) / entry
                if item.is_symlink():
                    # npm's executable links stay inside this real install.
                    if (item.parent.name == ".bin" and path.name == "node_modules"
                            and item.resolve().is_relative_to(path) and item.is_file()):
                        continue
                    if local_schema_link(item, path):
                        continue
                    refuse(f"symlink at {item.relative_to(root)}")
    states = [root / ".dev", root / ".dev/native-https/run"]
    if os.environ.get("ABOUTME_DEV_STATE_DIR"):
        states.append(root / os.environ["ABOUTME_DEV_STATE_DIR"])
    for directory in states:
        ancestors(directory)
        if not directory.exists():
            continue
        for item in directory.iterdir():
            if item.name.startswith("session"):
                refuse("development session state exists; stop the active development stack first")
            if item.suffix != ".pid":
                continue
            meta = item.lstat()
            if not stat.S_ISREG(meta.st_mode) or meta.st_size > 32:
                refuse("cannot validate development stack PID state")
            value = item.read_text().strip()
            if not value.isdecimal() or int(value) <= 1:
                refuse("cannot validate development stack PID state")
            try:
                os.kill(int(value), 0)
            except ProcessLookupError:
                continue
            refuse("active development stack in this checkout; stop it first")
except (OSError, ValueError, TypeError, AttributeError, OverflowError):
    refuse("cannot inspect dependency paths or development stack state")
GUARD
}

# tool_drift <dir with package-lock.json> <node_modules> <package...>: prints
# each package whose installed version differs from the lockfile's.
tool_drift() {
  local dir=$1 modules=$2
  shift 2
  node - "$dir/package-lock.json" "$modules" "$@" <<'EOF'
const fs = require('node:fs');
const [lockPath, modules, ...names] = process.argv.slice(2);
const lock = JSON.parse(fs.readFileSync(lockPath, 'utf8')).packages;
for (const name of names) {
  const want = lock[`node_modules/${name}`]?.version ?? 'absent';
  let have = 'absent';
  try {
    have = JSON.parse(
      fs.readFileSync(`${modules}/${name}/package.json`, 'utf8')).version;
  } catch {}
  if (have !== want) console.log(`${name} installed ${have}, lockfile ${want}`);
}
EOF
}

# node_modules_for <dir> <refuse-label> <package...>: sets MODULES to the
# node_modules for <dir> (relative to ROOT), or records a refusal and fails.
MODULES=
node_modules_for() {
  local dir=$1 label=$2 modules drift prefix=$1/
  shift 2
  [[ $dir == . ]] && prefix=
  if touched "${prefix}package.json" || touched "${prefix}package-lock.json"; then
    record "$label" REFUSED "the branch changed $dir/package.json or its lockfile; CI installs and checks it"
    return 1
  fi
  modules=$ROOT/$dir/node_modules
  if ! dependency_paths "${prefix}node_modules"; then
    record "$label" REFUSED "unsafe dependency paths or active development stack; CI covers it"
    return 1
  fi
  [[ -d $modules ]] || {
    record "$label" REFUSED "no checkout-local node_modules in $ROOT/$dir; prepare dependencies separately; CI covers it"
    return 1
  }
  command -v node >/dev/null || {
    record "$label" REFUSED "node is not installed"
    return 1
  }
  if ! drift=$(tool_drift "$ROOT/$dir" "$modules" "$@"); then
    record "$label" REFUSED "cannot inspect selected tools against $dir/package-lock.json; CI covers it"
    return 1
  fi
  if [[ -n $drift ]]; then
    say "$modules does not match $dir/package-lock.json:"
    sed 's/^/  /' <<<"$drift"
    record "$label" REFUSED "stale $modules; run npm ci in $(dirname "$modules")"
    return 1
  fi
  MODULES=$modules
}

check_web() {
  local -a files
  mapfile -t files < <(list "${CHANGED[@]}" |
    filter '^apps/web/.*\.(ts|mts|cts|vue|js|mjs|cjs)$')
  if ((${#files[@]} == 0)); then
    record eslint skip "no changed web sources"
    return
  fi
  local web=$ROOT/apps/web
  node_modules_for apps/web eslint eslint @nuxt/eslint \
    @nuxt/eslint-config @typescript-eslint/parser eslint-plugin-vue \
    vue-eslint-parser @stylistic/eslint-plugin || return 0
  if ! dependency_paths apps/web/.nuxt apps/web/.output; then
    record eslint REFUSED "unsafe generated paths or active development stack; CI covers it"
    return 0
  fi
  [[ -f $web/.nuxt/eslint.config.mjs ]] || {
    record eslint REFUSED "no generated checkout-local apps/web/.nuxt; prepare dependencies separately; CI runs ESLint"
    return 0
  }
  [[ -x $MODULES/.bin/eslint ]] || {
    record eslint REFUSED "checkout-local eslint is missing; CI runs ESLint"
    return 0
  }
  files=("${files[@]#apps/web/}")
  run eslint "${#files[@]} file(s)" bash -c 'cd apps/web &&
    NODE_OPTIONS=--max-old-space-size=1536 node_modules/.bin/eslint \
      --no-warn-ignored -- "$@"' eslint "${files[@]}"
}

# Markdown people read: the docs-lint scope in package.json, which leaves out
# the agent-only paths AGENTS.md lists. Prettier also applies .prettierignore,
# which covers apps/, so Prettier checks no apps/web file.
check_docs() {
  local -a files
  mapfile -t files < <(list "${CHANGED[@]}" | filter '\.md$' |
    grep -Ev '(^|/)(AGENTS|CLAUDE)\.md$|^(instructions/|docs/plans/|\.claude/|\.codex/|\.worktrees/|\.superpowers/|apps/web/(\.nuxt|\.output|public/font-licenses)/)|(^|/)(node_modules|\.terraform)/' ||
    true)
  if ((${#files[@]} == 0)); then
    record prettier skip "no changed Markdown people read"
    record markdownlint skip "no changed Markdown people read"
    return
  fi
  node_modules_for . prettier prettier markdownlint-cli2 markdownlint || {
    record markdownlint REFUSED "see the prettier line"
    return 0
  }
  if [[ ! -x $MODULES/.bin/prettier || ! -x $MODULES/.bin/markdownlint-cli2 ]]; then
    record prettier REFUSED "checkout-local formatting tools are missing; CI covers them"
    record markdownlint REFUSED "see the prettier line"
    return 0
  fi
  run prettier "${#files[@]} file(s)" "$MODULES/.bin/prettier" --check -- \
    "${files[@]}"
  run markdownlint "${#files[@]} file(s)" "$MODULES/.bin/markdownlint-cli2" \
    "${files[@]}"
}

# go_packages <lint dir>: prints ./pkg for each package below <lint dir> with
# a changed or deleted .go file that still holds Go files.
go_packages() {
  local root=$1 f dir
  for f in "${TOUCHED[@]}"; do
    [[ $f == "$root"/*.go && $f != */testdata/* ]] || continue
    dir=$(dirname "${f#"$root"/}")
    compgen -G "$root/$dir/*.go" >/dev/null && printf './%s\n' "$dir"
  done | sort -u
}

# lint_dirs: prints every directory (relative to ROOT) that holds its own
# .golangci.yml, tracked or newly added on this branch. Lint scopes are
# structural, so this reads the current tree rather than TOUCHED.
lint_dirs() {
  { git ls-files -- '*/.golangci.yml'
    git ls-files --others --exclude-standard -- '*/.golangci.yml'; } |
    sed -e 's|/\.golangci\.yml$||' | sort -u
}

check_go() {
  local -a dirs files
  mapfile -t dirs < <(lint_dirs)
  mapfile -t files < <(list "${CHANGED[@]}" | filter '\.go$')
  if ((${#files[@]} == 0)); then
    record gofmt skip "no changed Go files"
    record golangci-lint skip "no changed Go files"
    return
  fi
  local unformatted
  if unformatted=$(gofmt -l "${files[@]}") && [[ -z $unformatted ]]; then
    record gofmt pass "${#files[@]} file(s)"
  else
    say "gofmt would change:"
    sed 's/^/  /' <<<"$unformatted"
    record gofmt FAIL "${#files[@]} file(s); run gofmt -w on the files above"
  fi
  # Every directory with its own .golangci.yml (apps/server, deploy/observer)
  # is linted with that config, from that directory, over its changed
  # packages. Go code outside those directories (packages/schema/gen/go,
  # deploy/caddy/production/build) gets gofmt alone, as before.
  local dir packages_probe
  local -a linted=()
  for dir in "${dirs[@]}"; do
    packages_probe=$(go_packages "$dir")
    [[ -n $packages_probe ]] && linted+=("$dir")
  done
  if ((${#linted[@]} == 0)); then
    record golangci-lint skip "no changed packages in a linted directory"
    return
  fi
  local want have
  want=$(awk '$1 == "golangci-lint" { print $2 }' .tool-versions)
  have=$(golangci-lint version --short 2>/dev/null || true)
  if [[ ${have#v} != "$want" ]]; then
    record golangci-lint REFUSED "installed ${have:-none}, .tool-versions pins $want"
    return
  fi
  local -a packages
  for dir in "${linted[@]}"; do
    mapfile -t packages < <(go_packages "$dir")
    run golangci-lint "$dir: ${#packages[@]} package(s)" bash -c "cd '$dir' &&
      GOMEMLIMIT=1536MiB GOFLAGS=-p=2 golangci-lint run --concurrency=2 \"\$@\"" \
      golangci-lint "${packages[@]}"
  done
}

check_shell() {
  local -a files
  local -a vn_files=() other_files=()
  mapfile -t files < <(list "${CHANGED[@]}" | filter '\.sh$')
  if ((${#files[@]} == 0)); then
    record bash-n skip "no changed shell scripts"
    record shellcheck skip "no changed shell scripts"
    return
  fi
  local f ok=1
  for f in "${files[@]}"; do bash -n -- "$f" || ok=0; done
  if ((ok)); then record bash-n pass "${#files[@]} file(s)"; else
    record bash-n FAIL "${#files[@]} file(s)"
  fi
  # .github/workflows/vn-infra.yml runs bash -n and shellcheck -x -S warning
  # on tracked deploy/vn scripts. Match its flags for changed deploy/vn files;
  # other changed shell scripts keep error severity only.
  for f in "${files[@]}"; do
    if [[ $f == deploy/vn/* ]]; then vn_files+=("$f"); else
      other_files+=("$f")
    fi
  done
  if command -v shellcheck >/dev/null; then
    if ((${#vn_files[@]})); then
      run shellcheck "deploy/vn: ${#vn_files[@]} file(s), warnings and errors" \
        shellcheck -x -S warning -- "${vn_files[@]}"
    fi
    if ((${#other_files[@]})); then
      run shellcheck "outside deploy/vn: ${#other_files[@]} file(s), errors only" \
        shellcheck -S error -- "${other_files[@]}"
    fi
  else
    record shellcheck skip "shellcheck is not installed"
  fi
}

report() {
  local line
  say "summary"
  for line in "${SUMMARY[@]}"; do say "  $line"; done
  say "${SECONDS}s"
}

capped() {
  collect_changes
  check_web
  check_docs
  check_go
  run lengths "whole tree" bash scripts/check-lengths.sh
  run path-case "index paths" bash scripts/check-path-case.sh
  check_shell
  report
  if ((FAILED)); then exit 1; fi
  if ((REFUSED)); then
    say "some checks could not run locally; CI is their only gate"
    exit 2
  fi
}

case ${1:-} in
"") outer ;;
--locked) locked ;;
--capped) capped ;;
--check-dependencies) shift; dependency_paths "$@" ;;
*)
  say "usage: scripts/pre-push.sh"
  exit 2
  ;;
esac
