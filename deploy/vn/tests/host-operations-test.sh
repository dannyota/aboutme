#!/usr/bin/env bash
set -euo pipefail
umask 077
here=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=../host/operation-state.sh
source "$here/host/operation-state.sh"
fail() { echo "host-operations-test: $1" >&2; exit 1; }
valid_id=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
valid_uuid=018f5b6a-9a3e-7c21-8b1e-000000000030
operation_id_valid "$valid_id" || fail id
for id in '' ../escape "${valid_id}A" AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB; do
  if operation_id_valid "$id"; then fail 'noncanonical id accepted'; fi
done
for envelope in \
  '{"verb":"show","kind":"account-delete","slug":"valid-slug"}' \
  "{\"verb\":\"apply\",\"requestId\":\"$valid_id\",\"confirmation\":\"delete-account:$valid_id:$valid_uuid:valid-slug\"}" \
  "{\"verb\":\"status\",\"requestId\":\"$valid_id\"}"; do
  operation_envelope "$envelope" >/dev/null || fail 'valid envelope refused'
done
for envelope in \
  '{"verb":"show","kind":"account-delete","slug":"../bad"}' \
  '{"verb":"show","kind":"account-delete","slug":"valid-slug","extra":true}' \
  '{"verb":"show","verb":"status","kind":"account-delete","slug":"valid-slug"}' \
  '{"verb":"plan","kind":"totp-rotate"}' \
  "{\"verb\":\"run\",\"requestId\":\"$valid_id\"}" \
  "{\"verb\":\"apply\",\"requestId\":\"$valid_id\",\"confirmation\":\"delete-account:$valid_id:$valid_uuid:other-slug\"}"; do
  if operation_envelope "$envelope" >/dev/null 2>&1; then
    # A different valid slug is syntax-valid; binding is checked against saved state.
    [[ $envelope == *other-slug* ]] || fail 'invalid envelope accepted'
  fi
done
# Invocation controls must stay outside generic jobs and their media secrets.
! rg -q 'account-delete|media-deletion-status' "$here/host/job-run.sh" || fail 'generic job allowlist expanded'
for text in '--network none' '--memory-swap 256m' '--log-driver none' '--ulimit core=0:0' '--cap-drop ALL' '--read-only' '--security-opt no-new-privileges'; do
  rg -q -- "$text" "$here/host/operation-account.sh" || fail "missing isolation: $text"
done
! rg -q 'secret (media-|smtp-|google-|linkedin-)' "$here/host/operation-account.sh" || fail 'credential overreach'
rg -q 'v0.7.9' "$here/scripts/operations.sh" || fail 'supporting release missing'
rg -q 'StrictHostKeyChecking=yes' "$here/scripts/operations.sh" || fail 'unpinned SSH'
rg -q 'ForwardAgent=no' "$here/scripts/operations.sh" || fail 'SSH forwarding enabled'
rg -q 'KillMode=control-group' "$here/host/etc/systemd/system/aboutme-operation@.service" || fail 'unit kill boundary'
rg -q 'Restart=no' "$here/host/etc/systemd/system/aboutme-operation@.service" || fail 'unit automatic replay'
echo 'host operation grammar and isolation passed'

# Bound input before looking at a target or calling a remote command.
for verb in show apply status resume verify abort proof; do
  if operation_envelope "{\"verb\":\"$verb\",\"requestId\":\"$valid_id\",\"extra\":true}" >/dev/null 2>&1; then fail 'extra fields accepted'; fi
done
if operation_envelope "$(printf '%05000d' 0)" >/dev/null 2>&1; then fail 'oversized envelope accepted'; fi
for envelope in \
  "{\"verb\":\"apply\",\"requestId\":\"$valid_id\",\"confirmation\":\"delete-account:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBA:$valid_uuid:valid-slug\"}" \
  '{"verb":"show","kind":"account-delete","slug":"admin"}' \
  '{"verb":"show","kind":"account-delete","slug":"UPPER"}'; do
  if operation_envelope "$envelope" >/dev/null 2>&1; then fail 'invalid target binding accepted'; fi
done
# The host must not accept an arbitrary supported command over the generic unit.
for text in 'operation-run.sh run %i' 'TimeoutStartSec=45min' 'TimeoutStopSec=60s' 'LimitCORE=0' 'StandardOutput=null' 'StandardError=null'; do
  rg -qF "$text" "$here/host/etc/systemd/system/aboutme-operation@.service" || fail "missing unit limit: $text"
done
for text in '--secret db-app-password,type=env,target=PGPASSWORD' '--volume /run/postgresql:/run/postgresql' '--restart no'; do
  rg -qF -- "$text" "$here/host/operation-account.sh" || fail 'container authority changed'
done
for text in operation-run.sh operation-state.sh operation-account.sh aboutme-operation@.service aboutme-operation-retention.service aboutme-operation-retention.timer; do
  rg -qF "$text" "$here/scripts/operations.sh" || fail 'bundle hash omission'
done
rg -qF 'mountpoint -q /srv/data' "$here/scripts/fence.sh" || fail 'missing mount accepted'
rg -qF 'key-change.json' "$here/scripts/fence.sh" || fail 'marker not checked'
rg -qF 'operation_release_guard' "$here/scripts/fence.sh" || fail 'direct release bypass'
echo 'host operation bounds and bundle checks passed'

# Run the real wrapper in a private checkout with fixed synthetic eligibility,
# provenance, registry and SSH replies. No network or host state is accessed.
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/client/deploy/vn" "$work/client/deploy/aws/scripts" "$work/client/scripts" "$work/bin"
cp -R "$here/host" "$here/scripts" "$work/client/deploy/vn/"
cat >"$work/client/scripts/release-eligibility.sh" <<'STUB'
#!/usr/bin/env bash
[[ ${REJECT_ELIGIBILITY:-0} == 0 ]] || exit 1
printf '%040d\n' 0
STUB
cat >"$work/client/deploy/aws/scripts/provenance.sh" <<'STUB'
provenance_verify() { [[ ${REJECT_PROVENANCE:-0} == 0 ]]; }
STUB
cat >"$work/bin/ssh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf 'ssh\n' >>"$CLIENT_FIXTURE/calls"
command=${!#}
case $command in
  sha256sum*)
    [[ ${REJECT_HASH:-0} == 0 ]] || { echo wrong; exit 0; }
    target=${command#sha256sum }
    name=${target##*/}
    match=$(find "$CLIENT_FIXTURE/client/deploy/vn" -type f -name "$name" -print -quit)
    sha256sum "$match"
    ;;
  *'deploy-host.sh - - release-json') cat "$CLIENT_FIXTURE/release" ;;
  *'operation-run.sh request')
    cat >"$CLIENT_FIXTURE/envelope"
    case ${SSH_LOSS:-none} in
      before) exit 255 ;;
      after) touch "$CLIENT_FIXTURE/dispatched"; exit 255 ;;
      *) cat "$CLIENT_FIXTURE/reply" ;;
    esac
    ;;
  *) exit 1 ;;
esac
STUB
cat >"$work/bin/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
case ${!#} in
  *'/token?'*) echo '{"token":"synthetic-public-pull"}' ;;
  *) printf '{"manifests":[{"platform":{"os":"linux","architecture":"amd64"},"digest":"sha256:%064d"}]}\n' 0 ;;
esac
STUB
chmod +x "$work/bin/"* "$work/client/scripts/release-eligibility.sh"
export CLIENT_FIXTURE=$work
export PATH=$work/bin:$PATH DEPLOY_HOST=fixture.example SSH_PORT=22922
jq -cn '{release_tag:"v0.7.9",release_number:7009,maintenance_tag:"v0.7.9",maintenance_number:7009,
 server_image:("ghcr.io/dannyota/aboutme-server@sha256:"+("0"*64)),web_image:("ghcr.io/dannyota/aboutme-web@sha256:"+("0"*64)),
 caddy_image:("ghcr.io/dannyota/aboutme-caddy@sha256:"+("0"*64)),maintenance_image:("ghcr.io/dannyota/aboutme-caddy@sha256:"+("0"*64))}' >"$work/release"
wrapper=$work/client/deploy/vn/scripts/operations.sh
if REJECT_ELIGIBILITY=1 bash "$wrapper" status "$valid_id" >/dev/null 2>&1; then fail 'ineligible checkout accepted'; fi
[[ ! -e $work/calls ]] || fail 'SSH preceded eligibility'
if REJECT_HASH=1 bash "$wrapper" status "$valid_id" >/dev/null 2>&1; then fail 'wrong bundle hash accepted'; fi
[[ ! -e $work/envelope ]] || fail 'wrong bundle reached dispatch'
if REJECT_PROVENANCE=1 bash "$wrapper" status "$valid_id" >/dev/null 2>&1; then fail 'wrong provenance accepted'; fi
[[ ! -e $work/envelope ]] || fail 'wrong provenance reached dispatch'
for loss in before after; do
  rm -f "$work/envelope" "$work/dispatched"
  if SSH_LOSS=$loss bash "$wrapper" resume "$valid_id" >"$work/output" 2>"$work/error"; then fail 'lost SSH returned success'; fi
  [[ $(<"$work/error") == transport_failed && ! -s $work/output ]] || fail 'transport diagnostic leaked'
  [[ $(jq -r .verb "$work/envelope") == resume ]] || fail 'transport loss replayed apply'
  if [[ $loss == before ]]; then [[ ! -e $work/dispatched ]] || fail 'pre-dispatch loss ran unit'
  else [[ -e $work/dispatched ]] || fail 'post-dispatch fixture failed'; fi
done
echo 'host operation eligibility, hash and SSH-loss cases passed'

# Exercise the installer's unit inventory guard without touching host paths.
install_guard=$(sed -n '/^operation_units=/,/^# Validate the saved protocol/p' "$here/host/install.sh" | sed '$d')
for unit_state in inactive failed active activating deactivating unknown; do
  (
    die() { exit 1; }
    systemctl() { printf 'aboutme-operation@%s.service loaded %s dead active request description\n' "$valid_id" "$unit_state"; }
    eval "$install_guard"
  ) >"$work/install-output" 2>&1 && accepted=true || accepted=false
  case $unit_state in
    inactive | failed) [[ $accepted == true ]] || fail 'stopped listed operation blocked installation' ;;
    *) [[ $accepted == false ]] || fail 'unresolved listed operation allowed installation' ;;
  esac
done

# The real HTTPS proof submits a fixed receipt against synthetic discovery.
cp "$here/../../scripts/host-operations-proof.sh" "$work/client/scripts/"
cat >"$work/client/deploy/vn/scripts/operations.sh" <<'STUB'
#!/usr/bin/env bash
cat "$CLIENT_FIXTURE/proof-status"
STUB
cat >"$work/bin/ssh" <<'STUB'
#!/usr/bin/env bash
cat >"$CLIENT_FIXTURE/proof-receipt"
STUB
cat >"$work/bin/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
output=''
while (($# > 1)); do
  if [[ $1 == --output ]]; then output=$2; shift; fi
  shift
done
case $1 in
  */sitemap.xml) cp "$CLIENT_FIXTURE/sitemap" "$output"; printf 200 ;;
  */llms.txt) cp "$CLIENT_FIXTURE/llms" "$output"; printf 200 ;;
  */readyz | */healthz) : >"$output"; printf 200 ;;
  *'/api/v1/public/showcase?page=1') printf '{"items":[],"pageCount":0}' >"$output"; printf 200 ;;
  *) : >"$output"; printf 404 ;;
esac
STUB
jq -cn --arg user "$valid_uuid" '{challenge:{nonce:"synthetic",phase:"serving"},action:{userId:$user,shownSlugs:["adam"],returnedSlugs:[]}}' >"$work/proof-status"
for discovery in prefix sitemap llms escaped malformed; do
  printf '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://aboutme.vn/adam-long</loc></url></urlset>\n' >"$work/sitemap"
  printf '# aboutme.vn\n\n## Resumes\n\n- [adam-long](https://aboutme.vn/adam-long.md)\n' >"$work/llms"
  expected=fail
  case $discovery in
    prefix) expected=pass ;;
    sitemap) sed -i 's|/adam-long<|/adam<|' "$work/sitemap" ;;
    llms) sed -i 's|/adam-long.md)|/adam.md)|' "$work/llms" ;;
    escaped) sed -i 's|/adam-long<|/a\&#100;am<|' "$work/sitemap" ;;
    malformed) printf '<urlset' >"$work/sitemap" ;;
  esac
  bash "$work/client/scripts/host-operations-proof.sh" "$valid_id" >/dev/null || fail 'synthetic public proof refused'
  [[ $(jq -r .result "$work/proof-receipt") == "$expected" ]] || fail "discovery URL equality: $discovery"
done
echo 'host operation install states and exact discovery URLs passed'
