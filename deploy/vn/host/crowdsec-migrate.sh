#!/usr/bin/env bash
# One host migration step. See docs/runbooks/crowdsec-postgresql.md.
# shellcheck source-path=SCRIPTDIR

lib=/usr/local/lib/aboutme
migrate_consumers=(aboutme-caddy.service aboutme-maintenance.service
  crowdsec-firewall-bouncer.service crowdsec.service
  aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer
  aboutme-crowdsec-retention.service aboutme-crowdsec-vacuum.service
  aboutme-crowdsec-retention-startup.service)

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
  install -o root -g root -m 0644 /etc/crowdsec/config.yaml.postgresql /etc/crowdsec/config.yaml.local || return 1
  # Remove inherited database keys so the installed overlay selects only pgx.
  # shellcheck source=crowdsec-offline.sh
  source "$lib/crowdsec-offline.sh"
  crowdsec_remove_online /etc/crowdsec/config.yaml /etc/crowdsec/config.yaml.local \
    /etc/crowdsec/online_api_credentials.yaml || return 1
  "$lib/crowdsec-db.sh" preflight-ready </dev/null || return 1
  crowdsec_assert_postgresql && crowdsec_configure_local_detection || return 1
  local group file
  for group in acquis.d parsers/s01-parse parsers/s02-enrich scenarios; do
    for file in "$lib/crowdsec/$group/"*.yaml; do
      [[ -f $file ]] || continue
      install -m 0644 "$file" "/etc/crowdsec/$group/${file##*/}" || return 1
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
    cscli bouncers delete aboutme-firewall || return 1
  fi
  temp=$(mktemp "${destination}.XXXXXX") || return 1
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
migrate_main() {
  [[ $EUID == 0 && ( $# == 0 || ( $# == 1 && $1 == --reset ) ) ]] || return 1
  local mode=fresh unit
  local -a edge=()
  if (($#)); then mode=restore-reset
  elif migrate_postgresql_selected && "$lib/crowdsec-db.sh" preflight-ready </dev/null; then return 0; fi
  for unit in aboutme-caddy.service aboutme-maintenance.service; do
    if systemctl is-active --quiet "$unit"; then edge+=("$unit"); fi
  done
  systemctl stop "${migrate_consumers[@]}" || return 1
  "$lib/crowdsec-provision.sh" "$mode" </dev/null || return 1
  migrate_activate && migrate_remove_sqlite || return 1
  "$lib/crowdsec-db.sh" preflight-ready </dev/null || return 1
  cscli machines add -a --force --error || return 1
  chmod 0600 /etc/crowdsec/local_api_credentials.yaml || return 1
  # Startup retention clears owned cache/firewall state and binds the new lease.
  systemctl start aboutme-crowdsec-retention-startup.service || return 1
  systemctl enable --now aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer || return 1
  systemctl unmask --runtime crowdsec.service crowdsec-firewall-bouncer.service || return 1
  systemctl enable crowdsec.service crowdsec-firewall-bouncer.service || return 1
  systemctl start crowdsec.service || return 1
  "$lib/crowdsec-db.sh" preflight-ready </dev/null || return 1
  "$lib/secrets-host.sh" bouncer-register || return 1
  migrate_firewall_register || return 1
  "$lib/crowdsec-db.sh" preflight-ready </dev/null || return 1
  systemctl start crowdsec-firewall-bouncer.service || return 1
  migrate_firewall_ready || return 1
  if ((${#edge[@]})); then systemctl start "${edge[@]}" || return 1; fi
  cscli lapi status || return 1
  "$lib/crowdsec-db.sh" preflight-ready </dev/null
}
migrate_run() {
  [[ $EUID == 0 && ( $# == 0 || ( $# == 1 && $1 == --reset ) ) ]] || {
    printf 'crowdsec_migrate_refused\n' >&2; return 1;
  }
  if ! migrate_main "$@" >/dev/null 2>&1; then
    systemctl stop "${migrate_consumers[@]}" >/dev/null 2>&1 || true
    printf 'crowdsec_migrate_refused\n' >&2
    return 1
  fi
}
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  set -euo pipefail
  umask 077
  [[ $EUID == 0 ]] || { printf 'crowdsec_migrate_refused\n' >&2; exit 1; }
  trap 'systemctl stop "${migrate_consumers[@]}" >/dev/null 2>&1; printf "crowdsec_migrate_refused\n" >&2; exit 1' HUP INT TERM
  migrate_run "$@"
fi
