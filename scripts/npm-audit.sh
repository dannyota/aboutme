#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=${NPM_AUDIT_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)}
DIR=${1:-.}
PNPM=${NPM_AUDIT_PNPM:-pnpm}
EXCEPTIONS=${NPM_AUDIT_EXCEPTIONS:-$ROOT/.github/npm-audit-exceptions.json}
TODAY=${NPM_AUDIT_TODAY:-$(date -u +%F)}
LIMIT=${NPM_AUDIT_TIMEOUT:-60}

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

reldir=$(realpath -m --relative-to="$ROOT" "$DIR")
[ -f "$DIR/package.json" ] || die "$DIR has no package.json"
name=$(jq -r '.name // empty' "$DIR/package.json")
[ -n "$name" ] || die "$DIR/package.json has no name for the pnpm filter"

# pnpm audit exits 1 for any advisory at or above its audit level, so the report decides (pnpm.io/cli/audit).
out=$(mktemp "${TMPDIR:-/tmp}/npm-audit.XXXXXX")
trap 'rm -f -- "$out"' EXIT
usable() {
  jq -e '(.advisories | type == "object") and (.metadata.vulnerabilities | type == "object") and (.error | not)' \
    <<<"$report" >/dev/null 2>&1
}
for attempt in 1 2; do
  (cd "$DIR" && timeout -k 2 "$LIMIT" "$PNPM" audit --prod --json --filter "$name" </dev/null) >"$out" || true
  report=$(<"$out")
  usable && break
  printf 'npm-audit: attempt %s gave no usable report in %s\n' "$attempt" "$DIR" >&2
done
usable || die "pnpm audit gave no usable report in $DIR"

failing=$(jq -r --arg dir "$reldir" --slurpfile ex "$EXCEPTIONS" '
  [.advisories[]
    | select(.severity == "high" or .severity == "critical")
    | {id: (.github_advisory_id // ((.url // "" | capture("(?<id>GHSA(-[0-9a-z]{4}){3})").id) // .url)), severity, title}]
  | unique_by(.id)
  | map(select(.id as $id | ($ex[0] | any(.[]; .id == $id and (.dirs | index($dir)))) | not))
  | .[] | "\(.severity) \(.id) \(.title)"
' <<<"$report")

if [ -n "$failing" ]; then
  printf 'npm-audit: unlisted high or critical advisories in %s:\n%s\n' "$DIR" "$failing" >&2
  exit 1
fi
printf 'npm-audit: %s ok\n' "$DIR"
