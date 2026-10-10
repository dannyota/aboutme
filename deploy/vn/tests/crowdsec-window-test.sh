#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
check=$repo_root/deploy/vn/host/crowdsec-window-check.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cat >"$work/ssh-bf.yaml" <<'YAML'
name: crowdsecurity/ssh-bf
type: leaky
capacity: 5
leakspeed: 10s
labels:
  service: ssh
YAML
cat >"$work/http-rate.yaml" <<'YAML'
name: aboutme/http-rate
type: leaky
capacity: 120
leakspeed: 1s
labels:
  service: http
YAML
"$check" "$work" >/dev/null

cat >"$work/ssh-time-based-bf.yaml" <<'YAML'
name: crowdsecurity/ssh-time-based-bf
type: leaky
capacity: -1
leakspeed: 2h
labels:
  service: ssh
YAML
if "$check" "$work" >/dev/null 2>&1; then
  echo "two-hour SSH scenario was accepted" >&2
  exit 1
fi

echo "CrowdSec scenario window tests passed"
