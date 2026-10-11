#!/usr/bin/env bash

# Released document versions are immutable (docs/design/data.md).
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
approved_old_v1_sha256="2da37bb75297fefe32a920e3fae960100f0a99236ba4dc21ef25ae6b3f61f13f"
approved_new_v1_sha256="879858284bc3cb4092d1d671466a9c620e27abf134aecedce070b6f21e4e5866"
# Compare the event base and candidate trees directly. A merge-base
# diff can hide a released-schema reversion after rewritten history.
if ! diff=$(git diff --name-status "$base" "$head" -- 'packages/schema/resume.v*.schema.json'); then
  echo "Could not compare released schemas with base." >&2
  exit 1
fi
changed=$(printf '%s\n' "$diff" | grep -E '^(M|D|R|T)' || true)
approved_v1_change=$(printf 'M\tpackages/schema/resume.v1.schema.json')
if [ "$changed" = "$approved_v1_change" ]; then
  base_v1_sha256=$(git show "$base:packages/schema/resume.v1.schema.json" | sha256sum | cut -d ' ' -f 1)
  head_v1_sha256=$(git show "$head:packages/schema/resume.v1.schema.json" | sha256sum | cut -d ' ' -f 1)
  if [ "$base_v1_sha256" = "$approved_old_v1_sha256" ] && [ "$head_v1_sha256" = "$approved_new_v1_sha256" ]; then
    changed=""
  fi
fi
if [ -n "$changed" ]; then
  echo "Released schemas are immutable; only a new version file may be added:"
  echo "$changed"
  exit 1
fi
