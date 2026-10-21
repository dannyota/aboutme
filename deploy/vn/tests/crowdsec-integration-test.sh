#!/usr/bin/env bash
# Hosted-only PostgreSQL and real pinned-consumer proof in private namespaces.
set -euo pipefail
trap 'echo "FAIL crowdsec-integration" >&2' ERR

((EUID == 0)) || { echo "run as root" >&2; exit 1; }
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
edge=$repo_root/deploy/vn/edge/crowdsec
feed=/run/aboutme/caddy-log/crowdsec/serving.json
# shellcheck source=../host/crowdsec-offline.sh
source "$repo_root/deploy/vn/host/crowdsec-offline.sh"
# shellcheck source=crowdsec-retention-fixture.sh
source "$repo_root/deploy/vn/tests/crowdsec-retention-fixture.sh"
retention_fixture_start "$repo_root"
retention_fixture_consumers
# A second real pinned listener exercises maintenance's independent bouncer
# cache. Process control remains the existing explicit namespace adapter.
sed -e 's/:18080 {/:18081 {/' -e 's/respond "allowed" 200/respond "maintenance" 503/' \
  "$retention_fixture/Caddyfile" >"$retention_fixture/Caddyfile.maintenance"
python3 -I - "$retention_fixture/control" <<'PYCONTROL'
import pathlib,sys
path=pathlib.Path(sys.argv[1]); text=path.read_text()
old='aboutme-maintenance.service) exit 1 ;;'
assert text.count(old)==1
text=text.replace(old,'''aboutme-maintenance.service)
          spawn env XDG_CONFIG_HOME="$fixture/maintenance-state" XDG_DATA_HOME="$fixture/maintenance-state" \
            "$fixture/bin/caddy" run --config "$fixture/Caddyfile.maintenance" --adapter caddyfile ;;''')
path.write_text(text)
PYCONTROL
sql() {
  /usr/local/lib/aboutme/crowdsec-db.sh preflight-ready </dev/null >/dev/null 2>&1
  printf '%s\n' "$1" | retention_fixture_sql
}
crowdsec_assert_postgresql

# A positive vendor call distinguishes gate refusal from a broken CLI. Trace
# only private, fixed arguments and prove refusal precedes vendor execution.
crowdsec_admitted_cli bouncers list -o json >/dev/null 2>&1
for fault in schema statistics; do
  if [[ $fault == schema ]]; then
    retention_fixture_sql <<<'ALTER TABLE public.events RENAME COLUMN serialized TO unavailable;' >/dev/null
  else
    retention_fixture_sql <<<'ALTER TABLE public.alerts ALTER COLUMN source_ip SET STATISTICS 1;' >/dev/null
  fi
  if strace -f -e trace=execve -o "$retention_fixture/vendor-trace" bash -c \
    'source "$1"; crowdsec_admitted_cli bouncers list -o json' proof \
    "$repo_root/deploy/vn/host/crowdsec-offline.sh" >/dev/null 2>&1; then
    echo 'FAIL invalid-database-helper-admission'; exit 1
  fi
  if grep -qF 'execve("/usr/bin/cscli"' "$retention_fixture/vendor-trace"; then
    echo 'FAIL vendor-ran-before-admission'; exit 1
  fi
  if [[ $fault == schema ]]; then
    retention_fixture_sql <<<'ALTER TABLE public.events RENAME COLUMN unavailable TO serialized;' >/dev/null
  else
    retention_fixture_sql <<<'ALTER TABLE public.alerts ALTER COLUMN source_ip SET STATISTICS 0;' >/dev/null
  fi
  crowdsec_admitted_cli bouncers list -o json >/dev/null 2>&1
done

crowdsec_configure_local_detection
[[ $(crowdsec_admitted_cli collections list -o json | jq -r '.collections | map(.name) | join("\n")') == \
  crowdsecurity/sshd ]] || { echo "unexpected CrowdSec collection is enabled" >&2; exit 1; }
"$repo_root/deploy/vn/host/crowdsec-window-check.sh" >/dev/null
install -m 0644 "$edge/config.yaml.local" /etc/crowdsec/config.yaml.local
install -m 0644 "$edge/console.yaml" /etc/crowdsec/console.yaml
install -m 0644 "$edge/profiles.yaml" /etc/crowdsec/profiles.yaml
install -m 0644 "$edge/acquis.d/aboutme-http.yaml" /etc/crowdsec/acquis.d/aboutme-http.yaml
install -m 0644 "$edge/acquis.d/sshd.yaml" /etc/crowdsec/acquis.d/sshd.yaml
install -m 0644 "$edge/parsers/s01-parse/aboutme-http.yaml" \
  /etc/crowdsec/parsers/s01-parse/aboutme-http.yaml
install -d -m 0755 /etc/crowdsec/parsers/s02-enrich
install -m 0644 "$edge/parsers/s02-enrich/zz-aboutme-http-source.yaml" \
  /etc/crowdsec/parsers/s02-enrich/zz-aboutme-http-source.yaml
install -m 0644 "$edge/scenarios/aboutme-http-rate.yaml" \
  /etc/crowdsec/scenarios/aboutme-http-rate.yaml
install -m 0644 "$edge/scenarios/aboutme-http-status.yaml" \
  /etc/crowdsec/scenarios/aboutme-http-status.yaml
[[ $(find /etc/crowdsec/acquis.d -maxdepth 1 -type f -printf '%f\n' | sort) == \
  $'aboutme-http.yaml\nsshd.yaml' ]] || { echo "unexpected CrowdSec acquisition is enabled" >&2; exit 1; }
"$repo_root/deploy/vn/host/crowdsec-window-check.sh" >/dev/null

crowdsec_remove_online /etc/crowdsec/config.yaml /etc/crowdsec/config.yaml.local \
  /etc/crowdsec/online_api_credentials.yaml
sed -i 's|^url: .*|url: http://127.0.0.1:8095|' /etc/crowdsec/local_api_credentials.yaml
crowdsec_admitted_cli machines add -a --force --error >/dev/null 2>&1
umask 077
crowdsec_admitted_cli bouncers add aboutme-firewall -o raw >"$retention_fixture/firewall.key" 2>/dev/null
crowdsec_admitted_cli bouncers add aboutme-caddy -o raw >"$retention_fixture/caddy.key" 2>/dev/null
install -d -m 0700 /etc/crowdsec/bouncers
cat "$edge/bouncers/crowdsec-firewall-bouncer.yaml.local" > /etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local
printf 'api_key: %s\n' "$(<"$retention_fixture/firewall.key")" >>/etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local
# Package hooks see the managed effective key before configuration. Package
# output and package logs have no durable or public sink.
strace -f -e trace=execve -o "$retention_fixture/package-exec" bash -c '
  source "$1"
  crowdsec_install_offline apt-get -o Dir::Log::Terminal=/dev/null -o Dir::Log::History=/dev/null \
    -o DPkg::Options::=--log=/dev/null install -y -qq crowdsec-firewall-bouncer-nftables=0.0.36
' proof "$repo_root/deploy/vn/host/crowdsec-offline.sh" >/dev/null 2>&1
[[ $(dpkg-query -W -f='${Status}\t${Version}' crowdsec-firewall-bouncer-nftables) == $'install ok installed\t0.0.36' ]]
[[ ! -e /etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.id ]]
[[ $(sql 'SELECT count(*) FROM bouncers;') == 2 ]]
/usr/bin/crowdsec-firewall-bouncer -c /etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml -T \
  >"$retention_fixture/bouncer-effective" 2>/dev/null
python3 -I - "$retention_fixture/bouncer-effective" "$retention_fixture/firewall.key" \
  "$retention_fixture/package-exec" >/dev/null 2>&1 <<'PY'
import pathlib,sys,yaml
effective=yaml.safe_load(pathlib.Path(sys.argv[1]).read_text())
assert effective['api_key']==pathlib.Path(sys.argv[2]).read_text().strip()
assert 'API_KEY' not in pathlib.Path(sys.argv[1]).read_text()
assert all(effective['nftables'][family]['set-only'] for family in ('ipv4','ipv6'))
trace=pathlib.Path(sys.argv[3]).read_text()
assert effective['api_key'] not in trace and 'API_KEY=' not in trace
PY

integration_body() {
integration_stage=consumers
trap 'printf "FAIL real-consumer-proof %s\n" "$integration_stage" >&2' ERR
ip link set lo up
install -d -o 10001 -g 10001 -m 0700 /run/aboutme/caddy-log/crowdsec
install -o 10001 -g 10001 -m 0600 /dev/null "$feed"
install -o 10001 -g 10001 -m 0600 /dev/null /run/aboutme/caddy-log/crowdsec/maintenance.json
install -o 10001 -g 10001 -m 0600 /dev/null /run/aboutme/caddy-log/crowdsec/serving-bouncer.json
install -o 10001 -g 10001 -m 0600 /dev/null /run/aboutme/caddy-log/crowdsec/maintenance-bouncer.json

/usr/local/lib/aboutme/crowdsec-db.sh preflight-ready </dev/null >/dev/null 2>&1
crowdsec -t >/dev/null 2>&1
/usr/local/lib/aboutme/crowdsec-retention.sh startup
systemctl start crowdsec.service
systemctl start crowdsec-firewall-bouncer.service aboutme-caddy.service

append_records() { # ip status count route [method] [probe] [target]
  local ip=$1 status=$2 count=$3 route=$4 method=${5:-GET} probe=${6:-none} target=${7:-} i ts target_json destination=$feed
  [[ $route != maintenance ]] || destination=/run/aboutme/caddy-log/crowdsec/maintenance.json
  target_json=$(jq -cn --arg value "$target" '$value')
  for ((i = 0; i < count; i++)); do
    ts=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)
    printf '{"ts":"%s","source_ip":"%s","method":"%s","status":%s,"route_class":"%s","probe_class":"%s","target_path":%s}\n' \
      "$ts" "$ip" "$method" "$status" "$route" "$probe" "$target_json" >>"$destination"
  done
}

for scenario in aboutme-http-rate aboutme-http-status; do
  if grep -qF target_path "$edge/scenarios/$scenario.yaml"; then
    echo "scenario-target-grouping" >&2
    exit 1
  fi
done

# Each invalid shape has its own source and enough scanner events to overflow
# a bucket if the parser accepts it. Diagnostics report fixed labels only.
printf '{"malformed":"parserdiagnosticmarker4c8a",\n' >>"$feed"
rejected_ips=()
rejection_filters=(
  'del(.probe_class, .target_path)'
  'del(.probe_class)'
  'del(.target_path)'
  '.extra = "discarded"'
  '.target_path = null'
  '.target_path = 7'
  '.probe_class = "unrecognized"'
  '.route_class = "unrecognized"'
  '.method = "TRACE"'
  '.status = "404"'
  '.status = 404.5'
  '.status = 99'
  '.status = 600'
  '.probe_class = "none"'
  '.route_class = "api"'
  '.status = 200'
  '.target_path = ""'
  '.probe_class = "repository"'
  '.target_path = "/api/private"'
  '.target_path = "/%2561pi/private"'
  '.target_path = "/unrecognized/%252e%252e/%2561pi/private"'
  '.target_path = "/%2541PP/private"'
  '.target_path = "/unknown/%25a"'
  '.target_path = "/unknown/%0a"'
  '.target_path = "/unknown/%C2%85"'
  '.target_path = "/unknown/%ed%a0%80"'
  '.target_path = "/unknown/%ff"'
  '.target_path = "/unknown/%zz"'
  '.target_path = "/unknown/sample?token=discarded"'
  '.target_path = "/unknown/" + ("x" * 504)'
  '.probe_class = "invalid"'
  '.probe_class = "oversized"'
  '.ts = "2026-02-30T00:00:00Z"'
  '.target_path = "/unknown/%252541pi/private" | .probe_class = "repository"'
)
for index in "${!rejection_filters[@]}"; do
  ip=192.0.2.$((100 + index))
  rejected_ips+=("$ip")
  ts=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)
  record=$(jq -cn --arg ts "$ts" --arg ip "$ip" \
    '{ts:$ts,source_ip:$ip,method:"GET",status:404,route_class:"unknown",probe_class:"other",target_path:"/unknown/sample"}' |
    jq -c "${rejection_filters[$index]}")
  for _ in $(seq 21); do
    printf '%s\n' "$record" >>"$feed"
  done
done
duplicate_ip=192.0.2.149
rejected_ips+=("$duplicate_ip")
for _ in $(seq 21); do
  ts=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)
  printf '{"ts":"%s","source_ip":"%s","method":"GET","status":404,"route_class":"unknown","probe_class":"other","target_path":"/unknown/sample","target_path":"/unknown/duplicate"}\n' \
    "$ts" "$duplicate_ip" >>"$feed"
done

wait_for_decision() { # ip
  local ip=$1 attempt decisions
  for ((attempt = 0; attempt < 30; attempt++)); do
    decisions=$(crowdsec_admitted_cli decisions list -o json)
    if jq -e --arg ip "$ip" \
      '.[] | .decisions[] | select(.value == $ip and .scenario == "aboutme/http-status")' \
      >/dev/null <<<"$decisions"; then
      return 0
    fi
    sleep 1
  done
  return 1
}

benign_ip=192.0.2.40
append_records "$benign_ip" 503 20 maintenance
sleep 3
if crowdsec_admitted_cli decisions list -o json | \
  jq -e --arg ip "$benign_ip" '.[] | .decisions[] | select(.value == $ip)' >/dev/null; then
  echo "maintenance 503 created a decision" >&2
  exit 1
fi

unknown_ip=192.0.2.39
append_records "$unknown_ip" 200 21 unknown
wait_for_decision "$unknown_ip" >/dev/null || {
  echo "unknown route scanning created no decision" >&2
  exit 1
}

mixed_ip=192.0.2.38
append_records "$mixed_ip" 400 10 static GET
append_records "$mixed_ip" 405 11 static HEAD
wait_for_decision "$mixed_ip" >/dev/null || {
  echo "mixed failure scanning created no decision" >&2
  exit 1
}

eligible_ip=192.0.2.43
append_records "$eligible_ip" 404 21 unknown GET other '/unknown/eligible%2fmarker'
wait_for_decision "$eligible_ip" >/dev/null || {
  echo "eligible-target-created-no-decision" >&2
  exit 1
}
for ip in "${rejected_ips[@]}"; do
  if crowdsec_admitted_cli decisions list -o json |
    jq -e --arg ip "$ip" '.[] | .decisions[] | select(.value == $ip)' >/dev/null; then
    echo "parser-invalid-feed-accepted" >&2
    exit 1
  fi
done
[[ $(sql "SELECT count(*) FROM events WHERE alert_events IN (SELECT id FROM alerts WHERE source_ip = '$eligible_ip') AND position('/unknown/eligible%2fmarker' in serialized) > 0;") -gt 0 ]] || {
  echo "eligible-target-sample-missing" >&2
  exit 1
}

valid_targets=(
  'traversal|/unknown/%252e%252e/admin'
  'secret-config|/unknown/config.json'
  'repository|/.git/config'
  'web-shell|/unknown/shell.php'
  'framework|/wp-admin/index'
  'other|/unknown/caf%c3%a9'
  'other|/unknown/a%3Fb'
  'other|/unknown/%25252egit'
  'invalid|'
  'oversized|'
  "other|/unknown/$(printf '%0503d' 0)"
)
for index in "${!valid_targets[@]}"; do
  ip=192.0.2.$((60 + index))
  IFS='|' read -r probe target <<<"${valid_targets[$index]}"
  append_records "$ip" 404 21 unknown GET "$probe" "$target"
  wait_for_decision "$ip" >/dev/null || { echo "valid-probe-rejected" >&2; exit 1; }
  if [[ -n $target ]]; then
    [[ $(sql "SELECT count(*) FROM events WHERE alert_events IN (SELECT id FROM alerts WHERE source_ip = '$ip') AND position('$target' in serialized) > 0;") -gt 0 ]] || {
      echo "valid-probe-sample-missing" >&2
      exit 1
    }
  fi
done
parser_logs=$(find /run/aboutme/crowdsec -maxdepth 1 -type f -name '*.log' -exec cat {} +)
if grep -qE 'parserdiagnosticmarker4c8a|eligible%2fmarker|/unknown/sample\?token=discarded' <<<"$parser_logs"; then
  echo "parser-raw-diagnostic" >&2
  exit 1
fi

# A shared address sends wrong passwords, throttled requests, and the
# bouncer's 403s; none of them may create a ban.
shared_ip=192.0.2.50
append_records "$shared_ip" 401 15 auth POST
append_records "$shared_ip" 403 15 static GET
append_records "$shared_ip" 429 15 api POST

attack_ip=192.0.2.41
append_records "$attack_ip" 404 21 static
wait_for_decision "$attack_ip" || { echo "status scenario created no decision" >&2; exit 1; }
denied=$(sql "SELECT count(*) FROM decisions WHERE value = '$shared_ip';")
[[ $denied == 0 ]] || { echo "401, 403, or 429 responses created a decision" >&2; exit 1; }
until=$(sql "SELECT until FROM decisions WHERE value = '$attack_ip' AND scenario = 'aboutme/http-status' ORDER BY id DESC LIMIT 1;")
[[ -n $until ]] || { echo "HTTP decision expiry is missing" >&2; exit 1; }
now_epoch=$(date +%s)
until_epoch=$(date -d "$until" +%s)
remaining=$((until_epoch - now_epoch))
((remaining >= 780 && remaining <= 900)) || {
  echo "a first HTTP decision does not last 15 minutes" >&2
  exit 1
}
# A second offense from the same address escalates to one hour. It comes
# from another route class: the first bucket is blackholed for five minutes.
append_records "$attack_ip" 404 21 public
for _ in $(seq 60); do
  [[ $(sql "SELECT count(*) FROM decisions WHERE value = '$attack_ip' AND scenario = 'aboutme/http-status';") -ge 2 ]] && break
  sleep 1
done
until=$(sql "SELECT until FROM decisions WHERE value = '$attack_ip' AND scenario = 'aboutme/http-status' ORDER BY id DESC LIMIT 1;")
remaining=$(( $(date -d "$until" +%s) - $(date +%s) ))
((remaining >= 3480 && remaining <= 3600)) || {
  echo "a second HTTP decision does not last one hour" >&2
  exit 1
}

[[ $(sql "SELECT count(*) FROM alerts WHERE source_ip = '$attack_ip' AND (coalesce(source_country, '') != '' OR coalesce(source_as_number, '') NOT IN ('', '0') OR coalesce(source_as_name, '') != '' OR coalesce(source_latitude, 0.0) != 0.0 OR coalesce(source_longitude, 0.0) != 0.0 OR coalesce(source_range, '') != '');") == 0 ]] || {
  echo "HTTP alert contains network enrichment" >&2
  exit 1
}
[[ $(sql "SELECT count(*) FROM meta WHERE alert_metas IN (SELECT id FROM alerts WHERE source_ip = '$attack_ip') AND key IN ('source_as_number', 'source_as_name', 'source_country', 'source_latitude', 'source_longitude', 'source_range');") == 0 ]] || {
  echo "HTTP metadata contains network enrichment" >&2
  exit 1
}
[[ $(sql "SELECT count(*) FROM events WHERE alert_events IN (SELECT id FROM alerts WHERE source_ip = '$attack_ip') AND (serialized LIKE '%source_as_number%' OR serialized LIKE '%source_as_name%' OR serialized LIKE '%source_country%' OR serialized LIKE '%source_latitude%' OR serialized LIKE '%source_longitude%' OR serialized LIKE '%source_range%');") == 0 ]] || {
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
append_records "$second_ip" 404 21 static
wait_for_decision "$second_ip" >/dev/null || {
  echo "parser did not continue after active-feed truncation" >&2
  exit 1
}

old_child_ip=192.0.2.44
append_records "$old_child_ip" 404 21 static
wait_for_decision "$old_child_ip" >/dev/null || {
  echo "old-child fixture created no decision" >&2
  exit 1
}
fresh_ip=192.0.2.45
append_records "$fresh_ip" 404 21 static
wait_for_decision "$fresh_ip" >/dev/null || {
  echo "fresh fixture created no decision" >&2
  exit 1
}

# These are real HTTP and kernel checks, not stream or last-pull assertions.
http_status() { curl --noproxy '*' -s -o /dev/null -w '%{http_code}' --max-time 2 --interface "$1" "$2"; }
wait_status() {
  local want=$1 source=$2 url=$3 result attempt
  for ((attempt=0; attempt<20; attempt++)); do
    result=$(http_status "$source" "$url" 2>/dev/null) || result=timeout
    [[ $result != "$want" ]] || return 0
    sleep 1
  done
  return 1
}
ip addr add 192.0.2.41/32 dev lo >/dev/null 2>&1
ip addr add 192.0.2.45/32 dev lo >/dev/null 2>&1
ip addr add 192.0.2.80/32 dev lo >/dev/null 2>&1
ip -6 addr add 2001:db8::80/128 dev lo >/dev/null 2>&1
ip addr add 192.0.2.81/32 dev lo >/dev/null 2>&1
ip -6 addr add 2001:db8::81/128 dev lo >/dev/null 2>&1
wait_status 403 192.0.2.41 http://127.0.0.1:18080/
wait_status 403 192.0.2.45 http://127.0.0.1:18080/
systemctl start aboutme-maintenance.service
wait_status 403 192.0.2.41 http://127.0.0.1:18081/
wait_status 403 192.0.2.45 http://127.0.0.1:18081/
sql "INSERT INTO alerts (id,created_at,updated_at,started_at,stopped_at,scenario,source_ip,source_scope,source_value,capacity,events_count,leak_speed,simulated) VALUES
 (90000,now(),now(),now(),now(),'crowdsecurity/ssh-bf','192.0.2.80','Ip','192.0.2.80',1,1,'1s',false),
 (90001,now(),now(),now(),now(),'crowdsecurity/ssh-bf','2001:db8::80','Ip','2001:db8::80',1,1,'1s',false),
 (90002,now(),now(),now(),now(),'crowdsecurity/ssh-bf','192.0.2.81','Ip','192.0.2.81',1,1,'1s',false),
 (90003,now(),now(),now(),now(),'crowdsecurity/ssh-bf','2001:db8::81','Ip','2001:db8::81',1,1,'1s',false);
 INSERT INTO decisions (created_at,updated_at,until,scenario,type,scope,value,origin,alert_decisions,simulated) VALUES
 (now(),now(),now()+interval '1 hour','crowdsecurity/ssh-bf','ban','Ip','192.0.2.80','crowdsec',90000,false),
 (now(),now(),now()+interval '1 hour','crowdsecurity/ssh-bf','ban','Ip','2001:db8::80','crowdsec',90001,false),
 (now(),now(),now()+interval '1 hour','crowdsecurity/ssh-bf','ban','Ip','192.0.2.81','crowdsec',90002,false),
 (now(),now(),now()+interval '1 hour','crowdsecurity/ssh-bf','ban','Ip','2001:db8::81','crowdsec',90003,false);" >/dev/null
wait_status timeout 192.0.2.80 http://127.0.0.1:18080/
wait_status timeout 2001:db8::80 'http://[::1]:18080/'
wait_status timeout 192.0.2.81 http://127.0.0.1:18080/
wait_status timeout 2001:db8::81 'http://[::1]:18080/'
nft -j list set ip crowdsec crowdsec-blacklists >"$retention_fixture/kernel4"
nft -j list set ip6 crowdsec6 crowdsec6-blacklists >"$retention_fixture/kernel6"
python3 -I - "$retention_fixture/kernel4" "$retention_fixture/kernel6" >/dev/null 2>&1 <<'PY'
import json,sys
for path,expected in zip(sys.argv[1:],('192.0.2.80','2001:db8::80')):
    sets=[x['set'] for x in json.load(open(path))['nftables'] if 'set' in x]
    assert len(sets)==1 and expected in str(sets[0].get('elem',[]))
PY
nft add table inet retention_unrelated
credential_digest=$(sql 'SELECT machine_id,password FROM machines ORDER BY id;' | sha256sum)
bouncer_digest=$(sql 'SELECT name,api_key FROM bouncers ORDER BY id;' | sha256sum)
# Keep decisions active in caches. Stale parents and an old child force the
# exact candidate fallback rather than natural decision expiry.
sql "UPDATE alerts SET created_at=now()-interval '23 hours 31 minutes'
 WHERE source_ip IN ('192.0.2.41','192.0.2.42','192.0.2.80','2001:db8::80');
 UPDATE events SET time=now()-interval '23 hours 31 minutes'
 WHERE alert_events IN (SELECT id FROM alerts WHERE source_ip='192.0.2.44');" >/dev/null
/usr/local/lib/aboutme/crowdsec-retention.sh runtime
[[ $(sql "SELECT count(*) FROM alerts WHERE source_ip IN ('192.0.2.41','192.0.2.42','192.0.2.44','192.0.2.80','2001:db8::80');") == 0 ]]
[[ $(sql "SELECT count(*) FROM alerts WHERE source_ip='192.0.2.45';") == 1 ]]
[[ $(sql 'SELECT machine_id,password FROM machines ORDER BY id;' | sha256sum) == "$credential_digest" ]]
[[ $(sql 'SELECT name,api_key FROM bouncers ORDER BY id;' | sha256sum) == "$bouncer_digest" ]]
nft list table inet retention_unrelated >/dev/null
wait_status 200 192.0.2.41 http://127.0.0.1:18080/
wait_status 503 192.0.2.41 http://127.0.0.1:18081/
wait_status 403 192.0.2.45 http://127.0.0.1:18081/
wait_status 200 192.0.2.80 http://127.0.0.1:18080/
wait_status 200 2001:db8::80 'http://[::1]:18080/'
wait_status timeout 192.0.2.81 http://127.0.0.1:18080/
wait_status timeout 2001:db8::81 'http://[::1]:18080/'
wait_status 403 192.0.2.45 http://127.0.0.1:18080/
/usr/local/lib/aboutme/crowdsec-retention-lease-check.sh
[[ $(sql "SELECT count(*) FROM pg_stats WHERE schemaname='public';") == 0 ]]
/usr/local/lib/aboutme/crowdsec-vacuum.sh
[[ $(sql "SELECT count(*) FROM pg_stats WHERE schemaname='public';") == 0 ]]
# A genuine lease is accepted before independent failures remove authorization.
systemctl stop aboutme-caddy.service aboutme-maintenance.service crowdsec-firewall-bouncer.service crowdsec.service
sql "UPDATE alerts SET created_at=now()-interval '23 hours 31 minutes' WHERE source_ip='192.0.2.45';" >/dev/null
/usr/local/lib/aboutme/crowdsec-retention.sh startup
[[ $(sql "SELECT count(*) FROM alerts WHERE source_ip='192.0.2.45';") == 0 ]]
[[ $(sql 'SELECT count(*) FROM machines;') -gt 0 ]]
[[ $(sql 'SELECT count(*) FROM bouncers;') == 2 ]]
[[ -z $(find /var/lib/crowdsec -type f -name '*.db' -print -quit) ]]
systemctl start crowdsec.service crowdsec-firewall-bouncer.service aboutme-caddy.service
wait_status 200 192.0.2.41 http://127.0.0.1:18080/
runuser -u postgres -- /usr/lib/postgresql/18/bin/pg_ctl -D "$retention_fixture/data" -m fast -w stop >/dev/null 2>&1
if /usr/local/lib/aboutme/crowdsec-retention.sh runtime; then
  echo 'FAIL database-loss-retention-admission'; exit 1
fi
for unit in aboutme-caddy.service aboutme-maintenance.service crowdsec-firewall-bouncer.service crowdsec.service; do
  [[ $(systemctl show --property=ActiveState --value "$unit") == inactive ]]
done
[[ ! -e /run/aboutme/crowdsec-retention-ok && ! -e /run/aboutme/caddy-log/crowdsec ]]
runuser -u postgres -- /usr/lib/postgresql/18/bin/pg_ctl -D "$retention_fixture/data" \
  -l "$retention_fixture/data/server.log" -w start >/dev/null 2>&1
[[ $(retention_fixture_admin aboutme <<<'SELECT count(*) FROM public.retention_sentinel;') == 1 ]]
/usr/local/lib/aboutme/crowdsec-retention.sh startup
/usr/local/lib/aboutme/crowdsec-retention-lease-check.sh
printf 'PASS actual-consumer-cache-and-both-family-kernel-rebuild\nPASS actual-database-loss-stops-consumers-and-removes-feeds\n'

# Restoration drops old samples, statistics and registrations rather than
# cleaning rows in place. The application sentinel stays in its own database.
integration_stage=restore-reset
runuser -u postgres -- /usr/lib/postgresql/18/bin/psql -X -qAt -w -v ON_ERROR_STOP=1 \
  -h /run/postgresql -d crowdsec >/dev/null 2>&1 <<'SQL'
ALTER TABLE public.alerts ALTER COLUMN source_ip SET STATISTICS 1;
ANALYZE public.alerts;
SQL
if /usr/local/lib/aboutme/crowdsec-db.sh preflight-ready </dev/null >/dev/null 2>&1; then
  echo 'FAIL contaminated-statistics-admission'; exit 1
fi
op=$(jq -r .operation_id /var/lib/aboutme/fence.json)
printf '{"operation_id":"%s","release_tag":"v0.8.9"}\n' "$op" |
  /usr/local/lib/aboutme/crowdsec-provision.sh restore-reset >/dev/null 2>&1
[[ $(sql 'SELECT count(*) FROM alerts;') == 0 ]]
[[ $(sql 'SELECT count(*) FROM machines;') == 0 ]]
[[ $(sql 'SELECT count(*) FROM bouncers;') == 0 ]]
[[ $(sql "SELECT count(*) FROM pg_stats WHERE schemaname='public';") == 0 ]]
[[ $(retention_fixture_admin aboutme <<<'SELECT count(*) FROM public.retention_sentinel;') == 1 ]]
old_key=$(sha256sum "$retention_fixture/caddy.key" | cut -d' ' -f1)
integration_stage=restore-registrations
crowdsec_admitted_cli machines add -a --force --error >/dev/null 2>&1
crowdsec_admitted_cli bouncers add aboutme-firewall -o raw >"$retention_fixture/firewall.key" 2>/dev/null
install -m 0600 "$retention_fixture/caddy.key" "$retention_fixture/old-caddy.key"
crowdsec_admitted_cli bouncers add aboutme-caddy -o raw >"$retention_fixture/caddy.key" 2>/dev/null
[[ $(sha256sum "$retention_fixture/caddy.key" | cut -d' ' -f1) != "$old_key" ]]
cat "$edge/bouncers/crowdsec-firewall-bouncer.yaml.local" >/etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local
printf 'api_key: %s\n' "$(cat "$retention_fixture/firewall.key")" >>/etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local
/usr/local/lib/aboutme/crowdsec-retention.sh startup
integration_stage=restore-consumers
systemctl start crowdsec.service
old_code=$(printf 'header = "X-Api-Key: %s"\n' "$(cat "$retention_fixture/old-caddy.key")" |
  curl -s -o /dev/null -w '%{http_code}' --max-time 5 -K - http://127.0.0.1:8095/v1/decisions)
[[ $old_code == 401 || $old_code == 403 ]]
systemctl start crowdsec-firewall-bouncer.service aboutme-caddy.service
wait_status 200 192.0.2.41 http://127.0.0.1:18080/
printf 'PASS fresh-reset-discards-statistics-and-registrations\nPASS replaced-credential-rejects-old-key\n'

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
}
export -f integration_body sql retention_fixture_sql retention_fixture_admin crowdsec_admitted_cli
export repo_root edge feed retention_fixture PATH
unshare --net bash -euo pipefail -c integration_body
