#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
work=$(cd "$work" && pwd -P)
fail() { printf 'host rules: %s\n' "$*" >&2; exit 1; }

mkdir -p "$work/lock-bin" "$work/run" "$work/lib"
chmod 0755 "$work/run"
: >"$work/run/crowdsec-retention.lock"
chmod 0600 "$work/run/crowdsec-retention.lock"
: >"$work/lib/fence.lock"
export HOST_RULES_REAL_FLOCK HOST_RULES_FLOCK_LOG=$work/flock-calls
HOST_RULES_REAL_FLOCK=$(command -v flock)
cat >"$work/lock-bin/flock" <<'STUB'
#!/usr/bin/env bash
descriptor=${!#}
path=$(readlink "/proc/$PPID/fd/$descriptor")
fence=no
for entry in "/proc/$PPID/fd/"*; do
  if [[ $(readlink "$entry" 2>/dev/null) == */fence.lock ]]; then fence=yes; fi
done
printf '%s fence-open=%s\n' "$path" "$fence" >>"$HOST_RULES_FLOCK_LOG"
exec "$HOST_RULES_REAL_FLOCK" "$@"
STUB
chmod +x "$work/lock-bin/flock"
rewrite_locks() {
  sed -e "s|/run/aboutme|$work/run|g" -e "s|/var/lib/aboutme/fence.lock|$work/lib/fence.lock|g"
}
retention_record="$work/run/crowdsec-retention.lock fence-open=no"

sed -n '/^watch_crowdsec() (/,/^)/p' "$root/deploy/vn/host/bin/watch.sh" | rewrite_locks >"$work/watch-lock.sh"
# shellcheck source=/dev/null
source "$work/watch-lock.sh"
declare -F watch_crowdsec >/dev/null || fail 'watch.sh has no watch_crowdsec'
timeout() { echo 7; }
: >"$HOST_RULES_FLOCK_LOG"
bans=$(PATH=$work/lock-bin:$PATH; watch_crowdsec) || fail 'watch_crowdsec failed with both locks free'
[[ $bans == 7 ]] || fail "watch_crowdsec returned '$bans'"
printf '%s\n' "$retention_record" "$work/lib/fence.lock fence-open=yes" >"$work/watch-expected"
cmp -s "$work/watch-expected" "$HOST_RULES_FLOCK_LOG" ||
  fail "watch.sh must take the retention lock before opening the fence: $(paste -sd'|' "$HOST_RULES_FLOCK_LOG")"
unset -f timeout

sed -n '/^aboutme_edge_transition() (/,/^)/p' "$root/deploy/vn/scripts/deploy-safety.sh" |
  rewrite_locks >"$work/edge-lock.sh"
# shellcheck source=/dev/null
source "$work/edge-lock.sh"
declare -F aboutme_edge_transition >/dev/null || fail 'deploy-safety.sh has no aboutme_edge_transition'
aboutme_edges_stable() { return 0; }
: >"$HOST_RULES_FLOCK_LOG"
(PATH=$work/lock-bin:$PATH; aboutme_edge_transition touch "$work/edge-done") ||
  fail 'aboutme_edge_transition failed with the retention lock free'
[[ -e $work/edge-done ]] || fail 'aboutme_edge_transition did not run its command'
[[ $(cat "$HOST_RULES_FLOCK_LOG") == "$retention_record" ]] ||
  fail "deploy-safety.sh must request the retention lock with no fence open: $(paste -sd'|' "$HOST_RULES_FLOCK_LOG")"
for script in deploy-safety.sh deploy-host.sh; do
  if grep -n 'fence\.lock' "$root/deploy/vn/scripts/$script" >&2; then fail "$script opens the fence lock itself"; fi
done
if grep -nE '(^|[;&|[:space:]])(source|\.)[[:space:]]+[^[:space:]]*crowdsec-db\.sh' \
  "$root/deploy/vn/scripts/deploy-host.sh" >&2; then
  fail 'deploy-host.sh must run crowdsec-db.sh as a child, not source it'
fi

mkdir -p "$work/volume-bin" "$work/dev" "$work/root"
export HOST_RULES_WORK=$work HOST_RULES_CALLS=$work/volume-calls HOST_RULES_REAL_MKDIR
HOST_RULES_REAL_MKDIR=$(command -v mkdir)
for tool in lsblk findmnt mountpoint blkid mkfs.ext4 wipefs systemctl mount chown chmod id; do
  cat >"$work/volume-bin/$tool" <<'STUB'
#!/usr/bin/env bash
tool=${0##*/}
printf '%s %s\n' "$tool" "$*" >>"$HOST_RULES_CALLS"
case "$tool $*" in
  'id -u') echo 0 ;;
  'findmnt -no SOURCE /') echo /dev/vda1 ;;
  'lsblk -no PKNAME '*) echo vda ;;
  'mountpoint '*) exit 1 ;;
  'blkid -p -o value -s TYPE '*) [[ -n $HOST_RULES_SIGNATURE ]] || exit 2; echo "$HOST_RULES_SIGNATURE" ;;
  'blkid -o value -s LABEL '*) [[ -n $HOST_RULES_LABEL ]] || exit 2; echo "$HOST_RULES_LABEL" ;;
esac
exit 0
STUB
  chmod +x "$work/volume-bin/$tool"
done
cat >"$work/volume-bin/mkdir" <<'STUB'
#!/usr/bin/env bash
printf 'mkdir %s\n' "$*" >>"$HOST_RULES_CALLS"
for arg in "$@"; do
  case $arg in
    -p) ;;
    *..*) echo "mkdir refused: $arg" >&2; exit 1 ;;
    "$HOST_RULES_WORK"/*) ;;
    *) echo "mkdir refused outside the work directory: $arg" >&2; exit 1 ;;
  esac
done
exec "$HOST_RULES_REAL_MKDIR" "$@"
STUB
chmod +x "$work/volume-bin/mkdir"
sed -e "s|^mnt=/srv/data\$|mnt=$work/srv/data|" \
  -e "s|^fstab=/etc/fstab\$|fstab=$work/fstab|" \
  -e 's|\[ -b "\$dev" \]|[ -f "$dev" ]|' \
  -e "s|^\(  \"[^\"]* \)/|\1$work/root/|" \
  "$root/deploy/vn/host/data-volume.sh" >"$work/data-volume.sh"
[[ $(grep -c "^mnt=$work/srv/data\$\|^fstab=$work/fstab\$\|\[ -f \"\$dev\" \]" "$work/data-volume.sh") == 3 &&
  $(grep -c "^  \"[^\"]* $work/root/" "$work/data-volume.sh") == 9 ]] ||
  fail 'data-volume.sh paths changed; update the rewrite'
device=$work/dev/vdb
: >"$device"
run_volume() {
  local signature=$1 label=$2
  shift 2
  : >"$HOST_RULES_CALLS"
  rm -rf "$work/srv" "$work/root" "$work/fstab"
  "$HOST_RULES_REAL_MKDIR" -p "$work/root"
  : >"$work/fstab"
  HOST_RULES_SIGNATURE=$signature HOST_RULES_LABEL=$label PATH=$work/volume-bin:$PATH \
    bash "$work/data-volume.sh" "$device" "$@" >"$work/volume-out" 2>&1
}
assert_no_format() {
  if grep -nE '^(mkfs\.ext4|wipefs) ' "$HOST_RULES_CALLS" >&2; then fail "$1 formatted or wiped the device"; fi
}

if run_volume '' ''; then fail 'data-volume.sh accepted a blank device without --format'; fi
grep -q 're-run with --format' "$work/volume-out" || fail "blank device without --format: $(cat "$work/volume-out")"
assert_no_format 'a blank device without --format'

if run_volume xfs '' --format; then fail 'data-volume.sh accepted a foreign xfs signature with --format'; fi
grep -q 'refusing' "$work/volume-out" || fail "foreign signature: $(cat "$work/volume-out")"
assert_no_format 'a foreign signature with --format'

run_volume ext4 aboutme-data --format || fail "own volume with --format: $(cat "$work/volume-out")"
assert_no_format 'an existing aboutme-data volume with --format'

run_volume '' '' --format || fail "blank device with --format: $(cat "$work/volume-out")"
[[ $(grep -E '^(mkfs\.ext4|wipefs) ' "$HOST_RULES_CALLS") == "mkfs.ext4 -q -L aboutme-data $device" ]] ||
  fail "blank device with --format must call mkfs.ext4 once: $(grep -E '^(mkfs\.ext4|wipefs) ' "$HOST_RULES_CALLS" | paste -sd'|')"

drill=$root/deploy/vn/scripts/restore-drill.sh
mapfile -t install_body < <(sed -n '/^install_drill_host() {/,/^}/p' "$drill" |
  sed '1d;$d' | grep -vE '^[[:space:]]*(#|$)')
((${#install_body[@]} >= 3)) || fail 'restore-drill.sh has no install_drill_host body'
install_line=${install_body[-3]}
stop_lines=$(printf '%s\n' "${install_body[@]: -2}" | sed 's/^[[:space:]]*//' | sort)
expected_stops=$(printf '%s\n' \
  'rsh sudo systemctl stop postgresql@18-main.service' \
  "rsh sudo bash -c 'systemctl stop \"aboutme-backup*.timer\" 2>/dev/null || true'" | sort)
[[ $install_line == *'/host/install.sh"'* && $stop_lines == "$expected_stops" ]] ||
  fail "install_drill_host must end with install.sh, then the PostgreSQL and backup timer stops: $(printf '%s|' "${install_body[@]: -3}")"
install_call=$(grep -nx 'install_drill_host' "$drill" | cut -d: -f1)
copy_call=$(grep -nx 'copy_secrets' "$drill" | cut -d: -f1)
restore_call=$(grep -n '^restore_run ' "$drill" | head -n1 | cut -d: -f1)
[[ $install_call =~ ^[0-9]+$ && $copy_call =~ ^[0-9]+$ && $restore_call =~ ^[0-9]+$ ]] ||
  fail 'restore-drill.sh must call install_drill_host, copy_secrets and restore_run once each at top level'
((install_call < copy_call && copy_call < restore_call)) ||
  fail "restore-drill.sh must run install_drill_host (line $install_call) before copy_secrets (line $copy_call)"
copy_start=$(grep -n '^copy_secrets() {' "$drill" | cut -d: -f1)
copy_end=$(awk -v s="$copy_start" 'NR > s && /^}/ {print NR; exit}' "$drill")
while IFS=: read -r number _; do
  ((number > copy_start && number < copy_end)) ||
    fail "restore-drill.sh line $number reads secrets.conf outside copy_secrets"
done < <(grep -n 'secrets\.conf' "$drill")

echo 'host rules tests passed'
