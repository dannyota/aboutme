#!/usr/bin/env bash
# Installs and configures everything the production host needs besides the
# containers (docs/design/vietnam-production.md, "Host", "PostgreSQL", "Edge",
# "Scheduled jobs", "Logs, metrics, and alarms"). Runs as root on the host.
# Usage: install.sh <bundle-dir>, where bundle-dir is a copy of deploy/vn.
# Every step is safe to re-run. It never prints a secret and never reads the
# contents of /etc/aboutme/secrets.
set -euo pipefail

# The PostgreSQL minor is pinned (design "PostgreSQL"). Empty installs the
# repository's current 18.x and prints it; after the first install the
# operator sets this to that version, for example "18.1-1.pgdg24.04+1".
PG_VERSION=""

# Fingerprint of the PGDG archive signing key (ACCC4CF8).
PGDG_KEY_FINGERPRINT=B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8
PGDG_KEY_URL=https://www.postgresql.org/media/keys/ACCC4CF8.asc

# Placeholder: refuses until filled. Read the fingerprint from
# https://docs.crowdsec.net/u/getting_started/installation/linux
CROWDSEC_KEY_FINGERPRINT=""
CROWDSEC_KEY_URL=https://packagecloud.io/crowdsec/crowdsec/gpgkey

FILEBEAT_VERSION=8.7.1
FILEBEAT_URL=https://artifacts.elastic.co/downloads/beats/filebeat/filebeat-8.7.1-amd64.deb
# Placeholder: refuses until filled with the SHA-512 Elastic publishes for the
# .deb at the URL above plus ".sha512".
FILEBEAT_SHA512=""

die() {
  echo "install: $*" >&2
  exit 1
}
say() { echo "install: $*"; }

[ "$(id -u)" -eq 0 ] || die "run as root"
[ $# -eq 1 ] || die "usage: install.sh <bundle-dir>"
bundle=$(readlink -f "$1")
[ -d "$bundle/host/etc" ] || die "$bundle/host/etc not found"
mountpoint -q /srv/data || die "/srv/data is not a mountpoint; run host/data-volume.sh first"

export DEBIAN_FRONTEND=noninteractive
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

timers=(
  aboutme-backup-full.timer
  aboutme-backup-diff.timer
  aboutme-backup-verify.timer
  aboutme-backup-age.timer
)

# Fetch an armored key and check that its fingerprint equals $2.
fetch_key() { # url fingerprint outfile
  local fprs
  curl -fsSL --proto '=https' "$1" -o "$tmp/key.asc"
  fprs=$(gpg --show-keys --with-colons "$tmp/key.asc" | awk -F: '$1 == "fpr" {print $10}')
  if ! grep -qxF "$2" <<<"$fprs"; then
    die "key from $1 does not carry fingerprint $2"
  fi
  cp "$tmp/key.asc" "$3"
}

# copy_tree <src> <dest-root>: install every file under src into dest-root,
# files 0644 and directories 0755, skipping *.example and the files named in
# the remaining arguments (paths relative to src).
copy_tree() {
  local src=$1 dest=$2 f rel skip s
  shift 2
  while IFS= read -r -d '' f; do
    rel=${f#"$src"/}
    skip=no
    for s in "$@"; do [ "$rel" = "$s" ] && skip=yes; done
    [ "$skip" = no ] || continue
    install -d -m 0755 "$(dirname "$dest/$rel")"
    install -m 0644 "$f" "$dest/$rel"
  done < <(find "$src" -type f ! -name '*.example' -print0 | sort -z)
}

# 1. Keys and apt repositories.
install -d -m 0755 /etc/apt/keyrings
apt-get update -qq
apt-get install -y -qq ca-certificates curl gnupg >/dev/null

fetch_key "$PGDG_KEY_URL" "$PGDG_KEY_FINGERPRINT" /etc/apt/keyrings/postgresql.asc
echo "deb [signed-by=/etc/apt/keyrings/postgresql.asc] https://apt.postgresql.org/pub/repos/apt noble-pgdg main" \
  >/etc/apt/sources.list.d/pgdg.list
say "PGDG repository in place"

[ -n "$CROWDSEC_KEY_FINGERPRINT" ] ||
  die "CROWDSEC_KEY_FINGERPRINT is empty; read it from https://docs.crowdsec.net/u/getting_started/installation/linux"
fetch_key "$CROWDSEC_KEY_URL" "$CROWDSEC_KEY_FINGERPRINT" "$tmp/crowdsec.asc"
gpg --dearmor --yes -o /etc/apt/keyrings/crowdsec.gpg "$tmp/crowdsec.asc"
echo "deb [signed-by=/etc/apt/keyrings/crowdsec.gpg] https://packagecloud.io/crowdsec/crowdsec/ubuntu noble main" \
  >/etc/apt/sources.list.d/crowdsec.list
say "CrowdSec repository in place"
apt-get update -qq

# 2. CrowdSec config goes in before the package starts the service, so its
# local API never tries 127.0.0.1:8080, which Go owns.
install -d -m 0755 /etc/crowdsec/acquis.d
install -m 0644 "$bundle/host/etc/crowdsec/config.yaml.local" /etc/crowdsec/config.yaml.local
install -m 0644 "$bundle/host/etc/crowdsec/acquis.d/caddy.yaml" /etc/crowdsec/acquis.d/caddy.yaml
install -m 0644 "$bundle/host/etc/crowdsec/acquis.d/sshd.yaml" /etc/crowdsec/acquis.d/sshd.yaml

# 3. Packages. postgresql-common creates the postgres user; the data
# directories on the volume are handed to it before postgresql-18 creates the
# cluster there.
apt-get install -y -qq postgresql-common >/dev/null
for d in /srv/data/postgresql /srv/data/pgbackrest-conf; do
  chown postgres:postgres "$d"
  chmod 0700 "$d"
done
pg_pkg=postgresql-18
[ -z "$PG_VERSION" ] || pg_pkg="postgresql-18=$PG_VERSION"
apt-get install -y -qq "$pg_pkg" pgbackrest >/dev/null
say "installed $(dpkg-query -W -f='${Package} ${Version}' postgresql-18)"
[ -n "$PG_VERSION" ] || say "PG_VERSION is empty: pin the version printed above in this script"
apt-get install -y -qq crowdsec crowdsec-firewall-bouncer-nftables >/dev/null

have_fb=$(dpkg-query -W -f='${Version}' filebeat 2>/dev/null || true)
if [ "$have_fb" = "$FILEBEAT_VERSION" ]; then
  say "filebeat $FILEBEAT_VERSION already installed"
else
  [ -n "$FILEBEAT_SHA512" ] || die "FILEBEAT_SHA512 is empty; fill it before installing filebeat"
  curl -fsSL --proto '=https' "$FILEBEAT_URL" -o "$tmp/filebeat.deb"
  echo "$FILEBEAT_SHA512  $tmp/filebeat.deb" | sha512sum -c --quiet - ||
    die "filebeat .deb does not match FILEBEAT_SHA512"
  dpkg -i "$tmp/filebeat.deb" >/dev/null
  say "installed filebeat $FILEBEAT_VERSION"
fi

# 4. Configuration files. PostgreSQL files belong to postgres; the firewall
# bouncer file is built in step 5.
etc=$bundle/host/etc
copy_tree "$etc" /etc \
  crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local \
  postgresql/18/main/pg_hba.conf \
  postgresql/18/main/conf.d/90-aboutme.conf
install -o postgres -g postgres -m 0640 "$etc/postgresql/18/main/pg_hba.conf" /etc/postgresql/18/main/pg_hba.conf
install -d -o postgres -g postgres -m 0755 /etc/postgresql/18/main/conf.d
install -o postgres -g postgres -m 0644 "$etc/postgresql/18/main/conf.d/90-aboutme.conf" \
  /etc/postgresql/18/main/conf.d/90-aboutme.conf
say "configuration copied to /etc"

# 5. CrowdSec: point the agent at the local API, register the firewall
# bouncer once, and keep its key on the data volume. The key is never printed.
# /etc/crowdsec is itself bound from the data volume (data-volume.sh), so the
# agent's credentials and this bouncer file never touch the root disk.
sed -i 's|^url: .*|url: http://127.0.0.1:8095|' /etc/crowdsec/local_api_credentials.yaml
systemctl restart crowdsec
keyfile=/srv/data/crowdsec/firewall-bouncer.key
bouncer=aboutme-firewall
if [ ! -s "$keyfile" ]; then
  cscli bouncers delete "$bouncer" >/dev/null 2>&1 || true
  (umask 077 && cscli bouncers add "$bouncer" -o raw >"$keyfile")
  say "registered CrowdSec bouncer $bouncer"
fi
conf=/etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local
(
  umask 077
  cat "$etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local" >"$conf"
  printf 'api_key: %s\n' "$(cat "$keyfile")" >>"$conf"
)
systemctl enable crowdsec crowdsec-firewall-bouncer >/dev/null
systemctl restart crowdsec-firewall-bouncer

# 6. Host scripts, quadlet templates, and environment files.
lib=/usr/local/lib/aboutme
install -d -m 0755 "$lib" /etc/aboutme
copy_script() {
  if [ -f "$1" ]; then
    install -m 0755 "$1" "$lib/$(basename "$1")"
  else
    say "skipping missing $1"
  fi
}
copy_script "$bundle/scripts/fence.sh"
copy_script "$bundle/scripts/deploy-host.sh"
copy_script "$bundle/host/job-run.sh"
for f in "$bundle"/host/bin/*.sh; do
  [ -e "$f" ] && copy_script "$f"
done
if [ -d "$bundle/host/quadlet" ]; then
  install -d -m 0755 "$lib/quadlet"
  for f in "$bundle"/host/quadlet/*; do
    [ -f "$f" ] && install -m 0644 "$f" "$lib/quadlet/$(basename "$f")"
  done
else
  say "skipping missing $bundle/host/quadlet"
fi
for f in "$bundle"/host/env/*.env; do
  [ -f "$f" ] && install -m 0644 "$f" "/etc/aboutme/$(basename "$f")"
done
for f in "$bundle"/host/env/*.env.example; do
  [ -f "$f" ] || continue
  target=/etc/aboutme/$(basename "$f" .example)
  if [ -e "$target" ]; then
    say "kept existing $target"
  else
    install -m 0644 "$f" "$target"
  fi
done
if [ -x "$lib/fence.sh" ]; then
  "$lib/fence.sh" init
else
  say "fence.sh not installed; skipping fence init"
fi

# 7. Apply settings and start units. The job timers start with the first
# deploy (deploy-host.sh start-timers), since no release exists before it.
systemd-tmpfiles --create /etc/tmpfiles.d/aboutme.conf
sysctl --system >/dev/null
systemctl restart systemd-journald
systemctl daemon-reload
systemctl start systemd-zram-setup@zram0.service
systemctl restart postgresql@18-main.service
systemctl enable --now "${timers[@]}" >/dev/null
systemctl enable --now unattended-upgrades.service >/dev/null

# Filebeat needs the vMonitor client files and keystore values from the
# operator; start it only when the key file is present.
if [ -e /etc/aboutme/secrets/vmonitor/user.key.pem ]; then
  systemctl enable --now filebeat >/dev/null
else
  say "vmonitor client files absent: filebeat not started"
fi
say "done"
