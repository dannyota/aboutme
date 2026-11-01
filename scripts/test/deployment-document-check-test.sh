#!/usr/bin/env bash
# Offline wrapper tests with stub network tools and watchdog execution.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
SCRIPT=$ROOT/scripts/deployment-document-check.sh

fail() {
  printf 'deployment-document-check-test: %s\n' "$*" >&2
  exit 1
}

WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-document-check-test.XXXXXX")
WORK=$(cd "$WORK" && pwd -P)
trap 'rm -rf -- "$WORK"' EXIT

REPO=$WORK/repo
STUB=$WORK/stub
BIN=$WORK/bin
mkdir -p "$REPO/scripts" "$STUB" "$BIN"
cp "$SCRIPT" "$REPO/scripts/deployment-document-check.sh"
mkdir -p "$REPO/scripts/lib"
cp "$ROOT/scripts/lib/platform.sh" "$ROOT/scripts/lib/local-check.py" "$REPO/scripts/lib/"

FIXTURE=$ROOT/deploy/observer/testdata/examples/verified.json
[ -f "$FIXTURE" ] || fail 'the verified.json fixture is missing'

cat >"$REPO/scripts/run-owned.sh" <<'STUBEOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$STUB/run-owned.log"
[ "${STUB_OWNED_REFUSAL:-}" != invalid ] || { echo 'refused: cannot inspect process ownership' >&2; exit 125; }
shift 3
exec "$@"
STUBEOF
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
if [ -f "$STUB/dc-out-www" ] && [ "$(grep -c . "$STUB/dc.log")" -eq 2 ]; then
  cat "$STUB/dc-out-www"
else
  cat "$STUB/dc-out"
fi
STUBEOF
chmod 0755 "$BIN"/*

LOCK_PATH=$WORK/local-check.lock
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
  rm -rf -- "$REPO/.dev" "$STUB"/*.log "$STUB"/*-fail "$STUB"/fail-* "$STUB/dc-out-www"
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

STUB_OWNED_REFUSAL=invalid run_check
expect_refusal 'missing process ownership was accepted' 'refused: cannot inspect process ownership'

python3 -B - "$LOCK_PATH" "$WORK/lock-ready" <<'LOCK' &
import fcntl, pathlib, sys, time
with open(sys.argv[1], "w") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    pathlib.Path(sys.argv[2]).touch()
    time.sleep(30)
LOCK
holder=$!
for ((i=0; i<200; i++)); do
  [ ! -f "$WORK/lock-ready" ] || break
  sleep 0.05
done
[ -f "$WORK/lock-ready" ] || fail 'lock holder never started'
run_check
kill "$holder"
wait "$holder" || true
expect_refusal 'a held shared lock was accepted' 'another local check holds the shared lock'

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
grep -Fxq -- '--timeout 900 -- bash '"$REPO"'/scripts/deployment-document-check.sh' "$STUB/run-owned.log" ||
  fail 'the check did not run inside the watchdog'
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
grep -v '^date' <<<"$GOOD_HEADERS" >"$STUB/headers"
expect_fail 'missing date' 'one date header'
[ ! -f "$STUB/dc.log" ] || fail 'document-check ran without a Date header'

reset
printf '%s' "${GOOD_HEADERS/age: 5/age: soon}" >"$STUB/headers"
expect_fail 'non-numeric age' 'date and age headers'

reset
printf '%s %s v0.6.5\n' "$IMAGE" "$D1" >"$STUB/dc-out-www"
expect_fail 'www differs from apex' 'www lists the same images as apex'

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
