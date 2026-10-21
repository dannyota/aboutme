#!/usr/bin/env bash
# Hosted-only catalog derivation and regressions for the inactive database gate.
set -euo pipefail
umask 077
trap 'printf "FAIL harness-command\n"' ERR

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
host=$repo_root/deploy/vn/host
candidate=$repo_root/deploy/vn/edge/crowdsec/config.yaml.postgresql
mode=${1:-static}
failures=0
scratch=''
pg_started=no
pgbin=/usr/lib/postgresql/18/bin

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
    umount "$scratch"
    rmdir -- "$scratch"
  fi
  exit "$status"
}

admin() {
  runuser -u postgres -- "$pgbin/psql" -X -qAt -v ON_ERROR_STOP=1 \
    -h /run/postgresql -U postgres -d "${1:-crowdsec}"
}

runtime() {
  "$pgbin/psql" -X -qAt -v ON_ERROR_STOP=1 \
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
    -o "-c listen_addresses='' -c unix_socket_directories=/run/postgresql -c shared_buffers=32MB -c max_connections=16 -c log_statement=none -c log_min_messages=panic -c log_min_error_statement=panic -c log_error_verbosity=terse -c log_parameter_max_length=0 -c log_parameter_max_length_on_error=0" \
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
  check policy-application gate apply-policy
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
}

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
