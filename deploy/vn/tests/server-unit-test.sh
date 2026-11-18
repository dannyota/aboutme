#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail() { printf 'server unit: %s\n' "$*" >&2; exit 1; }
host=$root/deploy/vn/scripts/deploy-host.sh
helpers=(flags_json flag server_secrets flags_check render_unit render_all step_secrets_check
  cimd_env_mcp_flag cimd_effective_mcp_flag)
{
  grep -E '^fence_epoch[a-z_]*=[0-9]+$' "$host"
  for helper in "${helpers[@]}"; do
    sed -n "/^$helper() {/,/^}/p" "$host"
  done
} | sed "s|/etc/aboutme/flags.env|$work/flags.env|g" >"$work/deploy-functions"
[[ $(grep -c '^fence_epoch' "$work/deploy-functions") == 3 ]] || fail 'the release fence epochs are missing'
[[ $(grep -c '^[a-z_]*() {$' "$work/deploy-functions") == "${#helpers[@]}" ]] ||
  fail 'a deploy-host.sh helper is missing'
sed -n '/^imported=(/,/)$/p; /^replaceable=(/,/)$/p; /^podman_names=(/,/)$/p
  /^member() {/,/^}/p; /^store() {/,/^}/p; /^cmd_import() {/,/^}/p; /^cmd_podman_sync() {/,/^}/p' \
  "$root/deploy/vn/scripts/secrets-host.sh" >"$work/secrets-functions"
# shellcheck source=/dev/null
source "$work/deploy-functions"
# shellcheck source=/dev/null
source "$work/secrets-functions"

say() { printf 'deploy-host: %s\n' "$*" >&2; }
die() { say "$*"; exit 1; }
fence_stub() {
  case $1 in
    read) printf '{"minimum_release":0}\n' ;;
    check) ;;
    *) return 1 ;;
  esac
}
aboutme_edge_transition() { printf '%s\n' "$*" >>"$work/effects"; }
write_release_json() { printf 'release state\n' >>"$work/effects"; }
podman() {
  printf '%s\n' "$*" >>"$work/podman-calls"
  [[ $1 == secret ]] || return 125
  case $2:$# in
    exists:3)
      [[ ! -e $work/podman-broken ]] || return 125
      if grep -qxF -- "$3" "$work/secrets"; then return 0; fi
      return 1
      ;;
    create:4 | rm:3) ;;
    *) return 125 ;;
  esac
}
attempt() {
  local out=$1
  shift
  set +e
  (set -e; "$@") >"$out" 2>&1
  status=$?
  set -e
}

# shellcheck disable=SC2034
templates=$root/deploy/vn/host/quadlet fence=fence_stub flags='{}' secrets_block=''
cp "$root/deploy/vn/host/env/flags.env.example" "$work/flags.env"
{
  sed -n 's/^Secret=\([^,]*\),.*/\1/p' "$templates"/*.container.in
  echo totp-key-a
} | sort -u >"$work/base-secrets"
digest=$(printf '%064d' 0)
server=ghcr.io/dannyota/aboutme-server@sha256:$digest
web=ghcr.io/dannyota/aboutme-web@sha256:$digest
caddy=ghcr.io/dannyota/aboutme-caddy@sha256:$digest
totp_line='Secret=totp-key-a,type=env,target=TOTP_ACTIVE_KEY'
id_line='Secret=github-evidence-client-id,type=env,target=GITHUB_EVIDENCE_CLIENT_ID'
secret_line='Secret=github-evidence-client-secret,type=env,target=GITHUB_EVIDENCE_CLIENT_SECRET'

use_secrets() {
  {
    cat "$work/base-secrets"
    [[ $# == 0 ]] || printf '%s\n' "$@"
  } >"$work/secrets"
}
# shellcheck disable=SC2034
render() {
  units=$work/$1/units
  mkdir -p "$work/$1"
  : >"$work/effects"
  : >"$work/podman-calls"
  attempt "$work/$1/out" render_all v0.9.0 9000 "$server" "$web" "$caddy" "$caddy" v0.9.0 9000
}
expected_server_unit() {
  sed -e 's|@RELEASE_TAG@|v0.9.0|g' -e 's|@RELEASE_NUMBER@|9000|g' -e "s|@SERVER_IMAGE@|$server|g" \
    -e "s|@WEB_IMAGE@|$web|g" -e "s|^@SERVER_SECRETS@$|$1|" "$templates/aboutme-server.container.in"
}

use_secrets
render absent
((status == 0)) || fail "render without evidence secrets failed: $(cat "$work/absent/out")"
expected_server_unit "$totp_line" >"$work/absent/expected"
cmp -s "$work/absent/expected" "$work/absent/units/aboutme-server.container" ||
  fail 'the server unit without evidence secrets is not the template with only the TOTP secret'

use_secrets github-evidence-client-id github-evidence-client-secret
render present
((status == 0)) || fail "render with both evidence secrets failed: $(cat "$work/present/out")"
expected_server_unit "$totp_line\n$id_line\n$secret_line" >"$work/present/expected"
cmp -s "$work/present/expected" "$work/present/units/aboutme-server.container" ||
  fail 'the server unit lacks the exact GitHub evidence secret lines'
[[ $(cat "$work/podman-calls") == $'secret exists github-evidence-client-id\nsecret exists github-evidence-client-secret' ]] ||
  fail "render made unexpected Podman calls: $(cat "$work/podman-calls")"
grep -qxF 'timeout --kill-after=1s 10 systemctl daemon-reload' "$work/effects" ||
  fail 'render with both evidence secrets did not reload units'
[[ $(cd "$work/absent/units" && ls) == "$(cd "$work/present/units" && ls)" ]] ||
  fail 'evidence secrets changed the rendered unit set'
for unit in "$work/absent/units"/*; do
  [[ ${unit##*/} == aboutme-server.container ]] || cmp -s "$unit" "$work/present/units/${unit##*/}" ||
    fail "evidence secrets changed ${unit##*/}"
done

for unit in "$work/absent/units/aboutme-server.container" "$work/present/units/aboutme-server.container"; do
  [[ $(grep -c '^Ulimit=' "$unit") == 1 ]] || fail 'the server unit must set exactly one Ulimit'
  [[ $(awk '/^\[/ { section = $0 } $0 == "Ulimit=core=0:0" { print section }' "$unit") == '[Container]' ]] ||
    fail 'the server unit must turn core dumps off in [Container]'
  [[ $(grep -c '^PodmanArgs=' "$unit") == 1 ]] || fail 'the server unit must set exactly one PodmanArgs'
  [[ $(awk '/^\[/ { section = $0 } /^PodmanArgs=(.* )?--stop-timeout=22( |$)/ { print section }' "$unit") == '[Container]' ]] ||
    fail 'the server unit must give Podman a 22 s stop timeout in [Container]'
  [[ $(grep -c '^TimeoutStopSec=' "$unit") == 1 ]] || fail 'the server unit must set exactly one TimeoutStopSec'
  [[ $(awk '/^\[/ { section = $0 } $0 == "TimeoutStopSec=25s" { print section }' "$unit") == '[Service]' ]] ||
    fail 'the server unit must give systemd a 25 s stop timeout in [Service]'
  if grep -q '^StopTimeout=' "$unit"; then fail 'Podman 4.9 rejects the Quadlet StopTimeout key'; fi
done
sed "s|/etc/aboutme/|$work/guard-|g" "$work/present/units/aboutme-server.container" >"$work/guard.container"
cp "$root/deploy/vn/host/env/server.env" "$work/guard-server.env"
cp "$work/flags.env" "$work/guard-flags.env"
printf 'SMTP_HOST=smtp.example\n' >"$work/guard-smtp.env"
[[ $(cimd_effective_mcp_flag "$work/guard.container") == true ]] ||
  fail 'the MCP rollback guard refused the rendered server unit'

for name in github-evidence-client-id github-evidence-client-secret; do
  case $name in
    github-evidence-client-id) missing=github-evidence-client-secret ;;
    *) missing=github-evidence-client-id ;;
  esac
  message="deploy-host: Podman secret $missing is missing; GitHub evidence needs both its secrets or neither"
  use_secrets "$name"
  render "only-$name"
  ((status != 0)) || fail "render accepted only $name"
  grep -qxF "$message" "$work/only-$name/out" || fail "render did not name $missing: $(cat "$work/only-$name/out")"
  [[ ! -e $work/only-$name/units && ! -s $work/effects ]] || fail "only $name wrote a unit or changed a service"
  attempt "$work/only-$name/check" step_secrets_check
  ((status != 0)) || fail "secrets-check accepted only $name"
  grep -qxF "$message" "$work/only-$name/check" || fail "secrets-check did not name $missing"
done

use_secrets github-evidence-client-id github-evidence-client-secret
touch "$work/podman-broken"
render broken
((status != 0)) || fail 'render accepted a failed Podman secret check'
grep -qxF 'deploy-host: could not check Podman secret github-evidence-client-id' "$work/broken/out" ||
  fail "render did not report the failed check: $(cat "$work/broken/out")"
[[ ! -e $work/broken/units && ! -s $work/effects ]] || fail 'a failed Podman check wrote a unit or changed a service'
rm "$work/podman-broken"

for secrets in '' 'github-evidence-client-id github-evidence-client-secret'; do
  # shellcheck disable=SC2086
  use_secrets $secrets
  attempt "$work/check.out" step_secrets_check
  ((status == 0)) || fail "secrets-check refused a whole set: $(cat "$work/check.out")"
  grep -qxF 'deploy-host: every referenced secret exists' "$work/check.out" || fail 'secrets-check did not pass'
done
sed -i 's/^TOTP_ACTIVE_KEY_SLOT=.*/TOTP_ACTIVE_KEY_SLOT=c/' "$work/flags.env"
attempt "$work/check.out" step_secrets_check
((status != 0)) || fail 'secrets-check accepted a malformed TOTP slot'
grep -qxF 'deploy-host: TOTP_ACTIVE_KEY_SLOT must be a or b' "$work/check.out" ||
  fail 'secrets-check did not name the malformed TOTP slot'

# shellcheck disable=SC2034
dir=$work/secret-files
mkdir -m 0700 "$dir"
: >"$work/secrets"
: >"$work/podman-calls"
for name in github-evidence-client-id github-evidence-client-secret; do
  attempt "$work/import.out" cmd_import "$name" < <(printf 'synthetic-%s' "$name")
  ((status == 0)) || fail "secrets.sh cannot import $name: $(cat "$work/import.out")"
  [[ $(stat -c %a "$dir/$name") == 400 ]] || fail "$name is not stored 0400"
  # shellcheck disable=SC2154
  member "$name" "${replaceable[@]}" || fail "$name cannot be rotated"
done
attempt "$work/sync.out" cmd_podman_sync
((status == 0)) || fail "podman-sync failed: $(cat "$work/sync.out")"
for name in github-evidence-client-id github-evidence-client-secret; do
  grep -qxF "secret create $name $dir/$name" "$work/podman-calls" || fail "podman-sync did not create $name"
done
