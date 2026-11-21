#!/usr/bin/env bash
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
{
  printf '#!/usr/bin/env bash\nset -euo pipefail\nsource "$FIXTURE_SUPPORT"\n'
  sed -n '/^POSTGRESQL_COMMON_VERSION=/,/^CROWDSEC_BOUNCER_VERSION=/p' "$installer"
  sed -n '/^# Installs packages at/,/^# copy_tree/{ /^# copy_tree/d; p; }' "$installer"
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

new_case
export FIXTURE_BUNDLE=$work/bundle
mkdir -p "$FIXTURE_BUNDLE/host" "$FIXTURE_BUNDLE/scripts"
for helper in host/crowdsec-privacy-notice-host.sh host/workload-lock.sh \
  scripts/fence.sh scripts/deploy-safety.sh scripts/deploy-host.sh \
  scripts/crowdsec-privacy-notice.sh scripts/restore-drill.sh; do
  touch "$FIXTURE_BUNDLE/$helper"
done
{
  printf '%s\n' 'set -euo pipefail' 'source "$FIXTURE_SUPPORT"' 'bundle=$FIXTURE_BUNDLE'
  sed -n '/^for required in /,/^done$/p' "$installer"
} >"$work/required-helpers.sh"
for fault in missing symlink; do
  if [[ $fault == symlink ]]; then
    ln -s fence.sh "$FIXTURE_BUNDLE/scripts/image-prune.sh"
  fi
  if bash "$work/required-helpers.sh" >"$work/out" 2>"$work/err"; then
    fail "installer accepted a $fault image-prune.sh"
  fi
  grep -qF 'required notice helper is missing' "$work/err" || fail 'wrong helper refusal'
  ! grep -qF 'deploy-host.sh' "$CALLS" || fail 'installer replaced deploy-host.sh before its helper'
done
rm "$FIXTURE_BUNDLE/scripts/image-prune.sh"
touch "$FIXTURE_BUNDLE/scripts/image-prune.sh"
bash "$work/required-helpers.sh" || fail 'installer refused the complete helper set'
grep -qF " $lib/image-prune.sh" "$CALLS" || fail 'installer did not copy image-prune.sh'

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

pg_version=$(sed -n 's/^POSTGRESQL_COMMON_VERSION=//p' "$installer")
[[ -n $pg_version ]] || fail 'postgresql-common pin missing'
mkdir -p "$work/pg-calls"
awk -v dir="$work/pg-calls" '
  /^[[:space:]]*install_pinned / { n++; copy = 1 }
  copy { print > (dir "/" n); if ($0 !~ /\\$/) { copy = 0; close(dir "/" n) } }
' "$installer"
mapfile -t pg_calls < <(grep -lF 'postgresql-common=' "$work/pg-calls"/*)
((${#pg_calls[@]} == 2)) || fail 'postgresql-common install calls changed'
cat >"$work/pg-fixture.sh" <<'SH'
die() { printf 'install: %s\n' "$*" >&2; exit 1; }
dpkg-query() {
  [[ -f $PG_STATE/${!#} ]] || return 1
  printf '%s' "$(<"$PG_STATE/${!#}")"
}
apt-mark() {
  local package state
  case $1 in
    showhold)
      for package in "$PG_STATE"/*; do
        [[ ! -f $package || $(<"$package") != hold\ * ]] || printf '%s\n' "${package##*/}"
      done
      ;;
    hold)
      shift
      for package in "$@"; do
        printf 'mark hold %s\n' "$package" >>"$PG_CALLS"
        [[ -f $PG_STATE/$package ]] || return 1
        state=$(<"$PG_STATE/$package")
        printf 'hold %s' "${state#* }" >"$PG_STATE/$package"
      done
      ;;
    *) return 2 ;;
  esac
}
apt-get() {
  local spec common='' client=''
  printf 'apt %s\n' "$*" >>"$PG_CALLS"
  for spec in "$@"; do
    case $spec in
      postgresql-common=*) common=${spec#*=} ;;
      postgresql-client-common=*) client=${spec#*=} ;;
    esac
  done
  if [[ -z $client ]]; then
    client=$PG_CLIENT_CANDIDATE
    [[ ! -f $PG_STATE/postgresql-client-common ]] || client=$(cut -f2 "$PG_STATE/postgresql-client-common")
  fi
  if [[ -n $common && $client != "$common" ]]; then
    printf 'postgresql-common : Depends: postgresql-client-common (= %s) but %s is to be installed\n' \
      "$common" "$client" >&2
    return 100
  fi
  for spec in "$@" "postgresql-client-common=$client"; do
    [[ $spec == *=* ]] || continue
    [[ -f $PG_STATE/${spec%%=*} && $(cut -f2 "$PG_STATE/${spec%%=*}") == "${spec#*=}" ]] ||
      printf 'install ok installed\t%s' "${spec#*=}" >"$PG_STATE/${spec%%=*}"
  done
}
SH
for pg_call in "${pg_calls[@]}"; do
  for pg_host in fresh live; do
    pg_case=$work/pg-case
    rm -rf -- "$pg_case"
    mkdir -p "$pg_case/state"
    : >"$pg_case/calls"
    if [[ $pg_host == live ]]; then
      printf 'hold ok installed\t%s' "$pg_version" >"$pg_case/state/postgresql-common"
      printf 'install ok installed\t%s' "$pg_version" >"$pg_case/state/postgresql-client-common"
    fi
    {
      printf '#!/usr/bin/env bash\nset -euo pipefail\n'
      sed -n '/^POSTGRESQL_COMMON_VERSION=/,/^CROWDSEC_BOUNCER_VERSION=/p' "$installer"
      sed -n '/^# Installs packages at/,/^# copy_tree/{ /^# copy_tree/d; p; }' "$installer"
      cat "$work/pg-fixture.sh" "$pg_call"
    } >"$work/pg-install.sh"
    if ! PG_STATE=$pg_case/state PG_CALLS=$pg_case/calls PG_CLIENT_CANDIDATE=999.pgdg24.04+1 \
      bash "$work/pg-install.sh" >"$work/out" 2>"$work/err"; then
      cat "$work/err" >&2
      fail "$pg_host host refused the pinned postgresql-common install"
    fi
    pg_apt=$(grep '^apt ' "$pg_case/calls") || fail 'postgresql-common install skipped apt'
    [[ $(wc -l <<<"$pg_apt") == 1 && " $pg_apt " == *" postgresql-common=$pg_version "* &&
      " $pg_apt " == *" postgresql-client-common=$pg_version "* ]] ||
      fail "$pg_host host did not pin postgresql-client-common with postgresql-common"
    for package in postgresql-common postgresql-client-common; do
      [[ $(<"$pg_case/state/$package") == "hold ok installed"$'\t'"$pg_version" ]] ||
        fail "$pg_host host left $package unheld or off its pin"
    done
  done
done
printf 'PASS postgresql-common-installs-and-holds-client-common-at-its-pin\n'

pg_order=$(awk '
  BEGIN { path = "preamble" }
  /^if [[] "[$]only" = postgres []]; then$/ { path = "postgres-only"; next }
  path == "postgres-only" && /^fi$/ { path = "full"; next }
  /^[[:space:]]*install_pinned / {
    start = FNR; call = $0
    while (call ~ /\\$/ && (getline line) > 0) call = call " " line
    if (call ~ /"postgresql-common=/ && call ~ /"postgresql-client-common=/) print path, "common", start
    if (call ~ /"postgresql-18=/) print path, "server", start
  }
  /^[[:space:]]*chown postgres:postgres \/srv\/data\/postgresql$/ { print path, "chown", FNR }
' "$installer")
pg_step() { awk -v p="$pg_path" -v s="$1" '$1 == p && $2 == s { print $3 }' <<<"$pg_order"; }
for pg_path in postgres-only full; do
  pg_common=$(pg_step common) pg_chown=$(pg_step chown) pg_server=$(pg_step server)
  [[ $pg_common =~ ^[0-9]+$ && $pg_chown =~ ^[0-9]+$ && $pg_server =~ ^[0-9]+$ ]] ||
    fail "$pg_path path lost a PostgreSQL install step"
  ((pg_common < pg_chown)) || fail "$pg_path path chowns the data directory before installing postgresql-common"
  ((pg_chown < pg_server)) || fail "$pg_path path installs postgresql-18 before chowning the data directory"
done
printf 'PASS postgresql-data-directory-chown-runs-between-common-and-server-installs\n'

pg_server_version=$(sed -n 's/^POSTGRESQL_VERSION=//p' "$installer")
[[ -n $pg_server_version ]] || fail 'postgresql-18 pin missing'
pg_offline=${2:-$root/deploy/vn/tests/crowdsec-offline-test.sh}
pg_proof=$(awk '
  /^pinned_postgresql=[(]/ { copy = 1; sub(/^pinned_postgresql=[(]/, "") }
  copy { closed = sub(/[)].*/, ""); print; if (closed) exit }
' "$pg_offline" | tr -s ' \t' '\n\n' | sed '/^$/d' | sort)
pg_expected=$(printf '%s\n' "postgresql-common=$pg_version" "postgresql-client-common=$pg_version" \
  "postgresql-18=$pg_server_version" "postgresql-client-18=$pg_server_version" | sort)
[[ $pg_proof == "$pg_expected" ]] || fail 'CrowdSec package proof pins PostgreSQL off the installer pins'
printf 'PASS crowdsec-package-proof-pins-postgresql-at-installer-versions\n'

crowdsec_version=$(sed -n 's/^CROWDSEC_VERSION=//p' "$installer")
bouncer_version=$(sed -n 's/^CROWDSEC_BOUNCER_VERSION=//p' "$installer")
[[ -n $crowdsec_version && -n $bouncer_version ]] || fail 'CrowdSec pins missing'
collect=${3:-$root/deploy/vn/tests/ci-debs/collect.sh}
collect_tokens=$(awk '
  /^apt-get install .*--download-only/ { copy = 1 }
  copy { more = sub(/\\$/, ""); print; if (!more) exit }
' "$collect" | tr -s ' \t' '\n\n')
collect_pins() { { grep -E "^($1)(=|\$)" <<<"$collect_tokens" || true; } | sort; }
[[ $(collect_pins 'postgresql-common|postgresql-client-common|postgresql-18|postgresql-client-18') == "$pg_expected" ]] ||
  fail 'CI package collection pins PostgreSQL off the installer pins'
crowdsec_expected=$(printf '%s\n' "crowdsec=$crowdsec_version" \
  "crowdsec-firewall-bouncer-nftables=$bouncer_version" | sort)
[[ $(collect_pins 'crowdsec|crowdsec-firewall-bouncer-nftables') == "$crowdsec_expected" ]] ||
  fail 'CI package collection pins CrowdSec off the installer pins'
printf 'PASS ci-package-collection-pins-match-installer-versions\n'
