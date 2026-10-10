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

cat >"$work/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$CROWDSEC_TEST_SYSTEMCTL"
case $1 in
  stop) [[ ${CROWDSEC_TEST_STOP_FAIL:-} != yes ]] || exit 70 ;;
  show)
    [[ ${CROWDSEC_TEST_SHOW_FAIL:-} != yes ]] || exit 71
    if [[ $2 == --property=LoadState ]]; then
      if [[ ${CROWDSEC_TEST_BOUNCER_ABSENT:-} == yes && $4 == crowdsec-firewall-bouncer.service ]]; then
        printf 'not-found\n'
      else
        printf 'loaded\n'
      fi
    else
      printf '%s\n' "${CROWDSEC_TEST_ACTIVE_STATE:-inactive}"
    fi
    ;;
  mask)
    if [[ ${CROWDSEC_TEST_CLEANUP_MASK_FAIL:-} == yes && $3 == crowdsec.service ]]; then
      exit 72
    fi
    ;;
esac
STUB
chmod +x "$work/bin/systemctl"
export CROWDSEC_SYSTEMCTL=$work/bin/systemctl
export CROWDSEC_POLICY_RC_D=$work/policy-rc.d
export CROWDSEC_TEST_SYSTEMCTL=$work/systemctl

install_stub() {
  [[ -f $CROWDSEC_TEST_PRESEED ]] || {
    echo "package install ran before the CrowdSec opt-out" >&2
    return 1
  }
  [[ ${SYSTEMD_OFFLINE:-} == 1 ]] || {
    echo "package install ran without systemd offline mode" >&2
    return 1
  }
  [[ -x $CROWDSEC_POLICY_RC_D ]] || {
    echo "package install ran without a policy-rc.d guard" >&2
    return 1
  }
  if "$CROWDSEC_POLICY_RC_D"; then
    echo "policy-rc.d allowed package service startup" >&2
    return 1
  else
    policy_status=$?
  fi
  [[ $policy_status -eq 101 ]] || {
    echo "policy-rc.d returned the wrong refusal status" >&2
    return 1
  }
  mapfile -t install_systemctl_calls <"$CROWDSEC_TEST_SYSTEMCTL"
  [[ ${install_systemctl_calls[5]} == 'mask crowdsec-hubupdate.timer crowdsec-hubupdate.service' ]] || {
    echo "CrowdSec hub update units were not masked before package install" >&2
    return 1
  }
  [[ ${install_systemctl_calls[6]} == 'unmask --runtime crowdsec.service crowdsec-firewall-bouncer.service' ]] || {
    echo "only CrowdSec package-enabled services were not unmasked" >&2
    return 1
  }
  printf '%s\n' "$@" >"$work/packages"
}

printf '#!/usr/bin/env bash\necho original-policy\n' >"$CROWDSEC_POLICY_RC_D"
chmod 0700 "$CROWDSEC_POLICY_RC_D"
crowdsec_install_offline install_stub crowdsec=1.8.1 crowdsec-firewall-bouncer-nftables=0.0.36
grep -qxF 'crowdsec crowdsec/capi boolean false' "$work/preseed" || {
  echo "CrowdSec was not opted out before package install" >&2
  exit 1
}
grep -qxF 'crowdsec=1.8.1' "$work/packages" || { echo "pinned CrowdSec package was not installed" >&2; exit 1; }
grep -qxF 'echo original-policy' <(tail -n 1 "$CROWDSEC_POLICY_RC_D") || {
  echo "existing policy-rc.d was not restored" >&2
  exit 1
}
[[ $(stat -c %a "$CROWDSEC_POLICY_RC_D") == 700 ]] || {
  echo "existing policy-rc.d mode was not restored" >&2
  exit 1
}
mapfile -t systemctl_calls <"$CROWDSEC_TEST_SYSTEMCTL"
[[ ${systemctl_calls[6]} == 'unmask --runtime crowdsec.service crowdsec-firewall-bouncer.service' ]] || {
  echo "CrowdSec runtime masks were not removed for package enablement" >&2
  exit 1
}
if [[ ${systemctl_calls[8]} != 'disable crowdsec.service' ]] || \
  [[ ${systemctl_calls[10]} != 'disable crowdsec-firewall-bouncer.service' ]] || \
  [[ ${systemctl_calls[12]} != 'disable crowdsec-hubupdate.timer' ]]; then
  echo "package-enabled CrowdSec units were not disabled after package install" >&2
  exit 1
fi
[[ ${systemctl_calls[13]} == 'mask --runtime crowdsec.service crowdsec-firewall-bouncer.service crowdsec-hubupdate.timer crowdsec-hubupdate.service' ]] || {
  echo "CrowdSec runtime masks were not restored after package install" >&2
  exit 1
}

: >"$CROWDSEC_TEST_SYSTEMCTL"
export CROWDSEC_TEST_BOUNCER_ABSENT=yes
crowdsec_install_offline install_stub crowdsec=1.8.1
unset CROWDSEC_TEST_BOUNCER_ABSENT
mapfile -t systemctl_calls <"$CROWDSEC_TEST_SYSTEMCTL"
[[ ${systemctl_calls[8]} == 'disable crowdsec.service' ]] || {
  echo "installed CrowdSec unit was not disabled when the bouncer was absent" >&2
  exit 1
}
[[ ${systemctl_calls[10]} == 'show --property=LoadState --value crowdsec-hubupdate.timer' ]] || {
  echo "absent bouncer did not skip only its disable" >&2
  exit 1
}
[[ ${systemctl_calls[12]} == 'mask --runtime crowdsec.service crowdsec-firewall-bouncer.service crowdsec-hubupdate.timer crowdsec-hubupdate.service' ]] || {
  echo "absent bouncer prevented final runtime masks" >&2
  exit 1
}

install_must_not_run() {
  : >"$work/install-ran"
}
export CROWDSEC_TEST_STOP_FAIL=yes CROWDSEC_TEST_SHOW_FAIL=yes
if crowdsec_install_offline install_must_not_run crowdsec=1.8.1; then
  echo "package install continued after service state query failure" >&2
  exit 1
else
  status=$?
fi
unset CROWDSEC_TEST_STOP_FAIL CROWDSEC_TEST_SHOW_FAIL
[[ $status -eq 71 ]] || { echo "service state query failure returned $status, not 71" >&2; exit 1; }
[[ ! -e $work/install-ran ]] || { echo "package install ran after service state query failure" >&2; exit 1; }

install_failure_stub() {
  [[ ${SYSTEMD_OFFLINE:-} == 1 && -x $CROWDSEC_POLICY_RC_D ]] || return 90
  return 42
}
policy_target=$work/original-policy
printf '#!/usr/bin/env bash\necho symlink-policy\n' >"$policy_target"
rm -f "$CROWDSEC_POLICY_RC_D"
ln -s "$policy_target" "$CROWDSEC_POLICY_RC_D"
: >"$CROWDSEC_TEST_SYSTEMCTL"
if crowdsec_install_offline install_failure_stub crowdsec=1.8.1; then
  echo "failed package install returned success" >&2
  exit 1
else
  status=$?
fi
[[ $status -eq 42 ]] || { echo "failed package install returned $status, not 42" >&2; exit 1; }
[[ -L $CROWDSEC_POLICY_RC_D ]] && [[ $(readlink "$CROWDSEC_POLICY_RC_D") == "$policy_target" ]] || {
  echo "existing policy-rc.d symlink was not restored after install failure" >&2
  exit 1
}
mapfile -t systemctl_calls <"$CROWDSEC_TEST_SYSTEMCTL"
if [[ ${systemctl_calls[8]} != 'disable crowdsec.service' ]] || \
  [[ ${systemctl_calls[10]} != 'disable crowdsec-firewall-bouncer.service' ]] || \
  [[ ${systemctl_calls[12]} != 'disable crowdsec-hubupdate.timer' ]]; then
  echo "package-enabled CrowdSec units were not disabled after install failure" >&2
  exit 1
fi
[[ ${systemctl_calls[13]} == 'mask --runtime crowdsec.service crowdsec-firewall-bouncer.service crowdsec-hubupdate.timer crowdsec-hubupdate.service' ]] || {
  echo "CrowdSec runtime masks were not restored after install failure" >&2
  exit 1
}

export CROWDSEC_TEST_CLEANUP_MASK_FAIL=yes
if crowdsec_install_offline install_failure_stub crowdsec=1.8.1; then
  echo "cleanup failure returned success" >&2
  exit 1
else
  status=$?
fi
unset CROWDSEC_TEST_CLEANUP_MASK_FAIL
[[ $status -eq 72 ]] || { echo "cleanup failure returned $status, not 72" >&2; exit 1; }
[[ -L $CROWDSEC_POLICY_RC_D ]] && [[ $(readlink "$CROWDSEC_POLICY_RC_D") == "$policy_target" ]] || {
  echo "policy-rc.d symlink was not restored after mask failure" >&2
  exit 1
}

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
