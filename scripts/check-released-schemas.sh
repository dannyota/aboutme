#!/usr/bin/env bash

# Released document versions freeze at the application v1 baseline.
set -Eeuo pipefail
[ "$#" -eq 3 ] && [ "$1" = --commits ] || {
  echo 'usage: --commits <base> <head>' >&2
  exit 1
}

if ! base=$(git rev-parse --verify --quiet "$2^{commit}") ||
  ! head=$(git rev-parse --verify --quiet "$3^{commit}"); then
  echo "Could not resolve released-schema comparison commits." >&2
  exit 1
fi
# The trusted base decides whether the freeze applies, even if HEAD removes it.
if ! git cat-file -e "$base:.v1-baseline" 2>/dev/null; then
  exit 0
fi
if ! git diff --quiet "$base" "$head" -- .v1-baseline; then
  echo "The v1 compatibility baseline is immutable: .v1-baseline" >&2
  exit 1
fi
# Compare the event base and candidate trees directly. A merge-base
# diff can hide a released-schema reversion after rewritten history.
if ! diff=$(git diff --name-status "$base" "$head" -- 'packages/schema/resume.v*.schema.json'); then
  echo "Could not compare released schemas with base." >&2
  exit 1
fi
changed=$(printf '%s\n' "$diff" | grep -E '^(M|D|R|T)' || true)
if [ -n "$changed" ]; then
  echo "Released schemas are immutable; only a new version file may be added:"
  echo "$changed"
  exit 1
fi
