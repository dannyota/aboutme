#!/usr/bin/env bash
# Isolated rollback guard regression; uses no host, database, or secret files.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
sed -n '/^cimd_rollback_check() {/,/^}/p' "$here/deploy-host.sh" >"$work/guard.sh"
[[ -s $work/guard.sh ]] || { echo 'missing CIMD rollback guard' >&2; exit 1; }

check_case() {
  local fixture_candidate=$1 fixture_mcp=$2 fixture_rows=$3 expected=$4 expected_error=${5:-} actual=0
  (
    die() { printf '%s\n' "$*" >&2; exit 1; }
    cimd_serving_app_stopped() { [[ ${serving_stopped:-true} == true ]]; }
    cimd_effective_mcp_flag() { [[ $fixture_mcp != failure ]] || return 1; printf '%s' "$fixture_mcp"; }
    cimd_url_rows_exist() { [[ $fixture_rows != failure ]] || return 1; printf '%s' "$fixture_rows"; }
    # shellcheck source=/dev/null
    source "$work/guard.sh"
    cimd_rollback_check "$fixture_candidate" "$work/guard.sh"
  ) >"$work/case.out" 2>"$work/case.err" || actual=$?
  [[ $actual == "$expected" ]] || {
    printf 'CIMD rollback: candidate=%s MCP_ENABLED=%s URL-client rows=%s serving stopped=%s: expected exit %s, got %s\n' \
      "$fixture_candidate" "$fixture_mcp" "$fixture_rows" "${serving_stopped:-true}" "$expected" "$actual" >&2
    cat "$work/case.err" >&2
    exit 1
  }
  printf '%s' "$expected_error" >"$work/expected.err"
  [[ -z $expected_error ]] || printf '\n' >>"$work/expected.err"
  cmp -s "$work/expected.err" "$work/case.err" || {
    printf 'CIMD rollback: candidate=%s MCP_ENABLED=%s URL-client rows=%s serving stopped=%s: expected guard diagnostic "%s"\n' \
      "$fixture_candidate" "$fixture_mcp" "$fixture_rows" "${serving_stopped:-true}" "$expected_error" >&2
    cat "$work/case.err" >&2
    exit 1
  }
}

check_case 8000 true t 0
check_case 7999 true t 1 'pre-CIMD rollback requires no URL clients or MCP_ENABLED=false'
check_case 7999 true f 0
check_case 7999 false t 0
check_case 7999 invalid f 1 'invalid MCP configuration for rollback'
check_case 7999 failure f 1 'cannot determine MCP configuration for rollback'
check_case invalid true f 1 'invalid CIMD rollback candidate'
check_case 7999 true failure 1 'cannot determine URL-client existence for rollback'
check_case 7999 true invalid 1 'invalid URL-client existence result for rollback'
serving_stopped=false check_case 8000 true f 1 'CIMD rollback check requires a proved stopped serving app'
echo 'CIMD rollback guard cases passed'
