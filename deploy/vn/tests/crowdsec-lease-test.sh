#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
check=$repo_root/deploy/vn/host/crowdsec-retention-lease-check.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"

cat >"$work/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
[[ ${LEASE_TEST_TIMER_DISABLED:-0} != 1 && ${LEASE_TEST_TIMER_INACTIVE:-0} != 1 ]]
STUB
chmod +x "$work/bin/"*
PATH=$work/bin:$PATH
lease=$work/retention-ok
failures=0
reject_lease() {
  local label=$1
  shift
  if "$@" >/dev/null 2>&1; then
    printf 'FAIL %s\n' "$label"
    failures=$((failures + 1))
  else
    printf 'PASS %s\n' "$label"
  fi
}

reject_lease missing-lease "$check" "$lease"
: >"$lease"
reject_lease empty-lease "$check" "$lease"
printf '%s\n' malformed >"$lease"
reject_lease malformed-lease "$check" "$lease"
touch -d '+1 minute' "$lease"
reject_lease future-lease "$check" "$lease"
touch -d '7 minutes ago' "$lease"
reject_lease stale-lease "$check" "$lease"
touch "$lease"
reject_lease disabled-retention-timer env LEASE_TEST_TIMER_DISABLED=1 "$check" "$lease"
reject_lease inactive-retention-timer env LEASE_TEST_TIMER_INACTIVE=1 "$check" "$lease"
rm "$lease"
ln -s "$work/target" "$lease"
printf '%s\n' malformed >"$work/target"
reject_lease symlink-lease "$check" "$lease"

if ! grep -qF preflight-ready "$check"; then
  echo "FAIL lease-current-database-epoch"
  failures=$((failures + 1))
fi

((failures == 0)) || exit 1

echo "CrowdSec retention lease tests passed"
