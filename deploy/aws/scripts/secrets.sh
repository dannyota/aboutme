#!/usr/bin/env bash
# Creates missing production secrets in SSM Parameter Store. It never
# overwrites a parameter and never prints or reads back a value.
set -euo pipefail
region=ap-southeast-1
prefix=/aboutme/prod

exists() {
  [[ $(aws ssm get-parameters --region "$region" --names "$1" \
    --query 'length(Parameters)' --output text) == 1 ]]
}

put() { # name type generator...
  local name=$prefix/$1 type=$2
  shift 2
  if exists "$name"; then
    echo "kept $name"
    return
  fi
  "$@" | jq -Rn --arg n "$name" --arg t "$type" '{Name: $n, Type: $t, Value: input}' >"$input"
  aws ssm put-parameter --region "$region" --cli-input-json "file://$input" >/dev/null
  rm -f "$input"
  echo "created $name"
}

# The request body holds the value, so it goes through a private tmpfs file,
# never a command argument.
umask 077
input=$(mktemp -p "${XDG_RUNTIME_DIR:?XDG_RUNTIME_DIR must point at a per-user tmpfs}")
trap 'rm -f "$input"' EXIT

hex48() { openssl rand -hex 24; }
key32() { openssl rand 32 | basenc --base64url | tr -d '=\n' && echo; }
keyid() { echo k1; }

put db/migrator-password SecureString hex48
put db/app-password SecureString hex48
put auth-email/active-key-id String keyid
put auth-email/active-key SecureString key32
put password-rate-hmac-key SecureString key32
