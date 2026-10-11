#!/usr/bin/env bash
# Wrapper refusal and staging tests. Podman is stubbed, so successful staging
# reaches the real host runner without building an image or contacting a host.
# The lock and memory fixtures keep these tests independent of local checks.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
SCRIPT=$ROOT/scripts/totp-production-proof.sh

fail() {
  printf 'totp-production-proof-test: %s\n' "$*" >&2
  exit 1
}

WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-totp-prod-proof-test.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

REPO=$WORK/repo
mkdir -p "$REPO/.dev/v0.4.7/production-input" "$REPO/.dev/native-https/run" \
  "$REPO/deploy/dev-https-browser"
cp "$SCRIPT" "$REPO/scripts/totp-production-proof.sh" 2>/dev/null ||
  { mkdir -p "$REPO/scripts"; cp "$SCRIPT" "$REPO/scripts/totp-production-proof.sh"; }
mkdir -p "$REPO/scripts/lib"
cp "$ROOT/scripts/lib/dev-https-browser-stage.sh" "$REPO/scripts/lib/"
chmod 0700 "$REPO/.dev/v0.4.7/production-input"

LOCK_PATH=$WORK/local-check.lock
MEMINFO_HIGH=$WORK/meminfo-high
MEMINFO_LOW=$WORK/meminfo-low
printf 'MemTotal:       33554432 kB\nMemAvailable:   16000000 kB\n' >"$MEMINFO_HIGH"
printf 'MemTotal:       33554432 kB\nMemAvailable:     100000 kB\n' >"$MEMINFO_LOW"

run_wrapper() {
  ABOUTME_LOCAL_CHECK_LOCK_PATH=$LOCK_PATH \
    ABOUTME_MEMINFO_PATH=$MEMINFO_HIGH \
    "$@" "$REPO/scripts/totp-production-proof.sh" totp-prod-flag-off
}

# --- usage --------------------------------------------------------------

if output=$(ABOUTME_LOCAL_CHECK_LOCK_PATH=$LOCK_PATH \
  "$REPO/scripts/totp-production-proof.sh" 2>&1); then
  fail 'missing mode argument was accepted'
fi
grep -Fq 'usage: totp-production-proof.sh' <<<"$output" ||
  fail 'missing mode argument returned the wrong diagnostic'

if output=$(ABOUTME_LOCAL_CHECK_LOCK_PATH=$LOCK_PATH \
  "$REPO/scripts/totp-production-proof.sh" bogus-mode 2>&1); then
  fail 'an unknown mode was accepted'
fi
grep -Fq 'mode must be totp-prod-flag-off, totp-prod-enabled, or totp-prod-cleanup' \
  <<<"$output" || fail 'unknown mode returned the wrong diagnostic'

# --- account input --------------------------------------------------------

if output=$(run_wrapper 2>&1); then
  fail 'a missing account.env was accepted'
fi
grep -Fq 'account.env is missing' <<<"$output" ||
  fail 'missing account.env returned the wrong diagnostic'

printf 'ABOUTME_TEST_EMAIL=totp-prod@example.invalid\nABOUTME_TEST_PASSWORD=x\n' \
  >"$REPO/.dev/v0.4.7/production-input/account.env"
chmod 0644 "$REPO/.dev/v0.4.7/production-input/account.env"
if output=$(run_wrapper 2>&1); then
  fail 'a world-readable account.env was accepted'
fi
grep -Fq 'account.env ownership or mode mismatch' <<<"$output" ||
  fail 'wrong-mode account.env returned the wrong diagnostic'
chmod 0600 "$REPO/.dev/v0.4.7/production-input/account.env"

chmod 0755 "$REPO/.dev/v0.4.7/production-input"
if output=$(run_wrapper 2>&1); then
  fail 'a wrong-mode production input directory was accepted'
fi
grep -Fq 'production input directory ownership or mode mismatch' <<<"$output" ||
  fail 'wrong-mode production input directory returned the wrong diagnostic'
chmod 0700 "$REPO/.dev/v0.4.7/production-input"

# --- memory floor ----------------------------------------------------------

if output=$(ABOUTME_LOCAL_CHECK_LOCK_PATH=$LOCK_PATH \
  ABOUTME_MEMINFO_PATH=$MEMINFO_LOW \
  "$REPO/scripts/totp-production-proof.sh" totp-prod-flag-off 2>&1); then
  fail 'low MemAvailable was accepted'
fi
grep -Fq 'fewer than 8 GiB MemAvailable' <<<"$output" ||
  fail 'low MemAvailable returned the wrong diagnostic'

# --- active stack ------------------------------------------------------------

printf '%s' "$$" >"$REPO/.dev/native-https/run/server.pid"
if output=$(run_wrapper 2>&1); then
  fail 'an active development stack was accepted'
fi
grep -Fq 'an aboutme development stack is active' <<<"$output" ||
  fail 'active stack returned the wrong diagnostic'
rm -f -- "$REPO/.dev/native-https/run/server.pid"

# --- shared lock --------------------------------------------------------

exec {HELD_FD}>>"$LOCK_PATH"
flock "$HELD_FD"
if output=$(run_wrapper 2>&1); then
  fail 'the wrapper ran while the shared lock was held'
fi
grep -Fq 'another local check holds the shared lock' <<<"$output" ||
  fail 'held shared lock returned the wrong diagnostic'
flock -u "$HELD_FD"
exec {HELD_FD}>&-

# Exercise the wrapper's staging through the real runner's validation.
cp "$ROOT/deploy/dev-https-browser/"* "$REPO/deploy/dev-https-browser/"
mkdir -p "$WORK/bin"
cat >"$WORK/bin/podman" <<'STUB'
#!/usr/bin/env bash
set -Eeuo pipefail
image=sha256:1111111111111111111111111111111111111111111111111111111111111111
case $1 in
build)
  for staged in "${!#}/../../.dev/v0.4.7/"totp-prod-spec.*; do
    case ${STAGING_TAMPER-} in
    extra) printf 'extra\n' >"$staged/extra.ts" ;;
    manifest) cp "${!#}/spec-sources.txt" "$staged/spec-sources.txt" ;;
    missing) rm "$staged/auth.spec.ts" ;;
    file-symlink) rm "$staged/auth.spec.ts"; ln -s transport.spec.ts "$staged/auth.spec.ts" ;;
    directory-symlink) mv "$staged" "$staged.real"; ln -s "$staged.real" "$staged" ;;
    file-mode) chmod 0644 "$staged/auth.spec.ts" ;;
    directory-mode) chmod 0755 "$staged" ;;
    esac
  done
  while [ "$#" -gt 0 ]; do
    if [ "$1" = --iidfile ]; then printf '%s\n' "$image" >"$2"; exit 0; fi
    shift
  done
  exit 1
  ;;
image)
  printf '%s|pwuser|["/opt/aboutme-auth/run.sh","--inside"]|2|%s|1.62.1|2:3.98-1ubuntu0.2\n' \
    "$image" 'mcr.microsoft.com/playwright:v1.62.1-noble@sha256:c091b21d9fae78c76e85cd4356431e9b018402f172a214fc7d7a5e9a7e29d8ac'
  ;;
run)
  [ "${!#}" = totp-prod-flag-off ]
  for arg in "$@"; do
    case $arg in
    --mount=type=bind,src=*,dst=/uat-spec,ro=true)
      path=${arg#--mount=type=bind,src=}
      path=${path%,dst=/uat-spec,ro=true}
      [ -f "$path/mcp-ts-sdk.spec.ts" ]
      printf '%s\n' "$path" >"$STAGED_PATH"
      ;;
    esac
  done
  [ -s "$STAGED_PATH" ]
  ;;
*) exit 1 ;;
esac
STUB
cat >"$WORK/bin/stat" <<'STUB'
#!/usr/bin/env bash
set -Eeuo pipefail
case ${STAGING_TAMPER-}:${1-}:${2-}:${3-} in
file-owner:-c:%u:*/totp-prod-spec.*/auth.spec.ts | directory-owner:-c:%u:*/totp-prod-spec.*)
  printf '0\n'; exit 0 ;;
esac
exec /usr/bin/stat "$@"
STUB
chmod 0700 "$WORK/bin/podman" "$WORK/bin/stat"
if ! output=$(PATH="$WORK/bin:$PATH" STAGED_PATH="$WORK/staged-path" run_wrapper 2>&1); then
  fail "wrapper staging did not pass runner validation: $output"
fi
[ -s "$WORK/staged-path" ] || fail 'wrapper never reached the stubbed browser run'
[ ! -e "$(cat "$WORK/staged-path")" ] || fail 'wrapper left its staging directory behind'

for tamper in extra manifest missing file-symlink directory-symlink file-mode directory-mode file-owner directory-owner; do
  rm -f "$WORK/staged-path"
  if output=$(PATH="$WORK/bin:$PATH" STAGED_PATH="$WORK/staged-path" \
    STAGING_TAMPER=$tamper run_wrapper 2>&1); then
    fail "runner accepted staged source tampering: $tamper"
  fi
  case $tamper in
  extra | manifest | missing) expected='spec input must contain exactly the spec sources' ;;
  file-symlink) expected='spec source is not a regular file' ;;
  directory-symlink) expected='mount path is not a real directory' ;;
  file-mode) expected='spec source mode must be 0600' ;;
  directory-mode) expected='spec input mode must be 0700' ;;
  file-owner) expected='spec source owner mismatch' ;;
  directory-owner) expected='spec input owner mismatch' ;;
  esac
  grep -Fq "$expected" <<<"$output" || fail "$tamper returned the wrong diagnostic: $output"
  [ ! -e "$WORK/staged-path" ] || fail "$tamper reached the browser run"
  rm -rf -- "$REPO/.dev/v0.4.7/"totp-prod-spec.*
done

printf 'totp-production-proof static tests: PASS\n'
