#!/usr/bin/env bash
# Reports Podman's running manifest digests without reading secrets or release
# intent (docs/design/deployment-transparency/host.md). Runs as root on the
# host; tests supply a temporary output directory and a stubbed Podman.
set -euo pipefail
umask 077

die() { echo "deployment-document: $1" >&2; exit 1; }
(($# <= 2)) || die 'usage: deployment-document.sh [directory [region]]'
directory=${1:-/var/lib/aboutme/deployment}
region=${2:-HCM03}
[[ $region =~ ^[A-Z]{2,4}[0-9]{2}$ ]] || die 'invalid region'
[[ -d $directory && ! -L $directory ]] || die 'public directory is missing'

# Writers serialize without involving the application deployment lock. The
# temporary file lives beside the public file, so rename is atomic.
exec 9>"$directory/.writer.lock"
flock -n 9 || die 'another reporter is running'
temporary=$(mktemp "$directory/.deployment.XXXXXX")
trap 'rm -f "$temporary"' EXIT
observed=$(date -u +%Y-%m-%dT%H:%M:%SZ)
stale=$(date -u -d "$observed +180 seconds" +%Y-%m-%dT%H:%M:%SZ)
components='[]'

for component in server web caddy maintenance; do
  exists=0
  podman container exists "aboutme-$component" >/dev/null 2>&1 || exists=$?
  case $exists in
    0)
      # ImageDigest is the running container's manifest digest, not its image
      # configuration ID or the image its name would pull today. A bare
      # StartedAt prints Go's spaced time format, so format it as RFC 3339.
      observation=$(podman container inspect --format \
        '{{.State.Running}} {{.ImageDigest}} {{.State.StartedAt.UTC.Format "2006-01-02T15:04:05.999999999Z07:00"}}' \
        "aboutme-$component" 2>/dev/null) || die 'container inspection failed'
      read -r running digest started extra <<<"$observation" || die 'malformed observation'
      [[ -z $extra && $observation != *$'\n'* ]] || die 'malformed observation'
      case $running in true | false) ;; *) die 'invalid container state' ;; esac
      ;;
    1) running=false ;;
    *) die 'container lookup failed' ;;
  esac
  images='[]'
  if [[ $running == true ]]; then
    [[ $digest =~ ^sha256:[0-9a-f]{64}$ ]] || die 'invalid running digest'
    [[ $started =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?(Z|[+-][0-9]{2}:[0-9]{2})$ ]] ||
      die 'invalid container start time'
    since=$(date -u -d "$started" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null) || die 'invalid container start time'
    [[ $since =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] ||
      die 'invalid normalized start time'
    images=$(jq -cn --arg d "$digest" --arg s "$since" '
      [{digest:$d, replicas:1, running_since:$s, version:null, commit:null,
        signature:{status:"unchecked", checked_at:null, signer_workflow:null, transparency_log_index:null},
        sbom:{status:"unchecked", format:null},
        links:{commit:null, release:null, build:null,
          provenance:("https://api.github.com/repos/dannyota/aboutme/attestations/"+$d+"?predicate_type=provenance"),
          sbom:null, transparency_log:null}}]')
  elif [[ $component == maintenance ]]; then
    continue
  fi
  image=$component
  [[ $component != maintenance ]] || image=caddy
  components=$(jq -cn --argjson c "$components" --arg n "$component" \
    --arg i "ghcr.io/dannyota/aboutme-$image" --argjson images "$images" \
    '$c + [{name:$n, image:$i, running_images:$images}]')
done

jq -n --arg region "$region" --arg observed "$observed" --arg stale "$stale" \
  --argjson components "$components" '
  {schema_version:1, project:"aboutme", environment:"production", site:"https://aboutme.vn",
    source_repository:"https://github.com/dannyota/aboutme",
    platform:{provider:"greennode", orchestrator:"podman", region:$region},
    observed_at:$observed, stale_after:$stale,
    summary:(if any($components[]; .name != "maintenance" and (.running_images | length) == 0)
      then "mismatch" else "unverified" end),
    release:null, components:$components,
    observer:{version:null, commit:null, source:"self-reported"}}
' >"$temporary" 2>/dev/null || die 'document construction failed'
[[ $(wc -c <"$temporary") -le 65536 ]] || die 'document exceeds size limit'
chmod 0644 "$temporary"
sync "$temporary"
mv -f "$temporary" "$directory/deployment.json"
sync "$directory"
