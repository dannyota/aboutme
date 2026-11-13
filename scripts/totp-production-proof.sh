#!/usr/bin/env bash
set -Eeuo pipefail

REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
# shellcheck source=scripts/lib/platform.sh
source "$REPO/scripts/lib/platform.sh"
require_modern_bash || exit 1
require_gnu_tools stat realpath || exit 1

CONTEXT=$REPO/deploy/dev-https-browser
ACCOUNT_DIR=$REPO/.dev/v0.4.7/production-input
ACCOUNT_FILE=$ACCOUNT_DIR/account.env
EVIDENCE_DIR=$REPO/.dev/v0.4.7/production-evidence

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

for record in "$REPO"/.dev/native-https/run/session*; do
  [ ! -e "$record" ] && [ ! -L "$record" ] ||
    fail 'development session state exists; stop the stack before a production proof'
done

if [ -z "${ABOUTME_TOTP_PROOF_CAPPED:-}" ]; then
  LOCK=${ABOUTME_LOCAL_CHECK_LOCK_PATH:-$(local_check_lock_path "$REPO")}
  exec python3 "$REPO/scripts/lib/local-check.py" "$LOCK" 0 \
    env ABOUTME_TOTP_PROOF_CAPPED=1 bash "$REPO/scripts/run-owned.sh" \
    --timeout 4200 -- bash "$REPO/scripts/totp-production-proof.sh" "$MODE"
fi

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

iidfile=$(mktemp "$REPO/.dev/v0.4.7/totp-prod-iid.XXXXXX")
if ! bash "$REPO/scripts/run-owned.sh" --timeout 600 -- \
  podman build \
  --iidfile "$iidfile" "$CONTEXT" >&2; then
  rm -f -- "$iidfile"
  fail 'browser image build failed'
fi
image_id=$(<"$iidfile")
rm -f -- "$iidfile"
[[ $image_id =~ ^sha256:[0-9a-f]{64}$ ]] ||
  fail 'browser image build returned a malformed ID'

printf 'totp-production-proof: image %s\n' "$image_id" >&2

bash "$REPO/scripts/run-owned.sh" --timeout 3600 -- \
  "$CONTEXT/run.sh" "$image_id" "$ACCOUNT_DIR" "$staging" "$EVIDENCE_DIR" "$MODE"
