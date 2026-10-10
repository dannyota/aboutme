#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
runner=$repo_root/deploy/vn/host/workload-lock.sh
cutover=$repo_root/deploy/vn/scripts/cutover.sh
deploy_host=$repo_root/deploy/vn/scripts/deploy-host.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
lock=$work/workload.lock
order=$work/order

grep -qF '/usr/local/lib/aboutme/workload-lock.sh 3600 systemctl start aboutme-db-setup.service' \
  "$cutover" || { echo "cutover db setup bypasses the workload lock" >&2; exit 1; }
grep -qF '/usr/local/lib/aboutme/workload-lock.sh 3600' "$deploy_host" || {
  echo "deploy one-shot bypasses the workload lock" >&2
  exit 1
}

ABOUTME_WORKLOAD_LOCK=$lock "$runner" 5 bash -c \
  'printf "first-start\n" >>"$1"; sleep 1; printf "first-end\n" >>"$1"' _ "$order" &
first_pid=$!
while [[ ! -s $order ]]; do sleep 0.05; done
ABOUTME_WORKLOAD_LOCK=$lock "$runner" 5 bash -c \
  'printf "second\n" >>"$1"' _ "$order" &
second_pid=$!
wait "$first_pid" "$second_pid"

[[ $(cat "$order") == $'first-start\nfirst-end\nsecond' ]] || {
  echo "memory-heavy work was not serialized" >&2
  exit 1
}

echo "workload lock tests passed"
