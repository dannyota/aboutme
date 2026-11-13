#!/usr/bin/env bash
set -euo pipefail
region=ap-southeast-1
cluster=aboutme-prod
prefix=/aboutme/prod

exists() {
  [[ $(aws ssm get-parameters --region "$region" --names "$1" \
    --query 'length(Parameters)' --output text) == 1 ]]
}

put() {
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

write_totp_key() {
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

totp_key() {
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
  put view-pass-key SecureString key32
  ;;
*)
  echo "usage: $0 [totp-key <a|b>]" >&2
  exit 1
  ;;
esac
