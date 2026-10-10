#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
retention="$repo_root/deploy/vn/host/crowdsec-retention.sh"
failure="$repo_root/deploy/vn/host/crowdsec-retention-fail.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/feed" "$work/bin"

db=$work/crowdsec.db
sqlite3 "$db" <<'SQL'
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
CREATE TABLE alerts (
  id INTEGER PRIMARY KEY,
  created_at TEXT NOT NULL,
  scenario TEXT NOT NULL
);
CREATE TABLE decisions (
  id INTEGER PRIMARY KEY,
  created_at TEXT NOT NULL,
  until TEXT,
  scenario TEXT NOT NULL,
  alert_decisions INTEGER,
  FOREIGN KEY(alert_decisions) REFERENCES alerts(id) ON DELETE CASCADE
);
CREATE TABLE events (
  id INTEGER PRIMARY KEY,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  time TEXT NOT NULL,
  serialized TEXT NOT NULL,
  alert_events INTEGER,
  FOREIGN KEY(alert_events) REFERENCES alerts(id) ON DELETE CASCADE
);
CREATE TABLE meta (
  id INTEGER PRIMARY KEY,
  created_at TEXT NOT NULL,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  alert_metas INTEGER,
  FOREIGN KEY(alert_metas) REFERENCES alerts(id) ON DELETE CASCADE
);
CREATE TABLE bouncers (id INTEGER PRIMARY KEY, api_key TEXT NOT NULL);
INSERT INTO alerts VALUES
  (1, datetime('now', '-23 hours', '-31 minutes'), 'aboutme/http-status'),
  (2, datetime('now', '-1 hour'), 'aboutme/http-rate'),
  (3, datetime('now', '-23 hours', '-31 minutes'), 'crowdsecurity/ssh-bf'),
  (4, datetime('now', '-1 hour'), 'aboutme/http-status'),
  (5, 'invalid-time', 'aboutme/http-status'),
  (6, datetime('now', '-1 hour'), 'manual/test');
INSERT INTO decisions VALUES
  (1, datetime('now', '-23 hours', '-31 minutes'), datetime('now', '-1 minute'), 'aboutme/http-status', 1),
  (2, datetime('now', '-1 hour'), datetime('now', '+21 hours'), 'aboutme/http-rate', 2),
  (3, datetime('now', '-23 hours', '-31 minutes'), datetime('now', '-1 minute'), 'crowdsecurity/ssh-bf', 3),
  (4, datetime('now', '-1 hour'), 'invalid-time', 'aboutme/http-status', 5),
  (5, datetime('now', '-1 hour'), datetime('now', '+47 hours'), 'manual/test', 6);
INSERT INTO events VALUES
  (1, datetime('now', '-23 hours', '-31 minutes'), datetime('now'), datetime('now', '-23 hours', '-31 minutes'), '{"source_ip":"192.0.2.20"}', 1),
  (2, datetime('now', '-1 hour'), datetime('now'), datetime('now', '-1 hour'), '{"source_ip":"192.0.2.21"}', 2),
  (3, datetime('now', '-1 hour'), datetime('now'), datetime('now', '-1 hour'), 'orphan', NULL),
  (4, datetime('now', '-23 hours', '-31 minutes'), datetime('now'), datetime('now', '-23 hours', '-31 minutes'), 'old ssh event', 3),
  (5, datetime('now', '-1 hour'), datetime('now'), datetime('now', '-23 hours', '-31 minutes'), 'old source timestamp', 4),
  (6, datetime('now', '-1 hour'), datetime('now'), 'invalid-time', 'malformed timestamp', 5);
INSERT INTO meta VALUES
  (1, datetime('now', '-23 hours', '-31 minutes'), 'source_ip', '192.0.2.20', 1),
  (2, datetime('now', '-1 hour'), 'source_ip', '192.0.2.21', 2),
  (3, datetime('now', '-1 hour'), 'source_ip', 'orphan', NULL),
  (4, datetime('now', '-23 hours', '-31 minutes'), 'source_ip', '192.0.2.22', 3),
  (5, datetime('now', '-1 hour'), 'source_ip', '192.0.2.23', 5);
INSERT INTO bouncers VALUES (1, 'credential-must-remain');
SQL

printf 'old record\nnew record\n' >"$work/feed/serving.json"
: >"$work/feed/serving.json.1"
: >"$work/feed/maintenance.json"
: >"$work/feed/serving-bouncer.json"
: >"$work/feed/maintenance-bouncer.json"
mv "$work/feed/serving.json.1" "$work/feed/serving-2026-10-10T01-00-00.000-time.json"
: >"$work/feed/maintenance-bouncer-2026-10-10T01-00-00.000-size.json"
printf 'Filename\tType\tSize\tUsed\tPriority\n/dev/zram0 partition 1 0 100\n' >"$work/swaps"
cat >"$work/append-after-truncate.sh" <<EOF
#!/usr/bin/env bash
printf '%s\n' fresh-record >>'$work/feed/serving.json'
EOF
chmod +x "$work/append-after-truncate.sh"

ABOUTME_CROWDSEC_DB="$db" \
  ABOUTME_CROWDSEC_FEED_DIR="$work/feed" \
  ABOUTME_CROWDSEC_SWAPS="$work/swaps" \
  ABOUTME_CROWDSEC_SKIP_MOUNT_CHECK=1 \
  ABOUTME_CROWDSEC_SKIP_TIME_CHECK=1 \
  ABOUTME_CROWDSEC_AFTER_FEED_HOOK="$work/append-after-truncate.sh" \
  ABOUTME_RETENTION_SUCCESS_MARKER="$work/retention-ok" \
  "$retention" startup

[[ $(sqlite3 "$db" 'SELECT count(*) FROM alerts WHERE id = 1') == 0 ]] || {
  echo "old alert remains" >&2
  exit 1
}
[[ $(sqlite3 "$db" 'SELECT count(*) FROM decisions WHERE id IN (1, 3)') == 0 ]] || {
  echo "expired decision remains" >&2
  exit 1
}
[[ $(sqlite3 "$db" 'SELECT count(*) FROM decisions WHERE id IN (4, 5)') == 0 ]] || {
  echo "malformed or overlong decision remains" >&2
  exit 1
}
[[ $(sqlite3 "$db" 'SELECT count(*) FROM alerts WHERE id = 3') == 0 ]] || {
  echo "old SSH alert remains" >&2
  exit 1
}
[[ $(sqlite3 "$db" 'SELECT count(*) FROM alerts WHERE id = 4') == 0 ]] || {
  echo "fresh parent with old child event remains" >&2
  exit 1
}
[[ $(sqlite3 "$db" 'SELECT count(*) FROM alerts WHERE id = 5') == 0 ]] || {
  echo "malformed timestamp graph remains" >&2
  exit 1
}
[[ $(sqlite3 "$db" 'SELECT count(*) FROM events WHERE id IN (1, 3)') == 0 ]] || {
  echo "old or orphan event remains" >&2
  exit 1
}
[[ $(sqlite3 "$db" 'SELECT count(*) FROM meta WHERE id IN (1, 3)') == 0 ]] || {
  echo "old or orphan metadata remains" >&2
  exit 1
}
[[ $(sqlite3 "$db" 'SELECT count(*) FROM alerts WHERE id = 2') == 1 ]] || {
  echo "fresh alert was removed" >&2
  exit 1
}
[[ $(sqlite3 "$db" "SELECT api_key FROM bouncers WHERE id = 1") == credential-must-remain ]] || {
  echo "bouncer credential was changed" >&2
  exit 1
}
[[ ! -e $work/feed/serving-2026-10-10T01-00-00.000-time.json ]] || {
  echo "feed roll remains" >&2
  exit 1
}
[[ ! -e $work/feed/maintenance-bouncer-2026-10-10T01-00-00.000-size.json ]] || {
  echo "bouncer log roll remains" >&2
  exit 1
}
[[ -e $work/feed/serving.json && -e $work/feed/maintenance.json ]] || {
  echo "active feed was removed" >&2
  exit 1
}
[[ $(cat "$work/feed/serving.json") == fresh-record ]] || {
  echo "active feed did not preserve a concurrent fresh write" >&2
  exit 1
}
[[ ! -s $db-wal ]] || { echo "CrowdSec WAL was not truncated" >&2; exit 1; }

printf 'Filename\tType\tSize\tUsed\tPriority\n/swapfile file 1 0 -2\n' >"$work/swaps"
if ABOUTME_CROWDSEC_DB="$db" \
  ABOUTME_CROWDSEC_FEED_DIR="$work/feed" \
  ABOUTME_CROWDSEC_SWAPS="$work/swaps" \
  ABOUTME_CROWDSEC_SKIP_MOUNT_CHECK=1 \
  ABOUTME_CROWDSEC_SKIP_TIME_CHECK=1 \
  "$retention" startup >/dev/null 2>&1; then
  echo "disk swap was accepted" >&2
  exit 1
fi

cat >"$work/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$ABOUTME_TEST_SYSTEMCTL"
if [[ ${1-} == is-active && ${ABOUTME_TEST_STILL_ACTIVE:-0} == 1 ]]; then
  exit 0
fi
[[ ${1-} != is-active ]]
STUB
cat >"$work/bin/alert.sh" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$ABOUTME_TEST_ALERT"
STUB
chmod +x "$work/bin/systemctl" "$work/bin/alert.sh"
ABOUTME_RETENTION_SYSTEMCTL="$work/bin/systemctl" \
  ABOUTME_RETENTION_ALERT="$work/bin/alert.sh" \
  ABOUTME_RETENTION_FAILURE_MARKER="$work/failure" \
  ABOUTME_RETENTION_FEED_DIR="$work/feed" \
  ABOUTME_RETENTION_SUCCESS_MARKER="$work/retention-ok" \
  ABOUTME_TEST_SYSTEMCTL="$work/systemctl.log" \
  ABOUTME_TEST_ALERT="$work/alert.log" \
  "$failure"
grep -qxF 'stop aboutme-caddy.service aboutme-maintenance.service crowdsec-firewall-bouncer.service crowdsec.service aboutme-crowdsec-retention-startup.service' \
  "$work/systemctl.log" || { echo "retention failure did not stop the edge" >&2; exit 1; }
[[ $(cat "$work/failure") == retention-failed ]] || { echo "failure marker is not fixed" >&2; exit 1; }
grep -qxF 'crowdsec-retention CrowdSec retention failed' "$work/alert.log" || {
  echo "retention failure alert is not fixed" >&2
  exit 1
}
[[ ! -e $work/feed ]] || { echo "retention failure kept the tmpfs feed" >&2; exit 1; }

mkdir -p "$work/feed"
if ABOUTME_RETENTION_SYSTEMCTL="$work/bin/systemctl" \
  ABOUTME_RETENTION_ALERT="$work/bin/alert.sh" \
  ABOUTME_RETENTION_FAILURE_MARKER="$work/failure" \
  ABOUTME_RETENTION_FEED_DIR="$work/feed" \
  ABOUTME_RETENTION_SUCCESS_MARKER="$work/retention-ok" \
  ABOUTME_TEST_SYSTEMCTL="$work/systemctl.log" \
  ABOUTME_TEST_ALERT="$work/alert.log" \
  ABOUTME_TEST_STILL_ACTIVE=1 \
  "$failure"; then
  echo "retention failure accepted an active consumer" >&2
  exit 1
fi
grep -qF 'kill --kill-whom=all --signal=SIGKILL' "$work/systemctl.log" || {
  echo "retention failure did not kill a consumer that stayed active" >&2
  exit 1
}

echo "CrowdSec retention tests passed"
