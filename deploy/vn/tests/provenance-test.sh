#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/bin"
cat >"$work/bin/gh" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$CALLS"
printf '%s' "${GH_OUT:-}"
exit "${GH_EXIT:-0}"
STUB
chmod +x "$work/bin/gh"

say() { printf '%s\n' "$*" >>"$work/said"; }
# shellcheck source=deploy/vn/scripts/provenance.sh
source "$repo_root/deploy/vn/scripts/provenance.sh"
PATH="$work/bin:$PATH"
export CALLS=$work/calls

hex=$(printf 'ab%062d' 7)
digest=sha256:$hex
commit=0123456789abcdef0123456789abcdef01234567
declare -A owner_id=([dannyota]=244348267 [dntmakers]=342323513)
attestation() {
  jq -cn --arg n "$1" --arg d "$2" --arg uri "https://github.com/$3/aboutme" --argjson rid "$4" --argjson oid "$5" \
    '{verificationResult:{statement:{subject:[{name:$n,digest:{sha256:$d}}]},
      signature:{certificate:{sourceRepositoryURI:$uri,sourceRepositoryIdentifier:$rid,sourceRepositoryOwnerIdentifier:$oid}}}}'
}
statement() {
  local owner=${3:-dannyota}
  attestation "$1" "$2" "$owner" '"1318874703"' "\"${owner_id[$owner]}\"" | jq -sc .
}

fail() { echo "provenance-test: $*" >&2; exit 1; }
expect() {
  local got=0
  : >"$CALLS"
  : >"$work/said"
  provenance_verify "$2" "$4" v1.2.3 "$5" || got=1
  [[ $got == "$3" ]] || fail "$1: status $got, want $3"
}

export GH_EXIT=0 GH_OUT
for owner in dannyota dntmakers; do
  repo=$owner/aboutme
  GH_OUT=$(statement "ghcr.io/$owner/aboutme-server" "$hex" "$owner")
  expect "ok $owner" server 0 "$digest" "$commit"
  want="attestation verify oci://ghcr.io/$owner/aboutme-server@$digest --repo $owner/aboutme"
  [[ $owner != dannyota ]] || want+=" --bundle-from-oci"
  want+=" --cert-identity https://github.com/$owner/aboutme/.github/workflows/release-images.yml@refs/tags/v1.2.3"
  want+=" --source-ref refs/tags/v1.2.3 --source-digest $commit --deny-self-hosted-runners --format json"
  [[ $(cat "$CALLS") == "$want" ]] || fail "ok $owner: gh called with '$(cat "$CALLS")'"
done

repo=dntmakers/aboutme
for case in other-id no-id numeric-id other-owner-id no-owner-id numeric-owner-id other-source no-certificate mixed; do
  good=$(attestation ghcr.io/dntmakers/aboutme-server "$hex" dntmakers '"1318874703"' '"342323513"')
  case $case in
    other-id) one=$(attestation ghcr.io/dntmakers/aboutme-server "$hex" dntmakers '"1318874704"' '"342323513"') ;;
    no-id) one=$(jq -c 'del(.verificationResult.signature.certificate.sourceRepositoryIdentifier)' <<<"$good") ;;
    numeric-id) one=$(attestation ghcr.io/dntmakers/aboutme-server "$hex" dntmakers 1318874703 '"342323513"') ;;
    other-owner-id) one=$(attestation ghcr.io/dntmakers/aboutme-server "$hex" dntmakers '"1318874703"' '"244348267"') ;;
    no-owner-id) one=$(jq -c 'del(.verificationResult.signature.certificate.sourceRepositoryOwnerIdentifier)' <<<"$good") ;;
    numeric-owner-id) one=$(attestation ghcr.io/dntmakers/aboutme-server "$hex" dntmakers '"1318874703"' 342323513) ;;
    other-source) one=$(attestation ghcr.io/dntmakers/aboutme-server "$hex" dannyota '"1318874703"' '"342323513"') ;;
    no-certificate) one=$(jq -c 'del(.verificationResult.signature)' <<<"$good") ;;
    mixed) one=$good$'\n'$(attestation ghcr.io/dntmakers/aboutme-web "$hex" dntmakers '"1318874704"' '"342323513"') ;;
  esac
  GH_OUT=$(jq -sc . <<<"$one")
  expect "identity $case" server 1 "$digest" "$commit"
  grep -qF "provenance: the build provenance for server@$digest names a different repository or owner" "$work/said" ||
    fail "identity $case: no message"
done

GH_OUT=$(statement ghcr.io/dannyota/aboutme-server "$hex" dntmakers)
expect subject_other_owner server 1 "$digest" "$commit"
grep -qF "names a different subject" "$work/said" || fail "subject_other_owner: no message"

repo=dannyota/aboutme
GH_OUT=$(statement ghcr.io/dannyota/aboutme-server "$hex" dntmakers)
expect dannyota_with_dntmakers_ids server 1 "$digest" "$commit"
GH_OUT=$(attestation ghcr.io/dannyota/aboutme-server "$hex" dannyota '"1318874704"' '"244348267"' | jq -sc .)
expect dannyota_other_id server 1 "$digest" "$commit"
grep -qF -- " --bundle-from-oci" "$CALLS" || fail "dannyota_other_id: no legacy bundle option"
grep -qF "provenance: the build provenance for server@$digest names a different repository or owner" "$work/said" ||
  fail "dannyota_other_id: no message"

for bad in evil/aboutme Dntmakers/aboutme DANNYOTA/aboutme dntmakers/other dntmakers dntmakers/aboutme/x '' 'dntmakers/aboutme '; do
  repo=$bad
  GH_OUT=$(statement ghcr.io/dntmakers/aboutme-server "$hex" dntmakers)
  expect "bad_repo '$bad'" server 1 "$digest" "$commit"
  [[ ! -s $CALLS ]] || fail "bad_repo '$bad': gh was called"
done
repo=dannyota/aboutme

GH_EXIT=1 GH_OUT=
expect gh_refuses server 1 "$digest" "$commit"
grep -qF "no valid build provenance for server@$digest from v1.2.3" "$work/said" || fail "gh_refuses: no message"
[[ $(wc -l <"$CALLS") == 1 ]] || fail "gh_refuses: gh called more than once"
grep -qF -e " --bundle-from-oci" "$CALLS" || fail "gh_refuses: no legacy bundle option"
repo=dntmakers/aboutme
expect gh_refuses_dntmakers server 1 "$digest" "$commit"
[[ $(wc -l <"$CALLS") == 1 ]] || fail "gh_refuses_dntmakers: gh called more than once"
grep -qF "no valid build provenance for server@$digest from v1.2.3" "$work/said" || fail "gh_refuses_dntmakers: no message"
! grep -qF -e "--bundle-from-oci" "$CALLS" || fail "gh_refuses_dntmakers: legacy bundle option"
repo=dannyota/aboutme
GH_EXIT=0

GH_OUT=$(statement ghcr.io/dannyota/aboutme-web "$hex")
expect wrong_name server 1 "$digest" "$commit"
grep -qF "names a different subject" "$work/said" || fail "wrong_name: no message"

GH_OUT=$(statement ghcr.io/dannyota/aboutme-server "$(printf 'cd%062d' 7)")
expect wrong_digest server 1 "$digest" "$commit"

GH_OUT=$(statement ghcr.io/dannyota/aboutme-server-evil "$hex")
expect prefix_name server 1 "$digest" "$commit"

for out in '[]' '{}' 'not json' ''; do
  GH_OUT=$out
  expect "empty_answer '$out'" server 1 "$digest" "$commit"
done

GH_OUT=$(statement ghcr.io/dannyota/aboutme-server "$hex")
for bad in "sha256:${hex:0:63}" "sha256:${hex^^}" "$hex" "sha512:$hex" "sha256:$hex --repo x/y"; do
  expect "bad_digest '$bad'" server 1 "$bad" "$commit"
  [[ ! -s $CALLS ]] || fail "bad_digest '$bad': gh was called"
done
for bad in "" "${commit:0:39}" "v1.2.3" "${commit^^}"; do
  expect "bad_commit '$bad'" server 1 "$digest" "$bad"
  [[ ! -s $CALLS ]] || fail "bad_commit '$bad': gh was called"
done

echo "provenance-test: ok"
