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
}

store() { # new document on stdin
  local tmp
  tmp=$(mktemp "$dir/.fence.XXXXXX")
  if ! jq 'if type == "object" and (.minimum_release | type) == "number" then . else error("malformed") end' >"$tmp"; then
    rm -f "$tmp"
    die "refusing to write a malformed document"
  fi
  chmod 0644 "$tmp"
  sync "$tmp"
  mv -f "$tmp" "$file"
  sync "$dir"
}

candidate_of() { # tag
  release_number "$1" || die "'$1' is not a strict vMAJOR.MINOR.PATCH tag"
}

with_lock() { # mode command...
  local mode=$1
  shift
  exec {fd}>>"$lockfile"
  flock "$mode" -w 30 "$fd" || die "could not take $lockfile within 30 s"
  "$@"
}

cmd_init() {
  if [[ -e $file ]]; then
    load
    say "kept $file (minimum $minimum)"
    return 0
  fi
  jq -n --argjson r "$floor_release" --arg t "$floor_tag" --arg s "$(now)" \
    '{minimum_release: $r, minimum_tag: $t, updated_at: $s}' | store
  say "created $file at $floor_tag ($floor_release)"
}

cmd_read() {
  load
  jq . <<<"$doc"
}

cmd_lock() { # kind tag
  local kind=$1 candidate oid ts
  case $kind in
    deploy | rollback | activate | totp_reencrypt | cutover) ;;
    *) die "unknown operation kind '$kind'" ;;
  esac
  candidate=$(candidate_of "$2")
  load
  [[ -z $owner ]] || die "an operation already holds the lock ($(jq -r '.operation_kind + " since " + .operation_started_at' <<<"$doc"))"
  ((candidate >= minimum)) || die "$2 ($candidate) is below the minimum release $minimum"
  oid=$(head -c32 /dev/urandom | basenc --base64url | tr -d '=')
  ts=$(now)
  jq --arg o "$oid" --arg k "$kind" --arg s "$ts" \
    '.operation_id = $o | .operation_kind = $k | .operation_started_at = $s | .operation_checked_at = $s' <<<"$doc" | store
  printf '%s\n' "$oid"
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
  ((candidate >= minimum)) || die "$2 ($candidate) is below the minimum release $minimum"
  jq --arg s "$(now)" '.operation_checked_at = $s' <<<"$doc" | store
}

cmd_raise() { # id tag
  local candidate
  candidate=$(candidate_of "$2")
  owned_or_die "$1"
  ((candidate >= minimum)) || die "$2 ($candidate) is below the minimum release $minimum"
  jq --argjson r "$candidate" --arg t "$2" --arg s "$(now)" \
    '.minimum_release = $r | .minimum_tag = $t | .updated_at = $s' <<<"$doc" | store
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
  tmp=$(mktemp "$dir/.cutover-mutation.XXXXXX")
  cat >"$tmp"
  chmod 0600 "$tmp"
  sync "$tmp"
  mv -f "$tmp" "$mutation_file"
  sync "$dir"
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
    '{operation_id: $o, mutation_kind: $k, state: "active", started_at: $s}' | store_mutation
}

cmd_mutation_clear() { # id kind
  mutation_kind_or_die "$2"
  assert_cutover_owner "$1"
  load_mutation
  [[ $(jq -r .operation_id <<<"$mutation") == "$1" && $(jq -r .mutation_kind <<<"$mutation") == "$2" ]] ||
    die "the cutover mutation marker belongs to another operation or kind"
  assert_cutover_quiescent
  rm -f "$mutation_file"
  sync "$dir"
}

# Releasing an already released lock succeeds, so a retried release after a
# lost SSH reply settles the same way the first one did.
cmd_release() { # id
  load
  if [[ -z $owner ]]; then
    return 0
  fi
  [[ $owner == "$1" ]] || die "the lock belongs to another operation; the manual clear owns it"
  if [[ $(jq -r '.operation_kind // empty' <<<"$doc") == cutover && ( -e $mutation_file || -L $mutation_file ) ]]; then
    die "the cutover mutation marker remains; recover or clear it first"
  fi
  jq 'del(.operation_id, .operation_kind, .operation_started_at, .operation_checked_at)' <<<"$doc" | store
}

# ExecStartPre for every production-image start. Takes a shared lock so it
# never reads a half-replaced file. A retained cutover lock or marker blocks
# every start except the owning reset-db operation's db-setup.
cmd_check() { # release unit
  local release=$1 unit=$2
  [[ $release =~ ^[0-9]+$ ]] || die "release '$release' is not a number"
  load
  if ((release < minimum)); then
    echo "aboutme-fence-refused unit=$unit release=$release minimum=$minimum"
    exit 1
  fi
  if [[ $(jq -r '.operation_kind // empty' <<<"$doc") != cutover && ! -e $mutation_file && ! -L $mutation_file ]]; then
    return 0
  fi
  if [[ $unit == aboutme-db-setup ]] && cutover_owner_is_valid; then
    load_mutation
    if [[ $(jq -r .operation_id <<<"$mutation") == "$owner" &&
      $(jq -r .mutation_kind <<<"$mutation") == reset-db ]]; then
      return 0
    fi
  fi
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
  [[ -n $owner ]] || {
    say "no lock to clear"
    return 0
  }
  logger -t aboutme-fence "manual clear of $(jq -c '{operation_kind, operation_started_at, operation_checked_at}' <<<"$doc") by ${SUDO_USER:-root}: $reason"
  jq 'del(.operation_id, .operation_kind, .operation_started_at, .operation_checked_at)' <<<"$doc" | store
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
  rm -f "$mutation_file"
  sync "$dir"
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

[[ $(id -u) == 0 ]] || die "run as root on the host"
umask 022
mkdir -p "$dir"

case "$#:${1-}" in
  1:init) with_lock -x cmd_init ;;
  1:read) with_lock -s cmd_read ;;
  3:lock) with_lock -x cmd_lock "$2" "$3" ;;
  3:checkpoint) with_lock -x cmd_checkpoint "$2" "$3" ;;
  4:mutation-start) with_lock -x cmd_mutation_start "$2" "$3" "$4" ;;
  3:mutation-clear) with_lock -x cmd_mutation_clear "$2" "$3" ;;
  3:raise) with_lock -x cmd_raise "$2" "$3" ;;
  2:release) with_lock -x cmd_release "$2" ;;
  3:check) with_lock -s cmd_check "$2" "$3" ;;
  2:clear) with_lock -x cmd_clear "$2" ;;
  4:recover-cutover) with_lock -x cmd_recover_cutover "$2" "$3" "$4" ;;
  4:recover-cutover-media) with_lock -x cmd_recover_cutover_media "$2" "$3" "$4" ;;
  *)
    say "usage: fence.sh init | read | lock <kind> <tag> | checkpoint <id> <tag> | mutation-start <id> <tag> <kind> | mutation-clear <id> <kind> | raise <id> <tag> | release <id> | check <release> <unit> | clear <reason> | recover-cutover <id> <kind> <reason> | recover-cutover-media <id> media-writer-stopped-and-result-inspected <reason>"
    exit 2
    ;;
esac
