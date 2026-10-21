#!/usr/bin/env bash
# Restore validation accepts only the application roles and isolated CrowdSec.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
sed -n '/^notice_database_checks() {/,/^}/p' \
  "$root/deploy/vn/scripts/restore-drill.sh" >"$work/checks"
# shellcheck disable=SC1091
source "$work/checks"
notice_sql() {
  local query
  query=$(cat)
  case $query in
    *'max(version_id)'*) printf '1\n' ;;
    *'query_to_xml'*) printf 'goose_db_version|1\nresumes|0\n' ;;
    *'aclexplode'*) printf '0\n' ;;
    *'has_schema_privilege'*) printf 'f\n' ;;
    *'string_agg(rolname'*) printf '%s\n' "$restore_roles" ;;
    *'SET ROLE aboutme_app'*) printf '0\n' ;;
    *) return 1 ;;
  esac
}
restore_roles=aboutme,aboutme_app,aboutme_migrator,crowdsec
if ! notice_database_checks >"$work/out" 2>&1; then
  echo 'FAIL restore-role-boundary' >&2
  exit 1
fi
for restore_roles in aboutme,aboutme_app,aboutme_migrator \
  aboutme,aboutme_app,aboutme_migrator,crowdsec,unexpected \
  aboutme,aboutme_app,crowdsec; do
  if notice_database_checks >"$work/out" 2>&1; then
    echo 'FAIL restore-unexpected-role-refusal' >&2
    exit 1
  fi
done
echo 'PASS restore-role-boundary'
