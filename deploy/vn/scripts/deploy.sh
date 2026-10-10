#!/usr/bin/env bash
# Deploys a tagged release to the Vietnam production host
# (docs/design/vietnam-production.md, "Deploy"; release fence in
# docs/design/passkey-release-fence.md). Runs from a current main checkout on
# the laptop and drives deploy-host.sh and fence.sh on the host over SSH.
#
#   deploy.sh <tag>                  normal deploy
#   deploy.sh <tag> --first-deploy   also runs db-setup before migrate
#   deploy.sh --rollback <tag>       earlier images; no backup, no migration
#   deploy.sh --activate <tag>       raises the release fence; changes nothing else
#
# Environment: DEPLOY_HOST (address of the host, required), DEPLOY_SITE
# (default https://aboutme.vn; a rehearsal sets its own origin), and
#
# Recovery follows docs/design/single-host-production.md, "Release and
# deploy": a failure before any migration request restores the previous
# release beside maintenance; once a migration may have run, maintenance stays
# up with the app and timers stopped, and recovery is a forward fix or a
# point-in-time restore. The operation lock is released on every exit.
set -euo pipefail

repo=dannyota/aboutme
admin=aboutme-admin
lib=/usr/local/lib/aboutme
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# shellcheck source=../../aws/scripts/provenance.sh
source "$here/../../aws/scripts/provenance.sh"
exec 9>&2
say() { printf 'deploy: %s\n' "$*" >&9; }
usage() {
  echo "usage: deploy.sh <tag> [--first-deploy] | deploy.sh --rollback <tag> | deploy.sh --activate <tag>" >&2
  exit 2
}

first=0 rollback=0 activate=0
case "$#:${1:-}:${2:-}" in
  1:[!-]*:) tag=$1 ;;
  2:[!-]*:--first-deploy) first=1 tag=$1 ;;
  2:--rollback:?*) rollback=1 tag=$2 ;;
  2:--activate:?*) activate=1 tag=$2 ;;
  *) usage ;;
esac
kind=deploy
((!rollback)) || kind=rollback
((!activate)) || kind=activate

release_number() {
  [[ $1 =~ ^v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})$ ]] || return 1
  echo $((10#${BASH_REMATCH[1]} * 1000000 + 10#${BASH_REMATCH[2]} * 1000 + 10#${BASH_REMATCH[3]}))
}
candidate=$(release_number "$tag") || {
  say "$tag is not a strict vMAJOR.MINOR.PATCH tag"
  exit 1
}

host=${DEPLOY_HOST:?set DEPLOY_HOST to the production host address}
site=${DEPLOY_SITE:-https://aboutme.vn}
[[ $site =~ ^https://[a-z0-9.-]+$ ]] || {
  say "DEPLOY_SITE must be an https origin with no path"
  exit 1
}

work=$(mktemp -d)
# One SSH connection for the whole run, so a hardware key is touched once.
ssh_opts=(-o BatchMode=yes -o ControlMaster=auto -o "ControlPath=$work/ssh-%C" -o ControlPersist=10m)
ssh_close() { ssh "${ssh_opts[@]}" -O exit "$admin@$host" 2>/dev/null || true; }

# Runs one host script as root. Every argument is a validated tag, digest,
# number, address, or operation id; each is quoted again for the remote shell.
on_host() { # script args...
  local cmd
  printf -v cmd '%q ' "$lib/$1" "${@:2}"
  ssh "${ssh_opts[@]}" "$admin@$host" "sudo $cmd"
}
step() { on_host deploy-host.sh "$op" "$tag" "$@"; }

op=-
lock_held=0
phase=prepare
rendered=0
migration_may_be_applied=0
timers_stopped=0

retry() { # attempts delay command...
  local n=$1 d=$2 i
  shift 2
  for ((i = 1; i <= n; i++)); do
    "$@" && return 0
    ((i == n)) || sleep "$d"
  done
  return 1
}

# Before any migration request: put the previous release back beside
# maintenance, then stop maintenance. After one: leave maintenance up.
restore() {
  if ((migration_may_be_applied)); then
    say "a migration may have run: maintenance stays up, the app and job timers stay stopped"
    say "recover with a forward fix or a point-in-time restore (docs/design/vietnam-production.md, \"PostgreSQL\")"
    return 1
  fi
  if ((first)); then
    say "a failed first deploy leaves maintenance up; no earlier release exists"
    return 1
  fi
  if ((rendered)); then
    step restore-previous || return 1
  fi
  step app-up || return 1
  step maintenance-down || return 1
  if ((timers_stopped)); then step timers-start || return 1; fi
  say "restored the previous release"
}

on_exit() {
  local status=$?
  exec 2>&9
  trap '' HUP INT TERM
  if ((status != 0)) && [[ $phase == changing ]]; then
    restore || say "recovery did not finish; check the units with: deploy-host.sh - $tag status"
  fi
  if ((lock_held)); then
    on_host fence.sh release "$op" >/dev/null ||
      { say "could not release the operation lock; fence.sh clear owns it"; status=1; }
  fi
  ssh_close
  rm -rf "$work"
  exit "$status"
}
trap on_exit EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# 1. The tag is on main with a green push run of ci.yml on main for its commit
# (a dispatched run on a branch never counts), and every image has amd64.
git -C "$here" fetch -q origin main
commit=$(git -C "$here" rev-list -n1 "$tag")
git -C "$here" merge-base --is-ancestor "$commit" origin/main || {
  say "$tag is not on main"
  exit 1
}
ci=$(gh run list --repo "$repo" --workflow ci.yml --commit "$commit" --event push --branch main \
  --json conclusion,status,event,headBranch,headSha --limit 20 |
  jq -r --arg c "$commit" '[.[] | select(.event == "push" and .headBranch == "main" and .headSha == $c)]
    | .[0] | if . == null then "none" elif (.conclusion // "") == "" then .status else .conclusion end')
[[ $ci == success ]] || {
  say "no successful push run of ci.yml on main for $tag (${commit:0:7}); latest: '$ci'"
  exit 1
}

# The deploy runs this checkout's host scripts, never the target release's:
# the copies installed on the host must equal these files.
for f in fence.sh deploy-host.sh; do
  want=$(sha256sum "$here/$f" | cut -d' ' -f1)
  have=$(ssh "${ssh_opts[@]}" "$admin@$host" sha256sum "$lib/$f" | cut -d' ' -f1)
  [[ $have == "$want" ]] || {
    say "$lib/$f on the host differs from this checkout; run host/install.sh first"
    exit 1
  }
done

manifest() { # image accept
  local token
  token=$(curl -fsS "https://ghcr.io/token?scope=repository:$repo-$1:pull&service=ghcr.io" | jq -r .token)
  curl -fsS -H "Authorization: Bearer $token" -H "Accept: $2" "https://ghcr.io/v2/$repo-$1/manifests/$tag"
}
digest_of() { # image
  local token
  token=$(curl -fsS "https://ghcr.io/token?scope=repository:$repo-$1:pull&service=ghcr.io" | jq -r .token)
  curl -fsSI -H "Authorization: Bearer $token" \
    -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
    "https://ghcr.io/v2/$repo-$1/manifests/$tag" |
    tr -d '\r' | awk -F': ' 'tolower($1) == "docker-content-digest" { print $2 }'
}
declare -A image
for name in server web caddy; do
  d=$(digest_of "$name")
  [[ $d =~ ^sha256:[0-9a-f]{64}$ ]] || {
    say "no $name image index for $tag"
    exit 1
  }
  manifest "$name" 'application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' |
    jq -e '[.manifests[]? | select(.platform.os == "linux" and .platform.architecture == "amd64")] | length == 1' >/dev/null || {
    say "$name for $tag has no single linux/amd64 image"
    exit 1
  }
  provenance_verify "$name" "$d" "$tag" "$commit" || exit 1
  image[$name]="ghcr.io/$repo-$name@$d"
done

# --activate raises the fence while it holds the lock, only when the running
# server already is the exact candidate (docs/design/passkey-release-fence.md).
if ((activate)); then
  running=$(step running-release) || exit 1
  [[ $running == "$candidate" ]] || {
    say "the running server is release $running, not the activation candidate $candidate"
    exit 1
  }
  op=$(on_host fence.sh lock activate "$tag") && lock_held=1 || exit 1
  on_host fence.sh raise "$op" "$tag"
  say "raised the release fence to $tag ($candidate)"
  exit 0
fi

# 2. The lock. A lower tag or a held lock stops here, before any change.
op=$(on_host fence.sh lock "$kind" "$tag") || exit 1
lock_held=1
say "holding the operation lock"

# 3. Pull while the old release serves, and prove every secret exists.
step pull "${image[server]}" "${image[web]}" "${image[caddy]}"
step secrets-check

# 4. The release backup, annotated with the tag.
if ((!rollback)); then
  step backup
fi

# A rollback keeps the current maintenance Caddy image, because an older
# Caddy image may not hold the maintenance page.
maintenance_image=${image[caddy]}
if ((rollback)); then
  maintenance_image=$(step release-json | jq -r .maintenance_image)
fi

# 5. Jobs stop; maintenance starts beside Caddy, then Caddy and the server
# stop. The external app-down check is a Route 53 health check outside this
# script; maintenance answers it with 503 for the length of the deploy.
phase=changing
timers_stopped=1
step timers-stop
step maintenance-up "$maintenance_image"
step app-down

# 6. The marked 503 from outside the host.
maintenance_marker='aboutme:maintenance'
maintenance_ok() {
  local out code
  out=$(curl -s -m 10 -w '\n%{http_code}' "$site/") || return 1
  code=${out##*$'\n'}
  [[ $code == 503 ]] && grep -qF "$maintenance_marker" <<<"${out%$'\n'*}"
}
retry 5 3 maintenance_ok || {
  say "could not confirm the maintenance page from outside"
  exit 1
}
say "maintenance confirmed from outside"

# 7. Database work under the new units.
step render "${image[server]}" "${image[web]}" "${image[caddy]}"
rendered=1
if ((first)); then
  migration_may_be_applied=1
  step db-setup
fi
if ((!rollback)); then
  migration_may_be_applied=1
  step migrate
fi

# 8. The new release beside maintenance, ready from the host; then maintenance
# stops and is proved stopped.
step app-up
step maintenance-down

# 9. Jobs resume.
step timers-start
timers_stopped=0
phase=finished

# 10. Smoke from outside: health, TLS (curl verifies the chain), security
# headers, and a 403 for a CrowdSec-banned address, the operator's own.
status_ok() { [[ $(curl -s -o /dev/null -m 10 -w '%{http_code}' "$site$1") == 200 ]]; }
for path in /healthz /readyz; do
  retry 5 3 status_ok "$path" || {
    say "smoke: $path did not answer 200"
    exit 1
  }
done
headers=$(curl -fsSI -m 10 "$site/")
grep -qi '^strict-transport-security:' <<<"$headers" || {
  say "smoke: HSTS header missing"
  exit 1
}
grep -qi '^x-content-type-options: *nosniff' <<<"$headers" || {
  say "smoke: nosniff header missing"
  exit 1
}
me=$(ssh "${ssh_opts[@]}" "$admin@$host" 'printf %s "${SSH_CLIENT%% *}"')
banned() { [[ $(curl -s -o /dev/null -m 10 -w '%{http_code}' "$site/") == 403 ]]; }
step ban-test add "$me"
if retry 12 5 banned; then
  step ban-test del "$me"
else
  step ban-test del "$me" || true
  say "smoke: CrowdSec did not answer 403 for a banned address"
  exit 1
fi
say "deployed $tag"
