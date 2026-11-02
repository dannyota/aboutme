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

# ABOUTME_INSTALL_ONLY=postgres installs PostgreSQL and pgBackRest only.
only=${ABOUTME_INSTALL_ONLY:-}
case $only in "" | postgres) ;; *) die "invalid installation mode" ;; esac

[ "$(id -u)" -eq 0 ] || die "run as root"
[ $# -eq 1 ] || die "usage: install.sh <bundle-dir>"
bundle=$(readlink -f "$1")
# Steps run as other users (setpriv, runuser); they need a directory they can
# enter, not the caller's home.
cd /
[ -d "$bundle/host/etc" ] || die "$bundle/host/etc not found"
# Validate the incoming denial primitive before any persistent-state access.
# shellcheck source=crowdsec-privacy-notice-host.sh
source "$bundle/host/crowdsec-privacy-notice-host.sh"
notice_mount_identity >/dev/null 2>&1 || die "invalid_state"
mountpoint -q /srv/data || die "/srv/data is not a mountpoint; run host/data-volume.sh first"
# shellcheck source=crowdsec-offline.sh
source "$bundle/host/crowdsec-offline.sh"

# Serialize bundle replacement against every request entry and request unit.
if [[ ! -e /srv/data/host-operations && ! -L /srv/data/host-operations ]]; then
  install -d -o root -g root -m 0700 /srv/data/host-operations
fi
[[ -d /srv/data/host-operations && ! -L /srv/data/host-operations && $(stat -c '%u:%a' /srv/data/host-operations) == 0:700 ]] || die "unsafe operation state directory"
[[ ! -L /srv/data/host-operations/.bundle.lock ]] || die "unsafe bundle lock"
(umask 077; touch /srv/data/host-operations/.bundle.lock)
[[ $(stat -c '%u:%a' /srv/data/host-operations/.bundle.lock) == 0:600 ]] || die "unsafe bundle lock"
exec {operation_bundle_fd}>>/srv/data/host-operations/.bundle.lock
flock -x -w 30 "$operation_bundle_fd" || die "an operation may run"
[[ ! -e /srv/data/host-operations/key-change.json && ! -L /srv/data/host-operations/key-change.json ]] || die "key-change recovery must finish before installation"
operation_units=$(systemctl list-units --all --plain --no-legend 'aboutme-operation@*.service') || die "cannot inspect operation units"
while read -r operation_unit _ operation_active_state _; do
  [[ -n $operation_unit ]] || continue
  [[ $operation_unit == aboutme-operation@*.service ]] || die "cannot inspect operation unit state"
  case $operation_active_state in
    inactive | failed) ;;
    *) die "an operation unit may run" ;;
  esac
done <<<"$operation_units"
# Validate the saved protocol with the reviewed bundle before replacing files.
# shellcheck source=operation-state.sh
source "$bundle/host/operation-state.sh"
for operation in /srv/data/host-operations/*; do
  [[ -e $operation ]] || continue
  [[ -d $operation && ! -L $operation && -f $operation/state.json && ! -L $operation/state.json ]] || die "malformed operation state"
  operation_record=$(operation_state load "${operation##*/}") || die "malformed operation protocol"
  jq -e '.closureQuiescent==true and .fenceHeld==false and (.action.writeOutcome!="unknown") and (.phase=="closed" or .phase=="planned" or .phase=="serving")' <<<"$operation_record" >/dev/null || die "an operation remains unresolved"
done

# Adoption is explicit and precedes ordinary helper replacement on legacy hosts.
if [[ -e /var/lib/aboutme/fence.json ]]; then
  jq -e '(.operation_id // null)==null' /var/lib/aboutme/fence.json >/dev/null 2>&1 || die "an operation remains owned"
fi
if [[ -e /var/lib/aboutme/release.json ]]; then
  jq -e '.notice_authority.state=="adopted"' /var/lib/aboutme/fence.json >/dev/null 2>&1 || die "notice adoption is required"
fi
lib=/usr/local/lib/aboutme
install -d -o root -g root -m 0755 "$lib" /run/aboutme
for required in host/crowdsec-privacy-notice-host.sh host/workload-lock.sh \
  scripts/fence.sh scripts/deploy-safety.sh scripts/deploy-host.sh \
  scripts/crowdsec-privacy-notice.sh scripts/restore-drill.sh; do
  [[ -f $bundle/$required && ! -L $bundle/$required ]] || die "required notice helper is missing"
  install -o root -g root -m 0755 "$bundle/$required" "$lib/${required##*/}"
done
"$lib/crowdsec-privacy-notice-host.sh" initialize || die "invalid_state"

export DEBIAN_FRONTEND=noninteractive
tmp=$(mktemp -d)
crowdsec_install_closed=no
cleanup_install() {
  local status=$?
  trap - EXIT
  if [[ $crowdsec_install_closed == yes ]]; then
    "$lib/crowdsec-retention-fail.sh" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$tmp"
  exit "$status"
}
trap cleanup_install EXIT

timers=(
  aboutme-operation-retention.timer
  aboutme-watch.timer
  aboutme-deployment-document.timer
  aboutme-crowdsec-retention.timer
  aboutme-crowdsec-vacuum.timer
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
package_is_pinned() { # package=version
  local spec=$1 state
  state=$(dpkg-query -W -f='${Status}\t${Version}' "${spec%%=*}" 2>/dev/null) || return 1
  [[ $state == $'install ok installed\t'"${spec#*=}" ||
    $state == $'hold ok installed\t'"${spec#*=}" ]]
}

install_pinned() { # package=version...
  local spec pkg
  apt-get install -y -qq --allow-downgrades "$@" >/dev/null
  for spec in "$@"; do
    pkg=${spec%%=*}
    package_is_pinned "$spec" || die "$pkg is not installed at pinned ${spec#*=}"
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
# age encrypts the secrets copy; msmtp sends alert.sh's mail; catatonit is
# the init process for units run with --init. All come from
# the Ubuntu archive.
apt-get install -y -qq ca-certificates curl gnupg age msmtp python3-yaml catatonit openssl >/dev/null

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
  install -o postgres -g postgres -m 0640 "$bundle/host/etc/postgresql/18/main/pg_ident.conf" \
    /etc/postgresql/18/main/pg_ident.conf
  install -d -o postgres -g postgres -m 0755 /etc/postgresql/18/main/conf.d
  install -o postgres -g postgres -m 0644 "$bundle/host/etc/postgresql/18/main/conf.d/90-aboutme.conf" \
    /etc/postgresql/18/main/conf.d/90-aboutme.conf
  systemctl restart postgresql@18-main.service
  say "PostgreSQL and pgBackRest installed"
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
install -m 0644 "$bundle/edge/crowdsec/config.yaml.local" /etc/crowdsec/config.yaml.postgresql
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
# Suppress package registration and startup until the migration provisions pgx.
for package_pin in "crowdsec=$CROWDSEC_VERSION" "crowdsec-firewall-bouncer-nftables=$CROWDSEC_BOUNCER_VERSION"; do
  if ! package_is_pinned "$package_pin"; then
    if [[ $package_pin == crowdsec-firewall-bouncer-nftables=* ]]; then
      install -d -m 0700 /etc/crowdsec/bouncers
      # The package needs a key to skip automatic registration. The migration
      # replaces this unregistered private value before starting the bouncer.
      if [[ ! -s /etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local ]]; then
        install -m 0600 /dev/null /etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local
        (umask 077
         cat "$bundle/edge/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local"
         printf 'api_key: '
         openssl rand -hex 32) >/etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local
      fi
    fi
    crowdsec_install_closed=yes
    crowdsec_install_offline install_pinned "$package_pin" || die "crowdsec_package_refused"
  fi
  apt-mark hold "${package_pin%%=*}" >/dev/null
done


# 4. Configuration files. PostgreSQL files belong to postgres; the firewall
# bouncer file is built in step 5.
systemctl stop aboutme-deployment-document.timer aboutme-deployment-document.service 2>/dev/null || true
etc=$bundle/host/etc
copy_tree "$etc" /etc \
  postgresql/18/main/pg_hba.conf \
  postgresql/18/main/pg_ident.conf \
  postgresql/18/main/conf.d/90-aboutme.conf
install -o postgres -g postgres -m 0640 "$etc/postgresql/18/main/pg_hba.conf" /etc/postgresql/18/main/pg_hba.conf
install -o postgres -g postgres -m 0640 "$etc/postgresql/18/main/pg_ident.conf" /etc/postgresql/18/main/pg_ident.conf
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
for helper in crowdsec-db crowdsec-provision crowdsec-vacuum crowdsec-migrate; do
  install -m 0755 "$bundle/host/$helper.sh" "$lib/$helper.sh"
done
install -m 0644 "$bundle/host/crowdsec-schema-v1.8.1.tsv" "$lib/crowdsec-schema-v1.8.1.tsv"
install -m 0755 "$bundle/host/crowdsec-retention.sh" "$lib/crowdsec-retention.sh"
install -m 0755 "$bundle/host/crowdsec-retention-fail.sh" "$lib/crowdsec-retention-fail.sh"
install -m 0755 "$bundle/host/crowdsec-retention-lease-check.sh" \
  "$lib/crowdsec-retention-lease-check.sh"
install -m 0755 "$bundle/host/crowdsec-window-check.sh" "$lib/crowdsec-window-check.sh"

# Reload peer and logging policy without changing the PostgreSQL process epoch.
systemctl reload postgresql@18-main.service
[[ $(findmnt -n -o FSTYPE -T /run/aboutme) == tmpfs ]] || die "CrowdSec diagnostics require tmpfs"
install -d -o root -g root -m 0700 /run/aboutme/crowdsec
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
rm -f /var/log/crowdsec.log /var/log/crowdsec-firewall-bouncer.log
say "configuration copied to /etc"

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
copy_script "$bundle/host/crowdsec-db.sh"
copy_script "$bundle/host/crowdsec-provision.sh"
copy_script "$bundle/host/crowdsec-migrate.sh"
copy_script "$bundle/host/crowdsec-vacuum.sh"
copy_script "$bundle/host/crowdsec-window-check.sh"
copy_script "$bundle/host/operation-run.sh"
copy_script "$bundle/host/operation-state.sh"
copy_script "$bundle/host/operation-account.sh"
copy_script "$bundle/host/job-run.sh"
copy_script "$bundle/host/deployment-document.sh"
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
# The bridge networks do not depend on a release. The observer's firewall
# must load before any observer starts, including after a reboot.
install -d -m 0755 /etc/containers/systemd
install -m 0644 "$bundle/host/quadlet/aboutme.network" /etc/containers/systemd/aboutme.network
install -m 0644 "$bundle/host/quadlet/aboutme-observer.network" /etc/containers/systemd/aboutme-observer.network
systemctl daemon-reload
systemctl enable aboutme-observer-firewall.service >/dev/null
systemctl restart aboutme-observer-firewall.service
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

# Keep the reviewed detection and bouncer templates available to the host step.
install -d -m 0755 "$lib/crowdsec"
copy_tree "$bundle/edge/crowdsec" "$lib/crowdsec"
ABOUTME_MIGRATE_BUNDLE_FD=$operation_bundle_fd "$lib/crowdsec-migrate.sh" || die "crowdsec_migrate_refused"
if ((${#edge_restart[@]})); then systemctl start "${edge_restart[@]}"; fi

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
systemctl start aboutme-deployment-document.service
systemctl enable --now "${timers[@]}" >/dev/null
systemctl enable --now unattended-upgrades.service >/dev/null

crowdsec_install_closed=no
say "done"
