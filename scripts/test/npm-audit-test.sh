#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
SCRIPT=$ROOT/scripts/npm-audit.sh

fail() {
  printf 'npm-audit-test: %s\n' "$*" >&2
  exit 1
}

WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-npm-audit-test.XXXXXX")
trap '[ ! -f "$WORK/child.pids" ] || xargs kill -9 <"$WORK/child.pids" 2>/dev/null || true; rm -rf -- "$WORK"' EXIT
mkdir -p "$WORK/pkg"
printf '{"name":"@test/pkg","private":true}\n' >"$WORK/pkg/package.json"

cat >"$WORK/pnpm" <<STUB
#!/usr/bin/env bash
printf '%s\\n' "\$*" >"$WORK/args"
n=\$(( \$(cat "$WORK/calls" 2>/dev/null || echo 0) + 1 ))
echo "\$n" >"$WORK/calls"
if [ "\$n" -le "\$(cat "$WORK/hangs" 2>/dev/null || echo 0)" ]; then
  if [ -e "$WORK/child" ]; then
    (trap "" TERM; exec sleep 30 2>/dev/null) &
    echo "\$!" >>"$WORK/child.pids"
    wait
  else
    trap "" TERM
    sleep 30
  fi
fi
cat "$WORK/report.json"
exit 1
STUB
chmod +x "$WORK/pnpm"

advisory() {
  printf '"%s":{"id":%s,"github_advisory_id":"%s","module_name":"pkg","severity":"%s","title":"t",' "$3" "$3" "$2" "$1"
  printf '"url":"https://github.com/advisories/%s","findings":[{"version":"1.0.0","paths":["pkg>dep>pkg"]}]}' "$2"
}

summary='"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":0,"critical":0},"dependencies":2}'

report() {
  printf '{"advisories":{%s},%s}\n' "$(advisory "$1" "$2" 1)" "$summary" >"$WORK/report.json"
}

exceptions() { printf '%s\n' "$1" >"$WORK/ex.json"; }

entry() {
  printf '[{"id":"%s","dirs":[%s],"expires":"%s","reason":"r"}]' "$1" "$2" "$3"
}

ID=GHSA-aaaa-bbbb-cccc
OTHER=GHSA-dddd-eeee-ffff
LISTED=$(entry "$ID" '"pkg"' 2026-11-01)
TODAY=2026-10-02
EXFILE=
AUDIT_TIMEOUT=10

run() {
  local name=$1 want=$2 needle=${3:-} rc=0 err
  rm -f "$WORK/calls"
  err=$(NPM_AUDIT_PNPM="$WORK/pnpm" NPM_AUDIT_EXCEPTIONS="${EXFILE:-$WORK/ex.json}" \
    NPM_AUDIT_ROOT="$WORK" NPM_AUDIT_TODAY=$TODAY NPM_AUDIT_TIMEOUT=$AUDIT_TIMEOUT \
    bash "$SCRIPT" "$WORK/pkg" 2>&1 >/dev/null) || rc=$?
  [ "$rc" = "$want" ] || fail "$name: rc $rc, want $want: $err"
  if [ -n "$needle" ]; then
    case $err in *"$needle"*) ;; *) fail "$name: stderr lacks '$needle': $err" ;; esac
  fi
  printf 'ok %s\n' "$name"
}

printf '{"advisories":{},%s}\n' "$summary" >"$WORK/report.json"
exceptions "$LISTED"
run 'clean report passes' 0
[ "$(cat "$WORK/args")" = 'audit --prod --json --filter @test/pkg' ] ||
  fail "pnpm audit arguments: $(cat "$WORK/args")"

report high "$ID"
exceptions '[]'
run 'unlisted high fails' 1 "$ID"

exceptions "$LISTED"
run 'listed high passes' 0

TODAY=2026-11-01
run 'exception passes on its expiry date' 0
TODAY=2026-11-02
run 'expired listed exception fails' 1 'expired'
TODAY=2026-10-02

report critical "$ID"
exceptions '[]'
run 'unlisted critical fails' 1 "$ID"

report critical "$OTHER"
exceptions "$LISTED"
run 'exception for another id does not match' 1 "$OTHER"

report moderate "$ID"
exceptions '[]'
run 'moderate is ignored' 0

report high "$ID"
exceptions '[{"id":"GHSA-aaaa-bbbb-cccc","dirs":["pkg"],"reason":"r"}]'
run 'entry without expires fails' 1 malformed

exceptions "$(entry "$ID" '"pkg"' 2026-11-01 | sed 's/"dirs":\["pkg"\],//')"
run 'entry without dirs fails' 1 malformed

exceptions "$(entry "$ID" '' 2026-11-01)"
run 'empty dirs fails' 1 malformed

exceptions "$(entry "$ID" '"other"' 2026-11-01)"
run 'exception for another directory fails' 1 "$ID"

exceptions "$(entry "$ID" '"pkg"' 2026-12-31)"
run 'expiry on the 90 day limit passes' 0
exceptions "$(entry "$ID" '"pkg"' 2027-01-01)"
run 'expiry beyond 90 days fails' 1 '90 days'

printf '{"advisories":{%s,%s},%s}\n' "$(advisory high "$ID" 1)" "$(advisory high "$OTHER" 2)" \
  "$summary" >"$WORK/report.json"
exceptions "$LISTED"
run 'one of two advisories excepted still fails' 1 "$OTHER"

cat >"$WORK/report.json" <<'JSON'
{"advisories":{"1117000":{"findings":[{"version":"4.6.5","paths":["apps__web>@nuxt/test-utils>zod"],
"dev":true,"optional":false,"bundled":false}],"id":1117000,"title":"Zod denial of service",
"module_name":"zod","vulnerable_versions":"<4.6.8","patched_versions":">=4.6.8","severity":"high",
"github_advisory_id":"GHSA-mrhx-c7xc-m3w5","url":"https://github.com/advisories/GHSA-mrhx-c7xc-m3w5"}},
"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":1,"critical":0},"dependencies":1}}
JSON
exceptions "$LISTED"
run 'a pnpm high advisory without an exception fails' 1 'GHSA-mrhx-c7xc-m3w5'
exceptions "$(entry GHSA-mrhx-c7xc-m3w5 '"pkg"' 2026-11-01)"
run 'the same advisory with an exception passes' 0

printf '{"advisories":{%s},%s}\n' \
  "$(advisory high "$ID" 1 | sed 's/"github_advisory_id":"[^"]*",//')" "$summary" >"$WORK/report.json"
exceptions '[]'
run 'an advisory without github_advisory_id falls back to its URL' 1 "$ID"

report high "$ID"
exceptions '["GHSA-aaaa-bbbb-cccc"]'
run 'non-object entry fails with a message' 1 'array of objects'

printf '{not json' >"$WORK/ex.json"
run 'malformed exceptions JSON fails with a message' 1 'array of objects'

EXFILE=$WORK/absent.json
run 'missing exceptions file fails' 1 'missing exceptions file'
EXFILE=

exceptions "$LISTED"
echo '{"error":{"code":"ERR_PNPM_AUDIT_NO_LOCKFILE","message":"no lockfile"}}' >"$WORK/report.json"
run 'missing lockfile fails' 1 'no usable report'

: >"$WORK/report.json"
run 'empty pnpm output fails' 1 'no usable report'

printf '{"advisories":{},%s}\n' "$summary" >"$WORK/report.json"
AUDIT_TIMEOUT=1
echo 1 >"$WORK/hangs"
run 'hung audit is retried once' 0
[ "$(cat "$WORK/calls")" = 2 ] || fail 'hung audit: want 2 pnpm calls'
echo 2 >"$WORK/hangs"
start=$SECONDS
run 'audit that hangs twice fails' 1 'no usable report'
[ $((SECONDS - start)) -lt 15 ] || fail 'hung audit was not stopped by the time limit'
touch "$WORK/child"
start=$SECONDS
run 'leftover child holding the output does not block' 1 'no usable report'
[ $((SECONDS - start)) -lt 10 ] || fail 'a leftover child blocked the audit'
rm -f "$WORK/child"
rm -f "$WORK/hangs"
AUDIT_TIMEOUT=10

echo 'npm-audit-test: ok'
