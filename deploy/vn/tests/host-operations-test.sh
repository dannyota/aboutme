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
! grep -qE 'account-delete|media-deletion-status' "$here/host/job-run.sh" || fail 'generic job allowlist expanded'
grep -qF -- '--no-healthcheck' "$here/host/job-run.sh" || fail 'scheduled jobs inherit image healthchecks'
for text in '--no-healthcheck' '--network none' '--memory-swap 256m' '--log-driver none' '--ulimit core=0:0' '--cap-drop ALL' '--read-only' '--security-opt no-new-privileges'; do
  grep -qF -- "$text" "$here/host/operation-account.sh" || fail "missing isolation: $text"
done
! grep -qE 'secret (media-|smtp-|google-|linkedin-)' "$here/host/operation-account.sh" || fail 'credential overreach'
! grep -qE -- '--deploy-checkout v[0-9]' "$here/scripts/operations.sh" || fail 'checkout proof pinned to a fixed release'
# The wrapper and the host share one release check; neither pins its own.
[[ $(grep -c '^operation_release_supported() {' "$here/host/operation-state.sh") == 1 ]] || fail 'shared release check missing'
grep -qxF 'operation_release_supported "$tag" || fail refused' "$here/scripts/operations.sh" || fail 'wrapper skips the shared release check'
grep -qxF '  operation_release_supported "$tag" || return 1' "$here/host/operation-account.sh" || fail 'host skips the shared release check'
! grep -qE 'v0\.7\.[0-9]|7009|_supported\(\)' "$here/scripts/operations.sh" "$here/host/operation-account.sh" "$here/host/operation-run.sh" ||
  fail 'release pinned outside the shared check'
grep -qF 'StrictHostKeyChecking=yes' "$here/scripts/operations.sh" || fail 'unpinned SSH'
grep -qF 'ForwardAgent=no' "$here/scripts/operations.sh" || fail 'SSH forwarding enabled'
grep -qF 'KillMode=control-group' "$here/host/etc/systemd/system/aboutme-operation@.service" || fail 'unit kill boundary'
grep -qF 'Restart=no' "$here/host/etc/systemd/system/aboutme-operation@.service" || fail 'unit automatic replay'
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
  grep -qF "$text" "$here/host/etc/systemd/system/aboutme-operation@.service" || fail "missing unit limit: $text"
done
for text in '--secret db-app-password,type=env,target=PGPASSWORD' '--volume /run/postgresql:/run/postgresql' '--restart no'; do
  grep -qF -- "$text" "$here/host/operation-account.sh" || fail 'container authority changed'
done
for text in operation-run.sh operation-state.sh operation-account.sh aboutme-operation@.service aboutme-operation-retention.service aboutme-operation-retention.timer; do
  grep -qF "$text" "$here/scripts/operations.sh" || fail 'bundle hash omission'
done
grep -qF 'mountpoint -q /srv/data' "$here/scripts/fence.sh" || fail 'missing mount accepted'
grep -qF 'key-change.json' "$here/scripts/fence.sh" || fail 'marker not checked'
grep -qF 'operation_release_guard' "$here/scripts/fence.sh" || fail 'direct release bypass'
echo 'host operation bounds and bundle checks passed'

# Run the real wrapper in a private checkout with fixed synthetic eligibility,
# provenance, registry and SSH replies. No network or host state is accessed.
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/client/deploy/vn" "$work/client/deploy/aws/scripts" "$work/client/scripts" "$work/bin"
cp -R "$here/host" "$here/scripts" "$work/client/deploy/vn/"
cat >"$work/client/scripts/release-eligibility.sh" <<'STUB'
#!/usr/bin/env bash
printf 'eligibility %s\n' "$*" >>"$CLIENT_FIXTURE/calls"
[[ ${REJECT_ELIGIBILITY:-0} == 0 ]] || exit 1
[[ $# == 1 || ${REJECT_TAG_ELIGIBILITY:-0} == 0 ]] || exit 1
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
      *)
        cat "$CLIENT_FIXTURE/reply"
        [[ ! -f $CLIENT_FIXTURE/host-error ]] || cat "$CLIENT_FIXTURE/host-error" >&2
        exit "${HOST_RC:-0}"
        ;;
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
  *) printf '{"manifests":[{"platform":{"os":"linux","architecture":"amd64"},"digest":"sha256:%064d"}]}\n' "${REGISTRY_DIGEST:-0}" ;;
esac
STUB
chmod +x "$work/bin/"* "$work/client/scripts/release-eligibility.sh"
export CLIENT_FIXTURE=$work
export PATH=$work/bin:$PATH DEPLOY_HOST=fixture.example SSH_PORT=22922
release_fixture() { # tag number
  jq -cn --arg tag "$1" --argjson number "$2" '{release_tag:$tag,release_number:$number,maintenance_tag:$tag,maintenance_number:$number,
   server_image:("ghcr.io/dannyota/aboutme-server@sha256:"+("0"*64)),web_image:("ghcr.io/dannyota/aboutme-web@sha256:"+("0"*64)),
   caddy_image:("ghcr.io/dannyota/aboutme-caddy@sha256:"+("0"*64)),maintenance_image:("ghcr.io/dannyota/aboutme-caddy@sha256:"+("0"*64))}' >"$work/release"
}
release_fixture v0.8.29 8029
wrapper=$work/client/deploy/vn/scripts/operations.sh
if REJECT_ELIGIBILITY=1 bash "$wrapper" status "$valid_id" >/dev/null 2>&1; then fail 'ineligible checkout accepted'; fi
[[ $(<"$work/calls") == 'eligibility --deploy-checkout' ]] || fail 'SSH preceded the tagless checkout proof'
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

# Remote command failures keep their closed diagnostic. SSH errors alone use
# transport_failed; malformed remote errors never expose raw diagnostics.
: >"$work/reply"
for verb in status resume verify abort apply; do
  args=("$verb" "$valid_id")
  [[ $verb != apply ]] || args+=(--confirm "delete-account:$valid_id:$valid_uuid:valid-slug")
  printf 'not_found\n' >"$work/host-error"
  rc=0
  HOST_RC=1 bash "$wrapper" "${args[@]}" >"$work/output" 2>"$work/error" || rc=$?
  [[ $rc == 1 && ! -s $work/output ]] || fail "unknown $verb exit or stdout"
  cmp -s "$work/host-error" "$work/error" || fail "unknown $verb diagnostic"
done
for code in refused protocol_failed; do
  printf '%s\n' "$code" >"$work/host-error"
  rc=0
  HOST_RC=1 bash "$wrapper" status "$valid_id" >"$work/output" 2>"$work/error" || rc=$?
  [[ $rc == 1 && ! -s $work/output ]] || fail 'host failure exit or stdout'
  cmp -s "$work/host-error" "$work/error" || fail 'host failure became transport failure'
done
for host_rc in 2 4 255 3; do
  case $host_rc in
    2) expected=invalid_request; expected_rc=2 ;;
    4) expected=not_found; expected_rc=4 ;;
    255) expected=transport_failed; expected_rc=1 ;;
    *) expected=protocol_failed; expected_rc=1 ;;
  esac
  rc=0
  HOST_RC=$host_rc bash "$wrapper" status "$valid_id" >"$work/output" 2>"$work/error" || rc=$?
  [[ $rc == "$expected_rc" && ! -s $work/output && $(<"$work/error") == "$expected" ]] || fail "remote exit mapping: $host_rc"
done
for malformed in empty unknown multiline oversized no-newline nul stdout; do
  : >"$work/reply"
  case $malformed in
    empty) : >"$work/host-error" ;;
    unknown) printf 'raw fixture diagnostic\n' >"$work/host-error" ;;
    multiline) printf 'not_found\nraw fixture diagnostic\n' >"$work/host-error" ;;
    oversized) printf '%040d\n' 0 >"$work/host-error" ;;
    no-newline) printf refused >"$work/host-error" ;;
    nul) printf 'refused\0\n' >"$work/host-error" ;;
    stdout) printf 'not_found\n' >"$work/host-error"; printf '{}\n' >"$work/reply" ;;
  esac
  rc=0
  HOST_RC=1 bash "$wrapper" status "$valid_id" >"$work/output" 2>"$work/error" || rc=$?
  [[ $rc == 1 && ! -s $work/output && $(<"$work/error") == protocol_failed ]] || fail "malformed failure accepted: $malformed"
done
rm "$work/host-error"
jq -cn --arg id "$valid_id" --arg user "$valid_uuid" --slurpfile release "$work/release" '
  {schemaVersion:1,requestId:$id,kind:"account-delete",phase:"planned",outcome:"pending",release:$release[0],
   expiresAt:"2099-01-01T00:00:00Z",confirmation:("delete-account:"+$id+":"+$user+":valid-slug"),
   action:{userId:$user,reportedSlug:"valid-slug",shownSlugs:["valid-slug"],returnedSlugs:null,
     writeOutcome:"not_started",publicVerification:"pending",physicalCleanup:"pending"},challenge:null,recoveryCopy:null}' >"$work/reply"
bash "$wrapper" status "$valid_id" >"$work/output" 2>"$work/error" || fail 'known status reply refused'
[[ ! -s $work/error && $(jq -cS . "$work/output") == "$(jq -cS . "$work/reply")" ]] || fail 'known status reply changed'
echo 'host operation remote errors and known status passed'

# The checkout proof names no tag and precedes SSH; eligibility then binds the
# release the host reports. Every release from v0.7.9 on passes the gate.
call_order() { uniq "$work/calls" | tr '\n' ' '; }
known_reply() { [[ ! -s $work/error && $(jq -cS . "$work/output") == "$(jq -cS . "$work/known-reply")" ]]; }
cp "$work/reply" "$work/known-reply"
for release in v0.7.9:7009 v0.7.10:7010 v0.8.29:8029 v1.0.0:1000000 v999.999.999:999999999; do
  tag=${release%:*}
  release_fixture "$tag" "${release#*:}"
  rm -f "$work/calls"
  bash "$wrapper" status "$valid_id" >"$work/output" 2>"$work/error" || fail "supported release refused: $tag"
  known_reply || fail "supported release reply changed: $tag"
  [[ $(call_order) == "eligibility --deploy-checkout ssh eligibility --deploy-checkout $tag ssh " ]] ||
    fail "checkout proof, hashes, deployed tag eligibility or dispatch out of order: $tag"
done
for tag in v0.7.8 v0.6.999 v0.0.0 '' v0.8 v0.8.29.1 v0.8.29-rc1 v0.8.29+build 'v0.8.29 ' v08.29.0 v0.08.29 V0.8.29 0.8.29 v1000.0.0 'v0.8.29;true'; do
  release_fixture "$tag" 0
  rm -f "$work/calls" "$work/envelope"
  rc=0
  bash "$wrapper" status "$valid_id" >"$work/output" 2>"$work/error" || rc=$?
  [[ $rc == 1 && ! -s $work/output && $(<"$work/error") == refused && ! -e $work/envelope ]] || fail "unsupported release accepted: $tag"
  [[ $(call_order) == 'eligibility --deploy-checkout ssh ' ]] || fail "unsupported release reached eligibility: $tag"
done
release_fixture v0.8.29 8029

# Every check after the host reports its release still refuses before dispatch.
post_ssh_refused() { # case expected-error
  local rc=0
  rm -f "$work/calls" "$work/envelope"
  bash "$wrapper" status "$valid_id" >"$work/output" 2>"$work/error" || rc=$?
  [[ $rc == 1 && ! -s $work/output && $(<"$work/error") == "$2" && ! -e $work/envelope ]] || fail "$1 reached dispatch"
  grep -qx ssh "$work/calls" || fail "$1 refused before SSH"
}
REJECT_HASH=1 post_ssh_refused 'wrong bundle hash' refused
REJECT_TAG_ELIGIBILITY=1 post_ssh_refused 'ineligible deployed tag' refused
[[ $(call_order) == 'eligibility --deploy-checkout ssh eligibility --deploy-checkout v0.8.29 ' ]] || fail 'deployed tag eligibility not checked'
REGISTRY_DIGEST=1 post_ssh_refused 'registry digest mismatch' refused
REJECT_PROVENANCE=1 post_ssh_refused 'wrong provenance' refused
jq -c '.web_image="ghcr.io/dannyota/aboutme-web:latest"' "$work/release" >"$work/pin" && mv "$work/pin" "$work/release"
post_ssh_refused 'unpinned image' protocol_failed
release_fixture v0.8.29 8029
jq -c '. + {extra:true}' "$work/known-reply" >"$work/reply"
rc=0
bash "$wrapper" status "$valid_id" >"$work/output" 2>"$work/error" || rc=$?
[[ $rc == 1 && ! -s $work/output && $(<"$work/error") == protocol_failed ]] || fail 'open reply accepted'
cp "$work/known-reply" "$work/reply"
echo 'host operation release gate and post-SSH checks passed'

# The real eligibility gate checks a private copy of the release repository.
# It must refuse an unclean, off-main or stale checkout before any SSH.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_COMMON_DIR
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_COUNT=1 \
  GIT_CONFIG_KEY_0="url.$work/release.git.insteadOf" GIT_CONFIG_VALUE_0=https://github.com/dannyota/aboutme.git \
  GIT_AUTHOR_NAME=fixture GIT_AUTHOR_EMAIL=fixture@example.invalid GIT_COMMITTER_NAME=fixture GIT_COMMITTER_EMAIL=fixture@example.invalid
cp "$here/../../scripts/release-eligibility.sh" "$work/client/scripts/"
cat >"$work/bin/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
while (($# > 1)); do [[ $1 != --commit ]] || commit=$2; shift; done
jq -n --arg sha "$commit" --arg conclusion "${CI_CONCLUSION:-success}" \
  '[{databaseId:1,headSha:$sha,event:"push",headBranch:"main",status:"completed",conclusion:$conclusion}]'
STUB
chmod +x "$work/bin/gh"
client() { git -C "$work/client" "$@"; }
release_repo() { git -C "$work/release.git" "$@"; }
client init -q -b main
client add -A
client commit -q -m fixture
client tag v0.8.29
git init -q --bare "$work/release.git"
client push -q "$work/release.git" main v0.8.29
reviewed=$(client rev-parse HEAD)
checkout_refused() { # case
  local rc=0
  rm -f "$work/calls" "$work/envelope"
  bash "$wrapper" status "$valid_id" >"$work/output" 2>"$work/error" || rc=$?
  [[ $rc == 1 && ! -s $work/output && $(<"$work/error") == refused ]] || fail "$1 checkout accepted"
  [[ ! -e $work/calls ]] || fail "$1 checkout reached SSH"
}
: >"$work/client/untracked"
checkout_refused untracked
rm "$work/client/untracked"
printf '# local edit\n' >>"$work/client/deploy/aws/scripts/provenance.sh"
checkout_refused modified
client checkout -q -- deploy/aws/scripts/provenance.sh
client checkout -q -b feature
checkout_refused 'non-main branch'
client checkout -q main
client checkout -q --detach
checkout_refused detached
client checkout -q main
# A stale local origin/main equal to HEAD must not pass once main moves on.
client update-ref refs/remotes/origin/main HEAD
release_repo update-ref refs/heads/main "$(release_repo commit-tree -p "$reviewed" -m next "$reviewed^{tree}")"
checkout_refused 'behind origin/main'
release_repo update-ref refs/heads/main "$reviewed"
rm -f "$work/calls"
bash "$wrapper" status "$valid_id" >"$work/output" 2>"$work/error" || fail 'clean current main checkout refused'
known_reply || fail 'clean current main reply changed'
# The real gate still checks the deployed tag after SSH.
release_repo update-ref refs/tags/v0.8.30 "$(release_repo commit-tree -m off-main "$reviewed^{tree}")"
release_fixture v0.8.30 8030
post_ssh_refused 'off-main deployed tag' refused
release_fixture v0.8.29 8029
CI_CONCLUSION=failure post_ssh_refused 'red deployed tag CI' refused
echo 'host operation checkout proof passed'

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
