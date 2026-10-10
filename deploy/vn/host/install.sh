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

die() {
  echo "install: $*" >&2
  exit 1
}
say() { echo "install: $*"; }

# ABOUTME_INSTALL_ONLY=postgres stops once PostgreSQL and pgBackRest are
# installed and configured, with no timer, agent, or CrowdSec: the restore
# drill server (scripts/restore-drill.sh) needs nothing else.
only=${ABOUTME_INSTALL_ONLY:-}
case $only in "" | postgres) ;; *) die "ABOUTME_INSTALL_ONLY must be empty or postgres" ;; esac

[ "$(id -u)" -eq 0 ] || die "run as root"
[ $# -eq 1 ] || die "usage: install.sh <bundle-dir>"
bundle=$(readlink -f "$1")
[ -d "$bundle/host/etc" ] || die "$bundle/host/etc not found"
mountpoint -q /srv/data || die "/srv/data is not a mountpoint; run host/data-volume.sh first"
# shellcheck source=crowdsec-offline.sh
source "$bundle/host/crowdsec-offline.sh"

export DEBIAN_FRONTEND=noninteractive
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

timers=(
  aboutme-watch.timer
  aboutme-crowdsec-retention.timer
  aboutme-backup-full.timer
  aboutme-backup-diff.timer
  aboutme-backup-verify.timer
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
# age encrypts the secrets copy; msmtp sends alert.sh's mail. Both come from
# the Ubuntu archive.
apt-get install -y -qq ca-certificates curl gnupg age msmtp sqlite3 >/dev/null

install_key "$bundle/host/keys/pgdg.asc" "$PGDG_SIGNER_FPR" /etc/apt/keyrings/postgresql.asc
echo "deb [signed-by=/etc/apt/keyrings/postgresql.asc] https://apt.postgresql.org/pub/repos/apt noble-pgdg main" \
  >/etc/apt/sources.list.d/pgdg.list
say "PGDG repository in place"

if [ "$only" = postgres ]; then
  apt-get update -qq
  install_pinned "postgresql-common=$POSTGRESQL_COMMON_VERSION"
  chown postgres:postgres /srv/data/postgresql
  chmod 0700 /srv/data/postgresql
  # pgBackRest's secrets directory: root writes, postgres only reads
  # (scripts/secrets-host.sh).
  chown root:postgres /srv/data/pgbackrest-conf
  chmod 0750 /srv/data/pgbackrest-conf
  install_pinned "postgresql-18=$POSTGRESQL_VERSION" "postgresql-client-18=$POSTGRESQL_VERSION" \
    "libpq5=$POSTGRESQL_VERSION" "pgbackrest=$PGBACKREST_VERSION"
  install -m 0644 "$bundle/host/etc/pgbackrest/pgbackrest.conf" /etc/pgbackrest/pgbackrest.conf
  install -o postgres -g postgres -m 0640 "$bundle/host/etc/postgresql/18/main/pg_hba.conf" \
    /etc/postgresql/18/main/pg_hba.conf
  install -d -o postgres -g postgres -m 0755 /etc/postgresql/18/main/conf.d
  install -o postgres -g postgres -m 0644 "$bundle/host/etc/postgresql/18/main/conf.d/90-aboutme.conf" \
    /etc/postgresql/18/main/conf.d/90-aboutme.conf
  systemctl restart postgresql@18-main.service
  say "PostgreSQL and pgBackRest only: done"
  exit 0
fi

install_key "$bundle/host/keys/crowdsec.asc" "$CROWDSEC_SIGNER_FPR" "$tmp/crowdsec.asc"
gpg --dearmor --yes -o /etc/apt/keyrings/crowdsec.gpg "$tmp/crowdsec.asc"
echo "deb [signed-by=/etc/apt/keyrings/crowdsec.gpg] https://packagecloud.io/crowdsec/crowdsec/ubuntu noble main" \
  >/etc/apt/sources.list.d/crowdsec.list
say "CrowdSec repository in place"
apt-get update -qq

# 2. CrowdSec config goes in before the package starts the service, so its
# local API never tries 127.0.0.1:8080, which Go owns.
install -d -m 0755 /etc/crowdsec/acquis.d
install -m 0644 "$bundle/edge/crowdsec/config.yaml.local" /etc/crowdsec/config.yaml.local
install -m 0644 "$bundle/edge/crowdsec/acquis.d/sshd.yaml" /etc/crowdsec/acquis.d/sshd.yaml
install -m 0644 "$bundle/edge/crowdsec/acquis.d/aboutme-http.yaml" \
  /etc/crowdsec/acquis.d/aboutme-http.yaml
rm -f /etc/crowdsec/acquis.d/caddy.yaml

# 3. Packages. postgresql-common creates the postgres user; the data
# directories on the volume are handed to it before postgresql-18 creates the
# cluster there.
install_pinned "postgresql-common=$POSTGRESQL_COMMON_VERSION"
chown postgres:postgres /srv/data/postgresql
chmod 0700 /srv/data/postgresql
# pgBackRest's secrets directory: root writes, postgres only reads
# (scripts/secrets-host.sh).
chown root:postgres /srv/data/pgbackrest-conf
chmod 0750 /srv/data/pgbackrest-conf
install_pinned "postgresql-18=$POSTGRESQL_VERSION" "postgresql-client-18=$POSTGRESQL_VERSION" \
  "libpq5=$POSTGRESQL_VERSION" "pgbackrest=$PGBACKREST_VERSION"
# Install the persistent startup requirements before package scripts can
# enable either service. Until step 4 installs the required gate and lease
# check, a reboot during package installation fails closed.
install -d -m 0755 /etc/systemd/system/crowdsec.service.d \
  /etc/systemd/system/crowdsec-firewall-bouncer.service.d
install -m 0644 "$bundle/host/etc/systemd/system/crowdsec.service.d/90-aboutme-alert.conf" \
  /etc/systemd/system/crowdsec.service.d/90-aboutme-alert.conf
install -m 0644 \
  "$bundle/host/etc/systemd/system/crowdsec-firewall-bouncer.service.d/90-aboutme-alert.conf" \
  /etc/systemd/system/crowdsec-firewall-bouncer.service.d/90-aboutme-alert.conf
systemctl daemon-reload
# CrowdSec 1.8.1 registers with its central API in the package post-install
# script before it starts the service. Set the package's supported debconf
# choice and inhibit service startup while allowing the package to enable its
# units. Disable every package-enabled unit, and keep CrowdSec, the bouncer, and
# the vendor hub updater masked until the controlled configuration is ready.
unset CROWDSEC_POLICY_RC_D CROWDSEC_SYSTEMCTL
crowdsec_install_offline install_pinned "crowdsec=$CROWDSEC_VERSION" \
  "crowdsec-firewall-bouncer-nftables=$CROWDSEC_BOUNCER_VERSION"
crowdsec_remove_online /etc/crowdsec/config.yaml /etc/crowdsec/config.yaml.local \
  /etc/crowdsec/online_api_credentials.yaml || die "CrowdSec central API opt-out failed"
crowdsec_configure_local_detection || die "CrowdSec local detection setup failed"
say "pinned packages installed"


# 4. Configuration files. PostgreSQL files belong to postgres; the firewall
# bouncer file is built in step 5.
etc=$bundle/host/etc
copy_tree "$etc" /etc \
  postgresql/18/main/pg_hba.conf \
  postgresql/18/main/conf.d/90-aboutme.conf
install -o postgres -g postgres -m 0640 "$etc/postgresql/18/main/pg_hba.conf" /etc/postgresql/18/main/pg_hba.conf
install -d -o postgres -g postgres -m 0755 /etc/postgresql/18/main/conf.d
install -o postgres -g postgres -m 0644 "$etc/postgresql/18/main/conf.d/90-aboutme.conf" \
  /etc/postgresql/18/main/conf.d/90-aboutme.conf

install -m 0644 "$bundle/edge/crowdsec/config.yaml.local" /etc/crowdsec/config.yaml.local
install -m 0644 "$bundle/edge/crowdsec/console.yaml" /etc/crowdsec/console.yaml
install -m 0644 "$bundle/edge/crowdsec/profiles.yaml" /etc/crowdsec/profiles.yaml
install -m 0644 "$bundle/edge/crowdsec/acquis.d/aboutme-http.yaml" \
  /etc/crowdsec/acquis.d/aboutme-http.yaml
install -m 0644 "$bundle/edge/crowdsec/acquis.d/sshd.yaml" /etc/crowdsec/acquis.d/sshd.yaml
rm -f /etc/crowdsec/acquis.d/caddy.yaml
install -d -m 0755 /etc/crowdsec/parsers/s01-parse /etc/crowdsec/parsers/s02-enrich \
  /etc/crowdsec/scenarios
install -m 0644 "$bundle/edge/crowdsec/parsers/s01-parse/aboutme-http.yaml" \
  /etc/crowdsec/parsers/s01-parse/aboutme-http.yaml
install -m 0644 "$bundle/edge/crowdsec/parsers/s02-enrich/zz-aboutme-http-source.yaml" \
  /etc/crowdsec/parsers/s02-enrich/zz-aboutme-http-source.yaml
for scenario in "$bundle"/edge/crowdsec/scenarios/*.yaml; do
  install -m 0644 "$scenario" "/etc/crowdsec/scenarios/$(basename "$scenario")"
done

# The startup retention gate must run before the first unmasked CrowdSec
# start. Install its scripts early; step 6 refreshes the same copies.
lib=/usr/local/lib/aboutme
install -d -m 0755 "$lib"
install -m 0755 "$bundle/host/crowdsec-retention.sh" "$lib/crowdsec-retention.sh"
install -m 0755 "$bundle/host/crowdsec-retention-fail.sh" "$lib/crowdsec-retention-fail.sh"
install -m 0755 "$bundle/host/crowdsec-retention-lease-check.sh" \
  "$lib/crowdsec-retention-lease-check.sh"
install -m 0755 "$bundle/host/crowdsec-window-check.sh" "$lib/crowdsec-window-check.sh"

"$lib/crowdsec-window-check.sh"
systemctl daemon-reload
edge_restart=()
install -d -o 10001 -g 10001 -m 0750 /run/aboutme/caddy-log
if ! mountpoint -q /run/aboutme/caddy-log; then
  for unit in aboutme-caddy.service aboutme-maintenance.service; do
    systemctl is-active --quiet "$unit" && edge_restart+=("$unit")
  done
  systemctl stop aboutme-caddy.service aboutme-maintenance.service 2>/dev/null || true
  for unit in aboutme-caddy.service aboutme-maintenance.service; do
    systemctl is-active --quiet "$unit" && die "$unit stayed active during log tmpfs migration"
  done
  setpriv --reuid 10001 --regid 10001 --clear-groups \
    /usr/bin/find /run/aboutme/caddy-log -mindepth 1 -xdev -delete
fi
systemctl enable --now 'run-aboutme-caddy\x2dlog.mount' >/dev/null
[[ $(findmnt -n -o FSTYPE -M /run/aboutme/caddy-log) == tmpfs ]] || die "Caddy log mount is not tmpfs"
log_mount_options=$(findmnt -n -o OPTIONS -M /run/aboutme/caddy-log)
for option in nosuid nodev noexec mode=750 uid=10001 gid=10001; do
  [[ ,$log_mount_options, == *,$option,* ]] || die "Caddy log mount lacks $option"
done
[[ ,$log_mount_options, == *,size=65536k,* || ,$log_mount_options, == *,size=64M,* ]] || \
  die "Caddy log mount is not capped at 64 MiB"
systemd-tmpfiles --create /etc/tmpfiles.d/aboutme.conf
systemctl start aboutme-crowdsec-retention-startup.service
systemctl enable --now aboutme-crowdsec-retention.timer >/dev/null
systemctl unmask --runtime crowdsec.service crowdsec-firewall-bouncer.service >/dev/null
rm -f /var/log/crowdsec.log /var/log/crowdsec-firewall-bouncer.log
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
  cat "$bundle/edge/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local" >"$conf"
  printf 'api_key: %s\n' "$(cat "$keyfile")" >>"$conf"
)
systemctl enable crowdsec crowdsec-firewall-bouncer >/dev/null
systemctl restart crowdsec-firewall-bouncer
if ((${#edge_restart[@]})); then
  systemctl start "${edge_restart[@]}"
fi

# 6. Host scripts, quadlet templates, and environment files.
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
copy_script "$bundle/scripts/deploy-safety.sh"
copy_script "$bundle/scripts/secrets-host.sh"
copy_script "$bundle/host/crowdsec-offline.sh"
copy_script "$bundle/host/crowdsec-retention.sh"
copy_script "$bundle/host/crowdsec-retention-fail.sh"
copy_script "$bundle/host/crowdsec-retention-lease-check.sh"
copy_script "$bundle/host/crowdsec-window-check.sh"
copy_script "$bundle/host/job-run.sh"
copy_script "$bundle/host/workload-lock.sh"
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
# The journal lives on the data volume (data-volume.sh binds /var/log/journal);
# flush moves early-boot entries there.
systemctl restart systemd-journald
journalctl --flush
systemctl daemon-reload
zram_row=$(swapon --bytes --noheadings --show=NAME,SIZE,USED | awk '$1 == "/dev/zram0" { print $2, $3 }')
if [[ -z $zram_row ]]; then
  systemctl start systemd-zram-setup@zram0.service
else
  read -r zram_size zram_used <<<"$zram_row"
  if ((zram_size > 536870912)); then
    ((zram_used == 0)) || die "zram exceeds 512 MiB and is in use; reboot to apply the lower ceiling"
    systemctl restart systemd-zram-setup@zram0.service
  fi
fi
systemctl restart postgresql@18-main.service
systemctl enable --now "${timers[@]}" >/dev/null
systemctl enable --now unattended-upgrades.service >/dev/null

say "done"
