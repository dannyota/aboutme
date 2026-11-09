#!/usr/bin/env bash
set -euo pipefail

ROOT=$(git rev-parse --show-toplevel)
cd "$ROOT"

git ls-files -z -- ':(top,glob)docs/adr/[0-9][0-9][0-9][0-9]-*.md' | {
  status=0
  while IFS= read -r -d '' file; do
    for section in 'Alternatives rejected' History; do
      if ! grep -Fxq -- "## $section" "$file"; then
        printf '%s: missing ## %s\n' "$file" "$section" >&2
        status=1
      fi
    done
  done
  exit "$status"
}
