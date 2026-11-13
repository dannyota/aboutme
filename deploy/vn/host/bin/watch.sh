#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
set -euo pipefail
export LC_ALL=C

if [[ -n ${ABOUTME_WATCH_TEST_LIB:-} ]]; then
  ((EUID != 0)) || { echo "watch: test library is forbidden for root" >&2; exit 1; }
  lib=$ABOUTME_WATCH_TEST_LIB
  transport_file=$lib/alert-transport
else
  lib=/usr/local/lib/aboutme
  transport_file=/etc/aboutme/alert-transport
fi
sender=$lib/alert.sh
if [[ -e $transport_file || -L $transport_file ]]; then
  [[ -f $transport_file && ! -L $transport_file &&
    $(stat -c '%u:%a' "$transport_file") == "$EUID:600" &&
    $(<"$transport_file") == telegram ]] || exit 1
  sender=$lib/alert-telegram.sh
fi
disk_percent=70
backup_hours=26
crowdsec_bans_per_hour=${CROWDSEC_BANS_PER_HOUR:-50}

alert() {
  printf '%s\n' "${3-}" | "$sender" "$1" "$2" || true
}

watch_crowdsec() (
  # Match retention's lock order. Unrelated host checks hold neither lock.
  [[ -f /run/aboutme/crowdsec-retention.lock && ! -L /run/aboutme/crowdsec-retention.lock ]] || return 1
  exec {watch_retention_lock}</run/aboutme/crowdsec-retention.lock
  local admitted=yes
  flock -s -n "$watch_retention_lock" || admitted=no
  [[ -f /var/lib/aboutme/fence.lock && ! -L /var/lib/aboutme/fence.lock ]] || return 1
  exec {crowdsec_db_lock_fd}</var/lib/aboutme/fence.lock
  flock -s -w 2 "$crowdsec_db_lock_fd" || return 1
  local result=0 count
  count=$(timeout --kill-after=1s 20 "$BASH" "$0" --crowdsec-query "$crowdsec_db_lock_fd" "$admitted") || result=$?
  case $result in
    0) printf '%s\n' "$count" ;;
    3 | 124 | 137)
      "$lib/crowdsec-retention-fail.sh" || true
      echo 0 ;;
    *) return 1 ;;
  esac
)

watch_crowdsec_query() {
  trap '' TERM
  # The parent owns the fence and, when admitted, retention. Validate its fence
  # descriptor before the bounded admission query.
  [[ $1 =~ ^[0-9]+$ ]] || return 1
  # Nested preflight timeouts stay in this bounded child's process group, so
  # forced termination also closes inherited fence and retention descriptors.
  timeout() { command timeout --foreground --kill-after=1s "$@"; }
  crowdsec_db_lock_fd=$1
  # shellcheck source=../crowdsec-db.sh
  source "$lib/crowdsec-db.sh"
  crowdsec_db_held_lock || return 1
  # shellcheck source=../crowdsec-retention-lease-check.sh
  source "$lib/crowdsec-retention-lease-check.sh"
  crowdsec_lease_check || return 3
  if [[ $2 != yes ]]; then echo 0; return 0; fi
  if crowdsec_db_ready; then
    timeout --kill-after=1s 5 cscli alerts list --since 1h -o json 2>/dev/null |
      jq 'if type == "array" then length else 0 end' 2>/dev/null || echo 0
  else
    echo 0
  fi
}

if [[ $# == 3 && $1 == --crowdsec-query ]]; then watch_crowdsec_query "$2" "$3"; exit; fi
(($# == 0)) || exit 2

bans=$(watch_crowdsec) || exit 1

for pair in root:/ data:/srv/data; do
  name=${pair%%:*} mount=${pair#*:}
  used=$(df --output=pcent "$mount" | tail -n1 | tr -dc 0-9)
  if [[ -z $used ]] || ((used > disk_percent)); then
    alert "disk-$name" "$mount is ${used:-?}% full" "grow it: vngcloud volume resize-volume, then growpart and resize2fs"
  fi
done

failed=$(systemctl list-units --state=failed --no-legend --plain | awk '{ print $1 }' | sort |
  awk '
    NF { count++
      if (kept < 20 && length($0) <= 80 && $0 ~ /^[A-Za-z0-9@._:\\-]+$/) {
        names = names $0 "\n"; kept++
      }
    }
    END { if (count) printf "%d\n%somitted units: %d\n", count, names, count-kept }')
if [[ -n $failed ]]; then
  alert failed-units "failed units: ${failed%%$'\n'*}" "${failed#*$'\n'}"
fi

if ! runuser -u postgres -- pg_isready -q -h /run/postgresql; then
  alert postgres-down "PostgreSQL does not answer on /run/postgresql"
else
  archive_failing=$(runuser -u postgres -- psql -X -q -At -h /run/postgresql -d postgres -c \
    "SELECT coalesce(last_failed_time > coalesce(last_archived_time, 'epoch'), false) FROM pg_stat_archiver" || echo t)
  [[ $archive_failing == f ]] || alert archive-failing "WAL archiving to the backup bucket is failing"
fi

newest=$(runuser -u postgres -- pgbackrest --stanza=aboutme info --output=json 2>/dev/null |
  jq -r '[.[0].backup[]?.timestamp.stop | numbers] | max // empty' 2>/dev/null || true)
if [[ -z $newest ]]; then
  alert backup-missing "no pgBackRest backup can be read"
elif ((($(date +%s) - newest) / 3600 >= backup_hours)); then
  alert backup-stale "the newest backup is $((($(date +%s) - newest) / 3600)) hours old"
fi

((bans <= crowdsec_bans_per_hour)) || alert crowdsec-bans "CrowdSec raised $bans alerts in the last hour"

mapfile -t waf_logs < <(
  for stream in serving-match maintenance-match; do
    find /run/aboutme/caddy-log/waf -maxdepth 1 -type f \
      \( -name "$stream.log" -o -name "$stream-*.log*" \) \
      -printf '%T@ %p\n' 2>/dev/null | sort -rn | head -n 3 | cut -d' ' -f2-
  done
)
matches=0
unparsed=0
diagnostics=0
if ((${#waf_logs[@]})); then
  since=$(($(date +%s) - 3600))
  counts=$(zcat -f -- "${waf_logs[@]}" 2>/dev/null |
    jq -r --argjson since "$since" '
      if type != "object" or (.ts | type != "number") then "unknown"
      elif .ts < $since then empty
      elif .msg == "waf_engine_diagnostic" then "diagnostic"
      elif .msg == "waf_rule_match" then "match"
      elif .msg == "waf_rule_match_unparsed" then "unparsed"
      else "unknown" end' |
    awk '{ count[$1]++ } END { print count["match"] + 0, count["unparsed"] + 0, count["diagnostic"] + 0, count["unknown"] + 0 }') || {
    alert coraza-log-invalid "the Coraza match log is malformed"
    counts='0 0 0 0'
  }
  read -r matches unparsed diagnostics unknown <<<"$counts"
  ((unknown == 0)) || alert coraza-log-invalid "the Coraza match log has $unknown unknown records in the last hour"
fi
printf 'coraza counts in the last hour: matches=%s unparsed=%s diagnostics=%s\n' "$matches" "$unparsed" "$diagnostics"

totp=$(journalctl -u aboutme-server --since -6min -o cat --no-pager 2>/dev/null | grep -c -F totp_unavailable || true)
((${totp:-0} == 0)) || alert totp-unavailable "the server logged totp_unavailable $totp times"
