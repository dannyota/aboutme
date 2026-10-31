#!/usr/bin/env bash

# Black-box regression for Semgrep's dependency and handwritten Go selection.
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
handwritten_inputs=()

while IFS= read -r -d '' path; do
  if [[ "$path" = *.go ]] &&
    ! grep -Eq '^// Code generated .* DO NOT EDIT\.$' "$ROOT/$path"; then
    handwritten_inputs+=("$path")
  fi
done < <(git -C "$ROOT" ls-files -z -- packages/schema/gen/go/)

if [ "${#handwritten_inputs[@]}" -eq 0 ]; then
  fail 'no handwritten schema Go inputs were found'
fi

for path in "${dependency_inputs[@]}" "${handwritten_inputs[@]}" "$ignored_control"; do
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

  for path in "${handwritten_inputs[@]}"; do
    if ! grep -Fqx -- "$path" "$WORK/selected"; then
      fail "handwritten schema Go input is excluded: $path"
    fi
  done

  if grep -Fqx -- "$ignored_control" "$WORK/selected"; then
    fail "ignore-policy control was unexpectedly selected: $ignored_control"
  fi
fi

if [ "$failures" -ne 0 ]; then
  exit 1
fi

printf 'ok - Semgrep selects dependency inputs and handwritten schema Go files\n'
