#!/usr/bin/env bash
# Proves create-only writes with one disposable, nonsecret state-bucket object.
set -euo pipefail

BUCKET=aboutme-tfstate
ENDPOINT=https://hcm04.vstorage.vngcloud.vn
REGION=HCM04
PREFIX=restore-drill/probes
credentials=${DRILL_STATE_CREDENTIALS_FILE:-${HOME:-}/.config/aboutme/vn/tfstate-s3-credentials}
work=
key=
created=0
cleanup_attempted=0
second_written=0
probe_max_bytes=0

say() { printf '%s\n' "$*"; }
die() { printf 'drill state proof: %s\n' "$*" >&2; exit 1; }

s3_call() {
  env -i PATH="$PATH" \
    AWS_SHARED_CREDENTIALS_FILE="$credentials" \
    AWS_CONFIG_FILE="$work/aws-config" \
    AWS_EC2_METADATA_DISABLED=true \
    AWS_MAX_ATTEMPTS=1 \
    AWS_RETRY_MODE=standard \
    AWS_PAGER= \
    AWS_CLI_AUTO_PROMPT=off \
    timeout --foreground --signal=KILL 60s \
    aws --profile default --endpoint-url "$ENDPOINT" --region "$REGION" \
    --no-cli-pager --no-cli-auto-prompt s3api "$@"
}

# Returns 0 only for a definite absence, 1 for an existing object, and 2 when
# the response cannot prove either result.
object_absent() {
  local errors=$1
  : >"$errors"
  if s3_call head-object --bucket "$BUCKET" --key "$key" >/dev/null 2>"$errors"; then
    return 1
  fi
  grep -Eq '^An error occurred \((NoSuchKey|404)\) when calling the HeadObject operation:' "$errors" && return 0
  return 2
}

cleanup_probe() {
  local errors=$work/cleanup-errors readback=$work/cleanup-readback owned=0 readback_size
  ((created == 1 && cleanup_attempted == 0)) || return 0
  cleanup_attempted=1
  if ! s3_call get-object --bucket "$BUCKET" --key "$key" \
    --range "bytes=0-$probe_max_bytes" "$readback" \
    >/dev/null 2>"$errors"; then
    say "cleanup retained an unreadable probe; inspect the nonsecret recovery key: s3://$BUCKET/$key" >&2
    return 1
  fi
  if ! readback_size=$(stat -c %s -- "$readback") ||
    ((readback_size == 0 || readback_size > probe_max_bytes)); then
    say "cleanup retained oversized or invalid bytes; inspect the nonsecret recovery key: s3://$BUCKET/$key" >&2
    return 1
  fi
  cmp -s -- "$work/first.json" "$readback" && owned=1
  if ((second_written == 1)) && cmp -s -- "$work/second.json" "$readback"; then owned=1; fi
  if ((owned == 0)); then
    say "cleanup retained unowned bytes; inspect the nonsecret recovery key: s3://$BUCKET/$key" >&2
    return 1
  fi
  s3_call delete-object --bucket "$BUCKET" --key "$key" >/dev/null 2>"$errors" || true
  if object_absent "$errors"; then
    created=0
    return 0
  fi
  say "cleanup failed; inspect the nonsecret recovery key: s3://$BUCKET/$key" >&2
  return 1
}

on_exit() {
  local status=$?
  trap - EXIT INT TERM
  if ! cleanup_probe; then status=1; fi
  [[ -z ${work:-} ]] || rm -rf -- "$work"
  exit "$status"
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

command -v aws >/dev/null || die 'AWS CLI is required'
command -v jq >/dev/null || die 'jq is required'
command -v uuidgen >/dev/null || die 'uuidgen is required'
[[ -f $credentials && ! -L $credentials ]] || die "credential file is not a regular file: $credentials"

work=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-drill-state-proof.XXXXXX")
chmod 0700 "$work"
printf '[default]\ns3 =\n    addressing_style = path\n' >"$work/aws-config"
chmod 0600 "$work/aws-config"

uuid=$(uuidgen | tr '[:upper:]' '[:lower:]')
[[ $uuid =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]] ||
  die 'uuidgen returned an invalid UUID'
key=$PREFIX/$uuid.json
printf '{"probe":"%s","value":"first"}\n' "$uuid" >"$work/first.json"
printf '{"probe":"%s","value":"second"}\n' "$uuid" >"$work/second.json"
first_size=$(stat -c %s -- "$work/first.json") || die 'first probe payload size is unavailable'
second_size=$(stat -c %s -- "$work/second.json") || die 'second probe payload size is unavailable'
probe_max_bytes=$first_size
if ((second_size > probe_max_bytes)); then probe_max_bytes=$second_size; fi
((probe_max_bytes > 0)) || die 'probe payloads must not be empty'

if ! s3_call get-bucket-versioning --bucket "$BUCKET" \
  >"$work/versioning" 2>"$work/errors"; then
  die 'bucket versioning state could not be read'
fi
if [[ -s $work/versioning ]]; then
  status=$(jq -er '
    if type == "object"
      and ((keys_unsorted - ["Status", "MFADelete"]) | length == 0)
      and (.Status == "Enabled" or .Status == "Suspended")
      and ((has("MFADelete") | not) or .MFADelete == "Enabled" or .MFADelete == "Disabled")
    then .Status else error("invalid versioning response") end
  ' "$work/versioning" 2>/dev/null) || die 'bucket versioning response is invalid'
  die "bucket versioning is $status; deletion could leave a recoverable probe version"
fi

if object_absent "$work/errors"; then
  :
else
  case $? in
    1) die "probe key already exists; refusing to write or delete it: s3://$BUCKET/$key" ;;
    *) die "probe key absence is unproved: s3://$BUCKET/$key" ;;
  esac
fi

if ! s3_call put-object --bucket "$BUCKET" --key "$key" --body "$work/first.json" \
  --content-type application/json --if-none-match '*' >/dev/null 2>"$work/errors"; then
  die "create response is ambiguous; inspect the nonsecret recovery key: s3://$BUCKET/$key"
fi
created=1

if s3_call put-object --bucket "$BUCKET" --key "$key" --body "$work/second.json" \
  --content-type application/json --if-none-match '*' >/dev/null 2>"$work/errors"; then
  second_written=1
  die 'conditional overwrite unexpectedly succeeded'
fi
grep -Eq '^An error occurred \((PreconditionFailed|412)\) when calling the PutObject operation:' "$work/errors" ||
  die 'conditional overwrite did not return a definite precondition conflict'

if ! s3_call get-object --bucket "$BUCKET" --key "$key" \
  --range "bytes=0-$probe_max_bytes" "$work/readback.json" \
  >/dev/null 2>"$work/errors"; then
  die 'original probe could not be read back'
fi
readback_size=$(stat -c %s -- "$work/readback.json") || die 'probe read-back size is unavailable'
((readback_size > 0 && readback_size <= probe_max_bytes)) ||
  die 'probe read-back exceeds the attributable payload bound'
cmp -s -- "$work/first.json" "$work/readback.json" ||
  die 'stored probe bytes differ from the original write'

cleanup_probe || die "probe cleanup is unproved: s3://$BUCKET/$key"
say "conditional state write proof passed; probe is absent: s3://$BUCKET/$key"
