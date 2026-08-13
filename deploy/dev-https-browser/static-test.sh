#!/usr/bin/env bash
set -Eeuo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
readonly ROOT=$PWD
readonly SOURCE=$ROOT/deploy/dev-https-browser
readonly IMAGE=aboutme-dev-https-browser:static-test
readonly BASE='mcr.microsoft.com/playwright:v1.62.1-noble@sha256:c091b21d9fae78c76e85cd4356431e9b018402f172a214fc7d7a5e9a7e29d8ac'

fail() {
  printf 'dev-https-browser static test: %s\n' "$*" >&2
  exit 1
}

for file in Dockerfile package.json package-lock.json playwright.config.ts auth.spec.ts run.sh; do
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
for file in Dockerfile package.json package-lock.json playwright.config.ts auth.spec.ts run.sh; do
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
  grep -Fq '"@playwright/test": "1.62.1"' "$context/package.json"
  grep -Fq '"@playwright/test": "1.62.1"' "$context/package-lock.json"
  printf '%s\n' \
    'base=mcr.microsoft.com/playwright:v1.62.1-noble@sha256:c091b21d9fae78c76e85cd4356431e9b018402f172a214fc7d7a5e9a7e29d8ac' \
    'user=pwuser' \
    'entrypoint=/opt/aboutme-auth/run.sh --inside' >"$FAKE_IMAGE_META"
  ;;
image)
  [ "${2-}" = inspect ]
  [ "${3-}" = aboutme-dev-https-browser:static-test ]
  cat "$FAKE_IMAGE_META"
  ;;
run)
  [ -s "$FAKE_IMAGE_META" ]
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
export PATH=$FAKE_BIN:$PATH

podman build --tag "$IMAGE" "$CONTEXT"
readonly INSPECT=$(podman image inspect "$IMAGE")
grep -Fqx "base=$BASE" <<<"$INSPECT" || fail 'wrong inspected base image'
grep -Fqx 'user=pwuser' <<<"$INSPECT" || fail 'image user is not pwuser'
grep -Fqx 'entrypoint=/opt/aboutme-auth/run.sh --inside' <<<"$INSPECT" ||
  fail 'wrong image entrypoint'

"$CONTEXT/run.sh" "$IMAGE" "$INPUT" "$EVIDENCE"

readonly READABLE_LOG=$WORK/podman.calls.txt
tr '\0' '\n' <"$CALL_LOG" >"$READABLE_LOG"
[ "$(grep -c '^CALL$' "$READABLE_LOG")" -eq 3 ] ||
  fail 'expected build, inspect, and run Podman calls'
grep -Fxq 'build' "$READABLE_LOG" || fail 'image build was not executed'
grep -Fxq 'run' "$READABLE_LOG" || fail 'runtime boundary was not executed'
grep -Fxq -- '--network=host' "$READABLE_LOG" || fail 'host network is missing'
grep -Fxq -- '--read-only' "$READABLE_LOG" || fail 'read-only root is missing'
grep -Fxq -- '--userns=keep-id' "$READABLE_LOG" || fail 'keep-id user namespace is missing'
grep -Exq -- '--user=[1-9][0-9]*:[1-9][0-9]*' "$READABLE_LOG" ||
  fail 'runtime user is not explicitly non-root'
grep -Fxq -- '--security-opt=no-new-privileges' "$READABLE_LOG" ||
  fail 'no-new-privileges is missing'
grep -Fxq -- '--cap-drop=all' "$READABLE_LOG" || fail 'capability drop is missing'
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
grep -Fq "const ORIGIN = 'https://localhost:20443'" "$SOURCE/auth.spec.ts" ||
  fail 'browser origin is not fixed'
grep -Fq "new URL(route.request().url()).origin === ORIGIN" "$SOURCE/auth.spec.ts" ||
  fail 'browser request-origin block is missing'

printf '%s\n' 'dev-https-browser static tests: PASS'
