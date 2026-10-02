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

ID=GHSA-aaaa-bbbb-cccc
LISTED="[{\"id\":\"$ID\",\"expires\":\"2026-11-01\",\"reason\":\"r\"}]"
TODAY=2026-10-02

# run NAME EXPECTED_RC [stderr substring]
run() {
  local name=$1 want=$2 needle=${3:-} rc=0 err
  err=$(NPM_AUDIT_NPM="$WORK/npm" NPM_AUDIT_EXCEPTIONS="$WORK/ex.json" \
    NPM_AUDIT_TODAY=$TODAY bash "$SCRIPT" "$WORK/pkg" 2>&1 >/dev/null) || rc=$?
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

report critical GHSA-dddd-eeee-ffff
exceptions "$LISTED"
run 'exception for another id does not match' 1 GHSA-dddd-eeee-ffff

report moderate "$ID"
exceptions '[]'
run 'moderate is ignored' 0

exceptions '[{"id":"GHSA-aaaa-bbbb-cccc","reason":"r"}]'
run 'entry without expires fails' 1 malformed

echo '{"error":{"code":"ENOTFOUND"}}' >"$WORK/report.json"
exceptions '[]'
run 'audit error fails' 1 'no usable report'

echo 'npm-audit-test: ok'
