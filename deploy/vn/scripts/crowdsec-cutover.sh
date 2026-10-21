#!/usr/bin/env bash
# The PostgreSQL transition retains its fence until the separate proof completes.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=ssh-lib.sh
source "$here/ssh-lib.sh"
# shellcheck source=../../aws/scripts/provenance.sh
source "$here/../../aws/scripts/provenance.sh"
host=${DEPLOY_HOST:?set DEPLOY_HOST}
lib=/usr/local/lib/aboutme
case "$#:${1:-}" in
  1:v*) tag=$1 mode=transition ;;
  3:--complete) mode=complete tag=$2 op=$3 ;;
  *) echo 'usage: crowdsec-cutover.sh <tag> | --complete <tag> <owner>' >&2; exit 2 ;;
esac
[[ $tag =~ ^v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})$ ]] || exit 2
commit=$(bash "$here/../../../scripts/release-eligibility.sh" --deploy-checkout "$tag")
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
remote() {
  local command
  printf -v command '%q ' "$lib/$1" "${@:2}"
  ssh -p "$SSH_PORT" -o BatchMode=yes "aboutme-admin@$host" "sudo $command"
}
if [[ $mode == complete ]]; then
  [[ $op =~ ^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$ ]] || exit 2
  remote fence.sh cutover-complete "$op" "$tag"
  printf 'PASS cutover-completed\n'
  exit 0
fi
# Resolve and authenticate each exact platform image with the release's existing
# verifier. Only immutable digests reach host rendering.
declare -A images
repo=dannyota/aboutme
for name in server web caddy; do
  token=$(curl -fsS "https://ghcr.io/token?scope=repository:$repo-$name:pull&service=ghcr.io" | jq -er .token)
  digest=$(curl -fsS -H "Authorization: Bearer $token" \
    -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
    "https://ghcr.io/v2/$repo-$name/manifests/$tag" |
    jq -er '[.manifests[]? | select(.platform.os=="linux" and .platform.architecture=="amd64")]
      | select(length==1) | .[0].digest | select(test("^sha256:[0-9a-f]{64}$"))')
  provenance_verify "$name" "$digest" "$tag" "$commit"
  images[$name]=ghcr.io/$repo-$name@$digest
done
# Each transfer receives a private root directory. Verify exact archive bytes
# before extraction; no competing candidate can replace this invocation's tree.
tar -C "$here/.." -cf "$work/bundle.tar" host scripts edge
bundle_digest=$(sha256sum "$work/bundle.tar" | cut -d' ' -f1)
printf -v transfer 'sudo bash -c %q bash %q' 'set -euo pipefail; umask 077; bundle=$(mktemp -d /run/aboutme/crowdsec-bundle.XXXXXXXX); cat >"$bundle/archive.tar"; printf "%s  %s\n" "$1" "$bundle/archive.tar" | sha256sum --check --status; tar -C "$bundle" -xf "$bundle/archive.tar"; printf "%s\n" "$bundle"' "$bundle_digest"
bundle=$(ssh -p "$SSH_PORT" -o BatchMode=yes "aboutme-admin@$host" "$transfer" <"$work/bundle.tar")
[[ $bundle =~ ^/run/aboutme/crowdsec-bundle\.[A-Za-z0-9]{8}$ ]] || exit 1
# The installer retains its existing bundle lock until the ordinary canonical
# lock helper acquires the owner. An unknown reply never triggers a retry.
printf -v staging 'sudo env ABOUTME_INSTALL_ONLY=crowdsec-stage bash %q %q %q %q' "$bundle/host/install.sh" "$bundle" "$tag" "$bundle_digest"
op=$(ssh -p "$SSH_PORT" -o BatchMode=yes "aboutme-admin@$host" "$staging")
[[ $op =~ ^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$ ]] || exit 1
# Failure always retains exact ownership; uncertain SSH replies never trigger a
# release or a database retry. Inspection and forward recovery own that result.
remote fence.sh mutation-start "$op" "$tag" reset-db
remote deploy-host.sh "$op" "$tag" pull "${images[server]}" "${images[web]}" "${images[caddy]}"
remote crowdsec-cutover-host.sh quiesce "$op" "$tag" "$bundle" "$bundle_digest"
remote deploy-host.sh "$op" "$tag" cutover-render "${images[server]}" "${images[web]}" "${images[caddy]}"
remote crowdsec-cutover-host.sh transition "$op" "$tag" "$bundle" "$bundle_digest"
printf 'PASS cutover-serving-fenced\n'
printf 'owner %s\n' "$op"
