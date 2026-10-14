#!/usr/bin/env bash
# Account deletion never retries a saved destructive attempt.
set -euo pipefail

# The runner supplies these inputs before calling the account helpers.
declare lib id operations_dir

account_supported() { [[ $1 == v0.7.9 ]]; }
account_secret_id() {
  local metadata
  metadata=$(podman secret inspect --format '{{.ID}}' db-app-password 2>/dev/null) || return 1
  [[ $metadata =~ ^[A-Za-z0-9_-]{1,128}$ ]] || return 1
  printf '%s\n' "$metadata"
}

account_live() {
  local live tag component pin image running expected digests minimum flags platform metadata
  live=$("$lib/deploy-host.sh" - - release-json 2>/dev/null) || return 1
  tag=$(jq -r .release_tag <<<"$live")
  account_supported "$tag" || return 1
  for component in server web caddy; do
    systemctl is-active --quiet "aboutme-$component" || return 1
    expected=$(jq -r --arg c "$component" '.[$c+"_image"]' <<<"$live")
    read -r running pin image < <(podman container inspect --format '{{.State.Running}} {{.ImageName}} {{.Image}}' "aboutme-$component" 2>/dev/null)
    [[ $running == true && $pin == "$expected" && $image =~ ^[0-9a-f]{64}$ ]] || return 1
    digests=$(podman image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$image" 2>/dev/null) || return 1
    grep -qxF "$expected" <<<"$digests" || return 1
    platform=$(podman image inspect --format '{{.Os}}/{{.Architecture}}' "$image" 2>/dev/null) || return 1
    [[ $platform == linux/amd64 ]] || return 1
    # Select only the two public release fields inside the template, never
    # print or capture the other process environment entries.
    metadata=$(podman container inspect --format '{{range .Config.Env}}{{if ge (len .) 19}}{{if eq (slice . 0 19) "DEPLOY_RELEASE_TAG="}}{{println .}}{{end}}{{end}}{{if ge (len .) 22}}{{if eq (slice . 0 22) "DEPLOY_RELEASE_NUMBER="}}{{println .}}{{end}}{{end}}{{end}}' "aboutme-$component" 2>/dev/null) || return 1
    [[ $(grep -cxF "DEPLOY_RELEASE_TAG=$tag" <<<"$metadata") == 1 && $(grep -cxF "DEPLOY_RELEASE_NUMBER=$(jq -r .release_number <<<"$live")" <<<"$metadata") == 1 && $(wc -l <<<"$metadata") == 2 ]] || return 1
  done
  minimum=$("$lib/fence.sh" read 2>/dev/null | jq -er .minimum_release) || return 1
  flags=$(sha256sum /etc/aboutme/flags.env | cut -d' ' -f1) || return 1
  if grep -qx 'PASSKEY_ENROLLMENT_ENABLED=true' /etc/aboutme/flags.env; then ((minimum >= 4002)) || return 1; fi
  if grep -qx 'TOTP_ENROLLMENT_ENABLED=true' /etc/aboutme/flags.env; then ((minimum >= 4007)) || return 1; fi
  if grep -qx 'SIGN_IN_TO_VIEW_ENABLED=true' /etc/aboutme/flags.env; then ((minimum >= 6026)) || return 1; fi
  db_secret_id=$(account_secret_id) || return 1
  if [[ -n ${state:-} ]]; then
    [[ $(jq -cS .release <<<"$state") == $(jq -cS . <<<"$live") &&
      $(jq -r .flagsHash <<<"$state") == "$flags" &&
      $(jq -r .dbSecretId <<<"$state") == "$db_secret_id" ]] || return 1
  fi
  # The sourced runner consumes the live release and flags returned here.
  # shellcheck disable=SC2034
  live_release=$live flags_hash=$flags
}

account_backend_stopped() {
  local app=$1 count
  # Only backend metadata is read. A failed query never proves absence.
  count=$(timeout 30 runuser -u postgres -- psql -XAt -v ON_ERROR_STOP=1 \
    --dbname aboutme --set=app="$app" <<'SQL'
SELECT count(*) FROM pg_stat_activity WHERE application_name = :'app';
SQL
  ) || return 1
  [[ $count == 0 ]]
}
account_attempts_stopped() {
  local attempt name app exists running
  while IFS= read -r attempt; do
    name=$(jq -r .container <<<"$attempt") app=$(jq -r .applicationName <<<"$attempt")
    exists=0
    podman container exists "$name" >/dev/null 2>&1 || exists=$?
    case $exists in
      0)
        running=$(podman container inspect --format '{{.State.Running}}' "$name" 2>/dev/null) || return 1
        [[ $running == false ]] || return 1
        podman rm "$name" >/dev/null 2>&1 || return 1
        ;;
      1) ;;
      *) return 1 ;;
    esac
    account_backend_stopped "$app" || return 1
  done < <(jq -c '.attempts[]' <<<"$state")
  state=$(jq '.attempts |= map(.stopped=true) | .closureQuiescent=true' <<<"$state")
  operation_save
}

account_container() { # intent server arguments...
  local intent=$1 number name app image tag release_number result rc=0
  shift
  account_live || return 1
  account_attempts_stopped || return 1
  number=$(jq '.attemptCount + 1' <<<"$state")
  ((number <= 999999)) || return 1
  name="aboutme-operation-$id-$number" app="hostop:$id:$number"
  image=$(jq -r .release.server_image <<<"$state")
  tag=$(jq -r .release.release_tag <<<"$state")
  release_number=$(jq -r .release.release_number <<<"$state")
  "$lib/fence.sh" checkpoint "$id" "$tag" >/dev/null 2>&1 || return 1
  state=$(jq --arg i "$intent" --argjson n "$number" --arg c "$name" --arg a "$app" --arg t "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.intent=$i | .attemptCount=$n | .closureQuiescent=false | .attempts += [{number:$n,container:$c,applicationName:$a,startedAt:$t,stopped:false}]' <<<"$state")
  operation_save || return 1
  result="$operations_dir/$id/output"
  [[ ! -L $result ]] || return 1
  rm -f "$result"
  (umask 077; : >"$result")
  # Creation is an effect with a durable intent. No network/media credential.
  podman create --name "$name" --network none --entrypoint /usr/local/bin/server \
    --memory 256m --memory-swap 256m --log-driver none --ulimit core=0:0 \
    --cap-drop ALL --security-opt no-new-privileges --read-only --restart no \
    --volume /run/postgresql:/run/postgresql --tmpfs /tmp:rw,noexec,nosuid,nodev,size=16m \
    --env DATABASE_URL="postgres://aboutme_app@/aboutme?host=/run/postgresql&application_name=$app" \
    --env DEPLOY_RELEASE_TAG="$tag" --env DEPLOY_RELEASE_NUMBER="$release_number" \
    --secret db-app-password,type=env,target=PGPASSWORD "$image" "$@" >/dev/null 2>&1 || rc=125
  if ((rc == 0)); then
    "$lib/fence.sh" checkpoint "$id" "$tag" >/dev/null 2>&1 || return 1
    (ulimit -f 4; timeout 150 podman start --attach "$name" >"$result" 2>/dev/null) || rc=$?
  fi
  command_rc=$rc command_output=$result
  # A timeout, missing container, or lost reply does not replace a backend check.
  account_attempts_stopped || return 1
}

account_parse_write() { # exit output-file uuid slug
  local rc=$1 file=$2 user=$3 slug=$4 line slugs outcome=unknown returned=null
  if [[ $(wc -c <"$file") -le 1024 && $(wc -l <"$file") == 2 && $(tail -n 1 "$file") == 'account-delete: end' ]]; then
    line=$(head -n 1 "$file")
    if [[ $rc == 0 && $line == "deleted user=$user slugs="* ]]; then
      slugs=${line#"deleted user=$user slugs="}
      returned=$(jq -cn --arg s "$slugs" '$s | split(",")')
      if account_slugs_valid "$returned"; then outcome=committed; else returned=null; fi
    elif [[ $rc == 4 && $line == "not found: $slug" ]]; then
      outcome=not_found returned='[]'
    elif [[ $rc == 5 && $line == "mismatch: $slug is not owned by $user; nothing deleted" ]]; then
      outcome=mismatch returned='[]'
    fi
  fi
  jq -cn --arg o "$outcome" --argjson r "$returned" '{writeOutcome:$o,returnedSlugs:$r}'
}
account_slugs_valid() {
  jq -e 'type=="array" and length<=3 and .==sort and .==unique and all(.[]; type=="string" and length>=4 and length<=30 and test("^[a-z0-9]+(-[a-z0-9]+)*$"))' <<<"$1" >/dev/null
}
account_parse_snapshot() { # kind, bounded stdout
  python3 - "$1" 3<<<"$2" <<'PYTHON'
import json, os, sys
try:
    def pairs(items):
        result={}
        for key,value in items:
            if key in result: raise ValueError('duplicate')
            result[key]=value
        return result
    with os.fdopen(3,'rb') as stream: raw=stream.read(1025)
    if len(raw)>1024 or b'\n' in raw.rstrip(b'\n'): raise ValueError('size or lines')
    value=json.loads(raw,object_pairs_hook=pairs)
    key='state' if sys.argv[1]=='status' else 'pending'
    if set(value)!={key,'success'} or value['success'] is not True: raise ValueError('fields')
    answer=value[key]
    if key=='state':
        if answer not in ('present','absent'): raise ValueError('state')
    elif type(answer)!=int or answer<0: raise ValueError('count')
    print(answer)
except (ValueError,TypeError,KeyError,OSError,OverflowError,RecursionError):
    sys.exit(1)
PYTHON
}
account_parse_status() { account_parse_snapshot status "$1"; }
account_parse_queue() { account_parse_snapshot queue "$1"; }

account_show() {
  local user slugs line
  account_container show account-delete show "$(jq -r .action.reportedSlug <<<"$state")" || return 1
  [[ $command_rc == 0 && $(wc -c <"$command_output") -le 1024 && $(wc -l <"$command_output") == 2 && $(tail -n 1 "$command_output") == 'account-delete: end' ]] || return 1
  line=$(head -n 1 "$command_output")
  [[ $line =~ ^account\ user=([0-9a-f-]{36})\ slugs=(.*)$ ]] || return 1
  user=${BASH_REMATCH[1]} slugs=${BASH_REMATCH[2]}
  slugs=$(jq -cn --arg s "$slugs" '$s|split(",")')
  account_slugs_valid "$slugs" || return 1
  [[ $(date -u +%s) -lt $(date -u -d "$(jq -r .planningDeadline <<<"$state")" +%s) ]] || return 1
  state=$(jq --arg u "$user" --argjson s "$slugs" \
    '.action.userId=$u | .action.shownSlugs=$s | .phase="planned" | .pendingVerb="none" | .intent="none" |
     .confirmation=("delete-account:"+.requestId+":"+$u+":"+.action.reportedSlug)' <<<"$state")
  operation_save
}
account_delete() {
  local parsed user slug
  user=$(jq -r .action.userId <<<"$state") slug=$(jq -r .action.reportedSlug <<<"$state")
  # This is the sole destructive dispatch. Lost result reconciles by UUID.
  account_container delete account-delete confirm "$slug" "$user" || return 1
  parsed=$(account_parse_write "$command_rc" "$command_output" "$user" "$slug")
  state=$(jq --argjson r "$parsed" '.action += $r | .outcome=(if $r.writeOutcome=="unknown" then "unknown" else "pending" end)' <<<"$state")
  operation_save
}
account_reconcile() {
  local user presence
  [[ $(jq -r .action.writeOutcome <<<"$state") != evidence_expired ]] || return 1
  user=$(jq -r .action.userId <<<"$state")
  account_container status account-delete status "$user" || return 1
  [[ $command_rc == 0 && $(wc -c <"$command_output") -le 1024 ]] || return 1
  presence=$(account_parse_status "$(<"$command_output")") || return 1
  if [[ $presence == absent ]]; then
    state=$(jq '.action.writeOutcome="absent_after_unknown" | .outcome="absent_after_unknown"' <<<"$state")
  else
    state=$(jq '.action.writeOutcome="present" | .outcome="refused"' <<<"$state")
  fi
  operation_save
}
account_cleanup() {
  local pending slug
  [[ $(jq -r .action.writeOutcome <<<"$state") =~ ^(committed|absent_after_unknown)$ ]] || return 0
  # Each known slug must be absent at the command boundary as well as HTTPS.
  while IFS= read -r slug; do
    account_container show-verify account-delete show "$slug" || return 1
    if [[ $command_rc != 4 || $(wc -c <"$command_output") -gt 1024 || $(wc -l <"$command_output") != 2 || $(head -n1 "$command_output") != "not found: $slug" || $(tail -n1 "$command_output") != 'account-delete: end' ]]; then
      state=$(jq '.action.publicVerification="failed"' <<<"$state")
      operation_save
      return 1
    fi
  done < <(jq -r '(.action.shownSlugs+(.action.returnedSlugs//[]))|unique|.[]' <<<"$state")
  # Recheck account absence before interpreting a global queue snapshot.
  account_container status account-delete status "$(jq -r .action.userId <<<"$state")" || return 1
  [[ $command_rc == 0 && $(account_parse_status "$(<"$command_output")") == absent ]] || return 1
  account_container queue media-deletion-status || return 1
  [[ $command_rc == 0 ]] || return 1
  pending=$(account_parse_queue "$(<"$command_output")") || return 1
  if [[ $pending == 0 ]]; then state=$(jq '.action.physicalCleanup="complete"' <<<"$state"); fi
  operation_save
}
