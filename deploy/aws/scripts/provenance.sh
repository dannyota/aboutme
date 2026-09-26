# shellcheck shell=bash
# Build provenance check, sourced by deploy.sh (which defines say and repo).
# Before a deploy uses a digest, provenance_verify requires the signed SLSA
# provenance that release-images.yml attested for it: signed for this exact
# tag by that workflow on a GitHub-hosted runner, from the commit the tag
# names, with the image's own name as the subject. See
# docs/design/deployment-transparency/verification.md.

provenance_verify() { # name digest tag commit
  local name=$1 digest=$2 tag=$3 commit=$4 out
  [[ $digest =~ ^sha256:[0-9a-f]{64}$ ]] || { say "provenance: $name has a malformed digest"; return 1; }
  [[ $commit =~ ^[0-9a-f]{40}$ ]] || { say "provenance: $tag does not name a full commit"; return 1; }
  out=$(gh attestation verify "oci://ghcr.io/$repo-$name@$digest" \
    --repo "$repo" \
    --cert-identity "https://github.com/$repo/.github/workflows/release-images.yml@refs/tags/$tag" \
    --source-ref "refs/tags/$tag" \
    --source-digest "$commit" \
    --deny-self-hosted-runners \
    --format json) ||
    { say "provenance: no valid build provenance for $name@$digest from $tag"; return 1; }
  # gh checks the digest; the subject name must also be this image, so one
  # image's digest never passes as another's.
  jq -e --arg n "ghcr.io/$repo-$name" --arg d "${digest#sha256:}" \
    'type == "array" and any(.[]; any(.verificationResult.statement.subject[]?; .name == $n and .digest.sha256 == $d))' \
    <<<"$out" >/dev/null ||
    { say "provenance: the build provenance for $name@$digest names a different subject"; return 1; }
}
