#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
set +xv
set -euo pipefail
export LC_ALL=C
alert_runbooks=https://github.com/dannyota/aboutme/blob/main/docs/runbooks
alert_limit=1500

alert_html() {
  alert_escaped=${1//&/"&amp;"}
  alert_escaped=${alert_escaped//</"&lt;"}
  alert_escaped=${alert_escaped//>/"&gt;"}
}

alert_count() {
  if [[ $1 == 1 ]]; then alert_counted="1 $2"; else alert_counted="$1 $3"; fi
}

alert_units() {
  local count=$((10#$1)) list='' candidate name more listed=0
  local -a names
  mapfile -t names <<<"$2"
  alert_count "$count" unit units
  before="$alert_counted failed: "
  for name in "${names[@]}"; do
    [[ -n $name && ! $name =~ ^'omitted units: '[0-9]+$ ]] || continue
    ((listed < count)) || break
    candidate=${list:+$list, }$name
    more=.
    ((count - listed - 1 == 0)) || more=" and $((count - listed - 1)) more."
    alert_html "$candidate"
    ((${#before} + ${#alert_escaped} + ${#more} <= 300)) || break
    list=$candidate
    listed=$((listed + 1))
  done
  field=$list after=.
  ((count - listed == 0)) || after=" and $((count - listed)) more."
  ((listed > 0)) || before="$alert_counted failed." after=
}

alert_content() {
  local key=$1 subject=$2 detail=$3
  mark='🔴' before='' field=$subject after='' job=aboutme-watch anchor=production.md#host-watcher watcher=yes
  case $key in
    disk-?*)
      mark='🟠' title='disk almost full'
      third='Action: grow the volume with vngcloud volume resize-volume, then growpart and resize2fs.'
      if [[ $subject =~ ^(/[[:graph:]]{0,100})' is '([0-9]{1,3})'% full'$ ]]; then
        field=${BASH_REMATCH[1]} after=" is $((10#${BASH_REMATCH[2]}))% full."
      elif [[ $subject =~ ^(/[[:graph:]]{0,100})' is ?% full'$ ]]; then
        before='Disk use of ' field=${BASH_REMATCH[1]} after=' cannot be read.'
      fi ;;
    failed-units)
      title='units failed' third="Action: read each failed unit's journal on the host."
      if [[ $subject =~ ^'failed units: '([0-9]{1,6})$ ]]; then alert_units "${BASH_REMATCH[1]}" "$detail"; fi ;;
    postgres-down)
      title='PostgreSQL down' before='PostgreSQL does not answer on /run/postgresql.' field=
      third='Action: check the PostgreSQL service and its journal on the host.' ;;
    archive-failing)
      title='WAL archiving failing' before='WAL archiving to the backup bucket is failing.' field=
      third='Action: check pgBackRest archiving and the backup bucket from the host.'
      anchor=production.md#backups-and-restore ;;
    backup-missing)
      title='backup missing' before='No pgBackRest backup can be read.' field=
      third='Action: check pgBackRest and the backup bucket from the host.'
      anchor=production.md#backups-and-restore ;;
    backup-stale)
      mark='🟠' title='backup stale' anchor=production.md#backups-and-restore
      third='Action: check the backup timers and the last pgBackRest run on the host.'
      if [[ $subject =~ ^'the newest backup is '([0-9]{1,9})' hours old'$ ]]; then
        alert_count "$((10#${BASH_REMATCH[1]}))" hour hours
        before="The newest backup is $alert_counted old." field=
      fi ;;
    crowdsec-bans)
      mark='🟠' title='many CrowdSec alerts'
      third='Action: review the CrowdSec alerts on the host for an attack or false positives.'
      if [[ $subject =~ ^'CrowdSec raised '([0-9]{1,9})' alerts in the last hour'$ ]]; then
        alert_count "$((10#${BASH_REMATCH[1]}))" alert alerts
        before="CrowdSec raised $alert_counted in the last hour." field=
      fi ;;
    coraza-log-invalid)
      mark='🟠' title='Coraza log invalid' anchor=production.md#coraza-logs
      third='Action: read the Coraza metadata on the host.'
      if [[ $subject == 'the Coraza match log is malformed' ]]; then
        before='The Coraza match log is malformed.' field=
      elif [[ $subject =~ ^'the Coraza match log has '([0-9]{1,9})' unknown records in the last hour'$ ]]; then
        alert_count "$((10#${BASH_REMATCH[1]}))" 'unknown record' 'unknown records'
        before="The Coraza match log has $alert_counted in the last hour." field=
      fi ;;
    totp-unavailable)
      title='TOTP unavailable' third='Action: read the totp_unavailable reasons in the aboutme-server journal.'
      if [[ $subject =~ ^'the server logged totp_unavailable '([0-9]{1,9})' times'$ ]]; then
        alert_count "$((10#${BASH_REMATCH[1]}))" time times
        before="The server logged totp_unavailable $alert_counted." field=
      fi ;;
    unit-aboutme-crowdsec-retention)
      watcher=no job=aboutme-crowdsec-retention anchor=crowdsec-postgresql.md#failure-recovery
      title='CrowdSec retention failed' before='CrowdSec retention failed, so the host stops Caddy and CrowdSec.' field=
      third='Action: follow the CrowdSec failure recovery before reopening the edge.' ;;
    unit-crowdsec_retention_overdue)
      watcher=no mark='🟠' job=aboutme-crowdsec-retention anchor=crowdsec-postgresql.md#retention-and-maintenance
      title='CrowdSec retention overdue' field=
      before='A CrowdSec record passed its retention deadline before deletion.'
      third='System: this retention pass deletes it.' ;;
    crowdsec-vacuum)
      watcher=no job=aboutme-crowdsec-vacuum anchor=crowdsec-postgresql.md#retention-and-maintenance
      title='CrowdSec vacuum failed' before='The hourly CrowdSec vacuum failed.' field=
      third='System: the next hourly vacuum tries again.' ;;
    unit-?*)
      watcher=no job=${key#unit-} anchor=production.md#unit-failures
      title='unit failed' before='Unit ' field=${key#unit-} after=' failed.'
      third="Action: read the unit's journal on the host." ;;
    *)
      watcher=no job=aboutme-host anchor=production.md#alarms
      title='host alert' third='Action: read the journal on the host.' ;;
  esac
}

alert_compose() {
  alert_html "$job"
  printf -v text '%s <b>aboutme</b> · %s\n%s%s%s\n%s\n<i>%s ICT · %s UTC · %s · <a href="%s/%s">runbook</a></i>' \
    "$mark" "$title" "$before" "$1" "$after" "$third" "$ict" "$utc" "$alert_escaped" "$alert_runbooks" "$anchor"
}

alert_fit() {
  local escaped excess marker='…' cut_entity='&[a-z]{0,3}$'
  alert_html "$field"
  escaped=$alert_escaped
  alert_compose "$escaped"
  excess=$((${#text} - alert_limit))
  ((excess > 0)) || return 0
  excess=$((excess + ${#marker}))
  ((excess <= ${#escaped})) || return 1
  escaped=${escaped:0:${#escaped}-excess}
  [[ ! $escaped =~ $cut_entity ]] || escaped=${escaped%&*}
  alert_compose "$escaped$marker"
}

[[ ${BASH_SOURCE[0]} == "$0" ]] || return 0
umask 077
exec {alert_journal_fd}>&2
exec 2>/dev/null
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
if [[ -f $here/telegram-state.sh ]]; then
  # shellcheck source=../telegram-state.sh
  source "$here/telegram-state.sh"
else
  # shellcheck source=../telegram-state.sh
  source "$here/../telegram-state.sh"
fi

key=
outcome() {
  printf 'alert-telegram: %s%s\n' "$1" "${key:+ $key}" >&"$alert_journal_fd"
}
fail() { outcome "$1"; exit 1; }
test_mode=no
if [[ $# == 1 && $1 == --test ]]; then
  test_mode=yes
  lock=exclusive
else
  (($# == 2)) || fail invalid_input
  [[ $1 =~ ^[A-Za-z0-9@._-]{1,120}$ && $2 =~ ^[[:print:]]{1,200}$ ]] || fail invalid_input
  key=$1 subject=$2
  lock=shared
fi
telegram_init "$lock" 2>/dev/null || fail invalid_secret
[[ $test_mode != yes ]] || rm -f -- "$telegram_proof"
unset telegram_token telegram_chat
telegram_credentials 2>/dev/null || fail invalid_secret
identity=$(telegram_identity) || fail invalid_secret
if [[ $test_mode != yes ]]; then
  telegram_private_file "$telegram_active" 600 && [[ $(<"$telegram_active") == telegram ]] || fail invalid_secret
  telegram_private_file "$telegram_approved" 400 && [[ $(<"$telegram_approved") == "$identity" ]] || fail invalid_secret
fi

endpoint=https://api.telegram.org
protocol='=https'
if [[ -n ${ABOUTME_ALERT_TEST_ENDPOINT:-} ]]; then
  [[ -n ${ABOUTME_ALERT_TEST_ROOT:-} && $ABOUTME_ALERT_TEST_ENDPOINT =~ ^http://127\.0\.0\.1:(20[0-9]{3}|21000)$ ]] || fail invalid_input
  endpoint=$ABOUTME_ALERT_TEST_ENDPOINT
  protocol='=http'
fi

owns_stamp=no
cleanup() {
  [[ $owns_stamp != yes ]] || rm -f -- "$stamp"
}
trap cleanup EXIT
trap 'outcome transport_failed; exit 1' HUP INT TERM
now=$(date -u +%s) && [[ $now =~ ^[0-9]{1,12}$ ]] || fail invalid_input
printf -v utc '%02d:%02d' $((now / 3600 % 24)) $((now / 60 % 60))
printf -v ict '%02d:%02d' $(((now / 3600 + 7) % 24)) $((now / 60 % 60))
day_format='^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
if [[ $test_mode == yes ]]; then
  mark='🟢' title='alert delivery test' before='This message tests host alert delivery.' field='' after=''
  third='System: no action needed.' job=alert-telegram anchor=production.md#host-telegram-setup-and-rotation
  alert_fit || fail invalid_input
else
  detail=$(head -c 4096 | tr -cd '\40-\176\n' || true)
  alert_content "$key" "$subject" "$detail"
  [[ ! -L $telegram_stamps ]] || fail invalid_input
  install -d -m 0700 "$telegram_stamps" 2>/dev/null || fail invalid_input
  [[ $(stat -c '%u:%a' -- "$telegram_stamps") == "$EUID:700" ]] || fail invalid_input
  today=$(date -u -d "@$now" +%F) && [[ $today =~ $day_format ]] || fail invalid_input
  if [[ $watcher == yes ]]; then
    previous=$(date -u -d "@$((now - 86400))" +%F) && [[ $previous =~ $day_format ]] || fail invalid_input
    if telegram_private_file "$telegram_stamps/$key.$previous" 600 && [[ ! -s $telegram_stamps/$key.$previous ]]; then
      mark='🟠' title+=", still failing since the alert on $previous UTC"
    fi
  fi
  alert_fit || fail invalid_input
  stamp=$telegram_stamps/$key.$today
  if [[ -e $stamp || -L $stamp ]] || ! (set -o noclobber; : >"$stamp") 2>/dev/null; then
    telegram_private_file "$stamp" 600 && [[ ! -s $stamp ]] || fail stamp_failed
    outcome suppressed
    exit 0
  fi
  owns_stamp=yes
fi

curl_config() {
  local escaped json
  export -n escaped json
  escaped=${text//\\/\\\\}
  escaped=${escaped//\"/\\\"}
  escaped=${escaped//$'\n'/\\n}
  printf -v json '{"chat_id":"%s","text":"%s","parse_mode":"HTML","link_preview_options":{"is_disabled":true}}' \
    "$telegram_chat" "$escaped"
  escaped=${json//\\/\\\\}
  escaped=${escaped//\"/\\\"}
  printf 'url = "%s/bot%s/sendMessage"\n' "$endpoint" "$telegram_token"
  printf 'data = "%s"\n' "$escaped"
}

send() (
  set +e
  exec {reply_status_fd}>&1
  curl_config | curl -q --silent --connect-timeout 5 --max-time 15 \
    --proto "$protocol" --proto-redir '=https' --max-redirs 0 --retry 0 \
    --proxy '' --noproxy '*' --request POST --header 'Content-Type: application/json' \
    --config - --write-out 'http:%{http_code}\nupload:%{size_upload}\nrequest:%{size_request}\n' --output >(
      {
        head -c 16384
        extra=$(head -c 1 | wc -c)
        if ((extra != 0)); then printf 'oversized\n' >&"$reply_status_fd"; fi
      } | jq -Rrs '
        if length == 0 then "body:empty"
        else try (fromjson | if type == "object" and .ok == true then "body:sent"
                            else "body:invalid_response" end) catch "body:invalid_response" end' 2>/dev/null
    ) 2>/dev/null
  results=("${PIPESTATUS[@]}")
  printf 'curl:%s:%s\n' "${results[0]}" "${results[1]}"
)
for attempt in 1 2 3; do
  result=$(send)
  result=$'\n'$result$'\n'
  [[ $result != *$'\noversized\n'* ]] || fail invalid_response
  if [[ $result == *$'\ncurl:0:0\n'* ]]; then
    [[ $result == *$'\nhttp:200\n'* ]] || fail rejected
    [[ $result == *$'\nbody:sent\n'* ]] || fail invalid_response
    break
  fi
  [[ $key == unit-* && $attempt != 3 && $result == *$'\nhttp:000\n'* &&
    $result == *$'\nupload:0\n'* && $result == *$'\nrequest:0\n'* &&
    $result == *$'\nbody:empty\n'* &&
    ( $result == *$'\ncurl:0:6\n'* || $result == *$'\ncurl:0:7\n'* ||
      $result == *$'\ncurl:0:28\n'* ) ]] || fail transport_failed
  sleep 10 || fail transport_failed
done
if [[ $test_mode == yes ]]; then
  proof=$(mktemp "$telegram_base/.telegram-proof.XXXXXX")
  printf '%s\n' "$identity" >"$proof"
  chmod 0400 "$proof"
  mv -fT -- "$proof" "$telegram_proof"
fi
owns_stamp=no
outcome sent
