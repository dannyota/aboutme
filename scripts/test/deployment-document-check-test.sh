#!/usr/bin/env bash
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
*' --predicate-type '*) [ ! -f "$STUB/fail-sbom" ] || exit 1 ;;
*) [ ! -f "$STUB/fail-provenance" ] || exit 1 ;;
esac
[[ " $* " == *' --format json '* ]] || { echo 'verified'; exit 0; }
[[ $3 =~ ^oci://ghcr\.io/([^/]+)/aboutme- ]] || exit 1
owner=${BASH_REMATCH[1]}
case $owner in
dannyota) id=244348267 ;;
dntmakers) id=342323513 ;;
*) id=0 ;;
esac
jq -n --arg owner "$owner" --arg id "$id" '[{verificationResult: {signature: {certificate: {
  sourceRepositoryIdentifier: "1318874703", sourceRepositoryOwnerIdentifier: $id,
  buildSignerURI: "https://github.com/\($owner)/aboutme/.github/workflows/release-images.yml@refs/tags/v0.6.5"}}}}]' |
  jq "$(cat "$STUB/gh-filter" 2>/dev/null || echo .)"
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
D3=sha256:c0ddc0ddc0ddc0ddc0ddc0ddc0ddc0ddc0ddc0ddc0ddc0ddc0ddc0ddc0ddc0dd
D4=sha256:d00dd00dd00dd00dd00dd00dd00dd00dd00dd00dd00dd00dd00dd00dd00dd00d

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

reset() {
  rm -rf -- "$REPO/.dev" "$STUB"/*.log "$STUB"/*-fail "$STUB"/fail-* "$STUB/dc-out-www" "$STUB/gh-filter"
  printf '%s' "$GOOD_HEADERS" | sed 's/$/\r/' >"$STUB/headers"
  cp "$FIXTURE" "$STUB/body"
  printf '%s %s v0.6.5\n%s %s v0.6.5\n' "$IMAGE" "$D1" "$IMAGE" "$D2" >"$STUB/dc-out"
}

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

expect_refusal() {
  [ "$RC" -ne 0 ] || fail "$1: the script ran"
  grep -Fq -- "$2" <<<"$OUTPUT" || fail "$1: wrong diagnostic: $OUTPUT"
}

expect_fail() {
  run_check
  [ "$RC" -ne 0 ] || fail "$1: the check passed"
  summary | grep -E "^FAIL .*$2" >/dev/null || fail "$1: no FAIL line for $2"
}

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
[ "$(grep -c . "$STUB/gh.log")" -eq 4 ] || fail 'gh ran the wrong number of times'
[ "$(grep -c -- "--predicate-type https://spdx.dev/Document/v2.3" "$STUB/gh.log")" -eq 2 ] ||
  fail 'the SBOM verification did not run once per digest'
legacy_argv="attestation verify oci://$IMAGE@$D1 --repo dannyota/aboutme --bundle-from-oci --signer-workflow dannyota/aboutme/.github/workflows/release-images.yml --source-ref refs/tags/v0.6.5 --deny-self-hosted-runners"
grep -Fxq -- "$legacy_argv --format json" "$STUB/gh.log" ||
  fail 'the dannyota provenance command line is wrong'
grep -Fxq -- "$legacy_argv --predicate-type https://spdx.dev/Document/v2.3 --format json" "$STUB/gh.log" ||
  fail 'the dannyota SBOM command line is wrong'
summary | grep -q '^PASS app images share one owner$' || fail 'the app owner check did not run'

NEW=ghcr.io/dntmakers/aboutme
OLD=ghcr.io/dannyota/aboutme
images() {
  local line
  : >"$STUB/dc-out"
  for line in "$@"; do printf '%s v0.6.5\n' "$line" >>"$STUB/dc-out"; done
}
document() {
  local server=$1 web=$2 caddy=$3 maintenance=${4:-}
  jq --arg server "$server" --arg web "$web" --arg caddy "$caddy" --arg maintenance "$maintenance" --arg d4 "$D4" '
    .components |= map(.image = "ghcr.io/\({server: $server, web: $web, caddy: $caddy}[.name])/aboutme-\(.name)")
    | if $maintenance == "" then . else .components += [.components[] | select(.name == "caddy")
        | .name = "maintenance" | .image = "ghcr.io/\($maintenance)/aboutme-caddy"
        | .running_images[0].digest = $d4] end' "$FIXTURE" >"$STUB/body"
  images "ghcr.io/$server/aboutme-server $D1" "ghcr.io/$web/aboutme-web $D2" "ghcr.io/$caddy/aboutme-caddy $D3" \
    ${maintenance:+"ghcr.io/$maintenance/aboutme-caddy $D4"}
}
reset
document dntmakers dntmakers dntmakers
run_check
[ "$RC" -eq 0 ] || fail "dntmakers images exited $RC: $(summary)"
for pair in "server $D1" "web $D2" "caddy $D3"; do
  modern_argv="attestation verify oci://$NEW-${pair% *}@${pair#* } --repo dntmakers/aboutme --signer-workflow dntmakers/aboutme/.github/workflows/release-images.yml --source-ref refs/tags/v0.6.5 --deny-self-hosted-runners"
  grep -Fxq -- "$modern_argv --format json" "$STUB/gh.log" ||
    fail "the dntmakers ${pair% *} provenance command line is wrong"
  grep -Fxq -- "$modern_argv --predicate-type https://spdx.dev/Document/v2.3 --format json" "$STUB/gh.log" ||
    fail "the dntmakers ${pair% *} SBOM command line is wrong"
done
! grep -q 'dannyota' "$STUB/gh.log" || fail 'a dntmakers image was verified under dannyota'

reset
document dannyota dannyota dannyota dntmakers
run_check
[ "$RC" -eq 0 ] || fail "a rollback with a newer maintenance image exited $RC: $(summary)"
summary | grep -q '^PASS app images share one owner$' || fail 'the rollback app owner check did not pass'
grep -Fq -- "oci://$NEW-caddy@$D4 --repo dntmakers/aboutme --signer-workflow dntmakers/aboutme/" "$STUB/gh.log" ||
  fail 'the maintenance image was not verified under its own owner'

reset
document dntmakers dannyota dntmakers
jq '.components |= map(if .name == "web" then .running_images = [] else . end)' "$STUB/body" >"$STUB/body.next"
mv "$STUB/body.next" "$STUB/body"
images "$NEW-server $D1" "$NEW-caddy $D3"
run_check
[ "$RC" -eq 0 ] || fail "a stopped component of the other owner exited $RC: $(summary)"

for mixed in 'dntmakers dntmakers dannyota dntmakers' 'dannyota dntmakers dannyota' \
  'dannyota dannyota dntmakers' 'dntmakers dannyota dntmakers dannyota' 'dannyota dannyota dntmakers dannyota'; do
  reset
  # shellcheck disable=SC2086
  document $mixed
  expect_fail "mixed app owners: $mixed" 'app images share one owner'
done

reset
touch "$STUB/dc-fail"
run_check
summary | grep -q '^FAIL app images share one owner$' || fail 'an unvalidated document passed the app owner check'

reset
images "ghcr.io/evil/aboutme-server $D1"
expect_fail 'unknown owner' 'provenance'
! grep -q 'ghcr.io/evil' "$STUB/gh.log" 2>/dev/null || fail 'gh verified an image of an unknown owner'
for image in ghcr.io/Dannyota/aboutme-server ghcr.io/dannyota/aboutme-other ghcr.io/dannyota/other-server \
  docker.io/dannyota/aboutme-server; do
  reset
  images "$image $D1"
  expect_fail "unknown image $image" 'provenance'
  [ ! -s "$STUB/gh.log" ] || fail "gh verified $image"
done

for case in \
  'repository id|.[0].verificationResult.signature.certificate.sourceRepositoryIdentifier = "1318874704"' \
  'numeric repository id|.[0].verificationResult.signature.certificate.sourceRepositoryIdentifier = 1318874703' \
  'missing repository id|del(.[0].verificationResult.signature.certificate.sourceRepositoryIdentifier)' \
  'owner id|.[0].verificationResult.signature.certificate.sourceRepositoryOwnerIdentifier = "342323513"' \
  'missing owner id|del(.[0].verificationResult.signature.certificate.sourceRepositoryOwnerIdentifier)' \
  'signer owner|.[0].verificationResult.signature.certificate.buildSignerURI |= sub("dannyota"; "dntmakers")' \
  'signer workflow|.[0].verificationResult.signature.certificate.buildSignerURI |= sub("release-images"; "other")' \
  'signer prefix|.[0].verificationResult.signature.certificate.buildSignerURI |= "x" + .' \
  'mixed results|. + [.[0] | .verificationResult.signature.certificate.sourceRepositoryIdentifier = "1"]' \
  'no results|[]' \
  'not an array|.[0]'; do
  reset
  printf '%s\n' "${case#*|}" >"$STUB/gh-filter"
  expect_fail "${case%%|*}" 'provenance'
  summary | grep -q '^FAIL .*sbom' || fail "${case%%|*}: the SBOM check passed"
done
reset
printf '%s\n' '.[0].verificationResult.signature.certificate.sourceRepositoryOwnerIdentifier = "244348267"' >"$STUB/gh-filter"
images "$NEW-server $D1"
expect_fail 'a dntmakers image with the dannyota owner id' 'provenance'
reset
printf '%s\n' '.[0].verificationResult.signature.certificate.buildSignerURI |= sub("dntmakers"; "dannyota")' >"$STUB/gh-filter"
images "$NEW-server $D1"
expect_fail 'a dntmakers image with a dannyota signer' 'provenance'

reset
printf '%s %s v0.6.5\n%s %s v0.6.5\n' "$IMAGE" "$D1" "$IMAGE" "$D1" >"$STUB/dc-out"
run_check
[ "$RC" -eq 0 ] || fail "duplicate digests exited $RC"
[ "$(grep -c . "$STUB/gh.log")" -eq 2 ] || fail 'a duplicate digest was verified twice'

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
