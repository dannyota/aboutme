# shellcheck shell=bash
# shellcheck disable=SC2154 # The sourcing script sets repo.

provenance_verify() {
  local name=$1 digest=$2 tag=$3 commit=$4 out owner_id
  local -a repo_args
  case $repo in
    dannyota/aboutme) owner_id=244348267 repo_args=(--repo "$repo" --bundle-from-oci) ;;
    dntmakers/aboutme) owner_id=342323513 repo_args=(--repo "$repo") ;;
    *) say "provenance: $repo is not an accepted repository"; return 1 ;;
  esac
  [[ $digest =~ ^sha256:[0-9a-f]{64}$ ]] || { say "provenance: $name has a malformed digest"; return 1; }
  [[ $commit =~ ^[0-9a-f]{40}$ ]] || { say "provenance: $tag does not name a full commit"; return 1; }
  out=$(gh attestation verify "oci://ghcr.io/$repo-$name@$digest" \
    "${repo_args[@]}" \
    --cert-identity "https://github.com/$repo/.github/workflows/release-images.yml@refs/tags/$tag" \
    --source-ref "refs/tags/$tag" \
    --source-digest "$commit" \
    --deny-self-hosted-runners \
    --format json) ||
    { say "provenance: no valid build provenance for $name@$digest from $tag"; return 1; }
  jq -e --arg uri "https://github.com/$repo" --arg owner_id "$owner_id" '
    type == "array" and length > 0 and all(.[]; .verificationResult.signature.certificate
      | .sourceRepositoryIdentifier == "1318874703" and .sourceRepositoryOwnerIdentifier == $owner_id
        and .sourceRepositoryURI == $uri)' <<<"$out" >/dev/null ||
    { say "provenance: the build provenance for $name@$digest names a different repository or owner"; return 1; }
  jq -e --arg n "ghcr.io/$repo-$name" --arg d "${digest#sha256:}" \
    'type == "array" and any(.[]; any(.verificationResult.statement.subject[]?; .name == $n and .digest.sha256 == $d))' \
    <<<"$out" >/dev/null ||
    { say "provenance: the build provenance for $name@$digest names a different subject"; return 1; }
}
