#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
# shellcheck source=../host/crowdsec-offline.sh
source "$repo_root/deploy/vn/host/crowdsec-offline.sh"

if [[ ${1:-} == packages ]]; then
  [[ $EUID == 0 && ${GITHUB_ACTIONS:-} == true ]] || exit 1
  proof=$(mktemp -d /run/crowdsec-package-proof.XXXXXX)
  mount -t tmpfs -o size=512m,mode=0700 tmpfs "$proof"
  trap 'status=$?; systemctl mask --runtime crowdsec.service crowdsec-firewall-bouncer.service >/dev/null 2>&1; umount "$proof"; rmdir "$proof"; exit "$status"' EXIT
  trap 'echo "FAIL pinned-package-proof" >&2' ERR
  install -d -m 0755 /etc/apt/keyrings
  install -m 0644 "$repo_root/deploy/vn/host/keys/pgdg.asc" /etc/apt/keyrings/postgresql.asc
  gpg --dearmor --yes -o /etc/apt/keyrings/crowdsec.gpg "$repo_root/deploy/vn/host/keys/crowdsec.asc" 2>/dev/null
  [[ $(gpg --show-keys --with-colons "$repo_root/deploy/vn/host/keys/crowdsec.asc" 2>/dev/null |
    awk -F: '$1=="pub" {want=1;next} want&&$1=="fpr" {print $10;want=0}') == 6A89E3C2303A901A889971D3376ED5326E93CD0C ]]
  printf '%s\n' 'deb [signed-by=/etc/apt/keyrings/crowdsec.gpg] https://packagecloud.io/crowdsec/crowdsec/ubuntu noble main' >/etc/apt/sources.list.d/crowdsec.list
  printf '%s\n' 'deb [signed-by=/etc/apt/keyrings/postgresql.asc] https://apt.postgresql.org/pub/repos/apt noble-pgdg main' >/etc/apt/sources.list.d/pgdg.list
  apt-get update -qq >/dev/null 2>&1
  apt-get -o Dir::Log::Terminal=/dev/null -o Dir::Log::History=/dev/null -o DPkg::Options::=--log=/dev/null \
    install -y -qq postgresql-common=293.pgdg24.04+1 postgresql-18=18.6-1.pgdg24.04+2 \
    postgresql-client-18=18.6-1.pgdg24.04+2 python3-yaml nftables gettext-base strace >/dev/null 2>&1
  (
    cd "$proof"
    apt-get download crowdsec=1.8.1 crowdsec-firewall-bouncer-nftables=0.0.36 >/dev/null 2>&1
  )
  printf '%s\n' \
    "d639e87924abe497618795378c13a425e0ffab2bbaecbf50900fae55cbed0ad0  $proof/crowdsec_1.8.1_amd64.deb" \
    "fb3f38d222793cd031dac7e2b33076033a05c5bb476f323129d431cdc8b69b78  $proof/crowdsec-firewall-bouncer-nftables_0.0.36_amd64.deb" |
    sha256sum --check --status
  for spec in crowdsec:1.8.1 crowdsec-firewall-bouncer-nftables:0.0.36; do
    name=${spec%:*}; version=${spec#*:}; archive=$proof/${name}_${version}_amd64.deb
    [[ $(dpkg-deb -f "$archive" Package) == "$name" ]]
    [[ $(dpkg-deb -f "$archive" Version) == "$version" ]]
    [[ $(dpkg-deb -f "$archive" Architecture) == amd64 ]]
    # The reviewed archive digest binds every generated control and payload
    # member, not only tagged postinst source. Inspect all controls privately.
    dpkg-deb -e "$archive" "$proof/$name.control"
    for member in control preinst postinst prerm postrm conffiles md5sums; do
      [[ -f $proof/$name.control/$member && ! -L $proof/$name.control/$member ]]
    done
    [[ ! -e $proof/$name.control/config && ! -e $proof/$name.control/triggers ]]
    cp "$archive" /var/cache/apt/archives/
  done
  dpkg-deb --fsys-tarfile "$proof/crowdsec-firewall-bouncer-nftables_0.0.36_amd64.deb" |
    tar -xOf - ./etc/systemd/system/crowdsec-firewall-bouncer.service >"$proof/firewall-unit"
  python3 -I - "$proof/firewall-unit" \
    "$repo_root/deploy/vn/host/etc/systemd/system/crowdsec-firewall-bouncer.service.d/90-aboutme-alert.conf" <<'PY'
import pathlib,sys
commands=[]
for path in sys.argv[1:]:
    for line in pathlib.Path(path).read_text().splitlines():
        if line.startswith('ExecStartPre='):
            value=line.split('=',1)[1]
            if value: commands.append(value)
            else: commands=[]
gate='/usr/local/lib/aboutme/crowdsec-retention-lease-check.sh'
assert commands==[gate,'/usr/bin/crowdsec-firewall-bouncer -c /etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml -t',gate]
PY
  install -d -m 0755 /etc/crowdsec /etc/systemd/system/crowdsec.service.d \
    /etc/systemd/system/crowdsec-firewall-bouncer.service.d
  for unit in crowdsec crowdsec-firewall-bouncer; do
    install -m 0644 "$repo_root/deploy/vn/host/etc/systemd/system/$unit.service.d/90-aboutme-alert.conf" \
      "/etc/systemd/system/$unit.service.d/90-aboutme-alert.conf"
  done
  # PGDG skips default creation when any cluster exists. Runner clusters must
  # release the default socket port before this proof starts PostgreSQL 18.
  systemctl stop postgresql.service >/dev/null 2>&1
  if ! pg_lsclusters --no-header 18 main >/dev/null 2>&1; then
    pg_createcluster --port=5432 --start-conf=manual 18 main >/dev/null 2>&1
  fi
  systemctl daemon-reload
  for file in pg_hba.conf pg_ident.conf; do
    install -o postgres -g postgres -m 0640 "$repo_root/deploy/vn/host/etc/postgresql/18/main/$file" "/etc/postgresql/18/main/$file"
  done
  install -o postgres -g postgres -m 0644 "$repo_root/deploy/vn/host/etc/postgresql/18/main/conf.d/90-aboutme.conf" \
    /etc/postgresql/18/main/conf.d/90-aboutme.conf
  systemctl restart postgresql@18-main.service
  for config in config.yaml.local config.yaml.postgresql; do
    install -m 0644 "$repo_root/deploy/vn/edge/crowdsec/config.yaml.local" "/etc/crowdsec/$config"
  done
  install -d -m 0700 /run/aboutme/crowdsec
  [[ $(runuser -u postgres -- /usr/lib/postgresql/18/bin/psql -X -qAt -w \
    -h /run/postgresql -p 5432 -d postgres -v ON_ERROR_STOP=1 \
    -c "SELECT current_setting('server_version_num') = '180006'
      AND current_setting('config_file') = '/etc/postgresql/18/main/postgresql.conf';" 2>/dev/null) == t ]]
  catalog() {
    runuser -u postgres -- /usr/lib/postgresql/18/bin/psql -X -qAt -w -h /run/postgresql -p 5432 -d postgres \
      -v ON_ERROR_STOP=1 -c 'SELECT datname FROM pg_database ORDER BY datname; SELECT rolname FROM pg_roles ORDER BY rolname;' 2>/dev/null
  }
  before=$(catalog | sha256sum)
  [[ $(runuser -u postgres -- /usr/lib/postgresql/18/bin/psql -X -qAt -w -h /run/postgresql -p 5432 -d postgres \
    -c "SELECT count(*) FROM pg_database WHERE datname='crowdsec';" 2>/dev/null) == 0 ]]
  crowdsec_install_offline apt-get -o Dir::Log::Terminal=/dev/null -o Dir::Log::History=/dev/null \
    -o DPkg::Options::=--log=/dev/null install -y -qq crowdsec=1.8.1
  [[ $(dpkg-query -W -f='${Status}\t${Version}' crowdsec) == $'install ok installed\t1.8.1' ]]
  # Read loaded command metadata only. Services stay disabled and the missing
  # startup gate still denies starts; restore runtime denial before assertions.
  systemctl unmask --runtime crowdsec.service >/dev/null 2>&1
  systemctl daemon-reload
  systemctl show -p ExecStartPre -p ExecStart -p ExecReload crowdsec.service >"$proof/engine-commands"
  systemctl mask --runtime crowdsec.service >/dev/null 2>&1
  python3 -I - "$proof/engine-commands" <<'PY'
import pathlib,re,sys
commands={}
# systemctl prints one assignment per command under the same property name.
for line in pathlib.Path(sys.argv[1]).read_text().splitlines():
    key,value=line.split('=',1)
    commands[key]=commands.get(key,'')+' '+value
gate='/usr/local/lib/aboutme/crowdsec-retention-lease-check.sh'
def paths(key): return re.findall(r'\bpath=([^ ;]+)',commands[key])
assert paths('ExecStartPre')==[gate,'/usr/bin/crowdsec',gate]
assert paths('ExecStart')==['/usr/bin/crowdsec']
assert paths('ExecReload')==[gate,'/usr/bin/crowdsec',gate,'/bin/kill']
assert '-t -error' in commands['ExecStartPre'] and '-t -error' in commands['ExecReload']
PY
  [[ ! -s /etc/crowdsec/local_api_credentials.yaml && ! -s /etc/crowdsec/online_api_credentials.yaml ]]
  [[ ! -s /etc/crowdsec/acquis.yaml ]]
  [[ -z $(find /etc/crowdsec/acquis.d -mindepth 1 -maxdepth 1 -type f -print -quit) ]]
  crowdsec_remove_online /etc/crowdsec/config.yaml /etc/crowdsec/config.yaml.local /etc/crowdsec/online_api_credentials.yaml
  # Only these exact calls have reviewed non-database paths. Their outputs
  # include configuration and therefore never leave private descriptors.
  /usr/bin/cscli hub update >/dev/null 2>&1
  /usr/bin/cscli hub upgrade >/dev/null 2>&1
  CROWDSEC_SETUP_UNATTENDED_DISABLE=1 /usr/bin/cscli setup unattended >/dev/null 2>&1
  for field in Config.API.Server Config.API.Server.Enable Config.API.Server.ListenURI; do
    /usr/bin/cscli config show --key "$field" >/dev/null 2>&1
  done
  /usr/bin/cscli config show -oraw --key Config.API.Server.ListenURI >/dev/null 2>&1
  [[ $(catalog | sha256sum) == "$before" ]]
  [[ -z $(find /var/lib/crowdsec -type f -name '*.db*' -print -quit) ]]
  install -m 0644 "$repo_root/deploy/vn/edge/crowdsec/profiles.yaml" /etc/crowdsec/profiles.yaml
  install -m 0644 "$repo_root/deploy/vn/edge/crowdsec/console.yaml" /etc/crowdsec/console.yaml
  printf 'PASS signed-pinned-artifacts-and-generated-controls\nPASS suppressed-package-and-pure-calls-without-database\n'
  exit 0
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"

cat >"$work/bin/debconf-set-selections" <<'STUB'
#!/usr/bin/env bash
cat >"$CROWDSEC_TEST_PRESEED"
STUB
chmod +x "$work/bin/debconf-set-selections"
cat >"$work/bin/debconf-communicate" <<'STUB'
#!/usr/bin/env bash
while read -r verb key; do
  [[ $verb == GET ]] || exit 1
  if [[ ${CROWDSEC_TEST_BAD_SELECTION:-} == yes ]]; then printf '0 true\n'
  elif [[ $key == crowdsec/lapi_host ]]; then printf '0 127.0.0.1:8095\n'
  else printf '0 false\n'; fi
done
STUB
chmod +x "$work/bin/debconf-communicate"
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
  [[ ${DEBIAN_FRONTEND:-} == noninteractive && ${CROWDSEC_SETUP_UNATTENDED_DISABLE:-} == 1 ]] || {
    echo "package setup suppression is missing" >&2
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
grep -qxF 'crowdsec crowdsec/lapi boolean false' "$work/preseed"
grep -qxF 'crowdsec crowdsec/lapi_host string 127.0.0.1:8095' "$work/preseed"
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
export CROWDSEC_TEST_BAD_SELECTION=yes
if crowdsec_install_offline install_must_not_run crowdsec=1.8.1; then
  echo "package install accepted inconsistent suppression" >&2
  exit 1
fi
unset CROWDSEC_TEST_BAD_SELECTION
[[ ! -e $work/install-ran ]] || { echo "package install ran after suppression refusal" >&2; exit 1; }
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
db_config:
  type: sqlite
  use_wal: true
  password: forbidden-config
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
! grep -qE 'db_config:|sqlite|use_wal:|password:' "$base" || { echo "inherited database config was not removed" >&2; exit 1; }
grep -q trusted_ips "$base" || { echo "config after online_client was removed" >&2; exit 1; }
[[ ! -e $credentials ]] || { echo "central credentials were not removed" >&2; exit 1; }

printf 'api:\n  server:\n    online_client:\n      credentials_path: forbidden\n' >"$local_config"
if crowdsec_assert_offline "$base" "$local_config" "$credentials" 2>/dev/null; then
  echo "offline proof accepted inherited central API config" >&2
  exit 1
fi

echo "CrowdSec offline tests passed"
