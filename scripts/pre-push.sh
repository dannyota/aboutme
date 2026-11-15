#!/usr/bin/env bash
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

outer() {
  local status=0 lock_pid= pending=
  # A trap interrupts wait, so the loop waits again until the lock parent reaps the watchdog (bash manual, wait).
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
  exec bash "$ROOT/scripts/run-owned.sh" \
    --timeout "$TIMEOUT_SECONDS" -- bash "$SCRIPT" --capped
}

declare -a SUMMARY=()
FAILED=0
REFUSED=0

record() {
  SUMMARY+=("$(printf '%-15s %-7s %s' "$1" "$2" "$3")")
  case $2 in
  FAIL) FAILED=1 ;;
  REFUSED) REFUSED=1 ;;
  esac
}

run() {
  local name=$1 detail=$2
  shift 2
  say "running $name: $*"
  if "$@"; then record "$name" pass "$detail"; else record "$name" FAIL "$detail"; fi
}

filter() {
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

touched() {
  local f
  for f in "${TOUCHED[@]}"; do [[ $f == "$1" ]] && return 0; done
  return 1
}

# Reject borrowed dependencies and active checkouts before linting or installation.
# ci-local uses the same guard before pnpm lifecycle hooks can write outputs.
dependency_paths() {
  python3 -B - "$ROOT" "$@" <<'GUARD'
import json
import os
from pathlib import Path
import stat
import sys

root = Path(sys.argv[1])
real_root = Path(os.path.realpath(root))

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

def members(project):
    lines = (project / "pnpm-workspace.yaml").read_text().splitlines()
    found, listing = [], False
    for line in lines:
        if line == "packages:":
            listing = True
        elif listing and line.startswith("  - "):
            member = line[4:].strip().strip("'\"")
            if not member or any(char in member for char in "*?[]!{}") or ".." in Path(member).parts:
                refuse(f"unsupported workspace pattern in {project.relative_to(root)}/pnpm-workspace.yaml")
            found.append(member)
        elif listing and line and not line.startswith(" "):
            listing = False
    return found

def project_for(modules):
    owner = modules.parent
    if (owner / "pnpm-workspace.yaml").is_file():
        return owner
    for project in owner.parents:
        if (project / "pnpm-workspace.yaml").is_file():
            if any(project / member == owner for member in members(project)):
                return project
            break
        if project == root:
            break
    refuse(f"{modules.relative_to(root)} is not in a pnpm project")

def package_name(directory):
    return json.loads((directory / "package.json").read_text()).get("name")

def workspace_link(item, project, modules, name, target):
    for member in members(project):
        directory = project / member
        ancestors(directory)
        if target != real_root / directory.relative_to(root) or not directory.is_dir():
            continue
        if package_name(directory) != name:
            return False
        if modules == project / "node_modules/.pnpm/node_modules":
            return True
        owner = modules.parent
        if owner != project and owner not in (project / m for m in members(project)):
            return False
        manifest = json.loads((owner / "package.json").read_text())
        for field in ("dependencies", "devDependencies", "optionalDependencies"):
            spec = manifest.get(field, {}).get(name)
            if isinstance(spec, str) and spec.startswith("workspace:"):
                return True
        return False
    return False

def allowed_link(item, project):
    lexical = Path(os.path.normpath(item.parent / os.readlink(item)))
    if not lexical.is_relative_to(root) or lexical == root:
        return False
    for part in (lexical, *lexical.parents):
        if part == root:
            break
        if part.is_symlink():
            return False
    target = Path(os.path.realpath(item))
    if not target.is_relative_to(real_root):
        return False
    store = real_root / project.relative_to(root) / "node_modules/.pnpm"
    if target.is_dir() and target.is_relative_to(store) and target != store:
        return True
    enclosing = item.parent.parent if item.parent.name.startswith("@") else item.parent
    if item.parent.name == ".bin" and target.is_file():
        for tree in (enclosing.parent, project / "node_modules"):
            if target.is_relative_to(real_root / tree.relative_to(root)):
                return True
        return False
    if enclosing.name != "node_modules" or not target.is_dir():
        return False
    name = f"{item.parent.name}/{item.name}" if item.parent.name.startswith("@") else item.name
    return workspace_link(item, project, enclosing, name, target)

def walk_modules(modules, project):
    ancestors(modules)
    if not modules.exists():
        return
    if not modules.is_dir():
        refuse(f"{modules.relative_to(root)} must be a directory")
    for directory, dirs, files in os.walk(modules, onerror=lambda error: refuse("cannot inspect dependency paths")):
        for entry in dirs + files:
            item = Path(directory) / entry
            if item.is_symlink() and not allowed_link(item, project):
                refuse(f"symlink at {item.relative_to(root)}")

def check_project(project):
    ancestors(project)
    trees = [project / "node_modules", *(project / member / "node_modules" for member in members(project))]
    for modules in trees:
        ancestors(modules)
    for modules in trees:
        walk_modules(modules, project)

def check_generated(path):
    ancestors(path)
    if not path.exists():
        return
    if not path.is_dir():
        refuse(f"{path.relative_to(root)} must be a directory")
    for directory, dirs, files in os.walk(path, onerror=lambda error: refuse("cannot inspect dependency paths")):
        for entry in dirs + files:
            item = Path(directory) / entry
            if item.is_symlink():
                refuse(f"symlink at {item.relative_to(root)}")

try:
    checked = set()
    for name in sys.argv[2:]:
        path = root / name
        if path.name != "node_modules":
            check_generated(path)
            continue
        ancestors(path)
        project = project_for(path)
        if project not in checked:
            checked.add(project)
            check_project(project)
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

tool_drift() {
  local importer=$1 modules=$2
  shift 2
  node - "$ROOT/pnpm-lock.yaml" "$importer" "$modules" "$@" <<'EOF'
const fs = require('node:fs');
const [lockPath, importer, modules, ...names] = process.argv.slice(2);
const unquote = (value) => value.trim().replace(/^'(.*)'$/, '$1').replace(/^"(.*)"$/, '$1');
const documents = fs.readFileSync(lockPath, 'utf8').split(/^---$/m);
const locked = {};
let section = null;
let current = null;
let dependency = null;
for (const line of documents[documents.length - 1].split('\n')) {
  let match;
  if (/^\S/.test(line)) {
    section = line.replace(/:.*$/, '');
    current = null;
  } else if (section !== 'importers') {
    continue;
  } else if ((match = /^ {2}(\S.*):$/.exec(line))) {
    current = unquote(match[1]);
  } else if (current === importer && (match = /^ {6}(\S.*):$/.exec(line))) {
    dependency = unquote(match[1]);
  } else if (current === importer && dependency && (match = /^ {8}version: (.*)$/.exec(line))) {
    locked[dependency] = unquote(match[1]).replace(/\(.*$/, '');
  }
}
for (const name of names) {
  const want = locked[name] ?? 'absent';
  let have = 'absent';
  try {
    have = JSON.parse(
      fs.readFileSync(`${modules}/${name}/package.json`, 'utf8')).version;
  } catch {}
  if (have !== want) console.log(`${name} installed ${have}, lockfile ${want}`);
}
EOF
}

MODULES=
node_modules_for() {
  local dir=$1 label=$2 modules drift prefix=$1/ path
  shift 2
  [[ $dir == . ]] && prefix=
  for path in "${prefix}package.json" package.json pnpm-workspace.yaml pnpm-lock.yaml; do
    if touched "$path"; then
      record "$label" REFUSED "the branch changed $path; CI installs and checks it"
      return 1
    fi
  done
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
  if ! drift=$(tool_drift "$dir" "$modules" "$@"); then
    record "$label" REFUSED "cannot inspect selected tools against pnpm-lock.yaml; CI covers it"
    return 1
  fi
  if [[ -n $drift ]]; then
    say "$modules does not match pnpm-lock.yaml:"
    sed 's/^/  /' <<<"$drift"
    record "$label" REFUSED "stale $modules; run pnpm install --frozen-lockfile --offline at $ROOT"
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
  node_modules_for apps/web eslint eslint @nuxt/eslint || return 0
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
  node_modules_for . prettier prettier markdownlint-cli2 || {
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

go_packages() {
  local root=$1 f dir
  for f in "${TOUCHED[@]}"; do
    [[ $f == "$root"/*.go && $f != */testdata/* ]] || continue
    dir=$(dirname "${f#"$root"/}")
    compgen -G "$root/$dir/*.go" >/dev/null && printf './%s\n' "$dir"
  done | sort -u
}

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
  local dir package probe count
  local -a linted=() candidates=() selected=()
  for dir in "${dirs[@]}"; do
    count=${#selected[@]}
    mapfile -t candidates < <(go_packages "$dir")
    for package in "${candidates[@]}"; do
      if command -v go >/dev/null 2>&1 && probe=$(cd "$dir" && GOFLAGS=-p=2 go list -e -f \
        '{{if and .IgnoredGoFiles (not .InvalidGoFiles) (not .GoFiles) (not .CgoFiles) (not .TestGoFiles) (not .XTestGoFiles)}}skip{{end}}' \
        "$package") && [[ $probe == skip ]]; then
        record golangci-lint skip "$dir/${package#./}: no buildable Go files"
      else
        selected+=("$dir:$package")
      fi
    done
    ((${#selected[@]} == count)) || linted+=("$dir")
  done
  if ((${#selected[@]} == 0)); then
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
    packages=()
    for package in "${selected[@]}"; do
      [[ $package == "$dir:"* ]] && packages+=("${package#"$dir:"}")
    done
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
