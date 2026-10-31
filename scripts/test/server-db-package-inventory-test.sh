#!/usr/bin/env bash
set -euo pipefail

RECIPES=(server-test-db server-test-resumeapi server-migration-test)
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-server-db-inventory.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

fail() {
  printf 'server-db-package-inventory-test: %s\n' "$*" >&2
  return 1
}

recipe_patterns() {
  local makefile=$1 recipe=$2 line word active=0 found=0 count=0
  local words=()
  while IFS= read -r line; do
    if [[ $line == "$recipe:"* ]]; then
      active=1
      found=1
      continue
    fi
    [[ $active == 1 ]] || continue
    if [[ $line == $'\t'* ]]; then
      read -r -a words <<<"$line"
      for word in "${words[@]}"; do
        if [[ $word == ./* ]]; then
          printf '%s\n' "$word"
          count=$((count + 1))
        fi
      done
    elif [[ -n $line && $line != \#* ]]; then
      active=0
    fi
  done <"$makefile"
  [[ $found == 1 ]] || { fail "missing Makefile recipe: $recipe"; return 1; }
  [[ $count -gt 0 ]] || { fail "no package patterns in recipe: $recipe"; return 1; }
}

check_inventory() {
  local root=$1 pattern base file package covered status
  shift
  local patterns=("$@")
  [[ ${#patterns[@]} -gt 0 ]] || { fail 'empty package inventory'; return 1; }
  for pattern in "${patterns[@]}"; do
    base=${pattern%/...}
    [[ -d $root/$base ]] || { fail "pattern matches no directory: $pattern"; return 1; }
  done

  # Follow testutil.RequireTestDatabaseURL, RequireMigratedTestDatabaseURL,
  # and NewMigratedTestDatabase, including unqualified calls in testutil.
  # Package-local test helpers are included by scanning every _test.go file.
  find "$root" -type f -name '*_test.go' >"$WORK/test-files" || return 1
  while IFS= read -r file; do
    if grep -E '(RequireTestDatabaseURL|RequireMigratedTestDatabaseURL|NewMigratedTestDatabase)[[:space:]]*\(' "$file" >/dev/null; then
      package=./${file#"$root/"}
      package=${package%/*}
      covered=0
      for pattern in "${patterns[@]}"; do
        base=${pattern%/...}
        if [[ $package == "$pattern" ]] ||
          [[ $pattern == */... && ( $package == "$base" || $package == "$base/"* ) ]]; then
          covered=1
          break
        fi
      done
      [[ $covered == 1 ]] || { fail "uncovered live-database package: $package"; return 1; }
    else
      status=$?
      [[ $status == 1 ]] || { fail "cannot read test file: $file"; return 1; }
    fi
  done <"$WORK/test-files"
}

mkdir -p "$WORK/fixture/internal/covered/child" "$WORK/fixture/internal/covered-extra"
printf 'testutil.RequireTestDatabaseURL(t)\n' >"$WORK/fixture/internal/covered-extra/db_test.go"
if check_inventory "$WORK/fixture" ./internal/covered/... >"$WORK/output" 2>&1; then
  fail 'self-check accepted an uncovered package'
  exit 1
fi
grep -Fq 'uncovered live-database package: ./internal/covered-extra' "$WORK/output"
check_inventory "$WORK/fixture" ./internal/covered/... ./internal/covered-extra
mv "$WORK/fixture/internal/covered-extra/db_test.go" "$WORK/fixture/internal/covered/child/"
check_inventory "$WORK/fixture" ./internal/covered/...
if check_inventory "$WORK/fixture" ./internal/missing/... >"$WORK/output" 2>&1; then
  fail 'self-check accepted a pattern with no directory'
  exit 1
fi
grep -Fq 'pattern matches no directory: ./internal/missing/...' "$WORK/output"
: >"$WORK/Makefile"
if recipe_patterns "$WORK/Makefile" server-test-db >"$WORK/output" 2>&1; then
  fail 'self-check accepted a missing recipe'
  exit 1
fi
grep -Fq 'missing Makefile recipe: server-test-db' "$WORK/output"

: >"$WORK/patterns"
for recipe in "${RECIPES[@]}"; do
  recipe_patterns "$ROOT/Makefile" "$recipe" >>"$WORK/patterns"
done
patterns=()
while IFS= read -r pattern; do
  patterns+=("$pattern")
done <"$WORK/patterns"
check_inventory "$ROOT/apps/server" "${patterns[@]}"
printf 'server database package inventory tests passed\n'
