#!/usr/bin/env bash

# Author regression checks for fail-closed hosted append-only jobs.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORKFLOW=$ROOT/.github/workflows/ci.yml

fail() {
  printf 'workflow-safety-test: %s\n' "$*" >&2
  exit 1
}

if grep -Fq 'changed=$(git diff --name-status' "$WORKFLOW"; then
  fail "a hosted append-only job still filters git diff before checking its status"
fi

count=$(grep -Fc 'if ! diff=$(git diff --name-status' "$WORKFLOW" || true)
[ "$count" -eq 2 ] ||
  fail "want two fail-closed hosted diff captures, found $count"

grep -Fq 'could not compare migrations with the base commit' "$WORKFLOW" ||
  fail "migration job lacks an explicit diff-failure result"
grep -Fq 'Could not compare released schemas with base.' "$WORKFLOW" ||
  fail "released-schema job lacks an explicit diff-failure result"

grep -Fq 'semgrep ci --no-suppress-errors' "$WORKFLOW" ||
  fail "hosted Semgrep can suppress engine errors"

printf 'hosted workflow safety tests passed\n'
