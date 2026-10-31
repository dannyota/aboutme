#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

PATHS=$(mktemp "${TMPDIR:-/tmp}/aboutme-path-case-index.XXXXXX")
trap 'rm -f -- "$PATHS"' EXIT
git ls-files -z >"$PATHS"

# Include directory prefixes: Docs/x.md and docs/y.md also clash on macOS.
# Shell quoting keeps tabs and newlines inside paths out of record separators.
while IFS= read -r -d '' path; do
  prefix=$path
  while :; do
    printf '%q\t%q\n' "$prefix" "$path"
    [[ $prefix == */* ]] || break
    prefix=${prefix%/*}
  done
done <"$PATHS" | awk -F '\t' '
  {
    key = tolower($1)
    if (key in spelling && spelling[key] != $1) collision[key] = 1
    spelling[key] = $1
    member = key SUBSEP $2
    if (!(member in seen)) {
      seen[member] = 1
      paths[key] = paths[key] "  " $2 "\n"
    }
  }
  END {
    status = 0
    for (key in collision) {
      printf "check-path-case: case-insensitive path collision (%s):\n%s", key, paths[key]
      status = 1
    }
    exit status
  }
'
