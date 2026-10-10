# shellcheck shell=bash
# Release image resolution and build provenance check, sourced by deploy.sh
# (which defines say, repo, and tag) and observer.sh.
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

# The digest of the linux/arm64 image the tag names. A release tag names an
# image index of linux/arm64 and linux/amd64 (docs/design/vietnam-production.md,
# "Host"), and this host runs the arm64 image, so the deploy pins that platform
# digest, never the index. An older release tag names the arm64 image itself.
digest() { # image name -> sha256:...
  local token url headers type
  token=$(curl -fsS "https://ghcr.io/token?scope=repository:$repo-$1:pull&service=ghcr.io" | jq -r .token)
  url="https://ghcr.io/v2/$repo-$1/manifests/$tag"
  headers=$(curl -fsSI -H "Authorization: Bearer $token" \
    -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json' \
    "$url" | tr -d '\r') || return
  type=$(awk -F': ' 'tolower($1) == "content-type" { print $2 }' <<<"$headers")
  case $type in
    application/vnd.oci.image.index.v1+json | application/vnd.docker.distribution.manifest.list.v2+json)
      curl -fsS -H "Authorization: Bearer $token" -H "Accept: $type" "$url" |
        jq -r '[.manifests[]? | select(.platform.os == "linux" and .platform.architecture == "arm64")]
          | if length == 1 then .[0].digest else empty end' ;;
    *) awk -F': ' 'tolower($1) == "docker-content-digest" { print $2 }' <<<"$headers" ;;
  esac
}
