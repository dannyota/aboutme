#!/usr/bin/env bash
# Hosted-only catalog derivation and regressions for the inactive database gate.
set -euo pipefail
umask 077
trap 'printf "FAIL harness-command\n"' ERR

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
host=$repo_root/deploy/vn/host
candidate=$repo_root/deploy/vn/edge/crowdsec/config.yaml.local
mode=${1:-static}
failures=0
scratch=''
pg_started=no
loop_device=''
pgbin=/usr/lib/postgresql/18/bin
test_psql=$pgbin/psql
pg_start_options="-c listen_addresses='' -c unix_socket_directories=/run/postgresql -c shared_buffers=32MB -c max_connections=40 -c log_statement=none -c log_min_messages=panic -c log_min_error_statement=panic -c log_error_verbosity=terse -c log_parameter_max_length=0 -c log_parameter_max_length_on_error=0"

pass() { printf 'PASS %s\n' "$1"; }
fail() { printf 'FAIL %s\n' "$1"; failures=$((failures + 1)); }
check() {
  local label=$1
  shift
  if "$@" >/dev/null 2>&1; then pass "$label"; else fail "$label"; fi
}

static_tests() {
  check provisioner-present test -f "$host/crowdsec-provision.sh"
  check gate-present test -f "$host/crowdsec-db.sh"
  check reviewed-manifest-present test -s "$host/crowdsec-schema-v1.8.1.tsv"
  check candidate-socket-config python3 - "$candidate" <<'PY'
import sys
import yaml
with open(sys.argv[1], encoding="utf-8") as source:
    config = yaml.safe_load(source)
db = config["db_config"]
assert db["type"] == "pgx"
assert db["db_name"] == db["user"] == "crowdsec"
assert db["db_path"] == "/run/postgresql"
assert db["host"] == "" and db["port"] == 0
assert db["sslmode"] == "disable" and db["max_open_conns"] == 4
assert db["flush"]["max_age"] == "0s" and db["flush"]["max_items"] is None
assert "password" not in db and "use_wal" not in db
assert db.get("log_level", "info") not in ("debug", "trace")
PY
  check scoped-peer-auth python3 - "$host/etc/postgresql/18/main" <<'PY'
import pathlib
import sys
root = pathlib.Path(sys.argv[1])
hba = [line.split("#", 1)[0].split() for line in
       (root / "pg_hba.conf").read_text().splitlines()]
rules = [rule for rule in hba if rule and "crowdsec" in " ".join(rule)]
assert len(rules) == 1
rule = rules[0]
assert rule[:4] == ["local", "crowdsec", "crowdsec", "peer"]
assert len(rule) == 5 and rule[4].startswith("map=")
name = rule[4][4:]
maps = [line.split("#", 1)[0].split() for line in
        (root / "pg_ident.conf").read_text().splitlines()]
assert [entry for entry in maps if entry and entry[0] == name] == [
    [name, "root", "crowdsec"]]
assert not any(entry and entry[0].startswith("host") for entry in hba)
assert not any("trust" in entry for entry in hba)
assert not any(entry and entry[0] == "local" and
               (entry[1] == "all" or entry[2] == "all") and
               entry[2] != "postgres" for entry in hba)
PY
}

cleanup() {
  local status=$?
  trap - EXIT
  if [[ $pg_started == yes ]]; then
    if ! runuser -u postgres -- "$pgbin/pg_ctl" -D "$scratch/data" \
      -m immediate -w stop >/dev/null 2>&1; then
      printf 'FAIL harness-stop\n'
      exit 1
    fi
  fi
  # Only the exact mktemp mount created by this process can be removed.
  if [[ $scratch == /run/crowdsec-db-test.* ]]; then
    if [[ -n $loop_device ]]; then
      umount /var/lib/aboutme /srv/data
      losetup -d "$loop_device"
    fi
    umount "$scratch"
    rmdir -- "$scratch"
  fi
  exit "$status"
}

admin() {
  runuser -u postgres -- "$test_psql" -X -qAt -v ON_ERROR_STOP=1 \
    -h /run/postgresql -U postgres -d "${1:-crowdsec}"
}

runtime() {
  "$test_psql" -X -qAt -v ON_ERROR_STOP=1 \
    -h /run/postgresql -U crowdsec -d crowdsec
}

start_harness() {
  [[ ${GITHUB_ACTIONS:-} == true && $EUID == 0 ]] || {
    printf 'FAIL hosted-root-required\n'; exit 1;
  }
  # The caller supplies a private mount namespace. Never hide a live host socket.
  [[ $(readlink /proc/self/ns/mnt) != "$(readlink /proc/1/ns/mnt)" ]] || {
    printf 'FAIL private-mount-required\n'; exit 1;
  }
  [[ $("$pgbin/postgres" --version) == 'postgres (PostgreSQL) 18.'* ]] || {
    printf 'FAIL postgresql-version\n'; exit 1;
  }
  crowdsec -version 2>&1 | grep -qF v1.8.1 || {
    printf 'FAIL crowdsec-version\n'; exit 1;
  }
  mount -t tmpfs -o size=256m,mode=0755 tmpfs /run/postgresql
  chown postgres:postgres /run/postgresql
  scratch=$(mktemp -d /run/crowdsec-db-test.XXXXXX)
  chmod 0755 "$scratch"
  mount -t tmpfs -o size=512m,mode=0755 tmpfs "$scratch"
  trap cleanup EXIT
  install -d -o postgres -g postgres -m 0700 "$scratch/data"
  runuser -u postgres -- "$pgbin/initdb" -D "$scratch/data" \
    --auth-local=peer --auth-host=reject --no-locale --encoding=UTF8 >/dev/null 2>&1
  printf 'local all postgres peer\nlocal crowdsec crowdsec peer map=crowdsec_test\n' \
    >"$scratch/data/pg_hba.conf"
  printf 'crowdsec_test root crowdsec\n' >"$scratch/data/pg_ident.conf"
  runuser -u postgres -- "$pgbin/pg_ctl" -D "$scratch/data" \
    -l "$scratch/data/server.log" -w start \
    -o "$pg_start_options" \
    >/dev/null 2>&1
  pg_started=yes
  admin postgres >/dev/null 2>&1 <<'SQL'
CREATE ROLE crowdsec LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 8;
CREATE DATABASE crowdsec OWNER crowdsec;
REVOKE ALL ON DATABASE crowdsec FROM PUBLIC;
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
CREATE ROLE aboutme_app LOGIN;
CREATE ROLE aboutme_migrator LOGIN;
CREATE DATABASE aboutme OWNER aboutme_migrator;
REVOKE ALL ON DATABASE aboutme FROM PUBLIC;
SQL
  admin >/dev/null 2>&1 <<'SQL'
ALTER SCHEMA public OWNER TO crowdsec;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
SQL
  install -d -m 0700 "$scratch/config" "$scratch/engine-data"
  # No installed credentials or acquisition sources enter this test config.
  python3 - "$scratch" <<'PY'
import pathlib
import sys
import yaml
root = pathlib.Path(sys.argv[1])
config = {
    "common": {"log_media": "stdout", "log_level": "error"},
    "config_paths": {"config_dir": str(root / "config"),
                     "data_dir": str(root / "engine-data"),
                     "hub_dir": "/etc/crowdsec/hub",
                     "index_path": "/etc/crowdsec/hub/.index.json"},
    "db_config": {"type": "pgx", "db_name": "crowdsec", "user": "crowdsec",
                  "db_path": "/run/postgresql", "host": "", "port": 0,
                  "sslmode": "disable", "max_open_conns": 4,
                  "log_level": "error", "flush": {"max_age": "0s", "max_items": None}},
    "api": {"server": {"enable": True,
                        "listen_socket": str(root / "lapi.sock"),
                        "profiles_path": str(root / "config/profiles.yaml"),
                        "console_path": str(root / "config/console.yaml")}},
    "cscli": {"output": "json", "color": "never"},
}
(root / "config/profiles.yaml").write_text(yaml.safe_dump(
    {"name": "catalog-only", "filters": ["false"], "decisions": []}))
(root / "config/console.yaml").write_text(yaml.safe_dump(
    {"share_custom": False, "share_tainted": False,
     "share_manual_decisions": False, "share_context": False}))
(root / "config/config.yaml").write_text(yaml.safe_dump(config))
PY
  # This exception belongs only to the disposable, verified-empty derivation DB.
  [[ $(admin <<<'BEGIN READ ONLY; SELECT count(*) FROM pg_class WHERE relnamespace = '\''public'\''::regnamespace; COMMIT;') == 0 ]]
  timeout 60 cscli -c "$scratch/config/config.yaml" bouncers list -o json >/dev/null 2>&1
  [[ $(admin <<<'SELECT count(*) FROM pg_class WHERE relnamespace = '\''public'\''::regnamespace AND relkind = '\''r'\'';') -gt 0 ]]
  pass pinned-schema-initialized
}

catalog() {
  # This allowlist selects catalog metadata only. It never scans user tables.
  admin <<'SQL'
BEGIN READ ONLY;
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
COMMIT;
SQL
}

gate() { bash "$host/crowdsec-db.sh" "$@"; }
rejects_ready() { ! gate preflight-ready; }

regressions() {
  if [[ ! -f $host/crowdsec-db.sh || ! -f $host/crowdsec-provision.sh ]]; then
    fail provisioner-required-before-policy
    return
  fi
  prepare_installed_fixture
  check invalid-ownership-refuses fresh_refuses_invalid_owner
  check missing-marker-refuses fresh_refuses_missing_marker
  check active-consumer-refuses active_consumer_refused
  check sourced-gate-cannot-bypass-owner source_cannot_bypass
  check fresh-nested-lock-completes provision fresh
  check schema-actions-follow-read-only action_trace_valid
  check initialized-ready gate preflight-ready
  check empty-preflight-refuses-unfenced gate_refuses_empty
  check role-budget runtime_boolean <<'SQL'
SELECT count(*) = 1 FROM pg_roles WHERE rolname = 'crowdsec' AND rolcanlogin
AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication
AND NOT rolbypassrls AND rolconnlimit = 8;
SQL
  check no-memberships runtime_boolean <<'SQL'
SELECT NOT EXISTS (SELECT FROM pg_auth_members WHERE member = 'crowdsec'::regrole OR roleid = 'crowdsec'::regrole);
SQL
  check application-isolation runtime_boolean <<'SQL'
SELECT NOT has_database_privilege('crowdsec', 'aboutme', 'CONNECT')
AND NOT has_database_privilege('aboutme_app', 'crowdsec', 'CONNECT')
AND NOT has_database_privilege('aboutme_migrator', 'crowdsec', 'CONNECT');
SQL
  check protected-role-logging runtime_boolean <<'SQL'
SELECT current_setting('log_min_messages') = 'panic'
AND current_setting('log_min_error_statement') = 'panic'
AND current_setting('log_error_verbosity') = 'terse'
AND current_setting('log_statement') = 'none'
AND current_setting('log_min_duration_statement') = '-1'
AND current_setting('log_min_duration_sample') = '-1'
AND current_setting('log_statement_sample_rate') = '0'
AND current_setting('log_transaction_sample_rate') = '0'
AND current_setting('log_parameter_max_length') = '0'
AND current_setting('log_parameter_max_length_on_error') = '0';
SQL
  check protected-log-setting-refuses-write logging_refuses_write
  check private-planner-state runtime_boolean <<'SQL'
SELECT NOT EXISTS (SELECT FROM pg_stats WHERE schemaname = 'public')
AND NOT EXISTS (SELECT FROM pg_statistic_ext WHERE stxnamespace = 'public'::regnamespace)
AND NOT EXISTS (SELECT FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid WHERE c.relnamespace = 'public'::regnamespace AND i.indexprs IS NOT NULL)
AND NOT EXISTS (SELECT FROM pg_class WHERE relnamespace = 'public'::regnamespace AND relkind = 'r' AND NOT coalesce(reloptions @> ARRAY['autovacuum_enabled=false'], false))
AND NOT EXISTS (SELECT FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r' AND a.attnum > 0 AND NOT a.attisdropped AND a.attstattarget <> 0);
SQL
  admin >/dev/null 2>&1 <<'SQL'
CREATE TABLE public.unexpected_relation (id integer);
SQL
  check extra-relation-closes-gate rejects_ready
  admin >/dev/null 2>&1 <<'SQL'
DROP TABLE public.unexpected_relation;
ALTER ROLE crowdsec CONNECTION LIMIT 9;
SQL
  check changed-role-budget-closes-gate rejects_ready
  admin >/dev/null 2>&1 <<'SQL'
ALTER ROLE crowdsec CONNECTION LIMIT 8;
GRANT CONNECT ON DATABASE crowdsec TO aboutme_app;
SQL
  check application-grant-closes-gate rejects_ready
  admin >/dev/null 2>&1 <<'SQL'
REVOKE CONNECT ON DATABASE crowdsec FROM aboutme_app;
SQL
  check repaired-catalog-ready gate preflight-ready
  check fresh-refuses-existing-schema fresh_refuses_existing
  check owner-marker-retained marker_retained
  check peer-root-admitted runtime_boolean <<'SQL'
SELECT current_user = 'crowdsec' AND current_database() = 'crowdsec'
AND system_user = 'peer:root' AND inet_client_addr() IS NULL;
SQL
  check application-os-user-refused application_identity_refused
  check container-uids-refused container_uids_refused
  check tcp-refused tcp_refused
  check missing-column-closes-gate missing_column_rejected
  check missing-index-closes-gate missing_index_rejected
  check missing-constraint-closes-gate missing_constraint_rejected
  check statistics-target-closes-gate target_rejected
  check expression-index-closes-gate expression_rejected
  check extended-statistics-closes-gate extended_rejected
  check public-grant-closes-gate public_grant_rejected
  check role-membership-closes-gate membership_rejected
  check global-default-grant-closes-gate global_default_rejected
  check logging-grant-closes-gate logging_grant_rejected
  check database-owner-closes-gate database_owner_rejected
  check changed-manifest-closes-gate manifest_rejected
  check config-overlay-refused config_overlay_rejected
  check forced-errors-stay-private forced_errors_private
  check ordinary-vacuum-stays-private ordinary_vacuum_private
  check planner-contamination-refuses-repair contamination_rejected
  check same-epoch-admitted same_epoch
  check service-invocation-invalidates-epoch invocation_invalidates
  check restart-invalidates-epoch restart_invalidates
  check restore-replaces-only-crowdsec restore_isolated
  check recovery-closes-gate recovery_rejected
  check empty-outside-provisioner-stays-empty empty_refused_without_mutation
  check malformed-epoch-refused malformed_epoch
}

prepare_installed_fixture() {
  install -d -m 0755 /srv/data /var/lib/aboutme /usr/local/lib/aboutme
  truncate -s 64M "$scratch/fence-disk"
  mkfs.ext4 -q -L aboutme-data "$scratch/fence-disk"
  loop_device=$(losetup --find --show "$scratch/fence-disk")
  mount "$loop_device" /srv/data
  install -d -m 0755 /srv/data/aboutme
  install -d -m 0700 /srv/data/host-operations
  mount --bind /srv/data/aboutme /var/lib/aboutme
  install -d -m 0755 "$scratch/lib" "$scratch/bin"
  for script in crowdsec-db.sh crowdsec-provision.sh crowdsec-privacy-notice-host.sh; do
    install -m 0755 "$host/$script" "$scratch/lib/$script"
  done
  install -m 0755 "$repo_root/deploy/vn/scripts/fence.sh" "$scratch/lib/fence.sh"
  install -m 0644 "$host/crowdsec-schema-v1.8.1.tsv" "$scratch/lib/"
  mount --bind "$scratch/lib" /usr/local/lib/aboutme
  install -m 0644 "$candidate" /etc/crowdsec/config.yaml.postgresql
  install -d -m 0755 /run/aboutme
  mount -t tmpfs -o size=16m,mode=0755 tmpfs /run/aboutme
  install -d -m 0700 /run/aboutme/crowdsec
  mount -t tmpfs -o size=16m,mode=0755 tmpfs /var/lib/crowdsec
  install -d -m 0700 /var/lib/crowdsec/data
  install -d -m 0755 "$scratch/pgconfig" /etc/postgresql/18/main
  install -m 0644 "$host/etc/postgresql/18/main/pg_ident.conf" "$scratch/pgconfig/pg_ident.conf"
  install -m 0644 "$host/etc/postgresql/18/main/pg_hba.conf" "$scratch/pgconfig/pg_hba.conf"
  mount --bind "$scratch/pgconfig" /etc/postgresql/18/main
  printf "hba_file = '/etc/postgresql/18/main/pg_hba.conf'\nident_file = '/etc/postgresql/18/main/pg_ident.conf'\n" >>"$scratch/data/postgresql.conf"
  runuser -u postgres -- "$pgbin/pg_ctl" -D "$scratch/data" -m fast -w restart >/dev/null 2>&1
  cp /usr/bin/systemctl "$scratch/bin/systemctl-real"
  printf '#!/usr/bin/env bash\nset -euo pipefail\n' >"$scratch/bin/systemctl"
  printf 'case "$*" in\n*MainPID*) printf "ActiveState=active\\nJob=\\nMainPID=%%s\\nInvocationID=%%s\\n" "$(head -n1 %q/data/postmaster.pid)" "$(cat %q/invocation)" ;;\n*show*) printf "ActiveState=%%s\\nJob=\\n" "$(cat %q/consumer-state)" ;;\n*) exit 1 ;;\nesac\n' \
    "$scratch" "$scratch" "$scratch" >>"$scratch/bin/systemctl"
  chmod 0755 "$scratch/bin/systemctl"
  printf 'inactive\n' >"$scratch/consumer-state"
  printf '11111111111111111111111111111111\n' >"$scratch/invocation"
  mount --bind "$scratch/bin/systemctl" /usr/bin/systemctl
  instrument_actions
  /usr/local/lib/aboutme/fence.sh init >/dev/null 2>&1
  operation=$(/usr/local/lib/aboutme/fence.sh lock cutover v0.8.9 2>/dev/null)
  /usr/local/lib/aboutme/fence.sh mutation-start "$operation" v0.8.9 reset-db >/dev/null 2>&1
  admin aboutme >/dev/null <<'SQL'
CREATE TABLE public.provisioner_sentinel (id integer PRIMARY KEY);
INSERT INTO public.provisioner_sentinel VALUES (1);
SQL
  admin postgres >/dev/null <<'SQL'
DROP DATABASE crowdsec;
DROP ROLE crowdsec;
SQL
}

authority() { printf '{"operation_id":"%s","release_tag":"v0.8.9"}\n' "$operation"; }
provision() { authority | timeout 45 bash /usr/local/lib/aboutme/crowdsec-provision.sh "$1"; }
fresh_refuses_invalid_owner() {
  ! printf '{"operation_id":"%043d","release_tag":"v0.8.9"}\n' 0 | \
    timeout 10 bash /usr/local/lib/aboutme/crowdsec-provision.sh fresh
}
fresh_refuses_missing_marker() {
  mv /var/lib/aboutme/cutover-mutation.json "$scratch/marker"
  local rc=0
  provision fresh || rc=$?
  mv "$scratch/marker" /var/lib/aboutme/cutover-mutation.json
  ((rc != 0))
}
fresh_refuses_existing() { ! provision fresh; }
active_consumer_refused() {
  printf 'active\n' >"$scratch/consumer-state"
  local rc=0
  provision fresh || rc=$?
  printf 'inactive\n' >"$scratch/consumer-state"
  ((rc != 0))
}
source_cannot_bypass() {
  ! bash -c 'source /usr/local/lib/aboutme/crowdsec-db.sh; crowdsec_db_provisioning=yes; crowdsec_db_empty'
}
marker_retained() {
  jq -e --arg owner "$operation" '.operation_id == $owner and .mutation_kind == "reset-db"' \
    /var/lib/aboutme/cutover-mutation.json >/dev/null
}
application_identity_refused() {
  ! runuser -u nobody -- "$test_psql" -XAt -w -h /run/postgresql -U crowdsec -d crowdsec -c 'SELECT 1'
}
container_uids_refused() {
  local uid
  for uid in 1000 10001; do
    if setpriv --reuid "$uid" --regid "$uid" --clear-groups \
      "$test_psql" -XAt -w -h /run/postgresql -U crowdsec -d crowdsec -c 'SELECT 1'; then
      return 1
    fi
  done
}
tcp_refused() { ! "$test_psql" -XAt -w -h 127.0.0.1 -p 20999 -U crowdsec -d crowdsec -c 'SELECT 1'; }
missing_column_rejected() {
  admin <<<'ALTER TABLE public.events RENAME COLUMN serialized TO unexpected;' >/dev/null
  local rc=0
  gate preflight-ready </dev/null || rc=$?
  admin <<<'ALTER TABLE public.events RENAME COLUMN unexpected TO serialized;' >/dev/null
  ((rc != 0))
}
missing_index_rejected() {
  admin <<<'ALTER INDEX public.alert_id RENAME TO unexpected;' >/dev/null
  local rc=0
  gate preflight-ready </dev/null || rc=$?
  admin <<<'ALTER INDEX public.unexpected RENAME TO alert_id;' >/dev/null
  ((rc != 0))
}
missing_constraint_rejected() {
  admin <<<'ALTER TABLE public.events DROP CONSTRAINT events_alerts_events;' >/dev/null
  local rc=0
  gate preflight-ready </dev/null || rc=$?
  admin <<<'ALTER TABLE public.events ADD CONSTRAINT events_alerts_events FOREIGN KEY (alert_events) REFERENCES public.alerts(id) ON DELETE CASCADE;' >/dev/null
  ((rc != 0))
}
target_rejected() {
  admin <<<'ALTER TABLE public.events ALTER COLUMN serialized SET STATISTICS 1;' >/dev/null
  local rc=0
  gate preflight-ready </dev/null || rc=$?
  admin <<<'ALTER TABLE public.events ALTER COLUMN serialized SET STATISTICS 0;' >/dev/null
  ((rc != 0))
}
expression_rejected() {
  admin <<<'CREATE INDEX unexpected ON public.events ((length(serialized)));' >/dev/null
  local rc=0
  gate preflight-ready </dev/null || rc=$?
  admin <<<'DROP INDEX public.unexpected;' >/dev/null
  ((rc != 0))
}
extended_rejected() {
  admin <<<'CREATE STATISTICS unexpected ON created_at, updated_at FROM public.events;' >/dev/null
  local rc=0
  gate preflight-ready </dev/null || rc=$?
  admin <<<'DROP STATISTICS public.unexpected;' >/dev/null
  ((rc != 0))
}
public_grant_rejected() {
  admin <<<'GRANT CONNECT ON DATABASE crowdsec TO PUBLIC;' >/dev/null
  local rc=0
  gate preflight-ready </dev/null || rc=$?
  admin <<<'REVOKE CONNECT ON DATABASE crowdsec FROM PUBLIC;' >/dev/null
  ((rc != 0))
}
membership_rejected() {
  admin <<<'GRANT aboutme_app TO crowdsec;' >/dev/null
  local rc=0
  gate preflight-ready </dev/null || rc=$?
  admin <<<'REVOKE aboutme_app FROM crowdsec;' >/dev/null
  ((rc != 0))
}
global_default_rejected() {
  admin <<<'ALTER DEFAULT PRIVILEGES FOR ROLE crowdsec GRANT SELECT ON TABLES TO aboutme_app;' >/dev/null
  local rc=0
  gate preflight-ready </dev/null || rc=$?
  admin <<<'ALTER DEFAULT PRIVILEGES FOR ROLE crowdsec REVOKE SELECT ON TABLES FROM aboutme_app;' >/dev/null
  ((rc != 0))
}
logging_grant_rejected() {
  admin <<<'GRANT SET ON PARAMETER log_min_error_statement TO crowdsec;' >/dev/null
  local rc=0
  gate preflight-ready </dev/null || rc=$?
  admin <<<'REVOKE SET ON PARAMETER log_min_error_statement FROM crowdsec;' >/dev/null
  ((rc != 0))
}
database_owner_rejected() {
  admin postgres <<<'ALTER DATABASE crowdsec OWNER TO postgres;' >/dev/null
  local rc=0
  gate preflight-ready </dev/null || rc=$?
  admin postgres <<<'ALTER DATABASE crowdsec OWNER TO crowdsec;' >/dev/null
  ((rc != 0))
}
manifest_rejected() {
  cp /usr/local/lib/aboutme/crowdsec-schema-v1.8.1.tsv "$scratch/manifest"
  printf 'relation\tunexpected\tr\tcrowdsec\t\n' >>/usr/local/lib/aboutme/crowdsec-schema-v1.8.1.tsv
  local rc=0
  gate preflight-ready </dev/null || rc=$?
  cp "$scratch/manifest" /usr/local/lib/aboutme/crowdsec-schema-v1.8.1.tsv
  ((rc != 0))
}
config_overlay_rejected() {
  printf 'db_config:\n  type: sqlite\n' >/etc/crowdsec/config.yaml.postgresql.local
  local rc=0
  gate preflight-ready </dev/null || rc=$?
  rm /etc/crowdsec/config.yaml.postgresql.local
  ((rc != 0))
}
forced_errors_private() {
  runtime <<<'INSERT INTO events (created_at, updated_at, time, serialized) SELECT now(), now(), now(), '\''private-test-marker'\'' FROM generate_series(1,64);' >/dev/null
  ! runtime <<<'SELECT '\''private-test-marker'\''::integer;' >/dev/null 2>&1 || return 1
  ! runtime <<<'INSERT INTO events (serialized) VALUES ('\''private-test-marker'\'');' >/dev/null 2>&1 || return 1
  ! grep -qF private-test-marker "$scratch/data/server.log"
}
ordinary_vacuum_private() {
  runtime <<<'VACUUM (ANALYZE FALSE) public.events;' >/dev/null
  gate preflight-ready </dev/null
}
contamination_rejected() {
  # The disposable fixture deliberately contaminates planner state to prove
  # the production boundary requires database replacement, never catalog edits.
  admin <<<'ALTER TABLE public.events ALTER COLUMN serialized SET STATISTICS 100; ANALYZE public.events; ALTER TABLE public.events ALTER COLUMN serialized SET STATISTICS 0;' >/dev/null
  ! gate preflight-ready </dev/null || return 1
  ! authority | gate apply-policy || return 1
  provision restore-reset
}
same_epoch() {
  gate preflight-ready </dev/null 3>"$scratch/epoch"
  [[ $(wc -c <"$scratch/epoch") == 65 ]] || return 1
  gate preflight-ready <"$scratch/epoch"
}
invocation_invalidates() {
  gate preflight-ready </dev/null 3>"$scratch/epoch"
  printf '22222222222222222222222222222222\n' >"$scratch/invocation"
  local rc=0
  gate preflight-ready <"$scratch/epoch" || rc=$?
  printf '11111111111111111111111111111111\n' >"$scratch/invocation"
  ((rc != 0))
}
restart_invalidates() {
  gate preflight-ready </dev/null 3>"$scratch/epoch"
  runuser -u postgres -- "$pgbin/pg_ctl" -D "$scratch/data" -m fast -w restart >/dev/null 2>&1
  ! gate preflight-ready <"$scratch/epoch"
}
restore_isolated() {
  provision restore-reset || return 1
  [[ $(admin aboutme <<<'SELECT count(*) FROM public.provisioner_sentinel;') == 1 ]] || return 1
  [[ $(runtime <<<'SELECT count(*) FROM public.events;') == 0 ]] || return 1
  gate preflight-ready </dev/null
}
recovery_rejected() {
  runuser -u postgres -- "$pgbin/pg_ctl" -D "$scratch/data" -m fast -w stop >/dev/null 2>&1
  touch "$scratch/data/standby.signal"
  runuser -u postgres -- "$pgbin/pg_ctl" -D "$scratch/data" -l "$scratch/data/server.log" \
    -w start -o "$pg_start_options" >/dev/null 2>&1
  [[ $(admin <<<'SELECT pg_is_in_recovery();') == t ]] || return 1
  local rc=0
  gate preflight-ready </dev/null || rc=$?
  runuser -u postgres -- "$pgbin/pg_ctl" -D "$scratch/data" -m fast -w stop >/dev/null 2>&1
  rm "$scratch/data/standby.signal"
  runuser -u postgres -- "$pgbin/pg_ctl" -D "$scratch/data" -l "$scratch/data/server.log" \
    -w start -o "$pg_start_options" >/dev/null 2>&1
  ((rc != 0)) && gate preflight-ready </dev/null
}
empty_refused_without_mutation() {
  admin <<<'DROP SCHEMA public CASCADE; CREATE SCHEMA public AUTHORIZATION crowdsec; REVOKE ALL ON SCHEMA public FROM PUBLIC;' >/dev/null
  ! gate preflight-ready </dev/null || return 1
  ! authority | gate preflight-empty || return 1
  [[ $(admin <<<'SELECT count(*) FROM pg_class WHERE relnamespace = '\''public'\''::regnamespace;') == 0 ]]
}
malformed_epoch() { ! printf 'malformed\n' | gate preflight-ready; }

instrument_actions() {
  install -d -o root -g postgres -m 2770 "$scratch/trace"
  cp "$pgbin/psql" "$scratch/bin/psql-real"
  chmod 0755 "$scratch/bin/psql-real"
  test_psql=$scratch/bin/psql-real
  printf '#!/usr/bin/env bash\nset -euo pipefail\numask 007\nfixture=%q\n' "$scratch" >"$scratch/bin/psql"
  cat >>"$scratch/bin/psql" <<'SH'
[[ " $* " == *' -X '* && " $* " == *' ON_ERROR_STOP=1 '* ]] || exit 1
input=$(mktemp "$fixture/trace/sql.XXXXXX")
trap 'rm -f "$input"' EXIT
cat >"$input"
rm -f "$fixture/trace/read-only"
"$fixture/bin/psql-real" "$@" <"$input"
if grep -qE '^BEGIN .*READ ONLY;' "$input"; then
  printf 'read-only\n' >"$fixture/trace/read-only"
fi
SH
  chmod 0755 "$scratch/bin/psql"
  mount --bind "$scratch/bin/psql" "$pgbin/psql"
  local binary
  for binary in cscli crowdsec; do
    cp "/usr/bin/$binary" "$scratch/bin/$binary-real"
    chmod 0755 "$scratch/bin/$binary-real"
    printf '#!/usr/bin/env bash\nset -euo pipefail\nfixture=%q\nbinary=%q\n' "$scratch" "$binary" >"$scratch/bin/$binary"
    cat >>"$scratch/bin/$binary" <<'SH'
[[ -s $fixture/trace/read-only ]] || exit 1
rm "$fixture/trace/read-only"
printf '%s\n' "$binary" >>"$fixture/trace/actions"
exec "$fixture/bin/$binary-real" "$@"
SH
    chmod 0755 "$scratch/bin/$binary"
    mount --bind "$scratch/bin/$binary" "/usr/bin/$binary"
  done
}
action_trace_valid() { [[ $(<"$scratch/trace/actions") == $'cscli\ncrowdsec' ]]; }

gate_refuses_empty() { ! gate preflight-empty; }
runtime_boolean() { [[ $(runtime) == t ]]; }
logging_refuses_write() { ! runtime <<<'SET log_min_error_statement = '\''error'\'';'; }

case $mode in
  static) static_tests ;;
  derive)
    start_harness
    printf 'BEGIN crowdsec-v1.8.1-postgresql18-catalog\n'
    catalog 2>/dev/null | LC_ALL=C sort
    printf 'END crowdsec-v1.8.1-postgresql18-catalog\n'
    ;;
  integration)
    static_tests
    start_harness
    regressions
    ;;
  *) printf 'FAIL unknown-mode\n'; exit 1 ;;
esac
((failures == 0))
