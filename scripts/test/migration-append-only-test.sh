#!/usr/bin/env bash

# Regression tests for local and hosted migration immutability checks.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-migration-append-only.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

new_repo() {
  local repo=$1 baseline=${2:-false}
  mkdir -p "$repo/apps/server/migrations" "$repo/scripts"
  cp "$ROOT/scripts/check-migrations-append-only.sh" "$repo/scripts/"
  git -C "$repo" init -q -b main
  git -C "$repo" config user.email test@example.invalid
  git -C "$repo" config user.name "migration gate test"
  printf '%s\n' '-- released migration' >"$repo/apps/server/migrations/00001_released.sql"
  if [ "$baseline" = true ]; then
    printf '%s\n' 'First UAT migration baseline.' >"$repo/apps/server/migrations/.uat-baseline"
  fi
  git -C "$repo" add -- apps/server/migrations
  git -C "$repo" commit -qm "test: release migration"
  git -C "$repo" update-ref refs/remotes/origin/main HEAD
}

run_local() {
  local repo=$1
  (cd "$repo" && bash scripts/check-migrations-append-only.sh --local origin/main)
}

run_commits() {
  local repo=$1 base=$2 head=$3
  (cd "$repo" && bash scripts/check-migrations-append-only.sh --commits "$base" "$head")
}

assert_hosted_sql_rejected() {
  local mode=$1 repo="$WORK/hosted-sql-$1" base
  new_repo "$repo" true
  base=$(git -C "$repo" rev-parse HEAD)
  case "$mode" in
  modified)
    printf '%s\n' '-- tampered migration' >"$repo/apps/server/migrations/00001_released.sql"
    ;;
  deleted)
    rm "$repo/apps/server/migrations/00001_released.sql"
    ;;
  renamed)
    mv "$repo/apps/server/migrations/00001_released.sql" \
      "$repo/apps/server/migrations/00001_renamed.sql"
    ;;
  renamed-out)
    mv "$repo/apps/server/migrations/00001_released.sql" \
      "$repo/apps/server/migrations/00001_released.retired"
    ;;
  type-changed)
    rm "$repo/apps/server/migrations/00001_released.sql"
    ln -s missing.sql "$repo/apps/server/migrations/00001_released.sql"
    ;;
  *) return 2 ;;
  esac
  git -C "$repo" add -A -- apps/server/migrations
  git -C "$repo" commit -qm "test: $mode baselined migration"
  if run_commits "$repo" "$base" HEAD >/dev/null 2>&1; then
    printf 'migration-append-only-test: hosted %s migration passed\n' "$mode" >&2
    return 1
  fi
}

assert_rejected() {
  local label=$1 mode=$2 repo="$WORK/$1" output="$WORK/$1.out"
  new_repo "$repo" true
  printf '%s\n' '-- tampered migration' >"$repo/apps/server/migrations/00001_released.sql"
  case "$mode" in
  staged)
    git -C "$repo" add -- apps/server/migrations/00001_released.sql
    ;;
  hidden-index)
    git -C "$repo" add -- apps/server/migrations/00001_released.sql
    git -C "$repo" show HEAD:apps/server/migrations/00001_released.sql \
      >"$repo/apps/server/migrations/00001_released.sql"
    ;;
  unstaged) ;;
  *) return 2 ;;
  esac
  if run_local "$repo" >"$output" 2>&1; then
    printf 'migration-append-only-test: %s tamper passed\n' "$label" >&2
    return 1
  fi
  grep -Fq 'apps/server/migrations/00001_released.sql' "$output" || {
    printf 'migration-append-only-test: %s did not report the migration\n' "$label" >&2
    return 1
  }
}

assert_rejected unstaged unstaged
assert_rejected staged staged
assert_rejected hidden-index hidden-index

assert_local_sql_rename_out_rejected() {
  local label=$1 mode=$2 repo="$WORK/$1" output="$WORK/$1.out"
  new_repo "$repo" true
  mv "$repo/apps/server/migrations/00001_released.sql" \
    "$repo/apps/server/migrations/00001_released.retired"
  case "$mode" in
  staged)
    git -C "$repo" add -A -- apps/server/migrations
    ;;
  hidden-index)
    git -C "$repo" add -A -- apps/server/migrations
    git -C "$repo" show HEAD:apps/server/migrations/00001_released.sql \
      >"$repo/apps/server/migrations/00001_released.sql"
    ;;
  unstaged) ;;
  *) return 2 ;;
  esac
  if run_local "$repo" >"$output" 2>&1; then
    printf 'migration-append-only-test: %s rename out of SQL passed\n' \
      "$label" >&2
    return 1
  fi
  grep -Fq 'apps/server/migrations/00001_released.sql' "$output" || {
    printf 'migration-append-only-test: %s did not report the source migration\n' \
      "$label" >&2
    return 1
  }
}

assert_local_sql_rename_out_rejected rename-out-unstaged unstaged
assert_local_sql_rename_out_rejected rename-out-staged staged
assert_local_sql_rename_out_rejected rename-out-hidden-index hidden-index

repo=$WORK/baseline-deleted
new_repo "$repo" true
rm "$repo/apps/server/migrations/.uat-baseline"
if run_local "$repo" >/dev/null 2>&1; then
  echo "migration-append-only-test: deleting the UAT baseline passed" >&2
  exit 1
fi

repo=$WORK/baseline-modified
new_repo "$repo" true
printf '%s\n' 'weakened marker' >"$repo/apps/server/migrations/.uat-baseline"
if run_local "$repo" >/dev/null 2>&1; then
  echo "migration-append-only-test: modifying the UAT baseline passed" >&2
  exit 1
fi

repo=$WORK/baseline-deleted-staged
new_repo "$repo" true
git -C "$repo" rm -q -- apps/server/migrations/.uat-baseline
if run_local "$repo" >/dev/null 2>&1; then
  echo "migration-append-only-test: staged UAT baseline deletion passed" >&2
  exit 1
fi

repo=$WORK/baseline-deleted-hidden-index
new_repo "$repo" true
git -C "$repo" rm -q -- apps/server/migrations/.uat-baseline
git -C "$repo" show HEAD:apps/server/migrations/.uat-baseline \
  >"$repo/apps/server/migrations/.uat-baseline"
if run_local "$repo" >/dev/null 2>&1; then
  echo "migration-append-only-test: hidden-index UAT baseline deletion passed" >&2
  exit 1
fi

repo=$WORK/baseline-modified-hidden-index
new_repo "$repo" true
printf '%s\n' 'weakened marker' >"$repo/apps/server/migrations/.uat-baseline"
git -C "$repo" add -- apps/server/migrations/.uat-baseline
git -C "$repo" show HEAD:apps/server/migrations/.uat-baseline \
  >"$repo/apps/server/migrations/.uat-baseline"
if run_local "$repo" >/dev/null 2>&1; then
  echo "migration-append-only-test: hidden-index UAT baseline modification passed" >&2
  exit 1
fi

repo=$WORK/pre-uat-edit
new_repo "$repo"
printf '%s\n' '-- corrected before first UAT' >"$repo/apps/server/migrations/00001_released.sql"
run_local "$repo" >/dev/null

repo=$WORK/addition
new_repo "$repo" true
printf '%s\n' '-- new migration' >"$repo/apps/server/migrations/00002_new.sql"
run_local "$repo" >/dev/null

repo=$WORK/hosted-invalid-base
new_repo "$repo" true
if run_commits "$repo" not-a-commit HEAD >/dev/null 2>&1; then
  echo "migration-append-only-test: invalid hosted base passed as pre-UAT" >&2
  exit 1
fi
if run_commits "$repo" HEAD not-a-commit >/dev/null 2>&1; then
  echo "migration-append-only-test: invalid hosted head passed" >&2
  exit 1
fi

repo=$WORK/local-non-commit-base
new_repo "$repo" true
tree=$(git -C "$repo" rev-parse 'HEAD^{tree}')
git -C "$repo" update-ref refs/remotes/origin/main "$tree"
if run_local "$repo" >/dev/null 2>&1; then
  echo "migration-append-only-test: local non-commit base passed as absent" >&2
  exit 1
fi

assert_hosted_sql_rejected modified
assert_hosted_sql_rejected deleted
assert_hosted_sql_rejected renamed
assert_hosted_sql_rejected renamed-out
assert_hosted_sql_rejected type-changed

repo=$WORK/hosted-marker-tamper
new_repo "$repo" true
base=$(git -C "$repo" rev-parse HEAD)
printf '%s\n' 'weakened marker' >"$repo/apps/server/migrations/.uat-baseline"
git -C "$repo" add -- apps/server/migrations/.uat-baseline
git -C "$repo" commit -qm "test: tamper with UAT baseline"
if run_commits "$repo" "$base" HEAD >/dev/null 2>&1; then
  echo "migration-append-only-test: hosted UAT baseline tamper passed" >&2
  exit 1
fi

repo=$WORK/hosted-non-ancestor
new_repo "$repo"
root=$(git -C "$repo" rev-parse HEAD)
printf '%s\n' '-- corrected before baseline' >"$repo/apps/server/migrations/00001_released.sql"
printf '%s\n' 'First UAT migration baseline.' >"$repo/apps/server/migrations/.uat-baseline"
git -C "$repo" add -- apps/server/migrations
git -C "$repo" commit -qm "test: establish UAT baseline"
base=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" switch -q --detach "$root"
printf '%s\n' '-- rewritten migration' >"$repo/apps/server/migrations/00001_released.sql"
git -C "$repo" add -- apps/server/migrations/00001_released.sql
git -C "$repo" commit -qm "test: rewrite non-ancestor history"
head=$(git -C "$repo" rev-parse HEAD)
if run_commits "$repo" "$base" "$head" >/dev/null 2>&1; then
  echo "migration-append-only-test: non-ancestor baseline deletion passed" >&2
  exit 1
fi

repo=$WORK/hosted-addition
new_repo "$repo" true
base=$(git -C "$repo" rev-parse HEAD)
printf '%s\n' '-- new migration' >"$repo/apps/server/migrations/00002_new.sql"
git -C "$repo" add -- apps/server/migrations/00002_new.sql
git -C "$repo" commit -qm "test: add forward migration"
run_commits "$repo" "$base" HEAD >/dev/null

echo "Migration append-only tests passed"
