#!/usr/bin/env bash
# Creates missing production secrets in SSM Parameter Store. It never
# overwrites a parameter and never prints or reads back a value.
#
#   secrets.sh                 create the base set of missing secrets
#   secrets.sh totp-key <a|b>  create a fresh totp/key-<slot> if unused
#
# secrets.sh totp-key refuses a slot the running app task definition names,
# so it never overwrites a key a task could still be reading or writing. See
# docs/design/totp-key-management.md, "Rotation".
set -euo pipefail
region=ap-southeast-1
cluster=aboutme-prod
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

# Unlike put, always writes a fresh value: rotation steps 1 and 7 in
# docs/design/totp-key-management.md overwrite a slot that already holds a
# retired key but that no running task names. The base set below keeps its
# never-overwrite rule; only this path takes SSM's Overwrite.
write_totp_key() { # a|b
  local name=$prefix/totp/key-$1 verb=created
  if exists "$name"; then
    verb=replaced
  fi
  key32 | jq -Rn --arg n "$name" --arg t SecureString \
    '{Name: $n, Type: $t, Value: input, Overwrite: true}' >"$input"
  aws ssm put-parameter --region "$region" --cli-input-json "file://$input" >/dev/null
  rm -f "$input"
  echo "$verb $name"
}

# Refuses a slot the running app task definition names in TOTP_ACTIVE_KEY or
# TOTP_PREVIOUS_KEY, so rotation never overwrites a key a task can still read
# or write. An AWS error aborts the script (set -e; no fallback on failure),
# so a credential, region, or throttling problem never reads as "no service".
# Only a call that succeeds and reports the app service does not exist yet
# (empty services list with a MISSING failure) skips the task check.
totp_key() { # a|b
  local slot=$1 resp task_def
  case $slot in
  a | b) ;;
  *)
    echo "usage: $0 totp-key <a|b>" >&2
    return 1
    ;;
  esac
  resp=$(aws ecs describe-services --region "$region" --cluster "$cluster" --services "$cluster-app")
  task_def=$(jq -r '.services[0].taskDefinition // empty' <<<"$resp")
  if [[ -n $task_def ]]; then
    local named
    named=$(aws ecs describe-task-definition --region "$region" --task-definition "$task_def" \
      --query "length(taskDefinition.containerDefinitions[?name=='server'].secrets[] | [?ends_with(valueFrom, '/totp/key-$slot')])" \
      --output text)
    if [[ $named != 0 ]]; then
      echo "refusing: the running app task definition names slot $slot" >&2
      return 1
    fi
  elif [[ $(jq -r '.services | length' <<<"$resp") != 0 || $(jq -r '.failures[0].reason // empty' <<<"$resp") != MISSING ]]; then
    echo "could not confirm the app service state; refusing to guess" >&2
    return 1
  fi
  write_totp_key "$slot"
}

case "${1-}" in
totp-key)
  totp_key "${2-}"
  ;;
"")
  put db/migrator-password SecureString hex48
  put db/app-password SecureString hex48
  put auth-email/active-key-id String keyid
  put auth-email/active-key SecureString key32
  put password-rate-hmac-key SecureString key32
  ;;
*)
  echo "usage: $0 [totp-key <a|b>]" >&2
  exit 1
  ;;
esac
