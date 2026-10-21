#!/usr/bin/env bash
# Hosted app-absent restore with authenticated standalone CrowdSec executables.
set -euo pipefail
umask 077
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
[[ ${1:-} == hosted && $EUID == 0 && ${GITHUB_ACTIONS:-} == true ]] || exit 2
[[ $(dpkg-query -W -f='${Status}' crowdsec 2>/dev/null || true) != 'install ok installed' ]]
[[ ! -e /etc/crowdsec/config.yaml && ! -e /etc/crowdsec/local_api_credentials.yaml ]]
[[ $(find /etc/crowdsec -type f -printf '%P\n' | LC_ALL=C sort) == $'config.yaml.postgresql\nconsole.yaml\nprofiles.yaml' ]]
[[ -z $(find /var/lib/crowdsec/data -mindepth 1 -print -quit) ]]
(cd / && sha256sum --check --status /usr/local/lib/aboutme/crowdsec-restore-binaries.sha256)
# shellcheck source=crowdsec-restore-integration-fixture.sh
source "$root/deploy/vn/tests/crowdsec-restore-integration-fixture.sh"
restore_fixture_start "$root"
trap restore_fixture_cleanup EXIT
stage=application
trap 'printf "FAIL standalone-restore %s\n" "$stage" >&2' ERR
restore_admin >/dev/null <<'SQL'
CREATE ROLE aboutme LOGIN;
CREATE ROLE aboutme_app LOGIN;
CREATE ROLE aboutme_migrator LOGIN;
CREATE DATABASE aboutme OWNER aboutme_migrator;
REVOKE ALL ON DATABASE aboutme FROM PUBLIC;
SQL
restore_admin aboutme >/dev/null <<'SQL'
REVOKE ALL ON SCHEMA public FROM PUBLIC;
CREATE TABLE public.goose_db_version (version_id bigint NOT NULL);
INSERT INTO public.goose_db_version VALUES (1);
CREATE TABLE public.resumes (id bigint PRIMARY KEY);
INSERT INTO public.resumes VALUES (1);
GRANT USAGE ON SCHEMA public TO aboutme_app;
GRANT SELECT ON public.resumes TO aboutme_app;
SQL
[[ $(restore_admin <<<"SELECT count(*) FROM pg_database WHERE datname='crowdsec';") == 0 ]]
runuser -u postgres -- /usr/bin/pgbackrest --stanza=aboutme stanza-create >/dev/null 2>&1
runuser -u postgres -- /usr/bin/pgbackrest --stanza=aboutme --type=full backup >/dev/null 2>&1
controller=/usr/local/lib/aboutme/restore-drill.sh
guest=${restore_guest:?initialized standalone fixture required}
pass() {
  local previous owner
  previous=$(cat /var/lib/aboutme/privacy-notice/epoch)
  rm -f -- "$guest/audit/restore-joined" "$guest/audit/restoring-owner"
  "$controller" host-pass "$@" >"$guest/reports" 2>"$guest/errors"
  [[ -f $guest/audit/restore-joined && -s $guest/audit/restoring-owner ]]
  owner=$(jq -r .operation_id "$guest/audit/restoring-owner")
  [[ $owner =~ ^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$ ]]
  [[ $(jq -r '.operation_id // empty' /var/lib/aboutme/fence.json) == '' ]]
  [[ $(cat /var/lib/aboutme/privacy-notice/epoch) != "$previous" ]]
  [[ -f /var/lib/aboutme/privacy-notice/restore-reconciliation &&
    ! -e /var/lib/aboutme/privacy-notice/activation.json && ! -e /var/lib/aboutme/privacy-notice/send.json ]]
  [[ ! -e /var/lib/aboutme/cutover-mutation.json && ! -e /var/lib/aboutme/release.json ]]
  [[ $(restore_admin crowdsec <<<'SELECT count(*) FROM bouncers;') == 0 ]]
  [[ $(restore_admin crowdsec <<<'SELECT count(*) FROM machines;') == 0 ]]
  [[ $(restore_admin crowdsec <<<'SELECT count(*) FROM alerts;') == 0 ]]
  [[ $(restore_admin crowdsec <<<"SELECT count(*) FROM pg_stats WHERE schemaname='public';") == 0 ]]
  /usr/bin/flock -n /run/aboutme/workload.lock true
  /usr/bin/flock -xn /var/lib/aboutme/fence.lock true
  if /usr/local/lib/aboutme/fence.sh check 8009 crowdsec >/dev/null 2>&1; then exit 1; fi
}
stage=pre-crowdsec-backup
pass latest
[[ $(restore_admin aboutme <<<'SELECT count(*) FROM resumes;') == 1 ]]
[[ -s $guest/reset-locks ]]
# Re-enable archival on this synthetic source cluster after the first restored
# target disabled it. No production configuration or backup repository is used.
printf "archive_mode = 'on'\n" >>"$guest/data/postgresql.auto.conf"
systemctl stop postgresql@18-main.service
systemctl start postgresql@18-main.service
# Seed a revoked identity and planner contamination into the backup source.
/usr/local/lib/aboutme/crowdsec-db.sh preflight-ready </dev/null >/dev/null 2>&1
/usr/bin/cscli -c /etc/crowdsec/config.yaml.postgresql bouncers add synthetic-restored -o raw >"$guest/old-key" 2>/dev/null
restore_admin crowdsec >/dev/null <<'SQL'
INSERT INTO public.alerts (id,created_at,updated_at,started_at,stopped_at,scenario,source_ip,source_scope,source_value,capacity,events_count,leak_speed,simulated)
VALUES (1,now(),now(),now(),now(),'aboutme/http-status','192.0.2.1','Ip','192.0.2.1',1,1,'1s',false);
ALTER TABLE public.alerts ALTER COLUMN source_ip SET STATISTICS 1;
ANALYZE public.alerts;
SQL
[[ $(restore_admin crowdsec <<<"SELECT count(*) FROM pg_stats WHERE schemaname='public';") -gt 0 ]]
[[ $(restore_admin crowdsec <<<'SELECT count(*) FROM bouncers;') == 1 ]]
runuser -u postgres -- /usr/bin/pgbackrest --stanza=aboutme --type=full backup >/dev/null 2>&1
sleep 1
target=$(date -u +%Y-%m-%dT%H:%M:%SZ)
sleep 1
restore_admin aboutme <<<'INSERT INTO resumes VALUES (2);' >/dev/null
restore_admin <<<'SELECT pg_switch_wal();' >/dev/null
runuser -u postgres -- /usr/bin/pgbackrest --stanza=aboutme check >/dev/null 2>&1
stage=restored-old-state
pass latest
[[ $(restore_admin aboutme <<<'SELECT count(*) FROM resumes;') == 2 ]]
stage=point-in-time
pass time "$target"
[[ $(restore_admin aboutme <<<'SELECT count(*) FROM resumes;') == 1 ]]
[[ $(wc -l <"$guest/reset-locks") == 3 ]]
[[ ! -e /etc/crowdsec/config.yaml && ! -e /etc/crowdsec/acquis.yaml ]]
stage=failed-dependency-retains-owner
rm -f -- "$guest/audit/restore-joined" "$guest/audit/restoring-owner"
mv /usr/local/lib/aboutme/crowdsec-restore-binaries.sha256 "$guest/provenance"
if "$controller" host-pass latest >"$guest/refused" 2>&1; then exit 1; fi
[[ ! -e $guest/audit/restoring-owner && ! -e $guest/audit/restore-joined ]]
jq -e '.operation_kind=="notice_restore" and (.operation_id|type=="string")' /var/lib/aboutme/fence.json >/dev/null
[[ $(restore_admin aboutme <<<'SELECT count(*) FROM resumes;') == 1 ]]
if "$controller" finish >"$guest/refused" 2>&1; then exit 1; fi
if "$controller" _host-pass >"$guest/refused" 2>&1; then exit 1; fi
[[ -f /var/lib/aboutme/privacy-notice/restore-reconciliation && ! -e /var/lib/aboutme/privacy-notice/activation.json ]]
echo 'PASS standalone-private-controller-latest-and-pitr-reset'
