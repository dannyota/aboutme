#!/usr/bin/env bash
# Exercise engine activation and helper ordering without host services.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
installer=${1:-$root/deploy/vn/host/install.sh}
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
export FIXTURE_BUNDLE=$root/deploy/vn FIXTURE_CASE=$work/case
export FIXTURE_INSTALL FIXTURE_UID FIXTURE_GID FIXTURE_SUPPORT=$work/support.sh
FIXTURE_INSTALL=$(command -v ginstall || command -v install)
FIXTURE_UID=$(id -u)
FIXTURE_GID=$(id -g)
fail() { printf 'crowdsec-install-test: %s\n' "$*" >&2; exit 1; }

python3 - "$installer" "$work/install.sh" <<'PY'
import pathlib,sys
source=pathlib.Path(sys.argv[1]).read_text()
def block(start,end):
    return source.split(start,1)[1].split(end,1)[0]
helpers=block('# Serialize service operations with retention and watchdog admission.','# copy_tree')
early=block('# 2. CrowdSec config','# 3. Packages')
early=early[early.index('\n'):]
packages=block('# 3. Packages','chown postgres:postgres')
packages=packages[packages.index('\n'):]
config=block('# 4. Configuration files.','# Copy and reload peer policy')
config=config[config.index('install',config.index('90-aboutme.conf')):]
scripts='copy_script() {'+block('copy_script() {','for f in "$bundle"/host/bin/')
activation=block('ABOUTME_MIGRATE_BUNDLE_FD=', 'if ((${#edge_restart[@]}))')
activation='ABOUTME_MIGRATE_BUNDLE_FD='+activation
activation=activation.replace('"$lib/crowdsec-migrate.sh"','fixture_migrate')
body=helpers+early+packages+config+scripts+activation
body=body.replace(' /etc',' "$FIXTURE_CASE/etc"')
body=body.replace('"/etc','"$FIXTURE_CASE/etc')
body=body.replace('/usr/local/lib/aboutme','$FIXTURE_CASE/lib')
body=body.replace('/run/aboutme','$FIXTURE_CASE/run')
body=body.replace('/var/lib/aboutme','$FIXTURE_CASE/state')
pathlib.Path(sys.argv[2]).write_text(
    '#!/usr/bin/env bash\nset -euo pipefail\nsource "$FIXTURE_SUPPORT"\n'+body)
PY

cat >"$FIXTURE_SUPPORT" <<'SH'
bundle=$FIXTURE_BUNDLE
lib=$FIXTURE_CASE/lib
tmp=$FIXTURE_CASE/tmp
operation_bundle_fd=9
POSTGRESQL_COMMON_VERSION=fixture
die() { printf 'install: %s\n' "$*" >&2; exit 1; }
say() { :; }
install_pinned() { [[ ${FIXTURE_FAULT:-} != package ]]; }
flock() {
  python3 - "$@" <<'PY'
import fcntl,sys
mode=fcntl.LOCK_SH if '-s' in sys.argv else fcntl.LOCK_EX
fcntl.flock(int(sys.argv[-1]),mode|fcntl.LOCK_NB)
PY
}
stat() {
  command stat "$@" | sed -E "s/^$FIXTURE_UID:$FIXTURE_GID:/0:0:/;s/^$FIXTURE_UID:/0:/"
}
locked() {
  python3 - "$FIXTURE_CASE" <<'PY'
import fcntl,pathlib,sys
root=pathlib.Path(sys.argv[1])
for path,mode,blocked in [('run/crowdsec-retention.lock',fcntl.LOCK_SH,True),
                          ('state/fence.lock',fcntl.LOCK_EX,True),
                          ('state/fence.lock',fcntl.LOCK_SH,False)]:
    with (root/path).open('a') as f:
        try: fcntl.flock(f,mode|fcntl.LOCK_NB)
        except BlockingIOError: assert blocked, path+' blocks service admission'
        else: assert not blocked, path+' omitted exclusion'
PY
}
install() {
  local -a args=()
  while (($#)); do
    case $1 in -o | -g) shift 2 ;; *) args+=("$1"); shift ;; esac
  done
  if [[ ${args[-1]} == "$lib/crowdsec-retention.sh" ]]; then
    locked || return 1
    if [[ $(<"$FIXTURE_CASE/engine") == active ]]; then
      cmp -s "$FIXTURE_CASE/loaded-profile" "$bundle/edge/crowdsec/profiles.yaml" || {
        printf 'batching installed on old profile\n' >&2
        return 1
      }
      [[ -e $FIXTURE_CASE/healthy ]] || return 1
    fi
    printf 'helper\n' >>"$FIXTURE_CASE/calls"
    [[ ${FIXTURE_FAULT:-} != helper ]] || return 1
  fi
  "$FIXTURE_INSTALL" -o "$FIXTURE_UID" -g "$FIXTURE_GID" "${args[@]}"
}
fixture_migrate() {
  printf 'migration\n' >>"$FIXTURE_CASE/calls"
  if [[ ! -e $FIXTURE_CASE/migrated ]]; then
    [[ -x $lib/crowdsec-retention.sh ]] || return 1
    cp "$FIXTURE_CASE/etc/crowdsec/profiles.yaml" "$FIXTURE_CASE/loaded-profile"
    cp "$FIXTURE_CASE/etc/crowdsec/acquis.d/aboutme-http.yaml" "$FIXTURE_CASE/loaded-acquisition"
    printf 'active\n' >"$FIXTURE_CASE/engine"
    touch "$FIXTURE_CASE/migrated"
  fi
}
systemctl() {
  local unit=${!#}
  if [[ $unit == aboutme-caddy.service || $unit == aboutme-maintenance.service ]]; then
    printf 'edge-mutated\n' >>"$FIXTURE_CASE/calls"
    return 2
  fi
  case $1 in
    restart)
      [[ $unit == crowdsec.service ]] || return 2
      locked || return 1
      printf 'restart\n' >>"$FIXTURE_CASE/calls"
      [[ ${FIXTURE_FAULT:-} != restart ]] || return 1
      printf '42\n' >"$FIXTURE_CASE/engine-job"
      rm -f "$FIXTURE_CASE/healthy"
      cp "$FIXTURE_CASE/etc/crowdsec/profiles.yaml" "$FIXTURE_CASE/loaded-profile"
      cp "$FIXTURE_CASE/etc/crowdsec/acquis.d/aboutme-http.yaml" "$FIXTURE_CASE/loaded-acquisition"
      ;;
    show)
      if [[ $unit == crowdsec.service ]]; then
        if [[ -s $FIXTURE_CASE/engine-job ]]; then
          locked || return 1
          printf 'ActiveState=active\nJob=42\n'
          touch "$FIXTURE_CASE/pending-observed"
          [[ ${FIXTURE_FAULT:-} == job ]] || : >"$FIXTURE_CASE/engine-job"
        else
          rm -f "$FIXTURE_CASE/pending-observed"
          if [[ ${FIXTURE_FAULT:-} == state ]]; then
            printf 'ActiveState=activating\nJob=\n'
          else printf 'ActiveState=%s\nJob=\n' "$(<"$FIXTURE_CASE/engine")"; fi
        fi
      elif [[ $unit == postgresql@18-main.service ]]; then
        printf 'ActiveState=active\nJob=\n'
      else return 2; fi
      ;;
    start)
      [[ $unit == aboutme-crowdsec-retention-failure.service ]] || return 2
      locked || return 1
      [[ ! -e $FIXTURE_CASE/run/crowdsec-retention-ok &&
        -s $FIXTURE_CASE/run/crowdsec-retention-failed ]] || return 1
      printf 'closed\n' >>"$FIXTURE_CASE/calls"
      ;;
    *) return 2 ;;
  esac
}
cscli() {
  [[ $* == 'lapi status' ]] || return 2
  locked || return 1
  if [[ -e $FIXTURE_CASE/pending-observed ]]; then
    printf 'premature-lapi\n' >>"$FIXTURE_CASE/calls"
    return 1
  fi
  printf 'lapi\n' >>"$FIXTURE_CASE/calls"
  [[ ${FIXTURE_FAULT:-} != lapi ]] || return 1
  if [[ ! -e $FIXTURE_CASE/health-retried ]]; then
    touch "$FIXTURE_CASE/health-retried"
    return 1
  fi
  touch "$FIXTURE_CASE/healthy"
}
timeout() {
  [[ $1 != --kill-after=* ]] || shift
  shift
  if [[ $1 == "$lib/crowdsec-db.sh" ]]; then
    [[ $2 == preflight-ready ]] || return 2
    locked || return 1
    printf 'gate\n' >>"$FIXTURE_CASE/calls"
    [[ ${FIXTURE_FAULT:-} != gate ]]
  else "$@"; fi
}
# Advance only the polling budget; production sleep remains bounded.
sleep() { SECONDS=$((SECONDS + 2)); }
SH

new_case() {
  rm -rf -- "$FIXTURE_CASE"
  mkdir -p "$FIXTURE_CASE/"{etc,lib,run,state,tmp}
  chmod 0755 "$FIXTURE_CASE/run"
  touch "$FIXTURE_CASE/run/crowdsec-retention.lock" "$FIXTURE_CASE/state/fence.lock"
  chmod 0600 "$FIXTURE_CASE/run/crowdsec-retention.lock" "$FIXTURE_CASE/state/fence.lock"
  printf 'inactive\n' >"$FIXTURE_CASE/engine"
  : >"$FIXTURE_CASE/calls"
  bash "$work/install.sh" >"$work/out" 2>"$work/err" || fail 'fresh install refused stopped bootstrap'
  [[ -x $FIXTURE_CASE/lib/crowdsec-retention.sh ]] || fail 'fresh install omitted startup helper'
  printf 'lease\n' >"$FIXTURE_CASE/run/crowdsec-retention-ok"
  : >"$FIXTURE_CASE/calls"
}
run_install() {
  rm -rf -- "$FIXTURE_CASE/tmp"
  mkdir "$FIXTURE_CASE/tmp"
  bash "$work/install.sh" >"$work/out" 2>"$work/err"
}
expect_activation() {
  [[ $(grep -cxF restart "$FIXTURE_CASE/calls") == 1 ]] || fail 'configuration change did not restart once'
  [[ $(grep -cxF lapi "$FIXTURE_CASE/calls") -ge 2 ]] || fail 'engine readiness was not polled'
  [[ ! -s $FIXTURE_CASE/engine-job ]] || fail 'engine job outlived locks'
  [[ ! -e $FIXTURE_CASE/state/crowdsec-engine-pending ]] || fail 'successful activation kept pending marker'
  if grep -qE '^(premature-lapi|edge-mutated)$' "$FIXTURE_CASE/calls"; then fail 'engine activation skipped admission or changed Caddy'; fi
  python3 - "$FIXTURE_CASE/calls" <<'PY'
import pathlib,sys
calls=pathlib.Path(sys.argv[1]).read_text().splitlines()
assert calls.index('migration')<calls.index('restart')<calls.index('lapi')<calls.index('helper')
PY
}

unlocked() {
  python3 - "$FIXTURE_CASE" <<'PY'
import fcntl,pathlib,sys
root=pathlib.Path(sys.argv[1])
for name in ('run/crowdsec-retention.lock','state/fence.lock'):
    with (root/name).open('a') as f: fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB)
PY
}

new_case
run_install || fail 'unchanged reinstall failed'
if grep -qE '^(restart|lapi|closed)$' "$FIXTURE_CASE/calls"; then fail 'unchanged reinstall changed service state'; fi
printf 'PASS unchanged-engine-config-restarts-nothing\n'
printf 'inactive\n' >"$FIXTURE_CASE/engine"
: >"$FIXTURE_CASE/calls"
run_install || fail 'unchanged stopped engine refused reinstall'
[[ $(<"$FIXTURE_CASE/engine") == inactive ]] || fail 'unchanged reinstall started engine'
if grep -qE '^(restart|lapi|closed)$' "$FIXTURE_CASE/calls"; then fail 'unchanged stopped engine changed service state'; fi
unlocked || fail 'unchanged reinstall leaked locks'
printf 'PASS unchanged-stopped-engine-stays-stopped\n'

for file in config.yaml.local config.yaml.postgresql console.yaml profiles.yaml \
  acquis.d/aboutme-http.yaml acquis.d/sshd.yaml parsers/s01-parse/aboutme-http.yaml \
  parsers/s02-enrich/zz-aboutme-http-source.yaml scenarios/aboutme-http-status.yaml \
  scenarios/aboutme-http-rate.yaml; do
  new_case
  target=$FIXTURE_CASE/etc/crowdsec/$file
  [[ -f $target ]] || fail "missing configuration fixture: $file"
  printf '\n# drift\n' >>"$target"
  rm -f "$FIXTURE_CASE/health-retried"
  run_install || fail "changed engine config refused: $file"
  expect_activation
  unlocked || fail 'successful activation leaked locks'
done
printf 'PASS every-copied-engine-config-restarts-once-and-waits-under-locks\n'

for fault in acquisition mode missing; do
  new_case
  case $fault in
    acquisition) printf 'legacy\n' >"$FIXTURE_CASE/etc/crowdsec/acquis.d/caddy.yaml" ;;
    mode) chmod 0600 "$FIXTURE_CASE/etc/crowdsec/console.yaml" ;;
    missing) rm "$FIXTURE_CASE/etc/crowdsec/profiles.yaml" ;;
  esac
  rm -f "$FIXTURE_CASE/health-retried"
  run_install || fail "configuration repair refused: $fault"
  expect_activation
done
printf 'PASS removed-acquisition-and-repaired-metadata-or-missing-file-restart\n'

new_case
printf 'legacy\n' >"$FIXTURE_CASE/etc/crowdsec/acquis.d/caddy.yaml"
printf '\n# drift\n' >>"$FIXTURE_CASE/etc/crowdsec/profiles.yaml"
chmod 0600 "$FIXTURE_CASE/etc/crowdsec/console.yaml"
rm -f "$FIXTURE_CASE/health-retried"
run_install || fail 'multiple configuration changes refused'
expect_activation
[[ ! -e $FIXTURE_CASE/etc/crowdsec/acquis.d/caddy.yaml ]] || fail 'legacy acquisition survived'
printf 'PASS multiple-config-changes-and-acquisition-removal-share-one-restart\n'

new_case
printf 'old-profile\n' >"$FIXTURE_CASE/loaded-profile"
printf '#!/bin/sh\n# eager reconciliation\n' >"$FIXTURE_CASE/lib/crowdsec-retention.sh"
printf '\n# drift\n' >>"$FIXTURE_CASE/etc/crowdsec/profiles.yaml"
rm -f "$FIXTURE_CASE/health-retried"
run_install || fail 'profile activation failed'
expect_activation
printf 'PASS batching-helper-follows-loaded-profile\n'

# A previous interrupted copy can leave matching disk config with an old helper.
new_case
printf 'old-profile\n' >"$FIXTURE_CASE/loaded-profile"
printf '#!/bin/sh\n# eager reconciliation\n' >"$FIXTURE_CASE/lib/crowdsec-retention.sh"
rm -f "$FIXTURE_CASE/health-retried"
run_install || fail 'interrupted upgrade could not activate'
expect_activation
printf 'PASS old-helper-requires-activation-even-with-matching-disk-config\n'

new_case
printf '\n# old acquisition\n' >>"$FIXTURE_CASE/etc/crowdsec/acquis.d/aboutme-http.yaml"
cp "$FIXTURE_CASE/etc/crowdsec/acquis.d/aboutme-http.yaml" "$FIXTURE_CASE/loaded-acquisition"
cp "$FIXTURE_CASE/lib/crowdsec-retention.sh" "$work/unchanged-helper"
if FIXTURE_FAULT=package run_install; then fail 'package interruption was accepted'; fi
cmp "$work/unchanged-helper" "$FIXTURE_CASE/lib/crowdsec-retention.sh" || fail 'interruption changed helper'
cmp "$FIXTURE_BUNDLE/edge/crowdsec/acquis.d/aboutme-http.yaml" \
  "$FIXTURE_CASE/etc/crowdsec/acquis.d/aboutme-http.yaml" || fail 'interruption preceded configuration copy'
if cmp -s "$FIXTURE_CASE/loaded-acquisition" "$FIXTURE_CASE/etc/crowdsec/acquis.d/aboutme-http.yaml"; then
  fail 'interrupted configuration was already active'
fi
pending=$FIXTURE_CASE/state/crowdsec-engine-pending
[[ -f $pending && ! -L $pending && $(stat -c '%u:%g:%a:%h' "$pending") == "$FIXTURE_UID:$FIXTURE_GID:600:1" ]] ||
  fail 'configuration interruption lost private pending activation'
if grep -qxF restart "$FIXTURE_CASE/calls"; then fail 'package interruption reached activation'; fi
rm -f "$FIXTURE_CASE/health-retried"
: >"$FIXTURE_CASE/calls"
run_install || fail 'configuration-only retry refused activation'
expect_activation
cmp "$FIXTURE_CASE/loaded-acquisition" "$FIXTURE_CASE/etc/crowdsec/acquis.d/aboutme-http.yaml" || fail 'retry left old acquisition active'
unlocked || fail 'configuration-only retry leaked locks'
: >"$FIXTURE_CASE/calls"
run_install || fail 'activated configuration refused reinstall'
if grep -qE '^(restart|lapi|closed)$' "$FIXTURE_CASE/calls"; then fail 'activated configuration restarted again'; fi
printf 'PASS configuration-only-interruption-retries-activation-with-unchanged-helper\n'

for fault in restart state job lapi gate helper; do
  new_case
  printf 'old-profile\n' >"$FIXTURE_CASE/loaded-profile"
  printf '#!/bin/sh\n# eager reconciliation\n' >"$FIXTURE_CASE/lib/crowdsec-retention.sh"
  cp "$FIXTURE_CASE/lib/crowdsec-retention.sh" "$work/old-helper"
  printf '\n# drift\n' >>"$FIXTURE_CASE/etc/crowdsec/profiles.yaml"
  if FIXTURE_FAULT=$fault run_install; then fail "accepted failed activation: $fault"; fi
  grep -qxF closed "$FIXTURE_CASE/calls" || fail "activation failure stayed open: $fault"
  cmp "$work/old-helper" "$FIXTURE_CASE/lib/crowdsec-retention.sh" || fail 'failed activation enabled batching'
  [[ -f $FIXTURE_CASE/state/crowdsec-engine-pending ]] || fail 'failed activation lost pending marker'
  unlocked || fail 'failed activation leaked locks'
done
printf 'PASS failed-or-unready-engine-closes-before-lock-release-and-keeps-old-helper\n'
