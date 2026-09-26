#!/usr/bin/env bash
# Updates the deployment observer to a tagged release
# (docs/design/deployment-transparency/README.md, "Where the observer runs").
#
#   observer.sh <tag>
#
# Verifies the tag's ghcr.io/dannyota/aboutme-observer digest has build
# provenance signed for that tag and its commit (the sourced provenance.sh),
# copies it byte for byte into the aboutme-prod-observer ECR repository,
# fails unless ECR holds the same digest, then points the observer function
# at that digest. Every AWS call runs as the deploy role, reached through the
# operator role (the sourced fence.sh). Before OpenTofu has created the
# function, it stops after the copy and prints the digest to set as
# observer_image_digest. Observer updates are separate from application
# deploys; this script never touches ECS.
set -euo pipefail

region=ap-southeast-1
site_alarm_region=us-east-1
repo=dannyota/aboutme
ecr_repository=aboutme-prod-observer
function_name=aboutme-prod-observer
operation_kind=observer
first=0

say() { printf 'observer: %s\n' "$*" >&2; }
[[ $# == 1 ]] || { echo "usage: observer.sh <tag>" >&2; exit 2; }
tag=$1
[[ $tag =~ ^v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})$ ]] ||
  { say "$tag is not a strict vMAJOR.MINOR.PATCH tag"; exit 1; }
release_number() {
  [[ $1 =~ ^v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})$ ]] || return 1
  echo $((10#${BASH_REMATCH[1]} * 1000000 + 10#${BASH_REMATCH[2]} * 1000 + 10#${BASH_REMATCH[3]}))
}
candidate=$(release_number "$tag")

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=provenance.sh
source "$script_dir/provenance.sh"

# 1. The tag must be on main; its image must carry provenance for it.
git fetch -q origin main
commit=$(git rev-list -n1 "$tag")
git merge-base --is-ancestor "$commit" origin/main || { say "$tag is not on main"; exit 1; }

token=$(curl -fsS "https://ghcr.io/token?scope=repository:$repo-observer:pull&service=ghcr.io" | jq -r .token)
digest=$(curl -fsSI -H "Authorization: Bearer $token" \
  -H 'Accept: application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json' \
  "https://ghcr.io/v2/$repo-observer/manifests/$tag" |
  tr -d '\r' | awk -F': ' 'tolower($1) == "docker-content-digest" { print $2 }')
[[ $digest =~ ^sha256:[0-9a-f]{64}$ ]] || { say "no single-platform observer image for $tag"; exit 1; }
provenance_verify observer "$digest" "$tag" "$commit" || exit 1
say "verified ghcr.io/$repo-observer@$digest for $tag"

# 2. Operator then deploy role.
# shellcheck source=fence.sh
source "$script_dir/fence.sh"
registry="$account_id.dkr.ecr.$region.amazonaws.com"

# 3. Copy by digest. Tags are immutable, so a tag already in ECR must hold
# this exact digest; a copy that would change the digest fails.
existing=$(aws_ ecr describe-images --repository-name "$ecr_repository" --image-ids "imageTag=$tag" \
  --query 'imageDetails[0].imageDigest' --output text 2>"$work/describe.err") || existing=""
if [[ -z $existing ]] && ! grep -q ImageNotFoundException "$work/describe.err"; then
  say "could not read the $ecr_repository repository: $(cat "$work/describe.err")"
  exit 1
fi
if [[ -n $existing && $existing != "$digest" ]]; then
  say "ECR already holds $tag as $existing, not $digest"
  exit 1
fi
if [[ -z $existing ]]; then
  # The registry password goes to a private auth file, never argv.
  aws_ ecr get-login-password >"$work/ecr-password"
  jq -n --arg r "$registry" --rawfile p "$work/ecr-password" \
    '{auths: {($r): {auth: ("AWS:" + ($p | rtrimstr("\n")) | @base64)}}}' >"$work/auth.json"
  rm -f "$work/ecr-password"
  chmod 600 "$work/auth.json"
  skopeo copy --preserve-digests --retry-times 3 --dest-authfile "$work/auth.json" \
    "docker://ghcr.io/$repo-observer@$digest" "docker://$registry/$ecr_repository:$tag"
  rm -f "$work/auth.json"
fi
copied=$(aws_ ecr describe-images --repository-name "$ecr_repository" --image-ids "imageTag=$tag" \
  --query 'imageDetails[0].imageDigest' --output text)
[[ $copied == "$digest" ]] || { say "ECR holds $tag as $copied, not $digest"; exit 1; }
say "ECR holds $registry/$ecr_repository@$digest"

# 4. Point the function at the copy by digest.
if ! aws_ lambda get-function --function-name "$function_name" --query 'Configuration.FunctionName' \
  --output text >/dev/null 2>"$work/get.err"; then
  if grep -q ResourceNotFoundException "$work/get.err"; then
    say "no $function_name function yet: set observer_image_digest = \"$digest\" in prod.tfvars and run tofu apply"
    exit 0
  fi
  say "could not read the $function_name function: $(cat "$work/get.err")"
  exit 1
fi
aws_ lambda update-function-code --function-name "$function_name" \
  --image-uri "$registry/$ecr_repository@$digest" >/dev/null
aws_ lambda wait function-updated-v2 --function-name "$function_name"
running=$(aws_ lambda get-function --function-name "$function_name" --query 'Code.ImageUri' --output text)
[[ $running == "$registry/$ecr_repository@$digest" ]] ||
  { say "the function runs $running, not $digest"; exit 1; }
say "the observer function runs $tag; set observer_image_digest = \"$digest\" in prod.tfvars"
