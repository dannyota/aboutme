#!/usr/bin/env bash
# Offline tests for scripts/npm-audit.sh. A stub npm prints a canned report.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
SCRIPT=$ROOT/scripts/npm-audit.sh

fail() {
  printf 'npm-audit-test: %s\n' "$*" >&2
  exit 1
}

WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-npm-audit-test.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT
mkdir -p "$WORK/pkg"

cat >"$WORK/npm" <<STUB
#!/usr/bin/env bash
cat "$WORK/report.json"
exit 1
STUB
chmod +x "$WORK/npm"

# report SEVERITY ID: pkg carries one advisory and dep depends on pkg.
report() {
  cat >"$WORK/report.json" <<JSON
{"auditReportVersion":2,"vulnerabilities":{
 "pkg":{"name":"pkg","severity":"$1","via":[{"source":1,"name":"pkg","title":"t","url":"https://github.com/advisories/$2","severity":"$1"}]},
 "dep":{"name":"dep","severity":"$1","via":["pkg"]}}}
JSON
}

exceptions() { printf '%s\n' "$1" >"$WORK/ex.json"; }

# entry ID DIR EXPIRES: one exception entry.
entry() {
  printf '[{"id":"%s","dirs":[%s],"expires":"%s","reason":"r"}]' "$1" "$2" "$3"
}

ID=GHSA-aaaa-bbbb-cccc
OTHER=GHSA-dddd-eeee-ffff
LISTED=$(entry "$ID" '"pkg"' 2026-11-01)
TODAY=2026-10-02
EXFILE=

# run NAME EXPECTED_RC [stderr substring]. The audited directory is
# $WORK/pkg and the repository root is $WORK, so exceptions name "pkg".
run() {
  local name=$1 want=$2 needle=${3:-} rc=0 err
  err=$(NPM_AUDIT_NPM="$WORK/npm" NPM_AUDIT_EXCEPTIONS="${EXFILE:-$WORK/ex.json}" \
    NPM_AUDIT_ROOT="$WORK" NPM_AUDIT_TODAY=$TODAY bash "$SCRIPT" "$WORK/pkg" 2>&1 >/dev/null) || rc=$?
  [ "$rc" = "$want" ] || fail "$name: rc $rc, want $want: $err"
  if [ -n "$needle" ]; then
    case $err in *"$needle"*) ;; *) fail "$name: stderr lacks '$needle': $err" ;; esac
  fi
  printf 'ok %s\n' "$name"
}

echo '{"auditReportVersion":2,"vulnerabilities":{}}' >"$WORK/report.json"
exceptions "$LISTED"
run 'clean report passes' 0

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

cat >"$WORK/report.json" <<JSON
{"auditReportVersion":2,"vulnerabilities":{
 "pkg":{"name":"pkg","severity":"high","via":[
  {"source":1,"name":"pkg","title":"a","url":"https://github.com/advisories/$ID","severity":"high"},
  {"source":2,"name":"pkg","title":"b","url":"https://github.com/advisories/$OTHER","severity":"high"}]}}}
JSON
exceptions "$LISTED"
run 'one of two advisories excepted still fails' 1 "$OTHER"

report high "$ID"
exceptions '["GHSA-aaaa-bbbb-cccc"]'
run 'non-object entry fails with a message' 1 'array of objects'

printf '{not json' >"$WORK/ex.json"
run 'malformed exceptions JSON fails with a message' 1 'array of objects'

EXFILE=$WORK/absent.json
run 'missing exceptions file fails' 1 'missing exceptions file'
EXFILE=

exceptions "$LISTED"
echo '{"error":{"code":"ENOLOCK","summary":"no lockfile"}}' >"$WORK/report.json"
run 'ENOLOCK fails' 1 'no usable report'

: >"$WORK/report.json"
run 'empty npm output fails' 1 'no usable report'

echo 'npm-audit-test: ok'
