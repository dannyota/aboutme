#!/usr/bin/env bash
# The five-minute host watch (docs/design/vietnam-production.md, "Logs,
# metrics, and alarms"). Runs as root from aboutme-watch.service and calls
# alert.sh for each condition that holds; alert.sh sends at most one message
# per condition per day. Every check runs even when an earlier one fires.
set -euo pipefail

lib=/usr/local/lib/aboutme
disk_percent=70
backup_hours=26
crowdsec_bans_per_hour=${CROWDSEC_BANS_PER_HOUR:-50}
coraza_matches_per_hour=${CORAZA_MATCHES_PER_HOUR:-50}

alert() { # key subject [detail]
  printf '%s\n' "${3-}" | "$lib/alert.sh" "$1" "$2" || true
}

# Root and data volume use.
for pair in root:/ data:/srv/data; do
  name=${pair%%:*} mount=${pair#*:}
  used=$(df --output=pcent "$mount" | tail -n1 | tr -dc 0-9)
  if [[ -z $used ]] || ((used > disk_percent)); then
    alert "disk-$name" "$mount is ${used:-?}% full" "grow it: vngcloud volume resize-volume, then growpart and resize2fs"
  fi
done

# Failed units: names only.
failed=$(systemctl list-units --state=failed --no-legend --plain | awk '{ print $1 }' | sort | tr '\n' ' ')
if [[ -n $failed ]]; then
  alert failed-units "failed units: $failed"
fi

# PostgreSQL over its socket.
if ! runuser -u postgres -- pg_isready -q -h /run/postgresql; then
  alert postgres-down "PostgreSQL does not answer on /run/postgresql"
else
  # WAL archiving: the last attempt failed after the last success.
  archive_failing=$(runuser -u postgres -- psql -X -q -At -h /run/postgresql -d postgres -c \
    "SELECT coalesce(last_failed_time > coalesce(last_archived_time, 'epoch'), false) FROM pg_stat_archiver" || echo t)
  [[ $archive_failing == f ]] || alert archive-failing "WAL archiving to the backup bucket is failing"
fi

# Newest backup age.
newest=$(runuser -u postgres -- pgbackrest --stanza=aboutme info --output=json 2>/dev/null |
  jq -r '[.[0].backup[]?.timestamp.stop | numbers] | max // empty' 2>/dev/null || true)
if [[ -z $newest ]]; then
  alert backup-missing "no pgBackRest backup can be read"
elif ((($(date +%s) - newest) / 3600 >= backup_hours)); then
  alert backup-stale "the newest backup is $((($(date +%s) - newest) / 3600)) hours old"
fi

# CrowdSec decisions in the last hour.
bans=$(cscli alerts list --since 1h -o json 2>/dev/null | jq 'if type == "array" then length else 0 end' 2>/dev/null || echo 0)
((bans <= crowdsec_bans_per_hour)) || alert crowdsec-bans "CrowdSec raised $bans alerts in the last hour"

# Coraza writes metadata only. Each Caddy process owns one current file and at
# most two rolls. Filter by record time because a low-volume current file can
# live longer than the one-hour window.
mapfile -t waf_logs < <(
  for stream in serving-match maintenance-match; do
    find /run/aboutme/caddy-log/waf -maxdepth 1 -type f \
      \( -name "$stream.log" -o -name "$stream-*.log*" \) \
      -printf '%T@ %p\n' 2>/dev/null | sort -rn | head -n 3 | cut -d' ' -f2-
  done
)
matches=0
unparsed=0
if ((${#waf_logs[@]})); then
  since=$(($(date +%s) - 3600))
  counts=$(zcat -f -- "${waf_logs[@]}" 2>/dev/null |
    jq -r --argjson since "$since" '
      if type != "object" or (.ts | type != "number") then "unknown"
      elif .ts < $since then empty
      elif .msg == "waf_rule_match" then "match"
      elif .msg == "waf_rule_match_unparsed" then "unparsed"
      else "unknown" end' |
    awk '{ count[$1]++ } END { print count["match"] + 0, count["unparsed"] + 0, count["unknown"] + 0 }') || {
    alert coraza-log-invalid "the Coraza match log is malformed"
    counts='0 0 0'
  }
  read -r matches unparsed unknown <<<"$counts"
  ((unknown == 0)) || alert coraza-log-invalid "the Coraza match log has $unknown unknown records in the last hour"
fi
((matches <= coraza_matches_per_hour)) || alert coraza-matches "Coraza matched $matches requests in the last hour"
((unparsed == 0)) || alert coraza-unparsed "Coraza wrote $unparsed unparsed match records in the last hour"

# totp_unavailable in the server journal since the last run.
totp=$(journalctl -u aboutme-server --since -6min -o cat --no-pager 2>/dev/null | grep -c -F totp_unavailable || true)
((${totp:-0} == 0)) || alert totp-unavailable "the server logged totp_unavailable $totp times"
