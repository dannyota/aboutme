# shellcheck shell=bash
# Sourced by deploy.sh. Implements deploy.sh --showcase-review, which runs the
# server's out-of-band showcase-review command as a one-shot production task
# (docs/design/showcase.md, "Review"; docs/runbooks/showcase-review.md). The
# task is the live tag's jobs family at that tag's provenance-checked server
# image with a command override, the same shape as the public-root audit. It
# changes no service, schedule, snapshot, or image.
#
# Needs $region, $cluster, $work, $tag, $commit, $candidate, $repo,
# $showcase_args, $operation_id, say(), digest(), current_def(), describe_task_def(), aws_(),
# task_def_release_number(), public_root_valid(), fence_lock, fence_checkpoint,
# and provenance_verify() already defined by deploy.sh, fence.sh, and
# provenance.sh.

showcase_log_group=/aboutme/prod

# Checks the subcommand and its arguments before any AWS call. The slug uses
# the public slug grammar; the key is a 16-digit lowercase hex review key.
showcase_validate() { # subcommand [args...]
  local sub=${1:-} slug=${2:-} key=${3:-}
  case "$sub:$#" in
    pending:1) return 0 ;;
    show:2 | approve:3 | decline:3) ;;
    *) say "usage: --showcase-review <tag> pending | show <slug> | approve <slug> <key> | decline <slug> <key>"; exit 2 ;;
  esac
  public_root_valid "$slug" || { say "invalid slug '$slug'"; exit 2; }
  [[ $sub == show || $key =~ ^[0-9a-f]{16}$ ]] || { say "invalid review key '$key'"; exit 2; }
}

# The deploy and operator roles cannot read CloudWatch Logs, so the owner's own
# login reads the task log; it never assumes either role and never writes.
showcase_log() { # task id -> prints the task's log lines on stdout
  local stream=jobs/jobs/$1 out attempt
  for ((attempt = 1; attempt <= 5; attempt++)); do
    out=$(aws --region "$region" logs get-log-events --log-group-name "$showcase_log_group" \
      --log-stream-name "$stream" --start-from-head --output json 2>/dev/null |
      jq -r '.events[].message' 2>/dev/null | LC_ALL=C tr -cd '\11\12\40-\176') || out=""
    if [[ -n $out ]]; then
      printf '%s\n' "$out"
      return 0
    fi
    ((attempt == 5)) || sleep "${DEPLOY_SMOKE_DELAY:-3}"
  done
  return 1
}

showcase_run() {
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
    | with_entries(select(.value != null))' >"$work/showcase-review.json" ||
    { say "could not build the showcase-review task definition"; exit 1; }

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
    { say "the running app service is not stable; showcase review requires a healthy running app"; exit 1; }
  service_release=$(task_def_release_number "$service_td") || { say "could not verify the running app's release"; exit 1; }
  [[ $service_release == "$candidate" ]] ||
    { say "the running app is release $service_release, not $tag"; exit 1; }

  revision=$(aws_ ecs register-task-definition --cli-input-json "file://$work/showcase-review.json" \
    --query taskDefinition.taskDefinitionArn --output text) ||
    { say "could not register the showcase-review task definition"; exit 1; }
  [[ $revision == arn:* ]] || { say "showcase-review task definition did not register"; exit 1; }

  fence_checkpoint || exit 1
  started_by="showcase-${operation_id:0:20}"
  client_token="showcase-$operation_id"
  # From RunTask until ECS proves the task stopped, the lock is deliberately
  # not released on exit: a lost response or a signal cannot prove the task
  # is not running.
  # shellcheck disable=SC2034 # read by on_exit in deploy.sh
  lock_held=0
  say "showcase-review correlation: started-by $started_by; the operation lock stays held until the task stops"
  task=$(aws_ ecs run-task --cluster "$cluster" --launch-type EC2 --task-definition "$revision" \
    --started-by "$started_by" --client-token "$client_token" \
    --overrides "$(jq -cn '{containerOverrides:[{name:"jobs",command:(["showcase-review"] + $ARGS.positional)}]}' --args "${showcase_args[@]}")" \
    --query 'tasks[0].taskArn' --output text) || { say "showcase-review did not start"; exit 1; }
  [[ $task == arn:* ]] || { say "showcase-review did not start"; exit 1; }
  say "showcase-review task: $task"
  aws_ ecs wait tasks-stopped --cluster "$cluster" --tasks "$task" ||
    { say "showcase-review did not stop"; exit 1; }
  status=$(aws_ ecs describe-tasks --cluster "$cluster" --tasks "$task" \
    --query 'tasks[0].lastStatus' --output text) || { say "could not confirm the showcase-review task stopped"; exit 1; }
  [[ $status == STOPPED ]] || { say "showcase-review is still $status"; exit 1; }
  # shellcheck disable=SC2034 # read by on_exit in deploy.sh
  lock_held=1
  # shellcheck disable=SC2016 # JMESPath quotes the jobs container name.
  code=$(aws_ ecs describe-tasks --cluster "$cluster" --tasks "$task" \
    --query 'tasks[0].containers[?name==`jobs`].exitCode | [0]' --output text) || code=unknown

  if ! showcase_log "${task##*/}"; then
    say "could not read the task log $showcase_log_group jobs/jobs/${task##*/}"
    [[ $code == 0 ]] && say "the task exited 0 but its result is unknown; run show <slug> to confirm"
    exit 1
  fi
  [[ $code == 0 ]] || { say "showcase-review exited with $code"; exit 1; }
  say "showcase-review ${showcase_args[0]} done for $tag"
  exit 0
}
