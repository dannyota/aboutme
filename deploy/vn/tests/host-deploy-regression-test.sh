#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
host=$root/deploy/vn/scripts/deploy-host.sh
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
grep -qxF 'Volume=/var/lib/aboutme/privacy-notice:/var/lib/aboutme/privacy-notice:ro' \
  "$root/deploy/vn/host/quadlet/aboutme-server.container.in" || {
  echo 'server lacks the whole read-only notice authority directory' >&2; exit 1;
}
if grep -Eq '^Volume=.*privacy-notice/(epoch|send.json|activation.json):' \
  "$root/deploy/vn/host/quadlet/aboutme-server.container.in"; then
  echo 'server pins a replaceable authority file' >&2; exit 1
fi
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
sed -n '/^clear_stale_healthchecks() {/,/^}/p; /^step_timers() {/,/^}/p' "$host" >"$work/timers-functions"
# shellcheck disable=SC1091
source "$work/timers-functions"
checkpoint() { :; }
say() { printf 'deploy-host: %s\n' "$*" >&2; }
# shellcheck disable=SC2034 # step_timers in the sourced deploy-host.sh reads it
jobs=(test)
stale_id=$(printf '%064d' 1)
live_id=$(printf '%064d' 2)
systemctl() {
  case $* in
    'enable --now aboutme-job-test.timer') ;;
    'list-units --failed --type=service --no-legend --no-pager --plain')
      [[ ${CLEANUP_ERROR:-} != list ]] || return 1
      printf '%s.service loaded failed failed healthcheck\n' "$stale_id" "$live_id"
      printf '%s\n' 'other.service loaded failed failed other' \
        "${stale_id}0.service loaded failed failed other" \
        "${stale_id:0:63}A.service loaded failed failed other"
      ;;
    reset-failed\ *)
      printf '%s\n' "$2" >>"$work/reset-attempts"
      [[ $# == 2 && $2 == "$stale_id.service" ]] || return 1
      [[ ${CLEANUP_ERROR:-} != reset ]] || return 1
      printf '%s\n' "$2" >>"$work/reset-units"
      ;;
    *) echo "unexpected systemctl call: $*" >&2; return 1 ;;
  esac
}
podman() {
  [[ $1 == container && $2 == exists && $# == 3 ]] || return 125
  printf '%s\n' "$3" >>"$work/checked-containers"
  [[ ${CLEANUP_ERROR:-} != exists ]] || return 125
  case $3 in
    "$live_id") return 0 ;;
    "$stale_id") return 1 ;;
    *) return 125 ;;
  esac
}
: >"$work/reset-units"
: >"$work/reset-attempts"
: >"$work/checked-containers"
step_timers start 2>"$work/timers-out"
[[ $(cat "$work/reset-units") == "$stale_id.service" ]] || {
  echo 'stale healthcheck was not the only unit reset' >&2; exit 1;
}
[[ $(cat "$work/reset-attempts") == "$stale_id.service" ]] || {
  echo 'cleanup attempted to reset an unexpected unit' >&2; exit 1;
}
[[ $(cat "$work/checked-containers") == "$stale_id"$'\n'"$live_id" ]] || {
  echo 'healthcheck cleanup inspected an unexpected unit' >&2; exit 1;
}
grep -qxF 'deploy-host: cleared 1 stale healthcheck units' "$work/timers-out"
for CLEANUP_ERROR in list exists reset; do
  export CLEANUP_ERROR
  : >"$work/reset-units"
  : >"$work/reset-attempts"
  set +e
  (set -euo pipefail; step_timers start) 2>"$work/timers-out"
  status=$?
  set -e
  if [[ $status != 0 ]]; then
    echo "cleanup $CLEANUP_ERROR error failed timer startup" >&2; exit 1
  fi
  [[ ! -s $work/reset-units ]] || { echo 'cleanup reset an unproved unit' >&2; exit 1; }
  if [[ $CLEANUP_ERROR == reset ]]; then
    [[ $(cat "$work/reset-attempts") == "$stale_id.service" ]] || exit 1
  else
    [[ ! -s $work/reset-attempts ]] || exit 1
  fi
  grep -qF 'deploy-host: warning:' "$work/timers-out" || {
    echo "cleanup $CLEANUP_ERROR error was not reported" >&2; exit 1;
  }
  grep -qxF 'deploy-host: cleared 0 stale healthcheck units' "$work/timers-out"
done
unset CLEANUP_ERROR
sed -n '/^prune_refs() {/,/^}/p; /^step_prune_images() {/,/^}/p' "$host" >"$work/prune-functions"
# shellcheck disable=SC1091
source "$work/prune-functions"
p=$work/prune
# shellcheck disable=SC2034 # read by the sourced prune functions
state=$p/state units=$p/units data=$p/data operations_dir=$p/data/host-operations release_json=$p/state/release.json \
  maintenance_json=$p/state/maintenance.json
ref() { printf 'ghcr.io/dannyota/aboutme-%s@sha256:%064d' "$1" "$2"; }
img() { printf '%064d' "$1" | tr 0 f; }
prune_fixture() {
  rm -rf "$p"
  mkdir -p "$state" "$units" "$operations_dir/op1" "$operations_dir/op2"
  jq -n --arg s "$(ref server 1)" --arg w "$(ref web 2)" --arg c "$(ref caddy 3)" \
    '{server_image:$s,web_image:$w,caddy_image:$c,maintenance_image:$c}' >"$state/release.json"
  jq -n --arg s "$(ref server 4)" --arg w "$(ref web 5)" --arg c "$(ref caddy 3)" \
    '{server_image:$s,web_image:$w,caddy_image:$c,maintenance_image:$c}' >"$state/release.previous.json"
  jq -n --arg i "$(ref caddy 6)" '{image:$i}' >"$state/maintenance.json"
  jq -n --arg s "$(ref server 7)" --arg w "$(ref web 2)" '{release:{server_image:$s,web_image:$w}}' \
    >"$operations_dir/op1/state.json"
  printf '[Container]\nImage=%s\n' "$(ref observer 8)" >"$units/aboutme-observer.container"
  printf '[Container]\nImage=%s\n' "$(ref server 1)" >"$units/aboutme-server.container"
  printf 'Image=%s\n' "$(ref server 10)" >"$units/aboutme-server.container.bak"
  printf '%s\n' "$(img 9 i)" "sha256:$(img 1 a)" >"$p/containers"
  {
    printf '%s %s\n' "$(ref server 1)" "$(img 1 a)" "$(ref server 11)" "$(img 1 a)" \
      "$(ref web 2)" "$(img 2 b)" "$(ref caddy 3)" "$(img 3 c)" "$(ref server 4)" "$(img 4 d)" \
      "$(ref caddy 6)" "$(img 6 f)" "$(ref server 7)" "$(img 7 g)" "$(ref observer 8)" "$(img 8 h)"
  } >"$p/local"
  jq -n --arg a "$(img 1 a)" --arg b "$(img 2 b)" --arg c "$(img 3 c)" --arg d "$(img 4 d)" \
    --arg f "$(img 6 f)" --arg g "$(img 7 g)" --arg h "$(img 8 h)" --arg i "$(img 9 i)" \
    --arg j "$(img 10 j)" --arg k "$(img 12 k)" --arg l "$(img 14 l)" --arg m "$(img 15 m)" \
    --arg n "$(img 16 n)" --arg o "$(img 17 o)" \
    --arg r1 "$(ref server 1)" --arg r11 "$(ref server 11)" --arg r2 "$(ref web 2)" --arg r3 "$(ref caddy 3)" \
    --arg r4 "$(ref server 4)" --arg r6 "$(ref caddy 6)" --arg r7 "$(ref server 7)" --arg r8 "$(ref observer 8)" \
    --arg r9 "$(ref server 9)" --arg r10 "$(ref server 10)" --arg r12 "$(ref web 12)" --arg r13 "$(ref web 13)" \
    --arg r14 "$(ref caddy 14)" --arg r15 "docker.io/library/alpine@sha256:$(printf '%064d' 15)" \
    --arg r17 "ghcr.io/other/aboutme-server@sha256:$(printf '%064d' 17)" '
    [{Id:$a,RepoDigests:[$r1,$r11]},{Id:$b,RepoDigests:[$r2]},{Id:$c,RepoDigests:[$r3]},{Id:$d,RepoDigests:[$r4]},
     {Id:$f,RepoDigests:[$r6]},{Id:$g,RepoDigests:[$r7]},{Id:$h,RepoDigests:[$r8]},{Id:$i,RepoDigests:[$r9]},
     {Id:$j,RepoDigests:[$r10]},{Id:$k,RepoDigests:[$r12,$r13]},{Id:$l,RepoDigests:[$r14]},
     {Id:$m,RepoDigests:[$r15]},{Id:$n,RepoDigests:[]},{Id:$n,RepoDigests:null},{Id:$o,RepoDigests:[$r17]}]' >"$p/images.json"
  : >"$p/calls"
  : >"$p/rm"
}
podman() {
  printf '%s\n' "$*" >>"$p/calls"
  case $* in
    'ps -a --no-trunc --format {{.ImageID}}')
      [[ ${PRUNE_FAIL:-} != ps ]] || return 125
      cat "$p/containers"
      ;;
    'images --format json')
      [[ ${PRUNE_FAIL:-} != images ]] || return 125
      cat "$p/images.json"
      ;;
    'image exists -- '*)
      [[ ${PRUNE_FAIL:-} != exists ]] || return 125
      grep -qF -- "$4 " "$p/local"
      ;;
    'image inspect --format {{.Id}} -- '*)
      [[ ${PRUNE_FAIL:-} != inspect ]] || return 125
      grep -F -- "$6 " "$p/local" | cut -d' ' -f2
      ;;
    'image rm --no-prune -- '*)
      [[ $# == 5 ]] || { printf 'unexpected podman call: %s\n' "$*" >>"$p/unexpected"; return 125; }
      printf '%s\n' "$5" >>"$p/rm"
      [[ $5 != "${PRUNE_FAIL_RM:-}" ]]
      ;;
    *) printf 'unexpected podman call: %s\n' "$*" >>"$p/unexpected"; return 125 ;;
  esac
}
die() { say "$*"; exit 1; }
mountpoint() {
  printf 'mountpoint %s\n' "$*" >>"$p/calls"
  [[ $* == "-q $data" && ${PRUNE_FAIL:-} != unmounted ]]
}
checkpoint() {
  printf 'checkpoint\n' >>"$p/calls"
  [[ ${PRUNE_FAIL:-} != checkpoint ]] || die 'fence checkpoint failed; the lock or the floor no longer holds'
}
prune_run() {
  set +e
  (set -euo pipefail; step_prune_images) >"$p/out" 2>&1
  prune_status=$?
  set -e
}
prune_failures=0
prune_fail() { echo "FAIL prune: $*" >&2; prune_failures=1; }
unused=$(printf '%s\n' "$(ref server 10)" "$(ref web 12)" "$(ref web 13)" "$(ref caddy 14)" | sort)
prune_fixture
prune_run
[[ $prune_status == 0 ]] || prune_fail "exit $prune_status on a clean run: $(cat "$p/out")"
[[ $(head -n 1 "$p/calls") == checkpoint ]] || prune_fail 'did not checkpoint first'
[[ $(sort "$p/rm") == "$unused" ]] || prune_fail "removed $(paste -sd ' ' "$p/rm"), not only the unused aboutme digests"
if grep -q -e '--force' -e ' -f ' "$p/calls"; then prune_fail 'passed force to podman'; fi
[[ ! -e $p/unexpected ]] || prune_fail "$(cat "$p/unexpected")"
grep -qxF 'deploy-host: removed 4 unused images, kept 9' "$p/out" || prune_fail "wrong summary: $(cat "$p/out")"
for source in release previous maintenance operation unit container; do
  prune_fixture
  case $source in
    release) rm "$state/release.json" "$units/aboutme-server.container"; sed -i '2d' "$p/containers"; want=$(ref server 1) ;;
    previous) rm "$state/release.previous.json"; want=$(ref server 4) ;;
    maintenance) rm "$state/maintenance.json"; want=$(ref caddy 6) ;;
    operation) rm "$operations_dir/op1/state.json"; want=$(ref server 7) ;;
    unit) rm "$units/aboutme-observer.container"; want=$(ref observer 8) ;;
    container) : >"$p/containers"; want=$(ref server 9) ;;
  esac
  prune_run
  if [[ $prune_status != 0 ]] || ! grep -qxF -- "$want" "$p/rm"; then
    prune_fail "the $source source was not what kept $want"
  fi
  if [[ $source == release ]] && ! grep -qxF -- "$(ref server 11)" "$p/rm"; then
    prune_fail 'an unused image kept the second digest of the same image'
  fi
done
for failure in previous-dir operation-dir operations-file operations-symlink units-missing bad-image-id \
  bad-container-id unmounted ps images exists inspect checkpoint; do
  prune_fixture
  PRUNE_FAIL=''
  case $failure in
    previous-dir) rm "$state/release.previous.json"; mkdir "$state/release.previous.json" ;;
    operation-dir) mkdir "$operations_dir/op2/state.json" ;;
    operations-file) rm -rf "$operations_dir"; : >"$operations_dir" ;;
    operations-symlink) mv "$operations_dir" "$p/real-operations"; ln -s "$p/real-operations" "$operations_dir" ;;
    units-missing) rm -rf "$units" ;;
    bad-image-id) jq -c '. + [{Id:"sha256:bad",RepoDigests:[]}]' "$p/images.json" >"$p/images.tmp"
      mv "$p/images.tmp" "$p/images.json" ;;
    bad-container-id) printf '%s\n' "$(img 9 i | cut -c1-12)" >>"$p/containers" ;;
    *) PRUNE_FAIL=$failure ;;
  esac
  export PRUNE_FAIL
  prune_run
  if [[ $prune_status == 0 || -s $p/rm ]]; then
    prune_fail "$failure removed images or exited 0"
  fi
  message='deploy-host: warning: '
  [[ $failure != checkpoint ]] || message='deploy-host: fence checkpoint failed'
  grep -qF "$message" "$p/out" || prune_fail "$failure gave no warning: $(cat "$p/out")"
done
unset PRUNE_FAIL
prune_fixture
rm -rf "$operations_dir"
prune_run
if [[ $prune_status != 0 || $(sort "$p/rm") != "$(printf '%s\n' "$unused" "$(ref server 7)" | sort)" ]] ||
   ! grep -qxF 'deploy-host: removed 5 unused images, kept 8' "$p/out"; then
  prune_fail "a mounted data volume with no operations directory did not prune: $(cat "$p/out")"
fi
grep -qxF "mountpoint -q $data" "$p/calls" || prune_fail 'did not check that the data volume is mounted'
prune_fixture
export PRUNE_FAIL_RM
PRUNE_FAIL_RM=$(ref web 12)
prune_run
unset PRUNE_FAIL_RM
if [[ $prune_status != 0 || $(sort "$p/rm") != "$unused" ]] ||
   ! grep -qxF "deploy-host: warning: could not remove $(ref web 12)" "$p/out" ||
   ! grep -qxF 'deploy-host: removed 3 unused images, kept 9' "$p/out"; then
  prune_fail "a failed removal stopped the rest or failed the step: $(cat "$p/out")"
fi
grep -qxF '  prune-images:0) step_prune_images ;;' "$host" || prune_fail 'no prune-images dispatcher entry'
((prune_failures == 0)) || exit 1
echo 'host migration, saved release, socket DSN, and image prune tests passed'
