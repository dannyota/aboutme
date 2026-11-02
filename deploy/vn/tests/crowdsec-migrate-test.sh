#!/usr/bin/env bash
# Fake commands prove migration order without touching host state.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
# Redirect configuration paths; exercise the production predicates and activation.
sed -e "s|/etc/crowdsec/local_api_credentials.yaml|$work/local_api_credentials.yaml|g" \
  -e "s|/etc/crowdsec/|$work/config/|g" \
  -e "s|/run/aboutme|$work/runtime|g" \
  "$root/deploy/vn/host/crowdsec-migrate.sh" >"$work/crowdsec-migrate.sh"
# shellcheck source=../host/crowdsec-migrate.sh
source "$work/crowdsec-migrate.sh"
# Keep the real lock implementation for descriptor and contention proofs.
eval "$(declare -f migrate_lock | sed '1s/migrate_lock/real_migrate_lock/')"
eval "$(declare -f migrate_firewall_register | sed '1s/migrate_firewall_register/real_firewall_register/')"
eval "$(declare -f migrate_activate | sed '1s/migrate_activate/real_migrate_activate/')"
lib=$work/lib
mkdir -p "$lib"
mkdir -p "$work/runtime"
export MIGRATE_CALLS=$work/calls
export MIGRATE_FAILURE_MARKER=$work/runtime/crowdsec-retention-failed
cat >"$lib/crowdsec-db.sh" <<'SH'
#!/usr/bin/env bash
printf 'gate\n' >>"$MIGRATE_CALLS"
[[ ${MIGRATE_FAIL:-} != gate ]]
SH
cat >"$lib/crowdsec-provision.sh" <<'SH'
#!/usr/bin/env bash
printf 'provision %s\n' "$1" >>"$MIGRATE_CALLS"
[[ ${MIGRATE_FAIL:-} != provision ]]
SH
cat >"$lib/secrets-host.sh" <<'SH'
#!/usr/bin/env bash
printf 'secret %s\n' "$*" >>"$MIGRATE_CALLS"
[[ ${MIGRATE_FAIL:-} != caddy ]]
SH
cat >"$lib/crowdsec-retention-lease-check.sh" <<'SH'
#!/usr/bin/env bash
[[ ${MIGRATE_FAIL:-} != lease && ! -e $MIGRATE_FAILURE_MARKER ]]
SH
/usr/bin/chmod +x "$lib/"*
chmod() { :; }
systemctl() {
  if [[ $1 == is-active ]]; then [[ ${active_edge:-aboutme-caddy.service} == "$3" ]]; return; fi
  printf 'unit %s\n' "$*" >>"$MIGRATE_CALLS"
  case "$*" in
    'start aboutme-crowdsec-retention-startup.service') [[ ${MIGRATE_FAIL:-} != startup ]] ;;
    'enable --now aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer') [[ ${MIGRATE_FAIL:-} != timers ]] ;;
    unmask*) [[ ${MIGRATE_FAIL:-} != unmask ]] ;;
    'enable crowdsec.service crowdsec-firewall-bouncer.service') [[ ${MIGRATE_FAIL:-} != enable ]] ;;
    'start crowdsec.service') [[ ${MIGRATE_FAIL:-} != engine ]] ;;
    'start crowdsec-firewall-bouncer.service') [[ ${MIGRATE_FAIL:-} != bouncer ]] ;;
  esac
}
listed_machines='[{"machineId":"fixture-machine","isValidated":true}]'
listed_bouncers='[{"name":"aboutme-caddy"},{"name":"aboutme-firewall"}]'
cscli() {
  [[ ${MIGRATE_FAIL:-} != machine || $1 != machines ]] || return 1
  [[ ${MIGRATE_FAIL:-} != lapi || $1 != lapi ]] || return 1
  printf 'cli %s\n' "$*" >>"$MIGRATE_CALLS"
  case "$*" in
    'machines list -o json') printf '%s\n' "$listed_machines" ;;
    'bouncers list -o json') printf '%s\n' "$listed_bouncers" ;;
  esac
}
migrate_postgresql_selected() { [[ ${selected:-no} == yes ]]; }
migrate_activate() {
  if [[ ${MIGRATE_FAIL:-} == signal ]]; then kill -TERM "$BASHPID"; return 1; fi
  if [[ ${MIGRATE_FAIL:-} == activate ]]; then
    printf 'discarded-command-output\n'
    printf 'discarded-command-diagnostic\n' >&2
    return 1
  fi
  printf 'activate\n' >>"$MIGRATE_CALLS"
}
migrate_remove_sqlite() { [[ ${MIGRATE_FAIL:-} != discard ]] || return 1; printf 'discard\n' >>"$MIGRATE_CALLS"; }
migrate_firewall_register() { [[ ${MIGRATE_FAIL:-} != firewall ]] || return 1; printf 'firewall-register\n' >>"$MIGRATE_CALLS"; }
migrate_firewall_ready() { [[ ${MIGRATE_FAIL:-} != rebuild ]] || return 1; printf 'firewall-ready\n' >>"$MIGRATE_CALLS"; }
# Admission doubles keep the fixture away from host paths.
migrate_lock() { [[ ${lock_busy:-no} == no ]]; }
migrate_idle() { [[ ${owned:-no} == no ]]; }
migrate_notice() { [[ ${notice:-yes} == yes ]]; }
migrate_fresh_admission() { [[ ${selected:-no} != yes ]]; }
migrate_stop() { printf 'stop-joined\n' >>"$MIGRATE_CALLS"; [[ ${stop_failed:-no} == no ]]; }
refused() {
  local step=$1
  shift
  if (migrate_run "$@") >"$work/out" 2>"$work/err"; then exit 1; fi
  [[ ! -s $work/out ]]
  [[ $(cat "$work/err") == "crowdsec_migrate_refused step=$step" ]]
}
refused arguments --unknown
printf 'PASS argument-refusal-step\n'
for admission in lock_busy owned; do
  : >"$MIGRATE_CALLS"
  printf -v "$admission" yes
  if [[ $admission == lock_busy ]]; then step=lock; else step=idle; fi
  refused "$step" --reset
  [[ ! -s $MIGRATE_CALLS ]]
  printf -v "$admission" no
done
notice=no; refused notice --reset; [[ ! -s $MIGRATE_CALLS ]]; notice=yes
printf 'PASS lock-owner-and-notice-before-stop\n'
selected=yes
for credentials in missing empty malformed absent unvalidated complete; do
  rm -f "$MIGRATE_FAILURE_MARKER"
  rm -f "$work/local_api_credentials.yaml"
  listed_machines='[{"machineId":"fixture-machine","isValidated":true}]'
  case $credentials in
    missing) ;;
    empty) : >"$work/local_api_credentials.yaml" ;;
    malformed) printf 'login: [\n' >"$work/local_api_credentials.yaml" ;;
    *)
      printf 'login: fixture-machine\n' >"$work/local_api_credentials.yaml"
      case $credentials in
        absent) listed_machines='[{"machineId":"other-machine","isValidated":true}]' ;;
        unvalidated) listed_machines='[{"machineId":"fixture-machine","isValidated":false}]' ;;
      esac ;;
  esac
  if [[ -f $work/local_api_credentials.yaml ]]; then
    /usr/bin/chmod 0600 "$work/local_api_credentials.yaml"
  fi
  : >"$MIGRATE_CALLS"
  if [[ $credentials == complete ]]; then
    migrate_run
  else
    refused fresh-admission
    [[ $(tail -n1 "$MIGRATE_CALLS") == stop-joined ]]
    ! grep -q '^provision' "$MIGRATE_CALLS"
  fi
done
cat >"$work/no-op-expected" <<'CALLS'
gate
gate
gate
cli machines list -o json
gate
cli bouncers list -o json
unit is-enabled --quiet crowdsec.service
unit is-enabled --quiet crowdsec-firewall-bouncer.service
CALLS
cmp "$work/no-op-expected" "$MIGRATE_CALLS"
active_edge=none
: >"$MIGRATE_CALLS"
migrate_run
cmp "$work/no-op-expected" "$MIGRATE_CALLS"
unset active_edge
printf 'PASS complete-no-op-preserves-stopped-consumers\n'
listed_bouncers='[{"name":"aboutme-caddy"}]'
: >"$MIGRATE_CALLS"
refused fresh-admission
[[ $(tail -n1 "$MIGRATE_CALLS") == stop-joined ]]
! grep -q '^provision' "$MIGRATE_CALLS"
listed_bouncers='[{"name":"aboutme-caddy"},{"name":"aboutme-firewall"}]'
printf 'PASS missing-registration-refuses-occupied-catalog\n'
selected=no
: >"$MIGRATE_CALLS"
touch "$work/runtime/crowdsec-retention-failed"
migrate_run
[[ ! -e $work/runtime/crowdsec-retention-failed ]]
cat >"$work/expected" <<'CALLS'
stop-joined
provision fresh
activate
discard
gate
cli machines add -a --force --error
unit start aboutme-crowdsec-retention-startup.service
unit enable --now aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer
unit unmask --runtime crowdsec.service crowdsec-firewall-bouncer.service
unit enable crowdsec.service crowdsec-firewall-bouncer.service
unit start crowdsec.service
gate
secret bouncer-register
firewall-register
gate
unit start crowdsec-firewall-bouncer.service
firewall-ready
unit start aboutme-caddy.service
cli lapi status
gate
unit enable --now aboutme-watch.timer
CALLS
cmp "$work/expected" "$MIGRATE_CALLS"
printf 'PASS switch-and-watcher-order\n'
: >"$MIGRATE_CALLS"
MIGRATE_FAIL=provision refused provision
: >"$MIGRATE_CALLS"
migrate_run
grep -qxF 'provision fresh' "$MIGRATE_CALLS"
printf 'PASS pre-schema-retry\n'
# Inject failures across provisioning, registration, startup, and final checks.
for fault in provision:provision activate:activate discard:discard-sqlite machine:machine \
  startup:startup-retention timers:maintenance-timers unmask:unmask enable:enable \
  engine:engine caddy:caddy-register firewall:firewall-register bouncer:firewall-start \
  rebuild:firewall-ready lapi:lapi gate:machine lease:lease signal:activate; do
  IFS=: read -r failure step <<<"$fault"
  : >"$MIGRATE_CALLS"
  export MIGRATE_FAIL=$failure
  refused "$step" --reset
  [[ $(tail -n1 "$MIGRATE_CALLS") == stop-joined ]]
done
unset MIGRATE_FAIL
printf 'PASS failures-join-consumers\n'
active_edge=none
: >"$MIGRATE_CALLS"
touch "$work/runtime/crowdsec-retention-failed"
migrate_run --reset 2>"$work/err"
[[ ! -e $work/runtime/crowdsec-retention-failed ]]
grep -qxF crowdsec_migrate_no_edge "$work/err"
! grep -qE '^unit start aboutme-(caddy|maintenance)' "$MIGRATE_CALLS"
sed -e 's/provision fresh/provision restore-reset/' -e '/^unit start aboutme-caddy.service$/d' "$work/expected" >"$work/reset-expected"
cmp "$work/reset-expected" "$MIGRATE_CALLS"
printf 'PASS stopped-edge-reset-preserves-edge-state\n'
# The real stop set includes the watcher, and the shared bounded helper verifies it.
# shellcheck disable=SC2154 # The sourced migration defines this stop set.
[[ " ${migrate_consumers[*]} " == *' aboutme-watch.timer '* ]]
[[ " ${migrate_consumers[*]} " == *' aboutme-watch.service '* ]]
grep -q 'retention_stop' "$root/deploy/vn/host/crowdsec-migrate.sh"
printf 'PASS complete-stop-set\n'

# Verify the shared shutdown code detects a surviving job, kills it, and checks
# every unit again. All commands and state live in this fixture.
# shellcheck source=../host/crowdsec-retention-fail.sh
source "$root/deploy/vn/host/crowdsec-retention-fail.sh"
mkdir -p "$work/bin"
cat >"$work/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$MIGRATE_CALLS"
case $1 in
  stop) exit 1 ;;
  kill) touch "$MIGRATE_CALLS.killed" ;;
  show)
    if [[ -e $MIGRATE_CALLS.killed ]]; then state=inactive; job=; else state=deactivating; job=7; fi
    printf 'ActiveState=%s\nJob=%s\n' "$state" "$job"
    if [[ $* == *--property=MainPID* ]]; then printf 'MainPID=0\nControlPID=0\nControlGroup=\n'; fi ;;
esac
SH
printf '#!/usr/bin/env bash\nexit 1\n' >"$work/bin/podman"
/usr/bin/chmod +x "$work/bin/"*
export PATH="$work/bin:$PATH"
# shellcheck disable=SC2034 # The sourced shutdown helper consumes this command.
systemctl_cmd=$work/bin/systemctl
protected_units=("${migrate_consumers[@]}")
: >"$MIGRATE_CALLS"
retention_stop
for unit in "${protected_units[@]}"; do grep -q -- " $unit$" "$MIGRATE_CALLS"; done
rm -f "$MIGRATE_CALLS.killed"
if retention_quiescent; then exit 1; fi
printf 'PASS bounded-shutdown-verifies-every-unit\n'
mkdir "$work/operations"
/usr/bin/chmod 0700 "$work/operations"
migrate_bundle_path=$work/operations/.bundle.lock
: >"$migrate_bundle_path"
/usr/bin/chmod 0600 "$migrate_bundle_path"
exec {held_fd}>>"$migrate_bundle_path"
flock -x "$held_fd"
if (real_migrate_lock); then exit 1; fi
ABOUTME_MIGRATE_BUNDLE_FD=$held_fd real_migrate_lock
exec {held_fd}>&-
(real_migrate_lock)
printf 'PASS bundle-lock-busy-and-inherited-descriptor\n'

# Exercise activation's real command boundaries with private fixture paths.
(
  migrate_activate() { real_migrate_activate; }
  mkdir -p "$work/config/acquis.d" "$lib/crowdsec/acquis.d"
  cp "$root/deploy/vn/edge/crowdsec/config.yaml.local" "$work/config/config.yaml.postgresql"
  printf 'reviewed-acquisition\n' >"$lib/crowdsec/acquis.d/fixture.yaml"
  cat >"$lib/crowdsec-offline.sh" <<'SH'
crowdsec_remove_online() { [[ ${MIGRATE_FAIL:-} != offline ]]; }
crowdsec_assert_postgresql() { [[ ${MIGRATE_FAIL:-} != config-proof ]]; }
crowdsec_configure_local_detection() { [[ ${MIGRATE_FAIL:-} != detection ]]; }
SH
  install() {
    if [[ ${MIGRATE_FAIL:-} == config && ${!#} == "$work/config/config.yaml.local" ]] ||
      [[ ${MIGRATE_FAIL:-} == files && ${!#} == "$work/config/acquis.d/fixture.yaml" ]]; then
      printf 'discarded-activation-diagnostic\n' >&2
      return 1
    fi
    /usr/bin/install "$@"
  }
  for fault in config:activate-config offline:activate-offline gate:activate-gate \
    config-proof:activate-config-proof detection:activate-detection files:activate-files; do
    IFS=: read -r failure step <<<"$fault"
    : >"$MIGRATE_CALLS"
    MIGRATE_FAIL=$failure refused "$step" --reset
    [[ $(tail -n1 "$MIGRATE_CALLS") == stop-joined ]]
  done
  real_migrate_activate
  cmp "$work/config/config.yaml.postgresql" "$work/config/config.yaml.local"
  cmp "$lib/crowdsec/acquis.d/fixture.yaml" "$work/config/acquis.d/fixture.yaml"
)
printf 'PASS activation-boundaries-hide-command-output\n'

# Gate each schema-capable bouncer command even when the prior list passed.
cat >"$lib/crowdsec-db.sh" <<'SH'
#!/usr/bin/env bash
count=0
[[ ! -e $MIGRATE_CALLS.count ]] || read -r count <"$MIGRATE_CALLS.count"
count=$((count+1))
printf '%s\n' "$count" >"$MIGRATE_CALLS.count"
((count<2))
SH
cscli() {
  if [[ $* == 'bouncers list -o json' ]]; then
    printf '%s\n' "$listed_registrations"
  else printf 'vendor-write\n' >>"$MIGRATE_CALLS"; return 1; fi
}
mktemp() { printf '%s\n' "$work/bootstrap-key"; }
for listed_registrations in '[{"name":"aboutme-firewall"}]' '[]'; do
  : >"$MIGRATE_CALLS"; rm -f "$MIGRATE_CALLS.count"
  if real_firewall_register; then exit 1; fi
  [[ ! -s $MIGRATE_CALLS && $(cat "$MIGRATE_CALLS.count") == 2 ]]
done
printf 'PASS firewall-mutations-recheck-admission\n'
