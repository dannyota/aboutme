#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
watch=$repo_root/deploy/vn/host/bin/watch.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/lib"

cat >"$work/lib/alert.sh" <<'STUB'
#!/usr/bin/env bash
printf '%s|%s\n' "$1" "$2" >>"$WATCH_TEST_ALERTS"
cat >/dev/null
STUB
cat >"$work/bin/df" <<'STUB'
#!/usr/bin/env bash
printf 'Use%%\n10%%\n'
STUB
cat >"$work/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
cat >"$work/bin/runuser" <<'STUB'
#!/usr/bin/env bash
case " $* " in
  *' pg_isready '*) exit 0 ;;
  *' psql '*) printf 'f\n' ;;
  *' pgbackrest '*) printf '[{"backup":[{"timestamp":{"stop":2000000000}}]}]\n' ;;
  *) exit 2 ;;
esac
STUB
cat >"$work/bin/date" <<'STUB'
#!/usr/bin/env bash
[[ ${1-} == +%s ]] && printf '2000000000\n'
STUB
cat >"$work/bin/cscli" <<'STUB'
#!/usr/bin/env bash
printf '[]\n'
STUB
cat >"$work/bin/find" <<'STUB'
#!/usr/bin/env bash
if [[ $* == *serving-match.log* ]]; then
  printf '2000000000 %s\n' "$WATCH_TEST_FIXTURE"
fi
STUB
cat >"$work/bin/journalctl" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
chmod +x "$work/bin/"* "$work/lib/alert.sh"
export PATH=$work/bin:$PATH ABOUTME_WATCH_TEST_LIB=$work/lib
export CORAZA_MATCHES_PER_HOUR=0

run_watch() { # fixture alerts
  export WATCH_TEST_FIXTURE=$1 WATCH_TEST_ALERTS=$2
  : >"$WATCH_TEST_ALERTS"
  "$watch"
  sort -o "$WATCH_TEST_ALERTS" "$WATCH_TEST_ALERTS"
}

base=$work/base.log
with_diagnostic=$work/with-diagnostic.log
cat >"$base" <<'EOF'
{"ts":1999999999,"level":"warn","logger":"http.handlers.waf","msg":"waf_rule_match","rule_id":1001}
{"ts":1999999999,"level":"warn","logger":"http.handlers.waf","msg":"waf_rule_match_unparsed"}
{"ts":1999999999,"level":"warn","logger":"http.handlers.waf","msg":"unexpected"}
EOF
{
  cat "$base"
  printf '%s\n' '{"ts":1999999999,"level":"warn","logger":"http.handlers.waf","msg":"waf_engine_diagnostic"}'
} >"$with_diagnostic"

run_watch "$base" "$work/base.alerts"
run_watch "$with_diagnostic" "$work/diagnostic.alerts"
cmp "$work/base.alerts" "$work/diagnostic.alerts"
grep -qxF 'coraza-matches|Coraza matched 1 requests in the last hour' "$work/diagnostic.alerts"
grep -qxF 'coraza-unparsed|Coraza wrote 1 unparsed match records in the last hour' "$work/diagnostic.alerts"
grep -qxF 'coraza-log-invalid|the Coraza match log has 1 unknown records in the last hour' "$work/diagnostic.alerts"
[[ $(wc -l <"$work/diagnostic.alerts") == 3 ]] || {
  echo "the watcher raised an unexpected alert" >&2
  exit 1
}

echo "watch tests passed"
