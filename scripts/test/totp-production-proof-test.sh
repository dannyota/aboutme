#!/usr/bin/env bash
# Wrapper refusal and staging tests. Podman is stubbed, so successful staging
# reaches the real host runner without building an image or contacting a host.
# The lock and watchdog fixtures keep these tests independent of local checks.
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
cp "$ROOT/scripts/lib/platform.sh" "$ROOT/scripts/lib/local-check.py" "$REPO/scripts/lib/"
cat >"$REPO/scripts/run-owned.sh" <<'STUB'
#!/usr/bin/env bash
[ "${STUB_OWNED_REFUSAL:-}" != invalid ] || { echo 'refused: cannot inspect process ownership' >&2; exit 125; }
printf '%s\n' "$*" >>"$WATCHDOG_LOG"
shift 3
exec "$@"
STUB
export WATCHDOG_LOG=$WORK/watchdog.log

chmod 0700 "$REPO/.dev/v0.4.7/production-input"

LOCK_PATH=$WORK/local-check.lock
run_wrapper() {
  ABOUTME_LOCAL_CHECK_LOCK_PATH=$LOCK_PATH \
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

# --- watchdog refusal --------------------------------------------------------

if output=$(STUB_OWNED_REFUSAL=invalid run_wrapper 2>&1); then
  fail 'missing process ownership was accepted'
fi
grep -Fq 'refused: cannot inspect process ownership' <<<"$output" ||
  fail 'missing ownership returned the wrong diagnostic'

# --- active stack ------------------------------------------------------------

printf '%s' "$$" >"$REPO/.dev/native-https/run/server.pid"
if output=$(run_wrapper 2>&1); then
  fail 'an active development stack was accepted'
fi
grep -Fq 'an aboutme development stack is active' <<<"$output" ||
  fail 'active stack returned the wrong diagnostic'
rm -f -- "$REPO/.dev/native-https/run/server.pid"

printf '{"version":1}\n' >"$REPO/.dev/native-https/run/session"
if output=$(run_wrapper 2>&1); then fail 'development session state was accepted'; fi
grep -Fq 'development session state exists' <<<"$output" || fail 'missing session refusal'
rm "$REPO/.dev/native-https/run/session"

# --- shared lock --------------------------------------------------------

python3 - "$LOCK_PATH" "$WORK/lock-ready" <<'LOCK' &
import fcntl, sys, time
with open(sys.argv[1], "w") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    open(sys.argv[2], "w").close()
    time.sleep(30)
LOCK
holder=$!
for ((i=0; i<200; i++)); do
  [ ! -f "$WORK/lock-ready" ] || break
  sleep 0.05
done
[ -f "$WORK/lock-ready" ] || fail 'lock holder never started'
if output=$(run_wrapper 2>&1); then
  kill "$holder"
  wait "$holder" || true
  fail 'the wrapper ran while the shared lock was held'
fi
kill "$holder"
wait "$holder" || true
grep -Fq 'another local check holds the shared lock' <<<"$output" ||
  fail 'held shared lock returned the wrong diagnostic'

# Exercise the wrapper's staging through the real runner's validation.
copy_browser_sources() {
  local source=$1 destination=$2 file
  local -a spec_sources=()
  mapfile -t spec_sources <"$source/spec-sources.txt"
  for file in Dockerfile package.json package-lock.json run.sh verify-evidence.mjs \
    spec-sources.txt "${spec_sources[@]}"; do
    cp "$source/$file" "$destination/"
  done
}
mkdir -p "$WORK/browser-source"
copy_browser_sources "$ROOT/deploy/dev-https-browser" "$WORK/browser-source"
mkdir "$WORK/browser-source/node_modules"
printf 'unrelated\n' >"$WORK/browser-source/node_modules/unrelated.js"
copy_browser_sources "$WORK/browser-source" "$REPO/deploy/dev-https-browser"
[ ! -e "$REPO/deploy/dev-https-browser/node_modules" ] ||
  fail 'fixture copied an unrelated source directory'
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
  printf '%s|pwuser|["/opt/aboutme-auth/run.sh","--inside"]|2|%s|1.64.0|2:3.98-1ubuntu0.3\n' \
    "$image" 'mcr.microsoft.com/playwright:v1.64.0-noble@sha256:bc72a8df40831ded821e3cbc79c318c21b44d6e51dff9fd89ace7a8249c5b462'
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
export STUB_REAL_STAT
STUB_REAL_STAT=$(command -v stat)
cat >"$WORK/bin/stat" <<'STUB'
#!/usr/bin/env bash
set -Eeuo pipefail
case ${STAGING_TAMPER-}:${1-}:${2-}:${3-} in
file-owner:-c:%u:*/totp-prod-spec.*/auth.spec.ts | directory-owner:-c:%u:*/totp-prod-spec.*)
  : >"$OWNER_INJECTED"; printf '0\n'; exit 0 ;;
esac
exec "$STUB_REAL_STAT" "$@"
STUB
export STUB_REAL_PYTHON
STUB_REAL_PYTHON=$(command -v python3)
cat >"$WORK/bin/python3" <<'STUB'
#!/usr/bin/env bash
set -Eeuo pipefail
if [ "${1-}" = -I ] && [ "${2-}" = -c ] && [[ ${3-} == *'os.lstat(sys.argv[2])'* ]]; then
  case ${STAGING_TAMPER-}:${4-}:${5-} in
  file-owner:u:*/totp-prod-spec.*/auth.spec.ts | directory-owner:u:*/totp-prod-spec.*)
    : >"$OWNER_INJECTED"; printf '0\n'; exit 0 ;;
  esac
fi
exec "$STUB_REAL_PYTHON" "$@"
STUB
# Refuse GNU find formatting even when this test runs on Linux.
export STUB_REAL_FIND
STUB_REAL_FIND=$(command -v find)
export OWNER_INJECTED=$WORK/owner-injected
cat >"$WORK/bin/find" <<'STUB'
#!/usr/bin/env bash
for argument in "$@"; do
  [ "$argument" != -printf ] || { echo 'BSD find has no -printf' >&2; exit 1; }
done
exec "$STUB_REAL_FIND" "$@"
STUB
chmod 0700 "$WORK/bin/find"
chmod 0700 "$WORK/bin/podman" "$WORK/bin/stat" "$WORK/bin/python3"
if ! output=$(PATH="$WORK/bin:$PATH" STAGED_PATH="$WORK/staged-path" run_wrapper 2>&1); then
  fail "wrapper staging did not pass runner validation: $output"
fi
[ -s "$WORK/staged-path" ] || fail 'wrapper never reached the stubbed browser run'
grep -Fq -- '--timeout 4200 -- bash ' "$WATCHDOG_LOG" || fail 'whole wrapper watchdog missing'
grep -Fq -- '--timeout 600 -- podman build ' "$WATCHDOG_LOG" || fail 'build timeout missing'
grep -Fq -- '--timeout 3600 -- ' "$WATCHDOG_LOG" || fail 'proof timeout missing'

[ ! -e "$(cat "$WORK/staged-path")" ] || fail 'wrapper left its staging directory behind'

for tamper in extra manifest missing file-symlink directory-symlink file-mode directory-mode file-owner directory-owner; do
  rm -f "$WORK/staged-path" "$OWNER_INJECTED"
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
  case $tamper in
  file-owner | directory-owner) [ -e "$OWNER_INJECTED" ] || fail "$tamper did not reach the owner check" ;;
  esac
  [ ! -e "$WORK/staged-path" ] || fail "$tamper reached the browser run"
  rm -rf -- "$REPO/.dev/v0.4.7/"totp-prod-spec.*
done

printf 'totp-production-proof static tests: PASS\n'
