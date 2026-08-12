#!/usr/bin/env bash

# Black-box regression for Semgrep's effective dependency-input selection.
# Listing targets exercises .semgrepignore without running rules or contacting
# Semgrep's service.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-semgrep-sca-inputs.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

fail() {
  printf 'not ok - %s\n' "$*" >&2
  failures=$((failures + 1))
}

dependency_inputs=(
  package.json
  package-lock.json
  apps/web/package.json
  apps/web/package-lock.json
  packages/schema/package.json
  packages/schema/package-lock.json
  apps/server/go.mod
  apps/server/go.sum
  packages/schema/gen/go/go.mod
  go.work
  go.work.sum
)
ignored_control=packages/schema/gen/go/resume.go
failures=0

for path in "${dependency_inputs[@]}" "$ignored_control"; do
  if ! git -C "$ROOT" ls-files --error-unmatch -- "$path" >/dev/null 2>&1; then
    fail "required tracked path is missing: $path"
  fi
done

if ! command -v semgrep >/dev/null 2>&1; then
  fail 'semgrep is unavailable; effective ignore policy was not tested'
elif ! (
  cd "$ROOT"
  SEMGREP_SEND_METRICS=off semgrep scan --config .semgrep.yml --x-ls .
) >"$WORK/selected" 2>"$WORK/semgrep.err"; then
  fail 'semgrep could not list its selected targets'
  sed 's/^/    /' "$WORK/semgrep.err" >&2
else
  sed -i 's#^\./##' "$WORK/selected"

  for path in "${dependency_inputs[@]}"; do
    if ! grep -Fqx -- "$path" "$WORK/selected"; then
      fail "connected Supply Chain dependency input is excluded: $path"
    fi
  done

  if grep -Fqx -- "$ignored_control" "$WORK/selected"; then
    fail "ignore-policy control was unexpectedly selected: $ignored_control"
  fi
fi

if [ "$failures" -ne 0 ]; then
  exit 1
fi

printf 'ok - Semgrep selects every tracked npm and Go dependency input\n'
