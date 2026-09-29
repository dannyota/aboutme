#!/usr/bin/env bash
# Deploys a tagged release to aboutme-prod. See docs/runbooks/production.md
# and docs/design/passkey-release-fence.md.
#
#   deploy.sh <tag>                  normal deploy
#   deploy.sh <tag> --first-deploy   also creates roles, grants, and logins
#   deploy.sh --rollback <tag>       earlier images, no snapshot, no migration
#   deploy.sh --activate <tag>       raises the release fence; no ECS change
#   deploy.sh --totp-key-reencrypt <tag>   runs the TOTP key re-encryption
#                                     one-shot; no image, service, or schedule
#                                     change (see fence.sh's fence_epoch_totp)
#   deploy.sh --check-public-root <tag> <root>  audits one candidate root
#   deploy.sh <tag> --require-free-root <root>  checks a root before release
#
# The operator role reads the fence and assumes the deploy role. A deploy holds
# the lock through provenance, task and secret checks, snapshot, handoff,
# database work, smoke, warm-up, and notification recovery. Each handoff starts
# the incoming service before stopping the outgoing one. Pre-migration failures
# restore the previous app. A database task may have changed the database, so
# recovery leaves maintenance up and requires a forward fix or snapshot restore.
set -euo pipefail

region=ap-southeast-1
site_alarm_region=us-east-1
cluster=aboutme-prod
group=aboutme-prod-jobs
repo=dannyota/aboutme
families=(app web maintenance migrate jobs db-setup)
# release-snapshot-sweep deletes snapshots with this tag within 30 days.
snapshot_tag_key=aboutme:created-by
snapshot_tag_value=deploy.sh
usage() {
  echo "usage: deploy.sh <tag> [--first-deploy] | deploy.sh <tag> --require-free-root <root> | deploy.sh --check-public-root <tag> <root> | deploy.sh --rollback <tag> | deploy.sh --activate <tag> | deploy.sh --totp-key-reencrypt <tag>" >&2
  exit 2
}
first=0
rollback=0
activate=0
totp_reencrypt=0
public_root_audit=0
require_free_root=0
public_root=""
case "$#:${1:-}:${2:-}" in
  2:--rollback:?*) rollback=1 tag=$2 ;;
  2:--activate:?*) activate=1 tag=$2 ;;
  2:--totp-key-reencrypt:?*) totp_reencrypt=1 tag=$2 ;;
  3:--check-public-root:?*) public_root_audit=1 tag=$2 public_root=$3 ;;
  1:[!-]*:) tag=$1 ;;
  2:[!-]*:--first-deploy) first=1 tag=$1 ;;
  3:[!-]*:--require-free-root) require_free_root=1 tag=$1 public_root=$3 ;;
  *) usage ;;
esac
operation_kind=deploy
((!rollback)) || operation_kind=rollback
((!activate)) || operation_kind=activate
((!totp_reencrypt)) || operation_kind=totp_reencrypt
((!public_root_audit)) || operation_kind=public_root_check

# fd 9 is a fixed duplicate of the script's own stderr, made once, before
# anything ever redirects fd 2 for a single read (notifications.sh's alarm
# wait does this to capture a failing call's stderr without losing its own
# messages to that same file).
exec 9>&2
say() { printf 'deploy: %s\n' "$*" >&9; }
public_root_valid() { [[ ${#1} -ge 4 && ${#1} -le 30 && $1 =~ ^[a-z0-9]+(-[a-z0-9]+)*$ ]]; }

# A strict vMAJOR.MINOR.PATCH tag, no leading zero, components 0 through 999,
# maps to MAJOR*1000000 + MINOR*1000 + PATCH. Defined here, before fence.sh is
# sourced, so a malformed tag is rejected before any AWS credential or config
# work; fence_read reuses this same function afterward.
release_number() {
  [[ $1 =~ ^v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})$ ]] || return 1
  echo $(( 10#${BASH_REMATCH[1]} * 1000000 + 10#${BASH_REMATCH[2]} * 1000 + 10#${BASH_REMATCH[3]} ))
}
candidate=$(release_number "$tag") || { say "$tag is not a strict vMAJOR.MINOR.PATCH tag"; exit 1; }
if ((public_root_audit || require_free_root)); then
  public_root_valid "$public_root" || { say "invalid public root '$public_root'"; exit 1; }
fi

work=$(mktemp -d)
# Cleared as soon as fence.sh's role and identity work needs it, so an early
# exit (a malformed tag, a failed identity check) never leaves it behind;
# on_exit below replaces this trap with the full cleanup once it is defined.
trap 'rm -rf "$work"' EXIT
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=fence.sh
source "$script_dir/fence.sh"
# shellcheck source=totp-reencrypt.sh
source "$script_dir/totp-reencrypt.sh"
# shellcheck source=notifications.sh
source "$script_dir/notifications.sh"
# shellcheck source=handoff.sh
source "$script_dir/handoff.sh"
# shellcheck source=edge.sh
source "$script_dir/edge.sh"
# shellcheck source=provenance.sh
source "$script_dir/provenance.sh"
# shellcheck source=public-root-check.sh
source "$script_dir/public-root-check.sh"
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

# phase tracks handoff recovery. oneshot_task remains set while a task may run.
# migration_may_be_applied marks a possible migration. app_stable and
# maintenance_stopped track the final handoff. site_alarm_waited prevents a
# second wait during cleanup.
phase=prepare
oneshot_task=""
# A root check retains the fence until ECS proves its task stopped.
public_root_pending=0
public_root_started_by=""
public_root_check_attempt=0
migration_may_be_applied=0
app_start_requested=0
app_stable=0
maintenance_stopped=0
site_up_epoch=0
schedules_enabled=()
site_alarm_restore=0
task_stopped_rule_restore=0
signaled=0
site_alarm_waited=0

on_exit() {
  local status=$? cleanup_failed=0
  # Undo any single-read stderr capture a signal interrupted.
  exec 2>&9
  # A second HUP/INT/TERM during cleanup must not re-enter on_signal and race
  # this same restore/release sequence against itself.
  trap '' HUP INT TERM
  if ((status != 0)) && [[ $phase == changing ]]; then
    # Keep notification cleanup reachable even when a recovery safety check
    # fails. The deployment's original nonzero status still wins.
    if ! restore; then
      say "service recovery did not complete; restoring deployment notifications"
    fi
  fi
  # A warm-up or smoke failure after the handoff finished must not re-enable
  # the alarm immediately: that recreates the false alert the bounded wait
  # exists to avoid, so it waits the same way a clean finish does. A signal
  # here, or a wait this exit already ran, skips straight to restoring so
  # Ctrl-C is never stuck behind a wait of up to DEPLOY_ALARM_WAIT seconds.
  # restore() keeping a confirmed app and stopping maintenance counts as a
  # finished handoff for this wait.
  if ((status != 0)) && { [[ $phase == finished ]] || ((app_stable && maintenance_stopped)); } &&
    ((site_alarm_restore && !signaled && !site_alarm_waited)); then
    local wait_s=${DEPLOY_ALARM_WAIT:-900}
    say "waiting up to $wait_s s for the site-down alarm before re-enabling its actions; press Ctrl-C to skip the wait"
    trap 'signaled=1' HUP INT TERM
    restore_deploy_notifications 1 || cleanup_failed=1
    trap '' HUP INT TERM
    # A Ctrl-C meant for the wait also reaches the AWS calls around it, so
    # retry, now immediately and with signals ignored, anything it cut short.
    if ((task_stopped_rule_restore || site_alarm_restore)); then
      cleanup_failed=0
      restore_deploy_notifications || cleanup_failed=1
    fi
  else
    restore_deploy_notifications || cleanup_failed=1
  fi
  if ((public_root_pending)); then
    say "public-root check may still run; leave the operation lock closed; list tasks with started-by $public_root_started_by, wait for STOPPED, then follow the release-fence recovery"
  elif ((lock_held)); then
    fence_release || { say "could not release the operation lock; the runbook owns the manual clear"; cleanup_failed=1; }
  fi
  rm -rf "$work"
  ((cleanup_failed)) && status=1
  exit "$status"
}
trap on_exit EXIT
on_signal() { # name exit status
  signaled=1
  say "received $1; restoring deployment notifications"
  exit "$2"
}
trap 'on_signal HUP 129' HUP
trap 'on_signal INT 130' INT
trap 'on_signal TERM 143' TERM

digest() { # image name -> sha256:...
  local token
  token=$(curl -fsS "https://ghcr.io/token?scope=repository:$repo-$1:pull&service=ghcr.io" | jq -r .token)
  curl -fsSI -H "Authorization: Bearer $token" \
    -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json' \
    "https://ghcr.io/v2/$repo-$1/manifests/$tag" |
    tr -d '\r' | awk -F': ' 'tolower($1) == "docker-content-digest" { print $2 }'
}

current_def() { describe_task_def "aboutme-prod-$1"; }
# Unlike current_def, takes an exact family:revision or full ARN rather than
# assuming the family's latest registration; fence_read uses it to prove what
# a service actually runs, not what was most recently registered.
describe_task_def() {
  aws_ ecs describe-task-definition --task-definition "$1" --query taskDefinition --output json
}

# 1. Candidate checks.
git fetch -q origin main
commit=$(git rev-list -n1 "$tag")
git merge-base --is-ancestor "$commit" origin/main || { say "$tag is not on main"; exit 1; }
# gh's own --commit filter accepts a run for that SHA from any event or
# branch, so a green workflow_dispatch run on a feature branch at the same
# commit would satisfy it. Filter in the script instead: only a push run on
# main for this exact commit counts (AGENTS.md: a manual run never replaces
# the release's green main run).
ci_runs=$(gh run list --workflow ci.yml --commit "$commit" --event push --branch main \
  --json databaseId,conclusion,status,event,headBranch,headSha --limit 20) ||
  { say "could not list CI runs for $tag"; exit 1; }
ci=$(jq -r --arg c "$commit" \
  '[.[] | select(.event == "push" and .headBranch == "main" and .headSha == $c)] | .[0]
   | if . == null then "none"
     elif (.conclusion // "") == "" then .status
     else .conclusion end' \
  <<<"$ci_runs")
[[ $ci == success ]] ||
  { say "no successful push run of ci.yml on main for $tag (${commit:0:7}); latest: '$ci'"; exit 1; }

fence_read || exit 1
((candidate >= fence_min)) ||
  { say "$tag ($candidate) is below the release fence minimum $fence_min_tag ($fence_min)"; exit 1; }

# --activate only raises the fence, while the activation operation holds the
# lock: no image, ECS, snapshot, or notification mutation. It requires the
# running app service to already be the exact stable candidate, so the raise
# always describes a release already healthy in production.
if ((activate)); then
  service=$(aws_ ecs describe-services --cluster "$cluster" --services aboutme-prod-app --output json) ||
    { say "could not read the running app service"; exit 1; }
  service_td=$(jq -r '.services[0].taskDefinition // empty' <<<"$service")
  running_count=$(jq -r '.services[0].runningCount // 0' <<<"$service")
  desired_count=$(jq -r '.services[0].desiredCount // 0' <<<"$service")
  [[ -n $service_td ]] && ((running_count > 0)) && ((running_count == desired_count)) ||
    { say "the running app service is not stable; activation requires a healthy running app"; exit 1; }
  service_release=$(task_def_release_number "$service_td") || { say "could not verify the running app's release"; exit 1; }
  [[ $service_release == "$candidate" ]] ||
    { say "the running app is release $service_release, not the activation candidate $candidate"; exit 1; }
  fence_lock || exit 1
  fence_raise || exit 1
  say "raised the release fence to $tag ($candidate)"
  exit 0
fi

# --totp-key-reencrypt runs the one-shot key re-encryption task under the
# totp_reencrypt operation kind (docs/design/passkey-release-fence.md,
# "Authenticator-app key re-encryption"; see totp-reencrypt.sh).
if ((totp_reencrypt)); then
  totp_reencrypt_run
  exit 0
fi

if ((public_root_audit)); then
  fence_lock || exit 1
  public_root_check "$public_root"
  exit $?
fi

declare -A image
for name in server web caddy; do
  d=$(digest "$name")
  [[ $d == sha256:* ]] || { say "no $name image for $tag"; exit 1; }
  provenance_verify "$name" "$d" "$tag" "$commit" || exit 1
  image[$name]="ghcr.io/$repo-$name@$d"
done

if ((require_free_root)); then
  fence_lock || exit 1
  public_root_check "$public_root"
fi

edge_origin_cert_check || exit 1

# The maintenance service must already exist (tofu apply creates it); fail
# clearly here rather than with a raw AWS error from the family loop below.
maintenance_status=$(aws_ ecs describe-services --cluster "$cluster" --services aboutme-prod-maintenance \
  --query 'services[0].status' --output text)
[[ $maintenance_status == ACTIVE ]] ||
  { say "the aboutme-prod-maintenance service does not exist (status: $maintenance_status); run tofu apply first"; exit 1; }

# 2. Build revisions with the release images. A rollback keeps the current
# maintenance image, because older Caddy images do not contain maintenance
# mode, and its release stamp, since that image is not the rollback target.
for family in "${families[@]}"; do
  caddy_image=${image[caddy]}
  stamp_tag=$tag
  stamp_rel=$candidate
  if ((rollback)) && [[ $family == maintenance ]]; then
    caddy_image=$(current_def maintenance | jq -r '.containerDefinitions[] | select(.name == "caddy") | .image')
    [[ -n $caddy_image && $caddy_image != null ]] || { say "no current maintenance Caddy image"; exit 1; }
    read -r stamp_tag stamp_rel < <(current_def maintenance | jq -r '.containerDefinitions[] | select(.name == "caddy")
      | (.environment // []) as $e | [($e[]? | select(.name=="DEPLOY_RELEASE_TAG").value), ($e[]? | select(.name=="DEPLOY_RELEASE_NUMBER").value)] | @tsv')
    [[ -n $stamp_tag && -n $stamp_rel ]] || { stamp_tag=$tag; stamp_rel=$candidate; }
  fi
  current_def "$family" | jq \
    --arg server "${image[server]}" --arg web "${image[web]}" --arg caddy "$caddy_image" \
    --arg tag "$stamp_tag" --argjson rel "$stamp_rel" '
    .containerDefinitions |= map(
      .image = (if .name == "caddy" then $caddy elif .name == "web" then $web else $server end)
      | .environment = (((.environment // []) | map(
          if .name == "APP_BUILD_DIGEST" then .value = $server
          elif .name == "PUBLIC_RENDERER_BUILD_DIGEST" then .value = $web
          else . end) | map(select(.name != "DEPLOY_RELEASE_TAG" and .name != "DEPLOY_RELEASE_NUMBER")))
          + [{name:"DEPLOY_RELEASE_TAG",value:$tag},{name:"DEPLOY_RELEASE_NUMBER",value:($rel|tostring)}]))
    | {family, taskRoleArn, executionRoleArn, networkMode, containerDefinitions,
       requiresCompatibilities, volumes}
    | with_entries(select(.value != null))' >"$work/$family.json"
done

edge_distribution_check || exit 1

# The handoff runs both Caddy services at once, which ECS refuses to place
# while either task definition reserves host port 443 or 8443.
if jq -e '.containerDefinitions[].portMappings[]?
    | select((.hostPort // .containerPort) == 443 or (.hostPort // .containerPort) == 8443)' \
    "$work/app.json" "$work/maintenance.json" >/dev/null; then
  say "the app or maintenance task definition still maps host port 443 or 8443; run tofu apply first"
  exit 1
fi

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
      aws_dep_ "$region_" secretsmanager describe-secret --secret-id "$(cut -d: -f1-7 <<<"$ref")" \
        --query ARN --output text >/dev/null 2>&1 ;;
    arn:aws:ssm:*)
      [[ $(aws_dep_ "$region_" ssm describe-parameters --parameter-filters "Key=Name,Values=${ref#*:parameter}" \
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

# A flag may turn on only once --activate raised the fence to its floor
# (fence.sh's fence_epoch*), so a revision below the floor never reaches ECS.
check_floor_flag() { # env-var-name fence-epoch human-name floor-tag
  local val
  val=$(jq -r --arg n "$1" '.containerDefinitions[] | select(.name == "server")
    | .environment[]? | select(.name == $n) | .value' "$work/app.json")
  if ((fence_min < $2)) && [[ $val == true ]]; then
    say "the new app turns $3 on while the release fence is below $4; run --activate first"
    exit 1
  fi
}
check_floor_flag PASSKEY_ENROLLMENT_ENABLED "$fence_epoch" "passkey enrollment" v0.4.2
check_floor_flag TOTP_ENROLLMENT_ENABLED "$fence_epoch_totp" "TOTP enrollment" v0.4.7
check_floor_flag SIGN_IN_TO_VIEW_ENABLED "$fence_epoch_signin" "sign in to view" v0.6.26

# A release at or above a key's floor needs its secret on the server container,
# or it fails after maintenance starts (totp-key-management.md, sign-in-to-view.md).
require_server_secret() { # env-var-name fence-epoch message
  ((candidate >= $2)) || return 0
  jq -e --arg n "$1" '.containerDefinitions[] | select(.name == "server") | (.secrets // [])[] | select(.name == $n)' "$work/app.json" >/dev/null 2>&1 ||
    { say "$3"; exit 1; }
}
require_server_secret TOTP_ACTIVE_KEY "$fence_epoch_totp" "run tofu apply for the TOTP key before deploying this release"
require_server_secret VIEW_PASS_KEY "$fence_epoch_signin" "run secrets.sh, then the reviewed tofu apply, for the view pass key before deploying this release"

((lock_held)) || fence_lock || exit 1

# 4. Snapshot.
if ((!rollback)); then
  fence_checkpoint || exit 1
  snap="aboutme-prod-${tag//./-}-$(date -u +%Y%m%d%H%M)"
  aws_ rds create-db-snapshot --db-instance-identifier aboutme-prod --db-snapshot-identifier "$snap" \
    --tags "Key=$snapshot_tag_key,Value=$snapshot_tag_value" >/dev/null
  aws_ rds wait db-snapshot-available --db-snapshot-identifier "$snap"
  say "snapshot $snap"
fi

# 5. Register the revisions.
fence_checkpoint || exit 1
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
  fence_checkpoint || return 1
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
      say "failed while a one-shot task may still be running: $oneshot_task ($status)"
      say "the maintenance page stays up; the app and job schedules stay stopped"
      say "check the task, then rerun deploy.sh"
      return
    fi
  fi
  # The released app is confirmed running and answers on port 8443; only a
  # later step failed. In every mode, including a rollback, tearing it down
  # over an unrelated API error would be a self-inflicted outage, so it stays
  # up and maintenance, which still shares the port, is stopped.
  if ((app_stable)); then
    say "the app started this release and is healthy; it stays up"
    if ((!maintenance_stopped)); then
      if maintenance_down; then
        maintenance_stopped=1
        site_up_epoch=$EPOCHSECONDS
      else
        say "could not confirm that maintenance stopped; scale aboutme-prod-maintenance to 0 by hand"
      fi
    fi
    say "some job schedules may not be enabled and pinned to this release:"
    for name in $schedules; do
      if ! printf '%s\n' "${schedules_enabled[@]:-}" | grep -qxF "$name"; then
        say "  $name: fix by hand, or rerun deploy.sh to retry the whole release"
      fi
    done
    return
  fi
  if ((migration_may_be_applied)); then
    # Maintenance has run since before the migration. It is confirmed (or
    # started again) first; only a confirmed maintenance page lets restore()
    # stop a new app that may have started beside it without proving
    # healthy. That app runs the migrated schema, so when maintenance cannot
    # be confirmed it is left running rather than leaving port 8443 dark.
    say "a migration may have been applied; job schedules stay stopped"
    say "the previous release is not proven against the migrated schema: fix forward, or restore the snapshot"
    say "leaving the maintenance page up so the site answers 503 instead of nothing"
    if ! maintenance_up; then
      say "could not confirm that the maintenance page runs; check both services before retrying"
      ((!app_start_requested)) ||
        say "the new app stays as it is so port 8443 keeps a listener; check aboutme-prod-app by hand"
      return
    fi
    if ((app_start_requested)); then
      say "failed while the new app was starting; its health was never confirmed; scaling it back to 0"
      app_down || say "could not confirm that the new app stopped; check aboutme-prod-app before retrying"
    fi
    return
  fi
  if ((first)); then
    say "failed on the first deploy; there is no previous release to restore"
    say "leaving the maintenance page up so the site answers 503 instead of nothing"
    if ! maintenance_up; then
      say "could not confirm that the maintenance page runs; the app stays as it is so port 8443 keeps a listener"
      say "check aboutme-prod-app and aboutme-prod-maintenance by hand before retrying"
      return
    fi
    app_down || say "could not confirm that the app stopped; check aboutme-prod-app before retrying"
    return
  fi
  # One checkpoint covers the whole sequence below: it runs before either
  # service changes, so a transient checkpoint failure never stops
  # maintenance with no app started to replace it. A genuine
  # below-the-fence previous release is refused the same way.
  if ! fence_checkpoint; then
    say "operation lock or release floor no longer holds; leaving both services as they are"
    return
  fi
  local prev_release
  prev_release=$(task_def_release_number "$previous_app") ||
    { say "could not verify the previous app's release; leaving both services as they are"; return; }
  if ((prev_release < fence_min)); then
    say "the previous app's release ($prev_release) is below the fence minimum ($fence_min); leaving both services as they are"
    return
  fi
  say "failed; restoring the previous app and job schedules"
  # The previous app starts (or is confirmed, if it never stopped) beside
  # maintenance, and maintenance stops only after that, so port 8443 always
  # has a listener.
  # A previous app that cannot be confirmed may be unhealthy, so it is not
  # left serving part of the traffic beside maintenance: once maintenance is
  # confirmed, the app is scaled back to 0. If maintenance cannot be
  # confirmed either, the app stays at its count as the only possible
  # listener.
  if ! app_up "$previous_app"; then
    say "could not confirm that the previous app started"
    if maintenance_up; then
      say "the maintenance page stays up; scaling the unconfirmed app back to 0"
      app_down || say "could not confirm that the app stopped; check aboutme-prod-app before retrying"
    else
      say "could not confirm that the maintenance page runs either; both services stay as they are; check them by hand"
    fi
    return 1
  fi
  local failed=0
  if ! maintenance_down; then
    say "could not confirm that maintenance stopped; scale aboutme-prod-maintenance to 0 by hand"
    failed=1
  fi
  for name in $schedules; do
    if [[ ${schedule_state[$name]:-} == ENABLED ]]; then
      set_schedule "$name" ENABLED
    fi
  done
  return "$failed"
}

# Do not start the handoff until both notification sources are confirmed
# suppressed. A failure here restores any uncertain change and leaves app,
# maintenance, schedules, and database untouched.
pause_deploy_notifications || { say "notification suppression failed; refusing to disrupt the site"; exit 1; }

phase=changing
for name in $schedules; do
  schedule_state[$name]=$(aws_ scheduler get-schedule --group-name "$group" --name "$name" --query State --output text)
  set_schedule "$name" DISABLED
done

# Maintenance starts beside the running app, then the app stops.
maintenance_up
say "maintenance up"

app_down
say "app down"

# Verify through CloudFront before a database task starts. A slow edge gets the
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
  say "maintenance smoke: could not confirm the maintenance page through CloudFront"
  exit 1
fi

if ((require_free_root)); then
  public_root_check "$public_root"
fi

# 7. Database steps.
run_once() { # family
  fence_checkpoint || return 1
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
fence_checkpoint || exit 1
aws_ ecs update-service --cluster "$cluster" --service aboutme-prod-web \
  --task-definition "${revision[web]}" --desired-count 1 >/dev/null
aws_ ecs wait services-stable --cluster "$cluster" --services aboutme-prod-web

# The new app starts beside maintenance, then maintenance stops.
app_start_requested=1
fence_checkpoint || exit 1
app_up "${revision[app]}"
app_stable=1
say "app up"
maintenance_down
maintenance_stopped=1
site_up_epoch=$EPOCHSECONDS
say "maintenance down"
for name in $schedules; do
  set_schedule "$name" ENABLED "${revision[jobs]}"
  schedules_enabled+=("$name")
done
phase=finished
say "app started; job schedules enabled"

# 9. Smoke through CloudFront, and prove the origin rejects direct requests.
# Right after Caddy restarts, CloudFront can return 502 on a bad origin
# certificate or TLS handshake or 504 when the origin does not answer in time
# (docs/design/cloudfront-edge.md, "Deploys and monitoring"), so each positive
# check gets a few attempts. The direct-origin check never retries into a
# pass: one answer from the origin fails the deploy.
status_ok() { # path
  smoke_code=$(curl -s -o /dev/null -w '%{http_code}' "https://aboutme.vn$1")
  [[ $smoke_code == 200 ]]
}
# One HEAD request checks the edge headers, so a retried attempt never sends
# more than one call. smoke_headers names the last attempt's problem for the
# message after retry() gives up. The response must come from CloudFront
# (its Via) and not through Cloudflare's proxy (no CF-Ray), which would still
# pass CloudFront's Via along.
smoke_headers=
edge_headers_ok() {
  local headers
  headers=$(curl -fsSI https://aboutme.vn/) || { smoke_headers="the header request failed"; return 1; }
  if ! grep -qi '^strict-transport-security:' <<<"$headers"; then
    smoke_headers="HSTS header missing"
  elif ! grep -qiE '^via:.*\(cloudfront\)' <<<"$headers"; then
    smoke_headers="Via does not name CloudFront; check the DNS records for aboutme.vn"
  elif grep -qi '^cf-ray:' <<<"$headers"; then
    smoke_headers="Cloudflare proxies the response; check the DNS records for aboutme.vn"
  else
    return 0
  fi
  return 1
}
origin_ip() {
  # ec2:DescribeAddresses is outside the deploy role's closed list; this
  # smoke-only read uses the base caller's own credentials instead.
  smoke_ip=$(aws --region "$region" ec2 describe-addresses --filters Name=tag:Name,Values=aboutme-prod \
    --query 'Addresses[0].PublicIp' --output text) &&
    [[ $smoke_ip =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]]
}
for path in /healthz /readyz; do
  retry status_ok "$path" || { say "smoke: $path returned $smoke_code"; exit 1; }
done
retry edge_headers_ok || { say "smoke: $smoke_headers"; exit 1; }
retry origin_ip || { say "smoke: could not resolve the origin address"; exit 1; }
edge_origin_closed "$smoke_ip" 443 || exit 1
edge_origin_closed "$smoke_ip" 8443 || exit 1

# Warm the new release through CloudFront before calling the site up. The
# first requests after a restart can be slow on the CloudFront-to-origin path,
# which /healthz does not exercise, so request a public resume page (Go, the
# database, and the Nuxt render) and the homepage (Nuxt) until each answers
# 200 quickly, within a bounded number of attempts.
warm_path() { # path
  local path=$1 attempts=${DEPLOY_WARM_ATTEMPTS:-8} fast=${DEPLOY_WARM_FAST:-3} \
    timeout=${DEPLOY_WARM_TIMEOUT:-30} attempt out code time_total
  for ((attempt = 1; attempt <= attempts; attempt++)); do
    if out=$(curl -s -o /dev/null -m "$timeout" -w '%{http_code} %{time_total}' "https://aboutme.vn$path"); then
      code=${out%% *}
      time_total=${out#* }
    else
      code=000
      time_total=$timeout
    fi
    say "warm: $path $code in $time_total s"
    # Compare as a float with awk, not bash arithmetic, which is integer-only.
    if [[ $code == 200 ]] && awk -v t="$time_total" -v f="$fast" 'BEGIN { exit !(t < f) }'; then
      return 0
    fi
    ((attempt == attempts)) || sleep "$smoke_delay"
  done
  say "warm-up: $path did not answer 200 within $fast s in $attempts attempts (last: $code in $time_total s)"
  return 1
}
for path in "${DEPLOY_WARM_PAGE:-/danny}" /; do
  warm_path "$path" || exit 1
done
say "site up"

restore_deploy_notifications 1 || { say "notification restoration failed after the deploy"; exit 1; }
say "deployed $tag"
