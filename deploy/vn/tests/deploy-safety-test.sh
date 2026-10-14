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
sed -n '/^cimd_effective_mcp_flag() {/,/^}/p' "$root/deploy/vn/scripts/deploy-host.sh" |
  sed "s|/etc/aboutme/server.env|$work/cimd-server.env|" >>"$work/cimd-helpers.sh"
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
flags_json() {
  local flags=${CIMD_FLAGS_JSON-'{}'}
  [[ $flags != failure ]] || return 1
  printf '%s' "$flags"
}
printf 'MCP_ENABLED=true\n' >"$work/cimd-server.env"
[[ $(cimd_effective_mcp_flag) == true ]] || exit 1
CIMD_FLAGS_JSON='{"MCP_ENABLED":"false"}'
[[ $(cimd_effective_mcp_flag) == false ]] || exit 1
for CIMD_FLAGS_JSON in failure '{"MCP_ENABLED":"invalid"}' 'invalid'; do
  if cimd_effective_mcp_flag; then echo 'invalid rollback override accepted' >&2; exit 1; fi
done
unset CIMD_FLAGS_JSON
for configuration in '' 'MCP_ENABLED=invalid' $'MCP_ENABLED=true\nMCP_ENABLED=false' 'MCP_ENABLED="true"'; do
  printf '%s\n' "$configuration" >"$work/cimd-server.env"
  if cimd_effective_mcp_flag; then echo 'unproved MCP configuration accepted' >&2; exit 1; fi
done
rm "$work/cimd-server.env"
if cimd_effective_mcp_flag; then echo 'missing MCP configuration accepted' >&2; exit 1; fi
bash "$root/deploy/vn/scripts/cimd-rollback-test.sh"

echo "deployment safety tests passed"
