#!/usr/bin/env bash
# Deploys a tagged release to aboutme-prod. See docs/runbooks/production.md.
#
#   deploy.sh <tag>                  normal deploy
#   deploy.sh <tag> --first-deploy   also creates roles, grants, and logins
#   deploy.sh --rollback <tag>       earlier images, no snapshot, no migration
#
# Order: build revisions, check that every task secret exists, snapshot,
# register revisions, stop jobs and app, swap in the maintenance page, migrate,
# start web, swap maintenance back out, start app, re-enable jobs, smoke. The
# maintenance and app services bind the same host port, so exactly one of them
# is ever asked to run at once. The release-snapshot-sweep job deletes this
# script's tagged snapshots once they are more than 27 days old, before they
# reach 30.
#
# Any failure after the app stops restores the previous app before a migration,
# or leaves maintenance up after a database task may have run. A failed ECS
# state check stops recovery before it starts the competing service. After a
# migration, if the new app never confirmed healthy, it is scaled back to 0
# before maintenance comes up; if it did confirm healthy and only the
# schedule-enable step failed afterward, tearing down a proven-healthy app
# would be a self-inflicted outage, so it stays up, maintenance stays down,
# and the script names the schedules to fix by hand.
set -euo pipefail

region=ap-southeast-1
cluster=aboutme-prod
group=aboutme-prod-jobs
repo=dannyota/aboutme
families=(app web maintenance migrate jobs db-setup)
# release-snapshot-sweep deletes snapshots with this tag within 30 days.
snapshot_tag_key=aboutme:created-by
snapshot_tag_value=deploy.sh

usage() {
  echo "usage: deploy.sh <tag> [--first-deploy] | deploy.sh --rollback <tag>" >&2
  exit 2
}
first=0
rollback=0
case "$#:${1:-}:${2:-}" in
  2:--rollback:?*) rollback=1 tag=$2 ;;
  1:[!-]*:) tag=$1 ;;
  2:[!-]*:--first-deploy) first=1 tag=$1 ;;
  *) usage ;;
esac

aws_() { aws --region "$region" "$@"; }
say() { printf 'deploy: %s\n' "$*" >&2; }
work=$(mktemp -d)

# Shared by the mid-deploy maintenance-page check and the final smoke checks.
smoke_attempts=5
smoke_delay=${DEPLOY_SMOKE_DELAY:-3}
retry() { # command...
  local attempt
  for ((attempt = 1; attempt <= smoke_attempts; attempt++)); do
    "$@" && return 0
    ((attempt == smoke_attempts)) || sleep "$smoke_delay"
  done
  return 1
}

# phase: prepare -> changing -> finished. oneshot_task is set while a database
# task may be running. migration_may_be_applied is set before requesting a
# migration task because an accepted request can lose its response.
# app_start_requested and app_stable track the new app
# through section 8, so restore() can tell "asked to start, health unknown"
# from "confirmed healthy" and never leave both app and maintenance wanting
# host port 443. schedules_enabled tracks the enable loop the same way.
phase=prepare
oneshot_task=""
migration_may_be_applied=0
app_start_requested=0
app_stable=0
schedules_enabled=()
on_exit() {
  local status=$?
  if ((status != 0)) && [[ $phase == changing ]]; then
    restore
  fi
  rm -rf "$work"
  exit "$status"
}
trap on_exit EXIT

digest() { # image name -> sha256:...
  local token
  token=$(curl -fsS "https://ghcr.io/token?scope=repository:$repo-$1:pull&service=ghcr.io" | jq -r .token)
  curl -fsSI -H "Authorization: Bearer $token" \
    -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json' \
    "https://ghcr.io/v2/$repo-$1/manifests/$tag" |
    tr -d '\r' | awk -F': ' 'tolower($1) == "docker-content-digest" { print $2 }'
}

current_def() {
  aws_ ecs describe-task-definition --task-definition "aboutme-prod-$1" --query taskDefinition --output json
}

# The app and maintenance services both bind host port 443, so only one of
# them ever runs at once. Scaling a service to zero also waits for its actual
# tasks to stop, not just for the service to report stable, so the host port
# is deterministically free before the other service is asked to claim it.
scale_to_zero_and_wait() { # service
  local task running_tasks stopping_tasks
  local -a tasks=()
  local -A seen=()
  # ListTasks defaults to desired RUNNING. Capture those tasks before lowering
  # the count, then capture desired STOPPED tasks after it. The second list
  # includes a task that was already stopping or was placed just before the
  # count update, so every task that could hold the host port is waited out.
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
# maintenance_up starts the just-registered revision (so a release's Caddy
# image takes effect during the window).
maintenance_up() {
  aws_ ecs update-service --cluster "$cluster" --service aboutme-prod-maintenance \
    --task-definition "${revision[maintenance]}" --desired-count 1 >/dev/null || return 1
  aws_ ecs wait services-stable --cluster "$cluster" --services aboutme-prod-maintenance || return 1
}
maintenance_down() { scale_to_zero_and_wait aboutme-prod-maintenance; }
app_down() { scale_to_zero_and_wait aboutme-prod-app; }

# 1. Candidate checks.
git fetch -q origin main
commit=$(git rev-list -n1 "$tag")
git merge-base --is-ancestor "$commit" origin/main || { say "$tag is not on main"; exit 1; }
ci=$(gh run list --workflow ci.yml --commit "$commit" --json conclusion -q '.[0].conclusion')
[[ $ci == success ]] || { say "CI for $tag is '$ci', not success"; exit 1; }

declare -A image
for name in server web caddy; do
  d=$(digest "$name")
  [[ $d == sha256:* ]] || { say "no $name image for $tag"; exit 1; }
  image[$name]="ghcr.io/$repo-$name@$d"
done

live=$(curl -fsS https://api.cloudflare.com/client/v4/ips | jq -r '.result.ipv4_cidrs | sort | join(" ")')
deployed=$(current_def app | jq -r '.containerDefinitions[] | select(.name == "caddy")
  | .environment[] | select(.name == "CLOUDFLARE_RANGES") | .value | split(" ") | sort | join(" ")')
[[ -n $live && $live == "$deployed" ]] || { say "Cloudflare ranges changed; run tofu apply first"; exit 1; }

# The maintenance service must already exist (tofu apply creates it); fail
# clearly here rather than with a raw AWS error from the family loop below.
maintenance_status=$(aws_ ecs describe-services --cluster "$cluster" --services aboutme-prod-maintenance \
  --query 'services[0].status' --output text)
[[ $maintenance_status == ACTIVE ]] ||
  { say "the aboutme-prod-maintenance service does not exist (status: $maintenance_status); run tofu apply first"; exit 1; }

# 2. Build revisions with the release images. A rollback keeps the current
# maintenance image because older Caddy images do not contain maintenance mode.
for family in "${families[@]}"; do
  caddy_image=${image[caddy]}
  if ((rollback)) && [[ $family == maintenance ]]; then
    caddy_image=$(current_def maintenance | jq -r '.containerDefinitions[] | select(.name == "caddy") | .image')
    [[ -n $caddy_image && $caddy_image != null ]] || { say "no current maintenance Caddy image"; exit 1; }
  fi
  current_def "$family" | jq \
    --arg server "${image[server]}" --arg web "${image[web]}" --arg caddy "$caddy_image" '
    .containerDefinitions |= map(
      .image = (if .name == "caddy" then $caddy elif .name == "web" then $web else $server end)
      | .environment = ((.environment // []) | map(
          if .name == "APP_BUILD_DIGEST" then .value = $server
          elif .name == "PUBLIC_RENDERER_BUILD_DIGEST" then .value = $web
          else . end)))
    | {family, taskRoleArn, executionRoleArn, networkMode, containerDefinitions,
       requiresCompatibilities, volumes}
    | with_entries(select(.value != null))' >"$work/$family.json"
done

# 3. Every secret the revisions reference must exist, or the app fails to start
# after migrate has run. This checks names and metadata only, never a value.
secret_exists() { # valueFrom: SSM parameter ARN or name, or Secrets Manager ARN
  local ref=$1 region_=$region
  if [[ $ref == arn:* ]]; then
    region_=$(cut -d: -f4 <<<"$ref")
  fi
  case $ref in
    arn:aws:secretsmanager:*)
      # Drop any :json-key:version-stage:version-id suffix.
      aws --region "$region_" secretsmanager describe-secret --secret-id "$(cut -d: -f1-7 <<<"$ref")" \
        --query ARN --output text >/dev/null 2>&1 ;;
    arn:aws:ssm:*)
      [[ $(aws --region "$region_" ssm describe-parameters --parameter-filters "Key=Name,Values=${ref#*:parameter}" \
        --query 'length(Parameters)' --output text 2>/dev/null) == 1 ]] ;;
    *)
      [[ $(aws_ ssm describe-parameters --parameter-filters "Key=Name,Values=$ref" \
        --query 'length(Parameters)' --output text 2>/dev/null) == 1 ]] ;;
  esac
}
# A plain assignment lets set -e stop on a jq failure; the app family always has
# secrets, so an empty list means the read failed.
refs=$(jq -r '.containerDefinitions[].secrets[]?.valueFrom' "$work"/*.json)
[[ -n $refs ]] || { say "no task secrets found in the new revisions; refusing to deploy"; exit 1; }
missing=0
while IFS= read -r ref; do
  if ! secret_exists "$ref"; then
    say "missing secret $ref (not found, or not describable with these credentials)"
    missing=1
  fi
done < <(sort -u <<<"$refs")
((!missing)) || { say "create the missing secrets, or turn off the setting that needs them, then rerun"; exit 1; }

# 4. Snapshot.
if ((!rollback)); then
  snap="aboutme-prod-${tag//./-}-$(date -u +%Y%m%d%H%M)"
  aws_ rds create-db-snapshot --db-instance-identifier aboutme-prod --db-snapshot-identifier "$snap" \
    --tags "Key=$snapshot_tag_key,Value=$snapshot_tag_value" >/dev/null
  aws_ rds wait db-snapshot-available --db-snapshot-identifier "$snap"
  say "snapshot $snap"
fi

# 5. Register the revisions.
declare -A revision
for family in "${families[@]}"; do
  revision[$family]=$(aws_ ecs register-task-definition --cli-input-json "file://$work/$family.json" \
    --query taskDefinition.taskDefinitionArn --output text)
done
say "registered revisions for $tag"

# 6. Stop jobs and the app.
schedules=$(aws_ scheduler list-schedules --group-name "$group" --query 'Schedules[].Name' --output text)
declare -A schedule_state
set_schedule() { # name state [task-definition]
  aws_ scheduler get-schedule --group-name "$group" --name "$1" --output json |
    jq --arg s "$2" --arg td "${3:-}" '{Name, GroupName, ScheduleExpression, ScheduleExpressionTimezone,
      FlexibleTimeWindow, Target, Description, StartDate, EndDate, KmsKeyArn,
      ActionAfterCompletion, State: $s}
      | if $td != "" then .Target.EcsParameters.TaskDefinitionArn = $td else . end
      | with_entries(select(.value != null))' >"$work/schedule.json"
  aws_ scheduler update-schedule --cli-input-json "file://$work/schedule.json" >/dev/null
}

previous_app=$(aws_ ecs describe-services --cluster "$cluster" --services aboutme-prod-app \
  --query 'services[0].taskDefinition' --output text)

restore() {
  set +e
  if [[ -n $oneshot_task ]]; then
    local status
    status=$(aws_ ecs describe-tasks --cluster "$cluster" --tasks "$oneshot_task" \
      --query 'tasks[0].lastStatus' --output text)
    if [[ $status != STOPPED ]]; then
      say "failed while a database task may still be running: $oneshot_task ($status)"
      say "the maintenance page stays up; the app and job schedules stay stopped"
      say "check the task, then rerun deploy.sh"
      return
    fi
  fi
  if ((migration_may_be_applied)); then
    if ((app_stable)); then
      # The new app is confirmed healthy and already holds host port 443;
      # only the schedule-enable loop failed partway. Tearing a healthy,
      # already-migrated app down over an unrelated Scheduler API error would
      # be a self-inflicted outage, so it stays up and maintenance stays down
      # (it already is, from before app started). Report the gap instead.
      say "the app started this release and is healthy; it stays up"
      say "some job schedules may not be enabled and pinned to this release:"
      for name in $schedules; do
        if ! printf '%s\n' "${schedules_enabled[@]:-}" | grep -qxF "$name"; then
          say "  $name: fix by hand, or rerun deploy.sh to retry the whole release"
        fi
      done
      return
    fi
    if ((app_start_requested)); then
      say "failed while the new app was starting; its health was never confirmed"
      say "scaling it back to 0 before bringing the maintenance page up"
      if ! app_down; then
        say "could not confirm that the app released host port 443"
        say "the maintenance page was not started; check both services before retrying"
        return
      fi
    fi
    say "a migration may have been applied; app and job schedules stay stopped"
    say "the previous release is not proven against the migrated schema: fix forward, or restore the snapshot"
    say "leaving the maintenance page up so the site answers 503 instead of nothing"
    if ! maintenance_up; then
      say "could not confirm that the maintenance page started; check both services before retrying"
    fi
    return
  fi
  if ((first)); then
    say "failed on the first deploy; there is no previous release to restore"
    if ! app_down; then
      say "could not confirm that the app released host port 443"
      say "the maintenance page was not started; check both services before retrying"
      return
    fi
    say "leaving the maintenance page up so the site answers 503 instead of nothing"
    if ! maintenance_up; then
      say "could not confirm that the maintenance page started; check both services before retrying"
    fi
    return
  fi
  say "failed; restoring the previous app and job schedules"
  # Turn the maintenance page off before the app comes back: both bind host
  # port 443, so bringing the app up first would fail to place.
  if ! maintenance_down; then
    say "could not confirm that maintenance released host port 443"
    say "the previous app was not started; check both services before retrying"
    return
  fi
  if ! aws_ ecs update-service --cluster "$cluster" --service aboutme-prod-app \
    --task-definition "$previous_app" --desired-count 1 >/dev/null; then
    say "could not request the previous app start; check both services before retrying"
    return
  fi
  if ! aws_ ecs wait services-stable --cluster "$cluster" --services aboutme-prod-app; then
    say "could not confirm that the previous app started; check both services before retrying"
    return
  fi
  for name in $schedules; do
    if [[ ${schedule_state[$name]:-} == ENABLED ]]; then
      set_schedule "$name" ENABLED
    fi
  done
}

phase=changing
for name in $schedules; do
  schedule_state[$name]=$(aws_ scheduler get-schedule --group-name "$group" --name "$name" --query State --output text)
  set_schedule "$name" DISABLED
done

app_down
say "site down"

maintenance_up
say "maintenance up"

# Verify through Cloudflare before a database task starts. A slow edge gets the
# same bounded retries as final smoke checks. If it still does not serve the
# maintenance page, restore before migration can change the database.
maintenance_marker='aboutme:maintenance'
maintenance_smoke_ok() {
  local out code body
  out=$(curl -s -m 10 -w '\n%{http_code}' https://aboutme.vn/) || return 1
  code=${out##*$'\n'}
  body=${out%$'\n'*}
  [[ $code == 503 ]] && grep -qF "$maintenance_marker" <<<"$body"
}
if retry maintenance_smoke_ok; then
  say "maintenance smoke: ok"
else
  say "maintenance smoke: could not confirm the maintenance page through Cloudflare"
  exit 1
fi

# 7. Database steps.
run_once() { # family
  local code
  [[ $1 != migrate ]] || migration_may_be_applied=1
  oneshot_task=$(aws_ ecs run-task --cluster "$cluster" --launch-type EC2 \
    --task-definition "${revision[$1]}" --started-by "deploy-$1" \
    --query 'tasks[0].taskArn' --output text)
  [[ $oneshot_task == arn:* ]] || { oneshot_task=""; say "$1 did not start"; return 1; }
  aws_ ecs wait tasks-stopped --cluster "$cluster" --tasks "$oneshot_task"
  code=$(aws_ ecs describe-tasks --cluster "$cluster" --tasks "$oneshot_task" \
    --query 'tasks[0].containers[0].exitCode' --output text)
  oneshot_task=""
  [[ $code == 0 ]] || { say "$1 exited with $code"; return 1; }
  say "$1 done"
}
if ((first)); then
  run_once db-setup
fi
if ((!rollback)); then
  run_once migrate
fi

# 8. Start the release.
aws_ ecs update-service --cluster "$cluster" --service aboutme-prod-web \
  --task-definition "${revision[web]}" --desired-count 1 >/dev/null
aws_ ecs wait services-stable --cluster "$cluster" --services aboutme-prod-web

# Maintenance and app both bind host port 443: take maintenance down before
# starting app, the same way it was taken down before the previous deploy's
# app came up.
maintenance_down
say "maintenance down"

app_start_requested=1
aws_ ecs update-service --cluster "$cluster" --service aboutme-prod-app \
  --task-definition "${revision[app]}" --desired-count 1 >/dev/null
aws_ ecs wait services-stable --cluster "$cluster" --services aboutme-prod-app
app_stable=1
for name in $schedules; do
  set_schedule "$name" ENABLED "${revision[jobs]}"
  schedules_enabled+=("$name")
done
phase=finished
say "site up; job schedules enabled"

# 9. Smoke through Cloudflare, and prove the origin rejects direct requests.
# Right after Caddy restarts, Cloudflare can see a TLS reset or serve a 525, so
# each positive check gets a few attempts. The direct-origin check never
# retries into a pass: one answer from the origin fails the deploy.
status_ok() { # path
  smoke_code=$(curl -s -o /dev/null -w '%{http_code}' "https://aboutme.vn$1")
  [[ $smoke_code == 200 ]]
}
hsts_ok() { curl -fsSI https://aboutme.vn/ | grep -qi '^strict-transport-security:'; }
origin_ip() {
  smoke_ip=$(aws_ ec2 describe-addresses --filters Name=tag:Name,Values=aboutme-prod \
    --query 'Addresses[0].PublicIp' --output text) &&
    [[ $smoke_ip =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]]
}
for path in /healthz /readyz; do
  retry status_ok "$path" || { say "smoke: $path returned $smoke_code"; exit 1; }
done
retry hsts_ok || { say "smoke: HSTS header missing"; exit 1; }
retry origin_ip || { say "smoke: could not resolve the origin address"; exit 1; }
if curl -sk -m "${DEPLOY_SMOKE_TIMEOUT:-5}" -o /dev/null "https://$smoke_ip/"; then
  say "smoke: the origin answered a direct request"
  exit 1
fi
say "deployed $tag"
