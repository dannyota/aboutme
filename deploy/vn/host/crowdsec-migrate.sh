#!/usr/bin/env bash
# One host migration step. See docs/runbooks/crowdsec-postgresql.md.
# shellcheck source-path=SCRIPTDIR

lib=/usr/local/lib/aboutme
migrate_bundle_path=/srv/data/host-operations/.bundle.lock
migrate_consumers=(aboutme-caddy.service aboutme-maintenance.service
  crowdsec-firewall-bouncer.service crowdsec.service
  aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer
  aboutme-crowdsec-retention.service aboutme-crowdsec-vacuum.service
  aboutme-crowdsec-retention-startup.service aboutme-watch.timer aboutme-watch.service)

migrate_at() {
  migrate_step=$1
  shift
  "$@"
}
migrate_lock() {
  local path=$migrate_bundle_path parent
  parent=${path%/*}
  [[ -d $parent && ! -L $parent &&
    $(stat -c '%u:%a' "$parent") == 0:700 &&
    -f $path && ! -L $path && $(stat -c '%u:%a' "$path") == 0:600 ]] || return 1
  if [[ -n ${ABOUTME_MIGRATE_BUNDLE_FD:-} ]]; then
    [[ $ABOUTME_MIGRATE_BUNDLE_FD =~ ^[0-9]+$ &&
      /proc/$BASHPID/fd/$ABOUTME_MIGRATE_BUNDLE_FD -ef $path ]] || return 1
    migrate_bundle_fd=$ABOUTME_MIGRATE_BUNDLE_FD
    grep -qE '^lock:.*FLOCK[[:space:]]+ADVISORY[[:space:]]+WRITE' "/proc/$BASHPID/fdinfo/$migrate_bundle_fd" || return 1
  else
    exec {migrate_bundle_fd}>>"$path" || return 1
    flock -x -n "$migrate_bundle_fd" || return 1
  fi
}
migrate_idle() {
  local units unit state record operation
  [[ ! -e /srv/data/host-operations/key-change.json && ! -L /srv/data/host-operations/key-change.json ]] || return 1
  record=$("$lib/fence.sh" read) || return 1
  jq -e '(.operation_id // null)==null' <<<"$record" >/dev/null || return 1
  units=$(systemctl list-units --all --plain --no-legend 'aboutme-operation@*.service') || return 1
  while read -r unit _ state _; do
    [[ -n $unit ]] || continue
    [[ $unit == aboutme-operation@*.service && ( $state == inactive || $state == failed ) ]] || return 1
  done <<<"$units"
  # Match the installer's unresolved-operation admission under the bundle lock.
  # shellcheck source=operation-state.sh
  source "$lib/operation-state.sh"
  for operation in /srv/data/host-operations/*; do
    [[ -e $operation ]] || continue
    [[ -d $operation && ! -L $operation && -f $operation/state.json && ! -L $operation/state.json ]] || return 1
    record=$(operation_state load "${operation##*/}") || return 1
    jq -e '.closureQuiescent==true and .fenceHeld==false and (.action.writeOutcome!="unknown") and (.phase=="closed" or .phase=="planned" or .phase=="serving")' <<<"$record" >/dev/null || return 1
  done
}
migrate_notice() { "$lib/fence.sh" check-current crowdsec.service; }
migrate_registered() {
  [[ -f /etc/crowdsec/local_api_credentials.yaml && ! -L /etc/crowdsec/local_api_credentials.yaml &&
    $(stat -c '%u:%g:%a' /etc/crowdsec/local_api_credentials.yaml) == 0:0:600 ]] || return 1
  local registrations machine
  "$lib/crowdsec-db.sh" preflight-ready </dev/null || return 1
  machine=$(python3 -I - <<'PY' 2>/dev/null
import pathlib,yaml
credentials=yaml.safe_load(pathlib.Path('/etc/crowdsec/local_api_credentials.yaml').read_text())
assert isinstance(credentials,dict)
login=credentials.get('login')
assert isinstance(login,str) and login and not any(c.isspace() for c in login)
print(login,end='')
PY
  ) || return 1
  "$lib/crowdsec-db.sh" preflight-ready </dev/null || return 1
  registrations=$(cscli machines list -o json) || return 1
  jq -e --arg machine "$machine" 'any(.[]; .machineId==$machine and .isValidated==true)' <<<"$registrations" >/dev/null || return 1
  "$lib/crowdsec-db.sh" preflight-ready </dev/null || return 1
  registrations=$(cscli bouncers list -o json) || return 1
  jq -e 'any(.[]; .name=="aboutme-caddy") and any(.[]; .name=="aboutme-firewall")' <<<"$registrations" >/dev/null || return 1
  "$lib/crowdsec-retention-lease-check.sh" &&
    systemctl is-enabled --quiet crowdsec.service &&
    systemctl is-enabled --quiet crowdsec-firewall-bouncer.service
}
migrate_stop() {
  # shellcheck source=crowdsec-retention-fail.sh
  source "$lib/crowdsec-retention-fail.sh"
  local systemctl_cmd=/usr/bin/systemctl
  local -a protected_units=("${migrate_consumers[@]}")
  retention_stop
}
migrate_postgresql_selected() {
  python3 -I - <<'PY' >/dev/null 2>&1
import pathlib,yaml
config={}
for name in ('config.yaml','config.yaml.local'):
    path=pathlib.Path('/etc/crowdsec')/name
    if path.exists():
        config.update((yaml.safe_load(path.read_text()) or {}).get('db_config',{}))
expected=yaml.safe_load(pathlib.Path('/etc/crowdsec/config.yaml.postgresql').read_text())['db_config']
assert config==expected and config['type']=='pgx'
PY
}
migrate_activate() {
  migrate_at activate-config install -o root -g root -m 0644 \
    /etc/crowdsec/config.yaml.postgresql /etc/crowdsec/config.yaml.local || return 1
  # Remove inherited database keys so the installed overlay selects only pgx.
  # shellcheck source=crowdsec-offline.sh
  migrate_at activate-offline source "$lib/crowdsec-offline.sh" || return 1
  crowdsec_remove_online /etc/crowdsec/config.yaml /etc/crowdsec/config.yaml.local \
    /etc/crowdsec/online_api_credentials.yaml || return 1
  migrate_at activate-gate "$lib/crowdsec-db.sh" preflight-ready </dev/null || return 1
  migrate_at activate-config-proof crowdsec_assert_postgresql || return 1
  migrate_at activate-detection crowdsec_configure_local_detection || return 1
  local group file
  for group in acquis.d parsers/s01-parse parsers/s02-enrich scenarios; do
    for file in "$lib/crowdsec/$group/"*.yaml; do
      [[ -f $file ]] || continue
      migrate_at activate-files install -m 0644 "$file" "/etc/crowdsec/$group/${file##*/}" || return 1
    done
  done
}
migrate_remove_sqlite() {
  rm -f -- /var/lib/crowdsec/data/crowdsec.db \
    /var/lib/crowdsec/data/crowdsec.db-shm /var/lib/crowdsec/data/crowdsec.db-wal
}
migrate_firewall_register() {
  local destination=/srv/data/crowdsec/firewall-bouncer.key temp registrations
  "$lib/crowdsec-db.sh" preflight-ready </dev/null || return 1
  registrations=$(cscli bouncers list -o json) || return 1
  if jq -e 'any(.[]; .name=="aboutme-firewall")' <<<"$registrations" >/dev/null; then
    "$lib/crowdsec-db.sh" preflight-ready </dev/null || return 1
    cscli bouncers delete aboutme-firewall || return 1
  fi
  temp=$(mktemp "${destination}.XXXXXX") || return 1
  if ! "$lib/crowdsec-db.sh" preflight-ready </dev/null; then rm -f -- "$temp"; return 1; fi
  if ! cscli bouncers add aboutme-firewall -o raw >"$temp"; then rm -f -- "$temp"; return 1; fi
  [[ -s $temp ]] || { rm -f -- "$temp"; return 1; }
  chmod 0600 "$temp" && mv -fT -- "$temp" "$destination" || return 1
  local conf=/etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local
  # Preserve the reviewed bouncer settings and replace only its private key.
  sed '/^api_key:/d' "$lib/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local" >"${conf}.new" || return 1
  printf 'api_key: %s\n' "$(cat "$destination")" >>"${conf}.new" || return 1
  chmod 0600 "${conf}.new" && mv -fT -- "${conf}.new" "$conf"
}
migrate_firewall_ready() {
  (
    # shellcheck source=crowdsec-retention.sh
    source "$lib/crowdsec-retention.sh"
    retention_work=$(mktemp -d /run/aboutme/crowdsec-rebuild.XXXXXX) || exit 1
    trap 'rm -rf -- "$retention_work"' EXIT
    # shellcheck disable=SC2034 # The sourced SQL helper consumes this timestamp.
    retention_time=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)
    "$lib/crowdsec-db.sh" preflight-ready </dev/null 3>"$retention_work/epoch" || exit 1
    retention_firewall_rebuilt
  )
}
migrate_fresh_admission() (
  # The provisioner alone initializes schema; this check only selects recovery.
  # shellcheck source=crowdsec-db.sh
  source "$lib/crowdsec-db.sh"
  unset crowdsec_db_lock_fd
  crowdsec_db_owned_lock && crowdsec_db_config && crowdsec_db_auth || exit 1
  local exists
  exists=$({ crowdsec_db_begin; printf '%s\n' "SELECT EXISTS (SELECT FROM pg_database WHERE datname='crowdsec');" 'COMMIT;'; } | crowdsec_db_admin postgres) || exit 1
  [[ $exists == f ]] && exit 0
  [[ $exists == t ]] || exit 1
  local crowdsec_db_provisioning=yes
  crowdsec_db_empty
)
migrate_main() {
  [[ $EUID == 0 && ( $# == 0 || ( $# == 1 && $1 == --reset ) ) ]] || return 1
  local mode=fresh unit
  local -a edge=()
  if (($#)); then mode=restore-reset
  elif migrate_postgresql_selected && "$lib/crowdsec-db.sh" preflight-ready </dev/null; then
    if migrate_registered; then return 0; fi
  fi
  for unit in aboutme-caddy.service aboutme-maintenance.service; do
    if systemctl is-active --quiet "$unit"; then edge+=("$unit"); fi
  done
  if ((${#edge[@]}==0)); then migrate_no_edge=yes; fi
  migrate_changed=yes
  migrate_at stop migrate_stop || return 1
  if [[ $mode == fresh ]]; then migrate_at fresh-admission migrate_fresh_admission || return 1; fi
  migrate_at provision "$lib/crowdsec-provision.sh" "$mode" </dev/null || return 1
  migrate_at activate migrate_activate && migrate_at discard-sqlite migrate_remove_sqlite || return 1
  migrate_at machine "$lib/crowdsec-db.sh" preflight-ready </dev/null || return 1
  cscli machines add -a --force --error || return 1
  chmod 0600 /etc/crowdsec/local_api_credentials.yaml || return 1
  # Only admitted fresh provisioning or explicit reset can reopen failed startup.
  # Both have stopped consumers and passed the new catalog and activation gates.
  migrate_at startup-admission rm -f -- /run/aboutme/crowdsec-retention-failed || return 1
  # Startup retention clears owned cache/firewall state and binds the new lease.
  migrate_at startup-retention systemctl start aboutme-crowdsec-retention-startup.service || return 1
  migrate_at maintenance-timers systemctl enable --now aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer || return 1
  migrate_at unmask systemctl unmask --runtime crowdsec.service crowdsec-firewall-bouncer.service || return 1
  migrate_at enable systemctl enable crowdsec.service crowdsec-firewall-bouncer.service || return 1
  migrate_at engine systemctl start crowdsec.service || return 1
  migrate_at caddy-register "$lib/crowdsec-db.sh" preflight-ready </dev/null || return 1
  "$lib/secrets-host.sh" bouncer-register || return 1
  migrate_at firewall-register migrate_firewall_register || return 1
  migrate_at firewall-start "$lib/crowdsec-db.sh" preflight-ready </dev/null || return 1
  systemctl start crowdsec-firewall-bouncer.service || return 1
  migrate_at firewall-ready migrate_firewall_ready || return 1
  if ((${#edge[@]})); then migrate_at edge-start systemctl start "${edge[@]}" || return 1; fi
  migrate_at lapi cscli lapi status || return 1
  migrate_at final-gate "$lib/crowdsec-db.sh" preflight-ready </dev/null &&
    migrate_at lease "$lib/crowdsec-retention-lease-check.sh" &&
    migrate_at watch systemctl enable --now aboutme-watch.timer
}
migrate_failure() {
  if [[ ${migrate_changed:-no} == yes ]]; then
    rm -f -- /run/aboutme/crowdsec-retention-ok
    printf 'retention-failed\n' >/run/aboutme/crowdsec-retention-failed
    if ! migrate_stop >/dev/null 2>&1; then
      printf 'crowdsec_migrate_stop_failed\n' >&2
      return 1
    fi
  fi
  printf 'crowdsec_migrate_refused step=%s\n' "$migrate_step" >&2
  return 1
}
migrate_run() {
  local migrate_changed=no migrate_no_edge=no migrate_error_fd migrate_step=arguments
  [[ $EUID == 0 && ( $# == 0 || ( $# == 1 && $1 == --reset ) ) ]] || {
    migrate_failure; return 1;
  }
  if ! { migrate_at lock migrate_lock && migrate_at idle migrate_idle && migrate_at notice migrate_notice; } >/dev/null 2>&1; then
    migrate_failure; return 1
  fi
  # Signal traps also run inside migrate_main's discarded diagnostics.
  exec {migrate_error_fd}>&2
  trap 'migrate_failure 2>&"$migrate_error_fd"; exit 1' HUP INT TERM
  if ! migrate_main "$@" >/dev/null 2>&1; then
    trap - HUP INT TERM
    exec {migrate_error_fd}>&-
    migrate_failure
    return 1
  fi
  trap - HUP INT TERM
  exec {migrate_error_fd}>&-
  if [[ $migrate_no_edge == yes ]]; then printf 'crowdsec_migrate_no_edge\n' >&2; fi
}
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  set -euo pipefail
  umask 077
  migrate_run "$@"
fi
