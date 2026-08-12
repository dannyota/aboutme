#!/usr/bin/env bash

# Regression tests for staged and unstaged local migration immutability checks.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-migration-append-only.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

gate_function=$(sed -n '/^migrations_append_only() {/,/^}/p' "$ROOT/scripts/ci.sh")
[ -n "$gate_function" ] || {
  echo "migration-append-only-test: local gate function is missing" >&2
  exit 1
}

new_repo() {
  local repo=$1
  mkdir -p "$repo/apps/server/migrations"
  git -C "$repo" init -q -b main
  git -C "$repo" config user.email test@example.invalid
  git -C "$repo" config user.name "migration gate test"
  printf '%s\n' '-- released migration' >"$repo/apps/server/migrations/00001_released.sql"
  git -C "$repo" add -- apps/server/migrations/00001_released.sql
  git -C "$repo" commit -qm "test: release migration"
  git -C "$repo" update-ref refs/remotes/origin/main HEAD
  printf '%s\n\nmigrations_append_only\n' "$gate_function" >"$repo/gate.sh"
}

assert_rejected() {
  local label=$1 mode=$2 repo="$WORK/$1" output="$WORK/$1.out"
  new_repo "$repo"
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
  if (cd "$repo" && bash gate.sh) >"$output" 2>&1; then
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

repo=$WORK/addition
new_repo "$repo"
printf '%s\n' '-- new migration' >"$repo/apps/server/migrations/00002_new.sql"
(cd "$repo" && bash gate.sh) >/dev/null

echo "Migration append-only tests passed"
