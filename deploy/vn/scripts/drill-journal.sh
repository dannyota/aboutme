#!/usr/bin/env bash
# Create-only transport for secret-free restore-drill journal records.
# Callers own record semantics, hashes, chains, and controller fencing.

drill_journal_error() {
  printf 'drill journal: %s\n' "$*" >&2
}

drill_journal_record_path_valid() {
  local path=${1:-}
  local uuid='[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}'
  local number='[1-9][0-9]{0,19}'
  [[ ${#path} -le 128 ]] || return 1
  [[ $path =~ ^locks/${number}\.json$ ]] ||
    [[ $path =~ ^releases/${number}\.json$ ]] ||
    [[ $path =~ ^runs/${uuid}/${number}\.json$ ]] ||
    [[ $path =~ ^runs/${uuid}/recovery/${number}\.json$ ]]
}

drill_journal_credentials_file() {
  if [[ -n ${DRILL_STATE_CREDENTIALS_FILE:-} ]]; then
    printf '%s\n' "$DRILL_STATE_CREDENTIALS_FILE"
  elif [[ -n ${HOME:-} ]]; then
    printf '%s/.config/aboutme/vn/tfstate-s3-credentials\n' "$HOME"
  else
    drill_journal_error 'HOME and DRILL_STATE_CREDENTIALS_FILE are unset'
    return 1
  fi
}

drill_journal_input_valid() {
  local file=$1 size
  [[ -f $file && ! -L $file ]] || {
    drill_journal_error 'input must be a regular file, not a symbolic link'
    return 1
  }
  size=$(stat -c '%s' -- "$file") || return 1
  ((size > 0 && size <= 1048576)) || {
    drill_journal_error 'JSON record must be 1 to 1048576 bytes'
    return 1
  }
  jq -e -s 'length == 1' "$file" >/dev/null 2>&1 || {
    drill_journal_error 'input must contain exactly one valid JSON value'
    return 1
  }
}

drill_journal_aws() {
  local config credentials rc
  credentials=$(drill_journal_credentials_file)
  [[ -f $credentials && ! -L $credentials ]] || {
    drill_journal_error "credential file is not a regular file: $credentials"
    return 1
  }
  config=$(mktemp "${TMPDIR:-/tmp}/aboutme-drill-aws.XXXXXX") || return 1
  chmod 0600 "$config"
  printf '[default]\ns3 =\n    addressing_style = path\n' >"$config"
  env -i PATH="$PATH" \
    AWS_SHARED_CREDENTIALS_FILE="$credentials" \
    AWS_CONFIG_FILE="$config" \
    AWS_EC2_METADATA_DISABLED=true \
    AWS_MAX_ATTEMPTS=1 \
    AWS_RETRY_MODE=standard \
    AWS_PAGER= \
    AWS_CLI_AUTO_PROMPT=off \
    timeout --foreground --signal=KILL 60s \
    aws --profile default --endpoint-url https://hcm04.vstorage.vngcloud.vn \
    --region HCM04 --no-cli-pager --no-cli-auto-prompt \
    s3api "$@" || rc=$?
  rm -f -- "$config"
  return "${rc:-0}"
}

# drill_journal_get <record-path> <new-output-file>
drill_journal_get() {
  local record=${1:-} output=${2:-} key parent temp rc=0 size
  drill_journal_record_path_valid "$record" || {
    drill_journal_error 'record path is outside the canonical journal namespace'
    return 2
  }
  [[ -n $output && ! -e $output && ! -L $output ]] || {
    drill_journal_error 'output path must not exist'
    return 2
  }
  parent=$(dirname -- "$output")
  [[ -d $parent ]] || {
    drill_journal_error 'output directory does not exist'
    return 2
  }
  temp=$(mktemp "$parent/.drill-journal-read.XXXXXX") || return 1
  key=restore-drill/v1/$record
  drill_journal_aws get-object --bucket aboutme-tfstate --key "$key" \
    --range 'bytes=0-1048576' "$temp" >/dev/null 2>&1 || rc=$?
  if ((rc != 0)); then
    rm -f -- "$temp"
    drill_journal_error 'GetObject failed; absence was not proved'
    return 1
  fi
  size=$(stat -c '%s' -- "$temp") || rc=1
  if ((rc != 0 || size == 0 || size > 1048576)) ||
    ! jq -e -s 'length == 1' "$temp" >/dev/null 2>&1; then
    rm -f -- "$temp"
    drill_journal_error 'stored record is not bounded valid JSON'
    return 1
  fi
  chmod 0600 "$temp"
  if ! ln -T -- "$temp" "$output" >/dev/null 2>&1; then
    rm -f -- "$temp"
    drill_journal_error 'output path was claimed before publication'
    return 1
  fi
  rm -f -- "$temp"
}

# drill_journal_put_create <record-path> <JSON-file>
drill_journal_put_create() {
  local record=${1:-} file=${2:-} key errors readback snapshot rc=0
  drill_journal_record_path_valid "$record" || {
    drill_journal_error 'record path is outside the canonical journal namespace'
    return 2
  }
  [[ -f $file && ! -L $file ]] || {
    drill_journal_error 'input must be a regular file, not a symbolic link'
    return 2
  }
  snapshot=$(mktemp "${TMPDIR:-/tmp}/aboutme-drill-input.XXXXXX") || return 1
  if ! head -c 1048577 -- "$file" >"$snapshot"; then
    rm -f -- "$snapshot"
    drill_journal_error 'input snapshot failed'
    return 1
  fi
  if ! drill_journal_input_valid "$snapshot"; then
    rm -f -- "$snapshot"
    return 2
  fi
  key=restore-drill/v1/$record
  if ! errors=$(mktemp "${TMPDIR:-/tmp}/aboutme-drill-put-errors.XXXXXX"); then
    rm -f -- "$snapshot"
    return 1
  fi
  if drill_journal_aws put-object --bucket aboutme-tfstate --key "$key" \
    --body "$snapshot" --content-type application/json --if-none-match '*' \
    >/dev/null 2>"$errors"; then
    rm -f -- "$errors" "$snapshot"
    return 0
  fi
  if grep -Eq '^An error occurred \((PreconditionFailed|412)\) when calling the PutObject operation:' "$errors"; then
    rm -f -- "$errors" "$snapshot"
    drill_journal_error 'record already exists; create-only write refused'
    return 3
  fi
  rm -f -- "$errors"

  if ! readback=$(mktemp "${TMPDIR:-/tmp}/aboutme-drill-readback.XXXXXX"); then
    rm -f -- "$snapshot"
    return 1
  fi
  rm -f -- "$readback"
  if ! drill_journal_get "$record" "$readback"; then
    rm -f -- "$readback" "$snapshot"
    drill_journal_error 'PutObject outcome is unproved; do not retry the write'
    return 1
  fi
  cmp -s -- "$snapshot" "$readback" || rc=1
  rm -f -- "$readback" "$snapshot"
  if ((rc != 0)); then
    drill_journal_error 'PutObject outcome conflicts with stored bytes; do not retry the write'
    return 1
  fi
}
