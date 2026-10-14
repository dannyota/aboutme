#!/usr/bin/env bash
# Isolated rollback guard regression; uses no host, database, or secret files.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
sed -n '/^cimd_rollback_check() {/,/^}/p' "$here/deploy-host.sh" >"$work/guard.sh"
[[ -s $work/guard.sh ]] || { echo 'missing CIMD rollback guard' >&2; exit 1; }

check_case() {
  local candidate=$1 mcp=$2 rows=$3 expected=$4
  (
    die() { exit 1; }
    cimd_serving_app_stopped() { [[ ${serving_stopped:-true} == true ]]; }
    cimd_effective_mcp_flag() { printf '%s' "$mcp"; }
    cimd_url_rows_exist() { [[ $rows != failure ]] || return 1; printf '%s' "$rows"; }
    # shellcheck source=/dev/null
    source "$work/guard.sh"
    cimd_rollback_check "$candidate"
  ) && actual=0 || actual=1
  [[ $actual == "$expected" ]] || { echo 'CIMD rollback case failed' >&2; exit 1; }
}

check_case 8000 true t 0
check_case 7999 true t 1
check_case 7999 true f 0
check_case 7999 false t 0
check_case 7999 invalid f 1
check_case 7999 true failure 1
check_case 7999 true invalid 1
serving_stopped=false check_case 8000 true f 1
echo 'CIMD rollback guard cases passed'
