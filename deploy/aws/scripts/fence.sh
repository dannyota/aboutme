# shellcheck shell=bash

fence_table=aboutme-prod-release-fence
operator_role=aboutme-prod-operator
deploy_role=aboutme-prod-deploy
fence_epoch=4002
fence_epoch_totp=4007
fence_epoch_signin=6026

base_arn=$(aws sts get-caller-identity --query Arn --output text) ||
  { say "could not identify the base AWS caller"; exit 1; }
[[ $base_arn =~ ^arn:aws:(iam::[0-9]+:(user|role)/|sts::[0-9]+:assumed-role/) ]] ||
  { say "the base AWS caller '$base_arn' is not an IAM user, role, or assumed-role session"; exit 1; }
account_id=$(aws sts get-caller-identity --query Account --output text) ||
  { say "could not resolve the AWS account"; exit 1; }
say "base caller: $base_arn"

user_config=${AWS_CONFIG_FILE:-$HOME/.aws/config}
base_profile=${AWS_PROFILE:-default}
session_name="aboutme-deploy-$operation_kind-$$"
aws_config=$work/aws-config
{
  [[ -f $user_config ]] && cat "$user_config"
  printf '\n[profile fence-operator]\nrole_arn = arn:aws:iam::%s:role/%s\nsource_profile = %s\nrole_session_name = %s\nregion = %s\n' \
    "$account_id" "$operator_role" "$base_profile" "$session_name" "$region"
  printf '\n[profile fence-deploy]\nrole_arn = arn:aws:iam::%s:role/%s\nsource_profile = fence-operator\nrole_session_name = %s\nregion = %s\n' \
    "$account_id" "$deploy_role" "$session_name" "$region"
} >"$aws_config" || true
chmod 600 "$aws_config"

# AWS CLI credential env vars outrank a profile's role_arn, so they are unset here (AWS CLI configuration docs, precedence).
aws_dep_() {
  local r=$1
  shift
  env -u AWS_ACCESS_KEY_ID -u AWS_SECRET_ACCESS_KEY -u AWS_SESSION_TOKEN \
    AWS_CONFIG_FILE="$aws_config" AWS_PROFILE=fence-deploy aws --region "$r" "$@"
}
aws_() { aws_dep_ "$region" "$@"; }
aws_site_alarm_() { aws_dep_ "$site_alarm_region" "$@"; }
aws_op_() {
  env -u AWS_ACCESS_KEY_ID -u AWS_SECRET_ACCESS_KEY -u AWS_SESSION_TOKEN \
    AWS_CONFIG_FILE="$aws_config" AWS_PROFILE=fence-operator aws --region "$region" "$@"
}

verify_role() {
  local arn
  arn=$(env -u AWS_ACCESS_KEY_ID -u AWS_SECRET_ACCESS_KEY -u AWS_SESSION_TOKEN \
    AWS_CONFIG_FILE="$aws_config" AWS_PROFILE="$1" aws --region "$region" \
    sts get-caller-identity --query Arn --output text) || { say "could not verify the $1 identity"; exit 1; }
  [[ $arn == *":assumed-role/$2/"* ]] ||
    { say "the $1 identity is '$arn', not an assumed-role session of $2"; exit 1; }
}
verify_role fence-operator "$operator_role"
verify_role fence-deploy "$deploy_role"

fence_min=0
fence_min_tag=v0.0.0
lock_held=0
operation_id=""

task_def_release_number() {
  local def n
  def=$(describe_task_def "$1") || return 1
  n=$(jq -r '.containerDefinitions[] | select(.name == "server")
    | .environment[]? | select(.name == "DEPLOY_RELEASE_NUMBER") | .value' <<<"$def" 2>/dev/null)
  [[ $n =~ ^[0-9]+$ ]] && echo "$n" || echo 0
}

fence_read() {
  local item item_present release tag_ service running running_count desired_count deployed=1 enrolled totp_enrolled def
  item=$(aws_op_ dynamodb get-item --table-name "$fence_table" \
    --key '{"id":{"S":"application"}}' --consistent-read --output json) ||
    { say "could not read the release fence"; return 1; }
  item_present=$(jq -r '.Item // empty' <<<"$item")
  if [[ -z $item_present ]]; then
    fence_min=0 fence_min_tag=v0.0.0
  else
    release=$(jq -r '.Item.minimum_release.N // empty' <<<"$item")
    tag_=$(jq -r '.Item.minimum_tag.S // empty' <<<"$item")
    [[ $release =~ ^[0-9]+$ && $(release_number "$tag_") == "$release" ]] ||
      { say "the release fence item is malformed"; return 1; }
    fence_min=$release fence_min_tag=$tag_
  fi
  service=$(aws_ ecs describe-services --cluster "$cluster" --services aboutme-prod-app --output json) ||
    { say "could not read the running app service"; return 1; }
  running=$(jq -r '.services[0].taskDefinition // empty' <<<"$service")
  running_count=$(jq -r '.services[0].runningCount // 0' <<<"$service")
  desired_count=$(jq -r '.services[0].desiredCount // 0' <<<"$service")
  if [[ -n $running ]] && ((running_count == 0)) && ((desired_count == 0)); then
    describe_task_def "$running" | jq -e '.containerDefinitions[] | select(.name == "server")
      | (.environment // [])[]? | select(.name == "DEPLOY_RELEASE_TAG")' >/dev/null 2>&1 || deployed=0
  fi
  if ((first)); then
    ((!deployed)) ||
      { say "--first-deploy but aboutme-prod-app already runs a deployed revision ($running)"; return 1; }
    return 0
  fi
  enrolled="" totp_enrolled="" signin_enabled=""
  if ((deployed)); then
    def=$(describe_task_def "$running") || { say "could not read the running app task definition"; return 1; }
    enrolled=$(jq -r '.containerDefinitions[] | select(.name == "server")
      | .environment[]? | select(.name == "PASSKEY_ENROLLMENT_ENABLED") | .value' <<<"$def" 2>/dev/null) ||
      { say "could not read the running app task definition"; return 1; }
    totp_enrolled=$(jq -r '.containerDefinitions[] | select(.name == "server")
      | .environment[]? | select(.name == "TOTP_ENROLLMENT_ENABLED") | .value' <<<"$def" 2>/dev/null) ||
      { say "could not read the running app task definition"; return 1; }
    signin_enabled=$(jq -r '.containerDefinitions[] | select(.name == "server")
      | .environment[]? | select(.name == "SIGN_IN_TO_VIEW_ENABLED") | .value' <<<"$def" 2>/dev/null) ||
      { say "could not read the running app task definition"; return 1; }
  fi
  if [[ -z $item_present && -n $enrolled && $enrolled != false ]]; then
    say "the release fence is missing and the running app does not prove passkey enrollment off"
    return 1
  fi
  if [[ -z $item_present && -n $totp_enrolled && $totp_enrolled != false ]]; then
    say "the release fence is missing and the running app does not prove TOTP enrollment off"
    return 1
  fi
  if [[ -z $item_present && -n $signin_enabled && $signin_enabled != false ]]; then
    say "the release fence is missing and the running app does not prove sign in to view off"
    return 1
  fi
  if ((fence_min < fence_epoch)) && [[ $enrolled == true ]]; then
    say "the release fence is below v0.4.2 but the running app has passkey enrollment on"
    return 1
  fi
  if ((fence_min < fence_epoch_totp)) && [[ $totp_enrolled == true ]]; then
    say "the release fence is below v0.4.7 but the running app has TOTP enrollment on"
    return 1
  fi
  if ((fence_min < fence_epoch_signin)) && [[ $signin_enabled == true ]]; then
    say "the release fence is below v0.6.26 but the running app has sign in to view on"
    return 1
  fi
}

fence_lock() {
  local now=$(date -u +%Y-%m-%dT%H:%M:%SZ) values item
  local oid=$(head -c32 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=')
  values="{\":o\":{\"S\":\"$oid\"},\":k\":{\"S\":\"$operation_kind\"},\":s\":{\"S\":\"$now\"},\":z\":{\"N\":\"0\"},\":zt\":{\"S\":\"v0.0.0\"},\":c\":{\"N\":\"$candidate\"}}"
  if aws_ dynamodb update-item --table-name "$fence_table" --key '{"id":{"S":"application"}}' \
    --update-expression 'SET operation_id=:o, operation_kind=:k, operation_started_at=:s, operation_checked_at=:s, minimum_release=if_not_exists(minimum_release,:z), minimum_tag=if_not_exists(minimum_tag,:zt)' \
    --condition-expression 'attribute_not_exists(operation_id) AND (attribute_not_exists(minimum_release) OR minimum_release <= :c)' \
    --expression-attribute-values "$values" >/dev/null 2>"$work/lock.err"; then
    operation_id=$oid lock_held=1
    return 0
  fi
  item=$(aws_ dynamodb get-item --table-name "$fence_table" --key '{"id":{"S":"application"}}' \
    --consistent-read --output json) || { say "lock acquisition is uncertain and could not be resolved"; return 1; }
  if [[ $(jq -r '.Item.operation_id.S // empty' <<<"$item") == "$oid" ]]; then
    operation_id=$oid lock_held=1
    say "lock acquisition raced its own retry; a strong read proved this process owns it"
    return 0
  fi
  if grep -q ConditionalCheckFailedException "$work/lock.err"; then
    say "the fence already has an operation, or $tag is below its minimum"
  else
    say "lock acquisition is uncertain and this process does not own it; not retrying"
  fi
  return 1
}

fence_checkpoint() {
  local now=$(date -u +%Y-%m-%dT%H:%M:%SZ) attempt=1
  while :; do
    aws_ dynamodb update-item --table-name "$fence_table" --key '{"id":{"S":"application"}}' \
      --update-expression 'SET operation_checked_at=:s' \
      --condition-expression 'operation_id=:o AND (attribute_not_exists(minimum_release) OR minimum_release <= :c)' \
      --expression-attribute-values "{\":s\":{\"S\":\"$now\"},\":o\":{\"S\":\"$operation_id\"},\":c\":{\"N\":\"$candidate\"}}" \
      >/dev/null 2>"$work/checkpoint.err" && return 0
    grep -q ConditionalCheckFailedException "$work/checkpoint.err" && break
    ((attempt++ < 2)) || break
  done
  say "fence checkpoint failed; the operation lock or release floor no longer holds"
  return 1
}

fence_release() {
  local item
  if aws_ dynamodb update-item --table-name "$fence_table" --key '{"id":{"S":"application"}}' \
    --update-expression 'REMOVE operation_id, operation_kind, operation_started_at, operation_checked_at' \
    --condition-expression 'operation_id=:o' \
    --expression-attribute-values "{\":o\":{\"S\":\"$operation_id\"}}" \
    >/dev/null 2>"$work/release.err"; then
    lock_held=0
    return 0
  fi
  item=$(aws_ dynamodb get-item --table-name "$fence_table" --key '{"id":{"S":"application"}}' \
    --consistent-read --output json) || return 1
  [[ -n $(jq -r '.Item.operation_id.S // empty' <<<"$item") ]] || { lock_held=0; return 0; }
  return 1
}

fence_raise() {
  local now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  aws_ dynamodb update-item --table-name "$fence_table" --key '{"id":{"S":"application"}}' \
    --update-expression 'SET minimum_release=:c, minimum_tag=:t, updated_at=:s' \
    --condition-expression 'operation_id=:o AND (attribute_not_exists(minimum_release) OR minimum_release <= :c)' \
    --expression-attribute-values "{\":c\":{\"N\":\"$candidate\"},\":t\":{\"S\":\"$tag\"},\":s\":{\"S\":\"$now\"},\":o\":{\"S\":\"$operation_id\"}}" \
    >/dev/null 2>"$work/raise.err" && return 0
  if grep -q ConditionalCheckFailedException "$work/raise.err"; then
    say "fence raise failed; the condition did not hold"
  else
    say "fence raise is uncertain; rerun --activate"
  fi
  return 1
}
