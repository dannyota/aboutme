#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
host=$root/deploy/vn/scripts/deploy-host.sh
# Load only function definitions. All host commands below are stubs.
sed -n '/^step_oneshot() {/,/^}/p' "$host" >"$work/oneshot"
sed 's|/usr/local/lib/aboutme/workload-lock.sh|workload_lock|' "$work/oneshot" >"$work/functions"
# shellcheck disable=SC1091
source "$work/functions"
checkpoint() { :; }
die() { echo "$*" >&2; exit 1; }
say() { :; }
workload_lock() { return "$LOCK_RESULT"; }
systemctl() { echo read-result >>"$work/calls"; echo success; }
for LOCK_RESULT in 1 124; do
  export LOCK_RESULT
  : >"$work/calls"
  if (step_oneshot aboutme-migrate) >"$work/out" 2>&1; then
    echo 'failed lock/start accepted stale success' >&2; exit 1
  fi
  [[ ! -s $work/calls ]]
done
LOCK_RESULT=0 step_oneshot aboutme-migrate
[[ -s $work/calls ]]
grep -qF 'DATABASE_URL=postgres://aboutme@localhost/aboutme?host=/run/postgresql' \
  "$root/deploy/vn/host/quadlet/aboutme-db-setup.container.in"
if rg 'postgres://[^/ ]+@/' "$root/deploy/vn/host/quadlet"; then exit 1; fi
sed -n "/^release_shape=/,/end'/p" "$host" >"$work/shape"
sed -n '/^release_number() {/,/^}/p; /^step_restore_previous() {/,/^}/p' "$host" >>"$work/shape"
# shellcheck disable=SC1091
source "$work/shape"
record=$(jq -n --arg d "$(printf '%064d' 0)" '{release_tag:"v0.1.0",release_number:1000,
  maintenance_tag:"v0.1.0",maintenance_number:1000,
  server_image:("ghcr.io/dannyota/aboutme-server@sha256:"+$d),
  web_image:("ghcr.io/dannyota/aboutme-web@sha256:"+$d),
  caddy_image:("ghcr.io/dannyota/aboutme-caddy@sha256:"+$d),
  maintenance_image:("ghcr.io/dannyota/aboutme-caddy@sha256:"+$d)}')
release_read() { echo 'unexpected mutable release read' >&2; exit 1; }
render_all() { printf '%s\n' "$@" >"$work/render"; }
step_restore_previous "$record"
[[ $(head -n 1 "$work/render") == v0.1.0 ]]
[[ $(wc -l <"$work/render") == 8 ]]
if (step_restore_previous "$(jq '.release_number=999' <<<"$record")") >"$work/out" 2>&1; then exit 1; fi
echo 'host migration, saved release, and socket DSN tests passed'
