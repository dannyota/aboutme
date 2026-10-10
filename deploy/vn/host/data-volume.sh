#!/usr/bin/env bash
# Prepares the encrypted data volume on the production host
# (docs/design/vietnam-production.md, "Host" and "Secrets and host identity").
# Runs as root. Usage: data-volume.sh <device> [--format]
#
# Formats ext4 with label aboutme-data only with --format and only when the
# device has no signature at all. Mounts it at /srv/data by label, creates the
# source directories, and bind-mounts them to their targets. Safe to re-run.
set -euo pipefail

label=aboutme-data
mnt=/srv/data
fstab=/etc/fstab

die() {
  echo "data-volume: $*" >&2
  exit 1
}
kept() { echo "kept:    $*"; }
changed() { echo "changed: $*"; }

[ "$(id -u)" -eq 0 ] || die "run as root"
[ $# -ge 1 ] && [ $# -le 2 ] || die "usage: data-volume.sh <device> [--format]"
dev=$1
format=no
if [ $# -eq 2 ]; then
  [ "$2" = --format ] || die "unknown option $2"
  format=yes
fi
[ -b "$dev" ] || die "$dev is not a block device"
dev=$(readlink -f "$dev")

# source directory, owner, mode, bind target
dirs=(
  "secrets root:root 0700 /etc/aboutme/secrets"
  "podman-secrets root:root 0700 /var/lib/containers/storage/secrets"
  "postgresql root:root 0700 /var/lib/postgresql"
  "pgbackrest-conf root:root 0750 /etc/pgbackrest/conf.d"
  "crowdsec root:root 0755 /var/lib/crowdsec"
  "crowdsec-etc root:root 0755 /etc/crowdsec"
  "caddy 10001:10001 0700 /var/lib/aboutme-caddy"
  "aboutme root:root 0755 /var/lib/aboutme"
  "journal root:systemd-journal 2755 /var/log/journal"
)

already_ours=no
if mountpoint -q "$mnt" && [ "$(findmnt -no SOURCE "$mnt")" = "$dev" ]; then
  already_ours=yes
fi

if [ "$already_ours" = no ]; then
  if lsblk -nro MOUNTPOINT "$dev" | grep -q .; then
    die "$dev or one of its partitions is mounted"
  fi
  root_src=$(findmnt -no SOURCE /)
  root_disk=$(lsblk -no PKNAME "$root_src" 2>/dev/null | head -n1 || true)
  if [ -n "$root_disk" ]; then
    root_disk=/dev/$root_disk
  else
    root_disk=$root_src
  fi
  [ "$(readlink -f "$root_disk")" != "$dev" ] || die "$dev is the root disk"
  [ "$(readlink -f "$root_src")" != "$dev" ] || die "$dev is the root device"
fi

# Filesystem.
if [ "$already_ours" = yes ]; then
  kept "$dev is mounted at $mnt"
else
  sig=$(blkid -p -o value -s TYPE "$dev" 2>/dev/null || true)
  if [ -n "$sig" ]; then
    have=$(blkid -o value -s LABEL "$dev" 2>/dev/null || true)
    if [ "$have" = "$label" ]; then
      kept "$dev already carries label $label ($sig)"
    else
      die "$dev has a $sig signature with label '${have:-none}'; refusing"
    fi
  elif [ "$format" = yes ]; then
    mkfs.ext4 -q -L "$label" "$dev"
    changed "formatted $dev as ext4, label $label"
  else
    die "$dev has no filesystem; re-run with --format to create one"
  fi
fi

# fstab line for the volume.
vol_line="LABEL=$label $mnt ext4 defaults,nofail,x-systemd.device-timeout=30s 0 2"
mkdir -p "$mnt"
if grep -qxF "$vol_line" "$fstab"; then
  kept "fstab line for $mnt"
else
  echo "$vol_line" >>"$fstab"
  changed "added fstab line for $mnt"
fi
systemctl daemon-reload
if mountpoint -q "$mnt"; then
  kept "$mnt is mounted"
else
  mount "$mnt"
  changed "mounted $mnt"
fi

# Source directories, bind lines, and bind mounts.
for entry in "${dirs[@]}"; do
  read -r name owner mode target <<<"$entry"
  src=$mnt/$name
  new=no
  if [ -d "$src" ]; then
    kept "directory $src"
  else
    mkdir -p "$src"
    new=yes
    changed "created $src"
  fi
  # The PostgreSQL and pgBackRest directories get their final owner from
  # install.sh once the postgres user exists, so they are set here only when
  # just created; the others are set every run.
  case "$name" in
  postgresql | pgbackrest-conf)
    if [ "$new" = yes ]; then
      chown "$owner" "$src"
      chmod "$mode" "$src"
    fi
    ;;
  *)
    chown "$owner" "$src"
    chmod "$mode" "$src"
    ;;
  esac
  mkdir -p "$target"
  line="$src $target none bind,nofail,x-systemd.requires-mounts-for=$mnt 0 0"
  if grep -qxF "$line" "$fstab"; then
    kept "fstab bind line for $target"
  else
    echo "$line" >>"$fstab"
    changed "added fstab bind line for $target"
  fi
done
systemctl daemon-reload
for entry in "${dirs[@]}"; do
  read -r _ _ _ target <<<"$entry"
  if mountpoint -q "$target"; then
    kept "$target is mounted"
  else
    mount "$target"
    changed "mounted $target"
  fi
done
echo "data-volume: done"
