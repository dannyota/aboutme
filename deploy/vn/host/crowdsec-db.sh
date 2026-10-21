#!/usr/bin/env bash
# Read-only admission and fenced policy for the pinned CrowdSec catalog.
# See docs/design/crowdsec-http-privacy.md, Database and authorization.

crowdsec_db_fail() { printf 'crowdsec_db_%s\n' "$1" >&2; return 1; }
crowdsec_db_safe() {
  [[ -f $1 && ! -L $1 && $(stat -c '%u:%g:%a:%h' "$1") == "0:0:$2:1" ]]
}
crowdsec_db_root() {
  ((EUID == 0)) || return 1
  local name
  for name in ${!PG@} FENCE_DIR FENCE_SAFETY_LIB DB_NAME PG_TARGET; do
    [[ ! -v $name ]] || return 1
  done
  crowdsec_db_safe /usr/local/lib/aboutme/crowdsec-db.sh 755 &&
    crowdsec_db_safe /usr/local/lib/aboutme/crowdsec-schema-v1.8.1.tsv 644
}
crowdsec_db_psql() {
  timeout 15 env -i PATH=/usr/bin:/bin PGPASSFILE=/dev/null PGSSLMODE=disable \
    /usr/lib/postgresql/18/bin/psql -X -qAt -w -v ON_ERROR_STOP=1 \
    -h /run/postgresql -U crowdsec -d crowdsec 2>/dev/null
}
crowdsec_db_admin() {
  case ${1:-crowdsec} in postgres | crowdsec) ;; *) return 1 ;; esac
  timeout 15 runuser -u postgres -- env -i PATH=/usr/bin:/bin PGPASSFILE=/dev/null PGSSLMODE=disable \
    /usr/lib/postgresql/18/bin/psql -X -qAt -w -v ON_ERROR_STOP=1 \
    -h /run/postgresql -U postgres -d "${1:-crowdsec}" 2>/dev/null
}
crowdsec_db_begin() {
  printf '%s\n' 'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;' \
    "SET LOCAL statement_timeout = '10s';" "SET LOCAL lock_timeout = '2s';"
}
crowdsec_db_authority_input() {
  local input
  input=$(timeout 5 head -c 1025) || return 1
  ((${#input} <= 1024)) || return 1
  input=$(printf '%s' "$input" | python3 -I -c '
import json,re,sys
def pairs(items):
    result={}
    for key,value in items:
        if key in result: raise ValueError()
        result[key]=value
    return result
try:
    data=json.load(sys.stdin,object_pairs_hook=pairs)
    assert set(data)=={"operation_id","release_tag"}
    assert re.fullmatch(r"[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]",data["operation_id"])
    assert re.fullmatch(r"v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})",data["release_tag"])
    print(data["operation_id"]+" "+data["release_tag"])
except Exception: sys.exit(1)
' 2>/dev/null) || return 1
  read -r crowdsec_db_owner crowdsec_db_tag <<<"$input"
}
crowdsec_db_mount() {
  # shellcheck source=crowdsec-privacy-notice-host.sh
  source /usr/local/lib/aboutme/crowdsec-privacy-notice-host.sh
  crowdsec_db_mount_identity=$(notice_mount_identity) || return 1
}
crowdsec_db_authority() {
  local candidate
  [[ -n ${crowdsec_db_lock_fd:-} && -n ${crowdsec_db_owner:-} ]] || return 1
  [[ $(notice_mount_identity) == "$crowdsec_db_mount_identity" ]] || return 1
  crowdsec_db_safe /var/lib/aboutme/fence.json 644 &&
    crowdsec_db_safe /var/lib/aboutme/cutover-mutation.json 600 || return 1
  [[ $(stat -Lc '%d:%i' "/proc/$$/fd/$crowdsec_db_lock_fd") == \
    "$(stat -c '%d:%i' /var/lib/aboutme/fence.lock)" ]] || return 1
  grep -qE '^lock:.*FLOCK[[:space:]]+ADVISORY[[:space:]]+WRITE' \
    "/proc/$$/fdinfo/$crowdsec_db_lock_fd" || return 1
  # The caller's checkpoint happened before taking this flock. Validate the
  # exact same authority again while holding it, without a recursive helper.
  [[ $crowdsec_db_tag =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]] || return 1
  candidate=$((10#${BASH_REMATCH[1]} * 1000000 + 10#${BASH_REMATCH[2]} * 1000 + 10#${BASH_REMATCH[3]}))
  jq -e --arg owner "$crowdsec_db_owner" --arg tag "$crowdsec_db_tag" --argjson candidate "$candidate" '
    .operation_id == $owner and .operation_kind == "cutover"
    and (.minimum_release | type == "number" and . >= 0 and . == floor)
    and .minimum_release <= $candidate
    and (.operation_checked_at | type == "string")
    and ((.operation_tag // $tag) == $tag)
  ' /var/lib/aboutme/fence.json >/dev/null 2>&1 &&
    jq -e --arg owner "$crowdsec_db_owner" '
      keys == ["mutation_kind","operation_id","started_at","state"]
      and .operation_id == $owner and .mutation_kind == "reset-db"
      and .state == "active" and (.started_at | type == "string")
    ' /var/lib/aboutme/cutover-mutation.json >/dev/null 2>&1
}
crowdsec_db_stopped() {
  local unit state
  for unit in crowdsec.service crowdsec-firewall-bouncer.service \
    aboutme-caddy.service aboutme-maintenance.service \
    aboutme-crowdsec-retention.timer aboutme-crowdsec-retention.service \
    aboutme-crowdsec-retention-startup.service; do
    state=$(systemctl show --all --property=ActiveState --property=Job "$unit" 2>/dev/null) || return 1
    [[ $(wc -l <<<"$state") == 2 ]] &&
      grep -qxE 'ActiveState=(inactive|failed)' <<<"$state" &&
      grep -qxF 'Job=' <<<"$state" || return 1
  done
}
crowdsec_db_owned_lock() {
  crowdsec_db_root && crowdsec_db_authority_input && crowdsec_db_mount || return 1
  # fence.sh owns checkpoint validation and durability. Do this before the
  # in-process lock and never call it from the protected section.
  /usr/local/lib/aboutme/fence.sh checkpoint "$crowdsec_db_owner" "$crowdsec_db_tag" \
    >/dev/null 2>&1 || return 1
  [[ ! -L /var/lib/aboutme/fence.lock ]] || return 1
  exec {crowdsec_db_lock_fd}>>/var/lib/aboutme/fence.lock || return 1
  flock -x -w 2 "$crowdsec_db_lock_fd" || return 1
  crowdsec_db_authority && crowdsec_db_stopped
}
crowdsec_db_config() {
  crowdsec_db_safe /etc/crowdsec/config.yaml.postgresql 644 || return 1
  [[ ! -e /etc/crowdsec/config.yaml.postgresql.local && ! -L /etc/crowdsec/config.yaml.postgresql.local ]] || return 1
  python3 -I - <<'PY' >/dev/null 2>&1
import yaml
with open('/etc/crowdsec/config.yaml.postgresql',encoding='utf-8') as source:
    config=yaml.safe_load(source)
db=config['db_config']
assert db == dict(type='pgx', db_name='crowdsec', user='crowdsec',
                 db_path='/run/postgresql', host='', port=0, sslmode='disable',
                 max_open_conns=4, log_level='error',
                 flush=dict(max_age='0s',max_items=None))
assert config['common']['log_media']=='file'
assert config['common']['log_dir']=='/run/aboutme/crowdsec'
assert config['common']['log_level']=='error'
assert not config['api']['cti']['enabled']
assert 'online_client' not in config['api']['server']
assert 'client' not in config['api']
PY
}
crowdsec_db_auth() {
  local result
  result=$({ crowdsec_db_begin; printf '%s\n' "
SELECT current_setting('server_version_num')::integer / 10000 = 18
AND current_setting('listen_addresses') = ''
AND current_setting('unix_socket_directories') = '/run/postgresql'
AND current_setting('max_connections')::integer = 40
AND current_setting('hba_file') = '/etc/postgresql/18/main/pg_hba.conf'
AND current_setting('ident_file') = '/etc/postgresql/18/main/pg_ident.conf'
AND NOT pg_is_in_recovery()
AND NOT EXISTS (SELECT FROM pg_hba_file_rules WHERE error IS NOT NULL)
AND NOT EXISTS (SELECT FROM pg_hba_file_rules WHERE type <> 'local' OR auth_method = 'trust')
AND (SELECT count(*) = 1 FROM pg_hba_file_rules WHERE database = ARRAY['crowdsec'] AND user_name = ARRAY['crowdsec'] AND auth_method = 'peer' AND options = ARRAY['map=aboutme_crowdsec'])
AND NOT EXISTS (SELECT FROM pg_hba_file_rules WHERE (database && ARRAY['all','crowdsec'] OR user_name && ARRAY['all','crowdsec']) AND NOT (database = ARRAY['crowdsec'] AND user_name = ARRAY['crowdsec'] AND auth_method = 'peer' AND options = ARRAY['map=aboutme_crowdsec']) AND user_name <> ARRAY['postgres'])
AND NOT EXISTS (SELECT FROM pg_ident_file_mappings WHERE error IS NOT NULL)
AND (SELECT count(*) = 1 FROM pg_ident_file_mappings WHERE map_name = 'aboutme_crowdsec' AND sys_name = 'root' AND pg_username = 'crowdsec')
AND NOT EXISTS (SELECT FROM pg_ident_file_mappings WHERE (map_name = 'aboutme_crowdsec' OR pg_username = 'crowdsec') AND NOT (map_name = 'aboutme_crowdsec' AND sys_name = 'root' AND pg_username = 'crowdsec'));
SELECT extract(epoch FROM pg_conf_load_time());
COMMIT;"; } | crowdsec_db_admin postgres) || return 1
  python3 -I -c '
import decimal,os,pathlib,stat,sys
try:
    valid,loaded=sys.argv[1].splitlines()
    assert valid=="t"
    loaded=decimal.Decimal(loaded)*1000000000
    for path in ("/etc/postgresql/18/main/pg_hba.conf","/etc/postgresql/18/main/pg_ident.conf"):
        entry=os.lstat(path)
        assert stat.S_ISREG(entry.st_mode) and entry.st_nlink==1
        assert entry.st_mode & 0o022==0
        assert entry.st_uid in (0, __import__("pwd").getpwnam("postgres").pw_uid)
        assert entry.st_mtime_ns<=loaded
except Exception: sys.exit(1)
' "$result" 2>/dev/null
}
crowdsec_db_role() {
  local result
  result=$({ crowdsec_db_begin; printf '%s\n' "
SELECT (SELECT count(*) = 1 FROM pg_authid WHERE rolname = 'crowdsec' AND rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication AND NOT rolbypassrls AND rolconnlimit = 8 AND rolpassword IS NULL AND rolvaliduntil IS NULL)
AND NOT EXISTS (SELECT FROM pg_auth_members WHERE member = 'crowdsec'::regrole OR roleid = 'crowdsec'::regrole)
AND NOT EXISTS (SELECT FROM pg_database WHERE datdba = 'crowdsec'::regrole AND datname <> 'crowdsec')
AND NOT EXISTS (SELECT FROM pg_database WHERE datname = 'aboutme' AND has_database_privilege('crowdsec', oid, 'CONNECT'))
AND NOT EXISTS (SELECT FROM pg_db_role_setting WHERE setrole = 'crowdsec'::regrole AND setdatabase <> 0)
AND NOT EXISTS (SELECT FROM pg_db_role_setting, unnest(setconfig) setting WHERE setrole = 'crowdsec'::regrole AND setting NOT IN ('log_min_messages=panic','log_min_error_statement=panic','log_error_verbosity=terse','log_statement=none','log_min_duration_statement=-1','log_min_duration_sample=-1','log_statement_sample_rate=0','log_transaction_sample_rate=0','log_parameter_max_length=0','log_parameter_max_length_on_error=0'))
AND NOT EXISTS (SELECT FROM pg_settings WHERE name IN ('log_min_messages','log_min_error_statement','log_error_verbosity','log_statement','log_min_duration_statement','log_min_duration_sample','log_statement_sample_rate','log_transaction_sample_rate','log_parameter_max_length','log_parameter_max_length_on_error') AND context = 'superuser' AND has_parameter_privilege('crowdsec', name, 'SET'));
COMMIT;"; } | crowdsec_db_admin postgres) || return 1
  [[ $result == t ]]
}
crowdsec_db_catalog_sql() {
  cat <<'SQL'
SELECT 'database' || E'\t' || datname || E'\t' || pg_get_userbyid(datdba) || E'\t' || coalesce(datacl::text, '')
FROM pg_database WHERE datname = current_database();
SELECT 'schema' || E'\t' || nspname || E'\t' || pg_get_userbyid(nspowner) || E'\t' || coalesce(nspacl::text, '')
FROM pg_namespace WHERE nspname = 'public';
SELECT 'relation' || E'\t' || c.relname || E'\t' || c.relkind::text || E'\t' || pg_get_userbyid(c.relowner) || E'\t' || coalesce(c.relacl::text, '')
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public';
SELECT 'column' || E'\t' || c.relname || E'\t' || a.attnum || E'\t' || a.attname || E'\t' || format_type(a.atttypid, a.atttypmod) || E'\t' || a.attnotnull || E'\t' || a.attidentity::text || E'\t' || coalesce(pg_get_expr(d.adbin, d.adrelid), '') || E'\t' || coalesce(a.attacl::text, '')
FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE n.nspname = 'public' AND a.attnum > 0 AND NOT a.attisdropped AND c.relkind IN ('r', 'p');
SELECT 'constraint' || E'\t' || c.relname || E'\t' || con.conname || E'\t' || con.contype::text || E'\t' || pg_get_constraintdef(con.oid, true) || E'\t' || con.convalidated || E'\t' || con.condeferrable || E'\t' || con.condeferred
FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public';
SELECT 'index' || E'\t' || c.relname || E'\t' || i.relname || E'\t' || pg_get_indexdef(i.oid) || E'\t' || x.indisvalid || E'\t' || x.indisready
FROM pg_index x JOIN pg_class c ON c.oid = x.indrelid JOIN pg_class i ON i.oid = x.indexrelid JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public';
SELECT 'sequence' || E'\t' || c.relname || E'\t' || format_type(s.seqtypid, NULL) || E'\t' || s.seqstart || E'\t' || s.seqincrement || E'\t' || s.seqmin || E'\t' || s.seqmax || E'\t' || s.seqcache || E'\t' || s.seqcycle
FROM pg_sequence s JOIN pg_class c ON c.oid = s.seqrelid JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public';
SELECT 'default-acl' || E'\t' || pg_get_userbyid(defaclrole) || E'\t' || defaclobjtype::text || E'\t' || defaclacl::text
FROM pg_default_acl WHERE defaclnamespace = 'public'::regnamespace;
SQL
}
crowdsec_db_policy_sql() {
  cat <<'SQL'
SELECT 'check' || E'\t' || (
current_database() = 'crowdsec' AND current_user = 'crowdsec' AND session_user = 'crowdsec'
AND system_user = 'peer:root' AND inet_client_addr() IS NULL AND NOT pg_is_in_recovery()
AND current_setting('log_min_messages') = 'panic'
AND current_setting('log_min_error_statement') = 'panic'
AND current_setting('log_error_verbosity') = 'terse'
AND current_setting('log_statement') = 'none'
AND current_setting('log_min_duration_statement') = '-1'
AND current_setting('log_min_duration_sample') = '-1'
AND current_setting('log_statement_sample_rate') = '0'
AND current_setting('log_transaction_sample_rate') = '0'
AND current_setting('log_parameter_max_length') = '0'
AND current_setting('log_parameter_max_length_on_error') = '0'
AND (SELECT cardinality(setconfig) = 10 FROM pg_db_role_setting WHERE setrole = 'crowdsec'::regrole AND setdatabase = 0)
AND NOT EXISTS (SELECT FROM pg_namespace WHERE nspname NOT IN ('public','pg_catalog','information_schema','pg_toast') AND nspname !~ '^pg_(temp|toast_temp)_')
AND NOT EXISTS (SELECT FROM pg_proc WHERE pronamespace = 'public'::regnamespace)
AND NOT EXISTS (SELECT FROM pg_type WHERE typnamespace = 'public'::regnamespace AND typtype IN ('e','d'))
AND NOT EXISTS (SELECT FROM pg_default_acl WHERE defaclrole = 'crowdsec'::regrole)
AND NOT EXISTS (SELECT FROM pg_constraint x JOIN pg_class c ON c.oid = x.conrelid WHERE c.relnamespace = 'public'::regnamespace AND NOT x.conenforced)
AND NOT EXISTS (SELECT FROM pg_stats WHERE schemaname = 'public')
AND NOT EXISTS (SELECT FROM pg_statistic_ext WHERE stxnamespace = 'public'::regnamespace)
AND NOT EXISTS (SELECT FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid WHERE c.relnamespace = 'public'::regnamespace AND i.indexprs IS NOT NULL)
AND NOT EXISTS (SELECT FROM pg_class WHERE relnamespace = 'public'::regnamespace AND relkind = 'r' AND (relpersistence <> 'p' OR relrowsecurity OR relforcerowsecurity OR NOT coalesce(reloptions @> ARRAY['autovacuum_enabled=false'],false)))
AND NOT EXISTS (SELECT FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r' AND a.attnum > 0 AND NOT a.attisdropped AND (a.attstattarget <> 0 OR a.attgenerated <> ''))
AND NOT EXISTS (SELECT FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid WHERE c.relnamespace = 'public'::regnamespace AND NOT t.tgisinternal)
AND NOT EXISTS (SELECT FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid WHERE c.relnamespace = 'public'::regnamespace)
)::text;
SELECT 'identity' || E'\t' || oid || ':' || extract(epoch FROM pg_postmaster_start_time())
FROM pg_database WHERE datname = current_database();
SQL
}
crowdsec_db_process() {
  local state
  state=$(systemctl show --all --property=ActiveState --property=Job \
    --property=MainPID --property=InvocationID postgresql@18-main.service 2>/dev/null) || return 1
  python3 -I -c '
import pathlib,re,sys
try:
    entries=sys.argv[1].splitlines()
    assert len(entries)==4
    state=dict(entry.split("=",1) for entry in entries)
    assert state["ActiveState"]=="active" and state["Job"]==""
    pid=state["MainPID"]
    assert re.fullmatch(r"[1-9][0-9]*",pid)
    assert re.fullmatch(r"[0-9a-f]{32}",state["InvocationID"])
    process=pathlib.Path("/proc")/pid
    assert str((process/"exe").resolve())=="/usr/lib/postgresql/18/bin/postgres"
    tick=(process/"stat").read_text().rsplit(")",1)[1].split()[19]
    assert tick.isdecimal()
    boot=pathlib.Path("/proc/sys/kernel/random/boot_id").read_text().strip()
    assert re.fullmatch(r"[0-9a-f-]{36}",boot)
    print(boot+":"+state["InvocationID"]+":"+pid+":"+tick)
except Exception: sys.exit(1)
' "$state" 2>/dev/null
}
crowdsec_db_ready() {
  local before after result identity actual expected=${1:-}
  crowdsec_db_root && crowdsec_db_config && crowdsec_db_auth && crowdsec_db_role || return 1
  before=$(crowdsec_db_process) || return 1
  result=$({ crowdsec_db_begin; crowdsec_db_catalog_sql; crowdsec_db_policy_sql; printf 'COMMIT;\n'; } | crowdsec_db_psql) || return 1
  after=$(crowdsec_db_process) || return 1
  [[ $before == "$after" && $(grep -c '^check' <<<"$result") == 1 && \
    $(grep '^check' <<<"$result") == $'check\ttrue' ]] || return 1
  identity=$(grep '^identity' <<<"$result") || return 1
  [[ $identity =~ ^identity$'\t'[0-9]+:[0-9]+\.[0-9]+$ ]] || return 1
  actual=$(grep -vE '^(check|identity)' <<<"$result" | LC_ALL=C sort) || return 1
  [[ $actual == "$(< /usr/local/lib/aboutme/crowdsec-schema-v1.8.1.tsv)" ]] || return 1
  crowdsec_db_epoch=$(printf '%s\n%s\n%s\n' "$before" "$identity" \
    "$(sha256sum /usr/local/lib/aboutme/crowdsec-schema-v1.8.1.tsv | cut -d' ' -f1)" | sha256sum | cut -d' ' -f1) || return 1
  [[ -z $expected || $expected == "$crowdsec_db_epoch" ]]
}
crowdsec_db_read_lock() {
  crowdsec_db_root && crowdsec_db_mount || return 1
  [[ -f /var/lib/aboutme/fence.lock && ! -L /var/lib/aboutme/fence.lock ]] || return 1
  exec {crowdsec_db_lock_fd}</var/lib/aboutme/fence.lock || return 1
  flock -s -w 2 "$crowdsec_db_lock_fd"
}
crowdsec_db_empty() {
  local result
  # Empty admission exists only inside the owning provisioner's process.
  [[ ${crowdsec_db_provisioning:-} == yes ]] && crowdsec_db_authority &&
    crowdsec_db_stopped && crowdsec_db_auth && crowdsec_db_role || return 1
  result=$({ crowdsec_db_begin; printf '%s\n' "
SELECT current_database() = 'crowdsec' AND current_user = 'crowdsec' AND system_user = 'peer:root'
AND (SELECT pg_get_userbyid(datdba) = 'crowdsec' AND datacl::text = '{crowdsec=CTc/crowdsec}' FROM pg_database WHERE datname = current_database())
AND (SELECT pg_get_userbyid(nspowner) = 'crowdsec' AND nspacl::text = '{crowdsec=UC/crowdsec}' FROM pg_namespace WHERE nspname = 'public')
AND NOT EXISTS (SELECT FROM pg_class WHERE relnamespace = 'public'::regnamespace)
AND NOT EXISTS (SELECT FROM pg_proc WHERE pronamespace = 'public'::regnamespace)
AND NOT EXISTS (SELECT FROM pg_type WHERE typnamespace = 'public'::regnamespace)
AND NOT EXISTS (SELECT FROM pg_namespace WHERE nspname NOT IN ('public','pg_catalog','information_schema','pg_toast') AND nspname !~ '^pg_(temp|toast_temp)_')
AND NOT EXISTS (SELECT FROM pg_default_acl WHERE defaclrole = 'crowdsec'::regrole);
COMMIT;"; } | crowdsec_db_psql) || return 1
  [[ $result == t ]]
}
crowdsec_db_apply() {
  local actual
  [[ ${crowdsec_db_provisioning:-} == yes ]] && crowdsec_db_authority &&
    crowdsec_db_stopped && crowdsec_db_auth && crowdsec_db_role || return 1
  actual=$({ crowdsec_db_begin; crowdsec_db_catalog_sql; printf 'COMMIT;\n'; } | crowdsec_db_psql | LC_ALL=C sort) || return 1
  [[ $actual == "$(< /usr/local/lib/aboutme/crowdsec-schema-v1.8.1.tsv)" ]] || return 1
  crowdsec_db_psql >/dev/null <<'SQL'
BEGIN;
SET LOCAL statement_timeout = '10s';
SET LOCAL lock_timeout = '2s';
DO $policy$
DECLARE rel record; col record; occupied boolean;
BEGIN
  IF EXISTS (SELECT FROM pg_stats WHERE schemaname = 'public') OR
     EXISTS (SELECT FROM pg_statistic_ext WHERE stxnamespace = 'public'::regnamespace) THEN
    RAISE EXCEPTION 'invalid_state';
  END IF;
  FOR rel IN SELECT relname FROM pg_class WHERE relnamespace = 'public'::regnamespace AND relkind = 'r' LOOP
    EXECUTE format('SELECT EXISTS (SELECT FROM public.%I)', rel.relname) INTO occupied;
    IF occupied THEN RAISE EXCEPTION 'invalid_state'; END IF;
    EXECUTE format('ALTER TABLE public.%I SET (autovacuum_enabled=false)', rel.relname);
    FOR col IN SELECT a.attname FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid WHERE c.relnamespace = 'public'::regnamespace AND c.relname = rel.relname AND a.attnum > 0 AND NOT a.attisdropped LOOP
      EXECUTE format('ALTER TABLE public.%I ALTER COLUMN %I SET STATISTICS 0', rel.relname, col.attname);
    END LOOP;
  END LOOP;
END
$policy$;
COMMIT;
SQL
}
crowdsec_db_main() {
  [[ $# == 1 ]] || { crowdsec_db_fail arguments; return 1; }
  case $1 in
    preflight-ready)
      local expected
      crowdsec_db_read_lock || { crowdsec_db_fail authority; return 1; }
      expected=$(timeout 5 python3 -I -c '
import re,sys
raw=sys.stdin.buffer.read(66)
if raw and not re.fullmatch(rb"[0-9a-f]{64}\n?",raw): sys.exit(1)
print(raw.decode("ascii").rstrip("\n"),end="")
') || { crowdsec_db_fail epoch; return 1; }
      crowdsec_db_ready "$expected" || { crowdsec_db_fail ready; return 1; }
      if [[ -e /proc/$$/fd/3 ]]; then
        python3 -I -c '
import os,stat,sys
try:
    entry=os.fstat(3)
    assert entry.st_uid==entry.st_gid==0 and stat.S_IMODE(entry.st_mode)==0o600
    assert (stat.S_ISREG(entry.st_mode) and entry.st_nlink==1) or stat.S_ISFIFO(entry.st_mode)
except Exception: sys.exit(1)
' || return 1
        printf '%s\n' "$crowdsec_db_epoch" >&3 || return 1
      fi
      ;;
    preflight-empty | apply-policy)
      crowdsec_db_authority_input || { crowdsec_db_fail authority; return 1; }
      [[ ${crowdsec_db_provisioning:-} == yes ]] && crowdsec_db_authority && \
        crowdsec_db_stopped || { crowdsec_db_fail provisioner_required; return 1; }
      if [[ $1 == preflight-empty ]]; then crowdsec_db_empty; else crowdsec_db_apply; fi
      ;;
    *) crowdsec_db_fail arguments; return 1 ;;
  esac
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  set -euo pipefail
  umask 077
  crowdsec_db_main "$@" 2>/dev/null || { printf 'crowdsec_db_refused\n' >&2; exit 1; }
fi
