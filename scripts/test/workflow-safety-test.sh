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

grep -Fq 'bash scripts/check-migrations-append-only.sh --commits "$BASE_SHA" HEAD' "$WORKFLOW" ||
  fail "migration job does not use the tested commit-to-commit guard"
grep -Fq 'base is not a commit' "$ROOT/scripts/check-migrations-append-only.sh" ||
  fail "migration guard does not reject an invalid base"
grep -Fq 'git diff --quiet "$base" "$head"' "$ROOT/scripts/check-migrations-append-only.sh" ||
  fail "migration guard does not compare push trees directly"

count=$(grep -Fc 'if ! diff=$(git diff --name-status' "$WORKFLOW" || true)
[ "$count" -eq 1 ] ||
  fail "want one inline fail-closed hosted diff capture, found $count"
grep -Fq 'Could not compare released schemas with base.' "$WORKFLOW" ||
  fail "released-schema job lacks an explicit diff-failure result"
grep -Fq 'git diff --name-status "$base" "$head"' "$WORKFLOW" ||
  fail "released-schema job does not compare event trees directly"
grep -Fq 'git rev-parse --verify --quiet "$BASE_SHA^{commit}"' "$WORKFLOW" ||
  fail "released-schema job does not validate the base commit"
grep -Fq 'git rev-parse --verify --quiet "HEAD^{commit}"' "$WORKFLOW" ||
  fail "released-schema job does not validate the head commit"
if grep -Fq 'git diff --name-status "$base"...HEAD' "$WORKFLOW"; then
  fail "released-schema job still uses a merge-base diff"
fi

grep -Fq 'semgrep ci --code --supply-chain --secrets --no-suppress-errors' "$WORKFLOW" ||
  fail "hosted Semgrep does not explicitly select every product and fail closed"
grep -Fq -- '- run: scripts/test/semgrep-sca-inputs-test.sh' "$WORKFLOW" ||
  fail "hosted Semgrep does not verify its dependency inputs"

printf 'hosted workflow safety tests passed\n'
