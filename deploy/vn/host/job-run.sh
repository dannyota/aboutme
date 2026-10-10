#!/usr/bin/env bash
# Runs one scheduled job with the current release's server image, for
# aboutme-job@<name>.service (docs/design/vietnam-production.md, "Scheduled
# jobs"). Installed at /usr/local/lib/aboutme/job-run.sh; runs as root.
#
# Database jobs get no network; media jobs join the aboutme bridge for
# vStorage ("Host": only media jobs get outbound network). Every job reaches
# PostgreSQL only through the mounted socket, as aboutme_app. All jobs get the
# media settings because the server's configuration requires the key pair
# whenever MEDIA_ENDPOINT is set; a job without network cannot use them.
set -euo pipefail

lib=/usr/local/lib/aboutme

if [[ ${1-} != --locked ]]; then
  exec "$lib/workload-lock.sh" 3600 "$0" --locked "$@"
fi
shift

name=${1-}
case $name in
  idempotency-expiry-sweep | privacy-retention-sweep) network=none ;;
  media-deletion-sweep | media-orphan-sweep) network=aboutme ;;
  *)
    echo "job-run: unknown job '$name'" >&2
    exit 2
    ;;
esac

# deploy-host.sh owns the one parser and shape check for the release state.
release=$("$lib/deploy-host.sh" - - release-json) || {
  echo "job-run: no valid release is installed" >&2
  exit 1
}
tag=$(jq -r .release_tag <<<"$release")
number=$(jq -r .release_number <<<"$release")
image=$(jq -r .server_image <<<"$release")

"$lib/fence.sh" check "$number" "aboutme-job@$name"

exec podman run --rm --name "aboutme-job-$name" \
  --network "$network" \
  --entrypoint /usr/local/bin/server \
  --memory 256m \
  --log-driver journald \
  --volume /run/postgresql:/run/postgresql \
  --env-file <(grep '^MEDIA_' /etc/aboutme/server.env) \
  --env DATABASE_URL='postgres://aboutme_app@/aboutme?host=/run/postgresql' \
  --env DEPLOY_RELEASE_TAG="$tag" \
  --env DEPLOY_RELEASE_NUMBER="$number" \
  --secret db-app-password,type=env,target=PGPASSWORD \
  --secret media-access-key-id,type=env,target=MEDIA_ACCESS_KEY_ID \
  --secret media-secret-access-key,type=env,target=MEDIA_SECRET_ACCESS_KEY \
  "$image" "$name"
