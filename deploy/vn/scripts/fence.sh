#!/usr/bin/env bash
# The release fence on the production host (docs/design/vietnam-production.md,
# "Release fence"; protocol in docs/design/passkey-release-fence.md). Runs as
# root on the host, installed at /usr/local/lib/aboutme/fence.sh. Every change
# to /var/lib/aboutme/fence.json happens under an exclusive flock and is
# written to a temporary file, synced, then renamed, so a crash leaves either
# the old or the new document and never a torn one.
#
#   fence.sh init                      create the file at the rebuilt-host floor
#   fence.sh read                      print the document
#   fence.sh lock <kind> <tag>         take the operation lock; prints its id
#   fence.sh checkpoint <id> <tag>     reprove the lock and floor before a start
#   fence.sh mutation-start <id> <tag> <kind>  mark a cutover write active
#   fence.sh mutation-clear <id> <kind>        clear its completed marker
#   fence.sh raise <id> <tag>          raise the minimum to <tag>
#   fence.sh release <id>              release the lock this id owns
#   fence.sh check <release> <unit>    ExecStartPre: refuse an unsafe start
#   fence.sh cutover-checkpoint <id> <tag> validate exact cutover ownership
#   fence.sh cutover-fail <id> <tag>    stop only that current cutover's consumers
#   fence.sh clear <reason>            privileged manual clear of a stale lock
#
# There is no time-based takeover: a crash leaves the lock closed until a
# manual clear. A missing or malformed file fails every command but init.
set -euo pipefail

dir=${FENCE_DIR:-/var/lib/aboutme}
file=$dir/fence.json
lockfile=$dir/fence.lock
mutation_file=$dir/cutover-mutation.json
# The highest floor in docs/design/passkey-release-fence.md that a rebuilt
# host starts at (docs/design/vietnam-production.md, "Release fence").
floor_tag=v0.4.7
floor_release=4007

say() { printf 'fence: %s\n' "$*" >&2; }
die() {
  say "$*"
  exit 1
}

# A strict vMAJOR.MINOR.PATCH tag, no leading zero, components 0 through 999,
# maps to MAJOR*1000000 + MINOR*1000 + PATCH.
release_number() {
  [[ $1 =~ ^v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})$ ]] || return 1
  echo $((10#${BASH_REMATCH[1]} * 1000000 + 10#${BASH_REMATCH[2]} * 1000 + 10#${BASH_REMATCH[3]}))
}

now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# Reads and validates the document into $doc, $minimum, and $owner.
load() {
  [[ -f $file && ! -L $file ]] || die "$file is missing; run fence.sh init on a rebuilt host"
  doc=$(<"$file")
  local tag
  minimum=$(jq -er '.minimum_release | select(type == "number" and . >= 0 and . == floor)' <<<"$doc" 2>/dev/null) ||
    die "$file is malformed: minimum_release"
  tag=$(jq -er '.minimum_tag | strings' <<<"$doc" 2>/dev/null) || die "$file is malformed: minimum_tag"
  [[ $(release_number "$tag" || true) == "$minimum" ]] || die "$file is malformed: minimum_tag does not match minimum_release"
  owner=$(jq -r '.operation_id // empty' <<<"$doc")
  jq -e 'if has("notice_authority") then
    .notice_authority | type=="object" and keys==["operation_id","state","version"]
      and .version==1 and (.state=="initializing" or .state=="adopted")
      and (.operation_id|type=="string" and test("^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$"))
    else true end' <<<"$doc" >/dev/null 2>&1 || die "invalid notice evidence"
  if [[ $dir == /var/lib/aboutme && $(jq -r '.notice_authority.state // empty' <<<"$doc") == adopted ]]; then
    notice_authority_check || die "invalid notice authority"
  fi
}

store() { # new document on stdin
  local tmp
  tmp=$(mktemp "$dir/.fence.XXXXXX") || return 1
  if ! jq 'if type == "object" and (.minimum_release | type) == "number" then . else error("malformed") end' >"$tmp"; then
    rm -f "$tmp" || true
    return 1
  fi
  if ! chmod 0644 "$tmp" || ! sync "$tmp" || ! mv -f "$tmp" "$file"; then
    rm -f -- "$tmp" || true
    return 1
  fi
  sync "$dir" || return 1
}

candidate_of() { # tag
  release_number "$1" || die "'$1' is not a strict vMAJOR.MINOR.PATCH tag"
}

with_lock() { # mode command...
  local mode=$1
  shift
  exec {fd}>>"$lockfile" || return 1
  flock "$mode" -w 30 "$fd" || die "could not take $lockfile within 30 s"
  if [[ $dir == /var/lib/aboutme ]]; then notice_recheck_mount || die "invalid data mounts"; fi
  "$@"
}

cmd_init() {
  if [[ -e $file ]]; then
    load
    say "kept $file (minimum $minimum)"
    return 0
  fi
  jq -n --argjson r "$floor_release" --arg t "$floor_tag" --arg s "$(now)" \
    '{minimum_release: $r, minimum_tag: $t, updated_at: $s}' | store || return 1
  say "created $file at $floor_tag ($floor_release)"
}

cmd_read() {
  load
  jq . <<<"$doc"
}

cmd_lock() { # kind tag
  local kind=$1 candidate oid ts
  case $kind in
    deploy | rollback | activate | totp_reencrypt | account_delete | view_pass_rotate | cutover | notice_restore) ;;
    *) die "unknown operation kind '$kind'" ;;
  esac
  candidate=$(candidate_of "$2")
  load
  [[ -z $owner ]] || die "an operation already holds the lock ($(jq -r '.operation_kind + " since " + .operation_started_at' <<<"$doc"))"
  ((candidate >= minimum)) || die "$2 ($candidate) is below the minimum release $minimum"
  oid=$(head -c32 /dev/urandom | basenc --base64url | tr -d '=')
  ts=$(now)
  jq --arg o "$oid" --arg k "$kind" --arg s "$ts" --arg tag "$2" \
    '.operation_id = $o | .operation_kind = $k | .operation_started_at = $s | .operation_checked_at = $s
     | .operation_tag=$tag' <<<"$doc" | store || return 1
  printf '%s\n' "$oid"
}

cmd_lock_for() { # kind tag request id
  local kind=$1 tag=$2 id=$3 candidate ts
  case $kind in account_delete | totp_reencrypt | view_pass_rotate) ;; *) die "unsupported bound operation" ;; esac
  [[ $id =~ ^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$ ]] || die "invalid request id"
  candidate=$(candidate_of "$tag")
  load
  ((candidate >= minimum)) || die "release is below the floor"
  if [[ -n $owner ]]; then
    [[ $owner == "$id" && $(jq -r .operation_kind <<<"$doc") == "$kind" && $(jq -r .operation_tag <<<"$doc") == "$tag" ]] || die "another binding owns the fence"
    printf '%s\n' "$id"
    return
  fi
  ts=$(now)
  jq --arg id "$id" --arg kind "$kind" --arg tag "$tag" --arg ts "$ts" \
    '.operation_id=$id | .operation_kind=$kind | .operation_tag=$tag | .operation_started_at=$ts | .operation_checked_at=$ts' <<<"$doc" | store || return 1
  printf '%s\n' "$id"
}

operation_release_guard() {
  [[ ! -e /srv/data/host-operations/key-change.json && ! -L /srv/data/host-operations/key-change.json ]] || die "a key-change marker remains"
  if [[ $(jq -r '.operation_kind // empty' <<<"$doc") == account_delete ]]; then
    local safety=${FENCE_SAFETY_LIB:-/usr/local/lib/aboutme/deploy-safety.sh}
    [[ -r $safety && ! -L $safety ]] || die "operation safety helper is missing"
    # shellcheck disable=SC1090
    source "$safety"
    aboutme_assert_operation_release "$owner" || die "the account operation remains unresolved or active"
  fi
}

owned_or_die() { # id
  load
  [[ -n $owner ]] || die "no operation holds the lock"
  [[ $owner == "$1" ]] || die "the lock belongs to another operation"
}

cmd_checkpoint() { # id tag
  local candidate
  candidate=$(candidate_of "$2")
  owned_or_die "$1"
  if [[ $(jq -r '.operation_tag // empty' <<<"$doc") != '' ]]; then
    [[ $(jq -r .operation_tag <<<"$doc") == "$2" ]] || die "the bound operation tag differs"
  fi
  ((candidate >= minimum)) || die "$2 ($candidate) is below the minimum release $minimum"
  jq --arg s "$(now)" '.operation_checked_at = $s' <<<"$doc" | store || return 1
}

cmd_raise() { # id tag
  local candidate
  candidate=$(candidate_of "$2")
  owned_or_die "$1"
  ((candidate >= minimum)) || die "$2 ($candidate) is below the minimum release $minimum"
  jq --argjson r "$candidate" --arg t "$2" --arg s "$(now)" \
    '.minimum_release = $r | .minimum_tag = $t | .updated_at = $s' <<<"$doc" | store || return 1
  say "raised the minimum to $2 ($candidate)"
}

mutation_kind_or_die() {
  case $1 in quiesce | reset-db | dump-restore | media) ;; *) die "unknown cutover mutation kind '$1'" ;; esac
}

load_mutation() {
  [[ -f $mutation_file && ! -L $mutation_file ]] || die "$mutation_file is missing"
  mutation=$(jq -ce '
    if type == "object" and keys == ["mutation_kind", "operation_id", "started_at", "state"]
      and (.operation_id | type == "string" and test("^[A-Za-z0-9_-]{43}$"))
      and (.mutation_kind | IN("quiesce", "reset-db", "dump-restore", "media"))
      and .state == "active" and (.started_at | type == "string")
    then . else error("malformed") end' "$mutation_file" 2>/dev/null) || die "$mutation_file is malformed"
}

store_mutation() { # document on stdin
  local tmp
  tmp=$(mktemp "$dir/.cutover-mutation.XXXXXX") || return 1
  if ! cat >"$tmp" || ! chmod 0600 "$tmp" || ! sync "$tmp" || ! mv -f "$tmp" "$mutation_file"; then
    rm -f -- "$tmp" || true
    return 1
  fi
  sync "$dir" || return 1
}

assert_cutover_owner() { # id
  owned_or_die "$1"
  [[ $(jq -r '.operation_kind // empty' <<<"$doc") == cutover ]] || die "the operation is not a cutover"
}

cutover_owner_is_valid() {
  jq -e '
    (.operation_id | type == "string" and test("^[A-Za-z0-9_-]{43}$"))
      and .operation_kind == "cutover"
      and (.operation_started_at | type == "string")
      and (.operation_checked_at | type == "string")
  ' <<<"$doc" >/dev/null 2>&1
}

assert_cutover_quiescent() {
  local safety=${FENCE_SAFETY_LIB:-/usr/local/lib/aboutme/deploy-safety.sh}
  [[ -r $safety && ! -L $safety ]] || die "$safety is missing or is a symlink"
  # shellcheck disable=SC1090
  source "$safety"
  aboutme_assert_cutover_quiescent || die "the host is not quiescent"
}

cmd_mutation_start() { # id tag kind
  local candidate
  mutation_kind_or_die "$3"
  candidate=$(candidate_of "$2")
  assert_cutover_owner "$1"
  ((candidate >= minimum)) || die "$2 ($candidate) is below the minimum release $minimum"
  [[ ! -e $mutation_file && ! -L $mutation_file ]] || die "a cutover mutation marker already exists"
  jq -n --arg o "$1" --arg k "$3" --arg s "$(now)" \
    '{operation_id: $o, mutation_kind: $k, state: "active", started_at: $s}' | store_mutation || return 1
}

cmd_mutation_clear() { # id kind
  mutation_kind_or_die "$2"
  assert_cutover_owner "$1"
  load_mutation
  [[ $(jq -r .operation_id <<<"$mutation") == "$1" && $(jq -r .mutation_kind <<<"$mutation") == "$2" ]] ||
    die "the cutover mutation marker belongs to another operation or kind"
  assert_cutover_quiescent
  rm -f "$mutation_file" && sync "$dir" || return 1
}

# Releasing an already released lock succeeds, so a retried release after a
# lost SSH reply settles the same way the first one did.
cmd_release() { # id
  load
  if [[ -z $owner ]]; then
    return 0
  fi
  [[ $owner == "$1" ]] || die "the lock belongs to another operation; the manual clear owns it"
  [[ $(jq -r .operation_kind <<<"$doc") != notice_restore ]] || die "notice restore needs exact completion"
  if [[ $(jq -r '.operation_kind // empty' <<<"$doc") == cutover && ( -e $mutation_file || -L $mutation_file ) ]]; then
    die "the cutover mutation marker remains; recover or clear it first"
  fi
  [[ $(jq -r '.crowdsec_cutover_started // false' <<<"$doc") != true ]] || die "cutover requires exact completion"
  operation_release_guard
  jq 'del(.operation_id, .operation_kind, .operation_tag, .operation_started_at, .operation_checked_at)' <<<"$doc" | store || return 1
}

# ExecStartPre for every production-image start. Takes a shared lock so it
# never reads a half-replaced file. A retained cutover lock or marker blocks
# every start except the owning reset-db operation's db-setup.
cmd_check() { # release unit
  local release=$1 unit=$2
  [[ $release =~ ^[0-9]+$ ]] || die "release '$release' is not a number"
  load
  mountpoint -q /srv/data || die "the data mount is missing"
  [[ ! -e /srv/data/host-operations/key-change.json && ! -L /srv/data/host-operations/key-change.json ]] || die "a key-change marker blocks starts"
  if ((release < minimum)); then
    echo "aboutme-fence-refused unit=$unit release=$release minimum=$minimum"
    exit 1
  fi
  if [[ $(jq -r '.operation_kind // empty' <<<"$doc") == notice_restore ||
    $(jq -r '.notice_authority.state // empty' <<<"$doc") == initializing ]]; then
    case $unit in aboutme-maintenance | postgresql@18-main | postgresql@18-main.service) return 0 ;; esac
    die "notice restore blocks starts"
  fi
  if [[ $dir == /var/lib/aboutme ]]; then
    case $unit in
      aboutme-maintenance | postgresql@18-main | postgresql@18-main.service) ;;
      *) notice_absent /var/lib/aboutme/privacy-notice/capture-in-progress || die "notice capture blocks starts" ;;
    esac
    case $unit in
      crowdsec* | aboutme-crowdsec*)
        notice_absent /var/lib/aboutme/privacy-notice/restore-reconciliation &&
          notice_safe_file /var/lib/aboutme/privacy-notice/activation.json || die "notice activation required" ;;
    esac
  fi
  if [[ $(jq -r '.operation_kind // empty' <<<"$doc") != cutover && ! -e $mutation_file && ! -L $mutation_file ]]; then
    return 0
  fi
  if [[ $unit == aboutme-db-setup && $(jq -r '.crowdsec_cutover_started // false' <<<"$doc") != true ]] && cutover_owner_is_valid; then
    load_mutation
    if [[ $(jq -r .operation_id <<<"$mutation") == "$owner" &&
      $(jq -r .mutation_kind <<<"$mutation") == reset-db ]]; then
      return 0
    fi
  fi
  if [[ $unit != *.service ]]; then unit+=.service; fi
  if load_cutover_library && cutover_consume "$unit" "$release"; then return 0; fi
  echo "aboutme-fence-refused unit=$unit retained-cutover-state"
  exit 1
}

# The privileged manual clear (docs/design/passkey-release-fence.md,
# "Serialized production operation"): refuses while a deploy-host.sh process
# runs, and records who cleared what and why in the journal.
cmd_clear() { # reason
  local reason=$1
  [[ -n $reason ]] || die "clear needs a reason"
  if pgrep -f /usr/local/lib/aboutme/deploy-host.sh >/dev/null; then
    die "a deploy-host.sh process is running; wait for it to stop"
  fi
  [[ ! -e $mutation_file && ! -L $mutation_file ]] || die "a cutover mutation marker remains; recover it first"
  load
  [[ $(jq -r '.operation_kind // empty' <<<"$doc") != notice_restore ]] || die "notice restore requires exact-owner recovery"
  [[ $(jq -r '.crowdsec_cutover_started // false' <<<"$doc") != true ]] || die "cutover requires exact completion"
  [[ -n $owner ]] || {
    say "no lock to clear"
    return 0
  }
  logger -t aboutme-fence "manual clear of $(jq -c '{operation_kind, operation_started_at, operation_checked_at}' <<<"$doc") by ${SUDO_USER:-root}: $reason"
  operation_release_guard
  jq 'del(.operation_id, .operation_kind, .operation_tag, .operation_started_at, .operation_checked_at)' <<<"$doc" | store || return 1
  say "cleared"
}

recover_processes_stopped() { # kind
  ! pgrep -f '[/]usr/local/lib/aboutme/deploy-host.sh' >/dev/null || die "a deploy-host.sh process is running"
  case $1 in
    quiesce) ;;
    reset-db)
      ! pgrep -f '([d]ropdb|[c]reatedb).*aboutme' >/dev/null || die "a database reset process is running"
      ! systemctl is-active --quiet aboutme-db-setup.service || die "aboutme-db-setup.service is running"
      ;;
    dump-restore) ! pgrep -f '([p]g_restore.*aboutme|[a]ge -d .*cutover)' >/dev/null || die "a dump restore process is running" ;;
  esac
}

recover_marker() { # id kind reason [attestation]
  local id=$1 kind=$2 reason=$3 attestation=${4-}
  [[ -n $reason ]] || die "recovery needs a reason"
  assert_cutover_owner "$id"
  load_mutation
  [[ $(jq -r .operation_id <<<"$mutation") == "$id" && $(jq -r .mutation_kind <<<"$mutation") == "$kind" ]] ||
    die "the cutover mutation marker belongs to another operation or kind"
  recover_processes_stopped "$kind"
  assert_cutover_quiescent
  logger -t aboutme-fence "cutover mutation recovery marker=$mutation by ${SUDO_USER:-root} attestation=${attestation:-none}: $reason"
  rm -f "$mutation_file" && sync "$dir" || return 1
  say "cleared the cutover mutation marker; the operation lock remains"
}

cmd_recover_cutover() { # id kind reason
  case $2 in quiesce | reset-db | dump-restore) ;; *) die "unknown host cutover mutation kind '$2'" ;; esac
  recover_marker "$1" "$2" "$3"
}

cmd_recover_cutover_media() { # id attestation reason
  [[ $2 == media-writer-stopped-and-result-inspected ]] || die "media recovery needs the exact stop-and-inspect attestation"
  recover_marker "$1" media "$3" "$2"
}

cmd_notice_authority() {
  local id=$1 state=$2 kind previous
  [[ $id =~ ^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$ && ( $state == initializing || $state == adopted ) ]] || die "invalid notice evidence"
  owned_or_die "$id"
  kind=$(jq -r .operation_kind <<<"$doc")
  if [[ $kind == cutover ]]; then
    load_mutation
    [[ $(jq -r .operation_id <<<"$mutation") == "$id" && $(jq -r .mutation_kind <<<"$mutation") == quiesce ]] || die "notice adoption needs quiescence"
  else [[ $kind == notice_restore ]] || die "invalid notice operation"; fi
  previous=$(jq -r '.notice_authority.state // empty' <<<"$doc")
  [[ $previous != adopted || $state == adopted ]] || die "notice adoption cannot regress"
  if [[ $previous != '' ]]; then
    [[ $(jq -r .notice_authority.operation_id <<<"$doc") == "$id" ]] || die "notice evidence belongs to another operation"
  fi
  if [[ $state == adopted ]]; then
    notice_authority_check && notice_safe_file /var/lib/aboutme/privacy-notice/restore-reconciliation &&
      [[ ! -s /var/lib/aboutme/privacy-notice/restore-reconciliation ]] &&
      notice_absent /var/lib/aboutme/privacy-notice/send.json && notice_absent /var/lib/aboutme/privacy-notice/activation.json &&
      sync /var/lib/aboutme/privacy-notice || die "invalid adopted authority"
  fi
  jq --arg id "$id" --arg state "$state" '.notice_authority={version:1,state:$state,operation_id:$id}' <<<"$doc" | store || return 1
}
cmd_notice_restore_release() {
  owned_or_die "$1"
  [[ $(jq -r .operation_kind <<<"$doc") == notice_restore && $(jq -r .notice_authority.state <<<"$doc") == adopted ]] || die "invalid restore owner"
  operation_release_guard
  notice_authority_check && notice_safe_file /var/lib/aboutme/privacy-notice/restore-reconciliation &&
    [[ ! -s /var/lib/aboutme/privacy-notice/restore-reconciliation ]] &&
    notice_absent /var/lib/aboutme/privacy-notice/send.json && notice_absent /var/lib/aboutme/privacy-notice/activation.json || die "restore denial is incomplete"
  jq 'del(.operation_id,.operation_kind,.operation_tag,.operation_started_at,.operation_checked_at)' <<<"$doc" | store || return 1
}

cmd_check_current() {
  load
  local number
  cutover_safe /var/lib/aboutme/release.json 644 || die 'release record missing'
  number=$(jq -er .release_number /var/lib/aboutme/release.json) || die 'release number missing'
  cmd_check "$number" "$1"
}
cmd_start_unit() {
  local id=$1 tag=$2 unit=$3 result=0
  (with_lock -x cutover_start_prepare "$id" "$tag" "$unit") || return 1
  # ExecStartPre takes the same lock; never retain it across this wait.
  systemctl start "$unit" >/dev/null 2>&1 || result=1
  if ((result==0)); then
    (with_lock -x cutover_start_finish "$id" "$tag" "$unit") && return 0
  fi
  (with_lock -x cutover_failure "$id" "$tag") || true
  return 1
}
cmd_prepare_completion() {
  # Hold shared ownership while joining the watcher. A running watcher can
  # finish under its own shared guard; no exclusive helper runs here.
  cutover_validate_owner "$1" "$2" || return 1
  systemctl stop aboutme-watch.timer aboutme-watch.service >/dev/null 2>&1 || return 1
}
cmd_complete_cutover() {
  (with_lock -s cmd_prepare_completion "$1" "$2") || return 1
  if ! (with_lock -x cutover_complete "$1" "$2"); then
    (with_lock -x cutover_failure "$1" "$2") || true
    return 1
  fi
}
cmd_cutover_checkpoint() {
  cutover_validate_owner "$1" "$2" && cmd_checkpoint "$1" "$2"
}
load_cutover_library() {
  local cutover_library
  cutover_library=$(dirname "${BASH_SOURCE[0]}")/crowdsec-cutover-state.sh
  if [[ ! -f $cutover_library ]]; then cutover_library=$(dirname "${BASH_SOURCE[0]}")/../host/crowdsec-cutover-state.sh; fi
  [[ -f $cutover_library && ! -L $cutover_library ]] || return 1
  # shellcheck source=../host/crowdsec-cutover-state.sh
  source "$cutover_library"
}

[[ $(id -u) == 0 ]] || die "run as root on the host"
umask 022
# Keep the existing root-only fixture seam; installed access always proves the
# canonical guest mounts before touching fence state or opening its lock.
if [[ $dir == /var/lib/aboutme ]]; then
  notice_library=$(dirname "${BASH_SOURCE[0]}")/crowdsec-privacy-notice-host.sh
  if [[ ! -f $notice_library ]]; then notice_library=$(dirname "${BASH_SOURCE[0]}")/../host/crowdsec-privacy-notice-host.sh; fi
  # shellcheck source=../host/crowdsec-privacy-notice-host.sh
  source "$notice_library"
  # shellcheck disable=SC2034 # Used by the sourced canonical mount recheck.
  notice_identity=$(notice_mount_identity) || die "invalid data mounts"
fi
mkdir -p "$dir"

case "$#:${1-}" in
  1:init) with_lock -x cmd_init ;;
  1:read) with_lock -s cmd_read ;;
  3:lock) with_lock -x cmd_lock "$2" "$3" ;;
  4:lock-for) with_lock -x cmd_lock_for "$2" "$3" "$4" ;;
  3:checkpoint) with_lock -x cmd_checkpoint "$2" "$3" ;;
  4:mutation-start) with_lock -x cmd_mutation_start "$2" "$3" "$4" ;;
  3:mutation-clear) with_lock -x cmd_mutation_clear "$2" "$3" ;;
  3:raise) with_lock -x cmd_raise "$2" "$3" ;;
  2:release) with_lock -x cmd_release "$2" ;;
  3:notice-authority) with_lock -x cmd_notice_authority "$2" "$3" ;;
  2:notice-restore-release) with_lock -x cmd_notice_restore_release "$2" ;;
  3:check) with_lock -x cmd_check "$2" "$3" ;;
  2:safety-result) load_cutover_library || die 'cutover helper missing'; with_lock -x cutover_safety_result "$2" ;;
  2:check-current) load_cutover_library || die 'cutover helper missing'; with_lock -x cmd_check_current "$2" ;;
  4:start-unit) load_cutover_library || die 'cutover helper missing'; cmd_start_unit "$2" "$3" "$4" ;;
  3:cutover-mode) load_cutover_library || die 'cutover helper missing'; with_lock -x cutover_create_mode "$2" "$3" ;;
  3:cutover-watch) load_cutover_library || die 'cutover helper missing'; with_lock -x cutover_watch_pass "$2" "$3" ;;
  3:cutover-complete) load_cutover_library || die 'cutover helper missing'; cmd_complete_cutover "$2" "$3" ;;
  3:cutover-fail) load_cutover_library || die 'cutover helper missing'; with_lock -x cutover_failure "$2" "$3" ;;
  3:cutover-checkpoint) load_cutover_library || die 'cutover helper missing'; with_lock -x cmd_cutover_checkpoint "$2" "$3" ;;
  2:clear) with_lock -x cmd_clear "$2" ;;
  4:recover-cutover) with_lock -x cmd_recover_cutover "$2" "$3" "$4" ;;
  4:recover-cutover-media) with_lock -x cmd_recover_cutover_media "$2" "$3" "$4" ;;
  *)
    say "usage: fence.sh init | read | lock <kind> <tag> | checkpoint <id> <tag> | mutation-start <id> <tag> <kind> | mutation-clear <id> <kind> | raise <id> <tag> | release <id> | check <release> <unit> | clear <reason> | recover-cutover <id> <kind> <reason> | recover-cutover-media <id> media-writer-stopped-and-result-inspected <reason>"
    exit 2
    ;;
esac
