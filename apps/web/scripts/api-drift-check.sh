#!/usr/bin/env bash
# api-drift-check.sh — fail if the committed TypeScript API surface no
# longer matches docs/api/openapi.yaml. Non-mutating by construction.
#
#   bash apps/web/scripts/api-drift-check.sh
#
# Why a temp directory instead of "regenerate, then git diff":
#
#   * Regenerating in place mutates a shared worktree. On a developer's
#     machine that silently repairs the drift the gate exists to report,
#     and in a run with other uncommitted work it hides whose change it
#     was. This script never writes inside the repository.
#   * A `git diff` gate is blind to a NEW generated file that was never
#     added (git does not diff what it does not track). Comparing whole
#     DIRECTORIES with `diff -r` catches that from both sides: a file only
#     in the fresh output is a missing commit, and a file only in the
#     committed output is a stale artifact the generator no longer emits.
#     That is also why the committed artifact lives in a directory of its
#     own — app/api/generated/ holds generated files and nothing else.
#
# Generation itself goes through openapi-gen.sh, the same script
# `npm run api:gen` uses, so the gate can never check a different
# invocation than the one that produces the artifact.
set -Eeuo pipefail

web_root="$(cd "$(dirname "$0")/.." && pwd)"
committed="$web_root/app/api/generated"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

bash "$web_root/scripts/openapi-gen.sh" "$tmp/openapi.ts" >/dev/null

if diff -r -u "$tmp" "$committed"; then
  echo "api-drift-check: generated API surface is up to date."
  exit 0
fi

cat >&2 <<EOF

api-drift-check: apps/web/app/api/generated/ no longer matches
docs/api/openapi.yaml. The diff above reads: '-' fresh output, '+'
committed artifact. Run 'make api-gen' and commit the result.
EOF
exit 1
