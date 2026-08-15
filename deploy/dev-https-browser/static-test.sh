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

for file in Dockerfile package.json package-lock.json playwright.config.ts auth.spec.ts transport.spec.ts editor.spec.ts public.spec.ts editor-fixtures.ts network-policy.ts run.sh; do
  [ -f "$SOURCE/$file" ] || fail "missing $file"
done
for file in \
  packages/publicroots/public-roots.v4.json \
  packages/publicroots/app-build-sources.v1.json \
  packages/publicroots/renderer-build-sources.v1.json \
  deploy/caddy/public-roots.generated.caddy; do
  [ -f "$ROOT/$file" ] || fail "missing $file"
done
digest_output=$(node "$ROOT/scripts/generate-public-roots.mjs" --check) ||
  fail 'public-root generation check failed'
[[ $digest_output == *$'APP_BUILD_DIGEST=sha256:'* ]] ||
  fail 'application source-manifest digest is missing'
[[ $digest_output == *$'PUBLIC_RENDERER_BUILD_DIGEST=sha256:'* ]] ||
  fail 'renderer source-manifest digest is missing'
for file in \
  deploy/dev-https-browser/editor.spec.ts \
  deploy/dev-https-browser/public.spec.ts \
  deploy/dev-https-browser/editor-fixtures.ts; do
  grep -Eq "^[[:blank:]]*${file//./\\.}[[:blank:]]+\\\\$" "$ROOT/Makefile" ||
    fail "browser source hash does not include ${file##*/}"
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
for file in Dockerfile package.json package-lock.json playwright.config.ts auth.spec.ts transport.spec.ts editor.spec.ts public.spec.ts editor-fixtures.ts network-policy.ts run.sh; do
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
  grep -Fq '"@axe-core/playwright": "4.13.0"' "$context/package.json"
  grep -Fq '"@axe-core/playwright": "4.13.0"' "$context/package-lock.json"
  grep -Fq 'network-policy.ts' "$context/Dockerfile"
  grep -Fq 'transport.spec.ts' "$context/Dockerfile"
  grep -Fq 'editor.spec.ts' "$context/Dockerfile"
  grep -Fq 'public.spec.ts' "$context/Dockerfile"
  grep -Fq 'editor-fixtures.ts' "$context/Dockerfile"
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
  case ${!#} in
  "$FAKE_EXPECTED_IMAGE_ID") ;;
  transport | editor | public)
    previous_index=$(($# - 1))
    [ "${!previous_index}" = "$FAKE_EXPECTED_IMAGE_ID" ]
    ;;
  *) exit 64 ;;
  esac
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
image_line=$(grep -Fnx -- "$IMAGE_ID" "$READABLE_LOG" | tail -n 1 | cut -d: -f1)
[ -n "$image_line" ] || fail 'auth run lacks the verified image ID'
[ -z "$(sed -n "$((image_line + 1))p" "$READABLE_LOG")" ] ||
  fail 'three-argument auth run gained a mode argument'

readonly TRANSPORT_EVIDENCE=$WORK/transport-evidence
install -d -m 0700 "$TRANSPORT_EVIDENCE"
: >"$CALL_LOG"
FAKE_INSPECT_MODE=good "$CONTEXT/run.sh" \
  "$IMAGE_ID" "$INPUT" "$TRANSPORT_EVIDENCE" transport
tr '\0' '\n' <"$CALL_LOG" >"$READABLE_LOG"
grep -Fxq transport "$READABLE_LOG" || fail 'transport mode did not reach the image'
image_line=$(grep -Fnx -- "$IMAGE_ID" "$READABLE_LOG" | tail -n 1 | cut -d: -f1)
[ "$(sed -n "$((image_line + 1))p" "$READABLE_LOG")" = transport ] ||
  fail 'transport mode was not passed after the verified image ID'

readonly EDITOR_EVIDENCE=$WORK/editor-evidence
install -d -m 0700 "$EDITOR_EVIDENCE"
: >"$CALL_LOG"
FAKE_INSPECT_MODE=good "$CONTEXT/run.sh" \
  "$IMAGE_ID" "$INPUT" "$EDITOR_EVIDENCE" editor
tr '\0' '\n' <"$CALL_LOG" >"$READABLE_LOG"
grep -Fxq editor "$READABLE_LOG" || fail 'editor mode did not reach the image'
image_line=$(grep -Fnx -- "$IMAGE_ID" "$READABLE_LOG" | tail -n 1 | cut -d: -f1)
[ "$(sed -n "$((image_line + 1))p" "$READABLE_LOG")" = editor ] ||
  fail 'editor mode was not passed after the verified image ID'

readonly INVALID_MODE_EVIDENCE=$WORK/invalid-mode-evidence
install -d -m 0700 "$INVALID_MODE_EVIDENCE"
: >"$CALL_LOG"
if output=$(FAKE_INSPECT_MODE=good "$CONTEXT/run.sh" \
  "$IMAGE_ID" "$INPUT" "$INVALID_MODE_EVIDENCE" invalid 2>&1); then
  fail 'invalid host mode was accepted'
fi
grep -Fq 'mode must be auth, transport, editor, or public' <<<"$output" ||
  fail 'invalid host mode returned the wrong diagnostic'
[ ! -s "$CALL_LOG" ] || fail 'invalid host mode reached Podman'

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
grep -Fqx "const timeout = mode === 'editor' ? 120_000 : 30_000;" \
  "$SOURCE/playwright.config.ts" || fail 'editor timeout is not explicitly bounded'
grep -Fq '  timeout,' "$SOURCE/playwright.config.ts" ||
  fail 'Playwright does not use the bounded mode timeout'

node --input-type=module - "$SOURCE/network-policy.ts" <<'NETWORK_POLICY_TEST'
const policyPath = process.argv[2];
const {
  ALLOWED_ORIGIN,
  DIRECT_RENDER_ORIGIN,
  isAllowedHTTPURL,
  isAllowedWebSocketURL,
  isExpectedNegativeHTTPConsole,
  httpFailureStatus,
} =
  await import(`file://${policyPath}`);
const cases = [
  [ALLOWED_ORIGIN, 'https://localhost:20443'],
  [isAllowedHTTPURL('https://localhost:20443/login'), true],
  [isAllowedHTTPURL('https://localhost:20443/path?value=1#part'), true],
  [isAllowedHTTPURL(`${ALLOWED_ORIGIN}/internal-render`), true],
  [isAllowedHTTPURL(`${ALLOWED_ORIGIN}/internal-render/public`), true],
  [isAllowedHTTPURL('http://localhost:20443/login'), false],
  [isAllowedHTTPURL('https://localhost:20444/login'), false],
  [DIRECT_RENDER_ORIGIN, 'http://127.0.0.1:20440'],
  [isAllowedHTTPURL(`${DIRECT_RENDER_ORIGIN}/internal-render/public`), false],
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
  [httpFailureStatus(
    'Failed to load resource: the server responded with a status of 412 (Precondition Failed)',
  ), 412],
  [httpFailureStatus(
    'Failed to load resource: the server responded with a status of 500 ()',
  ), 500],
  [httpFailureStatus('Failed to fetch private data'), null],
];
for (const [actual, expected] of cases) {
  if (actual !== expected) process.exit(1);
}
NETWORK_POLICY_TEST

node --input-type=module - "$SOURCE/auth.spec.ts" "$SOURCE/transport.spec.ts" "$SOURCE/editor.spec.ts" <<'ROUTING_SCOPE_TEST'
import { readFile } from 'node:fs/promises';

for (const path of process.argv.slice(2, 4)) {
  const source = await readFile(path, 'utf8');
  const httpRoute = source.indexOf('await context.route(');
  const websocketRoute = source.indexOf('await context.routeWebSocket(');
  const firstNavigation = source.indexOf('await page.goto(');
  if (httpRoute === -1 || websocketRoute === -1 || firstNavigation === -1) process.exit(1);
  if (httpRoute > firstNavigation || websocketRoute > firstNavigation) process.exit(1);
  if (source.includes('await page.route(')) process.exit(1);
  if (!source.includes('isAllowedHTTPURL(')) process.exit(1);
  if (!source.includes('isAllowedWebSocketURL(webSocket.url())')) process.exit(1);
  if (!source.includes('await webSocket.close({ code: 1008')) {
    process.exit(1);
  }
  const attachFixture = source.indexOf('attachPageDiagnostics(page);');
  const attachFuture = source.indexOf('context.on(');
  if (attachFixture === -1 || attachFuture === -1) process.exit(1);
  if (attachFixture > firstNavigation || attachFuture > firstNavigation) process.exit(1);
}
const transport = await readFile(process.argv[3], 'utf8');
const networkHeaders = transport.indexOf('Network.requestWillBeSentExtraInfo');
const transportNavigation = transport.indexOf('await page.goto(');
if (networkHeaders === -1 || networkHeaders > transportNavigation) process.exit(1);
const editor = await readFile(process.argv[4], 'utf8');
const diagnosticsInstall = editor.indexOf('await installDiagnostics(context, page)');
const editorNavigation = editor.indexOf('await loginAsDevelopmentUser(page)');
if (diagnosticsInstall === -1 || editorNavigation === -1 || diagnosticsInstall > editorNavigation) {
  process.exit(1);
}
for (const required of [
  'await context.route(',
  'await context.routeWebSocket(',
  "await webSocket.close({ code: 1008",
  'isAllowedHTTPURL(',
  'isAllowedWebSocketURL(webSocket.url())',
  "context.on('serviceworker'",
  "openedPage.on('download'",
]) {
  if (!editor.includes(required)) process.exit(1);
}
ROUTING_SCOPE_TEST

ln -s "$SOURCE/node_modules" "$CONTEXT/node_modules"
readonly AUTH_LIST_OUTPUT=$(ABOUTME_BROWSER_MODE=auth \
  "$SOURCE/node_modules/.bin/playwright" test --list --config "$CONTEXT/playwright.config.ts")
grep -Fq 'proves trusted local Google authentication and CSRF boundaries' \
  <<<"$AUTH_LIST_OUTPUT" || fail 'Playwright could not compile and list the auth proof'
if grep -Fq 'proves authenticated transport preserves cache and precondition bytes' \
  <<<"$AUTH_LIST_OUTPUT"; then
  fail 'auth mode listed the transport proof'
fi
readonly TRANSPORT_LIST_OUTPUT=$(ABOUTME_BROWSER_MODE=transport \
  "$SOURCE/node_modules/.bin/playwright" test --list --config "$CONTEXT/playwright.config.ts")
grep -Fq 'proves authenticated transport preserves cache and precondition bytes' \
  <<<"$TRANSPORT_LIST_OUTPUT" || fail 'Playwright could not compile and list the transport proof'
if grep -Fq 'proves trusted local Google authentication and CSRF boundaries' \
  <<<"$TRANSPORT_LIST_OUTPUT"; then
  fail 'transport mode listed the auth proof'
fi
readonly EDITOR_LIST_OUTPUT=$(ABOUTME_BROWSER_MODE=editor \
  "$SOURCE/node_modules/.bin/playwright" test --list --config "$CONTEXT/playwright.config.ts")
grep -Fq 'proves authenticated editor behavior over trusted HTTPS' \
  <<<"$EDITOR_LIST_OUTPUT" || fail 'Playwright could not compile and list the editor proof'
if grep -Fq 'proves authenticated transport preserves cache and precondition bytes' \
  <<<"$EDITOR_LIST_OUTPUT"; then
  fail 'editor mode listed the transport proof'
fi

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
  -e "s#/tmp/playwright-uat.log#$INSIDE_TMP/playwright-uat.log#g" \
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
printf 'HOME=%s\nMODE=%s\nARGV=' "$HOME" "${ABOUTME_BROWSER_MODE:-}" \
  >>"$FAKE_BROWSER_LOG"
printf '%q ' "$@" >>"$FAKE_BROWSER_LOG"
printf '\n' >>"$FAKE_BROWSER_LOG"
case ${FAKE_BROWSER_MODE:-good} in
fail)
  printf '%s\n' 'browser-secret-must-not-escape'
  exit 1
  ;;
editor-fail)
  printf '%s\n' 'browser-secret-must-not-escape'
  printf '%s\n' 'editor-stage:entries-missing-select'
  printf '%s\n' 'editor-stage:photo-session'
  exit 1
  ;;
malformed)
  case " $* " in
  *' editor.spec.ts '*) evidence=editor-proof.json ;;
  *' transport.spec.ts '*) evidence=transport-proof.json ;;
  *) evidence=auth-proof.json ;;
  esac
  printf '%s\n' '{"wrong":true}' >"$FAKE_INSIDE_EVIDENCE/$evidence"
  ;;
oversized)
  case " $* " in
  *' editor.spec.ts '*) evidence=editor-proof.json ;;
  *' transport.spec.ts '*) evidence=transport-proof.json ;;
  *) evidence=auth-proof.json ;;
  esac
  head -c 9000 /dev/zero | tr '\0' x >"$FAKE_INSIDE_EVIDENCE/$evidence"
  ;;
good)
  case " $* " in
  *' editor.spec.ts '*)
    cat >"$FAKE_INSIDE_EVIDENCE/editor-proof.json" <<'JSON'
{
  "schemaVersion": 1,
  "scenario": "authenticated-editor",
  "origin": "https://localhost:20443",
  "errors": {"certificate": 0, "console": 0, "externalRequest": 0, "page": 0},
  "steps": {"auth": true, "cache": true, "etag": true, "ifMatch": true, "autosave": true, "conflict": true, "template": true, "photo": true, "session": true, "persistence": true, "accessibility": true, "teardown": true}
}
JSON
    ;;
  *' transport.spec.ts '*)
    cat >"$FAKE_INSIDE_EVIDENCE/transport-proof.json" <<'JSON'
{
  "errors": {"certificate": 0, "console": 0, "externalRequest": 0, "page": 0},
  "origin": "https://localhost:20443",
  "scenario": "authenticated-transport",
  "schemaVersion": 1,
  "steps": {"auth": true, "cache": true, "etag": true, "ifMatch": true, "teardown": true}
}
JSON
    ;;
  *)
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
  esac
  ;;
within-editor-bound)
  case " $* " in
  *' editor.spec.ts '*) ;;
  *) exit 64 ;;
  esac
  cat >"$FAKE_INSIDE_EVIDENCE/editor-proof.json" <<'JSON'
{"schemaVersion":1,"scenario":"authenticated-editor","origin":"https://localhost:20443","errors":{"certificate":0,"console":0,"externalRequest":0,"page":0},"steps":{"auth":true,"cache":true,"etag":true,"ifMatch":true,"autosave":true,"conflict":true,"template":true,"photo":true,"session":true,"persistence":true,"accessibility":true,"teardown":true}}
JSON
  while [ "$(stat -c %s "$FAKE_INSIDE_EVIDENCE/editor-proof.json")" -lt 5000 ]; do
    printf ' ' >>"$FAKE_INSIDE_EVIDENCE/editor-proof.json"
  done
  ;;
*) exit 64 ;;
esac
chmod 0600 "$FAKE_INSIDE_EVIDENCE"/*.json
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
    "$INSIDE_TMP/playwright-uat.log" "$INSIDE_TMP/imported-ca"
  install -d -m 0700 "$INSIDE_EVIDENCE"
  : >"$CERT_LOG"
  : >"$BROWSER_LOG"
}

run_inside() {
  if [ "$#" -eq 0 ]; then
    PATH="$INSIDE_BIN:$PATH" "$INSIDE_RUN" --inside
  else
    PATH="$INSIDE_BIN:$PATH" "$INSIDE_RUN" --inside "$1"
  fi
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
if output=$(FAKE_ROOT_OPTIONS=rw FAKE_BROWSER_MODE=good \
  PATH="$INSIDE_BIN:$PATH" "$INSIDE_RUN" --inside transport 2>&1); then
  fail 'transport accepted a writable root'
fi
grep -Fq 'root filesystem is not read-only' <<<"$output" ||
  fail 'transport writable-root diagnostic drifted'
[ ! -s "$BROWSER_LOG" ] || fail 'transport writable root reached the browser'

reset_inside
if output=$(FAKE_BROWSER_MODE=good PATH="$INSIDE_BIN:$PATH" \
  "$INSIDE_RUN" --inside invalid 2>&1); then
  fail 'invalid inside mode was accepted'
fi
grep -Fq 'mode must be auth, transport, editor, or public' <<<"$output" ||
  fail 'invalid inside mode returned the wrong diagnostic'
[ ! -s "$BROWSER_LOG" ] || fail 'invalid inside mode reached the browser'

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
if output=$(FAKE_BROWSER_MODE=editor-fail PATH="$INSIDE_BIN:$PATH" \
  "$INSIDE_RUN" --inside editor 2>&1); then
  fail 'editor browser failure was accepted'
fi
grep -Fxq 'dev-https-browser: editor-stage:photo-session' <<<"$output" ||
  fail 'editor failure did not expose its last bounded stage'
if grep -Fq 'browser-secret-must-not-escape' <<<"$output"; then
  fail 'editor failure leaked volatile output'
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
grep -Fxq 'MODE=auth' "$BROWSER_LOG" ||
  fail 'auth mode did not reach Playwright config'
[ -f "$INSIDE_TMP/home/.pki/nssdb/cert9.db" ] ||
  fail 'inside flow did not initialize the empty NSS database'
tr '\0' '\n' <"$CERT_LOG" >"$INSIDE_ROOT/certutil.calls.txt"
grep -Fxq -- '-N' "$INSIDE_ROOT/certutil.calls.txt" || fail 'certutil -N did not run'
grep -Fxq -- '-A' "$INSIDE_ROOT/certutil.calls.txt" || fail 'certutil -A did not run'
grep -Fxq -- '-L' "$INSIDE_ROOT/certutil.calls.txt" || fail 'certutil -L did not run'

reset_inside
readonly TRANSPORT_INSIDE_OUTPUT=$(FAKE_BROWSER_MODE=good run_inside transport)
grep -Fq 'dev-https-browser transport proof: PASS' <<<"$TRANSPORT_INSIDE_OUTPUT" ||
  fail 'inside-container transport success did not complete'
grep -Fq 'ARGV=test --config playwright.config.ts transport.spec.ts' "$BROWSER_LOG" ||
  fail 'focused transport invocation drifted'
grep -Fxq 'MODE=transport' "$BROWSER_LOG" ||
  fail 'transport mode did not reach Playwright config'
[ -f "$INSIDE_EVIDENCE/transport-proof.json" ] ||
  fail 'transport evidence filename drifted'

reset_inside
if output=$(FAKE_BROWSER_MODE=malformed PATH="$INSIDE_BIN:$PATH" \
  "$INSIDE_RUN" --inside transport 2>&1); then
  fail 'malformed transport evidence was accepted'
fi
grep -Fq 'browser evidence has invalid schema' <<<"$output" ||
  fail 'malformed transport evidence returned the wrong diagnostic'

reset_inside
if output=$(FAKE_BROWSER_MODE=oversized PATH="$INSIDE_BIN:$PATH" \
  "$INSIDE_RUN" --inside transport 2>&1); then
  fail 'oversized transport evidence was accepted'
fi
grep -Fq 'browser evidence exceeds its bound' <<<"$output" ||
  fail 'oversized transport evidence returned the wrong diagnostic'

reset_inside
readonly EDITOR_INSIDE_OUTPUT=$(FAKE_BROWSER_MODE=good run_inside editor)
grep -Fq 'dev-https-browser editor proof: PASS' <<<"$EDITOR_INSIDE_OUTPUT" ||
  fail 'inside-container editor success did not complete'
grep -Fq 'ARGV=test --config playwright.config.ts editor.spec.ts' "$BROWSER_LOG" ||
  fail 'focused editor invocation drifted'
grep -Fxq 'MODE=editor' "$BROWSER_LOG" ||
  fail 'editor mode did not reach Playwright config'
[ -f "$INSIDE_EVIDENCE/editor-proof.json" ] ||
  fail 'editor evidence filename drifted'
[ "$(stat -c %a "$INSIDE_EVIDENCE/editor-proof.json")" = 600 ] ||
  fail 'editor evidence mode drifted'

reset_inside
if output=$(FAKE_BROWSER_MODE=within-editor-bound PATH="$INSIDE_BIN:$PATH" \
  "$INSIDE_RUN" --inside editor 2>&1); then
  grep -Fq 'dev-https-browser editor proof: PASS' <<<"$output" ||
    fail 'within-bound editor evidence did not complete'
else
  fail 'editor evidence below 8 KiB was rejected'
fi

reset_inside
if output=$(FAKE_BROWSER_MODE=malformed PATH="$INSIDE_BIN:$PATH" \
  "$INSIDE_RUN" --inside editor 2>&1); then
  fail 'malformed editor evidence was accepted'
fi
grep -Fq 'browser evidence has invalid schema' <<<"$output" ||
  fail 'malformed editor evidence returned the wrong diagnostic'

reset_inside
if output=$(FAKE_BROWSER_MODE=oversized PATH="$INSIDE_BIN:$PATH" \
  "$INSIDE_RUN" --inside editor 2>&1); then
  fail 'oversized editor evidence was accepted'
fi
grep -Fq 'browser evidence exceeds its bound' <<<"$output" ||
  fail 'oversized editor evidence returned the wrong diagnostic'

printf '%s\n' 'dev-https-browser static tests: PASS'
