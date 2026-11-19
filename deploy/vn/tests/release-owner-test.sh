#!/usr/bin/env bash
set -euo pipefail
umask 077
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail() { echo "release-owner-test: $*" >&2; exit 1; }
host=$root/deploy/vn/scripts/deploy-host.sh
wrapper=$root/deploy/vn/scripts/crowdsec-privacy-notice.sh
primitive=$root/deploy/vn/host/crowdsec-privacy-notice-host.sh
notice=$work/notice
mkdir -p "$notice/units"

accepted=(
  'dannyota dannyota dannyota dannyota'
  'dntmakers dntmakers dntmakers dntmakers'
  'dannyota dannyota dannyota dntmakers'
  'dntmakers dntmakers dntmakers dannyota'
)
refused=(
  'dntmakers dannyota dannyota dannyota'
  'dannyota dntmakers dannyota dannyota'
  'dannyota dannyota dntmakers dannyota'
  'dntmakers dntmakers dannyota dntmakers'
  'dannyota dntmakers dntmakers dntmakers'
  'evil evil evil evil'
  'dannyota dannyota dannyota evil'
  'Dntmakers Dntmakers Dntmakers Dntmakers'
  'dannyota dannyota dannyota DANNYOTA'
  'dannyota/aboutme dannyota/aboutme dannyota/aboutme dannyota/aboutme'
)
record() {
  jq -cn --arg s "$1" --arg w "$2" --arg c "$3" --arg m "$4" --arg d "$(printf '%064d' 0)" '
    {release_tag:"v0.9.34",release_number:9034,maintenance_tag:"v0.9.33",maintenance_number:9033,
     server_image:("ghcr.io/"+$s+"/aboutme-server@sha256:"+$d),web_image:("ghcr.io/"+$w+"/aboutme-web@sha256:"+$d),
     caddy_image:("ghcr.io/"+$c+"/aboutme-caddy@sha256:"+$d),maintenance_image:("ghcr.io/"+$m+"/aboutme-caddy@sha256:"+$d)}'
}

release_shape=$(sed -n "/^release_shape=/,/end'/p" "$host" | sed -e "1s/^release_shape='//" -e "\$s/'\$//")
[[ $release_shape == *'malformed release state'* ]] || fail 'deploy-host.sh has no release shape'
host_record() { jq -e "$release_shape" <<<"$1" >/dev/null 2>&1; }

# shellcheck source=deploy/vn/host/operation-state.sh
source "$root/deploy/vn/host/operation-state.sh"
operation_record() {
  local reply
  reply=$(jq -cn --argjson release "$1" '{schemaVersion:1,requestId:("A"*43),kind:"account-delete",phase:"closed",
    outcome:"complete",release:$release,expiresAt:null,confirmation:null,
    action:{userId:null,reportedSlug:null,shownSlugs:[],returnedSlugs:[],writeOutcome:"committed",
      publicVerification:"passed",physicalCleanup:"complete"},challenge:null,recoveryCopy:null}')
  operation_state wire '' 3<<<"$reply" >/dev/null 2>&1
}

{
  grep -E '^readonly (publication_date|content_sha256)=' "$wrapper"
  sed -n '/^notice_manifest() {/,/^}/p; /^notice_release_number() {/,/^}/p; /^notice_release_read() {/,/^}/p' "$wrapper"
  sed -n '/^notice_installed_admission() {/,/^}/p' "$primitive"
} | sed -e "s|/var/lib/aboutme/release.json|$notice/release.json|g" \
  -e "s|/etc/containers/systemd/|$notice/units/|g" >"$work/notice-functions"
grep -qF "$notice/release.json" "$work/notice-functions" || fail 'notice validators read another release path'
# shellcheck disable=SC1091
source "$work/notice-functions"
notice_recheck_mount() { :; }
notice_safe_file() { [[ -f $1 && ! -L $1 ]]; }
notice_fixture() {
  printf '%s\n' "$1" >"$notice/release.json"
  printf '[Container]\nImage=%s\nEnvironment=DEPLOY_RELEASE_TAG=v0.9.34\n' "$(jq -r .server_image <<<"$1")" \
    >"$notice/units/aboutme-server.container"
  printf '[Container]\nImage=%s\n' "$(jq -r .web_image <<<"$1")" >"$notice/units/aboutme-web.container"
}
notice_record() { notice_fixture "$1" && (notice_release_read) >/dev/null 2>&1; }
notice_admission() { notice_fixture "$1" && [[ $( (notice_installed_admission) 2>/dev/null) == v0.9.34 ]]; }

python3 - "$root/deploy/vn/host/operation-state.sh" "$work/state.py" <<'PY'
import pathlib, sys
source = pathlib.Path(sys.argv[1]).read_text().split("<<'PY'\n", 1)[1].split('\nPY\n', 1)[0]
pathlib.Path(sys.argv[2]).write_text(source.replace('s.st_uid != 0', 's.st_uid != os.getuid()'))
PY
stored=$work/stored
stored_record() {
  local id=$2
  rm -rf "$stored"
  mkdir -m 0700 "$stored" "$stored/$id"
  jq -cn --arg id "$id" --argjson release "$1" --arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
    {schemaVersion:1,requestId:$id,kind:"account-delete",phase:"closed",outcome:"complete",createdAt:$at,
    expiresAt:null,planningDeadline:$at,evidenceAt:$at,confirmation:null,flagsHash:("a"*64),dbSecretId:"synthetic",
    release:$release,action:{userId:null,reportedSlug:null,shownSlugs:[],returnedSlugs:[],writeOutcome:"committed",
      publicVerification:"passed",physicalCleanup:"complete"},
    challenge:null,recoveryCopy:null,proofReceipt:null,attemptCount:0,attempts:[],intent:"none",pendingVerb:"none",
    fenceHeld:false,reserved:false,closureQuiescent:true}' >"$stored/$id/state.json"
  python3 "$work/state.py" prune '' "$stored" >/dev/null 2>&1
}

failures=0
check() {
  local want=$1 validator=$2 owners=$3 got=accepted
  read -r -a owner <<<"$owners"
  "$validator" "$(record "${owner[@]}")" || got=refused
  if [[ $got != "$want" ]]; then
    echo "$validator $got owners $owners" >&2
    failures=1
  fi
}
for validator in host_record operation_record notice_record; do
  for owners in "${accepted[@]}"; do check accepted "$validator" "$owners"; done
  for owners in "${refused[@]}"; do check refused "$validator" "$owners"; done
done
for owners in "${accepted[@]}"; do
  read -r -a owner <<<"$owners"
  stored_record "$(record "${owner[@]}")" "$(printf '%042dA' 1)" || { echo "the retention timer refused stored owners $owners" >&2; failures=1; }
done
for owners in "${refused[@]}"; do
  read -r -a owner <<<"$owners"
  if stored_record "$(record "${owner[@]}")" "$(printf '%042dA' 2)"; then
    echo "the retention timer accepted stored owners $owners" >&2
    failures=1
  fi
done
for server in dannyota dntmakers; do check accepted notice_admission "$server dannyota dannyota dannyota"; done
for server in evil Dntmakers DANNYOTA dannyota/aboutme; do check refused notice_admission "$server dannyota dannyota dannyota"; done
((failures == 0)) || fail 'a release record validator disagrees with the owner rules'
echo 'release owner records passed'
