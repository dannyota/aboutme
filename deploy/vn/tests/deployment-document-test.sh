#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/public"
printf 'secret-canary-not-a-release\n' >"$work/release.json"
export CALLS=$work/calls PODMAN_CASE=running
cat >"$work/bin/podman" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$CALLS"
[[ $1 == container ]]
case $2 in
  exists)
    case "$PODMAN_CASE:${3}" in
      error:*) exit 125 ;;
      running:aboutme-maintenance | missing:aboutme-web) exit 1 ;;
    esac ;;
  inspect)
    # No full inspect may read or print environment secrets.
    [[ $3 == --format && $4 == '{{.State.Running}} {{.ImageDigest}} {{.State.StartedAt}}' ]]
    if [[ $PODMAN_CASE == inspect-error ]]; then
      echo 'secret-canary-from-podman-error' >&2; exit 125
    fi
    running=true
    [[ $PODMAN_CASE != stopped || $5 != aboutme-web ]] || running=false
    digest=sha256:$(printf '%064d' 7)
    [[ $PODMAN_CASE != malformed ]] || digest=sha256:bad
    stamp=2026-10-11T08:14:05.123456789+07:00
    [[ $PODMAN_CASE != bad-time ]] || stamp=secret-canary
    [[ $PODMAN_CASE != extra-field ]] || stamp+=' secret-canary'
    printf '%s %s %s\n' "$running" "$digest" "$stamp" ;;
  *) exit 99 ;;
esac
STUB
chmod +x "$work/bin/podman"
export PATH="$work/bin:$PATH"
generator=$root/deploy/vn/host/deployment-document.sh
generate() { bash "$generator" "$work/public" HCM03 >"$work/out" 2>"$work/err"; }
fail() { echo "deployment-document-test: $*" >&2; exit 1; }

generate
doc=$work/public/deployment.json
jq -e '
  .schema_version == 1 and .platform == {provider:"greennode",orchestrator:"podman",region:"HCM03"}
  and .summary == "unverified" and .release == null
  and (.components | map(.name)) == ["server","web","caddy"]
  and all(.components[].running_images[];
    .digest == ("sha256:" + ("0" * 63) + "7") and .replicas == 1
    and .running_since == "2026-10-11T01:14:05Z"
    and .version == null and .commit == null
    and .signature.status == "unchecked" and .sbom.status == "unchecked"
    and .links.commit == null and .links.build == null and .links.sbom == null)
  and ((.stale_after | fromdateiso8601) - (.observed_at | fromdateiso8601)) == 180
' "$doc" >/dev/null || fail 'running state or evidence is wrong'
[[ $(stat -c %a "$doc") == 644 ]] || fail 'document is not public-readable'
[[ ! -s $work/out && ! -s $work/err ]] || fail 'successful generator printed output'
# The running digest differs from the intended release; release.json must not
# be read. A canary elsewhere in the directory must never enter the document.
cp "$doc" "$work/previous"
for PODMAN_CASE in error inspect-error malformed bad-time extra-field; do
  export PODMAN_CASE
  if generate; then fail "accepted $PODMAN_CASE"; fi
  cmp -s "$doc" "$work/previous" || fail "replaced good document on $PODMAN_CASE"
  if rg -q 'secret-canary' "$doc" "$work/out" "$work/err"; then fail 'leaked raw data'; fi
  [[ $(find "$work/public" -name '.deployment.*' | wc -l) == 0 ]] || fail 'left a temporary file'
done
if flock -o "$work/public/.writer.lock" bash "$generator" "$work/public" HCM03 \
  >"$work/out" 2>"$work/err"; then fail 'accepted a concurrent writer'; fi
cmp -s "$doc" "$work/previous" || fail 'changed document while locked'
for PODMAN_CASE in missing stopped; do
  export PODMAN_CASE
  generate
  jq -e '.summary == "mismatch" and .components[1].running_images == []' "$doc" >/dev/null
done
PODMAN_CASE=maintenance generate
jq -e '.components[3].name == "maintenance" and .components[3].image == "ghcr.io/dannyota/aboutme-caddy"' \
  "$doc" >/dev/null
if bash "$generator" "$work/public" 'HCM03 secret-canary' >"$work/out" 2>"$work/err"; then
  fail 'accepted invalid region'
fi
if rg -q 'secret-canary' "$work/out" "$work/err"; then fail 'printed invalid input'; fi
for unit in aboutme-caddy aboutme-maintenance; do
  rg -q '^Volume=/var/lib/aboutme/deployment:/srv/deployment:ro$' \
    "$root/deploy/vn/host/quadlet/$unit.container.in"
done
echo 'deployment document tests passed'
