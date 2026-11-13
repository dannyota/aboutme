# shellcheck shell=bash

handoff_hold=${DEPLOY_HANDOFF_HOLD:-5}

scale_to_zero_and_wait() {
  local task running_tasks stopping_tasks
  local -a tasks=()
  local -A seen=()
  # ECS ListTasks defaults to desiredStatus RUNNING (ECS ListTasks API reference).
  running_tasks=$(aws_ ecs list-tasks --cluster "$cluster" --service-name "$1" --query 'taskArns[]' --output text) || return 1
  while IFS= read -r task; do
    if [[ -n $task && -z ${seen[$task]:-} ]]; then
      tasks+=("$task")
      seen[$task]=1
    fi
  done < <(tr '\t' '\n' <<<"$running_tasks")
  aws_ ecs update-service --cluster "$cluster" --service "$1" --desired-count 0 >/dev/null || return 1
  aws_ ecs wait services-stable --cluster "$cluster" --services "$1" || return 1
  stopping_tasks=$(aws_ ecs list-tasks --cluster "$cluster" --service-name "$1" --desired-status STOPPED --query 'taskArns[]' --output text) || return 1
  while IFS= read -r task; do
    if [[ -n $task && -z ${seen[$task]:-} ]]; then
      tasks+=("$task")
      seen[$task]=1
    fi
  done < <(tr '\t' '\n' <<<"$stopping_tasks")
  ((${#tasks[@]} == 0)) || aws_ ecs wait tasks-stopped --cluster "$cluster" --tasks "${tasks[@]}" || return 1
}

# ECS scales a zero-count service's current deployment first when one update-service call changes both revision and count (ECS UpdateService behaviour).
start_service() {
  local svc=$1 rev=$2 state td count
  state=$(aws_ ecs describe-services --cluster "$cluster" --services "$svc" \
    --query 'services[0].[taskDefinition,desiredCount]' --output text) || return 1
  read -r td count <<<"$state"
  [[ $count =~ ^[0-9]+$ ]] || { say "could not read the $svc service state"; return 1; }
  if [[ $td != "$rev" ]] && ((count == 0)); then
    aws_ ecs update-service --cluster "$cluster" --service "$svc" --task-definition "$rev" >/dev/null || return 1
    aws_ ecs wait services-stable --cluster "$cluster" --services "$svc" || return 1
    td=$rev
  fi
  if [[ $td != "$rev" ]]; then
    aws_ ecs update-service --cluster "$cluster" --service "$svc" \
      --task-definition "$rev" --desired-count 1 >/dev/null || return 1
  else
    aws_ ecs update-service --cluster "$cluster" --service "$svc" --desired-count 1 >/dev/null || return 1
  fi
  aws_ ecs wait services-stable --cluster "$cluster" --services "$svc" || return 1
  confirm_running "$svc" "$rev"
}

confirm_running() {
  local out
  local -a tasks=()
  sleep "$handoff_hold"
  out=$(aws_ ecs list-tasks --cluster "$cluster" --service-name "$1" --desired-status RUNNING \
    --query 'taskArns[]' --output text) || return 1
  read -r -a tasks <<<"$out"
  if ((${#tasks[@]} != 1)) || [[ ${tasks[0]} != arn:* ]]; then
    say "$1 has ${#tasks[@]} tasks wanting to run, want 1"
    return 1
  fi
  out=$(aws_ ecs describe-tasks --cluster "$cluster" --tasks "${tasks[0]}" --output json) || return 1
  jq -e --arg rev "$2" '.tasks | length == 1 and all(.[];
      .taskDefinitionArn == $rev and .lastStatus == "RUNNING"
      and (.containers | length > 0) and all(.containers[]; .lastStatus == "RUNNING"))' \
    <<<"$out" >/dev/null ||
    { say "$1 is not running its task of $2 with every container up"; return 1; }
}

maintenance_up() { start_service aboutme-prod-maintenance "${revision[maintenance]}"; }
maintenance_down() { scale_to_zero_and_wait aboutme-prod-maintenance; }
app_up() { start_service aboutme-prod-app "$1"; }
app_down() { scale_to_zero_and_wait aboutme-prod-app; }
