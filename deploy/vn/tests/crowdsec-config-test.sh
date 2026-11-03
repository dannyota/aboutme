#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
base=$repo_root/deploy/vn

if [[ ${1:-} == parser ]]; then
  [[ ${GITHUB_ACTIONS:-} == true && $EUID == 0 ]] || exit 1
  # Parse synthetic Caddy encoder output with the installed production config.
  # Keep dumps private and remove them instead of uploading raw parser state.
  umask 077
  work=$(mktemp -d /run/aboutme/crowdsec-parser-proof.XXXXXX)
  trap 'rm -rf -- "$work"' EXIT
  for mode in serving maintenance; do
    mkdir "$work/$mode"
    python3 -I - "$base/edge/crowdsec/acquis.d/aboutme-http.yaml" "$work/$mode" "$mode" <<'PY'
import json, pathlib, sys, yaml
acquis, root, mode = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), sys.argv[3]
labels = next(item['labels'] for item in yaml.safe_load_all(acquis.read_text())
              if item['labels']['feed_mode'] == mode)
(root / 'labels').write_text(','.join(f'{key}:{value}' for key, value in labels.items()))
with (root / 'feed.json').open('w') as stream:
    for ts in ('2026-10-10T01:09:10Z', '2026-10-10T01:09:10.1Z',
               '2026-10-10T01:09:10.123456Z', '2026-10-10T01:09:10.123456789Z'):
        record = dict(ts=ts, source_ip='192.0.2.211', method='GET',
                      status=503 if mode == 'maintenance' else 404,
                      route_class='maintenance' if mode == 'maintenance' else 'unknown',
                      probe_class='none' if mode == 'maintenance' else 'other',
                      target_path='' if mode == 'maintenance' else '/unknown/time')
        stream.write(json.dumps(record, separators=(',', ':')) + '\n')
PY
    "$base/host/crowdsec-db.sh" preflight-ready </dev/null >/dev/null 2>&1
    if ! timeout 30 crowdsec -no-api -dsn "file://$work/$mode/feed.json" \
      -type aboutme-http -label "$(<"$work/$mode/labels")" -order-event \
      -dump-data "$work/$mode" >"$work/$mode/replay.log" 2>&1; then
      printf 'FAIL caddy-event-time mode=%s stage=parse\n' "$mode" >&2
      exit 1
    fi
    python3 -I - "$work/$mode" "$mode" <<'PY'
import json, pathlib, sys, yaml
root, mode = pathlib.Path(sys.argv[1]), sys.argv[2]
records = [json.loads(line) for line in (root / 'feed.json').read_text().splitlines()]
dump = yaml.safe_load((root / 'parser-dump.yaml').read_text())
results = dump['s02-enrich']['aboutme/http-source']
assert len(results) == len(records), 'event_count'
expected = sorted(record['ts'] for record in records)
assert all(result['success'] for result in results), 'parser_success'
events = [result['evt'] for result in results]
assert sorted(event.get('StrTime', '') for event in events) == expected, 'string_time'
assert sorted(event.get('MarshaledTime', '') for event in events) == expected, 'event_time'
assert all(event['Meta']['source_ip'] == '192.0.2.211' and
           event['Meta']['feed_mode'] == mode for event in events), 'acquisition_labels'
PY
  done
  echo 'PASS caddy-event-time-whole-and-fractional-utc'
  exit 0
fi

failures=0
check() {
  local label=$1
  shift
  if "$@" >/dev/null 2>&1; then
    printf 'PASS %s\n' "$label"
  else
    printf 'FAIL %s\n' "$label"
    failures=$((failures + 1))
  fi
}

check active-postgresql-storage python3 - "$base" <<'PY'
import pathlib
import re
import sys

base = pathlib.Path(sys.argv[1])
config = (base / "edge/crowdsec/config.yaml.local").read_text()
db = config.split("\ndb_config:\n", 1)[1].split("\n\n", 1)[0]
for line in (
    "  type: pgx", "  db_name: crowdsec", "  user: crowdsec",
    "  db_path: /run/postgresql", '  host: ""', "  port: 0",
    "  sslmode: disable", "  max_open_conns: 4",
    "    max_age: 0s", "    max_items: null",
):
    assert line in db.splitlines()
assert not re.search(r"(?im)^\s*(password|use_wal):|sqlite|crowdsec\.db", db)
assert "  log_media: file" in config and "  log_dir: /run/aboutme/crowdsec" in config
assert "online_client:" not in config
# A cached collection install must not resolve the latest engine release.
cscli = config.split("\ncscli:\n", 1)[1].split("\n\n", 1)[0]
assert "  hub_branch: master" in cscli.splitlines()
PY

check installer-postgresql-admission python3 - "$base/host/install.sh" <<'PY'
import pathlib
import re
import sys

source = pathlib.Path(sys.argv[1]).read_text()
assert not re.search(r"\bsqlite3?\b|wal_checkpoint", source, re.I)
assert "python3-yaml" in source
assert "/etc/crowdsec/config.yaml.postgresql" in source
assert "pg_ident.conf" in source
assert "crowdsec-db.sh" in source and "crowdsec-provision.sh" in source
assert "crowdsec-vacuum.sh" in source and "aboutme-crowdsec-vacuum.timer" in source
assert '"$lib/crowdsec-migrate.sh"' in source
source += pathlib.Path(sys.argv[1]).with_name("crowdsec-migrate.sh").read_text()
binary = re.search(r"(?m)^\s*(?:timeout\s+\S+\s+)?(?:\S*/)?(?:cscli|crowdsec)(?:\s|$)", source)
assert binary is not None
assert source.index("python3-yaml") < binary.start()
assert source.index("/etc/crowdsec/config.yaml.postgresql") < binary.start()
assert source.index("pg_ident.conf") < binary.start()
PY

check postgresql-consumer-start-gates python3 - "$base/host/etc/systemd/system" <<'PY'
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
startup = (root / "aboutme-crowdsec-retention-startup.service").read_text()
runtime = (root / "aboutme-crowdsec-retention.service").read_text()
for source in (startup, runtime):
    assert "postgresql@18-main.service" in source
    assert "StandardOutput=null" in source and "StandardError=null" in source
assert "TimeoutStartSec=5min" in startup
assert "TimeoutStartSec=1min" in runtime
for unit in ("crowdsec", "crowdsec-firewall-bouncer"):
    source = (root / (unit + ".service.d/90-aboutme-alert.conf")).read_text()
    assert "postgresql@18-main.service" in source
    assert "crowdsec-retention-lease-check.sh" in source
    pre=[line.split('=',1)[1] for line in source.splitlines() if line.startswith('ExecStartPre=')]
    assert pre[0]=='' and len(pre)==5
    assert pre[1]=='/usr/local/lib/aboutme/fence.sh check-current '+unit+'.service'
    assert pre[2]==pre[4]=='/usr/local/lib/aboutme/crowdsec-retention-lease-check.sh'
    assert pre[3].startswith('/usr/bin/'+unit+' -c ') and ' -t' in pre[3]
    if unit=='crowdsec':
        reloads=[line.split('=',1)[1] for line in source.splitlines() if line.startswith('ExecReload=')]
        assert reloads==['',pre[2],pre[3],pre[4],'/bin/kill -HUP $MAINPID']
PY
grep -qxF '    duration: 15m' "$base/edge/crowdsec/profiles.yaml"
check single-active-count-profile python3 - "$base/edge/crowdsec/profiles.yaml" <<'PY'
import pathlib, sys
source = pathlib.Path(sys.argv[1]).read_text()
expression = next(line for line in source.splitlines() if line.startswith('duration_expr:'))
assert expression == 'duration_expr: "let active = GetActiveDecisionsCount(Alert.GetValue()); active == 0 ? \'15m\' : (active == 1 ? \'1h\' : \'4h\')"'
assert source.count('GetActiveDecisionsCount(') == 1
assert 'GetDecisionsCount(' not in source
assert [line for line in source.splitlines() if line.startswith('name:')] == [
    'name: aboutme_http_remediation', 'name: default_ip_remediation', 'name: default_range_remediation']
assert source.count('on_success: break') == 3
PY
grep -qF '/run/aboutme/caddy-log/crowdsec/serving.json' "$base/edge/crowdsec/acquis.d/aboutme-http.yaml"
grep -qF '/run/aboutme/caddy-log/crowdsec/maintenance.json' "$base/edge/crowdsec/acquis.d/aboutme-http.yaml"
grep -qxF '  type: aboutme-http' "$base/edge/crowdsec/acquis.d/aboutme-http.yaml"
grep -qxF 'share_custom: false' "$base/edge/crowdsec/console.yaml"
grep -qF "evt.Meta.route_class == 'unknown'" \
  "$base/edge/crowdsec/scenarios/aboutme-http-status.yaml"
grep -qxF 'groupby: evt.Meta.source_ip' \
  "$base/edge/crowdsec/scenarios/aboutme-http-rate.yaml"
grep -qxF 'StandardOutput=null' \
  "$base/host/etc/systemd/system/crowdsec.service.d/90-aboutme-alert.conf"
grep -qxF 'LimitCORE=0' "$base/host/etc/systemd/system/crowdsec.service.d/90-aboutme-alert.conf"
grep -qxF 'MemoryMax=256M' \
  "$base/host/etc/systemd/system/crowdsec.service.d/90-aboutme-alert.conf"
grep -qxF 'MemoryMax=64M' \
  "$base/host/etc/systemd/system/crowdsec-firewall-bouncer.service.d/90-aboutme-alert.conf"
grep -qF 'size=64M' \
  "$base/host/etc/systemd/system/run-aboutme-caddy\x2dlog.mount"
grep -qxF 'MemoryMax=1G' \
  "$base/host/etc/systemd/system/postgresql@.service.d/90-aboutme-alert.conf"
grep -qxF 'OnCalendar=*:0/5' \
  "$base/host/etc/systemd/system/aboutme-crowdsec-retention.timer"
grep -qxF 'AccuracySec=1s' \
  "$base/host/etc/systemd/system/aboutme-crowdsec-retention.timer"
grep -qxF 'Requisite=aboutme-crowdsec-retention-startup.service' \
  "$base/host/etc/systemd/system/aboutme-crowdsec-retention.service"
grep -qE '^After=.*aboutme-crowdsec-retention-startup.service.*crowdsec.service' \
  "$base/host/etc/systemd/system/aboutme-crowdsec-retention.service"
grep -qxF 'TimeoutStartSec=5min' \
  "$base/host/etc/systemd/system/aboutme-crowdsec-retention-startup.service"
grep -qxF 'OnFailure=aboutme-crowdsec-retention-failure.service' \
  "$base/host/etc/systemd/system/aboutme-crowdsec-retention-startup.service"

if grep -RnE 'http_path|http_user_agent|request_uri|request_body|headers' \
  "$base/edge/crowdsec/parsers" "$base/edge/crowdsec/scenarios"; then
  echo "custom CrowdSec config retains an HTTP field outside the approved feed" >&2
  exit 1
fi

((failures == 0)) || exit 1
echo "CrowdSec configuration tests passed"
