#!/usr/bin/env bash
# Blocks an edge process start when the independent retention lease is stale.
set -euo pipefail

lease=${1:-/run/aboutme/crowdsec-retention-ok}
if ((EUID == 0)) && (($# != 0)); then
  exit 2
fi

systemctl is-enabled --quiet aboutme-crowdsec-retention.timer
systemctl is-active --quiet aboutme-crowdsec-retention.timer
[[ -f $lease && ! -L $lease ]]
lease_mtime=$(stat -c %Y "$lease")
((lease_mtime <= EPOCHSECONDS && EPOCHSECONDS - lease_mtime <= 360))
if ((EUID == 0)); then
  [[ $(stat -c '%u:%g:%a' "$lease") == 0:0:600 ]]
fi
