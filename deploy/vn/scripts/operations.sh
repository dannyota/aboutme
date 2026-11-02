#!/usr/bin/env bash
# Laptop coordinator for the reviewed account deletion protocol.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/../../.." && pwd)
lib=/usr/local/lib/aboutme
repo=dannyota/aboutme
umask 077
# shellcheck source=../host/operation-state.sh
source "$here/../host/operation-state.sh"
# shellcheck source=ssh-lib.sh
source "$here/ssh-lib.sh"
# shellcheck source=../../aws/scripts/provenance.sh
source "$here/../../aws/scripts/provenance.sh"
say() { printf '%s\n' refused >&2; }
fail() { printf '%s\n' "$1" >&2; exit "${2:-1}"; }
# Only releases whose operation protocol was reviewed belong here.
supported() { [[ $1 == v0.7.9 ]]; }
valid_id() { [[ $1 =~ ^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$ ]]; }
case "$#:${1:-}:${2:-}" in
  3:account-delete:show)
    [[ $3 =~ ^[a-z0-9]+(-[a-z0-9]+)*$ && ${#3} -ge 4 && ${#3} -le 30 ]] || fail invalid_request 2
    envelope=$(jq -cn --arg slug "$3" '{verb:"show",kind:"account-delete",slug:$slug}')
    ;;
  4:apply:*)
    valid_id "$2" && [[ $3 == --confirm && $4 == "delete-account:$2:"* && ${#4} -le 160 ]] || fail invalid_request 2
    envelope=$(jq -cn --arg id "$2" --arg text "$4" '{verb:"apply",requestId:$id,confirmation:$text}')
    ;;
  2:status:* | 2:resume:* | 2:verify:* | 2:abort:*)
    valid_id "$2" || fail invalid_request 2
    envelope=$(jq -cn --arg verb "$1" --arg id "$2" '{verb:$verb,requestId:$id}')
    ;;
  *) fail invalid_request 2 ;;
esac
envelope=$(operation_envelope "$envelope" 2>/dev/null) || fail invalid_request 2
[[ -n ${DEPLOY_HOST:-} && $DEPLOY_HOST =~ ^[A-Za-z0-9][A-Za-z0-9.:-]*$ ]] || fail invalid_request 2
[[ -z ${DEPLOY_SITE:-} || $DEPLOY_SITE == https://aboutme.vn ]] || fail invalid_request 2
# The first supporting release also proves clean current main before SSH.
bash "$root/scripts/release-eligibility.sh" --deploy-checkout v0.7.9 >/dev/null 2>&1 || fail refused
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
ssh_opts=(-p "$SSH_PORT" -o BatchMode=yes -o StrictHostKeyChecking=yes -o ForwardAgent=no -o ConnectTimeout=10)
remote() { ssh "${ssh_opts[@]}" "aboutme-admin@$DEPLOY_HOST" "$@"; }
# Check every installed source used by the operation, including generic jobs.
files=(
  host/operation-run.sh host/operation-state.sh host/operation-account.sh
  host/workload-lock.sh host/job-run.sh scripts/fence.sh scripts/deploy-safety.sh scripts/deploy-host.sh
  host/etc/systemd/system/aboutme-operation@.service
  host/etc/systemd/system/aboutme-operation-retention.service
  host/etc/systemd/system/aboutme-operation-retention.timer
  host/etc/systemd/system/aboutme-job@.service
  host/etc/systemd/system/aboutme-alert@.service host/bin/alert.sh
  host/bin/alert-telegram.sh host/telegram-state.sh
)
for source in "${files[@]}"; do
  name=${source##*/} target="$lib/$name"
  [[ $source != *systemd/system* ]] || target="/etc/systemd/system/$name"
  want=$(sha256sum "$here/../$source" | cut -d' ' -f1)
  printf -v command 'sha256sum %q' "$target"
  have=$(remote "$command" 2>/dev/null | cut -d' ' -f1) || fail transport_failed
  [[ $want == "$have" ]] || fail refused
done
release=$(remote "sudo $lib/deploy-host.sh - - release-json" 2>/dev/null) || fail transport_failed
tag=$(jq -er .release_tag <<<"$release") || fail protocol_failed
supported "$tag" || fail refused
commit=$(bash "$root/scripts/release-eligibility.sh" --deploy-checkout "$tag" 2>/dev/null) || fail refused
for component in server web caddy; do
  pin=$(jq -er --arg component "$component" '.[$component+"_image"]' <<<"$release") || fail protocol_failed
  [[ $pin =~ ^ghcr\.io/dannyota/aboutme-$component@(sha256:[0-9a-f]{64})$ ]] || fail protocol_failed
  digest=${BASH_REMATCH[1]}
  token=$(curl -fsS --max-time 30 "https://ghcr.io/token?scope=repository:$repo-$component:pull&service=ghcr.io" | jq -er .token) || fail refused
  platform=$(curl -fsS --max-time 30 --config <(printf 'header = "Authorization: Bearer %s"\n' "$token") \
    -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
    "https://ghcr.io/v2/$repo-$component/manifests/$tag" |
    jq -er '[.manifests[]? | select(.platform.os=="linux" and .platform.architecture=="amd64")] | select(length==1) | .[0].digest') || fail refused
  [[ $platform == "$digest" ]] || fail refused
  provenance_verify "$component" "$digest" "$tag" "$commit" >/dev/null 2>&1 || fail refused
done
rc=0
printf '%s\n' "$envelope" | remote "sudo $lib/operation-run.sh request" >"$work/reply" 2>"$work/error" || rc=$?
if ((rc != 0)); then
  case $rc in
    1)
      [[ ! -s $work/reply && $(wc -c <"$work/error") -le 32 ]] || fail protocol_failed
      IFS= read -r error <"$work/error" || fail protocol_failed
      [[ $(wc -c <"$work/error") == $((${#error}+1)) ]] || fail protocol_failed
      case $error in
        not_found | refused | protocol_failed) fail "$error" ;;
        *) fail protocol_failed ;;
      esac
      ;;
    2) fail invalid_request 2 ;;
    4) fail not_found 4 ;;
    255) fail transport_failed ;;
    *) fail protocol_failed ;;
  esac
fi
[[ $(wc -c <"$work/reply") -le 16384 ]] || fail protocol_failed
# Reuse the closed versioned validator without reading any host state locally.
operation_state wire '' 3<"$work/reply" 2>/dev/null || fail protocol_failed
