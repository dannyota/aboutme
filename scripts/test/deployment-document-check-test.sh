#!/usr/bin/env bash
# Static, offline tests for scripts/deployment-document-check.sh. curl, gh,
# systemd-run, and the document-check binary are stubs found through PATH and
# the script's test-only environment variables. The shared local-check lock
# and /proc/meminfo are overridden the same way, so this never touches the
# real lock, the network, or a Go build (instructions/resources.md).
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
SCRIPT=$ROOT/scripts/deployment-document-check.sh

fail() {
  printf 'deployment-document-check-test: %s\n' "$*" >&2
  exit 1
}

WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-document-check-test.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

REPO=$WORK/repo
STUB=$WORK/stub
BIN=$WORK/bin
mkdir -p "$REPO/scripts" "$STUB" "$BIN"
cp "$SCRIPT" "$REPO/scripts/deployment-document-check.sh"
FIXTURE=$ROOT/deploy/observer/testdata/examples/verified.json
[ -f "$FIXTURE" ] || fail 'the verified.json fixture is missing'

printf '#!/usr/bin/env bash\nexit 99\n' >"$BIN/systemd-run"
cat >"$BIN/curl" <<'STUBEOF'
#!/usr/bin/env bash
# Copies the staged headers and body to the -D and -o files.
hdr= out=
while [ "$#" -gt 0 ]; do
  case $1 in
  -D) hdr=$2; shift ;;
  -o) out=$2; shift ;;
  esac
  shift
done
[ ! -f "$STUB/curl-fail" ] || exit 22
cp "$STUB/headers" "$hdr"
cp "$STUB/body" "$out"
STUBEOF
cat >"$BIN/gh" <<'STUBEOF'
#!/usr/bin/env bash
if [ "$1 $2" = 'auth status' ]; then
  [ ! -f "$STUB/gh-auth-fail" ]
  exit
fi
printf '%s\n' "$*" >>"$STUB/gh.log"
case " $* " in
*' --predicate-type '*) [ ! -f "$STUB/fail-sbom" ] ;;
*) [ ! -f "$STUB/fail-provenance" ] ;;
esac
STUBEOF
cat >"$BIN/document-check" <<'STUBEOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$STUB/dc.log"
cat >/dev/null
if [ -f "$STUB/dc-fail" ]; then
  echo 'document is stale' >&2
  exit 1
fi
cat "$STUB/dc-out"
STUBEOF
chmod 0755 "$BIN"/*

LOCK_PATH=$WORK/local-check.lock
MEMINFO_HIGH=$WORK/meminfo-high
MEMINFO_LOW=$WORK/meminfo-low
printf 'MemTotal:       33554432 kB\nMemAvailable:   16000000 kB\n' >"$MEMINFO_HIGH"
printf 'MemTotal:       33554432 kB\nMemAvailable:     100000 kB\n' >"$MEMINFO_LOW"

IMAGE=ghcr.io/dannyota/aboutme-server
D1=sha256:9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c
D2=sha256:a41ba41ba41ba41ba41ba41ba41ba41ba41ba41ba41ba41ba41ba41ba41ba41b

GOOD_HEADERS='HTTP/2 200
content-type: application/json
strict-transport-security: max-age=31536000; includeSubDomains
x-content-type-options: nosniff
cache-control: public, max-age=30
access-control-allow-origin: *
date: Fri, 02 Oct 2026 03:21:00 GMT
age: 5
server: CloudFront
'

# reset restores the all-passing stubs and a clean evidence directory.
reset() {
  rm -rf -- "$REPO/.dev" "$STUB"/*.log "$STUB"/*-fail "$STUB"/fail-*
  printf '%s' "$GOOD_HEADERS" | sed 's/$/\r/' >"$STUB/headers"
  cp "$FIXTURE" "$STUB/body"
  printf '%s %s v0.6.5\n%s %s v0.6.5\n' "$IMAGE" "$D1" "$IMAGE" "$D2" >"$STUB/dc-out"
}

# run_check runs the script with the stubs and stores its output and status.
OUTPUT=
RC=0
run_check() {
  RC=0
  OUTPUT=$(STUB=$STUB PATH=$BIN:$PATH \
    ABOUTME_LOCAL_CHECK_LOCK_PATH=$LOCK_PATH \
    ABOUTME_MEMINFO_PATH=${MEMINFO:-$MEMINFO_HIGH} \
    ABOUTME_DOCUMENT_CHECK_BIN=$BIN/document-check \
    "$REPO/scripts/deployment-document-check.sh" "$@" 2>&1) || RC=$?
}

summary() { cat "$REPO"/.dev/prod-checks/transparency/*/summary.txt; }

expect_refusal() { # expect_refusal <name> <message fragment>
  [ "$RC" -ne 0 ] || fail "$1: the script ran"
  grep -Fq -- "$2" <<<"$OUTPUT" || fail "$1: wrong diagnostic: $OUTPUT"
}

expect_fail() { # expect_fail <name> <FAIL line fragment>
  run_check
  [ "$RC" -ne 0 ] || fail "$1: the check passed"
  summary | grep -E "^FAIL .*$2" >/dev/null || fail "$1: no FAIL line for $2"
}

# --- usage and preflight ------------------------------------------------------

reset
run_check extra
expect_refusal 'an argument was accepted' 'usage: deployment-document-check.sh'

MEMINFO=$MEMINFO_LOW run_check
expect_refusal 'low MemAvailable was accepted' 'fewer than 8 GiB MemAvailable'

exec {HELD_FD}>>"$LOCK_PATH"
flock "$HELD_FD"
run_check
expect_refusal 'a held shared lock was accepted' 'another local check holds the shared lock'
flock -u "$HELD_FD"
exec {HELD_FD}>&-

touch "$STUB/gh-auth-fail"
run_check
expect_refusal 'a signed-out gh was accepted' 'gh is not signed in'

# --- happy path ---------------------------------------------------------------

reset
run_check
[ "$RC" -eq 0 ] || fail "happy path exited $RC: $OUTPUT"
summary | grep -q . || fail 'summary.txt is empty'
if summary | grep -v '^PASS ' | grep -q .; then fail 'happy path has a non-PASS line'; fi
grep -Fq 'evidence: ' <<<"$OUTPUT" || fail 'the evidence path was not printed'
[ "$(grep -c . "$STUB/dc.log")" -eq 2 ] || fail 'document-check did not run once per URL'
grep -Fxq -- '-now 2026-10-02T03:21:05Z' "$STUB/dc.log" ||
  fail 'now is not the Date header plus Age'
# Two digests, each verified twice (provenance and SBOM), no repeats.
[ "$(grep -c . "$STUB/gh.log")" -eq 4 ] || fail 'gh ran the wrong number of times'
[ "$(grep -c -- "--predicate-type https://spdx.dev/Document/v2.3" "$STUB/gh.log")" -eq 2 ] ||
  fail 'the SBOM verification did not run once per digest'
grep -Fq -- "attestation verify oci://$IMAGE@$D1 --repo dannyota/aboutme --signer-workflow dannyota/aboutme/.github/workflows/release-images.yml --source-ref refs/tags/v0.6.5 --deny-self-hosted-runners" "$STUB/gh.log" ||
  fail 'the provenance command line is wrong'

# --- duplicate digests --------------------------------------------------------

reset
printf '%s %s v0.6.5\n%s %s v0.6.5\n' "$IMAGE" "$D1" "$IMAGE" "$D1" >"$STUB/dc-out"
run_check
[ "$RC" -eq 0 ] || fail "duplicate digests exited $RC"
[ "$(grep -c . "$STUB/gh.log")" -eq 2 ] || fail 'a duplicate digest was verified twice'

# --- header failures ----------------------------------------------------------

reset
grep -v '^x-content-type-options' <<<"$GOOD_HEADERS" >"$STUB/headers"
expect_fail 'missing nosniff' 'x-content-type-options'

reset
printf '%sx-amz-request-id: ABC\n' "$GOOD_HEADERS" >"$STUB/headers"
expect_fail 'x-amz-request-id' 'x-amz-request-id absent'

reset
printf '%s' "${GOOD_HEADERS/server: CloudFront/Server: AmazonS3}" >"$STUB/headers"
expect_fail 'Server: AmazonS3' 'server absent or CloudFront'

reset
printf '%s' "${GOOD_HEADERS/max-age=30/max-age=300}" >"$STUB/headers"
expect_fail 'wrong cache-control' 'cache-control'

reset
sed 's|^access-control-allow-origin: \*$|access-control-allow-origin: https://example.com|' <<<"$GOOD_HEADERS" >"$STUB/headers"
expect_fail 'wrong allow-origin' 'access-control-allow-origin'

reset
grep -v '^access-control-allow-origin' <<<"$GOOD_HEADERS" >"$STUB/headers"
expect_fail 'missing allow-origin' 'access-control-allow-origin'

reset
: >"$STUB/dc-out"
expect_fail 'no running images' 'at least one running image'

reset
touch "$STUB/curl-fail"
expect_fail 'fetch failure' 'fetch'

# --- document-check and attestation failures ------------------------------------

reset
touch "$STUB/dc-fail"
expect_fail 'document-check failure' 'document-check'

reset
touch "$STUB/fail-provenance"
expect_fail 'provenance failure' 'provenance'

reset
touch "$STUB/fail-sbom"
expect_fail 'sbom failure' 'sbom'
summary | grep -q '^PASS .*provenance' || fail 'provenance did not pass when only the SBOM failed'

reset
printf '%s %s -\n' "$IMAGE" "$D1" >"$STUB/dc-out"
expect_fail 'null version' 'no version'
[ ! -f "$STUB/gh.log" ] || fail 'gh ran for an image with no version'

printf 'deployment-document-check static tests: PASS\n'
