#!/usr/bin/env bash

# Before the first UAT baseline, migration history is development-only. Once
# the comparison base contains .uat-baseline, the marker and every migration
# already present on that base are immutable.
#
# A one-time pinned exemption (below) allows exactly one baseline reset: the
# repository's actual pre-baseline marker collapsing to the ADR 0038 release
# baseline. It is not a general mechanism for future resets.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

BASELINE=apps/server/migrations/.uat-baseline
MIGRATIONS=apps/server/migrations

# Pinned blobs for the one release-baseline reset this repository ever makes
# (ADR 0038), modeled on the hash pin in the released-schema-append-only CI
# job. Computed with `git hash-object` on the final files: the base marker
# is the pre-baseline .uat-baseline text, the head marker is the new release
# baseline text, and the head baseline blob is apps/server/migrations/00001_baseline.sql.
# A reset qualifies only when all three match exactly; any other marker
# change, at any base, is rejected as before.
OLD_MARKER_BLOB=905b8586357a82c75e3346446b9115bcbc26f7b6
NEW_MARKER_BLOB=b856e996a9b29a98e8da408df0f248e0ba6a4114
NEW_BASELINE_PATH=$MIGRATIONS/00001_baseline.sql
NEW_BASELINE_BLOB=2d55b14df3220906e9acf9872353f7adb025e3d5

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

# tree_blob prints the blob id of path in commit, or nothing if absent.
tree_blob() { # commit path
  git rev-parse --verify --quiet "$1:$2" 2>/dev/null
}

# pinned_reset_ok checks the one qualifying reset: the base marker is the
# exact pre-baseline blob, the head marker is the exact new blob, and the
# head's migrations/*.sql set is exactly the one pinned baseline file.
pinned_reset_ok() { # base_marker_blob head_marker_blob head_sql_list head_baseline_blob
  [ "$1" = "$OLD_MARKER_BLOB" ] &&
    [ "$2" = "$NEW_MARKER_BLOB" ] &&
    [ "$3" = "$NEW_BASELINE_PATH" ] &&
    [ "$4" = "$NEW_BASELINE_BLOB" ]
}

# worktree_sql_list prints the *.sql files currently on disk under
# MIGRATIONS, one per line (empty if none), for the --local head.
worktree_sql_list() {
  local f
  for f in "$MIGRATIONS"/*.sql; do
    [ -e "$f" ] && printf '%s\n' "$f"
  done
}

# index_blob prints the staged (index) blob id of path, or nothing if path
# is not staged.
index_blob() { # path
  git rev-parse --verify --quiet ":$1" 2>/dev/null
}

# index_sql_list prints the *.sql files staged under MIGRATIONS, one per
# line (empty if none). Unlike worktree_sql_list, this is what committing
# the index as it stands would actually contain.
index_sql_list() {
  git ls-files -- "$MIGRATIONS"/*.sql
}

reject_sql_changes() {
  local diff=$1 changed
  changed=$(printf '%s\n' "$diff" |
    awk -F '\t' '
      $1 ~ /^(M|D|T)/ && $2 ~ /\.sql$/ { print; next }
      $1 ~ /^R/ && ($2 ~ /\.sql$/ || $3 ~ /\.sql$/) { print }
    ')
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
    if pinned_reset_ok "$(tree_blob "$base" "$BASELINE")" "$(tree_blob "$head" "$BASELINE")" \
      "$(git ls-tree -r --name-only "$head" -- "$MIGRATIONS" | grep '\.sql$' || true)" \
      "$(tree_blob "$head" "$NEW_BASELINE_PATH")"; then
      printf '%s\n' 'pinned release-baseline reset (ADR 0038) verified'
      return 0
    fi
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
    if git rev-parse --verify --quiet "$1" >/dev/null; then
      fail "local base is not a commit: $1"
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
    # A local reset must match the pin twice: once on disk (what the
    # operator is looking at) and once in the index (what `git commit`
    # would actually record). Checking only the worktree would let a
    # correct-looking but unstaged change -- or a stale index left over
    # from an earlier, different edit -- pass here and then commit
    # something else entirely.
    if [ -f "$BASELINE" ] &&
      pinned_reset_ok "$(tree_blob "$base" "$BASELINE")" "$(git hash-object "$BASELINE")" \
        "$(worktree_sql_list)" "$(git hash-object "$NEW_BASELINE_PATH" 2>/dev/null)" &&
      pinned_reset_ok "$(tree_blob "$base" "$BASELINE")" "$(index_blob "$BASELINE")" \
        "$(index_sql_list)" "$(index_blob "$NEW_BASELINE_PATH")"; then
      printf '%s\n' 'pinned release-baseline reset (ADR 0038) verified'
      return 0
    fi
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
