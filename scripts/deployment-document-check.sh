#!/usr/bin/env bash
# Checks the published deployment document on production
# (docs/design/deployment-transparency/README.md "Serving and caching" and
# "Run, freshness, and staleness", verification.md "Verify it yourself").
# For the apex and www URLs it fetches the document with a cross-origin
# request, checks the response headers, runs the document-check command
# (schema, size, forbidden identifiers, staleness at Date plus Age), then
# verifies the build provenance and the SBOM attestation of every distinct
# image digest with gh. Every failure is recorded and the run continues, so
# one run reports everything.
#
# Usage: scripts/deployment-document-check.sh   (no arguments)
#
# The manager runs it after a deploy or an observer update. Evidence goes to
# .dev/prod-checks/transparency/<UTC time>/ with a summary.txt of PASS and
# FAIL lines; the exit status is 0 only when every check passed. It holds
# the shared local-check lock in a parent and runs through run-capped.sh.
#
# Test overrides: ABOUTME_LOCAL_CHECK_LOCK_PATH selects a scratch lock;
# ABOUTME_DOCUMENT_CHECK_BIN selects a prebuilt document-check binary.
# ABOUTME_DOCUMENT_CHECK_SCOPED marks the watchdog pass.
# shellcheck disable=SC2317 # helpers run through check
set -Eeuo pipefail

REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
URLS=(
  "apex https://aboutme.vn/.well-known/deployment.json"
  "www https://www.aboutme.vn/.well-known/deployment.json"
)
SIGNER=dannyota/aboutme/.github/workflows/release-images.yml

fail() {
  printf 'deployment-document-check: %s\n' "$*" >&2
  exit 1
}

[ "$#" -eq 0 ] || fail 'usage: deployment-document-check.sh (no arguments)'
[ "$(id -u)" -ne 0 ] || fail 'must not run as root'

# --- Preflight and cap ------------------------------------------------------

# shellcheck source=scripts/lib/platform.sh
source "$REPO/scripts/lib/platform.sh"
require_modern_bash || exit 1
require_gnu_tools date || exit 1
if [ -z "${ABOUTME_DOCUMENT_CHECK_SCOPED:-}" ]; then
  LOCK=${ABOUTME_LOCAL_CHECK_LOCK_PATH:-$(git -C "$REPO" rev-parse --path-format=absolute --git-common-dir)/aboutme-local-check.lock}
  exec python3 "$REPO/scripts/lib/local-check.py" "$LOCK" 0 \
    env ABOUTME_DOCUMENT_CHECK_SCOPED=1 bash "$REPO/scripts/run-capped.sh" \
    --timeout 900 -- bash "$REPO/scripts/deployment-document-check.sh"
fi

for tool in curl gh; do
  command -v "$tool" >/dev/null || fail "$tool is not installed"
done
gh auth status >/dev/null 2>&1 || fail 'gh is not signed in; run gh auth login'

EVIDENCE=$REPO/.dev/prod-checks/transparency/$(date -u +%Y%m%dT%H%M%SZ)
mkdir -p "$EVIDENCE"
SUMMARY=$EVIDENCE/summary.txt
: >"$SUMMARY"
FAILED=0

record() { # record <PASS|FAIL> <message>
  printf '%s %s\n' "$1" "$2" >>"$SUMMARY"
  [ "$1" = PASS ] || FAILED=1
}

# check <label> <command...> records PASS or FAIL from the command's status.
check() {
  local label=$1
  shift
  if "$@"; then record PASS "$label"; else record FAIL "$label"; fi
}

matches() { # matches <value> <glob>
  # shellcheck disable=SC2254 # the pattern is meant to glob
  case $1 in $2) return 0 ;; esac
  return 1
}
empty() { [ -z "$1" ]; }

finish() {
  cat "$SUMMARY"
  printf 'evidence: %s\n' "$EVIDENCE"
  exit "$FAILED"
}

# --- document-check binary ---------------------------------------------------

BUILD=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-document-check.XXXXXX")
trap 'rm -rf -- "$BUILD"' EXIT
if [ -n "${ABOUTME_DOCUMENT_CHECK_BIN:-}" ]; then
  DOC_CHECK=$ABOUTME_DOCUMENT_CHECK_BIN
else
  DOC_CHECK=$BUILD/document-check
  (
    cd "$REPO/deploy/observer"
    GOWORK=off bash "$REPO/scripts/run-capped.sh" --timeout 300 -- go build -o "$DOC_CHECK" ./cmd/document-check
  ) >"$EVIDENCE/build.txt" 2>&1 || {
    record FAIL 'build document-check'
    finish
  }
fi

# --- Headers -----------------------------------------------------------------

# header <file> <name> prints the values of a header, lower-cased name match.
header() {
  awk -F: -v n="$2" 'tolower($1) == n {
    sub(/^[^:]*:[ \t]*/, ""); sub(/[ \t]+$/, ""); print }' "$1"
}

# expect_header <label> <file> <name> <glob>: the lower-cased value must match.
expect_header() {
  local label=$1 file=$2 name=$3 pattern=$4 value
  value=$(header "$file" "$name" | tr '[:upper:]' '[:lower:]')
  check "$label $name matches $pattern" matches "$value" "$pattern"
}

server_ok() { [ -z "$1" ] || [ "$1" = CloudFront ]; }

check_headers() { # check_headers <label> <header file>
  local label=$1 file=$2 status name
  status=$(awk 'NR == 1 {print $2}' "$file")
  check "$label http status 200" [ "$status" = 200 ]
  expect_header "$label" "$file" content-type 'application/json*'
  expect_header "$label" "$file" strict-transport-security '*max-age=31536000*'
  expect_header "$label" "$file" x-content-type-options nosniff
  expect_header "$label" "$file" cache-control 'public, max-age=30'
  check "$label access-control-allow-origin is *" \
    [ "$(header "$file" access-control-allow-origin)" = '*' ]
  check "$label server absent or CloudFront" server_ok "$(header "$file" server)"
  for name in x-amz-request-id x-amz-id-2 x-amz-server-side-encryption; do
    check "$label $name absent" empty "$(header "$file" "$name")"
  done
}

# fetch_and_check <label> <url> leaves the document-check lines in
# $EVIDENCE/<label>.images.
fetch_and_check() {
  local label=$1 url=$2 hdr body date_value age now rc
  hdr=$EVIDENCE/$label.headers
  body=$EVIDENCE/$label.json
  if ! curl -fsS --max-time 20 -D "$hdr.raw" -o "$body" \
    -H 'Origin: https://example.com' "$url" 2>"$EVIDENCE/$label.curl.txt"; then
    record FAIL "$label fetch $url"
    return
  fi
  record PASS "$label fetch $url"
  # Keep only the last response block, without carriage returns.
  tr -d '\r' <"$hdr.raw" | awk 'BEGIN{RS=""} {last=$0} END{print last}' >"$hdr"
  rm -f -- "$hdr.raw"
  check_headers "$label" "$hdr"

  date_value=$(header "$hdr" date)
  # GNU date reads an empty string as midnight, so a missing Date header
  # must fail here rather than judge staleness at the wrong time.
  if [ -z "$date_value" ] || [[ $date_value == *$'\n'* ]]; then
    record FAIL "$label one date header"
    return
  fi
  age=$(header "$hdr" age)
  age=${age:-0}
  if [[ $age =~ ^[0-9]+$ ]] && now=$(date -u -d "$date_value" +%s 2>/dev/null); then
    now=$(date -u -d "@$((now + age))" +%Y-%m-%dT%H:%M:%SZ)
  else
    record FAIL "$label date and age headers are readable"
    return
  fi
  rc=0
  "$DOC_CHECK" -now "$now" <"$body" >"$EVIDENCE/$label.images" 2>"$EVIDENCE/$label.document-check.err" || rc=$?
  check "$label document-check at $now" [ "$rc" -eq 0 ]
  [ "$rc" -eq 0 ] || : >"$EVIDENCE/$label.images"
}

for entry in "${URLS[@]}"; do
  fetch_and_check "${entry%% *}" "${entry#* }"
done

# --- Attestations (apex document) --------------------------------------------

verify() { # verify <outfile> <image> <digest> <version> [extra args]
  local out=$1 image=$2 digest=$3 version=$4
  shift 4
  env -u GH_DEBUG bash "$REPO/scripts/run-capped.sh" --timeout 120 -- gh attestation verify "oci://$image@$digest" \
    --repo dannyota/aboutme --signer-workflow "$SIGNER" \
    --source-ref "refs/tags/$version" --deny-self-hosted-runners "$@" \
    >"$out" 2>&1
}

check 'apex lists at least one running image' [ -s "$EVIDENCE/apex.images" ]
# The design says www serves the same object, so its images must match.
check 'www lists the same images as apex' \
  cmp -s "$EVIDENCE/apex.images" "$EVIDENCE/www.images"
while read -r image digest version; do
  [ -n "$image" ] || continue
  tag=${digest#sha256:}
  tag=${tag:0:12}
  if [ "$version" = - ]; then
    record FAIL "$image@$digest not verified, no version"
    continue
  fi
  check "$image@$digest provenance" \
    verify "$EVIDENCE/gh-$tag.provenance.txt" "$image" "$digest" "$version"
  check "$image@$digest sbom" \
    verify "$EVIDENCE/gh-$tag.sbom.txt" "$image" "$digest" "$version" \
    --predicate-type https://spdx.dev/Document/v2.3
done < <(awk '!seen[$2]++' "$EVIDENCE/apex.images" 2>/dev/null)

finish
