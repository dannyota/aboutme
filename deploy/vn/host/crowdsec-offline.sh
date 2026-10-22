#!/usr/bin/env bash
# Shared CrowdSec central API opt-out checks. Callers set the paths when a
# test uses a temporary root.

crowdsec_preseed_offline() {
  local selected
  printf '%s\n' \
    'crowdsec crowdsec/lapi boolean false' \
    'crowdsec crowdsec/capi boolean false' \
    'crowdsec crowdsec/lapi_host string 127.0.0.1:8095' | debconf-set-selections || return $?
  selected=$(printf '%s\n' 'GET crowdsec/lapi' 'GET crowdsec/capi' 'GET crowdsec/lapi_host' |
    debconf-communicate crowdsec 2>/dev/null) || return $?
  [[ $selected == $'0 false\n0 false\n0 127.0.0.1:8095' ]]
}

crowdsec_install_offline() { # install-command package...
  (
    set -euo pipefail
    local policy=${CROWDSEC_POLICY_RC_D:-/usr/sbin/policy-rc.d}
    local systemctl=${CROWDSEC_SYSTEMCTL:-systemctl}
    local backup='' backup_dir='' had_policy=no cleanup_status=0 load_state state status unit spec
    local -a enable_units=(crowdsec.service crowdsec-firewall-bouncer.service)
    local -a hub_units=(crowdsec-hubupdate.timer crowdsec-hubupdate.service)
    local -a disable_units=("${enable_units[@]}" crowdsec-hubupdate.timer)
    local -a units=("${enable_units[@]}" "${hub_units[@]}")

    if ((EUID==0)); then
      # This checks configuration only. It grants no database admission.
      python3 -I - /etc/crowdsec/config.yaml.local /etc/crowdsec/config.yaml.postgresql >/dev/null 2>&1 <<'PY' || exit 1
import os,stat,sys,yaml
configs=[]
for path in sys.argv[1:]:
    info=os.lstat(path)
    assert stat.S_ISREG(info.st_mode) and info.st_uid==0 and info.st_nlink==1
    assert stat.S_IMODE(info.st_mode)==0o644
    configs.append(yaml.safe_load(open(path)))
assert configs[0]==configs[1]
db=configs[0]['db_config']
assert db['type']=='pgx' and db['db_name']==db['user']=='crowdsec'
assert db['db_path']=='/run/postgresql' and db['host']=='' and db['port']==0
assert db['max_open_conns']==4 and db['flush']=={'max_age':'0s','max_items':None}
assert 'password' not in db and 'use_wal' not in db
assert 'online_client' not in configs[0]['api']['server']
PY
      for spec in "$@"; do
        if [[ $spec == crowdsec-firewall-bouncer-nftables=* ]]; then
          python3 -I - /etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local >/dev/null 2>&1 <<'PY' || exit 1
import os,stat,sys,yaml
path=sys.argv[1]; info=os.lstat(path)
assert stat.S_ISREG(info.st_mode) and info.st_uid==info.st_gid==0
assert info.st_nlink==1 and stat.S_IMODE(info.st_mode)==0o600
key=yaml.safe_load(open(path))['api_key']
assert isinstance(key,str) and key and not any(c in key for c in '$<>\n\r')
PY
        fi
      done
    fi
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
    DEBIAN_FRONTEND=noninteractive SYSTEMD_OFFLINE=1 CROWDSEC_SETUP_UNATTENDED_DISABLE=1 "$@" >/dev/null 2>&1
  )
}

crowdsec_admitted_cli() {
  /usr/local/lib/aboutme/crowdsec-db.sh preflight-ready </dev/null >/dev/null 2>&1 || return 1
  /usr/bin/cscli "$@"
}

crowdsec_assert_postgresql() {
  (
    set -euo pipefail
    umask 077
    local work
    work=$(mktemp -d /run/aboutme/crowdsec-config-proof.XXXXXX)
    trap 'rm -rf -- "$work"' EXIT
    crowdsec_admitted_cli config show --key Config.DbConfig -o json >"$work/active" 2>/dev/null
    crowdsec_admitted_cli -c /etc/crowdsec/config.yaml.postgresql config show \
      --key Config.DbConfig -o json >"$work/candidate" 2>/dev/null
    python3 -I - "$work/active" "$work/candidate" >/dev/null 2>&1 <<'PY'
import json,sys
active,candidate=(json.load(open(path)) for path in sys.argv[1:])
assert active==candidate
assert active['Type']=='pgx' and active['DbName']==active['User']=='crowdsec'
assert active['DbPath']=='/run/postgresql' and active['Host']=='' and active['Port']==0
assert active['SSLMode']=='disable' and active['MaxOpenConns']==4
assert active['Password']=='' and active['UseWal'] is None
assert active['Flush']['MaxItems'] is None
PY
  )
}

crowdsec_configure_local_detection() {
  local item_type
  local -a item_types=(collections parsers scenarios postoverflows contexts)

  install -d -m 0755 /etc/crowdsec/acquis.d || return $?
  : >/etc/crowdsec/acquis.yaml || return $?
  find /etc/crowdsec/acquis.d -mindepth 1 -maxdepth 1 -delete || return $?
  for item_type in "${item_types[@]}"; do
    crowdsec_admitted_cli "$item_type" remove --all --force >/dev/null 2>&1 || return $?
  done
  crowdsec_admitted_cli collections install crowdsecurity/sshd >/dev/null 2>&1 || return $?
  rm -f /etc/crowdsec/scenarios/ssh-time-based-bf.yaml || return $?
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
    /^db_config:/ { skip_db = 1; next }
    skip_db && (/^[[:space:]]/ || /^$/) { next }
    { skip_db = 0 }
    /^    online_client:/ { skip = 1; next }
    skip && /^      / { next }
    { skip = 0; print }
  ' "$base" >"$tmp"
  install -m 0600 "$tmp" "$base"
  rm -f "$tmp" "$credentials"
  crowdsec_assert_offline "$base" "$local_config" "$credentials"
}
