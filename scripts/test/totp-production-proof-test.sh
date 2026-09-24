#!/usr/bin/env bash
# Static refusal tests for scripts/totp-production-proof.sh. Every case here
# is expected to fail before the script would build an image or touch
# podman, so none of these need a faked podman. The shared local-check lock
# and /proc/meminfo are overridden through the script's own test-only
# environment variables so this never touches the real shared lock any
# other worktree or session relies on (instructions/resources.md).
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

printf 'totp-production-proof static tests: PASS\n'
