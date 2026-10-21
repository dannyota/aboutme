#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
# Explicit fresh-state boundary for the separate CrowdSec database.
set -euo pipefail
umask 077

provision() {
  [[ $# == 1 && ( $1 == fresh || $1 == restore-reset ) ]] || return 1
  [[ -f /usr/local/lib/aboutme/crowdsec-db.sh && ! -L /usr/local/lib/aboutme/crowdsec-db.sh && \
    $(stat -c '%u:%g:%a:%h' /usr/local/lib/aboutme/crowdsec-db.sh) == 0:0:755:1 ]] || return 1
  # shellcheck source=deploy/vn/host/crowdsec-db.sh
  source /usr/local/lib/aboutme/crowdsec-db.sh
  crowdsec_db_owned_lock && crowdsec_db_config && crowdsec_db_auth || return 1
  local inventory auth
  auth=$(printf '{"operation_id":"%s","release_tag":"%s"}' "$crowdsec_db_owner" "$crowdsec_db_tag")
  inventory=$({ crowdsec_db_begin; printf '%s\n' "
SELECT EXISTS (SELECT FROM pg_roles WHERE rolname = 'crowdsec') || '|' || EXISTS (SELECT FROM pg_database WHERE datname = 'crowdsec');
COMMIT;"; } | crowdsec_db_admin postgres) || return 1
  case $inventory in
    false\|false)
      crowdsec_db_admin postgres >/dev/null <<'SQL' || return 1
SET statement_timeout = '10s';
SET lock_timeout = '2s';
CREATE ROLE crowdsec LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 8;
SQL
      ;;
    true\|true | true\|false) crowdsec_db_role || return 1 ;;
    *) return 1 ;;
  esac
  # Protected settings precede the first connection by the CrowdSec role.
  crowdsec_db_admin postgres >/dev/null <<'SQL' || return 1
SET statement_timeout = '10s';
SET lock_timeout = '2s';
ALTER ROLE crowdsec SET log_min_messages = 'panic';
ALTER ROLE crowdsec SET log_min_error_statement = 'panic';
ALTER ROLE crowdsec SET log_error_verbosity = 'terse';
ALTER ROLE crowdsec SET log_statement = 'none';
ALTER ROLE crowdsec SET log_min_duration_statement = -1;
ALTER ROLE crowdsec SET log_min_duration_sample = -1;
ALTER ROLE crowdsec SET log_statement_sample_rate = 0;
ALTER ROLE crowdsec SET log_transaction_sample_rate = 0;
ALTER ROLE crowdsec SET log_parameter_max_length = 0;
ALTER ROLE crowdsec SET log_parameter_max_length_on_error = 0;
SQL
  crowdsec_db_role && crowdsec_db_authority && crowdsec_db_stopped || return 1
  if [[ $inventory == true\|true ]]; then
    local safe
    safe=$({ crowdsec_db_begin; printf '%s\n' "
SELECT (SELECT datdba = 'crowdsec'::regrole AND datacl::text = '{crowdsec=CTc/crowdsec}' FROM pg_database WHERE datname = 'crowdsec')
AND NOT EXISTS (SELECT FROM pg_stat_activity WHERE datname = 'crowdsec');
COMMIT;"; } | crowdsec_db_admin postgres) || return 1
    [[ $safe == t ]] || return 1
    if [[ $1 == restore-reset ]]; then
      # No FORCE option: an unaccounted consumer blocks replacement.
      crowdsec_db_admin postgres >/dev/null <<'SQL' || return 1
SET statement_timeout = '10s';
SET lock_timeout = '2s';
DROP DATABASE crowdsec;
SQL
    fi
  fi
  if [[ $inventory != true\|true || $1 == restore-reset ]]; then
    crowdsec_db_authority && crowdsec_db_stopped || return 1
    crowdsec_db_admin postgres >/dev/null <<'SQL' || return 1
SET statement_timeout = '10s';
SET lock_timeout = '2s';
CREATE DATABASE crowdsec OWNER crowdsec TEMPLATE template0;
REVOKE ALL ON DATABASE crowdsec FROM PUBLIC;
SQL
    crowdsec_db_admin >/dev/null <<'SQL' || return 1
BEGIN;
SET LOCAL statement_timeout = '10s';
SET LOCAL lock_timeout = '2s';
ALTER SCHEMA public OWNER TO crowdsec;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
COMMIT;
SQL
  fi
  if [[ ${crowdsec_db_restore:-} == yes ]]; then
    crowdsec_restore_dependencies || return 1
  else [[ $(dpkg-query -W -f='${Version}' crowdsec 2>/dev/null) == 1.8.1 ]] || return 1; fi
  [[ -d /run/aboutme/crowdsec && ! -L /run/aboutme/crowdsec && \
    $(stat -c '%u:%g:%a' /run/aboutme/crowdsec) == 0:0:700 && \
    $(findmnt -n -o FSTYPE -T /run/aboutme/crowdsec) == tmpfs ]] || return 1
  local crowdsec_db_provisioning=yes
  # The functions run in this process under the existing canonical flock.
  # No lock-taking helper is launched from the protected section.
  crowdsec_db_main preflight-empty <<<"$auth" || return 1
  crowdsec_db_authority && crowdsec_db_stopped || return 1
  timeout 30 /usr/bin/cscli -c /etc/crowdsec/config.yaml.postgresql bouncers list -o json \
    >/dev/null 2>&1 || return 1
  crowdsec_db_main apply-policy <<<"$auth" || return 1
  crowdsec_db_ready || return 1
  crowdsec_db_authority && crowdsec_db_stopped || return 1
  timeout 30 /usr/bin/crowdsec -c /etc/crowdsec/config.yaml.postgresql -no-cs -t \
    >/dev/null 2>&1 || return 1
  crowdsec_db_ready && crowdsec_db_authority && crowdsec_db_stopped || return 1
  local empty
  empty=$(crowdsec_db_psql <<'SQL'
BEGIN READ ONLY;
SELECT NOT EXISTS (SELECT FROM alerts) AND NOT EXISTS (SELECT FROM decisions)
AND NOT EXISTS (SELECT FROM events) AND NOT EXISTS (SELECT FROM meta)
AND NOT EXISTS (SELECT FROM machines) AND NOT EXISTS (SELECT FROM bouncers);
COMMIT;
SQL
  ) || return 1
  [[ $empty == t ]]
}

crowdsec_restore_dependencies() {
  # The source inventory is generated from the authenticated pinned archive.
  crowdsec_db_safe /usr/local/lib/aboutme/crowdsec-restore-binaries.sha256 644 && crowdsec_db_config || return 1
  local file
  for file in profiles.yaml console.yaml; do crowdsec_db_safe "/etc/crowdsec/$file" 644 || return 1; done
  (cd / && sha256sum --check --status /usr/local/lib/aboutme/crowdsec-restore-binaries.sha256)
}
crowdsec_restore_reset() {
  (($#==0)) || return 1
  # Only the validated workload-locked child sources this fixed composition.
  # No argument, environment selector, operation switch, or public entry exists.
  # shellcheck source=crowdsec-db.sh
  source /usr/local/lib/aboutme/crowdsec-db.sh
  local crowdsec_db_restore=yes crowdsec_db_owner=$notice_operation crowdsec_db_tag=$notice_admission
  local crowdsec_db_lock_fd crowdsec_db_mount_identity
  crowdsec_db_root && crowdsec_restore_dependencies && notice_restore_denial && crowdsec_db_stopped || return 1
  local status=0
  provision restore-reset </dev/null || status=$?
  if [[ -n ${crowdsec_db_lock_fd:-} ]]; then exec {crowdsec_db_lock_fd}>&-; fi
  ((status==0)) || return 1
  # Restored registrations are never retained even on an app-absent drill.
  rm -f -- /etc/crowdsec/local_api_credentials.yaml /srv/data/crowdsec/firewall-bouncer.key \
    /etc/aboutme/secrets/crowdsec-bouncer-key || return 1
}
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  unset crowdsec_db_restore crowdsec_db_lock_fd
  if ! provision "$@" 2>/dev/null; then
    printf 'crowdsec_provision_refused\n' >&2
    exit 1
  fi
fi
