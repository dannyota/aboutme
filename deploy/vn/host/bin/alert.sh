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

secrets=/etc/aboutme/secrets
stamps=/var/lib/aboutme/alerts

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
    | if test("^[A-Z][A-Z0-9_]*=[A-Za-z0-9_.,:/@+-]*$") then capture("^(?<key>[^=]+)=(?<value>.*)$")
      else error("malformed line") end]
  | from_entries
  | if (.SMTP_HOST | test("^[a-z0-9.-]+$")) and (.SMTP_PORT | test("^[0-9]{2,5}$"))
      and (.SMTP_TLS == "implicit" or .SMTP_TLS == "starttls")
      and (.SMTP_FROM_ADDRESS | test("^[^@]+@[a-z0-9.-]+$"))
      and (.ALERT_TO | test("^[^@,]+@[a-z0-9.-]+$"))
    then . else error("incomplete") end' </etc/aboutme/smtp.env 2>/dev/null) ||
  die "/etc/aboutme/smtp.env is malformed or incomplete"
get() { jq -r --arg k "$1" '.[$k]' <<<"$smtp"; }
user=$(<"$secrets/smtp-username") || die "the SMTP username is missing"
[[ $user =~ ^[[:graph:]]{1,200}$ ]] || die "the SMTP username is malformed"
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
from $(get SMTP_FROM_ADDRESS)
user $user
passwordeval cat $secrets/smtp-password
account default : aboutme
CONF
{
  printf 'From: aboutme production <%s>\n' "$(get SMTP_FROM_ADDRESS)"
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
