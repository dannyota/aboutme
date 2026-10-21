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
# Reset must precede final application validation and completion.
python3 - "$root" <<'CONTRACT'
import pathlib,sys
root=pathlib.Path(sys.argv[1])
source=(root/'deploy/vn/scripts/restore-drill.sh').read_text()
child=source.split('notice_host_child() {',1)[1].split('notice_host_parent() {',1)[0]
assert child.index('crowdsec_restore_reset')<child.index('notice_database_checks')<child.index('notice_restore_postcheck')
assert '4>&-' in child
stage=(root/'deploy/vn/host/crowdsec-restore-stage.sh').read_text()
assert 'd639e87924abe497618795378c13a425e0ffab2bbaecbf50900fae55cbed0ad0' in stage
assert 'dpkg-deb' in stage and 'apt-get download' in stage
assert 'crowdsec_install_offline' not in stage
CONTRACT
echo 'PASS restore-role-boundary'
