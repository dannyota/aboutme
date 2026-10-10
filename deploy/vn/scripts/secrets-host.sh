#!/usr/bin/env bash
# The host half of secrets.sh (docs/design/vietnam-production.md, "Secrets and
# host identity"). Runs as root on the production host, installed at
# /usr/local/lib/aboutme/secrets-host.sh. Each secret is one root-owned 0400
# file under /etc/aboutme/secrets, on the encrypted data volume, and a Podman
# secret of the same name. Nothing here prints, logs, or echoes a value, and
# nothing overwrites a value except an explicit --replace on a name that moves
# from AWS at cutover or rotates.
#
#   secrets-host.sh generate                generate every missing value
#   secrets-host.sh import <name> [--replace]  value on stdin
#   secrets-host.sh import-s3 <media|backups> [--replace]
#                                           AWS-format key file on stdin
#   secrets-host.sh podman-sync [--replace <name>]
#   secrets-host.sh bouncer-register        register the Caddy CrowdSec bouncer
#   secrets-host.sh list                    names present, never values
#   secrets-host.sh age-copy <recipient>    age ciphertext of every value on stdout
set -euo pipefail

dir=/etc/aboutme/secrets
pgb_conf=/etc/pgbackrest/conf.d/secrets.conf

# Values this host creates with openssl.
generated=(db-admin-password db-migrator-password db-app-password
  auth-email-active-key-id auth-email-active-key password-rate-hmac-key
  view-pass-key totp-key-a pgbackrest-cipher-pass)
# Values the owner pipes in.
imported=(google-client-id google-client-secret linkedin-client-id linkedin-client-secret
  smtp-username smtp-password totp-key-b)
# Values that move from AWS once at cutover and replace the rehearsal ones,
# or rotate later (docs/design/vietnam-production.md, "Secrets and host
# identity"): only these accept --replace.
replaceable=(auth-email-active-key-id auth-email-active-key password-rate-hmac-key
  view-pass-key totp-key-a totp-key-b google-client-id google-client-secret
  linkedin-client-id linkedin-client-secret smtp-username smtp-password)
# Values the containers read as Podman secrets.
podman_names=(db-admin-password db-migrator-password db-app-password
  auth-email-active-key-id auth-email-active-key password-rate-hmac-key
  view-pass-key totp-key-a totp-key-b google-client-id google-client-secret
  linkedin-client-id linkedin-client-secret media-access-key-id
  media-secret-access-key smtp-username smtp-password crowdsec-bouncer-key)

say() { printf 'secrets: %s\n' "$*" >&2; }
die() {
  say "$*"
  exit 1
}
member() { # word list...
  local w=$1 x
  shift
  for x in "$@"; do [[ $x == "$w" ]] && return 0; done
  return 1
}

# Every write goes to a fresh mktemp file inside a directory only root can
# write, which root just created and so may chmod, then replaces the target
# with one rename (mv -T never follows a link at the target). No path is
# checked and then written in a separate step under a directory another user
# controls.
require_root_dir() { # path mode
  [[ -d $1 && ! -L $1 ]] || die "$1 is not a directory"
  [[ $(stat -c '%u %a' -- "$1") == "0 $2" ]] || die "$1 must be owned by root with mode $2"
}

# Writes stdin to one secret file, so a reader never sees a partial value.
store() { # name replace(0|1)
  local name=$1 replace=$2 tmp size
  if [[ -e $dir/$name || -L $dir/$name ]] && ((!replace)); then
    say "kept $name"
    cat >/dev/null
    return 0
  fi
  tmp=$(mktemp "$dir/.new.XXXXXX")
  head -c 65537 >"$tmp"
  size=$(stat -c %s -- "$tmp")
  if ((size == 0 || size > 65536)); then
    rm -f -- "$tmp"
    die "$name must be 1 to 65536 bytes"
  fi
  chmod 0400 -- "$tmp"
  mv -fT -- "$tmp" "$dir/$name"
  say "stored $name"
}

hex48() { openssl rand -hex 24 | tr -d '\n'; }
key32() { openssl rand 32 | basenc --base64url | tr -d '=\n'; }

cmd_generate() {
  local name
  for name in "${generated[@]}"; do
    [[ -e $dir/$name ]] && {
      say "kept $name"
      continue
    }
    case $name in
      auth-email-active-key-id) printf k1 | store "$name" 0 ;;
      *-password | pgbackrest-cipher-pass) hex48 | store "$name" 0 ;;
      *) key32 | store "$name" 0 ;;
    esac
  done
  write_pgbackrest_conf
}

# pgBackRest reads its repository key and cipher pass from a file on the data
# volume (host/etc/pgbackrest/secrets.conf.example). Its directory is owned by
# root, group postgres, mode 0750, so postgres can read the file but cannot
# place anything where root writes.
write_pgbackrest_conf() {
  local f
  for f in backup-access-key-id backup-secret-access-key pgbackrest-cipher-pass; do
    [[ -s $dir/$f ]] || {
      say "pgBackRest secrets wait for $f"
      return 0
    }
  done
  local tmp
  require_root_dir "$(dirname "$pgb_conf")" 750
  tmp=$(mktemp "$(dirname "$pgb_conf")/.new.XXXXXX")
  {
    printf '[global]\n'
    printf 'repo1-s3-key=%s\n' "$(cat "$dir/backup-access-key-id")"
    printf 'repo1-s3-key-secret=%s\n' "$(cat "$dir/backup-secret-access-key")"
    printf 'repo1-cipher-pass=%s\n' "$(cat "$dir/pgbackrest-cipher-pass")"
  } >"$tmp"
  chgrp postgres -- "$tmp"
  chmod 0440 -- "$tmp"
  mv -fT -- "$tmp" "$pgb_conf"
  say "wrote $pgb_conf"
}

cmd_import() { # name [--replace]
  local name=$1 replace=0
  [[ ${2-} == --replace ]] && replace=1
  member "$name" "${imported[@]}" "${replaceable[@]}" || die "$name is not an importable name"
  ((!replace)) || member "$name" "${replaceable[@]}" || die "$name cannot be replaced"
  store "$name" "$replace"
  ((!replace)) || cmd_podman_sync --replace "$name"
}

# The key file create-s3-key writes, in the AWS shared credentials format: one
# [default] section with exactly the two keys. One awk program is the only
# parser, and it refuses any other shape.
cmd_import_s3() { # media|backups [--replace]
  local which=$1 replace=0 parsed id secret prefix
  [[ ${2-} == --replace ]] && replace=1
  case $which in
    media) prefix=media ;;
    backups) prefix=backup ;;
    *) die "import-s3 takes media or backups" ;;
  esac
  if ((!replace)) && [[ -e $dir/$prefix-access-key-id || -e $dir/$prefix-secret-access-key ||
    -L $dir/$prefix-access-key-id || -L $dir/$prefix-secret-access-key ]]; then
    die "$which key exists; use --replace to rotate it"
  fi
  parsed=$(head -c 4096 | awk '
    /^[[:space:]]*$/ { next }
    NR_SEEN == 0 && $0 == "[default]" { NR_SEEN = 1; next }
    NR_SEEN == 1 && $0 ~ /^aws_access_key_id[[:space:]]*=[[:space:]]*[A-Za-z0-9]+$/ && id == "" {
      sub(/^aws_access_key_id[[:space:]]*=[[:space:]]*/, ""); id = $0; next }
    NR_SEEN == 1 && $0 ~ /^aws_secret_access_key[[:space:]]*=[[:space:]]*[A-Za-z0-9\/+=]+$/ && secret == "" {
      sub(/^aws_secret_access_key[[:space:]]*=[[:space:]]*/, ""); secret = $0; next }
    { bad = 1 }
    END { if (bad || id == "" || secret == "") exit 1; print id; print secret }') ||
    die "the $which key file is not one [default] section with the two keys"
  id=$(sed -n 1p <<<"$parsed")
  secret=$(sed -n 2p <<<"$parsed")
  printf '%s' "$id" | store "$prefix-access-key-id" "$replace"
  printf '%s' "$secret" | store "$prefix-secret-access-key" "$replace"
  if [[ $which == backups ]]; then
    write_pgbackrest_conf
  elif ((replace)); then
    cmd_podman_sync --replace media-access-key-id
    cmd_podman_sync --replace media-secret-access-key
  fi
}

cmd_podman_sync() { # [--replace name]
  local name
  if [[ ${1-} == --replace ]]; then
    name=$2
    podman secret rm "$name" >/dev/null 2>&1 || true
    podman secret create "$name" "$dir/$name" >/dev/null
    say "replaced Podman secret $name"
    return 0
  fi
  for name in "${podman_names[@]}"; do
    [[ -s $dir/$name ]] || continue
    if podman secret exists "$name"; then
      continue
    fi
    podman secret create "$name" "$dir/$name" >/dev/null
    say "created Podman secret $name"
  done
}

# bouncer_key_valid succeeds when the local API accepts the local key. The key
# goes to curl on stdin (printf is a builtin), never in argv.
bouncer_key_valid() {
  local code
  code=$(printf 'header = "X-Api-Key: %s"\n' "$(cat "$dir/crowdsec-bouncer-key")" |
    curl -s -o /dev/null -w '%{http_code}' --max-time 10 -K - \
      'http://127.0.0.1:8095/v1/decisions?ip=127.0.0.1') || true
  [[ $code == 200 ]]
}

# cscli generates the key directly into a private file. No value reaches argv.
cmd_bouncer_register() {
  local tmp
  if cscli bouncers list -o json | jq -e '[.[]? | select(.name == "aboutme-caddy")] | length == 1' >/dev/null; then
    [[ -s $dir/crowdsec-bouncer-key ]] || die "the registered bouncer has no local key"
    # A registration interrupted before the key file moved leaves an older
    # key here; refuse it rather than sync a key the local API rejects.
    bouncer_key_valid ||
      die "the local key does not match the registered aboutme-caddy bouncer; run 'cscli bouncers delete aboutme-caddy', then register again"
    cmd_podman_sync --replace crowdsec-bouncer-key
    say "kept the aboutme-caddy bouncer"
    return 0
  fi
  tmp=$(mktemp "$dir/.bouncer.XXXXXX")
  chmod 0600 "$tmp"
  if ! cscli bouncers add aboutme-caddy -o raw >"$tmp" 2>/dev/null; then
    rm -f -- "$tmp"
    die "could not register the aboutme-caddy bouncer"
  fi
  [[ -s $tmp ]] || { rm -f -- "$tmp"; die "cscli returned an empty bouncer key"; }
  chmod 0400 "$tmp"
  mv -fT -- "$tmp" "$dir/crowdsec-bouncer-key"
  cmd_podman_sync --replace crowdsec-bouncer-key
  say "registered the aboutme-caddy bouncer"
}

cmd_list() {
  find "$dir" -maxdepth 2 -type f ! -name '.*' -printf '%P\n' | sort
}

# The age copy for aboutme-infra: every value and the pgBackRest file, as one
# tar encrypted on this host, so only ciphertext leaves it.
cmd_age_copy() { # recipient
  [[ $1 =~ ^age1[0-9a-z]{58}$ ]] || die "'$1' is not an age X25519 recipient"
  command -v age >/dev/null || die "age is not installed"
  tar -C / -cf - "${dir#/}" "${pgb_conf#/}" 2>/dev/null | age -r "$1"
}

[[ $(id -u) == 0 ]] || die "run as root on the host"
umask 077
mountpoint -q /srv/data || die "/srv/data is not mounted; the secrets would land on the root disk"
require_root_dir "$dir" 700

case "${1-}:$#" in
  generate:1) cmd_generate ;;
  import:2 | import:3) cmd_import "$2" "${3-}" ;;
  import-s3:2 | import-s3:3) cmd_import_s3 "$2" "${3-}" ;;
  podman-sync:1) cmd_podman_sync ;;
  bouncer-register:1) cmd_bouncer_register ;;
  list:1) cmd_list ;;
  age-copy:2) cmd_age_copy "$2" ;;
  *)
    say "usage: secrets-host.sh generate | import <name> [--replace] | import-s3 <media|backups> [--replace] | podman-sync | bouncer-register | list | age-copy <recipient>"
    exit 2
    ;;
esac
