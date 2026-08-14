#!/usr/bin/env bash

# Author regression tests for fail-closed container discovery in Make targets.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-make-safety.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

REPO=$WORK/repo
BIN=$WORK/bin
CALLS=$WORK/calls
mkdir -p "$REPO" "$BIN"
cp "$ROOT/Makefile" "$REPO/Makefile"
: >"$REPO/.env"
: >"$CALLS"

cat >"$BIN/podman" <<'EOF'
#!/usr/bin/env bash
printf 'podman %s\n' "$*" >>"$CALL_LOG"
if [ "${1:-}" = ps ]; then
  if [ "${PODMAN_PS_MODE:-fail}" = compose-db ]; then
    printf '%s\n' 'aboutme-postgres-1|aboutme|postgres'
    exit 0
  fi
  exit 17
fi
exit 0
EOF
chmod +x "$BIN/podman"

assert_guarded() {
  local target=$1
  : >"$CALLS"
  if (
    cd "$REPO"
    PATH="$BIN:/usr/bin:/bin" CALL_LOG="$CALLS" \
      /usr/bin/make --no-print-directory "$target"
  ) >"$WORK/$target.out" 2>&1; then
    printf 'makefile-safety-test: make %s passed after podman ps failed\n' "$target" >&2
    exit 1
  fi
  if grep -Eq '^podman (compose|run)([[:space:]]|$)' "$CALLS"; then
    printf 'makefile-safety-test: make %s mutated containers after discovery failed\n' "$target" >&2
    exit 1
  fi
}

assert_guarded dev
assert_guarded test-db-up

assert_web_e2e_rejected() {
  local name=$1 target=$2 diagnostic=$3
  shift 3
  : >"$CALLS"
  if (
    cd "$REPO"
    env PATH="$BIN:/usr/bin:/bin" CALL_LOG="$CALLS" "$@" \
      /usr/bin/make --no-print-directory "$target"
  ) >"$WORK/$name.out" 2>&1; then
    printf 'makefile-safety-test: make %s accepted %s\n' "$target" "$name" >&2
    exit 1
  fi
  grep -Fq "$diagnostic" "$WORK/$name.out" || {
    printf 'makefile-safety-test: make %s lacked %s diagnostic\n' \
      "$target" "$name" >&2
    exit 1
  }
  if grep -Eq '^podman([[:space:]]|$)' "$CALLS"; then
    printf 'makefile-safety-test: make %s called Podman for %s\n' \
      "$target" "$name" >&2
    exit 1
  fi
}

assert_web_e2e_rejected invalid-run-id web-e2e \
  'WEB_E2E_RUN_ID must match [A-Za-z0-9_-]+' WEB_E2E_RUN_ID='../bad'
assert_web_e2e_rejected invalid-update-run-id web-e2e-update \
  'WEB_E2E_RUN_ID must match [A-Za-z0-9_-]+' WEB_E2E_RUN_ID='bad/value'
assert_web_e2e_rejected update-golden-present web-e2e \
  'UPDATE_GOLDEN must be absent' WEB_E2E_RUN_ID=valid UPDATE_GOLDEN=
assert_web_e2e_rejected playwright-update-present web-e2e \
  'PLAYWRIGHT_UPDATE_SNAPSHOTS must be absent' \
  WEB_E2E_RUN_ID=valid PLAYWRIGHT_UPDATE_SNAPSHOTS=

: >"$CALLS"
if (
  cd "$REPO"
  PATH="$BIN:/usr/bin:/bin" CALL_LOG="$CALLS" PODMAN_PS_MODE=compose-db \
    /usr/bin/make --no-print-directory test-db-up
) >"$WORK/compose-db.out" 2>&1; then
  printf 'makefile-safety-test: test-db-up passed beside Compose Postgres\n' >&2
  exit 1
fi
if grep -Eq '^podman run([[:space:]]|$)' "$CALLS"; then
  printf 'makefile-safety-test: test-db-up started a second Postgres\n' >&2
  exit 1
fi

printf 'Makefile container-discovery safety tests passed\n'

HTTPS_REPO=$WORK/https-repo
HTTPS_BIN=$WORK/https-bin
HTTPS_CALLS=$WORK/https.calls
HTTPS_ID=sha256:1111111111111111111111111111111111111111111111111111111111111111
HTTPS_BASE='mcr.microsoft.com/playwright:v1.62.1-noble@sha256:c091b21d9fae78c76e85cd4356431e9b018402f172a214fc7d7a5e9a7e29d8ac'
mkdir -p "$HTTPS_REPO/scripts" "$HTTPS_REPO/deploy/dev-https-browser" \
  "$HTTPS_REPO/.dev/native-https/input" "$HTTPS_BIN"
chmod 0700 "$HTTPS_REPO/.dev/native-https" \
  "$HTTPS_REPO/.dev/native-https/input"
cp "$ROOT/Makefile" "$HTTPS_REPO/Makefile"
cp "$ROOT/deploy/dev-https-browser/"{Dockerfile,package.json,package-lock.json,playwright.config.ts,auth.spec.ts,transport.spec.ts,network-policy.ts,run.sh} \
  "$HTTPS_REPO/deploy/dev-https-browser/"
printf '%s\n' 'static test root' > \
  "$HTTPS_REPO/.dev/native-https/input/caddy-root.crt"
chmod 0600 "$HTTPS_REPO/.dev/native-https/input/caddy-root.crt"
: >"$HTTPS_CALLS"

cat >"$HTTPS_REPO/scripts/dev-https.sh" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'lifecycle\0' >>"$HTTPS_CALL_LOG"
printf '%s\0' "$@" >>"$HTTPS_CALL_LOG"
printf '\n' >>"$HTTPS_CALL_LOG"
[ "${DEV_HTTPS_FAKE_FAIL:-}" != "${1-}" ]
EOF
chmod 0700 "$HTTPS_REPO/scripts/dev-https.sh"

cat >"$HTTPS_BIN/podman" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'podman\0' >>"$HTTPS_CALL_LOG"
printf '%s\0' "$@" >>"$HTTPS_CALL_LOG"
printf '\n' >>"$HTTPS_CALL_LOG"
case ${1-}:${2-} in
build:*)
  [ "${FAKE_PODMAN_BUILD_FAIL:-0}" != 1 ] || exit 125
  iidfile=
  while [ "$#" -gt 0 ]; do
    if [ "$1" = --iidfile ]; then shift; iidfile=${1-}; break; fi
    shift
  done
  [ -n "$iidfile" ]
  printf '%s\n' "$FAKE_HTTPS_IMAGE_ID" >"$iidfile"
  ;;
image:inspect)
  if [[ " $* " == *Config.Labels* ]]; then
    printf '%s|pwuser|["/opt/aboutme-auth/run.sh","--inside"]|1|%s|1.62.1|2:3.98-1ubuntu0.2\n' \
      "${FAKE_HTTPS_IMAGE_ID#sha256:}" "$FAKE_HTTPS_BASE"
  else
    printf '%s\n' "${FAKE_HTTPS_IMAGE_ID#sha256:}"
  fi
  ;;
run:*) ;;
*) exit 64 ;;
esac
EOF
chmod 0700 "$HTTPS_BIN/podman"

run_https_make() {
  local target=${!#}
  set -- "${@:1:$#-1}"
  (
    cd "$HTTPS_REPO"
    env PATH="$HTTPS_BIN:/usr/bin:/bin" HTTPS_CALL_LOG="$HTTPS_CALLS" \
      FAKE_HTTPS_IMAGE_ID="$HTTPS_ID" FAKE_HTTPS_BASE="$HTTPS_BASE" "$@" \
      /usr/bin/make --no-print-directory "$target"
  )
}

read_https_calls() {
  tr '\0' '\n' <"$HTTPS_CALLS"
}

assert_https_delegation() {
  local target=$1 expected=$2
  shift 2
  : >"$HTTPS_CALLS"
  run_https_make "$@" "$target" >"$WORK/$target.out" 2>&1 || {
    printf 'makefile-safety-test: make %s failed\n' "$target" >&2
    exit 1
  }
  mapfile -t actual < <(read_https_calls)
  [ "${actual[0]-}" = lifecycle ] && [ "${actual[1]-}" = "$expected" ] || {
    printf 'makefile-safety-test: make %s delegated incorrectly\n' "$target" >&2
    exit 1
  }
}

assert_https_delegation dev-https up
assert_https_delegation dev-https-down down
assert_https_delegation dev-https-status status
assert_https_delegation dev-https-logs logs ARGS='-f server'
mapfile -t log_call < <(read_https_calls)
[ "${log_call[2]-}" = -f ] && [ "${log_call[3]-}" = server ] || {
  printf 'makefile-safety-test: dev-https-logs lost ARGS\n' >&2
  exit 1
}

rm -f "$HTTPS_REPO/.dev/native-https/browser-image.manifest"
: >"$HTTPS_CALLS"
if run_https_make DEV_HTTPS_FAKE_FAIL=status dev-https-browser-image \
  >"$WORK/https-image-preflight.out" 2>&1; then
  printf 'makefile-safety-test: browser image passed failed status\n' >&2
  exit 1
fi
if read_https_calls | grep -Fxq podman; then
  printf 'makefile-safety-test: browser image mutated after failed status\n' >&2
  exit 1
fi
[ ! -e "$HTTPS_REPO/.dev/native-https/browser-image.manifest" ] || {
  printf 'makefile-safety-test: failed browser image left a manifest\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
run_https_make dev-https-browser-image >"$WORK/https-image.out" 2>&1 || {
  printf 'makefile-safety-test: browser image target failed\n' >&2
  exit 1
}
HTTPS_MANIFEST=$HTTPS_REPO/.dev/native-https/browser-image.manifest
[ -f "$HTTPS_MANIFEST" ] && [ ! -L "$HTTPS_MANIFEST" ] || {
  printf 'makefile-safety-test: browser image manifest is missing\n' >&2
  exit 1
}
[ "$(stat -c %a "$HTTPS_MANIFEST")" = 600 ] || {
  printf 'makefile-safety-test: browser image manifest mode drifted\n' >&2
  exit 1
}
grep -Fxq "image_id=$HTTPS_ID" "$HTTPS_MANIFEST" || {
  printf 'makefile-safety-test: manifest lacks immutable image ID\n' >&2
  exit 1
}
grep -Eq '^source_sha256=[0-9a-f]{64}$' "$HTTPS_MANIFEST" || {
  printf 'makefile-safety-test: manifest lacks source hash\n' >&2
  exit 1
}
read_https_calls | grep -Fxq -- '--iidfile' || {
  printf 'makefile-safety-test: image build lacks IID capture\n' >&2
  exit 1
}
read_https_calls | grep -Fxq -- '--tag' || {
  printf 'makefile-safety-test: image build lacks the fixed tag\n' >&2
  exit 1
}
read_https_calls | grep -Fxq -- 'localhost/aboutme-dev-https-browser:local' || {
  printf 'makefile-safety-test: image build tag drifted\n' >&2
  exit 1
}
read_https_calls | grep -Fxq -- 'deploy/dev-https-browser' || {
  printf 'makefile-safety-test: image build context drifted\n' >&2
  exit 1
}

cp "$HTTPS_REPO/deploy/dev-https-browser/transport.spec.ts" \
  "$WORK/transport.spec.ts.clean"
printf '%s\n' '// source-drift' >> \
  "$HTTPS_REPO/deploy/dev-https-browser/transport.spec.ts"
: >"$HTTPS_CALLS"
if run_https_make dev-https-auth-check >"$WORK/https-source-drift.out" 2>&1; then
  printf 'makefile-safety-test: transport source drift was accepted\n' >&2
  exit 1
fi
if read_https_calls | grep -Fxq podman; then
  printf 'makefile-safety-test: source drift reached the browser runtime\n' >&2
  exit 1
fi
cp "$WORK/transport.spec.ts.clean" \
  "$HTTPS_REPO/deploy/dev-https-browser/transport.spec.ts"
rm -rf "$HTTPS_REPO/.dev/native-https/evidence"

rm -f "$HTTPS_MANIFEST"
: >"$HTTPS_CALLS"
if run_https_make DEV_HTTPS_FAKE_FAIL=status dev-https-auth-check \
  >"$WORK/https-auth-preflight.out" 2>&1; then
  printf 'makefile-safety-test: auth check passed failed status\n' >&2
  exit 1
fi
if read_https_calls | grep -Fxq podman; then
  printf 'makefile-safety-test: auth check mutated after failed status\n' >&2
  exit 1
fi
[ ! -e "$HTTPS_REPO/.dev/native-https/evidence" ] || {
  printf 'makefile-safety-test: failed auth preflight created evidence\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
run_https_make dev-https-browser-image >"$WORK/https-image-2.out" 2>&1
run_https_make dev-https-auth-check >"$WORK/https-auth.out" 2>&1 || {
  sed -n '1,120p' "$WORK/https-auth.out" >&2
  printf 'makefile-safety-test: auth check target failed\n' >&2
  exit 1
}
HTTPS_EVIDENCE_ROOT=$HTTPS_REPO/.dev/native-https/evidence
[ -d "$HTTPS_EVIDENCE_ROOT" ] && [ ! -L "$HTTPS_EVIDENCE_ROOT" ] || {
  printf 'makefile-safety-test: evidence root is missing\n' >&2
  exit 1
}
[ "$(stat -c %a "$HTTPS_EVIDENCE_ROOT")" = 700 ] || {
  printf 'makefile-safety-test: evidence root mode drifted\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 1 ] || {
  printf 'makefile-safety-test: auth check did not create one fresh evidence directory\n' >&2
  exit 1
}
HTTPS_RUNTIME_LOG=$WORK/https-runtime.calls
read_https_calls >"$HTTPS_RUNTIME_LOG"
grep -Fxq -- '--pull=never' "$HTTPS_RUNTIME_LOG" || {
  printf 'makefile-safety-test: auth run can pull an image\n' >&2
  exit 1
}
grep -Fxq -- '--network=host' "$HTTPS_RUNTIME_LOG" || {
  printf 'makefile-safety-test: auth run lacks host network\n' >&2
  exit 1
}
grep -Fxq -- '--read-only' "$HTTPS_RUNTIME_LOG" || {
  printf 'makefile-safety-test: auth run lacks read-only root\n' >&2
  exit 1
}
grep -Fxq -- '--cap-drop=all' "$HTTPS_RUNTIME_LOG" || {
  printf 'makefile-safety-test: auth run lacks capability drop\n' >&2
  exit 1
}
grep -Fxq -- '--cap-add=SYS_CHROOT' "$HTTPS_RUNTIME_LOG" || {
  printf 'makefile-safety-test: auth run lacks the Chromium sandbox capability\n' >&2
  exit 1
}
[ "$(grep -Ec '^--cap-add=' "$HTTPS_RUNTIME_LOG")" -eq 1 ] || {
  printf 'makefile-safety-test: auth run added an unexpected capability\n' >&2
  exit 1
}
grep -Fxq -- "$HTTPS_ID" "$HTTPS_RUNTIME_LOG" || {
  printf 'makefile-safety-test: auth run did not use immutable image ID\n' >&2
  exit 1
}
[ "$(grep -Ec '^--mount=type=bind,.*dst=/' "$HTTPS_RUNTIME_LOG")" -eq 2 ] || {
  printf 'makefile-safety-test: auth run has the wrong bind-mount count\n' >&2
  exit 1
}
if grep -Eq '(/run/podman/podman\.sock|/var/run/docker\.sock|dst=/home|dst=/repo|dst=/workspace)' \
  "$HTTPS_RUNTIME_LOG"; then
  printf 'makefile-safety-test: auth run exposed a forbidden host path\n' >&2
  exit 1
fi
[ "$(grep -c '^transport$' "$HTTPS_RUNTIME_LOG")" -eq 0 ] || {
  printf 'makefile-safety-test: three-argument auth run gained a mode argument\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
if run_https_make DEV_HTTPS_FAKE_FAIL=status dev-https-transport-check \
  >"$WORK/https-transport-preflight.out" 2>&1; then
  printf 'makefile-safety-test: transport check passed failed status\n' >&2
  exit 1
fi
if read_https_calls | grep -Fxq podman; then
  printf 'makefile-safety-test: transport check mutated after failed status\n' >&2
  exit 1
fi
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 1 ] || {
  printf 'makefile-safety-test: failed transport preflight created evidence\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
run_https_make dev-https-transport-check >"$WORK/https-transport.out" 2>&1 || {
  sed -n '1,120p' "$WORK/https-transport.out" >&2
  printf 'makefile-safety-test: transport check target failed\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 2 ] || {
  printf 'makefile-safety-test: transport check did not create separate evidence\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d -name 'google-auth.*' | wc -l)" -eq 1 ] || {
  printf 'makefile-safety-test: auth evidence directory naming drifted\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d -name 'transport.*' | wc -l)" -eq 1 ] || {
  printf 'makefile-safety-test: transport evidence directory naming drifted\n' >&2
  exit 1
}
find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d -printf '%m\n' |
  awk '$0 != "700" { exit 1 }' || {
  printf 'makefile-safety-test: evidence directory mode drifted\n' >&2
  exit 1
}
HTTPS_TRANSPORT_LOG=$WORK/https-transport-runtime.calls
read_https_calls >"$HTTPS_TRANSPORT_LOG"
grep -Fxq transport "$HTTPS_TRANSPORT_LOG" || {
  printf 'makefile-safety-test: transport mode did not reach the runner\n' >&2
  exit 1
}
grep -Fxq -- '--pull=never' "$HTTPS_TRANSPORT_LOG" || {
  printf 'makefile-safety-test: transport run can pull an image\n' >&2
  exit 1
}
grep -Fxq -- '--network=host' "$HTTPS_TRANSPORT_LOG" || {
  printf 'makefile-safety-test: transport run lacks host network\n' >&2
  exit 1
}
grep -Fxq -- '--read-only' "$HTTPS_TRANSPORT_LOG" || {
  printf 'makefile-safety-test: transport run lacks read-only root\n' >&2
  exit 1
}
[ "$(grep -Ec '^--mount=type=bind,.*dst=/' "$HTTPS_TRANSPORT_LOG")" -eq 2 ] || {
  printf 'makefile-safety-test: transport run has the wrong bind-mount count\n' >&2
  exit 1
}

printf 'Makefile native HTTPS safety tests passed\n'
