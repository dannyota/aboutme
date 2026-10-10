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
: >"$lease"

PATH=$PATH "$check" "$lease"
touch -d '7 minutes ago' "$lease"
if PATH=$PATH "$check" "$lease"; then
  echo "stale retention lease passed the start guard" >&2
  exit 1
fi
if LEASE_TEST_TIMER_DISABLED=1 PATH=$PATH "$check" "$lease"; then
  echo "disabled retention timer passed the start guard" >&2
  exit 1
fi
if LEASE_TEST_TIMER_INACTIVE=1 PATH=$PATH "$check" "$lease"; then
  echo "inactive retention timer passed the start guard" >&2
  exit 1
fi

echo "CrowdSec retention lease tests passed"
