#!/usr/bin/env bash
# Hosted-only proof against the installed CrowdSec 1.8.1 package.
set -euo pipefail
trap 'echo "CrowdSec integration failed at line $LINENO" >&2' ERR

((EUID == 0)) || { echo "run as root" >&2; exit 1; }
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
edge=$repo_root/deploy/vn/edge/crowdsec
feed=/run/aboutme/caddy-log/crowdsec/serving.json

cleanup() {
  systemctl stop crowdsec.service >/dev/null 2>&1 || true
}
trap cleanup EXIT

crowdsec -version 2>&1 | grep -qF 'v1.8.1' || {
  echo "CrowdSec is not v1.8.1" >&2
  exit 1
}

install -d -m 0755 /etc/crowdsec/acquis.d /etc/crowdsec/parsers/s01-parse \
  /etc/crowdsec/parsers/s02-enrich /etc/crowdsec/scenarios
: >/etc/crowdsec/acquis.yaml
find /etc/crowdsec/acquis.d -maxdepth 1 -type f -delete
cscli collections install crowdsecurity/sshd >/dev/null
rm -f /etc/crowdsec/scenarios/ssh-time-based-bf.yaml
"$repo_root/deploy/vn/host/crowdsec-window-check.sh" >/dev/null
find /etc/crowdsec/parsers/s01-parse -maxdepth 1 -type f -delete
find /etc/crowdsec/scenarios -maxdepth 1 -type f -delete
install -m 0644 "$edge/config.yaml.local" /etc/crowdsec/config.yaml.local
install -m 0644 "$edge/console.yaml" /etc/crowdsec/console.yaml
install -m 0644 "$edge/profiles.yaml" /etc/crowdsec/profiles.yaml
install -m 0644 "$edge/acquis.d/aboutme-http.yaml" /etc/crowdsec/acquis.d/aboutme-http.yaml
install -m 0644 "$edge/parsers/s01-parse/aboutme-http.yaml" \
  /etc/crowdsec/parsers/s01-parse/aboutme-http.yaml
install -m 0644 "$edge/parsers/s02-enrich/zz-aboutme-http-source.yaml" \
  /etc/crowdsec/parsers/s02-enrich/zz-aboutme-http-source.yaml
install -m 0644 "$edge/scenarios/aboutme-http-rate.yaml" \
  /etc/crowdsec/scenarios/aboutme-http-rate.yaml
install -m 0644 "$edge/scenarios/aboutme-http-status.yaml" \
  /etc/crowdsec/scenarios/aboutme-http-status.yaml

# shellcheck source=../host/crowdsec-offline.sh
source "$repo_root/deploy/vn/host/crowdsec-offline.sh"
crowdsec_remove_online /etc/crowdsec/config.yaml /etc/crowdsec/config.yaml.local \
  /etc/crowdsec/online_api_credentials.yaml
sed -i 's|^url: .*|url: http://127.0.0.1:8095|' /etc/crowdsec/local_api_credentials.yaml

install -d -o 10001 -g 10001 -m 0700 /run/aboutme/caddy-log/crowdsec
install -o 10001 -g 10001 -m 0600 /dev/null "$feed"
install -o 10001 -g 10001 -m 0600 /dev/null /run/aboutme/caddy-log/crowdsec/maintenance.json
install -o 10001 -g 10001 -m 0600 /dev/null /run/aboutme/caddy-log/crowdsec/serving-bouncer.json
install -o 10001 -g 10001 -m 0600 /dev/null /run/aboutme/caddy-log/crowdsec/maintenance-bouncer.json

crowdsec -t >/dev/null
systemctl unmask crowdsec.service >/dev/null
systemctl start crowdsec.service

append_records() { # ip status count route [method]
  local ip=$1 status=$2 count=$3 route=$4 method=${5:-GET} i ts
  for ((i = 0; i < count; i++)); do
    ts=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)
    printf '{"ts":"%s","source_ip":"%s","method":"%s","status":%s,"route_class":"%s"}\n' \
      "$ts" "$ip" "$method" "$status" "$route" >>"$feed"
  done
}

wait_for_decision() { # ip
  local ip=$1 attempt decisions
  for ((attempt = 0; attempt < 30; attempt++)); do
    decisions=$(cscli decisions list -o json)
    if jq -e --arg ip "$ip" '.[] | select(.value == $ip and .scenario == "aboutme/http-status")' \
      >/dev/null <<<"$decisions"; then
      jq -r --arg ip "$ip" '.[] | select(.value == $ip and .scenario == "aboutme/http-status") | .until' \
        <<<"$decisions" | head -n1
      return 0
    fi
    sleep 1
  done
  return 1
}

benign_ip=192.0.2.40
append_records "$benign_ip" 503 20 maintenance
sleep 3
if cscli decisions list -o json | jq -e --arg ip "$benign_ip" '.[] | select(.value == $ip)' >/dev/null; then
  echo "maintenance 503 created a decision" >&2
  exit 1
fi

unknown_ip=192.0.2.39
append_records "$unknown_ip" 200 11 unknown
wait_for_decision "$unknown_ip" >/dev/null || {
  echo "unknown route scanning created no decision" >&2
  exit 1
}

mixed_ip=192.0.2.38
append_records "$mixed_ip" 400 4 static GET
append_records "$mixed_ip" 401 4 static POST
append_records "$mixed_ip" 403 3 static HEAD
wait_for_decision "$mixed_ip" >/dev/null || {
  echo "mixed failure scanning created no decision" >&2
  exit 1
}

attack_ip=192.0.2.41
append_records "$attack_ip" 404 11 static
until=$(wait_for_decision "$attack_ip") || { echo "status scenario created no decision" >&2; exit 1; }
now_epoch=$(date +%s)
until_epoch=$(date -d "$until" +%s)
remaining=$((until_epoch - now_epoch))
((remaining >= 78600 && remaining <= 79260)) || {
  echo "HTTP decision duration is not 22 hours" >&2
  exit 1
}

db=/var/lib/crowdsec/data/crowdsec.db
[[ $(sqlite3 "$db" "SELECT count(*) FROM alerts WHERE source_ip = '$attack_ip' AND (coalesce(source_country, '') != '' OR coalesce(source_as_number, '') NOT IN ('', '0') OR coalesce(source_as_name, '') != '' OR coalesce(source_latitude, '') NOT IN ('', '0') OR coalesce(source_longitude, '') NOT IN ('', '0') OR coalesce(source_range, '') != '');") == 0 ]] || {
  echo "HTTP alert contains network enrichment" >&2
  exit 1
}
[[ $(sqlite3 "$db" "SELECT count(*) FROM meta WHERE alert_metas IN (SELECT id FROM alerts WHERE source_ip = '$attack_ip') AND key IN ('source_as_number', 'source_as_name', 'source_country', 'source_latitude', 'source_longitude', 'source_range');") == 0 ]] || {
  echo "HTTP metadata contains network enrichment" >&2
  exit 1
}
[[ $(sqlite3 "$db" "SELECT count(*) FROM events WHERE alert_events IN (SELECT id FROM alerts WHERE source_ip = '$attack_ip') AND (serialized LIKE '%source_as_number%' OR serialized LIKE '%source_as_name%' OR serialized LIKE '%source_country%' OR serialized LIKE '%source_latitude%' OR serialized LIKE '%source_longitude%' OR serialized LIKE '%source_range%');") == 0 ]] || {
  echo "HTTP event contains network enrichment" >&2
  exit 1
}
if ss -Htpn | grep -F crowdsec | grep -qvE '127\.0\.0\.1:[0-9]+[[:space:]]+127\.0\.0\.1:[0-9]+'; then
  echo "CrowdSec has a non-local network connection" >&2
  exit 1
fi

# The host retention job truncates the active file every five minutes. A new
# record must still reach the parser after the same operation.
: >"$feed"
second_ip=192.0.2.42
append_records "$second_ip" 404 11 static
wait_for_decision "$second_ip" >/dev/null || {
  echo "parser did not continue after active-feed truncation" >&2
  exit 1
}

old_child_ip=192.0.2.44
append_records "$old_child_ip" 404 11 static
wait_for_decision "$old_child_ip" >/dev/null || {
  echo "old-child fixture created no decision" >&2
  exit 1
}
fresh_ip=192.0.2.45
append_records "$fresh_ip" 404 11 static
wait_for_decision "$fresh_ip" >/dev/null || {
  echo "fresh fixture created no decision" >&2
  exit 1
}

# Exercise the retention script against CrowdSec's real v1.8.1 schema. One
# HTTP graph and one graph relabeled as SSH are old. A fresh parent with an old
# child event is also removed. A fourth HTTP graph and all credentials remain.
cscli bouncers delete retention-proof >/dev/null 2>&1 || true
cscli bouncers add retention-proof --key fixed-safe-test-key >/dev/null
systemctl stop crowdsec.service
machine_digest=$(sqlite3 "$db" 'SELECT machine_id, password FROM machines ORDER BY id;' | sha256sum | cut -d' ' -f1)
sqlite3 "$db" <<'SQL'
PRAGMA foreign_keys=ON;
UPDATE alerts SET created_at = datetime('now', '-23 hours', '-31 minutes')
WHERE source_ip = '192.0.2.41';
UPDATE decisions SET created_at = datetime('now', '-23 hours', '-31 minutes'), until = datetime('now', '-1 minute')
WHERE value = '192.0.2.41';
UPDATE events SET created_at = datetime('now', '-23 hours', '-31 minutes')
WHERE alert_events IN (SELECT id FROM alerts WHERE source_ip = '192.0.2.41');
UPDATE meta SET created_at = datetime('now', '-23 hours', '-31 minutes')
WHERE alert_metas IN (SELECT id FROM alerts WHERE source_ip = '192.0.2.41');
UPDATE alerts SET created_at = datetime('now', '-23 hours', '-31 minutes'),
  scenario = 'crowdsecurity/ssh-bf'
WHERE source_ip = '192.0.2.42';
UPDATE decisions SET created_at = datetime('now', '-23 hours', '-31 minutes'),
  until = datetime('now', '-1 minute'), scenario = 'crowdsecurity/ssh-bf'
WHERE value = '192.0.2.42';
UPDATE events SET created_at = datetime('now', '-23 hours', '-31 minutes'),
  time = datetime('now', '-23 hours', '-31 minutes')
WHERE alert_events IN (SELECT id FROM alerts WHERE source_ip = '192.0.2.42');
UPDATE meta SET created_at = datetime('now', '-23 hours', '-31 minutes')
WHERE alert_metas IN (SELECT id FROM alerts WHERE source_ip = '192.0.2.42');
UPDATE events SET time = datetime('now', '-23 hours', '-31 minutes')
WHERE alert_events IN (SELECT id FROM alerts WHERE source_ip = '192.0.2.44');
PRAGMA journal_mode=WAL;
SQL

"$repo_root/deploy/vn/host/crowdsec-retention.sh" startup
[[ $(sqlite3 "$db" "SELECT count(*) FROM alerts WHERE source_ip IN ('192.0.2.41', '192.0.2.42', '192.0.2.44');") == 0 ]] || {
  echo "startup retention kept an old graph" >&2
  exit 1
}
[[ $(sqlite3 "$db" "SELECT count(*) FROM alerts WHERE source_ip = '192.0.2.45';") == 1 ]] || {
  echo "startup retention removed a fresh alert" >&2
  exit 1
}
[[ $(sqlite3 "$db" "SELECT api_key FROM bouncers WHERE name = 'retention-proof';") == fixed-safe-test-key ]] || {
  echo "startup retention changed bouncer credentials" >&2
  exit 1
}
[[ $(sqlite3 "$db" 'SELECT machine_id, password FROM machines ORDER BY id;' | sha256sum | cut -d' ' -f1) == "$machine_digest" ]] || {
  echo "startup retention changed machine credentials" >&2
  exit 1
}
[[ ! -s $db-wal ]] || { echo "startup retention did not truncate the WAL" >&2; exit 1; }

systemctl start crowdsec.service
sqlite3 "$db" <<'SQL'
.timeout 5000
UPDATE alerts SET created_at = datetime('now', '-23 hours', '-31 minutes')
WHERE source_ip = '192.0.2.45';
UPDATE decisions SET created_at = datetime('now', '-23 hours', '-31 minutes'), until = datetime('now', '-1 minute')
WHERE value = '192.0.2.45';
UPDATE events SET created_at = datetime('now', '-23 hours', '-31 minutes')
WHERE alert_events IN (SELECT id FROM alerts WHERE source_ip = '192.0.2.45');
UPDATE meta SET created_at = datetime('now', '-23 hours', '-31 minutes')
WHERE alert_metas IN (SELECT id FROM alerts WHERE source_ip = '192.0.2.45');
SQL
"$repo_root/deploy/vn/host/crowdsec-retention.sh" runtime
[[ $(sqlite3 "$db" "SELECT count(*) FROM alerts WHERE source_ip = '192.0.2.45';") == 0 ]] || {
  echo "runtime retention kept an old alert" >&2
  exit 1
}
[[ $(sqlite3 "$db" "SELECT api_key FROM bouncers WHERE name = 'retention-proof';") == fixed-safe-test-key ]] || {
  echo "runtime retention changed bouncer credentials" >&2
  exit 1
}

[[ ! -e /etc/crowdsec/online_api_credentials.yaml ]] || {
  echo "central API credentials exist" >&2
  exit 1
}
! grep -RqE 'request_uri|request_body|http_user_agent|headers' \
  /etc/crowdsec/parsers/s01-parse/aboutme-http.yaml \
  /etc/crowdsec/scenarios/aboutme-http-*.yaml || {
  echo "custom CrowdSec config accepts a sensitive HTTP field" >&2
  exit 1
}

echo "CrowdSec 1.8.1 integration tests passed"
