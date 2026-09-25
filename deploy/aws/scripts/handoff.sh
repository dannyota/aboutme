# Sourced by deploy.sh. Starts and stops the app and maintenance services so
# that something always accepts connections on host port 443
# (docs/design/single-host-production.md, "Deploy"). Needs $cluster, say(),
# aws_, and (for maintenance_up) the revision map already defined.
#
# Both services run Caddy with host networking, and Caddy binds every TCP
# listener with SO_REUSEPORT, so a second Caddy joins the first on port 443
# and the kernel spreads new connections across both. Neither task definition
# declares a port mapping, so ECS places one service's task beside the
# other's. Every handoff therefore starts the incoming service and confirms
# its task runs before it stops the outgoing one.

# Seconds a started task must stay up before confirm_running inspects it. A
# Caddy that cannot bind or load its config exits within this hold.
handoff_hold=${DEPLOY_HANDOFF_HOLD:-5}

# Scaling a service to zero also waits for its actual tasks to stop, not just
# for the service to report stable, so the app never writes during a migration
# and a stop is proven before the caller reports it. A scale-down is never
# fence-checkpointed: it is always safe to attempt, and gating it could strand
# a stop half-done.
scale_to_zero_and_wait() { # service
  local task running_tasks stopping_tasks
  local -a tasks=()
  local -A seen=()
  # ListTasks defaults to desired RUNNING. Capture those tasks before lowering
  # the count, then capture desired STOPPED tasks after it. The second list
  # includes a task that was already stopping or was placed just before the
  # count update, so every task of the service is waited out.
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

# Runs one task of the given revision and confirms it. A single update-service
# call that changes both the task definition and a zero count makes ECS scale
# the service's current deployment first, so it starts a task of the previous
# revision and then replaces it. A stopped service is therefore pointed at the
# revision while it runs nothing, and only then scaled up. A service already
# running another revision (for example maintenance left up by a failed
# deploy) is updated in one call; ECS then stops its task before the
# replacement starts, so that one case can leave a short gap on port 443.
start_service() { # service revision
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

# Proves that the service runs exactly one task, of the given revision, with
# every container running. The app's Caddy container starts only after the
# server container reports healthy, so a running app task already accepts
# connections on port 443.
confirm_running() { # service revision
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

# maintenance_up starts the just-registered revision, so a release's Caddy
# image takes effect during the window. It is never fence-checkpointed:
# Caddy's maintenance page carries no release or enrollment logic, and
# restore() relies on it as a safety net that must stay reachable even when a
# genuine lock loss blocks the app itself.
maintenance_up() { start_service aboutme-prod-maintenance "${revision[maintenance]}"; }
maintenance_down() { scale_to_zero_and_wait aboutme-prod-maintenance; }
app_up() { start_service aboutme-prod-app "$1"; } # revision
app_down() { scale_to_zero_and_wait aboutme-prod-app; }
