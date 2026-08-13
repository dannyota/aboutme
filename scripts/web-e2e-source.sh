#!/usr/bin/env bash

# Build a deterministic Playwright source tar from reviewed blobs at HEAD.
set -Eeuo pipefail
export LC_ALL=C
umask 077

fail() {
  printf 'web-e2e-source: %s\n' "$*" >&2
  exit 1
}

index_flags_are_visible() {
  local record tag
  while IFS= read -r -d '' record; do
    tag=${record%% *}
    case $tag in
      S | [a-z]) return 1 ;;
    esac
  done < <(git ls-files -v -z)
}

[ "$#" -eq 3 ] || fail \
  'usage: scripts/web-e2e-source.sh <commit> <manifest> <new-.dev-output.tar>'

requested_commit=$1
manifest_arg=$2
output_arg=$3
if [[ ! $requested_commit =~ ^([0-9a-f]{40}|[0-9a-f]{64})$ ]]; then
  fail 'requested commit must be a full lowercase object ID'
fi
root=$(git rev-parse --show-toplevel 2>/dev/null) ||
  fail 'current directory is not a Git worktree'
root=$(realpath "$root")

resolved_commit=$(git rev-parse --verify --quiet "${requested_commit}^{commit}") ||
  fail 'requested commit is invalid'
[ "$resolved_commit" = "$requested_commit" ] ||
  fail 'requested commit must be its canonical full object ID'
head_commit=$(git rev-parse --verify --quiet 'HEAD^{commit}') ||
  fail 'HEAD is not a commit'
[ "$resolved_commit" = "$head_commit" ] ||
  fail 'requested commit must equal HEAD'

clean_before=$(git status --porcelain=v1 --untracked-files=all)
[ -z "$clean_before" ] || fail 'worktree and index must be clean'
index_flags_are_visible ||
  fail 'assume-unchanged and skip-worktree are forbidden'

manifest=$(realpath "$manifest_arg" 2>/dev/null) || fail 'manifest does not exist'
[ "$manifest" = "$root/scripts/web-e2e-source.manifest" ] ||
  fail 'manifest must be scripts/web-e2e-source.manifest in this repository'
[ -f "$manifest" ] && [ ! -L "$manifest" ] ||
  fail 'manifest must be a regular file, not a symlink'

manifest_entry=$(git ls-tree "$resolved_commit" -- scripts/web-e2e-source.manifest)
case $manifest_entry in
  '100644 blob '*$'\t''scripts/web-e2e-source.manifest' | \
    '100755 blob '*$'\t''scripts/web-e2e-source.manifest') ;;
  *) fail 'manifest must be a regular file in the requested commit' ;;
esac
git cat-file blob \
  "$resolved_commit:scripts/web-e2e-source.manifest" | cmp -s - "$manifest" ||
  fail 'manifest bytes must match the requested commit'
[ -s "$manifest" ] || fail 'manifest must not be empty'
[ "$(tail -c 1 "$manifest" | od -An -tuC | tr -d ' ')" = 10 ] ||
  fail 'manifest must end with one newline'
LC_ALL=C sort -c -u "$manifest" 2>/dev/null ||
  fail 'manifest must be sorted bytewise with no duplicates'

canonical_future_path() {
  local candidate=$1 ancestor parent
  local -a tail=()
  ancestor=$(realpath -m "$candidate")
  while [ ! -e "$ancestor" ]; do
    parent=$(dirname "$ancestor")
    [ "$parent" != "$ancestor" ] || break
    tail=("$(basename "$ancestor")" "${tail[@]}")
    ancestor=$parent
  done
  ancestor=$(realpath "$ancestor")
  for parent in "${tail[@]}"; do
    ancestor=$ancestor/$parent
  done
  printf '%s\n' "$ancestor"
}

output=$(canonical_future_path "$output_arg")
dev_root=$(canonical_future_path "$root/.dev")
case $output in
  "$dev_root"/*) ;;
  *) fail 'output must be a new path below the repository .dev directory' ;;
esac
[ ! -e "$output" ] && [ ! -L "$output" ] || fail 'output already exists'
output_parent=$(dirname "$output")
install -d -m 0700 "$output_parent"
[ "$(canonical_future_path "$output_parent")" = "$output_parent" ] ||
  fail 'output parent changed while it was being prepared'

is_secret_like() {
  local path=$1 component lower
  IFS=/ read -r -a components <<<"$path"
  for component in "${components[@]}"; do
    lower=${component,,}
    case $lower in
      .env* | .git* | .dev | .superpowers | node_modules | .nuxt | .output | \
        dist | coverage | test-results | playwright-report | credentials* | \
        secrets* | id_rsa* | id_ed25519* | *.key | *.pem)
        return 0
        ;;
    esac
  done
  return 1
}

declare -a source_paths=()
while IFS= read -r path; do
  [ -n "$path" ] || fail 'manifest cannot contain a blank line'
  [[ $path != /* ]] || fail "absolute manifest path is forbidden: $path"
  [[ $path != *['*?[]'* ]] || fail "manifest globs are forbidden: $path"
  [[ $path != *\\* ]] || fail "backslashes are forbidden in manifest paths: $path"
  [[ $path != *[$'\001'-$'\037'$'\177']* ]] ||
    fail 'manifest paths cannot contain control characters'

  IFS=/ read -r -a components <<<"$path"
  for component in "${components[@]}"; do
    [ -n "$component" ] && [ "$component" != . ] && [ "$component" != .. ] ||
      fail "manifest path is not canonical: $path"
  done
  case $path in
    package.json | package-lock.json | apps/web/* | packages/schema/*) ;;
    *) fail "manifest path is outside the closed source roots: $path" ;;
  esac
  is_secret_like "$path" && fail "secret-like manifest path is forbidden: $path"

  mapfile -d '' -t entries < <(
    git ls-tree -z "$resolved_commit" -- ":(literal)$path"
  )
  [ "${#entries[@]}" -eq 1 ] ||
    fail "manifest path is not one exact committed file: $path"
  entry=${entries[0]}
  metadata=${entry%%$'\t'*}
  listed_path=${entry#*$'\t'}
  read -r mode object_type _object_id <<<"$metadata"
  [ "$listed_path" = "$path" ] || fail "Git path mismatch for: $path"
  [ "$object_type" = blob ] || fail "manifest path is not a blob: $path"
  case $mode in
    100644 | 100755) ;;
    *) fail "manifest path is not a regular file: $path" ;;
  esac
  source_paths+=("$path")
done <"$manifest"
[ "${#source_paths[@]}" -gt 0 ] || fail 'manifest must list at least one file'

# Deterministic scheduling seam: tests may pause here, but cannot bypass any
# check or change the archived commit.
if [ -n "${WEB_E2E_SOURCE_TEST_PAUSE_FILE:-}" ]; then
  pause_file=$WEB_E2E_SOURCE_TEST_PAUSE_FILE
  : >"$pause_file.ready"
  for _ in $(seq 1 3000); do
    [ -e "$pause_file.continue" ] && break
    sleep 0.01
  done
  [ -e "$pause_file.continue" ] || fail 'test pause timed out'
fi

stage=$(mktemp -d "$output_parent/.web-e2e-stage.XXXXXX")
tar_tmp=$(mktemp "$output_parent/.web-e2e-tar.XXXXXX")
tar_files=$(mktemp "$output_parent/.web-e2e-files.XXXXXX")
cleanup() {
  rm -rf -- "$stage"
  rm -f -- "$tar_tmp"
  rm -f -- "$tar_files"
}
trap cleanup EXIT

for path in "${source_paths[@]}"; do
  parent=$stage/$(dirname "$path")
  install -d -m 0755 "$parent"
  git cat-file blob "$resolved_commit:$path" >"$stage/$path"
  entry=$(git ls-tree "$resolved_commit" -- ":(literal)$path")
  mode=${entry%% *}
  if [ "$mode" = 100755 ]; then
    chmod 0755 "$stage/$path"
  else
    chmod 0644 "$stage/$path"
  fi
done
find "$stage" -type d -exec chmod 0755 {} +
printf '%s\0' "${source_paths[@]}" >"$tar_files"

tar --create --file "$tar_tmp" --directory "$stage" \
  --format=gnu --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner \
  --mode='u+rwX,go+rX,go-w' --no-acls --no-xattrs --no-selinux \
  --no-recursion --null --files-from="$tar_files"

[ "$(git rev-parse --verify 'HEAD^{commit}')" = "$resolved_commit" ] ||
  fail 'HEAD changed during archive creation'
[ -z "$(git status --porcelain=v1 --untracked-files=all)" ] ||
  fail 'worktree or index changed during archive creation'
index_flags_are_visible ||
  fail 'assume-unchanged and skip-worktree are forbidden'
git cat-file blob \
  "$resolved_commit:scripts/web-e2e-source.manifest" | cmp -s - "$manifest" ||
  fail 'manifest changed during archive creation'

ln -- "$tar_tmp" "$output" || fail 'output appeared during archive creation'
if [ "$(git rev-parse --verify 'HEAD^{commit}')" = "$resolved_commit" ] &&
  [ -z "$(git status --porcelain=v1 --untracked-files=all)" ] &&
  index_flags_are_visible; then
  :
else
  rm -f -- "$output"
  fail 'repository changed before archive publication'
fi

printf '%s\n' "$output"
