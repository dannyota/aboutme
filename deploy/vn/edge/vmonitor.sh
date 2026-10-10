#!/usr/bin/env bash
# Creates the vMonitor log project, the alarm channel, the app-down check, and
# the log alarms (docs/design/vietnam-production.md, "Logs, metrics, and
# alarms"). Each object is looked up by its exact name first, so a re-run
# creates nothing that exists.
#
#   vmonitor.sh quote
#   vmonitor.sh apply --max-price <vnd> --alarm-email <address>
#                     [--site-origin https://aboutme.vn] [--include-placeholders]
#
# quote is read-only. apply quotes the log project and refuses above
# --max-price before it orders anything. The CLI has no quote for synthetic
# checks or alarms, so those are not priced here (design Q7).
#
# Unconfirmed, from the vngcloud wiki and SDK source (monitor/logalarms_body.go),
# not from a live alarm:
#   - A log alarm query is a phrase match over the log text. The wiki documents
#     no boolean or field syntax, so each alarm matches one fixed phrase.
#   - The console's own filter shape is sent with the query, because the CLI
#     needs both together.
#   - The allowed --time-frame values and whether a hyphenated phrase matches as
#     written.
#   - Whether a phrase alarm counts each match as one event.
#   - The lowercase keys of an assertion inside --cli-input-json (type,
#     operator, target), taken from the SDK's Assertion struct.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=../scripts/vng-lib.sh
. "$here/../scripts/vng-lib.sh"

LOG_PROJECT=aboutme-prod
LOG_CLASS=Pro
LOG_RETENTION_DAYS=30
# The Pro class needs at least 5 GB a day at 30 days of retention.
LOG_GB_PER_DAY=${LOG_GB_PER_DAY:-5}
CHANNEL=aboutme-support
LOCATIONS=(SYNTT-VN-HCM01 SYNTT-VN-HAN01)

# The CrowdSec ban and Coraza block alarms need the field names the Caddy image
# will log. Until then these are placeholders that match nothing real, and the
# alarms are only created with --include-placeholders.
CROWDSEC_BAN_QUERY=${CROWDSEC_BAN_QUERY:-PLACEHOLDER-crowdsec-ban}
CORAZA_BLOCK_QUERY=${CORAZA_BLOCK_QUERY:-PLACEHOLDER-coraza-block}

usage() {
  cat >&2 <<'USAGE'
usage: vmonitor.sh quote
       vmonitor.sh apply --max-price <vnd> --alarm-email <address>
                         [--site-origin <https://origin>] [--include-placeholders]
USAGE
  exit 2
}

quote_project() {
  vng_ro monitor quote-create-log-project --name "$LOG_PROJECT" \
    --class "$LOG_CLASS" --retention-days "$LOG_RETENTION_DAYS" \
    --gb-per-day "$LOG_GB_PER_DAY" | jq -r '.OptimumPrice // empty'
}

cmd=${1:-}
[[ -n $cmd ]] || usage
shift

max_price=
email=
origin=https://aboutme.vn
placeholders=0
case $cmd in
  quote) (($# == 0)) || usage ;;
  apply)
    while (($#)); do
      case $1 in
        --max-price)
          (($# >= 2)) || usage
          max_price=$2
          shift 2
          ;;
        --alarm-email)
          (($# >= 2)) || usage
          email=$2
          shift 2
          ;;
        --site-origin)
          (($# >= 2)) || usage
          origin=$2
          shift 2
          ;;
        --include-placeholders)
          placeholders=1
          shift
          ;;
        *) usage ;;
      esac
    done
    [[ $max_price =~ ^[0-9]+$ ]] || usage
    [[ $email =~ ^[^@[:space:]]+@[^@[:space:]]+$ ]] || usage
    [[ $origin =~ ^https://[^/[:space:]]+$ ]] || usage
    ;;
  *) usage ;;
esac

need_tools

if [[ $cmd == quote ]]; then
  price=$(quote_project)
  require_price "log project $LOG_PROJECT ($LOG_CLASS, ${LOG_RETENTION_DAYS} days, ${LOG_GB_PER_DAY} GB a day)" "$price" "$price"
  say "synthetic checks and log alarms: the CLI has no quote command for them, so they are not priced here (design Q7)"
  exit 0
fi

# ensure_log_project prints the log project ID.
ensure_log_project() {
  local id price
  id=$(vng monitor list-log-projects --search "$LOG_PROJECT" | json_id_by_name "$LOG_PROJECT")
  if [[ -z $id ]]; then
    price=$(quote_project)
    require_price "log project $LOG_PROJECT" "$price" "$max_price" >&2
    vng monitor create-log-project --name "$LOG_PROJECT" --class "$LOG_CLASS" \
      --retention-days "$LOG_RETENTION_DAYS" --gb-per-day "$LOG_GB_PER_DAY" \
      --max-price "$max_price" >/dev/null
    id=$(vng monitor list-log-projects --search "$LOG_PROJECT" | json_id_by_name "$LOG_PROJECT")
    say "log project $LOG_PROJECT: ordered" >&2
  else
    say "log project $LOG_PROJECT: exists" >&2
  fi
  [[ -n $id ]] || die "log project $LOG_PROJECT not found after the order"
  say "$id"
}

# ensure_channel prints the channel ID. A new Email channel needs a code that
# vMonitor mails to the address. Emails beyond the free 20 spend a paid package.
ensure_channel() {
  local id ref code
  id=$(vng monitor list-channels --type Email | json_id_by_name "$CHANNEL")
  if [[ -z $id ]]; then
    [[ -t 0 ]] || die "channel $CHANNEL needs a mailed code; run this from a terminal"
    ref=$(vng monitor send-channel-otp --type Email --address "$email" | jq -r '.Ref // empty')
    [[ -n $ref ]] || die "send-channel-otp returned no ref"
    read -r -s -p "Code mailed to $email: " code
    printf '\n' >&2
    vng monitor create-channel --name "$CHANNEL" --type Email \
      --address "$email" --otp-ref "$ref" --otp "$code" >/dev/null
    id=$(vng monitor list-channels --type Email | json_id_by_name "$CHANNEL")
    say "channel $CHANNEL: created" >&2
  else
    say "channel $CHANNEL: exists" >&2
  fi
  [[ -n $id ]] || die "channel $CHANNEL not found after create"
  say "$id"
}

# location_id <name> prints a monitoring location's ID.
location_id() {
  local id
  id=$(vng monitor list-locations | json_id_by_name "$1")
  [[ -n $id ]] || die "monitor location $1 not found"
  say "$id"
}

# ensure_check <channel-id> creates the app-down check on <origin>/readyz from
# both Vietnamese locations. The assertion is the console default (fail on a 4xx
# or 5xx response, type status_code, operator does_not_match_regex, target
# [4-5][0-9][0-9]); the wiki documents no exact-200 assertion. The check fails
# only when both locations fail, which is the default for two locations.
ensure_check() {
  local channel_id=$1 ids=() loc json
  if [[ -n $(check_id_by_name "$APP_CHECK_NAME") ]]; then
    say "check $APP_CHECK_NAME: exists"
    return 0
  fi
  for loc in "${LOCATIONS[@]}"; do ids+=("$(location_id "$loc")"); done
  json=$(jq -n -c --arg c "$channel_id" --args '
    {Locations: $ARGS.positional,
     Assertions: [{type: "status_code", operator: "does_not_match_regex",
                   target: "[4-5][0-9][0-9]"}],
     Notifications: {"In-alarm": [$c], "Up": [$c], "Undetermined": []}}' "${ids[@]}")
  vng monitor create-check --name "$APP_CHECK_NAME" --url "$origin/readyz" \
    --method GET --cli-input-json "$json" >/dev/null
  say "check $APP_CHECK_NAME: created"
}

# ensure_alarm <name> <phrase> <threshold> <minutes> <severity> creates a log
# alarm that notifies the channel when more than <threshold> log lines in the
# last <minutes> contain <phrase>.
ensure_alarm() {
  local name=$1 phrase=$2 threshold=$3 minutes=$4 severity=$5 filter
  if vng monitor list-alarms --kind Log --name "$name" |
    jq -e --arg n "$name" '[.Items[]? | select(.Name == $n)] | length > 0' >/dev/null; then
    say "alarm $name: exists"
    return 0
  fi
  # The console's phrase filter, from monitor/logalarms_body.go in the SDK.
  filter=$(jq -n -c --arg q "$phrase" '
    {Filter: {type: "bool", value: {filter: [], should: [], mustNot: [],
      must: [{type: "bool", value: {filter: [], should: [], mustNot: [],
        must: [{type: "multi_match",
                value: {type: "phrase", query: $q, lenient: true}}]}}]}}}')
  vng monitor create-log-alarm --name "$name" \
    --log-project-id "$log_project_id" --query-string "$phrase" \
    --threshold-type frequency --condition gt --threshold-value "$threshold" \
    --time-frame "$minutes" --severity "$severity" \
    --in-alarm "$channel_id" --cli-input-json "$filter" >/dev/null
  say "alarm $name: created"
}

log_project_id=$(ensure_log_project)
channel_id=$(ensure_channel)
ensure_check "$channel_id"

# Fixed markers from the host scripts (docs/design/vietnam-production.md,
# "Scheduled jobs" and "Release fence"). One match is enough to alarm.
ensure_alarm aboutme-job-failed "aboutme-job-failed" 0 5 HIGH
ensure_alarm aboutme-backup-failed "aboutme-backup-failed" 0 5 HIGH
# The age check runs hourly, so the window covers one full run.
ensure_alarm aboutme-backup-stale "aboutme-backup-stale" 0 60 HIGH
ensure_alarm aboutme-fence-refused "aboutme-fence-refused" 0 5 HIGH
ensure_alarm aboutme-totp-unavailable "totp_unavailable" 0 5 HIGH
# systemd's wording when a unit ends in failure.
ensure_alarm aboutme-unit-failed "Failed with result" 0 5 HIGH

if ((placeholders)); then
  ensure_alarm aboutme-crowdsec-ban-rate "$CROWDSEC_BAN_QUERY" 50 5 MEDIUM
  ensure_alarm aboutme-coraza-block-rate "$CORAZA_BLOCK_QUERY" 50 5 MEDIUM
else
  say "alarms aboutme-crowdsec-ban-rate and aboutme-coraza-block-rate: skipped; their queries are placeholders until the Caddy image's log fields land (use --include-placeholders after setting CROWDSEC_BAN_QUERY and CORAZA_BLOCK_QUERY)"
fi
