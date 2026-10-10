#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
# shellcheck source=../host/crowdsec-offline.sh
source "$repo_root/deploy/vn/host/crowdsec-offline.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"

cat >"$work/bin/debconf-set-selections" <<'STUB'
#!/usr/bin/env bash
cat >"$CROWDSEC_TEST_PRESEED"
STUB
chmod +x "$work/bin/debconf-set-selections"
export PATH=$work/bin:$PATH CROWDSEC_TEST_PRESEED=$work/preseed

install_stub() {
  [[ -f $CROWDSEC_TEST_PRESEED ]] || {
    echo "package install ran before the CrowdSec opt-out" >&2
    return 1
  }
  printf '%s\n' "$@" >"$work/packages"
}
crowdsec_install_offline install_stub crowdsec=1.8.1 crowdsec-firewall-bouncer-nftables=0.0.36
grep -qxF 'crowdsec crowdsec/capi boolean false' "$work/preseed" || {
  echo "CrowdSec was not opted out before package install" >&2
  exit 1
}
grep -qxF 'crowdsec=1.8.1' "$work/packages" || { echo "pinned CrowdSec package was not installed" >&2; exit 1; }

base=$work/config.yaml
local_config=$work/config.yaml.local
credentials=$work/online_api_credentials.yaml
cat >"$base" <<'YAML'
api:
  server:
    online_client:
      credentials_path: /etc/crowdsec/online_api_credentials.yaml
    trusted_ips:
      - 127.0.0.1
YAML
printf 'api:\n  server:\n    listen_uri: 127.0.0.1:8095\n' >"$local_config"
printf 'login: should-be-deleted\n' >"$credentials"

crowdsec_remove_online "$base" "$local_config" "$credentials"
! grep -q online_client "$base" || { echo "online_client was not removed" >&2; exit 1; }
grep -q trusted_ips "$base" || { echo "config after online_client was removed" >&2; exit 1; }
[[ ! -e $credentials ]] || { echo "central credentials were not removed" >&2; exit 1; }

printf 'api:\n  server:\n    online_client:\n      credentials_path: forbidden\n' >"$local_config"
if crowdsec_assert_offline "$base" "$local_config" "$credentials" 2>/dev/null; then
  echo "offline proof accepted inherited central API config" >&2
  exit 1
fi

echo "CrowdSec offline tests passed"
