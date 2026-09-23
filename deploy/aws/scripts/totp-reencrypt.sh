# Sourced by deploy.sh. Implements deploy.sh --totp-key-reencrypt, the
# one-shot key re-encryption operation (docs/design/totp-key-management.md,
# "Rotation", and docs/design/passkey-release-fence.md, "Authenticator-app
# key re-encryption"). It builds and registers its own
# aboutme-prod-totp-reencrypt revision, the same way deploy.sh builds and
# registers migrate, jobs, and db-setup, so the task always runs the tag's
# server image under a DEPLOY_RELEASE_* stamp rather than OpenTofu's
# placeholder registration. It never registers or changes a service.
# Needs $region, $cluster, $work, $tag, $candidate, $repo, $fence_epoch_totp,
# $fence_min, say(), digest(), current_def(), describe_task_def(), aws_(),
# task_def_release_number(), fence_lock, and fence_checkpoint already defined
# by deploy.sh and fence.sh.

# Extracts TOTP_ACTIVE_KEY/TOTP_PREVIOUS_KEY valueFrom names from one
# container of a task definition, sorted so set order never causes a false
# mismatch. A missing previous slot is normal (see "Key ring") and yields one
# line rather than two.
totp_key_slots() { # task-definition JSON, container name
  jq -r --arg c "$2" '.containerDefinitions[] | select(.name == $c)
    | (.secrets // [])[] | select(.name == "TOTP_ACTIVE_KEY" or .name == "TOTP_PREVIOUS_KEY")
    | "\(.name)=\(.valueFrom)"' <<<"$1" | sort
}

totp_reencrypt_run() {
  ((fence_min >= fence_epoch_totp)) ||
    { say "the release fence is below v0.4.7; run --activate first"; exit 1; }

  local d
  d=$(digest server)
  [[ $d == sha256:* ]] || { say "no server image for $tag"; exit 1; }
  current_def totp-reencrypt | jq \
    --arg server "ghcr.io/$repo-server@$d" --arg tag "$tag" --argjson rel "$candidate" '
    .containerDefinitions |= map(
      .image = $server
      | .environment = (((.environment // []) | map(select(.name != "DEPLOY_RELEASE_TAG" and .name != "DEPLOY_RELEASE_NUMBER")))
          + [{name:"DEPLOY_RELEASE_TAG",value:$tag},{name:"DEPLOY_RELEASE_NUMBER",value:($rel|tostring)}]))
    | {family, taskRoleArn, executionRoleArn, networkMode, containerDefinitions,
       requiresCompatibilities, volumes}
    | with_entries(select(.value != null))' >"$work/totp-reencrypt.json"

  fence_lock || exit 1
  fence_checkpoint || exit 1

  # The running-app release and stability checks run only after the lock is
  # held, so a stale read can never pass them and then lose a race to a
  # concurrent deploy before the checkpoint below re-proves the lock.
  local service service_td running_count desired_count service_release
  service=$(aws_ ecs describe-services --cluster "$cluster" --services aboutme-prod-app --output json) ||
    { say "could not read the running app service"; exit 1; }
  service_td=$(jq -r '.services[0].taskDefinition // empty' <<<"$service")
  running_count=$(jq -r '.services[0].runningCount // 0' <<<"$service")
  desired_count=$(jq -r '.services[0].desiredCount // 0' <<<"$service")
  [[ -n $service_td ]] && ((running_count > 0)) && ((running_count == desired_count)) ||
    { say "the running app service is not stable; TOTP key re-encryption requires a healthy running app"; exit 1; }
  service_release=$(task_def_release_number "$service_td") || { say "could not verify the running app's release"; exit 1; }
  [[ $service_release == "$candidate" ]] ||
    { say "the running app is release $service_release, not $tag"; exit 1; }

  # Before registering: the new revision's key slots must be exactly the
  # running app's, so the task never seals a row under a key the running app
  # cannot read.
  local running_def new_def running_slots new_slots
  running_def=$(describe_task_def "$service_td") || { say "could not read the running app task definition"; exit 1; }
  new_def=$(cat "$work/totp-reencrypt.json")
  running_slots=$(totp_key_slots "$running_def" server)
  new_slots=$(totp_key_slots "$new_def" totp-reencrypt)
  [[ -n $new_slots && $new_slots == "$running_slots" ]] ||
    { say "the totp-reencrypt revision's key slots do not match the running app; run tofu apply first"; exit 1; }

  local revision
  revision=$(aws_ ecs register-task-definition --cli-input-json "file://$work/totp-reencrypt.json" \
    --query taskDefinition.taskDefinitionArn --output text)
  say "registered totp-reencrypt revision for $tag"

  fence_checkpoint || exit 1
  local task code
  task=$(aws_ ecs run-task --cluster "$cluster" --launch-type EC2 \
    --task-definition "$revision" --started-by deploy-totp-reencrypt \
    --query 'tasks[0].taskArn' --output text) || { say "totp-key-reencrypt did not start"; exit 1; }
  [[ $task == arn:* ]] || { say "totp-key-reencrypt did not start"; exit 1; }
  # One run may take 30 minutes (docs/design/totp-key-management.md), longer
  # than one ECS waiter (about 10 minutes), so the lock stays held until the
  # task stops or four waits pass. If the task is still running then, the
  # lock is deliberately kept and the operator is told why.
  local waited=0 last
  until aws_ ecs wait tasks-stopped --cluster "$cluster" --tasks "$task"; do
    waited=$((waited + 1))
    if ((waited >= 4)); then
      last=$(aws_ ecs describe-tasks --cluster "$cluster" --tasks "$task" \
        --query 'tasks[0].lastStatus' --output text) || last=unknown
      if [[ $last != STOPPED ]]; then
        # shellcheck disable=SC2034 # read by on_exit in deploy.sh
        lock_held=0
        say "totp-key-reencrypt still $last after about 40 minutes; the operation lock stays held until it stops"
        exit 1
      fi
      break
    fi
  done
  code=$(aws_ ecs describe-tasks --cluster "$cluster" --tasks "$task" \
    --query 'tasks[0].containers[0].exitCode' --output text)
  [[ $code == 0 ]] || { say "totp-key-reencrypt exited with $code"; exit 1; }
  say "totp-key-reencrypt done for $tag; counts are in the aboutme-prod-totp-reencrypt log stream"
}
