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
#   fence.sh raise <id> <tag>          raise the minimum to <tag>
#   fence.sh release <id>              release the lock this id owns
#   fence.sh check <release> <unit>    ExecStartPre: refuse a release below
#                                      the minimum
#   fence.sh clear <reason>            privileged manual clear of a stale lock
#
# There is no time-based takeover: a crash leaves the lock closed until a
# manual clear. A missing or malformed file fails every command but init.
set -euo pipefail

dir=${FENCE_DIR:-/var/lib/aboutme}
file=$dir/fence.json
lockfile=$dir/fence.lock
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
    deploy | rollback | activate | totp_reencrypt) ;;
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

# Releasing an already released lock succeeds, so a retried release after a
# lost SSH reply settles the same way the first one did.
cmd_release() { # id
  load
  if [[ -z $owner ]]; then
    return 0
  fi
  [[ $owner == "$1" ]] || die "the lock belongs to another operation; the manual clear owns it"
  jq 'del(.operation_id, .operation_kind, .operation_started_at, .operation_checked_at)' <<<"$doc" | store
}

# ExecStartPre for every unit that runs the server or web image. Takes a
# shared lock so it never reads a half-replaced file. A refusal fails the
# unit, whose OnFailure= mails the support mailbox (host/bin/alert.sh).
cmd_check() { # release unit
  local release=$1 unit=$2
  [[ $release =~ ^[0-9]+$ ]] || die "release '$release' is not a number"
  load
  if ((release < minimum)); then
    echo "aboutme-fence-refused unit=$unit release=$release minimum=$minimum"
    exit 1
  fi
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
  load
  [[ -n $owner ]] || {
    say "no lock to clear"
    return 0
  }
  logger -t aboutme-fence "manual clear of $(jq -c '{operation_kind, operation_started_at, operation_checked_at}' <<<"$doc") by ${SUDO_USER:-root}: $reason"
  jq 'del(.operation_id, .operation_kind, .operation_started_at, .operation_checked_at)' <<<"$doc" | store
  say "cleared"
}

[[ $(id -u) == 0 ]] || die "run as root on the host"
umask 022
mkdir -p "$dir"

case "$#:${1-}" in
  1:init) with_lock -x cmd_init ;;
  1:read) with_lock -s cmd_read ;;
  3:lock) with_lock -x cmd_lock "$2" "$3" ;;
  3:checkpoint) with_lock -x cmd_checkpoint "$2" "$3" ;;
  3:raise) with_lock -x cmd_raise "$2" "$3" ;;
  2:release) with_lock -x cmd_release "$2" ;;
  3:check) with_lock -s cmd_check "$2" "$3" ;;
  2:clear) with_lock -x cmd_clear "$2" ;;
  *)
    say "usage: fence.sh init | read | lock <kind> <tag> | checkpoint <id> <tag> | raise <id> <tag> | release <id> | check <release> <unit> | clear <reason>"
    exit 2
    ;;
esac
