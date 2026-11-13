# shellcheck shell=bash

provenance_verify() {
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
  jq -e --arg n "ghcr.io/$repo-$name" --arg d "${digest#sha256:}" \
    'type == "array" and any(.[]; any(.verificationResult.statement.subject[]?; .name == $n and .digest.sha256 == $d))' \
    <<<"$out" >/dev/null ||
    { say "provenance: the build provenance for $name@$digest names a different subject"; return 1; }
}

digest() {
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
