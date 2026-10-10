#!/usr/bin/env bash
# Shared CrowdSec central API opt-out checks. Callers set the paths when a
# test uses a temporary root.

crowdsec_preseed_offline() {
  printf 'crowdsec crowdsec/capi boolean false\n' | debconf-set-selections
}

crowdsec_install_offline() { # install-command package...
  (
    set -euo pipefail
    local policy=${CROWDSEC_POLICY_RC_D:-/usr/sbin/policy-rc.d}
    local systemctl=${CROWDSEC_SYSTEMCTL:-systemctl}
    local backup= backup_dir= had_policy=no cleanup_status=0 load_state state status unit
    local -a enable_units=(crowdsec.service crowdsec-firewall-bouncer.service)
    local -a hub_units=(crowdsec-hubupdate.timer crowdsec-hubupdate.service)
    local -a disable_units=("${enable_units[@]}" crowdsec-hubupdate.timer)
    local -a units=("${enable_units[@]}" "${hub_units[@]}")

    crowdsec_preseed_offline || exit $?
    "$systemctl" stop "${units[@]}" 2>/dev/null || true
    for unit in "${units[@]}"; do
      state=$("$systemctl" show --property=ActiveState --value "$unit") || exit $?
      case $state in
        inactive | failed) ;;
        *)
          echo "CrowdSec install refused while $unit is $state" >&2
          exit 1
          ;;
      esac
    done

    if [[ -e $policy || -L $policy ]]; then
      had_policy=yes
      backup_dir=$(mktemp -d "${policy}.aboutme.XXXXXX") || exit $?
      backup=$backup_dir/policy-rc.d
    fi

    cleanup() {
      status=$?
      trap - EXIT
      for unit in "${disable_units[@]}"; do
        if load_state=$("$systemctl" show --property=LoadState --value "$unit"); then
          if [[ $load_state != not-found ]]; then
            "$systemctl" disable "$unit" || cleanup_status=$?
          fi
        else
          cleanup_status=$?
        fi
      done
      "$systemctl" mask --runtime "${units[@]}" || cleanup_status=$?
      if [[ $had_policy == no || -e $backup || -L $backup ]]; then
        rm -f -- "$policy" || cleanup_status=$?
      fi
      if [[ -e $backup || -L $backup ]]; then
        mv -- "$backup" "$policy" || cleanup_status=$?
      fi
      if [[ -n $backup_dir ]]; then
        rmdir -- "$backup_dir" || cleanup_status=$?
      fi
      if ((cleanup_status != 0)); then
        echo "CrowdSec install cleanup failed with status $cleanup_status after status $status" >&2
        exit "$cleanup_status"
      fi
      exit "$status"
    }
    trap cleanup EXIT

    if [[ $had_policy == yes ]]; then
      mv -- "$policy" "$backup" || exit $?
    fi
    printf '#!/bin/sh\nexit 101\n' >"$policy" || exit $?
    chmod 0755 "$policy" || exit $?
    "$systemctl" mask "${hub_units[@]}" || exit $?
    "$systemctl" unmask --runtime "${enable_units[@]}" || exit $?
    SYSTEMD_OFFLINE=1 "$@"
  )
}

crowdsec_assert_offline() { # base-config local-config credentials
  local base=$1 local_config=$2 credentials=$3 file
  for file in "$base" "$local_config"; do
    [[ -f $file && ! -L $file ]] || {
      echo "CrowdSec config $file is missing or is a symlink" >&2
      return 1
    }
    if grep -Eq '^[[:space:]]+online_client:' "$file"; then
      echo "CrowdSec central API config remains in $file" >&2
      return 1
    fi
  done
  if [[ -e $credentials || -L $credentials ]]; then
    echo "CrowdSec central API credentials remain at $credentials" >&2
    return 1
  fi
}

crowdsec_remove_online() { # base-config local-config credentials
  local base=$1 local_config=$2 credentials=$3 tmp
  tmp=$(mktemp "${base}.XXXXXX")
  awk '
    /^    online_client:/ { skip = 1; next }
    skip && /^      / { next }
    { skip = 0; print }
  ' "$base" >"$tmp"
  install -m 0600 "$tmp" "$base"
  rm -f "$tmp" "$credentials"
  crowdsec_assert_offline "$base" "$local_config" "$credentials"
}
