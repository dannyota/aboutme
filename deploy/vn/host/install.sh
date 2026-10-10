#!/usr/bin/env bash
# Installs and configures everything the production host needs besides the
# containers (docs/design/vietnam-production.md, "Host", "PostgreSQL", "Edge",
# "Scheduled jobs", "Logs, metrics, and alarms"). Runs as root on the host.
# Usage: install.sh <bundle-dir>, where bundle-dir is a copy of deploy/vn.
# Every step is safe to re-run. It never prints a secret and never reads the
# contents of /etc/aboutme/secrets.
set -euo pipefail

# Every package from outside the Ubuntu archive is pinned to one version and
# verified against material written in this repository, never against a key
# or checksum fetched from the same source at install time:
# - apt repositories: the signing keys are committed under host/keys and must
#   carry the fingerprints below; apt then verifies every package against
#   them, and dpkg must report exactly the pinned version afterward.
# - Filebeat: the .deb must match the SHA-512 below.
# PostgreSQL stays on one minor version (docs/design/vietnam-production.md,
# "PostgreSQL"); unattended-upgrades does not touch these packages. Raising a
# version is a reviewed change to this file.
PGDG_SIGNER_FPR=B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8
# CrowdSec publishes no fingerprint in its documentation; this is the primary
# key in host/keys/crowdsec.asc as served by packagecloud.io on 2026-10-10.
CROWDSEC_SIGNER_FPR=6A89E3C2303A901A889971D3376ED5326E93CD0C

POSTGRESQL_COMMON_VERSION=293.pgdg24.04+1
POSTGRESQL_VERSION=18.6-1.pgdg24.04+2
PGBACKREST_VERSION=2.59.3-1.pgdg24.04+1
CROWDSEC_VERSION=1.8.1
CROWDSEC_BOUNCER_VERSION=0.0.36

FILEBEAT_VERSION=8.7.1
FILEBEAT_URL=https://artifacts.elastic.co/downloads/beats/filebeat/filebeat-8.7.1-amd64.deb
# Checked on 2026-10-10 against Elastic's detached signature by key
# 46095ACC8548582C1A2699A9D27D666CD88E42B4.
FILEBEAT_SHA512=722951ae4c91893a67ff5f66613082087be3f44e3fc450378994b565e829c35c1bf04949e9625880607f4adfe01a53a26a822d168f9dc05fe7d92c9fbc7ec70e

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

# Installs a committed armored key after checking that it holds exactly one
# primary key with the pinned fingerprint. gpg's machine-readable output is
# the only thing parsed.
install_key() { # committed-key fingerprint outfile
  local fprs
  fprs=$(gpg --show-keys --with-colons "$1" | awk -F: '$1 == "pub" { want = 1; next } want && $1 == "fpr" { print $10; want = 0 }')
  [ "$fprs" = "$2" ] || die "$1 is not exactly one primary key with fingerprint $2"
  install -m 0644 "$1" "$3"
}

# Installs packages at their pinned versions and proves dpkg holds exactly
# those versions afterward.
install_pinned() { # package=version...
  local spec pkg want have
  apt-get install -y -qq --allow-downgrades "$@" >/dev/null
  for spec in "$@"; do
    pkg=${spec%%=*}
    want=${spec#*=}
    have=$(dpkg-query -W -f='${Version}' "$pkg")
    [ "$have" = "$want" ] || die "$pkg is $have, not the pinned $want"
    apt-mark hold "$pkg" >/dev/null
  done
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
apt-get install -y -qq ca-certificates curl gnupg age >/dev/null

install_key "$bundle/host/keys/pgdg.asc" "$PGDG_SIGNER_FPR" /etc/apt/keyrings/postgresql.asc
echo "deb [signed-by=/etc/apt/keyrings/postgresql.asc] https://apt.postgresql.org/pub/repos/apt noble-pgdg main" \
  >/etc/apt/sources.list.d/pgdg.list
say "PGDG repository in place"

install_key "$bundle/host/keys/crowdsec.asc" "$CROWDSEC_SIGNER_FPR" "$tmp/crowdsec.asc"
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
install_pinned "postgresql-common=$POSTGRESQL_COMMON_VERSION"
for d in /srv/data/postgresql /srv/data/pgbackrest-conf; do
  chown postgres:postgres "$d"
  chmod 0700 "$d"
done
install_pinned "postgresql-18=$POSTGRESQL_VERSION" "postgresql-client-18=$POSTGRESQL_VERSION" \
  "libpq5=$POSTGRESQL_VERSION" "pgbackrest=$PGBACKREST_VERSION"
install_pinned "crowdsec=$CROWDSEC_VERSION" "crowdsec-firewall-bouncer-nftables=$CROWDSEC_BOUNCER_VERSION"
say "pinned packages installed"

have_fb=$(dpkg-query -W -f='${Version}' filebeat 2>/dev/null || true)
if [ "$have_fb" = "$FILEBEAT_VERSION" ]; then
  say "filebeat $FILEBEAT_VERSION already installed"
else
  curl -fsSL --proto '=https' "$FILEBEAT_URL" -o "$tmp/filebeat.deb"
  echo "$FILEBEAT_SHA512  $tmp/filebeat.deb" | sha512sum -c --quiet - ||
    die "filebeat .deb does not match FILEBEAT_SHA512"
  dpkg -i "$tmp/filebeat.deb" >/dev/null
  apt-mark hold filebeat >/dev/null
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
copy_script "$bundle/scripts/secrets-host.sh"
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
