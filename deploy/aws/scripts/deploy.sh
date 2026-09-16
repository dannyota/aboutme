#!/usr/bin/env bash
# Deploys a tagged release to aboutme-prod. See docs/runbooks/production.md.
#
#   deploy.sh <tag>                  normal deploy
#   deploy.sh <tag> --first-deploy   also bootstraps roles, grants and logins
#   deploy.sh --rollback <tag>       earlier images, no snapshot, no migration
#
# Order: snapshot, register revisions, stop jobs and app, migrate, start web
# and app, re-enable jobs, smoke. Any failure after the app stops restores the
# previous app and job schedules, unless a database task may still be running;
# then both stay stopped for the operator.
set -euo pipefail

region=ap-southeast-1
cluster=aboutme-prod
group=aboutme-prod-jobs
repo=dannyota/aboutme
families=(app web migrate jobs db-bootstrap db-provision db-set-login)

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

# phase: prepare -> changing -> finished. oneshot_task is set while a database task
# may be running.
phase=prepare
oneshot_task=""
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

# 2. Snapshot.
if ((!rollback)); then
  snap="aboutme-prod-${tag//./-}-$(date -u +%Y%m%d%H%M)"
  aws_ rds create-db-snapshot --db-instance-identifier aboutme-prod --db-snapshot-identifier "$snap" >/dev/null
  aws_ rds wait db-snapshot-available --db-snapshot-identifier "$snap"
  say "snapshot $snap"
fi

# 3. Register revisions with the release images.
declare -A revision
for family in "${families[@]}"; do
  current_def "$family" | jq \
    --arg server "${image[server]}" --arg web "${image[web]}" --arg caddy "${image[caddy]}" '
    .containerDefinitions |= map(
      .image = (if .name == "caddy" then $caddy elif .name == "web" then $web else $server end)
      | .environment = ((.environment // []) | map(
          if .name == "APP_BUILD_DIGEST" then .value = $server
          elif .name == "PUBLIC_RENDERER_BUILD_DIGEST" then .value = $web
          else . end)))
    | {family, taskRoleArn, executionRoleArn, networkMode, containerDefinitions,
       requiresCompatibilities, volumes}
    | with_entries(select(.value != null))' >"$work/$family.json"
  revision[$family]=$(aws_ ecs register-task-definition --cli-input-json "file://$work/$family.json" \
    --query taskDefinition.taskDefinitionArn --output text)
done
say "registered revisions for $tag"

# 4. Stop jobs and the app.
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
      say "the app and job schedules stay stopped; check the task, then rerun deploy.sh"
      return
    fi
  fi
  say "failed; restoring the previous app and job schedules"
  if ((!first)); then
    aws_ ecs update-service --cluster "$cluster" --service aboutme-prod-app \
      --task-definition "$previous_app" --desired-count 1 >/dev/null
  fi
  for name in $schedules; do
    if [[ ${schedule_state[$name]} == ENABLED ]]; then
      set_schedule "$name" ENABLED
    fi
  done
}

phase=changing
for name in $schedules; do
  schedule_state[$name]=$(aws_ scheduler get-schedule --group-name "$group" --name "$name" --query State --output text)
  set_schedule "$name" DISABLED
done

aws_ ecs update-service --cluster "$cluster" --service aboutme-prod-app --desired-count 0 >/dev/null
aws_ ecs wait services-stable --cluster "$cluster" --services aboutme-prod-app
say "site down"

# 5. Database steps.
run_once() { # family
  local code
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
  run_once db-bootstrap
  run_once db-provision
  run_once db-set-login
fi
((rollback)) || run_once migrate

# 6. Start the release.
aws_ ecs update-service --cluster "$cluster" --service aboutme-prod-web \
  --task-definition "${revision[web]}" --desired-count 1 >/dev/null
aws_ ecs wait services-stable --cluster "$cluster" --services aboutme-prod-web
aws_ ecs update-service --cluster "$cluster" --service aboutme-prod-app \
  --task-definition "${revision[app]}" --desired-count 1 >/dev/null
aws_ ecs wait services-stable --cluster "$cluster" --services aboutme-prod-app
for name in $schedules; do
  set_schedule "$name" ENABLED "${revision[jobs]}"
done
phase=finished
say "site up; job schedules enabled"

# 7. Smoke through Cloudflare, and prove the origin rejects direct requests.
for path in /healthz /readyz; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "https://aboutme.vn$path")
  [[ $code == 200 ]] || { say "smoke: $path returned $code"; exit 1; }
done
curl -fsSI https://aboutme.vn/ | grep -qi '^strict-transport-security:' || { say "smoke: HSTS header missing"; exit 1; }
ip=$(aws_ ec2 describe-addresses --filters Name=tag:Name,Values=aboutme-prod \
  --query 'Addresses[0].PublicIp' --output text)
if curl -sk -m "${DEPLOY_SMOKE_TIMEOUT:-5}" -o /dev/null "https://$ip/"; then
  say "smoke: the origin answered a direct request"
  exit 1
fi
say "deployed $tag"
