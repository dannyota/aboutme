# shellcheck shell=bash
# Sourced by deploy.sh. Implements deploy.sh --account-delete, which runs the
# server's out-of-band account-delete command as a one-shot production task
# (docs/design/showcase.md, "Derived values and reports"; docs/runbooks/account-delete.md).
# The task is the live tag's jobs family at that tag's provenance-checked
# server image with a command override, the same shape as the public-root
# audit. It changes no service, schedule, snapshot, or image; the server
# command deletes the account through the same path as a self-delete.
#
# Needs $region, $cluster, $work, $tag, $commit, $candidate, $repo,
# $account_delete_args, $operation_id, say(), digest(), current_def(), describe_task_def(), aws_(),
# task_def_release_number(), public_root_valid(), fence_lock, fence_checkpoint,
# and provenance_verify() already defined by deploy.sh, fence.sh, and
# provenance.sh.

account_delete_log_group=/aboutme/prod

# Checks the subcommand and its arguments before any AWS call. The slug uses
# the public slug grammar; the user ID is a lowercase UUID.
account_delete_validate() { # subcommand [args...]
  local sub=${1:-} slug=${2:-} user=${3:-}
  case "$sub:$#" in
    show:2 | confirm:3) ;;
    *) say "usage: --account-delete <tag> show <slug> | confirm <slug> <user-id>"; exit 2 ;;
  esac
  public_root_valid "$slug" || { say "invalid slug '$slug'"; exit 2; }
  [[ $sub == show || $user =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]] ||
    { say "invalid user ID '$user'"; exit 2; }
}

# The deploy and operator roles cannot read CloudWatch Logs, so the owner's own
# login reads the task log; it never assumes either role and never writes.
# Every run that reaches the database ends stdout with this exact line, so a
# log without it is incomplete (lag, a missed page, or an early failure).
account_delete_end_marker="account-delete: end"

# Reads the stream from the start, following nextForwardToken until the token
# repeats, and retries up to five times for a log still being delivered. Prints
# the lines read, never the marker line. Returns 1 when the marker never
# appeared; what was read is still printed.
account_delete_log() { # task id
  local stream=jobs/jobs/$1 attempt page token next buf="" pages
  for ((attempt = 1; attempt <= 5; attempt++)); do
    buf="" token="" pages=0
    while ((pages++ < 50)); do
      page=$(aws --region "$region" logs get-log-events --log-group-name "$account_delete_log_group" \
        --log-stream-name "$stream" --start-from-head --output json ${token:+--next-token "$token"} 2>/dev/null) || break
      buf+=$(jq -r '.events[].message' <<<"$page" 2>/dev/null | LC_ALL=C tr -cd '\11\12\40-\176')$'\n' || true
      next=$(jq -r '.nextForwardToken // empty' <<<"$page" 2>/dev/null) || break
      [[ -n $next && $next != "$token" ]] || break
      token=$next
    done
    if grep -qxF "$account_delete_end_marker" <<<"$buf"; then
      grep -vxF "$account_delete_end_marker" <<<"$buf" | grep -v '^$' || true
      return 0
    fi
    ((attempt == 5)) || sleep "${DEPLOY_SMOKE_DELAY:-3}"
  done
  grep -v '^$' <<<"$buf" || true
  return 1
}

account_delete_run() {
  local d task revision code status started_by client_token
  d=$(digest server) || { say "could not resolve the server image"; exit 1; }
  [[ $d == sha256:* ]] || { say "no server image for $tag"; exit 1; }
  provenance_verify server "$d" "$tag" "$commit" || exit 1
  current_def jobs | jq \
    --arg image "ghcr.io/$repo-server@$d" --arg tag "$tag" --argjson rel "$candidate" '
    if (.containerDefinitions | map(select(.name == "jobs")) | length) == 1 then
      .containerDefinitions |= map(
        if .name == "jobs" then .image = $image else . end
        | .environment = (((.environment // []) | map(select(.name != "DEPLOY_RELEASE_TAG" and .name != "DEPLOY_RELEASE_NUMBER")))
            + [{name:"DEPLOY_RELEASE_TAG",value:$tag},{name:"DEPLOY_RELEASE_NUMBER",value:($rel|tostring)}])
      )
    else error("jobs task must contain exactly one jobs container") end
    | {family, taskRoleArn, executionRoleArn, networkMode, containerDefinitions,
       requiresCompatibilities, volumes, placementConstraints, cpu, memory,
       pidMode, ipcMode, proxyConfiguration, inferenceAccelerators,
       ephemeralStorage, runtimePlatform, enableFaultInjection, tags}
    | with_entries(select(.value != null))' >"$work/account-delete.json" ||
    { say "could not build the account-delete task definition"; exit 1; }

  fence_lock || exit 1
  fence_checkpoint || exit 1

  # Under the lock, the review must run against the live release: a task at
  # another tag could read or write rows its schema does not match.
  local service service_td running_count desired_count service_release
  service=$(aws_ ecs describe-services --cluster "$cluster" --services aboutme-prod-app --output json) ||
    { say "could not read the running app service"; exit 1; }
  service_td=$(jq -r '.services[0].taskDefinition // empty' <<<"$service")
  running_count=$(jq -r '.services[0].runningCount // 0' <<<"$service")
  desired_count=$(jq -r '.services[0].desiredCount // 0' <<<"$service")
  [[ -n $service_td ]] && ((running_count > 0)) && ((running_count == desired_count)) ||
    { say "the running app service is not stable; account-delete requires a healthy running app"; exit 1; }
  service_release=$(task_def_release_number "$service_td") || { say "could not verify the running app's release"; exit 1; }
  [[ $service_release == "$candidate" ]] ||
    { say "the running app is release $service_release, not $tag"; exit 1; }

  revision=$(aws_ ecs register-task-definition --cli-input-json "file://$work/account-delete.json" \
    --query taskDefinition.taskDefinitionArn --output text) ||
    { say "could not register the account-delete task definition"; exit 1; }
  [[ $revision == arn:* ]] || { say "account-delete task definition did not register"; exit 1; }

  fence_checkpoint || exit 1
  started_by="acctdel-${operation_id:0:20}"
  client_token="acctdel-$operation_id"
  # From RunTask until ECS proves the task stopped, the lock is deliberately
  # not released on exit: a lost response or a signal cannot prove the task
  # is not running.
  # shellcheck disable=SC2034 # read by on_exit in deploy.sh
  lock_held=0
  say "account-delete correlation: started-by $started_by; the operation lock stays held until the task stops"
  task=$(aws_ ecs run-task --cluster "$cluster" --launch-type EC2 --task-definition "$revision" \
    --started-by "$started_by" --client-token "$client_token" \
    --overrides "$(jq -cn '{containerOverrides:[{name:"jobs",command:(["account-delete"] + $ARGS.positional)}]}' --args "${account_delete_args[@]}")" \
    --query 'tasks[0].taskArn' --output text) || { say "account-delete did not start"; exit 1; }
  [[ $task == arn:* ]] || { say "account-delete did not start"; exit 1; }
  say "account-delete task: $task"
  aws_ ecs wait tasks-stopped --cluster "$cluster" --tasks "$task" ||
    { say "account-delete did not stop"; exit 1; }
  status=$(aws_ ecs describe-tasks --cluster "$cluster" --tasks "$task" \
    --query 'tasks[0].lastStatus' --output text) || { say "could not confirm the account-delete task stopped"; exit 1; }
  [[ $status == STOPPED ]] || { say "account-delete is still $status"; exit 1; }
  # shellcheck disable=SC2034 # read by on_exit in deploy.sh
  lock_held=1
  # shellcheck disable=SC2016 # JMESPath quotes the jobs container name.
  code=$(aws_ ecs describe-tasks --cluster "$cluster" --tasks "$task" \
    --query 'tasks[0].containers[?name==`jobs`].exitCode | [0]' --output text) || code=unknown

  local complete=1
  account_delete_log "${task##*/}" || complete=0
  case $code in
    0) ((complete)) || { say "the task log has no end marker, so the result is incomplete; run show <slug> again: not found means the account is deleted"; exit 1; } ;;
    4) say "not found: no account holds that slug (unknown, tombstoned, or already deleted); nothing deleted"; exit 4 ;;
    5) say "mismatch: the slug is not owned by that user ID; nothing deleted; run show <slug> again"; exit 5 ;;
    *) say "account-delete exited with $code"; exit 1 ;;
  esac
  say "account-delete ${account_delete_args[0]} done for $tag"
  exit 0
}
