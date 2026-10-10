#!/usr/bin/env bash
# Mails one alert to the support mailbox through Bizfly SMTP with msmtp
# (docs/design/vietnam-production.md, "Logs, metrics, and alarms"). Runs as
# root, from aboutme-alert@.service (every unit's OnFailure=) and watch.sh.
#
#   alert.sh <key> <subject>     optional plain-text detail on stdin
#
# One message per key per day: the first call of a day creates a stamp file
# with noclobber, so a repeat that day sends nothing. A failed send removes the
# stamp, so the next call tries again. The mail leaves the country through
# the support mailbox, so it carries unit names, counts, and percentages only,
# never log lines or personal data.
set -euo pipefail

if [[ -n ${ABOUTME_ALERT_TEST_ROOT:-} ]]; then
  ((EUID != 0)) || {
    echo "alert: test root is forbidden for root" >&2
    exit 1
  }
  secrets=$ABOUTME_ALERT_TEST_ROOT/secrets
  stamps=$ABOUTME_ALERT_TEST_ROOT/alerts
  smtp_env=$ABOUTME_ALERT_TEST_ROOT/smtp.env
else
  secrets=/etc/aboutme/secrets
  stamps=/var/lib/aboutme/alerts
  smtp_env=/etc/aboutme/smtp.env
fi

die() {
  printf 'alert: %s\n' "$*" >&2
  exit 1
}

(($# == 2)) || die "usage: alert.sh <key> <subject>"
key=$1 subject=$2
[[ $key =~ ^[A-Za-z0-9@._-]{1,120}$ ]] || die "malformed key"
[[ $subject =~ ^[[:print:]]{1,200}$ ]] || die "malformed subject"
detail=$(head -c 4096 | tr -cd '[:print:]\n' || true)

# smtp.env as JSON; the same strict line shape as deploy-host.sh reads
# flags.env with, so msmtp gets exactly what the server gets.
smtp=$(jq -Rn '
  [inputs | select(test("^[[:space:]]*(#|$)") | not)
    | if test("^[A-Z][A-Z0-9_]*=") then capture("^(?<key>[^=]+)=(?<value>.*)$")
      else error("malformed line") end
    | if .key == "SES_FROM_NAME" then
        if (.value | length) <= 64
          and (.value | explode | all(.[]; . >= 32 and (. < 127 or . > 159)))
        then . else error("malformed sender name") end
      elif (.value | test("^[A-Za-z0-9_.,:/@+-]*$")) then .
      else error("malformed value") end]
  | if (map(.key) | length) == (map(.key) | unique | length) then from_entries
    else error("repeated name") end
  | if (.SMTP_HOST | test("^[a-z0-9.-]+$")) and (.SMTP_PORT | test("^[0-9]{2,5}$"))
      and (.SMTP_TLS == "implicit" or .SMTP_TLS == "starttls")
      and (.SES_FROM_ADDRESS | test("^[^@]+@[a-z0-9.-]+$"))
      and (.SES_FROM_NAME | type == "string")
      and (.ALERT_TO | test("^[^@,]+@[a-z0-9.-]+$"))
    then . else error("incomplete") end' <"$smtp_env" 2>/dev/null) ||
  die "$smtp_env is malformed or incomplete"
get() { jq -r --arg k "$1" '.[$k]' <<<"$smtp"; }
user_file=$secrets/smtp-username
[[ -f $user_file && ! -L $user_file ]] || die "the SMTP username is missing"
user_size=$(stat -c %s -- "$user_file") || die "the SMTP username is missing"
printable_size=$(LC_ALL=C tr -cd '[:print:]' <"$user_file" | wc -c)
((user_size >= 1 && user_size <= 256 && printable_size == user_size)) ||
  die "the SMTP username must be 1 to 256 printable ASCII bytes with no newline"
# Command substitution is safe after the byte check rejects every newline.
# msmtp's quoted config argument preserves spaces; escape its two delimiters.
user=$(<"$user_file")
quoted_user=${user//\\/\\\\}
quoted_user=${quoted_user//\"/\\\"}
[[ -s $secrets/smtp-password ]] || die "the SMTP password is missing"

umask 077
install -d -m 0700 "$stamps"
stamp=$stamps/$key.$(date -u +%F)
if ! (set -o noclobber && : >"$stamp") 2>/dev/null; then
  exit 0
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
starttls=off
[[ $(get SMTP_TLS) == starttls ]] && starttls=on
cat >"$work/msmtprc" <<CONF
defaults
auth plain
tls on
tls_starttls $starttls
tls_trust_file /etc/ssl/certs/ca-certificates.crt
logfile -
account aboutme
host $(get SMTP_HOST)
port $(get SMTP_PORT)
from $(get SES_FROM_ADDRESS)
user "$quoted_user"
passwordeval cat $secrets/smtp-password
account default : aboutme
CONF
{
  printf 'From: %s <%s>\n' "$(get SES_FROM_NAME)" "$(get SES_FROM_ADDRESS)"
  printf 'To: %s\n' "$(get ALERT_TO)"
  printf 'Subject: [aboutme-prod] %s\n' "$subject"
  printf 'Date: %s\n' "$(date -R)"
  printf 'Content-Type: text/plain; charset=utf-8\n\n'
  printf 'Host: %s\nKey: %s\nTime (UTC): %s\n\n' "$(hostname)" "$key" "$(date -u +%FT%TZ)"
  [[ -z $detail ]] || printf '%s\n\n' "$detail"
  printf 'Read the details on the host with journalctl.\n'
} >"$work/message"
if ! msmtp -C "$work/msmtprc" -t <"$work/message" >/dev/null 2>"$work/err"; then
  rm -f "$stamp"
  die "msmtp could not send the alert for $key"
fi
