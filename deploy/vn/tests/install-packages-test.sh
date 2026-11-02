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
apt-get() { printf 'apt %s\n' "$*" >>"$CALLS"; }
apt-mark() { printf 'mark %s\n' "$*" >>"$CALLS"; }
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
crowdsec_install_offline() {
  printf 'offline\n' >>"$CALLS"
  systemctl stop crowdsec.service crowdsec-firewall-bouncer.service
  systemctl mask --runtime crowdsec.service crowdsec-firewall-bouncer.service
  "$@"
}
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
  awk '/^crowdsec_install_closed=no$/ {copy=1} copy {print} /^trap cleanup_install EXIT$/ {exit}' "$installer"
  cat <<'SH'
if [[ ${VERIFY_PACKAGE:-no} == yes ]]; then
  install_pinned "crowdsec=$CROWDSEC_VERSION"
  exit 0
fi
SH
  awk '/^for package_pin in / {copy=1} copy {print} copy && /^done$/ {exit}' "$installer"
  awk '/^edge_restart=\(\)/ {copy=1} copy {print} copy && /^systemd-tmpfiles --create/ {exit}' "$installer"
  sed -n '/^ABOUTME_MIGRATE_BUNDLE_FD=/,/^if ((${#edge_restart/p' "$installer"
  printf 'crowdsec_install_closed=no\nprintf "complete\\n" >>"$CALLS"\n'
} | sed "s|/etc/crowdsec|$work/config|g" >"$work/install.sh"
[[ $(grep -c '^for package_pin in ' "$work/install.sh") == 1 ]]
[[ $(grep -c '^edge_restart=()' "$work/install.sh") == 1 ]]
[[ $(grep -c '^ABOUTME_MIGRATE_BUNDLE_FD=' "$work/install.sh") == 1 ]]

fail() { printf 'install-packages-test: %s\n' "$*" >&2; exit 1; }
new_case() {
  export FIXTURE_CASE=$work/case CALLS=$work/calls
  rm -rf -- "$FIXTURE_CASE"
  mkdir -p "$FIXTURE_CASE/tmp"
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
