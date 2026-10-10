#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
base=$repo_root/deploy/vn

grep -qxF '    max_age: 23h' "$base/edge/crowdsec/config.yaml.local"
grep -qxF '    duration: 15m' "$base/edge/crowdsec/profiles.yaml"
grep -qxF 'capacity: 3000' "$base/edge/crowdsec/scenarios/aboutme-http-static-flood.yaml"
grep -qF "GetDecisionsCount(Alert.GetValue()) == 1 ? '1h' : '4h'" "$base/edge/crowdsec/profiles.yaml"
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
grep -qxF 'Requisite=aboutme-crowdsec-retention-startup.service' \
  "$base/host/etc/systemd/system/aboutme-crowdsec-retention.service"
grep -qxF 'After=aboutme-crowdsec-retention-startup.service crowdsec.service' \
  "$base/host/etc/systemd/system/aboutme-crowdsec-retention.service"
grep -qxF 'TimeoutStartSec=5min' \
  "$base/host/etc/systemd/system/aboutme-crowdsec-retention-startup.service"
grep -qxF 'OnFailure=aboutme-crowdsec-retention-failure.service' \
  "$base/host/etc/systemd/system/aboutme-crowdsec-retention-startup.service"

if rg -n 'http_path|http_user_agent|request_uri|request_body|headers' \
  "$base/edge/crowdsec/parsers" "$base/edge/crowdsec/scenarios"; then
  echo "custom CrowdSec config retains an HTTP field outside the approved feed" >&2
  exit 1
fi

echo "CrowdSec configuration tests passed"
