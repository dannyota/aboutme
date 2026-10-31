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
  local makefile=$1 recipe=$2 line word command='' state active=0 found=0 count=0
  local words=()
  while IFS= read -r line; do
    if [[ $line == "$recipe:"* ]]; then
      active=1
      found=1
      continue
    fi
    [[ $active == 1 ]] || continue
    if [[ $line == $'\t'* ]]; then
      command+=" ${line#$'\t'}"
      if [[ $command == *\\ ]]; then
        command=${command%\\}
        continue
      fi
      # Ignore quoted text, then separate shell operators from arguments.
      command=$(sed -E "s/'[^']*'/ quoted /g; s/\"([^\"\\\\]|\\\\.)*\"/ quoted /g" <<<"$command")
      command=${command//;/ ; }
      command=${command//&/ & }
      command=${command//|/ | }
      read -r -a words <<<"$command"
      state=start
      for word in "${words[@]}"; do
        word=${word#@}
        case $word in
          ';'|'&'|'|') state=start; continue ;;
        esac
        case $state in
          start)
            case $word in
              *=*) continue ;;
              go) state=go ;;
              *) state=ignore ;;
            esac ;;
          go) if [[ $word == test ]]; then state=packages; else state=ignore; fi ;;
          packages)
            if [[ $word == ./* ]]; then
              printf '%s\n' "$word"
              count=$((count + 1))
            else
              state=ignore
            fi ;;
        esac
      done
      command=''
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

  # Match the environment key and RequireTestDatabaseURL,
  # RequireMigratedTestDatabaseURL, and NewMigratedTestDatabase, also as values.
  # internal/testutil is the test support package with non-test Go helpers.
  find "$root" -type f \( -name '*_test.go' -o -path "$root/internal/testutil/*.go" \) >"$WORK/test-files" || return 1
  while IFS= read -r file; do
    if grep -E '["`]TEST_DATABASE_URL["`]|RequireTestDatabaseURL|RequireMigratedTestDatabaseURL|NewMigratedTestDatabase' "$file" >/dev/null; then
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

for source in \
  'func TestDirect(t *testing.T) { os.Getenv("TEST_DATABASE_URL") }' \
  'func localDatabase() string { return os.Getenv("TEST_DATABASE_URL") }; func TestWrapped(t *testing.T) { localDatabase() }' \
  'var localDatabase = testutil.RequireMigratedTestDatabaseURL; func TestWrapped(t *testing.T) { localDatabase(t) }'; do
  printf '%s\n' "$source" >"$WORK/fixture/internal/covered-extra/db_test.go"
  if check_inventory "$WORK/fixture" ./internal/covered/... >"$WORK/output" 2>&1; then
    fail 'self-check accepted an uncovered environment reader, wrapper, or helper value'
    exit 1
  fi
  grep -Fq 'uncovered live-database package: ./internal/covered-extra' "$WORK/output"
done
rm "$WORK/fixture/internal/covered-extra/db_test.go"
mkdir -p "$WORK/fixture/internal/testutil"
printf 'func localDatabase() string { return os.Getenv("TEST_DATABASE_URL") }\n' >"$WORK/fixture/internal/testutil/db.go"
if check_inventory "$WORK/fixture" ./internal/covered/... >"$WORK/output" 2>&1; then
  fail 'self-check accepted an uncovered test support package'
  exit 1
fi
grep -Fq 'uncovered live-database package: ./internal/testutil' "$WORK/output"
check_inventory "$WORK/fixture" ./internal/covered/... ./internal/testutil

cat >"$WORK/Makefile" <<'EOF'
server-test-db:
	@printf '%s\n' 'skipped ./internal/printf/... here'
	@printf '%s\n' 'pretend && go test ./internal/quoted/... end'
	@other ./internal/unrelated/...
	@go test ./internal/covered/... \
	  ./internal/testutil -race -p=1 -count 1 -v -timeout 30m ./internal/flag-value ; other ./internal/after
	@go test ./internal/second && other ./internal/operator
EOF
for flag in -race -p=1 '-count 1' -v '-timeout 30m'; do
  printf '\t@go test ./internal/flags %s ./internal/flag-value\n' "$flag" >>"$WORK/Makefile"
done
recipe_patterns "$WORK/Makefile" server-test-db >"$WORK/actual"
printf '%s\n' ./internal/covered/... ./internal/testutil ./internal/second >"$WORK/expected"
for flag in -race -p=1 '-count 1' -v '-timeout 30m'; do
  printf '%s\n' ./internal/flags >>"$WORK/expected"
done
cmp "$WORK/expected" "$WORK/actual"

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
