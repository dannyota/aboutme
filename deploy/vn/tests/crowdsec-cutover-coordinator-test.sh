#!/usr/bin/env bash
# Execute the coordinator from a serving predecessor, with only remote effects
# adapted to a private host. Keep the real quiesce and CIMD start predicate.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
fixture=$work/repo
mkdir -p "$fixture/deploy/vn/scripts" "$fixture/deploy/vn/host" "$fixture/deploy/vn/edge" "$fixture/deploy/aws/scripts" "$fixture/scripts" "$work/bin" "$work/lib"
cp "$root/deploy/vn/scripts/crowdsec-cutover.sh" "$root/deploy/vn/scripts/ssh-lib.sh" "$fixture/deploy/vn/scripts/"
printf '#!/bin/sh\nprintf "reviewed-commit\\n"\n' >"$fixture/scripts/release-eligibility.sh"
printf 'provenance_verify() { return 0; }\n' >"$fixture/deploy/aws/scripts/provenance.sh"
sed -n '/^quiesce() {/,/^}/p' "$root/deploy/vn/host/crowdsec-cutover-host.sh" >"$work/quiesce"
sed -n '/^cimd_serving_app_stopped() {/,/^}/p; /^cimd_rollback_check() {/,/^}/p' "$root/deploy/vn/scripts/deploy-host.sh" >"$work/cimd"
printf 'aboutme_stop_jobs() { return 0; }\n' >"$work/lib/deploy-safety.sh"
printf 'cutover_stopped() { [[ $(cat "$COORDINATOR_FIXTURE/state") == inactive ]]; }\n' >"$work/lib/crowdsec-cutover-state.sh"
cat >"$work/bin/curl" <<'STUB'
#!/usr/bin/env bash
if [[ $* == *ghcr.io/token* ]]; then printf '{"token":"synthetic"}\n'
else printf '{"manifests":[{"platform":{"os":"linux","architecture":"amd64"},"digest":"sha256:%064d"}]}\n' 0; fi
STUB
cat >"$work/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
case $1 in
  show) if [[ $* == *LoadState* ]]; then printf 'loaded\n'; else cat "$COORDINATOR_FIXTURE/state"; fi ;;
  disable) : ;;
  stop)
    if [[ " $* " == *' aboutme-server.service '* ]]; then printf 'inactive\n' >"$COORDINATOR_FIXTURE/state"; fi ;;

  *) exit 1 ;;
esac
STUB
cat >"$work/bin/ssh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
command=${*: -1}
if [[ $command == 'sudo bash -c '* ]]; then
  cat >"$COORDINATOR_FIXTURE/transferred.tar"
  printf '/run/aboutme/crowdsec-bundle.abcdefgh\n'
  exit 0
fi
if [[ $command == 'sudo env ABOUTME_INSTALL_ONLY='* ]]; then echo stage >>"$COORDINATOR_FIXTURE/events"; echo lock >>"$COORDINATOR_FIXTURE/events"; [[ ${STAGE_REPLY_LOST:-} != yes ]] || exit 1; printf 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n'; exit 0; fi
# The exact quoted command originates in the copied trusted coordinator.
eval "set -- $command"
[[ $1 == sudo ]]; shift
script=${1##*/}; shift
case $script in
  fence.sh)
    case $1 in
      lock) echo lock >>"$COORDINATOR_FIXTURE/events"; printf 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n' ;;
      mutation-start) echo marker >>"$COORDINATOR_FIXTURE/events" ;;
      *) exit 1 ;;
    esac ;;
  crowdsec-cutover-host.sh)
    action=$1
    [[ $4 == /run/aboutme/crowdsec-bundle.abcdefgh && $5 == "$(sha256sum "$COORDINATOR_FIXTURE/transferred.tar" | cut -d' ' -f1)" ]]
    if [[ $action == quiesce ]]; then
      # shellcheck disable=SC1091
      source "$COORDINATOR_FIXTURE/quiesce"
      lib=$COORDINATOR_FIXTURE/lib
      checkpoint() { return 0; }
      quiesce
      echo quiesce >>"$COORDINATOR_FIXTURE/events"
    elif [[ $action == transition ]]; then
      [[ $(cat "$COORDINATOR_FIXTURE/state") == inactive ]]
      echo transition >>"$COORDINATOR_FIXTURE/events"
    else exit 1; fi ;;
  deploy-host.sh)
    case $3 in
      pull) echo pull >>"$COORDINATOR_FIXTURE/events" ;;
      cutover-render)
        # shellcheck disable=SC1091
        source "$COORDINATOR_FIXTURE/cimd"
        die() { exit 1; }
        templates=$COORDINATOR_FIXTURE
        cimd_rollback_check 8009
        echo render >>"$COORDINATOR_FIXTURE/events" ;;
      *) exit 1 ;;
    esac ;;
  *) exit 1 ;;
esac
STUB
chmod +x "$work/bin/"*
export COORDINATOR_FIXTURE=$work PATH=$work/bin:$PATH DEPLOY_HOST=fixture.invalid
printf 'active\n' >"$work/state"
bash "$fixture/deploy/vn/scripts/crowdsec-cutover.sh" v0.8.9 >"$work/out" 2>&1
[[ $(cat "$work/state") == inactive ]]
python3 - "$work/events" <<'PY'
import pathlib,sys
rows=pathlib.Path(sys.argv[1]).read_text().splitlines()
assert rows.index('marker')<rows.index('quiesce')<rows.index('render')<rows.index('transition')
assert rows.count('quiesce')==1
assert rows.count('lock')==1
assert rows.index('stage')<rows.index('lock')<rows.index('marker')
PY
# A lost stage/owner reply must not retry staging, acquire another owner, or
# begin mutation. The server-side owner is intentionally left for inspection.
cp "$work/events" "$work/success-events"
: >"$work/events"
if STAGE_REPLY_LOST=yes bash "$fixture/deploy/vn/scripts/crowdsec-cutover.sh" v0.8.9 >"$work/lost" 2>&1; then
  echo 'FAIL unknown-stage-reply'; exit 1
fi
[[ $(cat "$work/events") == $'stage\nlock' ]]
# The production verifier rejects a different release, archive, extracted byte,
# or mutable shared path before any consumer effect.
sed -n '/^bundle_verify() {/,/^}/p' "$root/deploy/vn/host/crowdsec-cutover-host.sh" >"$work/verify"
# shellcheck disable=SC1091
source "$work/verify"
mkdir -m 0700 "$work/bound"
printf 'candidate\n' >"$work/bound/code"
tar -C "$work/bound" -cf "$work/archive.tar" code
cp "$work/archive.tar" "$work/bound/archive.tar"
chmod 0600 "$work/bound/archive.tar"
bundle=$work/bound bundle_digest=$(sha256sum "$work/archive.tar" | cut -d' ' -f1)
tag=v0.8.9
[[ ${bundle_digest:?computed archive binding required} =~ ^[0-9a-f]{64}$ ]]
checkpoint() { [[ $tag == v0.8.9 ]]; }
# Root metadata is a production boundary. The static test adapts only metadata.
stat() { case ${*: -1} in "$bundle") printf '0:0:700\n' ;; "$bundle/archive.tar") printf '0:0:600:1\n' ;; *) command stat "$@" ;; esac; }
bundle_verify
for corruption in tag digest bytes; do
  case $corruption in
    tag) tag=v0.8.10 ;;
    digest) bundle_digest=$(printf '%064d' 0) ;;
    bytes) printf 'other candidate\n' >"$bundle/code" ;;
  esac
  if bundle_verify; then echo 'FAIL mutable-cutover-bundle'; exit 1; fi
  tag=v0.8.9 bundle_digest=$(sha256sum "$work/archive.tar" | cut -d' ' -f1)
done
# Execute the actual staging branch with its existing outer bundle flock and
# private destinations. A second candidate must refuse before any helper copy.
sed -n '/^crowdsec_stage() {/,/^}/p' "$root/deploy/vn/host/install.sh" |
  sed "s|/var/lib/aboutme|$work/host/state|g; s|/usr/local/lib/aboutme|$work/host/lib|g; s|/etc/|$work/host/etc/|g" >"$work/stage"
# shellcheck disable=SC1091
source "$work/stage"
mkdir -p "$work/host/state" "$work/host/lib"
printf '{}\n' >"$work/host/state/fence.json"
touch "$work/host/state/fence.lock"
cat >"$work/host/lib/fence.sh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ $1 == lock && $2 == cutover && $3 == v0.8.9 ]]
exec {guard}>>"$COORDINATOR_FIXTURE/host/state/fence.lock"
/usr/bin/flock -x "$guard"
jq -e '(.operation_id // null)==null' "$COORDINATOR_FIXTURE/host/state/fence.json" >/dev/null
printf '{"operation_id":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}\n' >"$COORDINATOR_FIXTURE/host/state/fence.json"
printf 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n'
STUB
chmod 0755 "$work/host/lib/fence.sh"
install() { printf '%s\n' "$bundle" >>"$work/helper-copies"; }
say() { :; }
die() { exit 1; }
stage_tag=v0.8.9
[[ ${stage_tag:?fixture release binding required} == v0.8.9 ]]
(
  exec {operation_bundle_fd}>>"$work/bundle.lock"
  /usr/bin/flock -x "$operation_bundle_fd"
  crowdsec_stage
) >"$work/stage-owner"
[[ $(cat "$work/stage-owner") == AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA ]]
cp "$work/helper-copies" "$work/before-second-stage"
cp "$bundle/code" "$work/first-code"
if (
  exec {operation_bundle_fd}>>"$work/bundle.lock"
  /usr/bin/flock -x "$operation_bundle_fd"
  bundle=$work/second-candidate stage_tag=v0.8.10
  crowdsec_stage
); then echo 'FAIL second-stage-owned'; exit 1; fi
cmp "$work/helper-copies" "$work/before-second-stage"
cmp "$bundle/code" "$work/first-code"
echo 'PASS coordinator-quiesces-and-binds-reviewed-bundle'
