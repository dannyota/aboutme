#!/usr/bin/env bash
# Runs one memory-heavy host operation at a time.
set -euo pipefail

if ((EUID == 0)) && [[ -n ${ABOUTME_WORKLOAD_LOCK:-} ]]; then
  exit 2
fi
lock=${ABOUTME_WORKLOAD_LOCK:-/run/aboutme/workload.lock}
wait_seconds=${1:?wait seconds are required}
shift
[[ $wait_seconds =~ ^[0-9]+$ && $# -gt 0 ]]
exec flock --exclusive --wait "$wait_seconds" "$lock" "$@"
