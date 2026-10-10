#!/usr/bin/env bash
# Enforces the bounded lifetime of attack records in CrowdSec. The startup
# mode runs before any reader of the local API. Runtime runs every five minutes
# while CrowdSec is active.
set -euo pipefail
exec >/dev/null 2>/dev/null
umask 077

mode=${1:-}
[[ $mode == startup || $mode == runtime ]] || exit 2

if ((EUID == 0)) && {
  [[ -n ${ABOUTME_CROWDSEC_DB:-} ]] ||
    [[ -n ${ABOUTME_CROWDSEC_FEED_DIR:-} ]] ||
    [[ -n ${ABOUTME_CROWDSEC_SWAPS:-} ]] ||
    [[ -n ${ABOUTME_CROWDSEC_CSCLI:-} ]] ||
    [[ -n ${ABOUTME_CROWDSEC_SQLITE:-} ]] ||
    [[ -n ${ABOUTME_CROWDSEC_AFTER_FEED_HOOK:-} ]] ||
    [[ -n ${ABOUTME_RETENTION_SUCCESS_MARKER:-} ]] ||
    [[ ${ABOUTME_CROWDSEC_SKIP_MOUNT_CHECK:-0} == 1 ]] ||
    [[ ${ABOUTME_CROWDSEC_SKIP_TIME_CHECK:-0} == 1 ]];
}; then
  exit 3
fi

db=${ABOUTME_CROWDSEC_DB:-/var/lib/crowdsec/data/crowdsec.db}
feed_dir=${ABOUTME_CROWDSEC_FEED_DIR:-/run/aboutme/caddy-log/crowdsec}
swaps=${ABOUTME_CROWDSEC_SWAPS:-/proc/swaps}
cscli=${ABOUTME_CROWDSEC_CSCLI:-/usr/bin/cscli}
sqlite=${ABOUTME_CROWDSEC_SQLITE:-/usr/bin/sqlite3}
success=${ABOUTME_RETENTION_SUCCESS_MARKER:-/run/aboutme/crowdsec-retention-ok}

renew_lease() {
  local lease_tmp=$success.$$
  umask 077
  : >"$lease_tmp"
  chmod 0600 "$lease_tmp"
  mv -fT "$lease_tmp" "$success"
  rm -f /run/aboutme/crowdsec-retention-failed
}

if [[ ${ABOUTME_CROWDSEC_SKIP_TIME_CHECK:-0} != 1 ]]; then
  [[ $(timedatectl show -p NTPSynchronized --value) == yes ]] || exit 9
fi

if [[ $mode == startup && ! -e $feed_dir ]]; then
  feed_parent=${feed_dir%/*}
  [[ -d $feed_parent && ! -L $feed_parent ]] || exit 10
  [[ $(findmnt -n -o FSTYPE -T "$feed_parent") == tmpfs ]] || exit 11
  install -d -o 10001 -g 10001 -m 0700 "$feed_dir"
  for active in serving maintenance serving-bouncer maintenance-bouncer; do
    install -o 10001 -g 10001 -m 0600 /dev/null "$feed_dir/$active.json"
  done
fi

[[ -d $feed_dir && ! -L $feed_dir ]] || exit 10
if [[ ${ABOUTME_CROWDSEC_SKIP_MOUNT_CHECK:-0} != 1 ]]; then
  [[ $(findmnt -n -o FSTYPE -T "$feed_dir") == tmpfs ]] || exit 11
fi
if ((EUID == 0)); then
  [[ $(stat -c '%u:%g:%a' "$feed_dir") == 10001:10001:700 ]] || exit 13
fi

while read -r device _; do
  [[ $device == Filename || $device == /dev/zram* ]] || exit 12
done <"$swaps"

for active in \
  "$feed_dir/serving.json" \
  "$feed_dir/maintenance.json" \
  "$feed_dir/serving-bouncer.json" \
  "$feed_dir/maintenance-bouncer.json"; do
  if ((EUID == 0)); then
    /usr/bin/setpriv --reuid 10001 --regid 10001 --clear-groups \
      /usr/bin/truncate -s 0 -- "$active" || exit 13
  else
    /usr/bin/truncate -s 0 -- "$active" || exit 13
  fi
  [[ -f $active && ! -L $active ]] || exit 13
  if ((EUID == 0)); then
    [[ $(stat -c '%u:%g:%a' "$active") == 10001:10001:600 ]] || exit 13
  fi
done
if ((EUID == 0)); then
  [[ $(stat -c '%u:%g:%a' "$feed_dir") == 10001:10001:700 ]] || exit 13
fi

if [[ -n ${ABOUTME_CROWDSEC_AFTER_FEED_HOOK:-} ]]; then
  "$ABOUTME_CROWDSEC_AFTER_FEED_HOOK"
fi

if find "$feed_dir" -maxdepth 1 -type l -regextype posix-extended \
  -regex '.*/(serving|maintenance)(-bouncer)?-[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}-[0-9]{2}-[0-9]{2}\.[0-9]{3}-(time|size)\.json' \
  -print -quit | grep -q .; then
  exit 14
fi
if ((EUID == 0)); then
  /usr/bin/setpriv --reuid 10001 --regid 10001 --clear-groups \
    /usr/bin/find "$feed_dir" -maxdepth 1 -type f -regextype posix-extended \
      -regex '.*/(serving|maintenance)(-bouncer)?-[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}-[0-9]{2}-[0-9]{2}\.[0-9]{3}-(time|size)\.json' \
      -delete || exit 14
else
  find "$feed_dir" -maxdepth 1 -type f -regextype posix-extended \
    -regex '.*/(serving|maintenance)(-bouncer)?-[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}-[0-9]{2}-[0-9]{2}\.[0-9]{3}-(time|size)\.json' \
    -delete || exit 14
fi

if [[ ! -e $db ]]; then
  [[ $mode == startup ]] || exit 15
  renew_lease
  exit 0
fi
[[ -f $db && ! -L $db ]] || exit 16

if [[ $mode == runtime ]]; then
  [[ ! -e /run/aboutme/crowdsec-retention-failed ]] || exit 20
  if systemctl is-active --quiet crowdsec.service; then
    timeout 20s "$cscli" alerts flush --max-age 23h >/dev/null 2>&1 || exit 20
  fi
fi

purge_batch() {
  timeout 10s "$sqlite" "$db" <<'SQL' >/dev/null
.timeout 5000
PRAGMA foreign_keys=ON;
BEGIN IMMEDIATE;
CREATE TEMP TABLE expired_alerts(id INTEGER PRIMARY KEY);
INSERT INTO expired_alerts
SELECT id
FROM alerts
WHERE datetime(created_at) IS NULL
   OR datetime(created_at) <= datetime('now', '-23 hours')
   OR EXISTS (
     SELECT 1 FROM events
     WHERE events.alert_events = alerts.id
       AND (
         datetime(events.created_at) IS NULL
         OR datetime(events.created_at) <= datetime('now', '-23 hours')
         OR datetime(events.time) IS NULL
         OR datetime(events.time) <= datetime('now', '-23 hours')
       )
   )
   OR EXISTS (
     SELECT 1 FROM meta
     WHERE meta.alert_metas = alerts.id
       AND (
         datetime(meta.created_at) IS NULL
         OR datetime(meta.created_at) <= datetime('now', '-23 hours')
       )
   )
ORDER BY id
LIMIT 1000;
DELETE FROM alerts WHERE id IN (SELECT id FROM expired_alerts);
DELETE FROM events
WHERE alert_events IS NULL
   OR NOT EXISTS (SELECT 1 FROM alerts WHERE alerts.id = events.alert_events);
DELETE FROM meta
WHERE alert_metas IS NULL
   OR NOT EXISTS (SELECT 1 FROM alerts WHERE alerts.id = meta.alert_metas);
COMMIT;
SQL
}

while :; do
  count=$(timeout 10s "$sqlite" "$db" ".timeout 5000" \
    "SELECT count(*) FROM alerts WHERE datetime(created_at) IS NULL OR datetime(created_at) <= datetime('now', '-23 hours') OR EXISTS (SELECT 1 FROM events WHERE events.alert_events = alerts.id AND (datetime(events.created_at) IS NULL OR datetime(events.created_at) <= datetime('now', '-23 hours') OR datetime(events.time) IS NULL OR datetime(events.time) <= datetime('now', '-23 hours'))) OR EXISTS (SELECT 1 FROM meta WHERE meta.alert_metas = alerts.id AND (datetime(meta.created_at) IS NULL OR datetime(meta.created_at) <= datetime('now', '-23 hours')));") || exit 21
  [[ $count =~ ^[0-9]+$ ]] || exit 22
  ((count > 0)) || break
  purge_batch || exit 23
done

# Decisions expire independently of their alert. The creation-time condition
# is the fallback for a malformed or unexpectedly long decision.
timeout 10s "$sqlite" "$db" <<'SQL' >/dev/null || exit 24
.timeout 5000
PRAGMA foreign_keys=ON;
BEGIN IMMEDIATE;
DELETE FROM decisions
WHERE until IS NULL
   OR datetime(until) IS NULL
   OR datetime(until) > datetime(created_at, '+22 hours')
   OR datetime(until) <= datetime('now')
   OR datetime(created_at) IS NULL
   OR datetime(created_at) <= datetime('now', '-23 hours')
   OR (
     alert_decisions IS NOT NULL
     AND NOT EXISTS (SELECT 1 FROM alerts WHERE alerts.id = decisions.alert_decisions)
   );
DELETE FROM events
WHERE alert_events IS NULL
   OR NOT EXISTS (SELECT 1 FROM alerts WHERE alerts.id = events.alert_events);
DELETE FROM meta
WHERE alert_metas IS NULL
   OR NOT EXISTS (SELECT 1 FROM alerts WHERE alerts.id = meta.alert_metas);
COMMIT;
SQL

checkpoint=$(timeout 10s "$sqlite" "$db" 'PRAGMA wal_checkpoint(TRUNCATE);') || exit 25
[[ $checkpoint == 0\|* ]] || exit 26

verification=$(timeout 10s "$sqlite" "$db" <<'SQL'
.timeout 5000
PRAGMA foreign_keys=ON;
SELECT count(*) FROM alerts
WHERE datetime(created_at) IS NULL
   OR datetime(created_at) <= datetime('now', '-23 hours', '-30 minutes');
SELECT count(*) FROM decisions
WHERE until IS NULL
   OR datetime(created_at) IS NULL
   OR datetime(until) IS NULL
   OR datetime(until) > datetime(created_at, '+22 hours')
   OR datetime(created_at) <= datetime('now', '-23 hours', '-30 minutes');
SELECT count(*) FROM events
WHERE datetime(created_at) IS NULL
   OR datetime(created_at) <= datetime('now', '-23 hours', '-30 minutes')
   OR datetime(time) IS NULL
   OR datetime(time) <= datetime('now', '-23 hours', '-30 minutes');
SELECT count(*) FROM meta
WHERE datetime(created_at) IS NULL
   OR datetime(created_at) <= datetime('now', '-23 hours', '-30 minutes');
SELECT count(*) FROM pragma_foreign_key_check;
SQL
) || exit 27
[[ $verification == $'0\n0\n0\n0\n0' ]] || exit 28

if ((EUID == 0)); then
  [[ -d $feed_dir && ! -L $feed_dir ]] || exit 29
  [[ $(stat -c '%u:%g:%a' "$feed_dir") == 10001:10001:700 ]] || exit 29
  for active in serving maintenance serving-bouncer maintenance-bouncer; do
    path=$feed_dir/$active.json
    [[ -f $path && ! -L $path ]] || exit 29
    [[ $(stat -c '%u:%g:%a' "$path") == 10001:10001:600 ]] || exit 29
  done
fi
renew_lease
