#!/usr/bin/env bash
# Source-only guest adapter. Database, backup, controller and locks remain real.
restore_fixture_start() {
  local root=$1 helper
  [[ $EUID == 0 && ${GITHUB_ACTIONS:-} == true &&
    $(readlink /proc/self/ns/mnt) != "$(readlink /proc/1/ns/mnt)" ]] || return 1
  restore_guest=$(mktemp -d /run/crowdsec-restore-guest.XXXXXX)
  mount -t tmpfs -o size=512m,mode=0755 tmpfs "$restore_guest"
  install -d -m 0755 "$restore_guest/bin" "$restore_guest/lib" /srv/data /var/lib/aboutme /run/aboutme /etc/crowdsec
  truncate -s 64M "$restore_guest/fence-disk"
  mkfs.ext4 -q -L aboutme-data "$restore_guest/fence-disk"
  restore_loop=$(losetup --find --show "$restore_guest/fence-disk")
  mount "$restore_loop" /srv/data
  install -d -m 0755 /srv/data/aboutme
  install -d -m 0700 /srv/data/host-operations
  mount --bind /srv/data/aboutme /var/lib/aboutme
  mount -t tmpfs -o size=16m,mode=0755 tmpfs /run/aboutme
  install -o root -g postgres -m 0660 /dev/null /run/aboutme/workload.lock
  mount -t tmpfs -o size=16m,mode=0755 tmpfs /run/postgresql
  chown postgres:postgres /run/postgresql
  install -d -o postgres -g postgres -m 0700 "$restore_guest/data" "$restore_guest/repository"
  install -d -o root -g postgres -m 2770 "$restore_guest/audit"
  runuser -u postgres -- /usr/lib/postgresql/18/bin/initdb -D "$restore_guest/data" \
    --auth-local=peer --auth-host=reject --no-locale --encoding=UTF8 >/dev/null 2>&1
  install -d -m 0755 "$restore_guest/pgconfig" /etc/postgresql/18/main "$restore_guest/backup-config" /etc/pgbackrest
  for helper in pg_hba.conf pg_ident.conf; do
    install -m 0644 "$root/deploy/vn/host/etc/postgresql/18/main/$helper" "$restore_guest/pgconfig/$helper"
  done
  mount --bind "$restore_guest/pgconfig" /etc/postgresql/18/main
  cat >>"$restore_guest/data/postgresql.conf" <<CONFIG
listen_addresses = ''
unix_socket_directories = '/run/postgresql'
shared_buffers = '32MB'
max_connections = 40
hba_file = '/etc/postgresql/18/main/pg_hba.conf'
ident_file = '/etc/postgresql/18/main/pg_ident.conf'
wal_level = 'replica'
archive_mode = 'on'
archive_command = '/usr/bin/pgbackrest --stanza=aboutme archive-push %p'
log_min_messages = 'panic'
log_min_error_statement = 'panic'
log_parameter_max_length = 0
log_parameter_max_length_on_error = 0
CONFIG
  cat >"$restore_guest/backup-config/pgbackrest.conf" <<CONFIG
[global]
repo1-type=posix
repo1-path=$restore_guest/repository
repo1-retention-full=2
log-level-console=off
log-level-file=off
compress-type=gz
process-max=1
[aboutme]
pg1-path=$restore_guest/data
pg1-socket-path=/run/postgresql
CONFIG
  chmod 0644 "$restore_guest/backup-config/pgbackrest.conf"
  mount --bind "$restore_guest/backup-config" /etc/pgbackrest
  runuser -u postgres -- /usr/lib/postgresql/18/bin/pg_ctl -D "$restore_guest/data" \
    -l "$restore_guest/data/server.log" -w start >/dev/null 2>&1
  # A pristine runner staged only the authenticated two executables and fixed
  # dependencies. Do not copy assets from a configured CrowdSec package.
  cp -a /etc/crowdsec "$restore_guest/crowdsec"
  mount --bind "$restore_guest/crowdsec" /etc/crowdsec
  for helper in crowdsec-db crowdsec-provision crowdsec-cutover-state crowdsec-privacy-notice-host workload-lock; do
    install -m 0755 "$root/deploy/vn/host/$helper.sh" "$restore_guest/lib/$helper.sh"
  done
  for helper in fence deploy-safety restore-drill; do
    install -m 0755 "$root/deploy/vn/scripts/$helper.sh" "$restore_guest/lib/$helper.sh"
  done
  install -m 0644 /usr/local/lib/aboutme/crowdsec-restore-binaries.sha256 "$restore_guest/lib/"
  install -m 0644 "$root/deploy/vn/host/crowdsec-schema-v1.8.1.tsv" "$restore_guest/lib/"
  mount --bind "$restore_guest/lib" /usr/local/lib/aboutme
  install -d -m 0700 /run/aboutme/crowdsec
  printf '#!/usr/bin/env bash\nset -euo pipefail\nfixture=%q\n' "$restore_guest" >"$restore_guest/bin/systemctl"
  cat >>"$restore_guest/bin/systemctl" <<'SH'
case $1 in
  show)
    if [[ $* == *postgresql@18-main.service* ]]; then
      if [[ $* == *MainPID* ]]; then
        printf 'ActiveState=active\nJob=\nMainPID=%s\nInvocationID=11111111111111111111111111111111\n' "$(head -n1 "$fixture/data/postmaster.pid")"
      else printf 'ActiveState=active\nJob=\n'; fi
    else printf 'ActiveState=inactive\nJob=\n'; fi ;;
  stop|start)
    [[ ${2-} == postgresql@18-main.service ]] || exit 1
    if [[ $1 == stop ]]; then
      runuser -u postgres -- /usr/lib/postgresql/18/bin/pg_ctl -D "$fixture/data" -m fast -w stop >/dev/null 2>&1
    else
      # A manager-started daemon does not inherit the request's workload lock.
      # The waiting adapter and controller retain it until foreground joins.
      (
        for entry in /proc/$BASHPID/fd/*; do
          descriptor=${entry##*/}
          if [[ $descriptor =~ ^[0-9]+$ ]] && ((descriptor>2)); then exec {descriptor}>&-; fi
        done
        exec runuser -u postgres -- /usr/lib/postgresql/18/bin/pg_ctl -D "$fixture/data" -l "$fixture/data/server.log" -w start
      ) >/dev/null 2>&1
    fi ;;
  *) exit 1 ;;
esac
SH
  # Audit only the actual controller's foreground pgBackRest process. The
  # unmodified binary performs restore, WAL recovery and all file operations.
  printf '#!/usr/bin/env bash\nset -euo pipefail\nfixture=%q\n' "$restore_guest" >"$restore_guest/bin/pgbackrest"
  cat >>"$restore_guest/bin/pgbackrest" <<'SH'
[[ ${*: -1} == restore && ! -e /proc/self/fd/3 && ! -e /proc/self/fd/4 ]]
if /usr/bin/flock -n /run/aboutme/workload.lock true; then exit 1; fi
state=$(jq -ce 'select(.operation_kind=="notice_restore") | {operation_id,operation_tag}' /var/lib/aboutme/fence.json)
printf '%s\n' "$state" >"$fixture/audit/restoring-owner"
[[ -f /var/lib/aboutme/privacy-notice/restore-reconciliation &&
  ! -e /var/lib/aboutme/privacy-notice/send.json && ! -e /var/lib/aboutme/privacy-notice/activation.json ]]
/usr/bin/pgbackrest "$@"
printf 'joined\n' >"$fixture/audit/restore-joined"
SH
  # Record the real exclusive canonical lock inside provisioning. The kernel
  # flock, private child context and all admission checks stay unchanged.
  printf '#!/usr/bin/env bash\nset -euo pipefail\nfixture=%q\n' "$restore_guest" >"$restore_guest/bin/flock"
  cat >>"$restore_guest/bin/flock" <<'SH'
if [[ $* == '-x -w 2 '* && $(readlink "/proc/self/fd/${*: -1}") == /var/lib/aboutme/fence.lock ]]; then
  jq -e --argjson bound "$(cat "$fixture/audit/restoring-owner")" \
    '.operation_kind=="notice_restore" and .operation_id==$bound.operation_id and .operation_tag==$bound.operation_tag' \
    /var/lib/aboutme/fence.json >/dev/null
  printf 'canonical\n' >>"$fixture/reset-locks"
fi
exec /usr/bin/flock "$@"
SH
  chmod 0755 "$restore_guest/bin/"*
  export PATH=$restore_guest/bin:/usr/sbin:/usr/bin:/sbin:/bin
  /usr/local/lib/aboutme/fence.sh init >/dev/null 2>&1
  /usr/local/lib/aboutme/crowdsec-privacy-notice-host.sh initialize >/dev/null 2>&1
}
restore_admin() {
  runuser -u postgres -- /usr/lib/postgresql/18/bin/psql -X -qAt -w -v ON_ERROR_STOP=1 \
    -h /run/postgresql -d "${1:-postgres}" 2>/dev/null
}
restore_fixture_cleanup() {
  local status=$?
  trap - EXIT
  if [[ -f ${restore_guest:-}/data/postmaster.pid ]]; then
    runuser -u postgres -- /usr/lib/postgresql/18/bin/pg_ctl -D "$restore_guest/data" -m immediate -w stop >/dev/null 2>&1 || status=1
  fi
  umount /var/lib/aboutme /srv/data || status=1
  losetup -d "${restore_loop:?fixture loop required}" || status=1
  umount "$restore_guest" || status=1
  rmdir -- "$restore_guest" || status=1
  exit "$status"
}
