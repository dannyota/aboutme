#!/usr/bin/env bash
# Stages and runs one production TOTP proof mode
# (docs/runbooks/totp-keys.md "Production proofs"). run.sh's validate_spec_dir
# requires an owner-matched, mode-0700, exact-fileset spec directory, which
# the live deploy/dev-https-browser source tree can never satisfy (it also
# carries Dockerfile, run.sh, and every other mode's non-production spec), so
# this wrapper stages a temporary copy before calling run.sh, the same way
# scripts/dev-https-check.sh does for the local modes.
#
# Usage: scripts/totp-production-proof.sh <mode>
#   mode: totp-prod-flag-off | totp-prod-enabled | totp-prod-cleanup
#
# The manager runs it directly from the release worktree:
#
#   scripts/totp-production-proof.sh totp-prod-enabled
#
# It takes the shared local-check lock itself without blocking and holds it
# until it exits. Child processes never inherit the lock descriptor, so a
# container helper cannot keep the lock after the run. It also checks the
# memory floor and stack quiescence named in the runbook before it builds or
# runs anything. Do not wrap it in another flock on the same lock.
set -Eeuo pipefail

REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
CONTEXT=$REPO/deploy/dev-https-browser
ACCOUNT_DIR=$REPO/.dev/v0.4.7/production-input
ACCOUNT_FILE=$ACCOUNT_DIR/account.env
EVIDENCE_DIR=$REPO/.dev/v0.4.7/production-evidence
MEM_FLOOR_KIB=$((8 * 1024 * 1024))

fail() {
  printf 'totp-production-proof: %s\n' "$*" >&2
  exit 1
}

[ "$#" -eq 1 ] ||
  fail 'usage: totp-production-proof.sh totp-prod-flag-off|totp-prod-enabled|totp-prod-cleanup'
MODE=$1
case $MODE in
totp-prod-flag-off | totp-prod-enabled | totp-prod-cleanup) ;;
*) fail 'mode must be totp-prod-flag-off, totp-prod-enabled, or totp-prod-cleanup' ;;
esac

UID_NOW=$(id -u)
[ "$UID_NOW" -ne 0 ] || fail 'must not run as root'

# --- Account input: existence and mode only, never contents ----------------

[ -d "$ACCOUNT_DIR" ] && [ ! -L "$ACCOUNT_DIR" ] ||
  fail 'production input directory is missing'
[ "$(stat -c %u "$ACCOUNT_DIR")" = "$UID_NOW" ] &&
  [ "$(stat -c %a "$ACCOUNT_DIR")" = 700 ] ||
  fail 'production input directory ownership or mode mismatch; want 0700'
[ -f "$ACCOUNT_FILE" ] && [ ! -L "$ACCOUNT_FILE" ] ||
  fail 'account.env is missing from the production input directory'
[ "$(stat -c %u "$ACCOUNT_FILE")" = "$UID_NOW" ] &&
  [ "$(stat -c %a "$ACCOUNT_FILE")" = 600 ] ||
  fail 'account.env ownership or mode mismatch; want 0600'

# --- Memory floor ------------------------------------------------------------

# ABOUTME_MEMINFO_PATH lets a static test point this at a fixture file
# instead of the real kernel-provided /proc/meminfo; production never sets it.
mem_available_kib=$(awk '/^MemAvailable:/{print $2}' "${ABOUTME_MEMINFO_PATH:-/proc/meminfo}")
[[ $mem_available_kib =~ ^[0-9]+$ ]] || fail 'cannot read MemAvailable'
[ "$mem_available_kib" -ge "$MEM_FLOOR_KIB" ] ||
  fail 'fewer than 8 GiB MemAvailable; wait for headroom'

# --- No active aboutme development stack ------------------------------------

stack_pid_alive() {
  local pidfile=$1 pid
  [ -f "$pidfile" ] && [ ! -L "$pidfile" ] || return 1
  pid=$(<"$pidfile")
  [[ $pid =~ ^[0-9]+$ ]] || return 1
  kill -0 "$pid" 2>/dev/null
}

for pidfile in "$REPO"/.dev/*.pid "$REPO"/.dev/native-https/run/*.pid; do
  [ -e "$pidfile" ] || continue
  ! stack_pid_alive "$pidfile" ||
    fail 'an aboutme development stack is active; stop it first'
done

# --- Shared local-check lock -------------------------------------------------
#
# The manager's own flock wrapper is the primary guard; this repeats it so a
# direct invocation fails fast with a clear message instead of blocking.

# ABOUTME_LOCAL_CHECK_LOCK_PATH lets a static test point this at a scratch
# file instead of the shared lock every worktree and session relies on, and
# skips the Git lookup that path would otherwise need; production never sets
# it.
if [ -n "${ABOUTME_LOCAL_CHECK_LOCK_PATH:-}" ]; then
  LOCK=$ABOUTME_LOCAL_CHECK_LOCK_PATH
else
  GIT_COMMON=$(git -C "$REPO" rev-parse --path-format=absolute --git-common-dir) ||
    fail 'cannot find the Git common directory'
  LOCK=$GIT_COMMON/aboutme-local-check.lock
fi
[ ! -L "$LOCK" ] || fail 'the shared local-check lock is a symbolic link'
exec {LOCK_FD}>>"$LOCK"
flock -n "$LOCK_FD" ||
  fail 'another local check holds the shared lock'

# --- Staging and cleanup -----------------------------------------------------

staging=
cleanup() {
  [ -z "$staging" ] || rm -rf -- "$staging"
}
trap cleanup EXIT

install -d -m 0700 "$EVIDENCE_DIR"
[ "$(stat -c %u "$EVIDENCE_DIR")" = "$UID_NOW" ] &&
  [ "$(stat -c %a "$EVIDENCE_DIR")" = 700 ] ||
  fail 'production evidence directory ownership or mode mismatch'
find "$EVIDENCE_DIR" -mindepth 1 -maxdepth 1 -print -quit |
  grep -q . && fail 'production evidence directory must start empty'

# shellcheck source=scripts/lib/dev-https-browser-stage.sh
source "$REPO/scripts/lib/dev-https-browser-stage.sh"
staging=$(mktemp -d "$REPO/.dev/v0.4.7/totp-prod-spec.XXXXXX")
stage_dev_https_browser_specs "$CONTEXT" "$staging" ||
  fail 'cannot stage the production spec fileset'

# --- Build the pinned image once, then run the staged proof -----------------

iidfile=$(mktemp "$REPO/.dev/v0.4.7/totp-prod-iid.XXXXXX")
if ! timeout --signal=TERM 10m \
  podman build --memory=2g --memory-swap=2g \
  --iidfile "$iidfile" "$CONTEXT" >&2 {LOCK_FD}>&-; then
  rm -f -- "$iidfile"
  fail 'browser image build failed'
fi
image_id=$(<"$iidfile")
rm -f -- "$iidfile"
[[ $image_id =~ ^sha256:[0-9a-f]{64}$ ]] ||
  fail 'browser image build returned a malformed ID'

printf 'totp-production-proof: image %s\n' "$image_id" >&2

timeout --signal=TERM 60m \
  "$CONTEXT/run.sh" "$image_id" "$ACCOUNT_DIR" "$staging" "$EVIDENCE_DIR" "$MODE" \
  {LOCK_FD}>&-
