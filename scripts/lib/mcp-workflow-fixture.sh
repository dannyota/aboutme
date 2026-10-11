#!/usr/bin/env bash

# Local synthetic API requests keep response bodies private. Failure evidence
# contains only fixed labels, closed error codes, and bounded request metrics.
set -Eeuo pipefail
umask 077

fixture_failure_evidence() {
  local response=$1 label=$2 want=$3 metrics=$4 curl_exit=$5 destination=$6
  local status=000 duration=unknown code=none temporary
  if [[ $metrics =~ ^(000|[1-5][0-9]{2})\ ([0-9]{1,5}\.[0-9]{6})$ ]]; then
    status=${BASH_REMATCH[1]} duration=${BASH_REMATCH[2]}
  fi
  case $label in
  register | verify | sign-in | sign-out | session | create | photo | inspect) ;;
  *) label=unknown ;;
  esac
  if [ -s "$response" ]; then
    code=unknown
    if [ "$(stat -c %s -- "$response")" -le 65536 ]; then
      code=$(jq -ser 'select(length == 1) | .[0].error.code | select(IN(
        "request_invalid", "credential_token_invalid", "authentication_failed",
        "reauth_failed", "authentication_required", "reauth_required",
        "media_type_unsupported", "body_too_large", "password_invalid",
        "rate_limited", "authentication_unavailable", "csrf_rejected",
        "invalid_client_ip", "internal_error", "not_found", "method_not_allowed"
      ))' "$response" 2>/dev/null) || code=unknown
    fi
  fi
  printf 'mcp-owner-workflow: fixture response at %s status %s code %s curl-exit %s duration-seconds %s\n' \
    "$label" "$status" "$code" "$curl_exit" "$duration"
  # The CI upload reads this staging root, which survives fixture cleanup and
  # later harness restarts. Never copy raw response or request files here.
  install -d -m 0700 "$destination" || return 1
  temporary=$(mktemp "$destination/.failure.XXXXXX") || return 1
  jq -n --arg stage "$label" --arg code "$code" --arg duration "$duration" \
    --argjson expected "$want" --argjson status "$((10#$status))" --argjson curl_exit "$curl_exit" \
    '{version: 1, stage: $stage, expected_status: $expected, http_status: $status,
      error_code: $code, curl_exit: $curl_exit, duration_seconds: $duration}' >"$temporary" &&
    chmod 0600 "$temporary" && mv -f -- "$temporary" "$destination/fixture-failure.json"
}

# Request secrets travel as private file paths, never file contents. Success
# requires both a zero curl exit and the exact HTTP status.
fixture_request() {
  local ca_input=$1 scratch=$2 origin=$3 destination=$4
  shift 4
  local want=$1 label=$2 method=$3 path=$4 body=${5-} headers=${6-} form=${7-} metrics curl_exit=0
  local -a args=(-sS --proto '=https' --cacert "$ca_input/caddy-root.crt" --max-time 30
    -o "$scratch/response" -w '%{http_code} %{time_total}' -b "$scratch/cookies" -c "$scratch/cookies"
    -H "Origin: $origin" -X "$method")
  [ -z "$headers" ] || args+=(-H "@$headers")
  [ -z "$body" ] || args+=(--data-binary "@$body")
  [ -z "$form" ] || args+=(-F "$form")
  rm -f -- "$scratch/response"
  metrics=$(curl "${args[@]}" "$origin$path" 2>/dev/null) || curl_exit=$?
  [ "$curl_exit" -eq 0 ] && [ "${metrics%% *}" = "$want" ] && return 0
  fixture_failure_evidence "$scratch/response" "$label" "$want" "$metrics" "$curl_exit" "$destination" ||
    printf 'mcp-owner-workflow: fixture diagnostic write failed\n'
  return 1
}

fixture_request "$@"
