#!/usr/bin/env bash
# Runs `npm audit --omit=dev` in one directory and fails on any high or
# critical advisory, like `--audit-level=high`, except advisories listed in
# .github/npm-audit-exceptions.json. Each exception names a GHSA id, the
# directories it applies to (`dirs`, relative to the repository root, "." for
# the root), a reason, and an `expires` date (YYYY-MM-DD). An exception is
# valid through its expiry date, and the date may be at most 90 days ahead.
# Any expired or malformed entry fails every run, so a stale exception cannot
# hide a finding.
#
# Usage: scripts/npm-audit.sh [directory]   (default: the current directory)
# Test hooks: NPM_AUDIT_NPM (npm command), NPM_AUDIT_EXCEPTIONS (file),
# NPM_AUDIT_TODAY (YYYY-MM-DD), NPM_AUDIT_ROOT (the repository root).
set -Eeuo pipefail

ROOT=${NPM_AUDIT_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)}
DIR=${1:-.}
NPM=${NPM_AUDIT_NPM:-npm}
EXCEPTIONS=${NPM_AUDIT_EXCEPTIONS:-$ROOT/.github/npm-audit-exceptions.json}
TODAY=${NPM_AUDIT_TODAY:-$(date -u +%F)}

die() {
  printf 'npm-audit: %s\n' "$*" >&2
  exit 1
}

command -v jq >/dev/null || die 'jq is required'
[ -f "$EXCEPTIONS" ] || die "missing exceptions file: $EXCEPTIONS"
jq -e 'type == "array" and all(.[]; type == "object")' "$EXCEPTIONS" >/dev/null ||
  die "$EXCEPTIONS must hold a JSON array of objects"

limit=$(date -u -d "$TODAY +90 days" +%F)
bad=$(jq -r --arg limit "$limit" '
  .[] | select(
    (.id | type != "string") or (.id | test("^GHSA(-[0-9a-z]{4}){3}$") | not)
    or (.reason | type != "string") or (.reason == "")
    or (.dirs | type != "array") or (.dirs | length == 0) or (.dirs | any(type != "string"))
    or (.expires | type != "string") or (.expires | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}$") | not)
  ) | "malformed exception entry: \(tojson)"
' "$EXCEPTIONS")
[ -z "$bad" ] || die "$bad"

expired=$(jq -r --arg today "$TODAY" '
  .[] | select(.expires < $today) | "\(.id) expired on \(.expires)"
' "$EXCEPTIONS")
if [ -n "$expired" ]; then
  printf 'npm-audit: expired exception: %s\n' "$expired" >&2
  die "renew or remove the entry in $EXCEPTIONS"
fi

toolong=$(jq -r --arg limit "$limit" '
  .[] | select(.expires > $limit) | "\(.id) expires on \(.expires)"
' "$EXCEPTIONS")
[ -z "$toolong" ] || die "exception expires more than 90 days ahead (latest $limit): $toolong"

# The directory as the exceptions file names it: relative to the repository
# root, "." for the root itself.
reldir=$(realpath -m --relative-to="$ROOT" "$DIR")

# npm audit exits non-zero when it finds anything; the report decides.
report=$(cd "$DIR" && "$NPM" audit --omit=dev --json) || true
jq -e '.vulnerabilities and (.error | not)' <<<"$report" >/dev/null ||
  die "npm audit gave no usable report in $DIR"

# Advisories are the object entries of each package's `via`; string entries
# only name another vulnerable package that carries the advisory.
failing=$(jq -r --arg dir "$reldir" --slurpfile ex "$EXCEPTIONS" '
  [.vulnerabilities[].via[] | select(type == "object")
    | select(.severity == "high" or .severity == "critical")
    | {id: ((.url | capture("(?<id>GHSA(-[0-9a-z]{4}){3})").id) // .url), severity, title}]
  | unique_by(.id)
  | map(select(.id as $id | ($ex[0] | any(.[]; .id == $id and (.dirs | index($dir)))) | not))
  | .[] | "\(.severity) \(.id) \(.title)"
' <<<"$report")

if [ -n "$failing" ]; then
  printf 'npm-audit: unlisted high or critical advisories in %s:\n%s\n' "$DIR" "$failing" >&2
  exit 1
fi
printf 'npm-audit: %s ok\n' "$DIR"
