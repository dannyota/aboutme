#!/usr/bin/env bash
# Hosted PostgreSQL is real; host controls below are explicit boundary doubles.

retention_fixture_admin() {
  runuser -u postgres -- /usr/lib/postgresql/18/bin/psql -X -qAt -w \
    -v ON_ERROR_STOP=1 -h /run/postgresql -U postgres -d "${1:-crowdsec}" 2>/dev/null
}

retention_fixture_sql() {
  /usr/lib/postgresql/18/bin/psql -X -qAt -w -v ON_ERROR_STOP=1 \
    -h /run/postgresql -U crowdsec -d crowdsec 2>/dev/null
}

retention_fixture_candidate_no_jit() {
  local retention_time plan=$retention_fixture/candidate-plan.json
  # shellcheck source=deploy/vn/host/crowdsec-retention.sh
  source /usr/local/lib/aboutme/crowdsec-retention.sh || return 1
  retention_time=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ) || return 1
  install -m 0600 /dev/null "$plan" || return 1
  {
    printf 'SET jit = on;\n'
    retention_sql
    printf 'EXPLAIN (ANALYZE, FORMAT JSON)\n'
    retention_select_sql
  } | retention_psql >"$plan" || return 1
  python3 -I - "$plan" <<'PY' || return 1
import json,pathlib,sys
result=json.loads(pathlib.Path(sys.argv[1]).read_text())
assert len(result)==1 and isinstance(result[0]['Plan'],dict)
assert result[0].get('JIT',{}).get('Functions',0)==0
PY
  rm -- "$plan"
}

retention_fixture_cleanup() {
  local status=$?
  trap - EXIT
  if [[ -x ${retention_fixture:-}/control ]]; then
    "$retention_fixture/control" stop aboutme-caddy.service aboutme-maintenance.service \
      crowdsec-firewall-bouncer.service crowdsec.service >/dev/null 2>&1 || status=1
  fi
  if [[ ${retention_fixture_started:-no} == yes ]]; then
    timeout 15 runuser -u postgres -- /usr/lib/postgresql/18/bin/pg_ctl \
      -D "$retention_fixture/data" -m immediate -w stop >/dev/null 2>&1 || status=1
  fi
  if [[ -n ${retention_fixture_loop:-} ]]; then
    umount /var/lib/aboutme /srv/data || status=1
    losetup -d "$retention_fixture_loop" || status=1
  fi
  if [[ ${retention_fixture:-} == /run/crowdsec-retention-fixture.* ]]; then
    umount "$retention_fixture" || status=1
    rmdir -- "$retention_fixture" || status=1
  fi
  exit "$status"
}

retention_fixture_feed() {
  local active
  install -d -o 10001 -g 10001 -m 0750 /run/aboutme/caddy-log
  install -d -o 10001 -g 10001 -m 0700 /run/aboutme/caddy-log/crowdsec
  for active in serving maintenance serving-bouncer maintenance-bouncer; do
    install -o 10001 -g 10001 -m 0600 /dev/null "/run/aboutme/caddy-log/crowdsec/$active.json"
  done
}

retention_fixture_start() {
  [[ $EUID == 0 && ${GITHUB_ACTIONS:-} == true &&
    $(readlink /proc/self/ns/mnt) != "$(readlink /proc/1/ns/mnt)" ]] || return 1
  local root=$1 script
  retention_fixture=$(mktemp -d /run/crowdsec-retention-fixture.XXXXXX)
  mount -t tmpfs -o size=512m,mode=0755 tmpfs "$retention_fixture"
  trap retention_fixture_cleanup EXIT
  install -d -o postgres -g postgres -m 0700 "$retention_fixture/data"
  install -d -m 0755 "$retention_fixture/bin" "$retention_fixture/lib" "$retention_fixture/pgconfig"
  install -d -o root -g postgres -m 2770 "$retention_fixture/queries"
  mount -t tmpfs -o size=16m,mode=0755 tmpfs /run/postgresql
  chown postgres:postgres /run/postgresql
  runuser -u postgres -- /usr/lib/postgresql/18/bin/initdb -D "$retention_fixture/data" \
    --auth-local=peer --auth-host=reject --no-locale --encoding=UTF8 >/dev/null 2>&1
  install -d -m 0755 /etc/postgresql/18/main
  for script in pg_hba.conf pg_ident.conf; do
    install -m 0644 "$root/deploy/vn/host/etc/postgresql/18/main/$script" "$retention_fixture/pgconfig/$script"
  done
  mount --bind "$retention_fixture/pgconfig" /etc/postgresql/18/main
  cat >>"$retention_fixture/data/postgresql.conf" <<'CONFIG'
listen_addresses = ''
unix_socket_directories = '/run/postgresql'
shared_buffers = '32MB'
max_connections = 40
hba_file = '/etc/postgresql/18/main/pg_hba.conf'
ident_file = '/etc/postgresql/18/main/pg_ident.conf'
log_min_messages = 'panic'
log_min_error_statement = 'panic'
log_error_verbosity = 'terse'
log_statement = 'none'
log_parameter_max_length = 0
log_parameter_max_length_on_error = 0
CONFIG
  runuser -u postgres -- /usr/lib/postgresql/18/bin/pg_ctl -D "$retention_fixture/data" \
    -l "$retention_fixture/data/server.log" -w start >/dev/null 2>&1
  retention_fixture_started=yes
  retention_fixture_admin postgres >/dev/null <<'SQL'
CREATE ROLE aboutme_migrator LOGIN;
CREATE ROLE aboutme_app LOGIN;
CREATE DATABASE aboutme OWNER aboutme_migrator;
REVOKE ALL ON DATABASE aboutme FROM PUBLIC;
SQL
  retention_fixture_admin aboutme >/dev/null <<'SQL'
CREATE TABLE public.retention_sentinel (id integer PRIMARY KEY);
INSERT INTO public.retention_sentinel VALUES (1);
SQL
  install -d -m 0755 /srv/data /var/lib/aboutme /usr/local/lib/aboutme
  truncate -s 64M "$retention_fixture/fence-disk"
  mkfs.ext4 -q -L aboutme-data "$retention_fixture/fence-disk"
  retention_fixture_loop=$(losetup --find --show "$retention_fixture/fence-disk")
  mount "$retention_fixture_loop" /srv/data
  install -d -m 0755 /srv/data/aboutme
  install -d -m 0700 /srv/data/host-operations
  mount --bind /srv/data/aboutme /var/lib/aboutme
  mount -t tmpfs -o size=16m,mode=0755 tmpfs /run/aboutme
  install -d -m 0700 /run/aboutme/crowdsec
  mount -t tmpfs -o size=16m,mode=0755 tmpfs /var/lib/crowdsec
  install -d -m 0700 /var/lib/crowdsec/data
  retention_fixture_feed
  for script in crowdsec-db crowdsec-provision crowdsec-privacy-notice-host \
    crowdsec-retention crowdsec-retention-fail crowdsec-retention-lease-check crowdsec-vacuum; do
    install -m 0755 "$root/deploy/vn/host/$script.sh" "$retention_fixture/lib/$script.sh"
  done
  install -m 0755 "$root/deploy/vn/scripts/fence.sh" "$retention_fixture/lib/fence.sh"
  install -m 0644 "$root/deploy/vn/host/crowdsec-schema-v1.8.1.tsv" "$retention_fixture/lib/"
  printf '#!/usr/bin/env bash\nprintf "alarm\\n" >>%q/alarms\n' "$retention_fixture" >"$retention_fixture/lib/alert.sh"
  chmod 0755 "$retention_fixture/lib/alert.sh"
  mount --bind "$retention_fixture/lib" /usr/local/lib/aboutme
  cp -a /etc/crowdsec "$retention_fixture/crowdsec-config"
  mount --bind "$retention_fixture/crowdsec-config" /etc/crowdsec
  install -m 0644 "$root/deploy/vn/edge/crowdsec/config.yaml.local" /etc/crowdsec/config.yaml.postgresql
  install -m 0644 "$root/deploy/vn/edge/crowdsec/profiles.yaml" /etc/crowdsec/profiles.yaml
  install -m 0644 "$root/deploy/vn/edge/crowdsec/console.yaml" /etc/crowdsec/console.yaml
  printf '11111111111111111111111111111111\n' >"$retention_fixture/invocation"
  printf 'inactive\n' >"$retention_fixture/consumer-state"
  printf '#!/usr/bin/env bash\nset -euo pipefail\nfixture=%q\n' "$retention_fixture" >"$retention_fixture/bin/systemctl"
  cat >>"$retention_fixture/bin/systemctl" <<'SH'
printf '%s\n' "$*" >>"$fixture/control-log"
if [[ -x $fixture/control && $* != *postgresql@18-main.service* ]]; then
  exec "$fixture/control" "$@"
fi
case $1 in
  show)
    if [[ $* == *postgresql@18-main.service* ]]; then
      printf 'ActiveState=active\nJob=\nMainPID=%s\nInvocationID=%s\n' \
        "$(head -n1 "$fixture/data/postmaster.pid")" "$(cat "$fixture/invocation")"
    elif [[ $* == *--value* ]]; then
      if [[ -e $fixture/caddy-stopped && ( $* == *aboutme-caddy.service* || $* == *aboutme-maintenance.service* ) ]]; then
        printf 'inactive\n'
      else cat "$fixture/consumer-state"; fi
    elif [[ $* == *MainPID* ]]; then
      printf 'ActiveState=%s\nJob=\nMainPID=0\nControlPID=0\nControlGroup=\n' "$(cat "$fixture/consumer-state")"
    else
      printf 'ActiveState=%s\nJob=\n' "$(cat "$fixture/consumer-state")"
    fi ;;
  is-enabled)
    if [[ $* == *vacuum.timer* ]]; then [[ ! -e $fixture/vacuum-timer-disabled ]]
    else [[ ! -e $fixture/timer-disabled ]]; fi ;;
  is-active)
    if [[ $* == *retention.timer* ]]; then [[ ! -e $fixture/timer-inactive ]]
    elif [[ $* == *vacuum.timer* ]]; then [[ ! -e $fixture/vacuum-timer-inactive ]]
    else [[ $(cat "$fixture/consumer-state") == active ]]; fi ;;
  start) printf 'active\n' >"$fixture/consumer-state" ;;
  stop | kill) printf 'inactive\n' >"$fixture/consumer-state" ;;
  *) exit 1 ;;
esac
SH
  printf '#!/usr/bin/env bash\n[[ ! -e %q/clock-failed ]] && printf "yes\\n"\n' "$retention_fixture" >"$retention_fixture/bin/timedatectl"
  printf '#!/usr/bin/env bash\n[[ $* == "container exists "* ]] && exit 1\nexit 2\n' >"$retention_fixture/bin/podman"
  printf '#!/usr/bin/env bash\nfixture=%q\n' "$retention_fixture" >"$retention_fixture/bin/nft"
  cat >>"$retention_fixture/bin/nft" <<'SH'
#!/usr/bin/env bash
if [[ $* == '-j list tables' && -e $fixture/nft-query-failed ]]; then exit 1; fi
if [[ $* == '-j list tables' && -e $fixture/nft-malformed ]]; then printf '{'; exit 0; fi
if [[ -e $fixture/nft-unexpected ]]; then
  if [[ $* == '-j list tables' ]]; then
    printf '{"nftables":[{"table":{"family":"ip","name":"crowdsec"}}]}\n'; exit 0
  fi
  if [[ $* == '-j list table ip crowdsec' ]]; then
    cat <<'JSON'
{"nftables":[{"table":{"family":"ip","name":"crowdsec","flags":["dormant"]}},{"chain":{"family":"ip","table":"crowdsec","name":"crowdsec-chain","type":"filter","hook":"input","prio":-10,"policy":"accept"}},{"set":{"family":"ip","table":"crowdsec","name":"crowdsec-blacklists","type":"ipv4_addr","flags":["timeout"]}},{"rule":{"family":"ip","table":"crowdsec","chain":"crowdsec-chain","expr":[{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"saddr"}},"right":"@crowdsec-blacklists"}},{"drop":null}]}}]}
JSON
    exit 0
  fi
fi
if [[ $* == '-f -' ]]; then printf 'nft-init\n' >>"$fixture/control-log"; fi
if [[ $* == '-j list set '* ]]; then
  if [[ -e $fixture/nft-rebuild-mismatch ]]; then
    printf '{"nftables":[{"set":{"elem":["192.0.2.99"]}}]}\n'
  else printf '{"nftables":[{"set":{"elem":[]}}]}\n'; fi
else
  printf '{"nftables":[]}\n'
fi
SH
  chmod 0755 "$retention_fixture/bin/"*
  mount --bind "$retention_fixture/bin" /usr/local/bin
  mount --bind "$retention_fixture/bin/systemctl" /usr/bin/systemctl
  cp /usr/lib/postgresql/18/bin/psql "$retention_fixture/bin/psql-real"
  printf '#!/usr/bin/env bash\nset -euo pipefail\numask 077\nfixture=%q\n' "$retention_fixture" >"$retention_fixture/bin/psql"
  cat >>"$retention_fixture/bin/psql" <<'SH'
query=$(mktemp "$fixture/queries/query.XXXXXX")
trap 'rm -f "$query"' EXIT
cat >"$query"
if grep -q '^COPY selected ' "$query"; then
  printf 'sql-delete\n' >>"$fixture/control-log"
  awk '/^COPY selected / { copy=1; next } /^\\\.$/ { copy=0 } copy { count++ } END { print count+0 }' \
    "$query" >>"$fixture/batch-counts"
  if [[ -e $fixture/change-candidate ]]; then
    rm "$fixture/change-candidate"
    "$fixture/bin/psql-real" -X -qAt -w -v ON_ERROR_STOP=1 \
      -h /run/postgresql -U crowdsec -d crowdsec >/dev/null 2>&1 <<'SQL'
UPDATE public.alerts SET created_at=now(),updated_at=now(),started_at=now(),stopped_at=now() WHERE id=3000;
SQL
  fi
  if [[ -e $fixture/add-child-decision || -e $fixture/replace-child-decision ]]; then
    if [[ -e $fixture/replace-child-decision ]]; then
      "$fixture/bin/psql-real" -X -qAt -w -v ON_ERROR_STOP=1 \
        -h /run/postgresql -U crowdsec -d crowdsec >/dev/null 2>&1 <<'SQL'
DELETE FROM public.decisions WHERE id=9001;
SQL
    fi
    rm -f "$fixture/add-child-decision" "$fixture/replace-child-decision"
    "$fixture/bin/psql-real" -X -qAt -w -v ON_ERROR_STOP=1 \
      -h /run/postgresql -U crowdsec -d crowdsec >/dev/null 2>&1 <<'SQL'
INSERT INTO public.decisions (id,created_at,updated_at,until,scenario,type,scope,value,origin,alert_decisions)
VALUES (9002,now(),now(),now()+interval '1 hour','aboutme/http-status','ban','Ip','192.0.2.21','crowdsec',3000);
SQL
  fi
fi
status=0
"$fixture/bin/psql-real" "$@" <"$query" || status=$?
exit "$status"
SH
  chmod 0755 "$retention_fixture/bin/psql" "$retention_fixture/bin/psql-real"
  mount --bind "$retention_fixture/bin/psql" /usr/lib/postgresql/18/bin/psql
  export PATH=/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
  /usr/local/lib/aboutme/fence.sh init >/dev/null 2>&1
  retention_fixture_owner=$(/usr/local/lib/aboutme/fence.sh lock cutover v0.8.9 2>/dev/null)
  /usr/local/lib/aboutme/fence.sh mutation-start "$retention_fixture_owner" v0.8.9 reset-db >/dev/null 2>&1
  printf '{"operation_id":"%s","release_tag":"v0.8.9"}\n' "$retention_fixture_owner" |
    /usr/local/lib/aboutme/crowdsec-provision.sh fresh >/dev/null 2>&1
  /usr/local/lib/aboutme/crowdsec-db.sh preflight-ready </dev/null >/dev/null 2>&1
}

# Only service control is adapted to the namespace. Every consumer and the
# kernel nft backend below is real, with private output and bounded lifetime.
retention_fixture_consumers() {
  [[ -f ${CROWDSEC_TEST_CADDY_BINARY:-} && -x $CROWDSEC_TEST_CADDY_BINARY ]]
  install -m 0755 "$CROWDSEC_TEST_CADDY_BINARY" "$retention_fixture/bin/caddy"
  rm -- "$retention_fixture/bin/nft"
  ip link set lo up
  install -d -m 0700 "$retention_fixture/pids" "$retention_fixture/caddy-state"
  cat >"$retention_fixture/Caddyfile" <<EOF
{
  admin off
  auto_https off
  order crowdsec first
  log {
    output discard
  }
  crowdsec {
    api_url http://127.0.0.1:8095
    api_key {file.$retention_fixture/caddy.key}
    ticker_interval 10s
  }
}
:18080 {
  crowdsec
  respond "allowed" 200
}
EOF
  printf '#!/usr/bin/env bash\nset -euo pipefail\nfixture=%q\n' "$retention_fixture" >"$retention_fixture/control"
  cat >>"$retention_fixture/control" <<'SH'
[[ ${SYSTEMD_OFFLINE:-} != 1 ]] || exit 0
pid_for() {
  pid=0
  if [[ -f $fixture/pids/$unit ]]; then
    read -r pid <"$fixture/pids/$unit"
    [[ $pid =~ ^[1-9][0-9]*$ ]] || exit 1
    if [[ ! -e /proc/$pid/stat || $(awk '{print $3}' "/proc/$pid/stat") == Z ]]; then pid=0; fi
  fi
}
spawn() {
  (
    # A real system manager does not pass the caller's retention lock into a
    # service. Close inherited descriptors before starting each real binary.
    for entry in /proc/$BASHPID/fd/*; do
      descriptor=${entry##*/}
      if [[ $descriptor =~ ^[0-9]+$ ]] && ((descriptor>2)); then exec {descriptor}>&-; fi
    done
    exec "$@"
  ) >/dev/null 2>&1 &
}
verb=$1
shift
case $verb in
  is-enabled | is-active)
    [[ $* == *timer* ]] && exit 0
    unit=${!#}; pid_for; ((pid>0)) ;;
  show)
    unit=${!#}; pid_for
    state=inactive; ((pid==0)) || state=active
    if [[ $* == *--value* ]]; then printf '%s\n' "$state"
    elif [[ $* == *--property=MainPID* ]]; then
      printf 'ActiveState=%s\nJob=\nMainPID=%s\nControlPID=0\nControlGroup=\n' "$state" "$pid"
    else printf 'ActiveState=%s\nJob=\n' "$state"; fi ;;
  stop | kill)
    for unit in "$@"; do
      [[ $unit != --* ]] || continue
      pid_for
      if ((pid>0)); then
        kill -TERM "$pid" 2>/dev/null || true
        for _ in {1..20}; do pid_for; ((pid>0)) || break; sleep 0.1; done
        if ((pid>0)); then kill -KILL "$pid"; fi
      fi
      rm -f -- "$fixture/pids/$unit"
    done ;;
  start | restart)
    for unit in "$@"; do
      /usr/local/lib/aboutme/crowdsec-db.sh preflight-ready </dev/null >/dev/null 2>&1
      /usr/local/lib/aboutme/crowdsec-retention-lease-check.sh >/dev/null 2>&1
      case $unit in
        crowdsec.service) spawn /usr/bin/crowdsec -c /etc/crowdsec/config.yaml ;;
        crowdsec-firewall-bouncer.service)
          spawn /usr/bin/crowdsec-firewall-bouncer -c /etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml ;;
        aboutme-caddy.service)
          spawn env XDG_CONFIG_HOME="$fixture/caddy-state" XDG_DATA_HOME="$fixture/caddy-state" \
            "$fixture/bin/caddy" run --config "$fixture/Caddyfile" --adapter caddyfile ;;
        aboutme-maintenance.service) exit 1 ;;
        *) exit 1 ;;
      esac
      printf '%s\n' "$!" >"$fixture/pids/$unit"
      sleep 0.2
      pid_for; ((pid>0))
      if [[ $unit == crowdsec.service ]]; then
        for _ in {1..25}; do
          [[ -z $(ss -Hln 'sport = :8095') ]] || break
          sleep 0.2
        done
        [[ -n $(ss -Hln 'sport = :8095') ]]
      fi
    done ;;
  mask | unmask | disable | enable | daemon-reload) exit 0 ;;
  *) exit 1 ;;
esac
SH
  chmod 0755 "$retention_fixture/control"
}
