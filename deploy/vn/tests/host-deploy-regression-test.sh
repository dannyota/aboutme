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
if grep -RE 'postgres://[^/ ]+@/' "$root/deploy/vn/host/quadlet"; then exit 1; fi
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
cimd_rollback_check() {
  printf '%s\n' "$@" >"$work/rollback-check"
  [[ ${REFUSE_CIMD_ROLLBACK:-0} == 0 ]] || die 'CIMD rollback refused'
}
render_all() {
  [[ -s $work/rollback-check ]] || die 'rendered before CIMD rollback check'
  printf '%s\n' "$@" >"$work/render"
}
step_restore_previous "$record"
[[ $(cat "$work/rollback-check") == 1000 ]]
[[ $(head -n 1 "$work/render") == v0.1.0 ]]
[[ $(wc -l <"$work/render") == 8 ]]
rm "$work/rollback-check" "$work/render"
if (step_restore_previous "$(jq '.release_number=999' <<<"$record")") >"$work/out" 2>&1; then exit 1; fi
grep -qxF 'saved release numbers do not match their tags' "$work/out"
[[ ! -e $work/rollback-check && ! -e $work/render ]]
if (REFUSE_CIMD_ROLLBACK=1 step_restore_previous "$record") >"$work/out" 2>&1; then
  echo 'restored a release after CIMD rollback refusal' >&2; exit 1
fi
grep -qxF 'CIMD rollback refused' "$work/out"
[[ $(cat "$work/rollback-check") == 1000 && ! -e $work/render ]]
sed -n '/^render_observer() {/,/^}/p; /^image_ok() {/,/^}/p' "$host" >"$work/observer-functions"
# shellcheck disable=SC1091
source "$work/observer-functions"
# shellcheck disable=SC2034 # used by the sourced render_observer function
units=$work/units templates=$root/deploy/vn/host/quadlet
repo=ghcr.io/dannyota/aboutme
mkdir "$units"
printf 'existing observer unit\n' >"$units/aboutme-observer.container"
cp "$units/aboutme-observer.container" "$work/previous-observer"
: >"$work/calls"
systemctl() { printf '%s\n' "$*" >>"$work/calls"; }
active() { return 0; }
render_observer ""
cmp "$units/aboutme-observer.container" "$work/previous-observer"
[[ ! -s $work/calls ]]
observer_image=$repo-observer@sha256:$(printf '%064d' 7)
render_observer "$observer_image"
grep -qxF "Image=$observer_image" "$units/aboutme-observer.container"
grep -qxF 'restart aboutme-observer' "$work/calls"
if grep -q '@[A-Z_]*@' "$units/aboutme-observer.container"; then exit 1; fi
grep -q 'aboutme-observer.container.in.*continue' "$host"
# Atomic authority replacement requires the directory mount, rather than an
# inode pinned by a single-file bind. This also applies after saved restoration.
grep -qxF 'Volume=/var/lib/aboutme/privacy-notice:/var/lib/aboutme/privacy-notice:ro' \
  "$root/deploy/vn/host/quadlet/aboutme-server.container.in" || {
  echo 'server lacks the whole read-only notice authority directory' >&2; exit 1;
}
if grep -Eq '^Volume=.*privacy-notice/(epoch|send.json|activation.json):' \
  "$root/deploy/vn/host/quadlet/aboutme-server.container.in"; then
  echo 'server pins a replaceable authority file' >&2; exit 1
fi
# Exercise both callers with a failed real checkpoint boundary before a render
# or any application start. The fence suite owns the notice state matrix.
sed -n '/^step_app_up() {/,/^}/p' "$host" >"$work/app-up"
# shellcheck disable=SC1091
source "$work/app-up"
checkpoint() { die 'invalid_state'; }
render_all() { echo render >>"$work/notice-effects"; }
systemctl() { echo start >>"$work/notice-effects"; }
for operation in restore app-up; do
  rm -f "$work/notice-effects"
  if (
    if [[ $operation == restore ]]; then step_restore_previous "$record"; else step_app_up; fi
  ) >"$work/notice-out" 2>&1; then
    echo 'application path bypassed notice checkpoint refusal' >&2; exit 1
  fi
  [[ ! -e $work/notice-effects ]]
  grep -qxF invalid_state "$work/notice-out"
done
# A new full gate is required before each schema-capable smoke action.
sed -n '/^step_ban_test() {/,/^}/p' "$host" |
  sed 's|/usr/local/lib/aboutme/crowdsec-db.sh|crowdsec_gate|g' >"$work/ban-function"
# shellcheck disable=SC1091
source "$work/ban-function"
crowdsec_gate() {
  [[ $* == preflight-ready ]] || return 1
  printf 'gate\n' >>"$work/ban-calls"
  [[ ! -e $work/schema-invalid ]]
}
cscli() { printf 'vendor\n' >>"$work/ban-calls"; }
for action in add del; do
  : >"$work/ban-calls"
  rm -f "$work/schema-invalid"
  (step_ban_test "$action" 192.0.2.1)
  [[ $(cat "$work/ban-calls") == $'gate\nvendor' ]] || {
    echo 'FAIL smoke-per-call-admission' >&2; exit 1;
  }
  : >"$work/ban-calls"
  touch "$work/schema-invalid"
  if (step_ban_test "$action" 192.0.2.1) >"$work/out" 2>&1; then
    echo 'FAIL smoke-corrupt-schema-refusal' >&2; exit 1
  fi
  [[ $(cat "$work/ban-calls") == gate ]] || {
    echo 'FAIL smoke-vendor-before-refusal' >&2; exit 1;
  }
done
echo 'host migration, saved release, and socket DSN tests passed'
