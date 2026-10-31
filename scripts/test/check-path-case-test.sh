#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-path-case.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

fail() { printf 'check-path-case-test: %s\n' "$*" >&2; exit 1; }

git init -q "$WORK/repo"
cd "$WORK/repo"
BLOB=$(git hash-object -w --stdin </dev/null)

index_paths() {
  git read-tree --empty
  local path
  for path in "$@"; do
    git update-index --add --cacheinfo "100644,$BLOB,$path"
  done
}

assert_collision() {
  index_paths "$@"
  local status=0 path quoted
  bash "$ROOT/scripts/check-path-case.sh" >"$WORK/output" 2>&1 || status=$?
  [[ $status == 1 ]] || fail "expected collision exit 1, got $status"
  for path in "$@"; do
    printf -v quoted '%q' "$path"
    grep -Fqx -- "  $quoted" "$WORK/output" || fail "missing path: $quoted"
  done
}

index_paths a/Foo.ts a/bar.ts docs/x.md docs/y.md 'space dir/file.ts'
bash "$ROOT/scripts/check-path-case.sh" >"$WORK/output" 2>&1
[[ ! -s $WORK/output ]] || fail 'distinct paths produced output'

assert_collision a/Foo.ts a/foo.ts
assert_collision a/Foo.ts a/foo.ts a/FOO.ts
assert_collision a/Foo.ts a/foo.ts b/Bar.ts b/bar.ts
assert_collision Docs/x.md docs/y.md
assert_collision Docs/Sub/x.md docs/sub/y.md
assert_collision Foo foo/bar.ts
assert_collision 'a/Foo bar.ts' 'a/foo bar.ts'
assert_collision $'a/Foo\tbar.ts' $'a/foo\tbar.ts'
assert_collision $'a/Foo\nbar.ts' $'a/foo\nbar.ts'

git read-tree --empty
bash "$ROOT/scripts/check-path-case.sh" >"$WORK/output" 2>&1
[[ ! -s $WORK/output ]] || fail 'empty index produced output'

mkdir "$WORK/bin"
printf '#!/usr/bin/env bash\nexit 1\n' >"$WORK/bin/git"
chmod +x "$WORK/bin/git"
if PATH="$WORK/bin:$PATH" bash "$ROOT/scripts/check-path-case.sh" >"$WORK/output" 2>&1; then
  fail 'failed index read passed'
fi

printf 'check-path-case-test: passed\n'
