#!/usr/bin/env bash
# Fake commands prove migration order without touching host state.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
# shellcheck source=../host/crowdsec-migrate.sh
source "$root/deploy/vn/host/crowdsec-migrate.sh"
# Keep the real lock implementation for descriptor and contention proofs.
eval "$(declare -f migrate_lock | sed '1s/migrate_lock/real_migrate_lock/')"
eval "$(declare -f migrate_firewall_register | sed '1s/migrate_firewall_register/real_firewall_register/')"
lib=$work/lib
mkdir -p "$lib"
export MIGRATE_CALLS=$work/calls
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
[[ ${MIGRATE_FAIL:-} != lease ]]
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
cscli() { [[ ${MIGRATE_FAIL:-} != machine || $1 != machines ]] || return 1; [[ ${MIGRATE_FAIL:-} != lapi || $1 != lapi ]] || return 1; printf 'cli %s\n' "$*" >>"$MIGRATE_CALLS"; }
migrate_postgresql_selected() { [[ ${selected:-no} == yes ]]; }
migrate_activate() { if [[ ${MIGRATE_FAIL:-} == signal ]]; then kill -TERM "$BASHPID"; return 1; fi; [[ ${MIGRATE_FAIL:-} != activate ]] || return 1; printf 'activate\n' >>"$MIGRATE_CALLS"; }
migrate_remove_sqlite() { [[ ${MIGRATE_FAIL:-} != discard ]] || return 1; printf 'discard\n' >>"$MIGRATE_CALLS"; }
migrate_firewall_register() { [[ ${MIGRATE_FAIL:-} != firewall ]] || return 1; printf 'firewall-register\n' >>"$MIGRATE_CALLS"; }
migrate_firewall_ready() { [[ ${MIGRATE_FAIL:-} != rebuild ]] || return 1; printf 'firewall-ready\n' >>"$MIGRATE_CALLS"; }
# Admission doubles keep the fixture away from host paths.
migrate_lock() { [[ ${lock_busy:-no} == no ]]; }
migrate_idle() { [[ ${owned:-no} == no ]]; }
migrate_notice() { [[ ${notice:-yes} == yes ]]; }
migrate_fresh_admission() { [[ ${selected:-no} != yes ]]; }
migrate_registered() { [[ ${registered:-yes} == yes ]]; }
migrate_stop() { printf 'stop-joined\n' >>"$MIGRATE_CALLS"; [[ ${stop_failed:-no} == no ]]; }
refused() {
  if (migrate_run "$@") >"$work/out" 2>"$work/err"; then exit 1; fi
  [[ ! -s $work/out ]]
}
for admission in lock_busy owned; do
  : >"$MIGRATE_CALLS"
  printf -v "$admission" yes
  refused --reset
  [[ ! -s $MIGRATE_CALLS ]]
  printf -v "$admission" no
done
notice=no; refused --reset; [[ ! -s $MIGRATE_CALLS ]]; notice=yes
printf 'PASS lock-owner-and-notice-before-stop\n'
selected=yes
: >"$MIGRATE_CALLS"
migrate_run
[[ $(cat "$MIGRATE_CALLS") == gate ]]
active_edge=none
: >"$MIGRATE_CALLS"
migrate_run
[[ $(cat "$MIGRATE_CALLS") == gate ]]
unset active_edge
printf 'PASS complete-no-op-preserves-stopped-consumers\n'
registered=no
: >"$MIGRATE_CALLS"
refused
[[ $(tail -n1 "$MIGRATE_CALLS") == stop-joined ]]
! grep -q '^provision' "$MIGRATE_CALLS"
registered=yes
printf 'PASS missing-registration-refuses-occupied-catalog\n'
selected=no
: >"$MIGRATE_CALLS"
migrate_run
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
MIGRATE_FAIL=provision refused
: >"$MIGRATE_CALLS"
migrate_run
grep -qxF 'provision fresh' "$MIGRATE_CALLS"
printf 'PASS pre-schema-retry\n'
# Inject failures across provisioning, registration, startup, and final checks.
for failure in provision activate discard machine startup timers unmask enable engine caddy firewall bouncer rebuild lapi gate lease signal; do
  : >"$MIGRATE_CALLS"
  export MIGRATE_FAIL=$failure
  refused --reset
  [[ $(tail -n1 "$MIGRATE_CALLS") == stop-joined ]]
  grep -qxF crowdsec_migrate_refused "$work/err"
done
unset MIGRATE_FAIL
printf 'PASS failures-join-consumers\n'
active_edge=none
: >"$MIGRATE_CALLS"
migrate_run --reset 2>"$work/err"
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
