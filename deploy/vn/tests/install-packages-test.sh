#!/usr/bin/env bash
# Exercise installer blocks and migration admission without host access or root.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
export FIXTURE_ROOT=$root
export FIXTURE_SUPPORT=$work/commands.sh FIXTURE_LIB=$work/lib
export FIXTURE_MIGRATION=$work/migrate.sh
lib=$FIXTURE_LIB
mkdir -p "$work/lib" "$work/config/bouncers" "$work/runtime"
printf 'login: fixture-machine\n' >"$work/config/local_api_credentials.yaml"
chmod 0600 "$work/config/local_api_credentials.yaml"

cat >"$FIXTURE_SUPPORT" <<'SH'
lib=$FIXTURE_LIB
bundle=$FIXTURE_ROOT/deploy/vn
tmp=$FIXTURE_CASE/tmp
operation_bundle_fd=9
die() { printf 'install: %s\n' "$*" >&2; exit 1; }
say() { :; }
dpkg-query() {
  local package=${!#} state version
  case $package in
    crowdsec) state=$ENGINE_STATE; version=${ENGINE_VERSION:-$CROWDSEC_VERSION} ;;
    crowdsec-firewall-bouncer-nftables)
      state=$BOUNCER_STATE; version=${BOUNCER_VERSION:-$CROWDSEC_BOUNCER_VERSION} ;;
    *) return 2 ;;
  esac
  [[ $state != missing ]] || return 1
  case $2 in
    '-f=${Status}\t${Version}') printf '%s\t%s' "$state" "$version" ;;
    '-f=${Version}') printf '%s' "$version" ;;
    *) return 2 ;;
  esac
}
apt-get() {
  printf 'apt %s\n' "$*" >>"$CALLS"
  [[ ${FIXTURE_APT_INSTALL:-no} == yes ]] || return 0
  [[ " $* " == *' --allow-change-held-packages '* ]] || return 100
  [[ ${FIXTURE_APT_FAIL:-no} != yes ]] || return 100
  [[ ${!#} == "crowdsec=$CROWDSEC_VERSION" ]] || return 2
  ENGINE_STATE='install ok installed'
  printf '%s\n' "$ENGINE_STATE" >"$FIXTURE_CASE/unpacked-state"
  printf '%s\n' "$ENGINE_STATE" >"$FIXTURE_CASE/engine-state"
  [[ ${FIXTURE_APT_FAIL:-no} != unpacked ]] || return 100
  [[ ${FIXTURE_APT_WRONG_VERSION:-no} == yes ]] || ENGINE_VERSION=$CROWDSEC_VERSION
  return 0
}
apt-mark() {
  local package
  printf 'mark %s\n' "$*" >>"$CALLS"
  case $1 in
    showhold)
      [[ $ENGINE_STATE != hold\ * ]] || printf 'crowdsec\n'
      [[ $BOUNCER_STATE != hold\ * ]] || printf 'crowdsec-firewall-bouncer-nftables\n'
      ;;
    hold)
      shift
      for package in "$@"; do
        if [[ $package == crowdsec ]]; then
          ENGINE_STATE="hold ${ENGINE_STATE#* }"
          printf '%s\n' "$ENGINE_STATE" >"$FIXTURE_CASE/engine-state"
        fi
      done
      ;;
    *) return 2 ;;
  esac
  return 0
}
install() { printf 'install %s\n' "$*" >>"$CALLS"; }
systemd-tmpfiles() { :; }
mountpoint() { [[ -e $FIXTURE_CASE/log-mounted ]]; }
findmnt() {
  case $* in
    *FSTYPE*) printf 'tmpfs\n' ;;
    *OPTIONS*) printf 'nosuid,nodev,noexec,mode=750,uid=10001,gid=10001,size=64M\n' ;;
    *) return 2 ;;
  esac
}
setpriv() { printf 'setpriv %s\n' "$*" >>"$CALLS"; }
openssl() { printf 'fixture-bootstrap-value\n'; }
systemctl() {
  printf 'unit %s\n' "$*" >>"$CALLS"
  case $1 in
    is-enabled) [[ ! -e $FIXTURE_CASE/masked ]] ;;
    is-active) [[ ${!#} == "$ACTIVE_EDGE" && ! -e $FIXTURE_CASE/stopped ]] ;;
    mask | disable) touch "$FIXTURE_CASE/masked" ;;
    stop) touch "$FIXTURE_CASE/stopped" ;;
  esac
}
crowdsec_install_offline() (
  trap - EXIT
  printf 'offline\n' >>"$CALLS"
  systemctl stop crowdsec.service crowdsec-firewall-bouncer.service
  systemctl mask --runtime crowdsec.service crowdsec-firewall-bouncer.service
  "$@"
)
stat() { printf '0:0:600\n'; }
python3() { cat >/dev/null; printf 'fixture-machine'; }
cscli() {
  printf 'cli %s\n' "$*" >>"$CALLS"
  case $* in
    'machines list -o json') printf '[{"machineId":"fixture-machine","isValidated":true}]\n' ;;
    'bouncers list -o json') printf '[{"name":"aboutme-caddy"},{"name":"aboutme-firewall"}]\n' ;;
    *) return 2 ;;
  esac
}
SH

# Keep the real no-op and registration predicates; replace host/root boundaries.
sed -e 's/\$EUID == 0/1 == 1/g' \
  -e "s|/etc/crowdsec|$work/config|g" \
  -e "s|/run/aboutme|$work/runtime|g" \
  "$root/deploy/vn/host/crowdsec-migrate.sh" >"$FIXTURE_MIGRATION"
cat >"$lib/crowdsec-migrate.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
source "$FIXTURE_SUPPORT"
source "$FIXTURE_MIGRATION"
lib=$FIXTURE_LIB
migrate_lock() { :; }
migrate_idle() { :; }
migrate_notice() { :; }
migrate_stop() { systemctl stop "${migrate_consumers[@]}"; }
migrate_fresh_admission() { return 1; }
migrate_run
printf 'migrate-no-op\n' >>"$CALLS"
SH
cat >"$lib/crowdsec-db.sh" <<'SH'
#!/usr/bin/env bash
[[ $* == preflight-ready ]] || exit 1
printf 'gate\n' >>"$CALLS"
SH
cat >"$lib/crowdsec-retention-lease-check.sh" <<'SH'
#!/usr/bin/env bash
printf 'lease\n' >>"$CALLS"
SH
cat >"$lib/crowdsec-retention-fail.sh" <<'SH'
#!/usr/bin/env bash
printf 'close\n' >>"$CALLS"
SH
chmod +x "$lib/"*.sh

installer=${1:-$root/deploy/vn/host/install.sh}
# Extract complete production blocks so a changed branch affects this proof.
{
  printf '#!/usr/bin/env bash\nset -euo pipefail\nsource "$FIXTURE_SUPPORT"\n'
  sed -n '/^POSTGRESQL_COMMON_VERSION=/,/^CROWDSEC_BOUNCER_VERSION=/p' "$installer"
  sed -n '/^# Installs packages at/,/^# copy_tree/{ /^# copy_tree/d; p; }' "$installer"
  # This fixture owns package and migration admission; lock behavior has its
  # own real-lock installer fixture.
  printf 'install_admission_transition() { [[ $1 != --shared-fence ]] || shift; "$@"; }\n'
  awk '/^crowdsec_install_closed=no$/ {copy=1} copy {print} /^trap cleanup_install EXIT$/ {exit}' "$installer"
  cat <<'SH'
if [[ ${VERIFY_PACKAGE:-no} != no ]]; then
  if [[ $VERIFY_PACKAGE == offline ]]; then
    crowdsec_install_closed=yes
    crowdsec_install_offline install_pinned "crowdsec=$CROWDSEC_VERSION" || die "crowdsec_package_refused"
  else
    install_pinned "crowdsec=$CROWDSEC_VERSION"
  fi
  crowdsec_install_closed=no
  exit 0
fi
SH
  awk '/^for package_pin in / {copy=1} copy {print} copy && /^done$/ {exit}' "$installer"
  awk '/^edge_restart=\(\)/ {copy=1} copy {print} copy && /^systemd-tmpfiles --create/ {exit}' "$installer"
  sed -n '/^ABOUTME_MIGRATE_BUNDLE_FD=/,/^if ((${#edge_restart/p' "$installer"
  printf 'crowdsec_install_closed=no\nprintf "complete\\n" >>"$CALLS"\n'
} | sed -e "s|/etc/crowdsec|$work/config|g" \
  -e 's|/var/lib/aboutme|$FIXTURE_CASE/state|g' >"$work/install.sh"
[[ $(grep -c '^for package_pin in ' "$work/install.sh") == 1 ]]
[[ $(grep -c '^edge_restart=()' "$work/install.sh") == 1 ]]
[[ $(grep -c '^ABOUTME_MIGRATE_BUNDLE_FD=' "$work/install.sh") == 1 ]]

fail() { printf 'install-packages-test: %s\n' "$*" >&2; exit 1; }
new_case() {
  export FIXTURE_CASE=$work/case CALLS=$work/calls
  rm -rf -- "$FIXTURE_CASE"
  mkdir -p "$FIXTURE_CASE/tmp" "$FIXTURE_CASE/state"
  cp "$root/deploy/vn/host/crowdsec-retention.sh" "$lib/crowdsec-retention.sh"
  touch "$FIXTURE_CASE/log-mounted"
  : >"$CALLS"
}

for ENGINE_STATE in 'hold ok installed' 'install ok installed'; do
  export ENGINE_STATE
  for BOUNCER_STATE in 'hold ok installed' 'install ok installed'; do
    export BOUNCER_STATE
    for ACTIVE_EDGE in aboutme-caddy.service aboutme-maintenance.service; do
      export ACTIVE_EDGE
      new_case
      bash "$work/install.sh" >"$work/out" 2>"$work/err" || fail 'matching pins refused reinstall'
      [[ ! -s $work/out && ! -s $work/err ]] || fail 'no-op printed diagnostics'
      grep -qxF migrate-no-op "$CALLS" || fail 'migration did not finish as a no-op'
      [[ $(grep -cxF gate "$CALLS") == 4 ]] || fail 'migration skipped database admission'
      grep -qxF lease "$CALLS" || fail 'migration skipped lease admission'
      grep -qxF 'cli machines list -o json' "$CALLS" || fail 'migration skipped machine admission'
      grep -qxF 'cli bouncers list -o json' "$CALLS" || fail 'migration skipped bouncer admission'
      grep -qxF complete "$CALLS" || fail 'installer did not complete'
      for package in crowdsec crowdsec-firewall-bouncer-nftables; do
        grep -qxF "mark hold $package" "$CALLS" || fail 'package hold missing'
      done
      if grep -qE '^(close|offline|apt |setpriv |unit (stop|mask|disable|unmask|start|restart) )' "$CALLS"; then
        fail 'matching pins changed consumer state or reinstalled packages'
      fi
      [[ ! -e $FIXTURE_CASE/masked && ! -e $FIXTURE_CASE/stopped ]]
    done
  done
done
printf 'PASS matching-held-and-unheld-pins-preserve-both-edge-modes\n'

# An installed version alone cannot admit an absent, broken, or different pin.
export ENGINE_STATE='hold ok installed' BOUNCER_STATE='hold ok installed'
for fault in missing unpacked half-configured removal reinstall-required version; do
  new_case
  ENGINE_STATE='hold ok installed'
  unset ENGINE_VERSION
  case $fault in
    missing) ENGINE_STATE=missing ;;
    unpacked) ENGINE_STATE='hold ok unpacked' ;;
    half-configured) ENGINE_STATE='hold ok half-configured' ;;
    removal) ENGINE_STATE='deinstall ok installed' ;;
    reinstall-required) ENGINE_STATE='hold reinstreq installed' ;;
    version) export ENGINE_VERSION=1.8.0 ;;
  esac
  export ENGINE_STATE
  if bash "$work/install.sh" >"$work/out" 2>"$work/err"; then fail "accepted $fault package"; fi
  grep -qxF offline "$CALLS" || fail "bypassed package isolation for $fault"
  grep -qxF close "$CALLS" || fail "failed package install stayed open for $fault"
done
unset ENGINE_VERSION
printf 'PASS missing-broken-and-wrong-version-packages-stay-closed\n'

export VERIFY_PACKAGE=yes
for ENGINE_STATE in 'install ok installed' 'hold ok installed' 'hold ok unpacked'; do
  export ENGINE_STATE
  new_case
  status=0
  bash "$work/install.sh" >"$work/out" 2>"$work/err" || status=$?
  if [[ $ENGINE_STATE == 'hold ok unpacked' ]]; then
    [[ $status != 0 ]] || fail 'post-install check accepted unpacked package'
  else
    [[ $status == 0 ]] || fail 'post-install check rejected installed pin'
    grep -qxF 'mark hold crowdsec' "$CALLS" || fail 'post-install hold missing'
  fi
done
printf 'PASS post-install-check-requires-installed-pin-with-either-selection\n'

export FIXTURE_APT_INSTALL=yes
for package_state in missing unheld held-older held-exact; do
  new_case
  export ENGINE_VERSION=1.8.0
  case $package_state in
    missing) export ENGINE_STATE=missing ;;
    unheld) export ENGINE_STATE='install ok installed' ;;
    held-older) export ENGINE_STATE='hold ok installed' ;;
    held-exact) export ENGINE_STATE='hold ok installed' ENGINE_VERSION=1.8.2 ;;
  esac
  bash "$work/install.sh" >"$work/out" 2>"$work/err" || fail "pinned install refused $package_state package"
  [[ ! -s $work/out && ! -s $work/err ]] || fail 'pinned install printed diagnostics'
  expected=$'mark showhold\napt install -y -qq --allow-downgrades --allow-change-held-packages crowdsec=1.8.2\nmark hold crowdsec'
  [[ $(<"$CALLS") == "$expected" ]] || fail 'pinned install changed apt arguments or hold order'
done
printf 'PASS pinned-install-and-held-upgrade-apply-hold-after-exact-version-check\n'

for VERIFY_PACKAGE in yes offline; do
  export VERIFY_PACKAGE
  for fault in apt unpacked version; do
    new_case
    export ENGINE_STATE='hold ok installed' ENGINE_VERSION=1.8.0
    export FIXTURE_APT_FAIL=no FIXTURE_APT_WRONG_VERSION=no
    case $fault in
      apt) FIXTURE_APT_FAIL=yes ;;
      unpacked) FIXTURE_APT_FAIL=unpacked ;;
      version) FIXTURE_APT_WRONG_VERSION=yes ;;
    esac
    status=0
    bash "$work/install.sh" >"$work/out" 2>"$work/err" || status=$?
    expected_status=1
    if [[ $VERIFY_PACKAGE == yes && $fault != version ]]; then expected_status=100; fi
    [[ $status == "$expected_status" ]] || fail "$fault failure lost its status through $VERIFY_PACKAGE"
    expected=$'mark showhold\napt install -y -qq --allow-downgrades --allow-change-held-packages crowdsec=1.8.2\nmark hold crowdsec'
    if [[ $VERIFY_PACKAGE == offline ]]; then
      expected=$'offline\nunit stop crowdsec.service crowdsec-firewall-bouncer.service\nunit mask --runtime crowdsec.service crowdsec-firewall-bouncer.service\n'"$expected"$'\nclose'
    fi
    [[ $(<"$CALLS") == "$expected" ]] || fail "$fault failure did not restore only the requested hold after apt"
    [[ $(<"$FIXTURE_CASE/engine-state") == 'hold ok installed' ]] || fail "$fault failure left the engine unheld"
    if [[ $fault != apt ]]; then
      [[ $(<"$FIXTURE_CASE/unpacked-state") == 'install ok installed' ]] || fail "$fault fixture did not change the selection"
    fi
    if [[ $fault == version ]]; then
      grep -qxF 'install: crowdsec is not installed at pinned 1.8.2' "$work/err" || fail 'version failure lost its diagnostic'
      if [[ $VERIFY_PACKAGE == offline ]]; then
        grep -qxF 'install: crowdsec_package_refused' "$work/err" || fail 'offline install lost its refusal'
      fi
    fi
  done
done
printf 'PASS failed-held-upgrades-restore-holds-before-refusing\n'
