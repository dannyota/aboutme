#!/usr/bin/env bash
# openapi-gen.sh — generate the TypeScript API surface from the contract.
#
# This is the SINGLE source of the generator invocation. `npm run api:gen`
# writes the committed artifact with it, and api-drift-check.sh runs the
# very same script into a throwaway directory. Neither one hard-codes the
# flags, so the gate can never be checking a different generation than the
# one a developer runs.
#
#   apps/web/scripts/openapi-gen.sh <output-file>
#
# The generator binary is resolved to apps/web's own node_modules rather
# than through `npx`: npx silently falls back to downloading whatever the
# registry currently calls that name, which would defeat the exact pin in
# package.json (docs/api/redocly.yaml documents the same hazard for the
# `redocly` decoy package).
set -Eeuo pipefail

out="${1:-}"
if [ -z "$out" ]; then
  echo "usage: $(basename "$0") <output-file>" >&2
  exit 2
fi

web_root="$(cd "$(dirname "$0")/.." && pwd)"
repo_root="$(cd "$web_root/../.." && pwd)"
generator="$web_root/node_modules/.bin/openapi-typescript"
spec="$repo_root/docs/api/openapi.yaml"

if [ ! -x "$generator" ]; then
  echo "openapi-gen: $generator is missing — run 'npm ci' in apps/web" >&2
  exit 1
fi

# No generator flags beyond --output, deliberately: every flag is another
# way for the committed artifact and a developer's local run to disagree,
# and the defaults already produce what this contract needs (the empty
# `Envelope.data` schema becomes `unknown`, not `Record<string, never>`).
#
# docs/api/redocly.yaml is NOT passed with -c. It is a *lint* config, and
# openapi-typescript bundles @redocly/openapi-core v1 while this repo lints
# with @redocly/cli v2 — v1 re-runs those lint rules here without honoring
# v2's docs/api/.redocly.lint-ignore.yaml, so passing it prints six
# already-adjudicated `operation-2xx-response` warnings on every generate
# and every drift check. Verified byte-identical output with and without
# the flag; `make api-check` keeps @redocly/cli as the single lint gate.
exec "$generator" "$spec" --output "$out"
