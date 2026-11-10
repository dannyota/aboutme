#!/usr/bin/env bash
# Hosted PostgreSQL is real; host controls below are explicit boundary doubles.

retention_fixture_diagnostics() {
  exec {retention_fixture_error_fd}>&2
  trap retention_fixture_report_failure EXIT
}

retention_fixture_report_failure() {
  local status=$?
  trap - EXIT
  if [[ -n ${retention_proxy_pid:-} ]]; then
    kill "$retention_proxy_pid" 2>/dev/null || true
    wait "$retention_proxy_pid" 2>/dev/null || true
  fi
  if ((status)); then
    printf 'FAIL %s stage=%s\n' "$retention_fixture_case" "${integration_stage:-fixture}" >&"$retention_fixture_error_fd"
  fi
  exit "$status"
}

retention_fixture_pid_for() {
  local pid_root=$1 unit=$2 process_stat
  pid=0
  if [[ -f $pid_root/pids/$unit ]]; then
    read -r pid <"$pid_root/pids/$unit"
    [[ $pid =~ ^[1-9][0-9]*$ ]] || return 1
    # A stopped process can disappear between its PID-file read and this read.
    if ! { IFS= read -r process_stat <"/proc/$pid/stat"; } 2>/dev/null; then
      [[ ! -e /proc/$pid ]] || return 1
      pid=0
    elif [[ ${process_stat##*) } == Z\ * ]]; then
      pid=0
    fi
  fi
}

retention_fixture_admin() {
  runuser -u postgres -- /usr/lib/postgresql/18/bin/psql -X -qAt -w \
    -v ON_ERROR_STOP=1 -v fixture_time="$(cat "$retention_fixture/clock")" \
    -h /run/postgresql -U postgres -d "${1:-crowdsec}" 2>/dev/null
}

retention_fixture_sql() {
  /usr/lib/postgresql/18/bin/psql -X -qAt -w -v ON_ERROR_STOP=1 \
    -v fixture_time="$(cat "$retention_fixture/clock")" \
    -h /run/postgresql -U crowdsec -d crowdsec "$@" 2>/dev/null
}

retention_fixture_time() {
  rm -f "$retention_fixture/clock" "$retention_fixture/verify-clock" "$retention_fixture/clock-read"
  printf '%s\n' "${1:-$retention_fixture_now}" >"$retention_fixture/clock"
  [[ -z ${2:-} ]] || printf '%s\n' "$2" >"$retention_fixture/verify-clock"
}

# Build outside the isolated network, against the same pinned engine as LAPI.
retention_fixture_profile_binary() {
  local go_binary=$1 proof=$retention_fixture/profile-proof
  mkdir -m 0700 "$proof"
  cat >"$proof/go.mod" <<'MOD'
module retention-profile-proof

go 1.27.3

require github.com/crowdsecurity/crowdsec v1.8.2
MOD
  cat >"$proof/main.go" <<'GO'
package main

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "os"
    "strings"
    "time"

    "entgo.io/ent/dialect"
    entsql "entgo.io/ent/dialect/sql"
    "github.com/crowdsecurity/crowdsec/pkg/csconfig"
    "github.com/crowdsecurity/crowdsec/pkg/csnet"
    "github.com/crowdsecurity/crowdsec/pkg/csprofiles"
    "github.com/crowdsecurity/crowdsec/pkg/database"
    "github.com/crowdsecurity/crowdsec/pkg/database/ent"
    "github.com/crowdsecurity/crowdsec/pkg/exprhelpers"
    "github.com/crowdsecurity/crowdsec/pkg/models"
    "github.com/expr-lang/expr"
    "gopkg.in/yaml.v3"
)

// Set fixture expiries relative to the real helper's bound evaluation time.
type clockDriver struct {
    dialect.Driver
    db *sql.DB
    parent int64
    offsets []time.Duration
    calls int
    fail bool
}
func (d *clockDriver) Query(ctx context.Context, query string, args, value any) error {
    if strings.Contains(query, "COUNT(") && strings.Contains(query, `"decisions"`) {
        d.calls++
        if d.fail { return errors.New("count_query_failed") }
        var at time.Time
        for _, arg := range args.([]any) { if bound, ok := arg.(time.Time); ok { at = bound } }
        if at.IsZero() { return errors.New("missing_active_cutoff") }
        // CrowdSec stores signed address bounds, not raw IPv4 integers.
        rng, err := csnet.NewRange("192.0.2.210")
        if err != nil { return err }
        _, err = d.db.ExecContext(ctx, "DELETE FROM decisions WHERE alert_decisions=$1", d.parent)
        if err != nil { return err }
        for _, offset := range d.offsets {
            _, err = d.db.ExecContext(ctx, `INSERT INTO decisions
              (created_at,updated_at,until,scenario,type,scope,value,origin,alert_decisions,
               start_ip,end_ip,start_suffix,end_suffix,ip_size)
              VALUES ($1,$1,$2,'aboutme/http-status','ban','Ip','192.0.2.210','crowdsec',$3,
                      $4,$5,$6,$7,$8)`, at.Add(-time.Hour), at.Add(offset), d.parent,
                      rng.Start.Addr, rng.End.Addr, rng.Start.Sfx, rng.End.Sfx, rng.Size())
            if err != nil { return err }
        }
    }
    return d.Driver.Query(ctx, query, args, value)
}

func must(err error) { if err != nil { panic("profile_proof_failed") } }
func main() {
    file, err := os.Open(os.Args[1]); must(err)
    var cfg csconfig.ProfileCfg
    must(yaml.NewDecoder(file).Decode(&cfg)); must(file.Close())
    profiles, err := csprofiles.NewProfile([]*csconfig.ProfileCfg{&cfg}); must(err)
    profile := profiles[0] // Compile the real registered helper before injecting a clock.
    ip, scope, scenario := "192.0.2.210", "Ip", "aboutme/http-status"
    alert := &models.Alert{Source: &models.Source{Value: &ip, Scope: &scope}, Scenario: &scenario}
    at := time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC)
    cases := []struct { name string; expiry []time.Time; want string }{
        {"zero", nil, "15m"},
        {"one", []time.Time{at.Add(time.Hour)}, "1h"},
        {"two", []time.Time{at.Add(time.Hour), at.Add(2*time.Hour)}, "4h"},
        {"before", []time.Time{at.Add(-time.Nanosecond)}, "15m"},
        {"equal", []time.Time{at}, "15m"},
        {"after", []time.Time{at.Add(time.Nanosecond)}, "1h"},
        {"revoked", []time.Time{at.Add(-time.Minute)}, "15m"},
        {"mixed", []time.Time{at.Add(-time.Hour), at.Add(time.Hour)}, "1h"},
    }
    evaluate := func(name string, until []time.Time, want string, queryError bool) {
        calls := 0
        clock := at
        helper := func(params ...any) (any, error) {
            calls++
            if params[0] != ip { return nil, errors.New("wrong_value") }
            if queryError { return nil, errors.New("count_query_failed") }
            count := 0
            for _, expiry := range until { if expiry.After(clock) { count++ } }
            clock = at.Add(24*time.Hour) // A second call would observe expiry.
            return count, nil
        }
        options := exprhelpers.GetExprOptions(map[string]any{"Alert": alert})
        options = append(options, expr.Function("GetActiveDecisionsCount", helper))
        profile.RuntimeDurationExpr, err = expr.Compile(cfg.DurationExpr, options...); must(err)
        decisions, err := profile.GenerateDecisionFromProfile(alert); must(err)
        if calls != 1 || len(decisions) != 1 || *decisions[0].Duration != want { panic(name) }
    }
    for _, test := range cases {
        evaluate(test.name, test.expiry, test.want, false)
        active := []time.Time{}
        for _, expiry := range test.expiry { if expiry.After(at) { active = append(active, expiry) } }
        evaluate(test.name+"-after-batch", active, test.want, false)
    }
    // Offenses at 08:00, 12:00 and 18:00 have no overlapping active ban.
    history := []time.Time{}
    for _, hour := range []int{8, 12, 18} {
        at = time.Date(2026, 1, 2, hour, 0, 0, 0, time.UTC)
        evaluate(fmt.Sprint(hour), history, "15m", false)
        history = append(history, at.Add(15*time.Minute))
    }
    evaluate("expiry-between-calls", []time.Time{at.Add(time.Nanosecond)}, "1h", false)
    evaluate("query-error-fallback", nil, "15m", true)

    db, err := sql.Open("pgx", "host=/run/postgresql user=crowdsec dbname=crowdsec sslmode=disable"); must(err)
    defer db.Close()
    db.SetMaxOpenConns(2)
    var parent int64
    must(db.QueryRow(`INSERT INTO alerts (created_at,updated_at,started_at,stopped_at,scenario)
      VALUES (now(),now(),now(),now(),'aboutme/http-status') RETURNING id`).Scan(&parent))
    driver := &clockDriver{Driver: entsql.OpenDB(dialect.Postgres, db), db: db, parent: parent}
    must(exprhelpers.Init(&database.Client{Ent: ent.NewClient(ent.Driver(driver))}))
    driver.offsets = []time.Duration{time.Hour}
    count, err := exprhelpers.GetActiveDecisionsCount(ip); must(err)
    if driver.calls != 1 || count != 1 { panic("active_seed_contract") }
    // Use the actual registered helper, its query predicate and PostgreSQL.
    for _, test := range []struct { offsets []time.Duration; want string; fail bool }{
        {nil, "15m", false}, {[]time.Duration{time.Hour}, "1h", false},
        {[]time.Duration{time.Hour,time.Hour}, "4h", false},
        {[]time.Duration{-time.Microsecond}, "15m", false},
        {[]time.Duration{0}, "15m", false},
        {[]time.Duration{time.Microsecond}, "1h", false},
        {[]time.Duration{-time.Minute,time.Hour}, "1h", false},
        {nil, "15m", true},
    } {
        driver.calls, driver.offsets, driver.fail = 0, test.offsets, test.fail
        profile.RuntimeDurationExpr, err = expr.Compile(cfg.DurationExpr,
            exprhelpers.GetExprOptions(map[string]any{"Alert": alert})...); must(err)
        decisions, err := profile.GenerateDecisionFromProfile(alert); must(err)
        if driver.calls != 1 || len(decisions) != 1 || *decisions[0].Duration != test.want {
            panic("active_query_contract")
        }
    }
    _, err = db.Exec("DELETE FROM alerts WHERE id=$1", parent); must(err)
    fmt.Println("PASS pinned-profile-single-count-expiry-and-query-error")
}
GO
  (cd "$proof" && timeout 300 env GOWORK=off "$go_binary" build -mod=mod -o "$retention_fixture/bin/profile-proof" .) \
    >"$proof/build.log" 2>&1
}

retention_fixture_candidate_no_jit() {
  local retention_time retention_batch=no plan=$retention_fixture/candidate-plan.json
  # shellcheck source=deploy/vn/host/crowdsec-retention.sh
  source /usr/local/lib/aboutme/crowdsec-retention.sh || return 1
  retention_time=$(cat "$retention_fixture/clock") || return 1
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
  retention_fixture_case=fixture-namespace-admission
  [[ $EUID == 0 && ${GITHUB_ACTIONS:-} == true &&
    $(readlink /proc/self/ns/mnt) != "$(readlink /proc/1/ns/mnt)" ]] || return 1
  local root=$1 script
  retention_fixture_case=fixture-runtime-directory
  retention_fixture=$(mktemp -d /run/crowdsec-retention-fixture.XXXXXX)
  mount -t tmpfs -o size=512m,mode=0755 tmpfs "$retention_fixture"
  trap retention_fixture_cleanup EXIT
  retention_fixture_now=${retention_fixture_now:-2026-01-02T19:00:00Z}
  retention_fixture_time "$retention_fixture_now"
  install -d -o postgres -g postgres -m 0700 "$retention_fixture/data"
  install -d -m 0755 "$retention_fixture/bin" "$retention_fixture/lib" "$retention_fixture/pgconfig"
  install -d -o root -g postgres -m 2770 "$retention_fixture/queries"
  mount -t tmpfs -o size=16m,mode=0755 tmpfs /run/postgresql
  chown postgres:postgres /run/postgresql
  retention_fixture_case=fixture-postgresql-initialization
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
  retention_fixture_case=fixture-postgresql-start
  runuser -u postgres -- /usr/lib/postgresql/18/bin/pg_ctl -D "$retention_fixture/data" \
    -l "$retention_fixture/data/server.log" -w start >/dev/null 2>&1
  retention_fixture_started=yes
  retention_fixture_case=fixture-application-sentinel
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
  retention_fixture_case=fixture-data-volume
  install -d -m 0755 /srv/data /var/lib/aboutme /usr/local/lib/aboutme
  truncate -s 64M "$retention_fixture/fence-disk"
  mkfs.ext4 -q -L aboutme-data "$retention_fixture/fence-disk"
  retention_fixture_loop=$(losetup --find --show "$retention_fixture/fence-disk")
  mount "$retention_fixture_loop" /srv/data
  install -d -m 0755 /srv/data/aboutme
  install -d -m 0700 /srv/data/host-operations
  install -m 0600 /dev/null /srv/data/host-operations/.bundle.lock
  mount --bind /srv/data/aboutme /var/lib/aboutme
  mount -t tmpfs -o size=16m,mode=0755 tmpfs /run/aboutme
  install -d -m 0700 /run/aboutme/crowdsec
  mount -t tmpfs -o size=16m,mode=0755 tmpfs /var/lib/crowdsec
  install -d -m 0700 /var/lib/crowdsec/data
  retention_fixture_feed
  retention_fixture_case=fixture-installed-helpers
  for script in crowdsec-db crowdsec-provision crowdsec-privacy-notice-host \
    crowdsec-retention crowdsec-retention-fail crowdsec-retention-lease-check crowdsec-vacuum; do
    install -m 0755 "$root/deploy/vn/host/$script.sh" "$retention_fixture/lib/$script.sh"
  done
  install -m 0755 "$root/deploy/vn/scripts/fence.sh" "$retention_fixture/lib/fence.sh"
  install -m 0644 "$root/deploy/vn/host/crowdsec-schema-v1.8.2.tsv" "$retention_fixture/lib/"
  printf '#!/usr/bin/env bash\nprintf "alarm\\n" >>%q/alarms\n' "$retention_fixture" >"$retention_fixture/lib/alert.sh"
  chmod 0755 "$retention_fixture/lib/alert.sh"
  mount --bind "$retention_fixture/lib" /usr/local/lib/aboutme
  cp -a /etc/crowdsec "$retention_fixture/crowdsec-config"
  mount --bind "$retention_fixture/crowdsec-config" /etc/crowdsec
  install -m 0644 "$root/deploy/vn/edge/crowdsec/config.yaml.local" /etc/crowdsec/config.yaml.postgresql
  install -m 0644 "$root/deploy/vn/edge/crowdsec/profiles.yaml" /etc/crowdsec/profiles.yaml
  install -m 0644 "$root/deploy/vn/edge/crowdsec/console.yaml" /etc/crowdsec/console.yaml
  retention_fixture_case=fixture-host-controls
  printf '11111111111111111111111111111111\n' >"$retention_fixture/invocation"
  printf 'inactive\n' >"$retention_fixture/consumer-state"
  printf '#!/usr/bin/env bash\nset -euo pipefail\nfixture=%q\n' "$retention_fixture" >"$retention_fixture/bin/systemctl"
  cat >>"$retention_fixture/bin/systemctl" <<'SH'
printf '%s\n' "$*" >>"$fixture/control-log"
if [[ $* == '--no-block start aboutme-alert@aboutme-crowdsec-retention.service' ]]; then
  printf 'alarm\n' >>"$fixture/alarms"
  exit 0
fi
if [[ $* == '--no-block start aboutme-alert@crowdsec_retention_overdue.service' ]]; then
  printf 'crowdsec_retention_overdue\n' >>"$fixture/alarms"
  exit 0
fi
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
  printf '#!/usr/bin/env bash\nfixture=%q\n' "$retention_fixture" >"$retention_fixture/bin/date"
  cat >>"$retention_fixture/bin/date" <<'SH'
if [[ $* == '-u +%Y-%m-%dT%H:%M:%S.%NZ' && -e $fixture/clock ]]; then
  if [[ -e $fixture/clock-read && -e $fixture/verify-clock ]]; then cat "$fixture/verify-clock"
  else cat "$fixture/clock"; fi
  touch "$fixture/clock-read"
else exec /usr/bin/date "$@"; fi
SH
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
  retention_fixture_case=fixture-query-adapter
  cp /usr/lib/postgresql/18/bin/psql "$retention_fixture/bin/psql-real"
  printf '#!/usr/bin/env bash\nset -euo pipefail\numask 077\nfixture=%q\n' "$retention_fixture" >"$retention_fixture/bin/psql"
  cat >>"$retention_fixture/bin/psql" <<'SH'
query=$(mktemp "$fixture/queries/query.XXXXXX")
trap 'rm -f "$query"' EXIT
cat >"$query"
insert_overdue() {
  "$fixture/bin/psql-real" -X -qAt -w -v ON_ERROR_STOP=1 \
    -v with_decision="${1:-yes}" -v fixture_time="$(cat "$fixture/clock")" \
    -h /run/postgresql -U crowdsec -d crowdsec >/dev/null 2>&1 <<'SQL'
INSERT INTO public.alerts (id,created_at,updated_at,started_at,stopped_at,scenario)
VALUES (5101,:'fixture_time'::timestamptz-interval '26 hours',:'fixture_time',:'fixture_time',:'fixture_time','aboutme/http-status');
\if :with_decision
INSERT INTO public.decisions (created_at,updated_at,until,scenario,type,scope,value,origin,alert_decisions)
VALUES (:'fixture_time',:'fixture_time',:'fixture_time'::timestamptz+interval '1 hour','aboutme/http-status','ban','Ip','192.0.2.23','crowdsec',5101);
\endif
SQL
}
if [[ -e $fixture/insert-overdue-before-selection ]] && grep -q 'ORDER BY kind,id LIMIT 1000' "$query"; then
  with_decision=$(cat "$fixture/insert-overdue-before-selection")
  rm "$fixture/insert-overdue-before-selection"
  insert_overdue "$with_decision"
fi
if grep -q '^COPY selected ' "$query"; then
  printf 'sql-delete\n' >>"$fixture/control-log"
  awk '/^COPY selected / { copy=1; next } /^\\\.$/ { copy=0 } copy { count++ } END { print count+0 }' \
    "$query" >>"$fixture/batch-counts"
  if [[ -e $fixture/change-candidate ]]; then
    rm "$fixture/change-candidate"
    "$fixture/bin/psql-real" -X -qAt -w -v ON_ERROR_STOP=1 \
      -v fixture_time="$(cat "$fixture/clock")" -h /run/postgresql -U crowdsec -d crowdsec >/dev/null 2>&1 <<'SQL'
UPDATE public.alerts SET created_at=:'fixture_time',updated_at=:'fixture_time',started_at=:'fixture_time',stopped_at=:'fixture_time' WHERE id=3000;
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
      -v fixture_time="$(cat "$fixture/clock")" -h /run/postgresql -U crowdsec -d crowdsec >/dev/null 2>&1 <<'SQL'
INSERT INTO public.decisions (id,created_at,updated_at,until,scenario,type,scope,value,origin,alert_decisions)
VALUES (9002,:'fixture_time',:'fixture_time',:'fixture_time'::timestamptz+interval '1 hour','aboutme/http-status','ban','Ip','192.0.2.21','crowdsec',3000);
SQL
  fi
fi
status=0
result=''
if grep -q '^COPY selected ' "$query"; then
  "$fixture/bin/psql-real" "$@" <"$query" >"$query.out" || status=$?
  result=$(cat "$query.out")
  cat "$query.out"
else
  "$fixture/bin/psql-real" "$@" <"$query" || status=$?
fi
if [[ $status == 0 && -e $fixture/insert-overdue-after-selection ]] && grep -q 'ORDER BY kind,id LIMIT 1000' "$query"; then
  rm "$fixture/insert-overdue-after-selection"
  insert_overdue
fi
if [[ $result == retry && -e $fixture/paused-ingestion ]]; then
  touch "$fixture/ingestion-release"
  timeout 5 bash -c 'until [[ -e $1/ingestion-done ]]; do sleep 0.05; done' proof "$fixture"
fi
if [[ $result == deleted && -e $fixture/stream-held ]]; then touch "$fixture/stream-release"; fi
rm -f "$query.out"
exit "$status"
SH
  chmod 0755 "$retention_fixture/bin/psql" "$retention_fixture/bin/psql-real"
  mount --bind "$retention_fixture/bin/psql" /usr/lib/postgresql/18/bin/psql
  export PATH=/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
  retention_fixture_case=fixture-fence-initialization
  /usr/local/lib/aboutme/fence.sh init >/dev/null 2>&1
  if [[ ${retention_fixture_migration:-no} != yes ]]; then
    retention_fixture_case=fixture-fence-ownership
    retention_fixture_owner=$(/usr/local/lib/aboutme/fence.sh lock cutover v0.8.9 2>/dev/null)
    retention_fixture_case=fixture-fence-mutation
    /usr/local/lib/aboutme/fence.sh mutation-start "$retention_fixture_owner" v0.8.9 reset-db >/dev/null 2>&1
  fi
  retention_fixture_case=fixture-postgresql-provision
  printf '{"operation_id":"%s","release_tag":"v0.8.9"}\n' "${retention_fixture_owner:-}" |
    /usr/local/lib/aboutme/crowdsec-provision.sh fresh >/dev/null 2>&1
  # shellcheck disable=SC2034 # The caller's ERR trap reads this fixed case name.
  retention_fixture_case=fixture-postgresql-ready
  /usr/local/lib/aboutme/crowdsec-db.sh preflight-ready </dev/null >/dev/null 2>&1
}

retention_fixture_stream_proxy() {
  cat >"$retention_fixture/stream-proxy.py" <<'PY'
import http.server,pathlib,sys,threading,time,urllib.request
root=pathlib.Path(sys.argv[1])
guard=threading.Lock()
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self,*args): pass
    def do_GET(self):
        request=urllib.request.Request('http://127.0.0.1:8095'+self.path,
                                       headers={'X-Api-Key':self.headers['X-Api-Key']})
        with opener.open(request,timeout=15) as response:
            data=response.read(4*1024*1024+1)
            assert len(data)<=4*1024*1024
        hold=False
        with guard:
            if (root/'stream-hold').exists() and b'192.0.2.52' in data:
                (root/'stream-hold').unlink()
                (root/'stream-held').touch()
                hold=True
        if hold:
            deadline=time.monotonic()+65
            while not (root/'stream-release').exists():
                assert time.monotonic()<deadline
                time.sleep(0.05)
        try:
            self.send_response(200)
            self.send_header('Content-Type','application/json')
            self.send_header('Content-Length',str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        except (BrokenPipeError,ConnectionResetError): pass
        if hold: (root/'stream-released').touch()
server=http.server.ThreadingHTTPServer(('127.0.0.1',18095),Handler)
(root/'proxy-ready').touch()
server.serve_forever()
PY
  python3 -I "$retention_fixture/stream-proxy.py" "$retention_fixture" >/dev/null 2>&1 &
  retention_proxy_pid=$!
  for _ in {1..50}; do [[ ! -e $retention_fixture/proxy-ready ]] || return 0; sleep 0.1; done
  return 1
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
  declare -f retention_fixture_pid_for >>"$retention_fixture/control"
  cat >>"$retention_fixture/control" <<'SH'
[[ ${SYSTEMD_OFFLINE:-} != 1 ]] || exit 0
pid_for() { retention_fixture_pid_for "$fixture" "$unit"; }
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
  list-units) exit 0 ;;
  is-enabled | is-active)
    if [[ -e $fixture/migration-controls ]]; then
      unit=${!#}
      if [[ $verb == is-enabled ]]; then [[ -e $fixture/enabled/$unit ]]; exit; fi
      if [[ $unit == *.timer ]]; then [[ -e $fixture/active/$unit ]]; exit; fi
    fi
    [[ $* == *timer* ]] && exit 0
    unit=${!#}; pid_for; ((pid>0)) ;;
  show)
    unit=${!#}; pid_for
    state=inactive; ((pid==0)) || state=active
    if [[ $unit == *.timer && -e $fixture/active/$unit ]]; then state=active; fi
    if [[ $* == *--value* ]]; then printf '%s\n' "$state"
    elif [[ $* == *--property=MainPID* ]]; then
      printf 'ActiveState=%s\nJob=\nMainPID=%s\nControlPID=0\nControlGroup=\n' "$state" "$pid"
    else printf 'ActiveState=%s\nJob=\n' "$state"; fi ;;
  stop | kill)
    for unit in "$@"; do
      [[ $unit != --* ]] || continue
      pid_for
      if ((pid>0)); then
        if ! kill -TERM "$pid" 2>/dev/null; then
          pid_for
          ((pid==0)) || exit 1
        fi
        for _ in {1..20}; do pid_for; ((pid>0)) || break; sleep 0.1; done
        if ((pid>0)); then kill -KILL "$pid"; fi
      fi
      rm -f -- "$fixture/pids/$unit" "$fixture/active/$unit"
    done ;;
  start | restart)
    for unit in "$@"; do
      if [[ $unit == aboutme-crowdsec-retention-startup.service ]]; then
        /usr/local/lib/aboutme/crowdsec-retention.sh startup >/dev/null 2>&1
        continue
      fi
      if [[ ! -e $fixture/sqlite-consumers ]]; then
        /usr/local/lib/aboutme/crowdsec-db.sh preflight-ready </dev/null >/dev/null 2>&1
        /usr/local/lib/aboutme/crowdsec-retention-lease-check.sh >/dev/null 2>&1
      fi
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
  enable)
    [[ -e $fixture/migration-controls ]] || exit 0
    for unit in "$@"; do
      [[ $unit != --* ]] || continue
      touch "$fixture/enabled/$unit"
      if [[ $* == *--now* ]]; then touch "$fixture/active/$unit"; fi
    done ;;
  mask | unmask | disable | daemon-reload) exit 0 ;;
  *) exit 1 ;;
esac
SH
  chmod 0755 "$retention_fixture/control"
}
