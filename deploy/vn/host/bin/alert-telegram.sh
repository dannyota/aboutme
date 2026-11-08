#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
# Sends bounded host metadata. See docs/design/telegram-host-alerts.md.
set +xv
set -euo pipefail
export LC_ALL=C
umask 077
# Provider and utility diagnostics must never reach the journal.
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
if [[ $test_mode == yes ]]; then
  text='[aboutme-prod] host alert delivery test'
else
  # Filtering precedes shell capture so NUL and control bytes cannot leak into
  # shell warnings. Read only the bounded prefix, even if stdin never closes.
  detail=$(head -c 4096 | tr -cd '\40-\176\n' || true)
  truncation=$'\n[detail truncated]'
  if ((${#detail} > 2048)); then detail=${detail:0:$((2048-${#truncation}))}$truncation; fi
  printf -v text '[aboutme-prod]\nKey: %s\nTime (UTC): %s\nSubject: %s\n\n%s\n\n' \
    "$key" "$(date -u +%FT%TZ)" "$subject" "$detail"
  if [[ $key == coraza-log-invalid ]]; then
    text+='Read Coraza metadata on the host in /run/aboutme/caddy-log/waf (tmpfs), including current and rotated serving-match and maintenance-match logs.'$'\n'
    text+='Summarize timestamps, levels, message kinds, and rule IDs only. Do not copy raw request data, full URLs, messages, or credentials into alerts.'$'\n'
    text+='Check the WAF log format and reader. Match and unparsed counts are available with journalctl -u aboutme-watch; they do not trigger alerts.'
  else
    text+='Read the details on the host with journalctl.'
  fi
  ((${#text} <= 3000)) || fail invalid_input
  [[ ! -L $telegram_stamps ]] || fail invalid_input
  install -d -m 0700 "$telegram_stamps" 2>/dev/null || fail invalid_input
  [[ $(stat -c '%u:%a' -- "$telegram_stamps") == "$EUID:700" ]] || fail invalid_input
  stamp=$telegram_stamps/$key.$(date -u +%F)
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
  printf -v json '{"chat_id":"%s","text":"%s"}' "$telegram_chat" "$escaped"
  escaped=${json//\\/\\\\}
  escaped=${escaped//\"/\\\"}
  # Both printf calls are builtins. Secrets travel only through this pipe.
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
        # Count, but never retain, one extra byte. This also detects oversized
        # streams without Content-Length, independently of curl's version.
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
  # Unit-failure alerts can wait independently; other callers get one attempt.
  # Any request bytes or response make delivery uncertain, including partial success.
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
