#!/usr/bin/env bash
# Persistence failures must remain failures under conditional Bash execution.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
sed -n '/^store() {/,/^}/p; /^store_mutation() {/,/^}/p' "$root/deploy/vn/scripts/fence.sh" >"$work/functions"
# shellcheck disable=SC1091
source "$work/functions"
dir=$work file=$work/fence.json mutation_file=$work/cutover-mutation.json
fail_at=none
chmod() { [[ $fail_at != chmod ]] && command chmod "$@"; }
mv() {
  [[ $fail_at != rename ]] || return 1
  [[ ${*: -1} != "$file" ]] || touch "$work/release-write"
  command mv "$@"
}
sync() {
  if [[ ${1-} == "$dir" ]]; then [[ $fail_at != directory-sync ]]; else [[ $fail_at != file-sync ]]; fi
}
die() { printf 'persistence_refused\n' >&2; exit 1; }
for writer in store store_mutation; do
  for fail_at in chmod file-sync rename directory-sync; do
    if printf '{"minimum_release":8009}\n' | "$writer" >"$work/out" 2>&1; then
      echo "FAIL persistence-$writer-$fail_at" >&2; exit 1
    fi
  done
done
# Completion and recovery must propagate the same faults through their real
# functions, without recreating ownership after a durable but uncertain release.
# shellcheck source=../host/crowdsec-cutover-state.sh
source "$root/deploy/vn/host/crowdsec-cutover-state.sh"
id=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
tag=v0.8.9
cutover_run=$work/runtime
mkdir "$cutover_run"
load() { doc=$(cat "$file"); owner=$(jq -r '.operation_id // empty' <<<"$doc"); }
cmd_checkpoint() { load; [[ $owner == "$1" && $(jq -r .operation_tag <<<"$doc") == "$2" ]]; }
cutover_internal_lease() { return 0; }
cutover_mode() { return 0; }
cutover_paths() { return 0; }
cutover_stopped() { return 0; }
cutover_safe() { return 0; }
now() { printf '2026-10-21T00:00:00Z\n'; }
systemctl() { [[ $* != 'is-active --quiet aboutme-maintenance.service' ]]; }
# Only the fixed release-record path is redirected, in a private function copy.
sed -e 's|/var/lib/aboutme/release.json|"$work/release.json"|g' \
  -e 's|/run/aboutme/crowdsec-retention-ok|"$work/lease"|g' \
  "$root/deploy/vn/host/crowdsec-cutover-state.sh" >"$work/state"
# shellcheck disable=SC1091
source "$work/state"
# Reapply the boundary doubles after loading the real completion/recovery bodies.
cutover_internal_lease() { return 0; }
cutover_mode() { return 0; }
cutover_paths() { return 0; }
cutover_stopped() { return 0; }
cutover_safe() { return 0; }
cutover_validate_owner() { cmd_checkpoint "$1" "$2"; }
printf '{"release_tag":"v0.8.9","maintenance_tag":"v0.8.9"}\n' >"$work/release.json"
for fail_at in chmod file-sync rename directory-sync; do
  printf '{"minimum_release":8009,"operation_id":"%s","operation_tag":"%s","operation_kind":"cutover","crowdsec_cutover_started":true}\n' "$id" "$tag" >"$file"
  printf '{"operation_id":"%s","mutation_kind":"reset-db","state":"active"}\n' "$id" >"$mutation_file"
  rm -f -- "$work/release-write"
  restoring=no
  # Marker removal precedes the injected final owner-write failure.
  sync() {
    if [[ ${1-} == "$dir" ]]; then
      [[ $fail_at != directory-sync || ( ! -e $work/release-write && $restoring == no ) ]]
    else [[ $fail_at != file-sync ]]; fi
  }
  if (cutover_complete "$id" "$tag") >"$work/out" 2>&1; then
    echo "FAIL completion-persistence-$fail_at" >&2; exit 1
  fi
  load
  if [[ -z $owner ]]; then
    [[ ! -e $mutation_file ]] || { echo 'FAIL released-owner-recreated'; exit 1; }
  else
    [[ $owner == "$id" ]]
  fi
  printf '{"minimum_release":8009,"operation_id":"%s","operation_tag":"%s","operation_kind":"cutover"}\n' "$id" "$tag" >"$file"
  rm -f -- "$mutation_file"
  restoring=yes
  if (cutover_restore_marker "$id" "$tag") >"$work/out" 2>&1; then
    echo "FAIL marker-persistence-$fail_at" >&2; exit 1
  fi
done
echo 'PASS fence-persistence-failure-propagation'
