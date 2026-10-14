#!/usr/bin/env bash
# Root entry point. Target and confirmation arrive only in the stdin envelope.
set -euo pipefail
umask 077
ulimit -c 0
lib=/usr/local/lib/aboutme
# shellcheck source=deploy/vn/host/operation-state.sh
source "$lib/operation-state.sh"
# shellcheck source=deploy/vn/host/operation-account.sh
source "$lib/operation-account.sh"

refuse() { printf '%s\n' "${1:-refused}" >&2; exit "${2:-1}"; }
request_lock() {
  [[ -d $operations_dir/$id && ! -L $operations_dir/$id ]] || refuse
  [[ ! -L $operations_dir/$id/.request.lock ]] || refuse
  exec {request_fd}>>"$operations_dir/$id/.request.lock"
  [[ $(stat -c '%u:%a' "$operations_dir/$id/.request.lock") == 0:600 ]] || refuse
  flock -w 30 "$request_fd" || refuse
  operation_load "$id" || refuse
}
request_unit_stopped() {
  local unit_state
  unit_state=$(systemctl show --property=ActiveState --property=Job "aboutme-operation@$id.service" 2>/dev/null) || return 1
  [[ $(wc -l <<<"$unit_state") == 2 ]] && grep -qxE 'ActiveState=(inactive|failed)' <<<"$unit_state" && grep -qxF 'Job=0' <<<"$unit_state"
}
request_fence() {
  local tag
  tag=$(jq -r .release.release_tag <<<"$state")
  "$lib/fence.sh" lock-for account_delete "$tag" "$id" >/dev/null 2>&1 || return 1
  state=$(jq '.fenceHeld=true | .reserved=true' <<<"$state")
  operation_save
}
request_release() {
  # The controller releases after the request unit exits, never from an EXIT trap.
  # An applicable plan keeps its capacity until quiescent closure.
  request_unit_stopped && account_attempts_stopped || return 1
  local held owner
  held=$(jq -r .fenceHeld <<<"$state")
  state=$(jq '.fenceHeld=false | .reserved=(.phase=="planned") | .closureQuiescent=true | .intent="none" | .pendingVerb="none"' <<<"$state")
  operation_save || return 1
  owner=$("$lib/fence.sh" read 2>/dev/null | jq -er '.operation_id // "none"') || return 1
  if [[ $owner == "$id" ]]; then
    "$lib/fence.sh" release "$id" >/dev/null 2>&1
  elif [[ $owner != none && $held == true ]]; then
    return 1
  fi
}
request_start() {
  request_unit_stopped || refuse
  request_fence || refuse
  systemctl reset-failed "aboutme-operation@$id.service" >/dev/null 2>&1 || true
  systemctl start --no-block "aboutme-operation@$id.service" >/dev/null 2>&1 || refuse
}

request_new() {
  local slug=$1 now expiry deadline
  state=''
  account_live || refuse
  id=$(head -c 32 /dev/urandom | basenc --base64url | tr -d '=\n')
  now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  expiry=$(date -u -d '+15 minutes' +%Y-%m-%dT%H:%M:%SZ)
  deadline=$(date -u -d '+60 seconds' +%Y-%m-%dT%H:%M:%SZ)
  state=$(jq -cn --arg id "$id" --arg slug "$slug" --arg now "$now" --arg expiry "$expiry" --arg deadline "$deadline" \
    --argjson live "$live_release" --arg flags "$flags_hash" --arg secret "$db_secret_id" '
    {schemaVersion:1,requestId:$id,kind:"account-delete",phase:"planning",outcome:"pending",createdAt:$now,
     expiresAt:$expiry,confirmation:null,release:$live,planningDeadline:$deadline,flagsHash:$flags,dbSecretId:$secret,
     attemptCount:0,attempts:[],intent:"none",pendingVerb:"show",fenceHeld:false,reserved:true,closureQuiescent:true,evidenceAt:$now,
     challenge:null,recoveryCopy:null,proofReceipt:null,
     action:{userId:null,reportedSlug:$slug,shownSlugs:[],returnedSlugs:null,writeOutcome:"not_started",publicVerification:"pending",physicalCleanup:"pending"}}')
  state=$(operation_state new '' 3<<<"$state") || refuse
  request_lock
  request_start
  flock -u "$request_fd"
  # The detached unit owns the read. SSH loss cannot extend its planning deadline.
  local stop_at=$((SECONDS+60))
  while ((SECONDS<stop_at)); do
    operation_load "$id" || refuse
    if [[ $(jq -r .phase <<<"$state") != planning ]] && request_unit_stopped; then break; fi
    sleep 1
  done
  if request_unit_stopped; then
    request_lock
    request_finish || refuse
  fi
  if [[ $(jq -r .outcome <<<"$state") == refused && $(jq -r .action.writeOutcome <<<"$state") == not_found ]]; then
    refuse not_found 4
  fi
  operation_output "$id"
}

request_challenge() {
  local nonce expires digest
  nonce=$(head -c32 /dev/urandom | basenc --base64url | tr -d '=\n')
  expires=$(date -u -d '+5 minutes' +%Y-%m-%dT%H:%M:%SZ)
  digest=$(jq -r '.release.server_image|split("@")[1]' <<<"$state")
  state=$(jq '.reserved=true' <<<"$state")
  operation_save || return 1
  state=$(jq --arg nonce "$nonce" --arg expiry "$expires" --arg digest "$digest" \
    '.challenge={requestId:.requestId,digest:$digest,phase:"serving",nonce:$nonce,expiresAt:$expiry} | .proofReceipt=null' <<<"$state")
  operation_save || return 1
  state=$(jq '.reserved=false' <<<"$state")
  operation_save
}
request_finish() {
  local outcome phase
  request_unit_stopped && account_attempts_stopped || return 1
  phase=$(jq -r .phase <<<"$state") outcome=$(jq -r .action.writeOutcome <<<"$state")
  if [[ $phase == planned ]]; then
    if [[ $(date -u +%s) -ge $(date -u -d "$(jq -r .expiresAt <<<"$state")" +%s) || $(jq -r .pendingVerb <<<"$state") == abort ]]; then
      state=$(jq '.phase="closed" | .outcome="aborted" | .confirmation=null | .expiresAt=null' <<<"$state")
      operation_save || return 1
    fi
    request_release
    return
  fi
  if [[ $phase == planning ]]; then
    state=$(jq '.phase="closed" | .outcome="refused" | .expiresAt=null | .confirmation=null' <<<"$state")
    operation_save && request_release
    return
  fi
  if [[ $phase == closed ]]; then
    request_release
    return
  fi
  case $outcome in
    unknown) return 0 ;;
    evidence_expired)
      [[ $(jq -r .pendingVerb <<<"$state") == abort ]] || return 1
      state=$(jq '.phase="closed" | .outcome="unverified" | .expiresAt=null | .confirmation=null' <<<"$state")
      operation_save && request_release
      ;;
    committed | absent_after_unknown)
      local pending_verb
      pending_verb=$(jq -r .pendingVerb <<<"$state")
      state=$(jq '.phase="serving" | .expiresAt=null | .confirmation=null' <<<"$state")
      operation_save && request_release || return 1
      if [[ $pending_verb == verify ]]; then request_challenge || return 1; fi
      # Dispatch the idempotent existing job only after durable fence release.
      state=$(jq '.reserved=true | .intent="sweep"' <<<"$state")
      operation_save || return 1
      systemctl start --no-block aboutme-job@media-deletion-sweep.service >/dev/null 2>&1 || return 1
      state=$(jq '.reserved=false | .intent="none"' <<<"$state")
      operation_save || return 1
      ;;
    not_found | mismatch | present)
      state=$(jq '.phase="closed" | .outcome="refused" | .expiresAt=null | .confirmation=null' <<<"$state")
      operation_save && request_release
      ;;
    not_started)
      if [[ $(jq -r .pendingVerb <<<"$state") == abort ]]; then
        state=$(jq '.phase="closed" | .outcome="aborted" | .expiresAt=null | .confirmation=null' <<<"$state")
        operation_save && request_release
      fi
      ;;
  esac
}

request_existing() {
  local verb=$1 confirmation=${2:-} phase write
  request_lock
  phase=$(jq -r .phase <<<"$state") write=$(jq -r .action.writeOutcome <<<"$state")
  if [[ $verb == apply ]]; then
    [[ $(jq -r .action.userId <<<"$state") != null && $confirmation == "delete-account:$id:$(jq -r .action.userId <<<"$state"):$(jq -r .action.reportedSlug <<<"$state")" ]] || refuse invalid_request 2
    if [[ $phase != planned ]]; then
      [[ $(jq -r .pendingVerb <<<"$state") != show ]] || refuse
      operation_output "$id"; return
    fi
    [[ $confirmation == $(jq -r .confirmation <<<"$state") ]] || refuse invalid_request 2
    [[ $(date -u +%s) -lt $(date -u -d "$(jq -r .expiresAt <<<"$state")" +%s) ]] || refuse
    request_unit_stopped || refuse
    state=$(jq '.reserved=true' <<<"$state")
    operation_save || refuse
    account_attempts_stopped || refuse
    account_live || refuse
    state=$(jq '.phase="accepted" | .expiresAt=null | .confirmation=null | .pendingVerb="apply"' <<<"$state")
    operation_save || refuse
    request_start
    operation_output "$id"; return
  fi
  if [[ $phase == closed ]]; then
    if [[ $(jq -r .fenceHeld <<<"$state") == true ]]; then request_finish || refuse; fi
    operation_output "$id"; return
  fi
  if [[ $write == evidence_expired && $verb != abort ]]; then refuse; fi
  request_unit_stopped || refuse
  state=$(jq '.reserved=true' <<<"$state")
  operation_save || refuse
  account_attempts_stopped || refuse
  # A crash after write intent is unknown even if no result was saved.
  if [[ $(jq -r .intent <<<"$state") == delete && $write == not_started ]]; then
    state=$(jq '.action.writeOutcome="unknown" | .outcome="unknown"' <<<"$state")
    operation_save || refuse
    write=unknown
  fi
  if [[ $phase == planned && $verb == resume ]]; then
    request_finish || refuse
    operation_output "$id"; return
  fi
  if [[ $write == evidence_expired ]]; then
    [[ $verb == abort ]] || refuse
    state=$(jq '.pendingVerb="abort"' <<<"$state")
    operation_save && request_finish || refuse
    operation_output "$id"; return
  fi
  if [[ $verb == abort && $write == not_started ]]; then
    state=$(jq '.pendingVerb="abort"' <<<"$state")
    operation_save && request_finish || refuse
    operation_output "$id"; return
  fi
  if [[ $verb == resume && $phase == accepted && $write == not_started && $(jq -r .intent <<<"$state") == none ]]; then
    # No create can precede durable delete intent, so this accepted request
    # has no destructive attempt to replay.
    account_live || refuse
    state=$(jq '.pendingVerb="apply"' <<<"$state")
    operation_save || refuse
    request_start
    operation_output "$id"; return
  fi
  if [[ $write == unknown ]]; then
    [[ $verb == resume || $verb == abort ]] || refuse
  elif [[ $verb == resume || $verb == abort ]]; then
    request_finish || refuse
    operation_output "$id"; return
  fi
  if [[ $verb == verify ]]; then
    [[ $write == committed || $write == absent_after_unknown ]] || refuse
    request_finish || refuse
    # Read verification can use a later capable release after the write fence
    # has gone. Bind the new read challenge to that live release.
    local saved_state=$state
    state=''
    account_live || refuse
    state=$(jq --argjson live "$live_release" --arg flags "$flags_hash" --arg secret "$db_secret_id" \
      '.release=$live | .flagsHash=$flags | .dbSecretId=$secret | .challenge=null | .proofReceipt=null | .action.physicalCleanup="pending" | .action.publicVerification="pending"' <<<"$saved_state")
  else
    account_live || refuse
  fi
  state=$(jq --arg verb "$verb" '.pendingVerb=$verb | .reserved=true' <<<"$state")
  operation_save || refuse
  request_start
  operation_output "$id"
}

request_proof() {
  request_lock
  [[ $(jq -r .challenge.nonce <<<"$state") == $(jq -r .nonce <<<"$envelope") &&
    $(jq -r .challenge.phase <<<"$state") == $(jq -r .phase <<<"$envelope") &&
    $(date -u +%s) -lt $(date -u -d "$(jq -r .challenge.expiresAt <<<"$state")" +%s) ]] || refuse
  local result nonce existing
  result=$(jq -r .result <<<"$envelope") nonce=$(jq -r .nonce <<<"$envelope")
  existing=$(jq -r '.proofReceipt.result // empty' <<<"$state")
  [[ -z $existing || $existing == "$result" ]] || refuse
  account_live || refuse
  [[ $(jq -r .phase <<<"$state") == serving && $(jq -r .fenceHeld <<<"$state") == false ]] || refuse
  if [[ -n $existing ]]; then operation_output "$id"; return; fi
  state=$(jq '.reserved=true' <<<"$state")
  operation_save || refuse
  state=$(jq --arg result "$result" --arg nonce "$nonce" \
    '.proofReceipt={nonce:$nonce,result:$result} | .action.publicVerification=(if $result=="pass" then (if .action.publicVerification=="failed" then "failed" else "limited" end) else "failed" end)' <<<"$state")
  # Versioned cards and a possibly lost committed slug list limit public proof.
  operation_save || refuse
  state=$(jq '.reserved=false' <<<"$state")
  operation_save || refuse
  operation_output "$id"
}

operation_effect() {
  operation_initialize || refuse
  account_live && request_fence || refuse
  account_attempts_stopped || refuse
  local verb rc=0
  verb=$(jq -r .pendingVerb <<<"$state")
  case $verb in
    show)
      account_show || rc=$?
      if ((rc != 0)); then
        if [[ ${command_rc:-1} == 4 && -n ${command_output:-} && $(wc -c <"$command_output") -le 1024 && $(wc -l <"$command_output") == 2 && $(head -n1 "$command_output") == "not found: $(jq -r .action.reportedSlug <<<"$state")" && $(tail -n1 "$command_output") == 'account-delete: end' ]]; then
          state=$(jq '.action.writeOutcome="not_found"' <<<"$state")
        fi
        state=$(jq '.phase="closed" | .outcome="refused" | .expiresAt=null | .confirmation=null' <<<"$state")
      fi
      ;;
    apply)
      if [[ $(jq -r .intent <<<"$state") == delete || $(jq -r .action.writeOutcome <<<"$state") != not_started ]]; then refuse; fi
      account_delete || {
        state=$(jq '.action.writeOutcome="unknown" | .outcome="unknown"' <<<"$state")
        rc=1
      }
      ;;
    resume | abort) account_reconcile || rc=1 ;;
    verify) account_cleanup || rc=1 ;;
    *) refuse ;;
  esac
  if [[ -n ${command_output:-} && -f $command_output && ! -L $command_output ]]; then rm -f "$command_output"; fi
  operation_save || refuse
  ((rc == 0)) || refuse
}

operation_prune() {
  local path
  for path in "$operations_dir"/*; do
    [[ -d $path && ! -L $path ]] || continue
    id=${path##*/}
    operation_id_valid "$id" || return 1
    (
      [[ ! -L $path/.request.lock ]] || exit 1
      exec {prune_fd}>"$path/.request.lock"
      flock -n "$prune_fd" || exit 0
      operation_load "$id" || exit 1
      if [[ $(jq -r .phase <<<"$state") == planned && $(jq -r .reserved <<<"$state") == true && $(date -u +%s) -ge $(date -u -d "$(jq -r .expiresAt <<<"$state")" +%s) ]]; then
        # Retention never releases a fence. Expired, already released plans
        # may return capacity only after fresh quiescence checks.
        [[ $(jq -r .fenceHeld <<<"$state") == false ]] || exit 0
        request_unit_stopped && account_attempts_stopped || exit 0
        state=$(jq '.phase="closed" | .outcome="aborted" | .expiresAt=null | .confirmation=null | .reserved=false | .pendingVerb="none" | .intent="none"' <<<"$state")
        operation_save || exit 1
      fi
    ) || return 1
  done
  operation_state prune ''
}

operation_main() {
  [[ $EUID == 0 ]] || refuse
  operation_initialize || refuse
  case "$#:${1:-}" in
    1:request)
      envelope=$(operation_state envelope '' 3<&0 2>/dev/null) || refuse invalid_request 2
      id=$(jq -r '.requestId // empty' <<<"$envelope")
      case $(jq -r .verb <<<"$envelope") in
        show) request_new "$(jq -r .slug <<<"$envelope")" ;;
        status) operation_output "$id" || refuse ;;
        apply) request_existing apply "$(jq -r .confirmation <<<"$envelope")" ;;
        resume | verify | abort) request_existing "$(jq -r .verb <<<"$envelope")" ;;
        proof) request_proof ;;
      esac
      ;;
    2:run)
      id=$2
      operation_id_valid "$id" || refuse invalid_request 2
      request_lock
      request_fence || refuse
      # Keep request and fence order before the shared workload lock. The child
      # inherits the request flock; it opens no second request lock.
      "$lib/workload-lock.sh" 60 bash -c \
        'source /usr/local/lib/aboutme/operation-run.sh; id=$1; operation_load "$id"; operation_effect' bash "$id"
      ;;
    1:prune) operation_prune ;;
    *) refuse invalid_request 2 ;;
  esac
}
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then operation_main "$@"; fi
