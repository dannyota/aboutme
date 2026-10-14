#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export ABOUTME_TEST_STATE=$work/state
mkdir -p "$ABOUTME_TEST_STATE/active" "$ABOUTME_TEST_STATE/enabled"

cat >"$work/systemctl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
state=${ABOUTME_TEST_STATE:?}
case $1 in
  is-active) [[ -e $state/active/${3} ]] ;;
  is-enabled) [[ -e $state/enabled/${3} ]] ;;
  stop)
    shift
    for unit in "$@"; do rm -f "$state/active/$unit"; done
    ;;
  start)
    shift
    for unit in "$@"; do : >"$state/active/$unit"; done
    ;;
  enable)
    shift
    [[ ${1:-} == --now ]] && shift
    for unit in "$@"; do
      : >"$state/enabled/$unit"
      : >"$state/active/$unit"
    done
    ;;
  *) echo "unexpected systemctl call: $*" >&2; exit 2 ;;
esac
STUB
chmod +x "$work/systemctl"
export PATH=$work:$PATH

# shellcheck source=../scripts/deploy-safety.sh
source "$root/deploy/vn/scripts/deploy-safety.sh"

for unit in aboutme-caddy aboutme-server aboutme-web \
  aboutme-job-idempotency-expiry-sweep.timer \
  aboutme-job-media-deletion-sweep.timer \
  aboutme-job-privacy-retention-sweep.timer \
  aboutme-job-media-orphan-sweep.timer \
  aboutme-job@media-deletion-sweep.service; do
  : >"$ABOUTME_TEST_STATE/active/$unit"
done

aboutme_fail_closed 1
aboutme_assert_fail_closed

for unit in aboutme-caddy aboutme-server aboutme-web \
  aboutme-job-idempotency-expiry-sweep.timer \
  aboutme-job-media-deletion-sweep.timer \
  aboutme-job-privacy-retention-sweep.timer \
  aboutme-job-media-orphan-sweep.timer \
  aboutme-job@media-deletion-sweep.service; do
  [[ ! -e $ABOUTME_TEST_STATE/active/$unit ]] || {
    echo "$unit still active after fail closed" >&2
    exit 1
  }
done
[[ -e $ABOUTME_TEST_STATE/active/aboutme-maintenance ]] || {
  echo "maintenance is not active after fail closed" >&2
  exit 1
}

# A timer restarted by another process must make the cutover guard fail.
: >"$ABOUTME_TEST_STATE/active/aboutme-job-media-orphan-sweep.timer"
if aboutme_assert_cutover_quiescent; then
  echo "cutover guard accepted a restarted job timer" >&2
  exit 1
fi
rm -f "$ABOUTME_TEST_STATE/active/aboutme-job-media-orphan-sweep.timer"
aboutme_assert_cutover_quiescent

# Exercise the actual stopped-app and Boolean database helpers with closed
# command fixtures, then invoke the isolated candidate-release matrix.
for helper in cimd_url_rows_exist cimd_serving_app_stopped; do
  sed -n "/^$helper() {/,/^}/p" "$root/deploy/vn/scripts/deploy-host.sh" >>"$work/cimd-helpers.sh"
done
for helper in cimd_env_mcp_flag cimd_effective_mcp_flag; do
  sed -n "/^$helper() {/,/^}/p" "$root/deploy/vn/scripts/deploy-host.sh" >>"$work/cimd-helpers.sh"
done
templates=$work/quadlet
mkdir -p "$templates"
sed "s|/etc/aboutme/|$work/cimd-|g" "$root/deploy/vn/host/quadlet/aboutme-server.container.in" >"$templates/aboutme-server.container.in"
# shellcheck source=/dev/null
source "$work/cimd-helpers.sh"
systemctl() {
  [[ $* == 'show -p ActiveState --value aboutme-server' ]] || return 1
  [[ ${CIMD_STATE:-inactive} != failure ]] || return 1
  printf '%s\n' "${CIMD_STATE:-inactive}"
}
runuser() {
  [[ $* == *'-u postgres -- psql -X -qAt -v ON_ERROR_STOP=1 -d aboutme'* ]] || return 1
  [[ $* == *'SELECT EXISTS (SELECT 1 FROM public.oauth_clients'* ]] || return 1
  [[ $* == *"to_jsonb(client)->>'metadata_url' IS NOT NULL)"* ]] || return 1
  [[ ${CIMD_QUERY:-f} != failure ]] || return 1
  printf '%s\n' "${CIMD_QUERY:-f}"
}
for CIMD_STATE in inactive failed; do
  export CIMD_STATE
  cimd_serving_app_stopped || { echo 'stopped app rejected' >&2; exit 1; }
done
for CIMD_STATE in active activating deactivating unknown failure; do
  export CIMD_STATE
  if cimd_serving_app_stopped; then echo 'unproved stopped app accepted' >&2; exit 1; fi
done
unset CIMD_STATE
for CIMD_QUERY in t f; do
  export CIMD_QUERY
  [[ $(cimd_url_rows_exist) == "$CIMD_QUERY" ]] || exit 1
done
if CIMD_QUERY=failure cimd_url_rows_exist; then echo 'database failure accepted' >&2; exit 1; fi
unset CIMD_QUERY
printf 'MCP_ENABLED=true\n' >"$work/cimd-server.env"
: >"$work/cimd-flags.env"
printf 'SMTP_HOST=smtp.example\n' >"$work/cimd-smtp.env"
[[ $(cimd_effective_mcp_flag) == true ]] || exit 1
printf 'MCP_ENABLED=false\n' >"$work/cimd-flags.env"
[[ $(cimd_effective_mcp_flag) == false ]] || exit 1
printf 'MCP_ENABLED=true\n' >>"$work/cimd-smtp.env"
[[ $(cimd_effective_mcp_flag) == true ]] || { echo 'SMTP override was ignored' >&2; exit 1; }
# The guard must refuse the pre-CIMD image under the actual effective flag.
sed -n '/^cimd_rollback_check() {/,/^}/p' "$root/deploy/vn/scripts/deploy-host.sh" >"$work/cimd-guard.sh"
(
  die() { exit 1; }
  # shellcheck source=/dev/null
  source "$work/cimd-guard.sh"
  CIMD_QUERY=t cimd_rollback_check 7999
) && { echo 'SMTP override allowed unsafe rollback' >&2; exit 1; }
printf 'MCP_ENABLED=false\n' >"$work/cimd-smtp.env"
[[ $(cimd_effective_mcp_flag) == false ]] || exit 1
printf 'Environment=MCP_ENABLED=true\n' >>"$templates/aboutme-server.container.in"
# Inline container variables override every environment file.
[[ $(cimd_effective_mcp_flag) == true ]] || exit 1
sed -i '/^Environment=MCP_ENABLED=/d' "$templates/aboutme-server.container.in"
for configuration in 'MCP_ENABLED=invalid' $'MCP_ENABLED=true\nMCP_ENABLED=false' 'MCP_ENABLED="true"'; do
  printf '%s\n' "$configuration" >"$work/cimd-smtp.env"
  if cimd_effective_mcp_flag; then echo 'invalid SMTP override accepted' >&2; exit 1; fi
done
: >"$work/cimd-smtp.env"
: >"$work/cimd-flags.env"
for configuration in '' 'MCP_ENABLED=invalid' $'MCP_ENABLED=true\nMCP_ENABLED=false' 'MCP_ENABLED="true"'; do
  printf '%s\n' "$configuration" >"$work/cimd-server.env"
  if cimd_effective_mcp_flag; then echo 'unproved MCP configuration accepted' >&2; exit 1; fi
done
rm "$work/cimd-server.env"
if cimd_effective_mcp_flag; then echo 'missing MCP configuration accepted' >&2; exit 1; fi
bash "$root/deploy/vn/scripts/cimd-rollback-test.sh"

echo "deployment safety tests passed"
