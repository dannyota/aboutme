#!/usr/bin/env bash

# Once the comparison base contains .v1-baseline, the marker and every
# migration already present on that base are immutable; only new migration
# files may be added. A base without the marker allows edits.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

BASELINE=.v1-baseline
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
    awk -F '\t' '
      $1 ~ /^(M|D|T)/ && $2 ~ /\.sql$/ { print; next }
      $1 ~ /^R/ && ($2 ~ /\.sql$/ || $3 ~ /\.sql$/) { print }
    ')
  if [ -n "$changed" ]; then
    printf '%s\n' 'v1-baselined migrations are immutable; only new files may be added:' >&2
    printf '%s\n' "$changed" >&2
    return 1
  fi
}

check_commits() {
  local base head diff
  base=$(resolve_commit "$1") || fail "base is not a commit: $1"
  head=$(resolve_commit "$2") || fail "head is not a commit: $2"

  if ! base_has_baseline "$base"; then
    printf '%s\n' 'no v1 compatibility baseline on the base commit; pre-v1 migration edits are allowed'
    return 0
  fi

  # Compare the two trees directly. A merge-base diff can hide a deleted
  # baseline and changed migration when a push rewrites non-ancestor history.
  if ! git diff --quiet "$base" "$head" -- "$BASELINE"; then
    fail "the v1 compatibility baseline is immutable after it lands: $BASELINE"
  fi
  if ! diff=$(git diff --name-status "$base" "$head" -- "$MIGRATIONS"); then
    fail 'could not compare migrations with the base commit'
  fi
  reject_sql_changes "$diff"
}

dispatch_base() {
  local requested=$1 head main bases
  [[ "$requested" =~ ^[0-9a-fA-F]{40}$ ]] || fail 'manual base must be a full commit SHA'
  requested=$(resolve_commit "$requested") || fail 'manual base is not a commit'
  head=$(resolve_commit "$2") || fail 'manual candidate is not a commit'
  main=$(resolve_commit "$3") || fail 'fetched main is not a commit'
  bases=$(git merge-base --all "$main" "$head") || fail 'candidate and fetched main have no common ancestor'
  [[ "$bases" =~ ^[0-9a-f]{40}$ ]] || fail 'candidate and fetched main must have one merge base'
  [ "$requested" = "$bases" ] || fail 'manual base must equal the merge base of fetched main and candidate'
  printf '%s\n' "$bases"
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
    if git rev-parse --verify --quiet "$1" >/dev/null; then
      fail "local base is not a commit: $1"
    fi
    printf 'no %s commit to compare against; skipped\n' "$1"
    return 0
  fi
  if ! base_has_baseline "$base"; then
    printf 'no v1 compatibility baseline on %s; pre-v1 migration edits are allowed\n' "$1"
    return 0
  fi

  if ! git diff --cached --quiet "$base" -- "$BASELINE" ||
    ! git diff --quiet "$base" -- "$BASELINE"; then
    fail "the v1 compatibility baseline is immutable after it lands: $BASELINE"
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
--dispatch-base)
  [ "$#" -eq 4 ] || fail 'usage: --dispatch-base <requested-sha> <head> <fetched-main>'
  dispatch_base "$2" "$3" "$4"
  ;;
--commits)
  [ "$#" -eq 3 ] || fail 'usage: --commits <base> <head>'
  check_commits "$2" "$3"
  ;;
--local)
  [ "$#" -eq 2 ] || fail 'usage: --local <base-ref>'
  check_local "$2"
  ;;
*)
  fail 'usage: --commits <base> <head> | --local <base-ref> | --dispatch-base <requested-sha> <head> <fetched-main>'
  ;;
esac
