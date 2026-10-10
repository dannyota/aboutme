#!/usr/bin/env bash
# Creates and moves production secrets on the Vietnam host
# (docs/design/vietnam-production.md, "Secrets and host identity"). Runs on
# the laptop and calls secrets-host.sh on the host over SSH. Values are made
# on the host with openssl or piped from the laptop's standard input straight
# into SSH; nothing is printed, written to the laptop's disk, or passed as a
# command-line argument on the laptop.
#
#   secrets.sh generate                  generate missing values, sync Podman
#                                        secrets, register the Caddy bouncer
#   secrets.sh import <name> [--replace] value on stdin, for example
#                                        `secrets.sh import smtp-password < /dev/tty`
#   secrets.sh import-s3 <media|backups> move buckets.sh's key file, then
#                                        delete the laptop copy
#   secrets.sh list                      names on the host, never values
#   secrets.sh age-copy <recipient> <out>  ciphertext for aboutme-infra
#   secrets.sh move-from-ssm             cutover only, run by the owner
#
# Environment: DEPLOY_HOST (the host address, required).
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=ssh-lib.sh
source "$here/ssh-lib.sh"

admin=aboutme-admin
host=${DEPLOY_HOST:?set DEPLOY_HOST to the production host address}
lib=/usr/local/lib/aboutme

say() { printf 'secrets: %s\n' "$*" >&2; }
die() {
  say "$*"
  exit 1
}
usage() {
  sed -n '9,18p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 2
}

# Runs secrets-host.sh as root with this process's stdin. Every argument is a
# fixed word, a validated name, or an age recipient.
on_host() {
  local cmd
  printf -v cmd '%q ' "$lib/secrets-host.sh" "$@"
  ssh -p "$SSH_PORT" -o BatchMode=yes "$admin@$host" "sudo $cmd"
}

name_ok() { [[ $1 =~ ^[a-z0-9-]+$ ]] || die "malformed secret name '$1'"; }

case "${1-}:$#" in
  generate:1)
    on_host generate </dev/null
    on_host podman-sync </dev/null
    on_host bouncer-register </dev/null
    ;;
  import:2 | import:3)
    name_ok "$2"
    [[ $# == 2 || $3 == --replace ]] || usage
    [[ ! -t 0 ]] || say "reading $2 from the terminal; end with Ctrl-D (the value is not echoed by this script)"
    on_host import "$2" ${3:+"$3"}
    on_host podman-sync </dev/null
    ;;
  import-s3:2)
    [[ $2 == media || $2 == backups ]] || usage
    file=${XDG_RUNTIME_DIR:?XDG_RUNTIME_DIR must point at a per-user tmpfs}/aboutme-vn/aboutme-$2-s3-credentials
    [[ -f $file && ! -L $file ]] || die "$file is missing; run buckets.sh first"
    on_host import-s3 "$2" <"$file"
    on_host podman-sync </dev/null
    rm -f -- "$file"
    say "moved the $2 key to the host and deleted the laptop copy"
    ;;
  list:1)
    on_host list </dev/null
    ;;
  age-copy:3)
    [[ ! -e $3 ]] || die "$3 exists; refusing to overwrite it"
    (
      umask 077
      on_host age-copy "$2" </dev/null >"$3.tmp"
    ) && mv "$3.tmp" "$3" || {
      rm -f "$3.tmp"
      die "the age copy failed"
    }
    say "wrote the age ciphertext to $3"
    ;;
  move-from-ssm:1)
    # Cutover only, run by the owner with AWS credentials: each value goes
    # from SSM straight into the SSH pipe and replaces the rehearsal value
    # (docs/design/vietnam-production.md, "Secrets and host identity").
    # These values rotate once AWS real data is deleted.
    declare -A from=(
      [auth-email-active-key-id]=/aboutme/prod/auth-email/active-key-id
      [auth-email-active-key]=/aboutme/prod/auth-email/active-key
      [password-rate-hmac-key]=/aboutme/prod/password-rate-hmac-key
      [view-pass-key]=/aboutme/prod/view-pass-key
      [totp-key-a]=/aboutme/prod/totp/key-a
      [totp-key-b]=/aboutme/prod/totp/key-b
      [google-client-id]=/aboutme/prod/oauth/google-client-id
      [google-client-secret]=/aboutme/prod/oauth/google-client-secret
    )
    for name in "${!from[@]}"; do
      if ! aws ssm describe-parameters --region ap-southeast-1 \
        --parameter-filters "Key=Name,Values=${from[$name]}" \
        --query 'length(Parameters)' --output text | grep -qx 1; then
        say "skipped $name: ${from[$name]} does not exist"
        continue
      fi
      aws ssm get-parameter --region ap-southeast-1 --name "${from[$name]}" --with-decryption \
        --query Parameter.Value --output text | tr -d '\n' | on_host import "$name" --replace
    done
    ;;
  *) usage ;;
esac
