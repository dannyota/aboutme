#!/usr/bin/env bash

# Before the first UAT baseline, migration history is development-only. Once
# the comparison base contains .uat-baseline, the marker and every migration
# already present on that base are immutable.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

BASELINE=apps/server/migrations/.uat-baseline
MIGRATIONS=apps/server/migrations

fail() {
  printf 'migration append-only check: %s\n' "$*" >&2
  exit 1
}

resolve_commit() {
  git rev-parse --verify --quiet "$1^{commit}" 2>/dev/null
}

base_has_baseline() {
  git cat-file -e "$1:$BASELINE" 2>/dev/null
}

reject_sql_changes() {
  local diff=$1 changed
  changed=$(printf '%s\n' "$diff" |
    awk -F '\t' '$1 ~ /^(M|D|R|T)/ && $0 ~ /\.sql$/ { print }')
  if [ -n "$changed" ]; then
    printf '%s\n' 'UAT-baselined migrations are immutable; only new files may be added:' >&2
    printf '%s\n' "$changed" >&2
    return 1
  fi
}

check_commits() {
  local base head diff
  base=$(resolve_commit "$1") || fail "base is not a commit: $1"
  head=$(resolve_commit "$2") || fail "head is not a commit: $2"

  if ! base_has_baseline "$base"; then
    printf '%s\n' 'no UAT migration baseline on the base commit; pre-UAT migration edits are allowed'
    return 0
  fi

  # Compare the two trees directly. A merge-base diff can hide a deleted
  # baseline and changed migration when a push rewrites non-ancestor history.
  if ! git diff --quiet "$base" "$head" -- "$BASELINE"; then
    fail "the UAT migration baseline is immutable after it lands: $BASELINE"
  fi
  if ! diff=$(git diff --name-status "$base" "$head" -- "$MIGRATIONS"); then
    fail 'could not compare migrations with the base commit'
  fi
  reject_sql_changes "$diff"
}

check_local() {
  local base index_diff status worktree_diff
  if base=$(resolve_commit "$1"); then
    :
  else
    status=$?
    if [ "$status" -ne 1 ]; then
      fail "could not resolve $1"
    fi
    printf 'no %s commit to compare against; skipped\n' "$1"
    return 0
  fi
  if ! base_has_baseline "$base"; then
    printf 'no UAT migration baseline on %s; pre-UAT migration edits are allowed\n' "$1"
    return 0
  fi

  if ! git diff --cached --quiet "$base" -- "$BASELINE" ||
    ! git diff --quiet "$base" -- "$BASELINE"; then
    fail "the UAT migration baseline is immutable after it lands: $BASELINE"
  fi
  if ! index_diff=$(git diff --cached --name-status "$base" -- "$MIGRATIONS"); then
    fail "could not compare the migration index with $1"
  fi
  if ! worktree_diff=$(git diff --name-status "$base" -- "$MIGRATIONS"); then
    fail "could not compare the migration worktree with $1"
  fi
  reject_sql_changes "$(printf '%s\n%s\n' "$index_diff" "$worktree_diff")"
}

case "${1:-}" in
--commits)
  [ "$#" -eq 3 ] || fail 'usage: --commits <base> <head>'
  check_commits "$2" "$3"
  ;;
--local)
  [ "$#" -eq 2 ] || fail 'usage: --local <base-ref>'
  check_local "$2"
  ;;
*)
  fail 'usage: --commits <base> <head> | --local <base-ref>'
  ;;
esac
