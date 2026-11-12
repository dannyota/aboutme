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
# (default https://aboutme.vn; a rehearsal sets its own origin), and SSH_PORT
# (ssh-lib.sh).
#
# Recovery follows docs/design/single-host-production.md, "Release and
# deploy": a failure before any migration or schema-floor raise request
# restores the previous release beside maintenance; once either may have run,
# maintenance stays up with the app and timers stopped, and recovery is a
# forward fix or a point-in-time restore. A failed recovery retains the
# operation lock.
set -euo pipefail

repo=dannyota/aboutme
admin=aboutme-admin
lib=/usr/local/lib/aboutme
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=ssh-lib.sh
source "$here/ssh-lib.sh"

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

# The host's fence.sh raise refuses a tag above the tracked floor
# (docs/design/release-floors.md); an activation above this checkout's copy
# stops here, before SSH or registry access.
[[ $(grep '^floor_release=' "$here/fence.sh") =~ ^floor_release=([0-9]+)$ ]] || {
  say "fence.sh does not set one tracked floor"
  exit 1
}
tracked_floor=$((10#${BASH_REMATCH[1]}))
if ((activate && candidate > tracked_floor)); then
  say "$tag ($candidate) is above the tracked floor $tracked_floor in fence.sh; land a change that raises it first"
  exit 1
fi

# Refuse before opening an SSH connection or querying the registry. Deployment
# code comes from clean, current main, even when rolling back to an older tag.
commit=$(bash "$here/../../../scripts/release-eligibility.sh" --deploy-checkout "$tag")

# A deploy reads its schema floor from the tag's commit, never from this
# checkout. A tree without the file has none.
schema_floor_path=apps/server/migrations/schema-floor
schema_floor_tag='' schema_floor=0
if ((!rollback && !activate)) && git -C "$here" cat-file -e "$commit:$schema_floor_path" 2>/dev/null; then
  schema_floor_text=$(git -C "$here" show "$commit:$schema_floor_path" && printf x) || {
    say "could not read $schema_floor_path at $tag"
    exit 1
  }
  schema_floor_tag=${schema_floor_text%$'\n'x}
  if [[ $schema_floor_text != "$schema_floor_tag"$'\n'x ]] || ! schema_floor=$(release_number "$schema_floor_tag"); then
    say "$schema_floor_path at $tag must hold one strict tag and a newline"
    exit 1
  fi
  if ((schema_floor > candidate)); then
    say "$tag ($candidate) is below its schema floor $schema_floor_tag ($schema_floor)"
    exit 1
  fi
  if ((schema_floor > tracked_floor)); then
    say "the schema floor $schema_floor_tag of $tag is above the tracked floor $tracked_floor in fence.sh"
    exit 1
  fi
fi

work='' ssh_dir='' probe=''
ssh_opts=()
ssh_close() { ssh "${ssh_opts[@]}" -O exit "$admin@$host" 2>/dev/null || true; }

# Runs one host script as root. Arguments include tags, digests, operation
# ids, and release records. Each is quoted for the remote shell; the host
# validates the values before use.
on_host() { # script args...
  local cmd
  printf -v cmd '%q ' "$lib/$1" "${@:2}"
  ssh "${ssh_opts[@]}" "$admin@$host" "sudo $cmd"
}
step() { on_host deploy-host.sh "$op" "$tag" "$@"; }

op=-
lock_held=0
phase=prepare
render_requested=0
previous=
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
    step fail-closed || return 1
    say "a migration or schema-floor raise may have run: enforced maintenance with the app and job timers stopped"
    say "recover with a forward fix or a point-in-time restore (docs/design/vietnam-production.md, \"PostgreSQL\")"
    return 0
  fi
  if ((first)); then
    say "a failed first deploy leaves maintenance up; no earlier release exists"
    return 1
  fi
  step maintenance-up "$maintenance_image" || return 1
  step app-down || return 1
  if ((render_requested)); then
    step restore-previous "$previous" || return 1
  fi
  step app-up || return 1
  step maintenance-down || return 1
  if ((timers_stopped)); then step timers-start || return 1; fi
  say "restored the previous release"
}

on_exit() {
  local status=$? recovery_ok=1
  exec 2>&9
  trap '' HUP INT TERM
  if ((status != 0)) && [[ $phase == changing ]]; then
    if ! restore; then
      recovery_ok=0
      status=1
      say "recovery did not finish; the operation lock remains for manual recovery"
      say "check the units with: deploy-host.sh - $tag status"
    fi
  fi
  if ((lock_held && recovery_ok)); then
    on_host fence.sh release "$op" >/dev/null ||
      { say "could not release the operation lock; fence.sh clear owns it"; status=1; }
  fi
  if [[ -n $probe ]]; then
    kill "$probe" 2>/dev/null || true
    wait "$probe" 2>/dev/null || true
  fi
  if [[ -n ${ssh_opts[*]:-} ]]; then ssh_close; fi
  if [[ -n $work ]]; then rm -rf "$work"; fi
  if [[ -n $ssh_dir ]]; then rm -rf "$ssh_dir"; fi
  exit "$status"
}
trap on_exit EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

work=$(mktemp -d)
ssh_dir=$(mktemp -d /tmp/aboutme-ssh.XXXXXX)
# One SSH connection for the whole run.
ssh_opts=(-p "$SSH_PORT" -o BatchMode=yes -o ControlMaster=auto -o "ControlPath=$ssh_dir/ssh-%C" -o ControlPersist=10m)

# The deploy runs this checkout's host scripts, never the target release's:
# the copies installed on the host must equal these files.
for f in fence.sh deploy-host.sh deploy-safety.sh; do
  want=$(sha256sum "$here/$f" | cut -d' ' -f1)
  have=$(ssh "${ssh_opts[@]}" "$admin@$host" sha256sum "$lib/$f" | cut -d' ' -f1)
  [[ $have == "$want" ]] || {
    say "$lib/$f on the host differs from this checkout; run host/install.sh first"
    exit 1
  }
done

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
  # Pin the linux/amd64 image inside this index, never the index: its own
  # provenance and SBOM attestations name that digest, and Podman reports it
  # as the running image (docs/design/deployment-transparency/host.md).
  token=$(curl -fsS "https://ghcr.io/token?scope=repository:$repo-$name:pull&service=ghcr.io" | jq -er .token)
  platform=$(curl -fsS -H "Authorization: Bearer $token" \
    -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
    "https://ghcr.io/v2/$repo-$name/manifests/$d" |
    jq -er '[.manifests[]? | select(.platform.os == "linux" and .platform.architecture == "amd64")]
      | select(length == 1) | .[0].digest | select(test("^sha256:[0-9a-f]{64}$"))') || {
    say "$name for $tag has no single linux/amd64 image"
    exit 1
  }
  provenance_verify "$name" "$platform" "$tag" "$commit" || exit 1
  image[$name]="ghcr.io/$repo-$name@$platform"
done

# The observer has a separate single-platform tag. Only a missing tag permits
# keeping the installed observer; registry errors and bad evidence refuse.
observer_image=""
if ((!activate)); then
  token=$(curl -fsS "https://ghcr.io/token?scope=repository:$repo-observer:pull&service=ghcr.io" | jq -er .token)
  code=$(curl -sS -D "$work/observer-headers" -o "$work/observer-manifest.json" -w '%{http_code}' \
    -H "Authorization: Bearer $token" \
    -H 'Accept: application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json' \
    "https://ghcr.io/v2/$repo-observer/manifests/$tag-amd64") || {
    say "could not read the observer image for $tag"
    exit 1
  }
  case $code in
    404) say "observer image absent; keeping the installed observer unit" ;;
    200)
      d=$(tr -d '\r' <"$work/observer-headers" |
        awk -F': ' 'tolower($1) == "docker-content-digest" { print $2 }')
      [[ $d =~ ^sha256:[0-9a-f]{64}$ ]] || { say "observer image has no digest"; exit 1; }
      config_digest=$(jq -er '
        select(.schemaVersion == 2 and .manifests == null
          and (.mediaType == "application/vnd.oci.image.manifest.v1+json"
            or .mediaType == "application/vnd.docker.distribution.manifest.v2+json"))
        | .config.digest | select(type == "string" and test("^sha256:[0-9a-f]{64}$"))
        ' "$work/observer-manifest.json") || {
        say "observer for $tag is not a single-platform image"
        exit 1
      }
      # GHCR answers a blob request with a redirect to storage; the digest
      # check binds whatever the redirect serves to the signed manifest.
      curl -fsSL -o "$work/observer-config.json" -H "Authorization: Bearer $token" \
        "https://ghcr.io/v2/$repo-observer/blobs/$config_digest" || {
        say "could not read the observer image configuration for $tag"
        exit 1
      }
      [[ sha256:$(sha256sum "$work/observer-config.json" | cut -d' ' -f1) == "$config_digest" ]] || {
        say "observer configuration for $tag does not match its digest"
        exit 1
      }
      jq -e '.os == "linux" and .architecture == "amd64"' "$work/observer-config.json" >/dev/null || {
        say "observer for $tag is not linux/amd64"
        exit 1
      }
      provenance_verify observer "$d" "$tag" "$commit" || exit 1
      observer_image="ghcr.io/$repo-observer@$d"
      ;;
    *) say "observer registry returned HTTP $code"; exit 1 ;;
  esac
fi

# --activate raises the fence while it holds the lock, only when the running
# server already is the exact candidate (docs/design/passkey-release-fence.md).
if ((activate)); then
  op=$(on_host fence.sh lock activate "$tag") && lock_held=1 || exit 1
  running=$(step running-release) || exit 1
  [[ $running == "$candidate" ]] || {
    say "the running server is release $running, not the activation candidate $candidate"
    exit 1
  }
  on_host fence.sh raise "$op" "$tag"
  say "raised the release fence to $tag ($candidate)"
  exit 0
fi

# The CrowdSec smoke bans the address the host sees for SSH and expects this
# laptop's HTTPS requests to get 403, so both must leave through one address.
me=$(ssh "${ssh_opts[@]}" "$admin@$host" 'printf %s "${SSH_CLIENT%% *}"')
[[ $me =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || {
  say "the host did not report an IPv4 SSH client address"
  exit 1
}
# Holds one HTTPS request open while the host counts its established port-443
# sockets from that address. ss filters on the host, so no other client's
# address crosses SSH. Caddy's dual-stack socket shows an IPv4 client as an
# IPv4-mapped IPv6 peer, so the filter names both forms.
egress_check() {
  local i count=''
  curl -s -o /dev/null -m 20 --limit-rate 1 "$site/" &
  probe=$!
  for ((i = 1; i <= 5; i++)); do
    sleep 1
    # shellcheck disable=SC2029 # me is a checked IPv4 address
    count=$(ssh "${ssh_opts[@]}" "$admin@$host" \
      "set -o pipefail; ss -Htn state established '( sport = :443 ) and ( dst $me or dst [::ffff:$me] )' | wc -l") ||
      count=''
    [[ $count =~ ^[0-9]+$ ]] || break
    ((10#$count == 0)) || break
  done
  kill "$probe" 2>/dev/null || true
  wait "$probe" 2>/dev/null || true
  probe=''
  [[ $count =~ ^[0-9]+$ ]] || {
    say "could not count the host's HTTPS connections from this laptop"
    return 1
  }
  ((10#$count > 0)) || {
    say "the host does not see this laptop's HTTPS from its SSH address; turn off VPNs such as Cloudflare WARP and retry"
    return 1
  }
}
# The check needs a listener on 443. With none (a first deploy, or an outage
# where nothing answers), it runs once maintenance serves, so a rollback or
# forward fix can still reach maintenance.
egress_after_maintenance=$first
if ((!first)); then
  code=$(curl -s -o /dev/null -m 10 -w '%{http_code}' "$site/") || code=000
  if [[ $code == 000 ]]; then
    egress_after_maintenance=1
    say "$site does not answer; the egress check waits until maintenance serves"
  else
    egress_check || exit 1
  fi
fi

# 2. The lock. A lower tag or a held lock stops here, before any change.
op=$(on_host fence.sh lock "$kind" "$tag") || exit 1
lock_held=1
say "holding the operation lock"

# 3. Pull while the old release serves, and prove every secret exists.
step pull "${image[server]}" "${image[web]}" "${image[caddy]}" "$observer_image"
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
# stop. The external uptime workflow keeps checking during maintenance,
# which answers it with 503 for the length of the deploy.
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
if ((egress_after_maintenance)); then egress_check || exit 1; fi

# 7. Database work under the new units.
if ((!first)); then previous=$(step release-json) || exit 1; fi
render_requested=1
step render "${image[server]}" "${image[web]}" "${image[caddy]}" "$observer_image"
# The schema floor goes up before db-setup and migrate. A raise that may have
# landed can refuse the previous release, so from here recovery fails closed.
# The fence document holds the private operation ID; keep only its minimum.
if [[ -n $schema_floor_tag ]]; then
  fence_minimum=$(on_host fence.sh read | jq -r .minimum_release) || exit 1
  [[ $fence_minimum =~ ^[0-9]+$ ]] || {
    say "could not read the release fence minimum"
    exit 1
  }
  if ((schema_floor > fence_minimum)); then
    migration_may_be_applied=1
    on_host fence.sh raise "$op" "$schema_floor_tag"
    say "raised the release fence to the schema floor $schema_floor_tag ($schema_floor)"
  fi
fi
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
# app-up rechecks the CIMD rollback boundary with the serving app stopped,
# under this operation's persistent lock. The host also checks restoration.
step app-up
step maintenance-down

# 9. Jobs resume.
step timers-start
timers_stopped=0

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
# Read only the ticker from the running edge configuration, not the API key.
# shellcheck disable=SC2016
ticker=$(ssh "${ssh_opts[@]}" "$admin@$host" \
  'sudo podman exec aboutme-caddy awk '\''$1 == "ticker_interval" {print $2}'\'' /run/caddy/crowdsec.global') || {
  say "smoke: could not read the CrowdSec decision-stream ticker"
  exit 1
}
[[ $ticker =~ ^([1-9][0-9]{0,2})s$ ]] || {
  say "smoke: invalid CrowdSec decision-stream ticker"
  exit 1
}
ticker_seconds=${BASH_REMATCH[1]}
banned() { [[ $(curl -s -o /dev/null -m 10 -w '%{http_code}' "$site/") == 403 ]]; }
# The new Caddy's first decision pull can fail and retry after a growing wait,
# so poll until 150 s after the ban: a last request of up to 10 s still ends
# inside the 3-minute test ban. 31 polls 5 s apart cannot end before that
# deadline; the bound also ends the loop when sleep returns at once.
ban_seen() { # deadline
  local i
  for ((i = 1; i <= 31; i++)); do
    banned && return 0
    ((i < 31 && SECONDS + 5 <= $1)) || return 1
    sleep 5
  done
  return 1
}
step ban-test add "$me"
if ban_seen $((SECONDS + 150)); then
  step ban-test del "$me"
else
  step ban-test del "$me" || true
  say "smoke: CrowdSec did not answer 403 for a banned address within 150 s"
  exit 1
fi
# Wait at least two stream ticks plus five seconds after deleting the ban.
edge_attempts=$(((2 * ticker_seconds + 9) / 5 + 1))
retry "$edge_attempts" 5 status_ok / || {
  say "smoke: / did not answer 200 after deleting the CrowdSec test ban"
  exit 1
}
phase=finished
say "deployed $tag"
