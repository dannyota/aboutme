#!/usr/bin/env bash
# Fake commands prove migration order without touching host state.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
# shellcheck source=../host/crowdsec-migrate.sh
source "$root/deploy/vn/host/crowdsec-migrate.sh"
lib=$work/lib
mkdir -p "$lib"
export MIGRATE_CALLS=$work/calls
cat >"$lib/crowdsec-db.sh" <<'SH'
#!/usr/bin/env bash
printf 'gate\n' >>"$MIGRATE_CALLS"
SH
cat >"$lib/crowdsec-provision.sh" <<'SH'
#!/usr/bin/env bash
printf 'provision %s\n' "$1" >>"$MIGRATE_CALLS"
[[ ${MIGRATE_FAIL:-} != yes ]]
SH
cat >"$lib/secrets-host.sh" <<'SH'
#!/usr/bin/env bash
printf 'secret %s\n' "$*" >>"$MIGRATE_CALLS"
SH
/usr/bin/chmod +x "$lib/"*
chmod() { :; }
systemctl() {
  if [[ $1 == is-active ]]; then [[ $3 == aboutme-caddy.service ]]; return; fi
  printf 'unit %s\n' "$*" >>"$MIGRATE_CALLS"
}
cscli() { printf 'cli %s\n' "$*" >>"$MIGRATE_CALLS"; }
migrate_postgresql_selected() { [[ ${selected:-no} == yes ]]; }
migrate_activate() { printf 'activate\n' >>"$MIGRATE_CALLS"; }
migrate_remove_sqlite() { printf 'discard\n' >>"$MIGRATE_CALLS"; }
migrate_firewall_register() { printf 'firewall-register\n' >>"$MIGRATE_CALLS"; }
migrate_firewall_ready() { printf 'firewall-ready\n' >>"$MIGRATE_CALLS"; }
selected=yes
: >"$MIGRATE_CALLS"
migrate_main
[[ $(cat "$MIGRATE_CALLS") == gate ]]
printf 'PASS already-postgresql\n'
selected=no
: >"$MIGRATE_CALLS"
migrate_main
cat >"$work/expected" <<'CALLS'
unit stop aboutme-caddy.service aboutme-maintenance.service crowdsec-firewall-bouncer.service crowdsec.service aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer aboutme-crowdsec-retention.service aboutme-crowdsec-vacuum.service aboutme-crowdsec-retention-startup.service
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
CALLS
cmp "$work/expected" "$MIGRATE_CALLS"
printf 'PASS switch-order\n'
: >"$MIGRATE_CALLS"
export MIGRATE_FAIL=yes
if (migrate_run) >"$work/out" 2>&1; then exit 1; fi
unset MIGRATE_FAIL
# shellcheck disable=SC2154 # The sourced migration script defines the stop set.
[[ $(tail -n1 "$MIGRATE_CALLS") == "unit stop ${migrate_consumers[*]}" ]]
! grep -q '^unit start' "$MIGRATE_CALLS"
grep -qxF crowdsec_migrate_refused "$work/out"
printf 'PASS provisioning-failure-stops-consumers\n'
: >"$MIGRATE_CALLS"
migrate_main --reset
sed 's/provision fresh/provision restore-reset/' "$work/expected" >"$work/reset-expected"
cmp "$work/reset-expected" "$MIGRATE_CALLS"
printf 'PASS reset-recreates-and-registers\n'
