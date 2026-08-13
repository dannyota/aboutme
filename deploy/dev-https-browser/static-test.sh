#!/usr/bin/env bash
set -Eeuo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
readonly ROOT=$PWD
readonly SOURCE=$ROOT/deploy/dev-https-browser
readonly IMAGE=aboutme-dev-https-browser:static-test
readonly IMAGE_ID=sha256:1111111111111111111111111111111111111111111111111111111111111111
readonly BASE='mcr.microsoft.com/playwright:v1.62.1-noble@sha256:c091b21d9fae78c76e85cd4356431e9b018402f172a214fc7d7a5e9a7e29d8ac'

fail() {
  printf 'dev-https-browser static test: %s\n' "$*" >&2
  exit 1
}

for file in Dockerfile package.json package-lock.json playwright.config.ts auth.spec.ts network-policy.ts run.sh; do
  [ -f "$SOURCE/$file" ] || fail "missing $file"
done

readonly WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-dev-https-browser.XXXXXX")
cleanup() {
  rm -rf -- "$WORK"
}
trap cleanup EXIT

readonly CONTEXT=$WORK/context
readonly FAKE_BIN=$WORK/bin
readonly CALL_LOG=$WORK/podman.calls
readonly IMAGE_META=$WORK/image.meta
readonly INPUT=$WORK/input
readonly EVIDENCE=$WORK/evidence
install -d -m 0700 "$CONTEXT" "$FAKE_BIN" "$INPUT" "$EVIDENCE"
for file in Dockerfile package.json package-lock.json playwright.config.ts auth.spec.ts network-policy.ts run.sh; do
  cp -- "$SOURCE/$file" "$CONTEXT/$file"
done
printf '%s\n' '-----BEGIN CERTIFICATE-----' 'static-test-only' \
  '-----END CERTIFICATE-----' >"$INPUT/caddy-root.crt"
chmod 0600 "$INPUT/caddy-root.crt"

cat >"$FAKE_BIN/podman" <<'FAKE_PODMAN'
#!/usr/bin/env bash
set -Eeuo pipefail
: "${FAKE_PODMAN_LOG:?}"
: "${FAKE_IMAGE_META:?}"
{
  printf 'CALL\0'
  printf '%s\0' "$@"
} >>"$FAKE_PODMAN_LOG"

case ${1-} in
build)
  context=${!#}
  [ -d "$context" ]
  [ "$context" != "$FAKE_SOURCE_CONTEXT" ]
  grep -Fqx "FROM $FAKE_EXPECTED_BASE" "$context/Dockerfile"
  grep -Fq 'libnss3-tools=2:3.98-1ubuntu0.2' "$context/Dockerfile"
  grep -Fqx 'USER pwuser' "$context/Dockerfile"
  grep -Fqx 'ENTRYPOINT ["/opt/aboutme-auth/run.sh", "--inside"]' \
    "$context/Dockerfile"
  grep -Fq 'io.aboutme.dev-https-browser.contract="1"' "$context/Dockerfile"
  grep -Fq "io.aboutme.dev-https-browser.base=\"$FAKE_EXPECTED_BASE\"" \
    "$context/Dockerfile"
  grep -Fq 'io.aboutme.dev-https-browser.playwright="1.62.1"' \
    "$context/Dockerfile"
  grep -Fq 'io.aboutme.dev-https-browser.libnss3-tools="2:3.98-1ubuntu0.2"' \
    "$context/Dockerfile"
  grep -Fq '"@playwright/test": "1.62.1"' "$context/package.json"
  grep -Fq '"@playwright/test": "1.62.1"' "$context/package-lock.json"
  grep -Fq 'network-policy.ts' "$context/Dockerfile"
  printf '%s\n' built >"$FAKE_IMAGE_META"
  ;;
image)
  [ "${2-}" = inspect ]
  case ${FAKE_INSPECT_MODE:-good} in
  fail) exit 125 ;;
  mismatch-id)
    image_id=2222222222222222222222222222222222222222222222222222222222222222
    ;;
  *) image_id=${FAKE_EXPECTED_IMAGE_ID#sha256:} ;;
  esac
  contract=1
  base=$FAKE_EXPECTED_BASE
  playwright=1.62.1
  nss='2:3.98-1ubuntu0.2'
  case ${FAKE_INSPECT_MODE:-good} in
  missing-label) contract='' ;;
  mismatched-label) playwright=1.62.0 ;;
  wrong-user) image_user=root ;;
  wrong-entrypoint) entrypoint='["/bin/sh"]' ;;
  esac
  printf '%s|%s|%s|%s|%s|%s|%s\n' \
    "$image_id" "${image_user:-pwuser}" \
    "${entrypoint:-[\"/opt/aboutme-auth/run.sh\",\"--inside\"]}" \
    "$contract" "$base" "$playwright" "$nss"
  ;;
run)
  [ -s "$FAKE_IMAGE_META" ]
  [ "${!#}" = "$FAKE_EXPECTED_IMAGE_ID" ]
  printf '%s\n' "$*" | grep -Eq '(^| )--pull=never( |$)'
  ;;
*)
  exit 64
  ;;
esac
FAKE_PODMAN
chmod 0700 "$FAKE_BIN/podman"

export FAKE_PODMAN_LOG=$CALL_LOG
export FAKE_IMAGE_META=$IMAGE_META
export FAKE_SOURCE_CONTEXT=$SOURCE
export FAKE_EXPECTED_BASE=$BASE
export FAKE_EXPECTED_IMAGE_ID=$IMAGE_ID
export PATH=$FAKE_BIN:$PATH

podman build --tag "$IMAGE" "$CONTEXT"

assert_runner_rejected() {
  local name=$1 image=$2 mode=$3 expected=$4
  local case_log=$WORK/$name.calls case_evidence=$WORK/$name-evidence
  local output status=0
  install -d -m 0700 "$case_evidence"
  : >"$case_log"
  if output=$(FAKE_PODMAN_LOG=$case_log FAKE_INSPECT_MODE=$mode \
    "$CONTEXT/run.sh" "$image" "$INPUT" "$case_evidence" 2>&1); then
    status=0
  else
    status=$?
  fi
  [ "$status" -ne 0 ] || fail "$name was accepted"
  grep -Fq "$expected" <<<"$output" || fail "$name returned the wrong diagnostic"
  if tr '\0' '\n' <"$case_log" | grep -Fxq run; then
    fail "$name reached podman run"
  fi
}

assert_runner_rejected arbitrary-image-tag aboutme-dev-https-browser:latest good \
  'image must be an immutable sha256 ID'
assert_runner_rejected missing-image "$IMAGE_ID" fail \
  'cannot inspect the local browser image'
assert_runner_rejected mismatched-image "$IMAGE_ID" mismatch-id \
  'inspected browser image ID does not match'
assert_runner_rejected missing-image-label "$IMAGE_ID" missing-label \
  'browser image contract mismatch'
assert_runner_rejected mismatched-image-label "$IMAGE_ID" mismatched-label \
  'browser image contract mismatch'
assert_runner_rejected wrong-image-user "$IMAGE_ID" wrong-user \
  'browser image contract mismatch'
assert_runner_rejected wrong-image-entrypoint "$IMAGE_ID" wrong-entrypoint \
  'browser image contract mismatch'

: >"$CALL_LOG"
FAKE_INSPECT_MODE=good "$CONTEXT/run.sh" "$IMAGE_ID" "$INPUT" "$EVIDENCE"

readonly READABLE_LOG=$WORK/podman.calls.txt
tr '\0' '\n' <"$CALL_LOG" >"$READABLE_LOG"
[ "$(grep -c '^CALL$' "$READABLE_LOG")" -eq 2 ] ||
  fail 'expected inspect and run Podman calls'
grep -Fxq 'inspect' "$READABLE_LOG" || fail 'image inspection was not executed'
grep -Fxq 'run' "$READABLE_LOG" || fail 'runtime boundary was not executed'
grep -Fxq -- '--pull=never' "$READABLE_LOG" || fail 'run can pull an image'
grep -Fxq -- "$IMAGE_ID" "$READABLE_LOG" || fail 'run did not use the verified image ID'
readonly FIRST_RUNTIME_CALL=$(awk '/^CALL$/{getline; print; exit}' "$READABLE_LOG")
readonly SECOND_RUNTIME_CALL=$(awk '/^CALL$/{n++; if (n == 2) {getline; print; exit}}' \
  "$READABLE_LOG")
[ "$FIRST_RUNTIME_CALL" = image ] || fail 'image inspection did not precede run'
[ "$SECOND_RUNTIME_CALL" = run ] || fail 'runtime was not the second call'
grep -Fxq -- '--network=host' "$READABLE_LOG" || fail 'host network is missing'
grep -Fxq -- '--read-only' "$READABLE_LOG" || fail 'read-only root is missing'
grep -Fxq -- '--userns=keep-id' "$READABLE_LOG" || fail 'keep-id user namespace is missing'
grep -Exq -- '--user=[1-9][0-9]*:[1-9][0-9]*' "$READABLE_LOG" ||
  fail 'runtime user is not explicitly non-root'
grep -Fxq -- '--security-opt=no-new-privileges' "$READABLE_LOG" ||
  fail 'no-new-privileges is missing'
grep -Fxq -- '--cap-drop=all' "$READABLE_LOG" || fail 'capability drop is missing'
grep -Fxq -- '--cap-add=SYS_CHROOT' "$READABLE_LOG" ||
  fail 'Chromium sandbox capability is missing'
[ "$(grep -Ec '^--cap-add=' "$READABLE_LOG")" -eq 1 ] ||
  fail 'runtime has an extra added capability'
grep -Fxq -- "--mount=type=bind,src=$INPUT,dst=/uat-input,ro=true" "$READABLE_LOG" ||
  fail 'closed CA input mount is missing'
grep -Fxq -- "--mount=type=bind,src=$EVIDENCE,dst=/evidence,rw=true" "$READABLE_LOG" ||
  fail 'closed evidence mount is missing'
[ "$(grep -Ec '^--mount=type=bind,.*dst=/' "$READABLE_LOG")" -eq 2 ] ||
  fail 'runtime has an extra bind mount'
if grep -Eq '(/var/run/docker\.sock|/run/podman/podman\.sock|dst=/home|dst=/workspace|dst=/repo)' \
  "$READABLE_LOG"; then
  fail 'runtime exposes a forbidden host path or socket'
fi

for source in "$SOURCE"/*.sh "$SOURCE"/*.ts "$SOURCE"/Dockerfile; do
  if grep -nE '[[:blank:]]+$' "$source"; then
    fail "trailing whitespace in ${source#$ROOT/}"
  fi
done
if rg -n --glob '!static-test.sh' \
  -- '(ignore-certificate|ignoreHTTPSErrors|no-sandbox|disable-setuid-sandbox|update-snapshot|updateSnapshots: .(all|changed)|trace: .(on|retain)|video: .(on|retain))' \
  "$SOURCE"; then
  fail 'TLS, sandbox, update, trace, or video bypass found'
fi
if rg -n --glob '!static-test.sh' \
  -- '(not-a-secret-local-google|uat-google-access-token|BEGIN (RSA )?PRIVATE KEY|eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)' \
  "$SOURCE"; then
  fail 'secret-like literal found'
fi

grep -Fq 'certutil -N --empty-password' "$SOURCE/run.sh" ||
  fail 'empty NSS database creation is missing'
grep -Fq 'certutil -A' "$SOURCE/run.sh" || fail 'CA import is missing'
grep -Fq 'certutil -L' "$SOURCE/run.sh" || fail 'CA verification is missing'
grep -Fq '/uat-input/caddy-root.crt' "$SOURCE/run.sh" ||
  fail 'runner does not use the closed CA path'
grep -Fq 'chromiumSandbox: true' "$SOURCE/playwright.config.ts" ||
  fail 'Chromium sandbox is not enabled'

node --input-type=module - "$SOURCE/network-policy.ts" <<'NETWORK_POLICY_TEST'
const policyPath = process.argv[2];
const {
  ALLOWED_ORIGIN,
  isAllowedHTTPURL,
  isAllowedWebSocketURL,
  isExpectedNegativeHTTPConsole,
} =
  await import(`file://${policyPath}`);
const cases = [
  [ALLOWED_ORIGIN, 'https://localhost:20443'],
  [isAllowedHTTPURL('https://localhost:20443/login'), true],
  [isAllowedHTTPURL('https://localhost:20443/path?value=1#part'), true],
  [isAllowedHTTPURL('http://localhost:20443/login'), false],
  [isAllowedHTTPURL('https://localhost:20444/login'), false],
  [isAllowedHTTPURL('https://127.0.0.1:20443/login'), false],
  [isAllowedHTTPURL('https://user:pass@localhost:20443/login'), false],
  [isAllowedHTTPURL('not a URL'), false],
  [isAllowedWebSocketURL('wss://localhost:20443/socket'), true],
  [isAllowedWebSocketURL('ws://localhost:20443/socket'), false],
  [isAllowedWebSocketURL('wss://localhost:20444/socket'), false],
  [isAllowedWebSocketURL('wss://127.0.0.1:20443/socket'), false],
  [isAllowedWebSocketURL('wss://user:pass@localhost:20443/socket'), false],
  [isExpectedNegativeHTTPConsole(
    'Failed to load resource: the server responded with a status of 403 ()',
    'https://localhost:20443/api/v1/auth/google/start?purpose=reauth',
  ), true],
  [isExpectedNegativeHTTPConsole(
    'Failed to load resource: the server responded with a status of 401 ()',
    'https://localhost:20443/api/v1/me',
  ), true],
  [isExpectedNegativeHTTPConsole(
    'Failed to load resource: the server responded with a status of 500 ()',
    'https://localhost:20443/api/v1/me',
  ), false],
  [isExpectedNegativeHTTPConsole(
    'Failed to load resource: the server responded with a status of 401 ()',
    'https://localhost:20443/api/v1/me?unexpected=1',
  ), false],
  [isExpectedNegativeHTTPConsole(
    'Failed to load resource: the server responded with a status of 403 ()',
    'https://example.invalid/api/v1/auth/google/start?purpose=reauth',
  ), false],
];
for (const [actual, expected] of cases) {
  if (actual !== expected) process.exit(1);
}
NETWORK_POLICY_TEST

node --input-type=module - "$SOURCE/auth.spec.ts" <<'ROUTING_SCOPE_TEST'
import { readFile } from 'node:fs/promises';

const source = await readFile(process.argv[2], 'utf8');
const httpRoute = source.indexOf("await context.route('**/*'");
const websocketRoute = source.indexOf("await context.routeWebSocket('**/*'");
const firstNavigation = source.indexOf("await page.goto('/login')");
if (httpRoute === -1 || websocketRoute === -1 || firstNavigation === -1) process.exit(1);
if (httpRoute > firstNavigation || websocketRoute > firstNavigation) process.exit(1);
if (source.includes("await page.route('**/*'")) process.exit(1);
if (!source.includes('isAllowedHTTPURL(route.request().url())')) process.exit(1);
if (!source.includes('isAllowedWebSocketURL(webSocket.url())')) process.exit(1);
if (!source.includes("await webSocket.close({ code: 1008, reason: 'blocked' })")) {
  process.exit(1);
}
const attachFixture = source.indexOf('attachPageDiagnostics(page);');
const attachFuture = source.indexOf("context.on('page', attachPageDiagnostics);");
if (attachFixture === -1 || attachFuture === -1) process.exit(1);
if (attachFixture > firstNavigation || attachFuture > firstNavigation) process.exit(1);
ROUTING_SCOPE_TEST

ln -s "$ROOT/apps/web/node_modules" "$CONTEXT/node_modules"
readonly LIST_OUTPUT=$("$ROOT/apps/web/node_modules/.bin/playwright" test --list \
  --config "$CONTEXT/playwright.config.ts")
grep -Fq 'proves trusted local Google authentication and CSRF boundaries' \
  <<<"$LIST_OUTPUT" || fail 'Playwright could not compile and list the auth proof'

readonly INSIDE_ROOT=$WORK/inside
readonly INSIDE_INPUT=$INSIDE_ROOT/uat-input
readonly INSIDE_EVIDENCE=$INSIDE_ROOT/evidence
readonly INSIDE_TMP=$INSIDE_ROOT/tmp
readonly INSIDE_APP=$INSIDE_ROOT/opt/aboutme-auth
readonly INSIDE_BIN=$INSIDE_ROOT/bin
readonly INSIDE_RUN=$INSIDE_ROOT/run.sh
readonly CERT_LOG=$INSIDE_ROOT/certutil.calls
readonly BROWSER_LOG=$INSIDE_ROOT/browser.calls
install -d -m 0700 "$INSIDE_INPUT" "$INSIDE_EVIDENCE" "$INSIDE_TMP" \
  "$INSIDE_APP/node_modules/.bin" "$INSIDE_BIN"
cp -- "$INPUT/caddy-root.crt" "$INSIDE_INPUT/caddy-root.crt"
sed \
  -e "s#/uat-input#$INSIDE_INPUT#g" \
  -e "s#/evidence#$INSIDE_EVIDENCE#g" \
  -e "s#/tmp/home#$INSIDE_TMP/home#g" \
  -e "s#/tmp/playwright-auth.log#$INSIDE_TMP/playwright-auth.log#g" \
  -e "s#/opt/aboutme-auth#$INSIDE_APP#g" \
  "$SOURCE/run.sh" >"$INSIDE_RUN"
chmod 0700 "$INSIDE_RUN"

cat >"$INSIDE_BIN/findmnt" <<'FAKE_FINDMNT'
#!/usr/bin/env bash
set -Eeuo pipefail
target=${!#}
field=
for ((i=1; i<=$#; i++)); do
  if [ "${!i}" = -o ]; then
    j=$((i + 1))
    field=${!j}
  fi
done
case "$target:$field" in
/:TARGET) printf '%s\n' / ;;
/:OPTIONS) printf '%s\n' "${FAKE_ROOT_OPTIONS:-ro,nosuid,nodev}" ;;
"$FAKE_INSIDE_INPUT":TARGET) printf '%s\n' "${FAKE_INPUT_TARGET:-$FAKE_INSIDE_INPUT}" ;;
"$FAKE_INSIDE_INPUT":OPTIONS) printf '%s\n' "${FAKE_INPUT_OPTIONS:-ro,nosuid,nodev}" ;;
"$FAKE_INSIDE_EVIDENCE":TARGET) printf '%s\n' "${FAKE_EVIDENCE_TARGET:-$FAKE_INSIDE_EVIDENCE}" ;;
"$FAKE_INSIDE_EVIDENCE":OPTIONS) printf '%s\n' "${FAKE_EVIDENCE_OPTIONS:-rw,nosuid,nodev}" ;;
*) exit 1 ;;
esac
FAKE_FINDMNT

cat >"$INSIDE_BIN/certutil" <<'FAKE_CERTUTIL'
#!/usr/bin/env bash
set -Eeuo pipefail
printf '%s\0' "$@" >>"$FAKE_CERT_LOG"
printf '\n' >>"$FAKE_CERT_LOG"
case " $* " in
*' -N '*) operation=N ;;
*' -A '*) operation=A ;;
*' -L '*) operation=L ;;
*) exit 64 ;;
esac
[ "${FAKE_CERTUTIL_FAIL:-}" != "$operation" ] || exit 1
case $operation in
N)
  database=
  while [ "$#" -gt 0 ]; do
    if [ "$1" = -d ]; then shift; database=${1#sql:}; break; fi
    shift
  done
  [ -n "$database" ]
  printf '%s\n' initialized >"$database/cert9.db"
  ;;
A)
  [ -f "$FAKE_INSIDE_INPUT/caddy-root.crt" ]
  printf '%s\n' imported >"$FAKE_INSIDE_TMP/imported-ca"
  ;;
L)
  [ -f "$FAKE_INSIDE_TMP/imported-ca" ]
  ;;
esac
FAKE_CERTUTIL

cat >"$INSIDE_APP/node_modules/.bin/playwright" <<'FAKE_PLAYWRIGHT'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'HOME=%s\nARGV=' "$HOME" >>"$FAKE_BROWSER_LOG"
printf '%q ' "$@" >>"$FAKE_BROWSER_LOG"
printf '\n' >>"$FAKE_BROWSER_LOG"
case ${FAKE_BROWSER_MODE:-good} in
fail)
  printf '%s\n' 'browser-secret-must-not-escape'
  exit 1
  ;;
malformed) printf '%s\n' '{"wrong":true}' >"$FAKE_INSIDE_EVIDENCE/auth-proof.json" ;;
oversized)
  head -c 5000 /dev/zero | tr '\0' x >"$FAKE_INSIDE_EVIDENCE/auth-proof.json"
  ;;
good)
  cat >"$FAKE_INSIDE_EVIDENCE/auth-proof.json" <<'JSON'
{
  "errors": {"certificate": 0, "console": 0, "externalRequest": 0, "page": 0},
  "origin": "https://localhost:20443",
  "scenario": "google-authentication",
  "schemaVersion": 1,
  "steps": {"1": true, "2": true, "3": true, "4": true, "5": true, "6": true, "7": true, "8": true, "9": true, "10": true}
}
JSON
  ;;
*) exit 64 ;;
esac
chmod 0600 "$FAKE_INSIDE_EVIDENCE/auth-proof.json"
FAKE_PLAYWRIGHT
chmod 0700 "$INSIDE_BIN/findmnt" "$INSIDE_BIN/certutil" \
  "$INSIDE_APP/node_modules/.bin/playwright"

export FAKE_INSIDE_INPUT=$INSIDE_INPUT
export FAKE_INSIDE_EVIDENCE=$INSIDE_EVIDENCE
export FAKE_INSIDE_TMP=$INSIDE_TMP
export FAKE_CERT_LOG=$CERT_LOG
export FAKE_BROWSER_LOG=$BROWSER_LOG

reset_inside() {
  rm -rf -- "$INSIDE_EVIDENCE" "$INSIDE_TMP/home" \
    "$INSIDE_TMP/playwright-auth.log" "$INSIDE_TMP/imported-ca"
  install -d -m 0700 "$INSIDE_EVIDENCE"
  : >"$CERT_LOG"
  : >"$BROWSER_LOG"
}

run_inside() {
  PATH="$INSIDE_BIN:$PATH" "$INSIDE_RUN" --inside
}

assert_inside_rejected() {
  local name=$1 expected=$2 output status=0
  shift 2
  if output=$(env PATH="$INSIDE_BIN:$PATH" "$@" "$INSIDE_RUN" --inside 2>&1); then
    status=0
  else
    status=$?
  fi
  [ "$status" -ne 0 ] || fail "$name was accepted"
  grep -Fq "$expected" <<<"$output" || fail "$name returned the wrong diagnostic"
  printf '%s' "$output"
}

reset_inside
FAKE_ROOT_OPTIONS=rw FAKE_BROWSER_MODE=good \
  assert_inside_rejected root-writable 'root filesystem is not read-only' >/dev/null
[ ! -s "$BROWSER_LOG" ] || fail 'writable root reached the browser'

reset_inside
FAKE_INPUT_OPTIONS=rw FAKE_BROWSER_MODE=good \
  assert_inside_rejected input-writable 'CA input is not read-only' >/dev/null
[ ! -s "$BROWSER_LOG" ] || fail 'wrong input mount mode reached the browser'

reset_inside
FAKE_EVIDENCE_OPTIONS=ro FAKE_BROWSER_MODE=good \
  assert_inside_rejected evidence-read-only \
  'evidence output is not writable' >/dev/null
[ ! -s "$BROWSER_LOG" ] || fail 'wrong evidence mount mode reached the browser'

reset_inside
FAKE_EVIDENCE_TARGET=$INSIDE_INPUT FAKE_BROWSER_MODE=good \
  assert_inside_rejected wrong-topology \
  'evidence output is not a dedicated mount' >/dev/null
[ ! -s "$BROWSER_LOG" ] || fail 'wrong mount topology reached the browser'

reset_inside
printf '%s\n' extra >"$INSIDE_INPUT/extra"
assert_inside_rejected extra-input 'CA input must contain one root' \
  FAKE_BROWSER_MODE=good >/dev/null
rm -- "$INSIDE_INPUT/extra"
[ ! -s "$BROWSER_LOG" ] || fail 'extra input reached the browser'

reset_inside
printf '%s\n' stale >"$INSIDE_EVIDENCE/stale"
assert_inside_rejected stale-evidence 'evidence output must start empty' \
  FAKE_BROWSER_MODE=good >/dev/null
[ ! -s "$BROWSER_LOG" ] || fail 'stale evidence reached the browser'

reset_inside
assert_inside_rejected certutil-init-failure \
  'cannot initialize the isolated NSS database' FAKE_CERTUTIL_FAIL=N \
  FAKE_BROWSER_MODE=good >/dev/null
[ ! -s "$BROWSER_LOG" ] || fail 'NSS initialization failure reached the browser'

reset_inside
assert_inside_rejected certutil-import-failure \
  'cannot import the Caddy root' FAKE_CERTUTIL_FAIL=A \
  FAKE_BROWSER_MODE=good >/dev/null
[ ! -s "$BROWSER_LOG" ] || fail 'CA import failure reached the browser'

reset_inside
assert_inside_rejected certutil-list-failure \
  'cannot verify the imported Caddy root' FAKE_CERTUTIL_FAIL=L \
  FAKE_BROWSER_MODE=good >/dev/null
[ ! -s "$BROWSER_LOG" ] || fail 'CA verification failure reached the browser'

reset_inside
readonly BROWSER_FAILURE=$(assert_inside_rejected browser-failure \
  'authentication proof failed; volatile browser output was withheld' \
  FAKE_BROWSER_MODE=fail)
if grep -Fq 'browser-secret-must-not-escape' <<<"$BROWSER_FAILURE"; then
  fail 'browser failure leaked volatile output'
fi

reset_inside
assert_inside_rejected malformed-evidence \
  'browser evidence has invalid schema' FAKE_BROWSER_MODE=malformed >/dev/null

reset_inside
assert_inside_rejected oversized-evidence \
  'browser evidence exceeds its bound' FAKE_BROWSER_MODE=oversized >/dev/null

reset_inside
readonly INSIDE_OUTPUT=$(FAKE_BROWSER_MODE=good run_inside)
grep -Fq 'dev-https-browser authentication proof: PASS' <<<"$INSIDE_OUTPUT" ||
  fail 'inside-container success did not complete'
grep -Fq "HOME=$INSIDE_TMP/home" "$BROWSER_LOG" ||
  fail 'browser did not use the isolated HOME'
grep -Fq 'ARGV=test --config playwright.config.ts auth.spec.ts' "$BROWSER_LOG" ||
  fail 'focused Playwright invocation drifted'
[ -f "$INSIDE_TMP/home/.pki/nssdb/cert9.db" ] ||
  fail 'inside flow did not initialize the empty NSS database'
tr '\0' '\n' <"$CERT_LOG" >"$INSIDE_ROOT/certutil.calls.txt"
grep -Fxq -- '-N' "$INSIDE_ROOT/certutil.calls.txt" || fail 'certutil -N did not run'
grep -Fxq -- '-A' "$INSIDE_ROOT/certutil.calls.txt" || fail 'certutil -A did not run'
grep -Fxq -- '-L' "$INSIDE_ROOT/certutil.calls.txt" || fail 'certutil -L did not run'

printf '%s\n' 'dev-https-browser static tests: PASS'
