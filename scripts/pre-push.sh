#!/usr/bin/env bash
# Static pre-push check over the files this branch changed since it left
# origin/main (committed, staged, unstaged, and untracked): ESLint on web
# sources, gofmt and golangci-lint on Go packages, Prettier and markdownlint on
# Markdown people read, check-lengths.sh, and bash -n plus shellcheck on shell
# scripts. It never runs tests, typechecks, builds, databases, stacks, or
# browsers; GitHub CI runs those (instructions/resources.md).
#
# The run holds the shared local-check lock and executes in one user systemd
# scope capped at 2 GiB with no swap and two CPUs, under a timeout. It refuses
# to start below 8 GiB of available memory.
#
# A worktree has no node_modules, so the web and docs checks borrow the main
# checkout's installs (and apps/web/.nuxt, which the ESLint config imports).
# They refuse when the branch changed a package manifest or lockfile, or when
# an installed lint tool's version differs from the branch lockfile.
#
# Exit status: 0 every check that applies passed; 1 a check failed; 2 a check
# or the whole run could not run locally, so CI alone covers it.
set -Eeuo pipefail

readonly TIMEOUT_SECONDS=600
readonly LOCK_WAIT_SECONDS=${PRE_PUSH_LOCK_WAIT:-300}
readonly MIN_AVAILABLE_KIB=$((8 * 1024 * 1024))
readonly MEMINFO=${PRE_PUSH_MEMINFO:-/proc/meminfo}
readonly BASE_REF=origin/main

SCRIPT=$(realpath "${BASH_SOURCE[0]}")
ROOT=$(git rev-parse --show-toplevel)
COMMON=$(git rev-parse --path-format=absolute --git-common-dir)
LOCK=$COMMON/aboutme-local-check.lock
MAIN=$(git worktree list --porcelain | sed -n '1s/^worktree //p')
cd "$ROOT"

say() { printf 'pre-push: %s\n' "$*"; }

# Stage 1: take the shared lock, then re-enter the script while holding it.
# flock -o keeps the lock out of child processes, so nothing the checks start
# can hold it after the run ends.
outer() {
  local status=0
  command -v flock >/dev/null || { say "flock is not installed"; exit 2; }
  flock -o -E 75 -w "$LOCK_WAIT_SECONDS" "$LOCK" bash "$SCRIPT" --locked ||
    status=$?
  if ((status == 75)); then
    say "another local check held $LOCK for ${LOCK_WAIT_SECONDS}s; try again later"
    exit 2
  fi
  exit "$status"
}

# Stage 2: under the lock, check memory and start the capped scope.
locked() {
  local available status=0
  available=$(awk '$1 == "MemAvailable:" { print $2 }' "$MEMINFO")
  if [[ -z $available ]] || ((available < MIN_AVAILABLE_KIB)); then
    say "refused: ${available:-unknown} KiB available, need $MIN_AVAILABLE_KIB KiB (8 GiB)"
    exit 2
  fi
  command -v systemd-run >/dev/null || {
    say "refused: systemd-run is missing, so the memory cap cannot be enforced"
    exit 2
  }
  systemd-run --user --scope --quiet -p MemoryMax=2G -p MemorySwapMax=0 \
    -p CPUQuota=200% -- timeout --kill-after=10s "$TIMEOUT_SECONDS" \
    bash "$SCRIPT" --capped || status=$?
  if ((status == 124 || status == 137)); then
    say "FAIL: the check passed its ${TIMEOUT_SECONDS}s bound or was killed"
    exit 1
  fi
  exit "$status"
}

# ---------------------------------------------------------------------------
# Stage 3: the checks, inside the scope.

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

declare -a CHANGED=() TOUCHED=() CREATED=()

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

cleanup() {
  local path
  for path in "${CREATED[@]}"; do rm -rf -- "$path"; done
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
  if [[ ! -d $modules ]]; then
    modules=$MAIN/$dir/node_modules
    [[ -d $modules ]] || {
      record "$label" REFUSED "no node_modules in $ROOT/$dir or $MAIN/$dir"
      return 1
    }
  fi
  command -v node >/dev/null || {
    record "$label" REFUSED "node is not installed"
    return 1
  }
  drift=$(tool_drift "$ROOT/$dir" "$modules" "$@")
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
  # A worktree borrows the main checkout's install through a symlink and a
  # private copy of its generated .nuxt, both removed on exit. The copy keeps
  # ESLint's generated typings out of the main checkout. A killed run leaves
  # them behind; the next run reclaims them by the link target and marker.
  if [[ -L $web/node_modules &&
    $(readlink "$web/node_modules") == "$MAIN/apps/web/node_modules" ]]; then
    CREATED+=("$web/node_modules")
  elif [[ ! -e $web/node_modules ]]; then
    ln -s -- "$MODULES" "$web/node_modules"
    CREATED+=("$web/node_modules")
  fi
  [[ -f $web/.nuxt/.pre-push-copy ]] && rm -rf -- "$web/.nuxt"
  if [[ ! -e $web/.nuxt ]]; then
    [[ -f $MAIN/apps/web/.nuxt/eslint.config.mjs ]] || {
      record eslint REFUSED "no generated $MAIN/apps/web/.nuxt; CI runs ESLint"
      return 0
    }
    cp -a -- "$MAIN/apps/web/.nuxt" "$web/.nuxt"
    : >"$web/.nuxt/.pre-push-copy"
    CREATED+=("$web/.nuxt")
  fi
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
    grep -Ev '^(AGENTS\.md|CLAUDE\.md|instructions/|docs/plans/|\.claude/|\.codex/|\.worktrees/|\.superpowers/|apps/web/(\.nuxt|\.output|public/font-licenses)/)|(^|/)(node_modules|\.terraform)/' ||
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
  run prettier "${#files[@]} file(s)" "$MODULES/.bin/prettier" --check -- \
    "${files[@]}"
  run markdownlint "${#files[@]} file(s)" "$MODULES/.bin/markdownlint-cli2" \
    "${files[@]}"
}

# go_packages <module dir>: prints ./pkg for each package with a changed or
# deleted .go file that still holds Go files.
go_packages() {
  local module=$1 f dir
  for f in "${TOUCHED[@]}"; do
    [[ $f == "$module"/*.go && $f != */testdata/* ]] || continue
    dir=$(dirname "${f#"$module"/}")
    compgen -G "$module/$dir/*.go" >/dev/null && printf './%s\n' "$dir"
  done | sort -u
}

check_go() {
  local -a files packages
  mapfile -t files < <(list "${CHANGED[@]}" |
    filter '^(apps/server|packages/schema/gen/go)/.*\.go$')
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
  # CI lints apps/server only, with apps/server/.golangci.yml; the generated
  # schema module gets gofmt alone.
  mapfile -t packages < <(go_packages apps/server)
  if ((${#packages[@]} == 0)); then
    record golangci-lint skip "no changed apps/server packages"
    return
  fi
  local want have
  want=$(awk '$1 == "golangci-lint" { print $2 }' .tool-versions)
  have=$(golangci-lint version --short 2>/dev/null || true)
  if [[ ${have#v} != "$want" ]]; then
    record golangci-lint REFUSED "installed ${have:-none}, .tool-versions pins $want"
    return
  fi
  run golangci-lint "${#packages[@]} package(s)" bash -c 'cd apps/server &&
    GOMEMLIMIT=1536MiB GOFLAGS=-p=2 golangci-lint run --concurrency=2 "$@"' \
    golangci-lint "${packages[@]}"
}

check_shell() {
  local -a files
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
  # Error severity only: CI does not run shellcheck, and older scripts carry
  # warnings a one-line edit should not have to fix.
  if command -v shellcheck >/dev/null; then
    run shellcheck "${#files[@]} file(s), errors only" shellcheck -S error \
      -- "${files[@]}"
  else
    record shellcheck skip "shellcheck is not installed"
  fi
}

report() {
  local cgroup peak line
  say "summary"
  for line in "${SUMMARY[@]}"; do say "  $line"; done
  cgroup=$(cut -d: -f3 /proc/self/cgroup 2>/dev/null || true)
  if peak=$(cat "/sys/fs/cgroup$cgroup/memory.peak" 2>/dev/null); then
    say "peak memory $((peak / 1024 / 1024)) MiB, ${SECONDS}s"
  else
    say "${SECONDS}s"
  fi
}

capped() {
  trap cleanup EXIT
  collect_changes
  check_web
  check_docs
  check_go
  run lengths "whole tree" bash scripts/check-lengths.sh
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
*)
  say "usage: scripts/pre-push.sh"
  exit 2
  ;;
esac
