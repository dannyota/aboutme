# shellcheck shell=bash
# Sourced by deploy.sh. Runs a read-only public-root audit from the candidate
# server image. It needs $candidate, $cluster, $commit, $region, $repo, $tag,
# $work, current_def(), digest(), provenance_verify(), aws_(), and say().

# shellcheck disable=SC2154
public_root_check() { # root
  local root=$1 d task revision code status started_by client_token
  public_root_valid "$root" || { say "invalid public root '$root'"; return 1; }
  ((public_root_check_attempt < 100)) || { say "too many public-root checks in one operation"; return 1; }
  public_root_check_attempt=$((public_root_check_attempt + 1))
  started_by="root-${operation_id:0:20}-$public_root_check_attempt"
  client_token="root-$operation_id-$public_root_check_attempt"

  d=$(digest server) || { say "could not resolve the candidate server image"; return 1; }
  [[ $d == sha256:* ]] || { say "no server image for $tag"; return 1; }
  provenance_verify server "$d" "$tag" "$commit" || return 1

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
    | with_entries(select(.value != null))' >"$work/public-root-check.json" || {
    say "could not build the public-root check task definition"
    return 1
  }

  fence_checkpoint || return 1
  revision=$(aws_ ecs register-task-definition --cli-input-json "file://$work/public-root-check.json" \
    --query taskDefinition.taskDefinitionArn --output text) || {
    say "could not register the public-root check task definition"
    return 1
  }
  [[ $revision == arn:* ]] || { say "public-root check task definition did not register"; return 1; }

  fence_checkpoint || return 1
  # Set pending before RunTask. A lost response cannot prove no task started.
  public_root_pending=1
  public_root_started_by=$started_by
  say "public-root check correlation: started-by $started_by"
  task=$(aws_ ecs run-task --cluster "$cluster" --launch-type EC2 --task-definition "$revision" \
    --started-by "$started_by" --client-token "$client_token" \
    --overrides "$(jq -cn --arg root "$root" '{containerOverrides:[{name:"jobs",command:["check-public-root",$root]}]}')" \
    --query 'tasks[0].taskArn' --output text) || { say "public-root check did not start"; return 1; }
  [[ $task == arn:* ]] || { say "public-root check did not start"; return 1; }
  oneshot_task=$task
  say "public-root check task: $task"
  aws_ ecs wait tasks-stopped --cluster "$cluster" --tasks "$task" || {
    say "public-root check did not stop"
    return 1
  }
  status=$(aws_ ecs describe-tasks --cluster "$cluster" --tasks "$task" \
    --query 'tasks[0].lastStatus' --output text) || {
    say "could not confirm the public-root check stopped"
    return 1
  }
  [[ $status == STOPPED ]] || { say "public-root check is still $status"; return 1; }
  public_root_pending=0
  public_root_started_by=""
  oneshot_task=""
  # shellcheck disable=SC2016 # JMESPath quotes the jobs container name.
  code=$(aws_ ecs describe-tasks --cluster "$cluster" --tasks "$task" \
    --query 'tasks[0].containers[?name==`jobs`].exitCode | [0]' --output text) || {
    say "could not read the public-root check result"
    return 1
  }
  case $code in
    0) printf '{"resumeOccupied":false,"tombstoneOccupied":false}\n' ;;
    10) printf '{"resumeOccupied":true,"tombstoneOccupied":false}\n' ;;
    11) printf '{"resumeOccupied":false,"tombstoneOccupied":true}\n' ;;
    12) printf '{"resumeOccupied":true,"tombstoneOccupied":true}\n' ;;
    *) say "public-root check exited with $code"; return 1 ;;
  esac
  return "$code"
}
