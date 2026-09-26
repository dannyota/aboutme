#!/usr/bin/env bash

# Regression for scripts/semgrep-gate.sh: which Semgrep JSON results block a
# hosted scan, and that an unreadable report fails closed.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
GATE=$ROOT/scripts/semgrep-gate.sh
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-semgrep-gate.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

failures=0
fail() {
  printf 'not ok - %s\n' "$*" >&2
  failures=$((failures + 1))
}

# result <rule> <severity> [extra-json-fields]
result() {
  printf '{"check_id":"%s","path":"src/x.go","start":{"line":7},"extra":{"severity":"%s"%s}}' \
    "$1" "$2" "${3:+,$3}"
}

# report <name> <result>...
report() {
  local name=$1
  shift
  local IFS=,
  printf '{"results":[%s],"errors":[]}' "$*" >"$WORK/$name.json"
}

# expect <exit-code> <mode> <name> [rule-that-must-be-named]
expect() {
  local want=$1 mode=$2 name=$3 rule=${4:-} got=0
  "$GATE" "$mode" "$WORK/$name.json" >"$WORK/$name.out" 2>&1 || got=$?
  [ "$got" -eq "$want" ] || fail "$mode $name: exit $got, want $want: $(cat "$WORK/$name.out")"
  if [ -n "$rule" ] && ! grep -Fq "$rule" "$WORK/$name.out"; then
    fail "$mode $name: output does not name $rule"
  fi
}

report empty
expect 0 code empty
expect 0 supply-chain empty

for severity in ERROR HIGH CRITICAL; do
  report "code-$severity" "$(result "rule.$severity" "$severity")" "$(result rule.info INFO)"
  expect 1 code "code-$severity" "rule.$severity"
done

report code-lower "$(result a WARNING)" "$(result b MEDIUM)" "$(result c LOW)" "$(result d INFO)"
expect 0 code code-lower

report code-ignored "$(result rule.ignored ERROR '"is_ignored":true')"
expect 0 code code-ignored

report sca-direct "$(result sca.direct HIGH '"sca_info":{"reachable":true,"kind":"DirectReachable"}')"
expect 1 supply-chain sca-direct sca.direct

report sca-transitive "$(result sca.transitive CRITICAL '"sca_info":{"reachable":false,"kind":["TransitiveReachable",{}]}')"
expect 1 supply-chain sca-transitive sca.transitive

report sca-unreachable \
  "$(result sca.lockfile CRITICAL '"sca_info":{"reachable":false,"kind":["LockfileOnlyMatch","Transitive"]}')" \
  "$(result sca.undetermined HIGH '"sca_info":{"reachable":false,"kind":["TransitiveUndetermined",{}]}')" \
  "$(result sca.medium MEDIUM '"sca_info":{"reachable":true,"kind":"DirectReachable"}')"
expect 0 supply-chain sca-unreachable

: >"$WORK/blank.json"
expect 1 code blank
expect 1 code missing
printf 'not json' >"$WORK/garbage.json"
expect 1 code garbage
printf '{"errors":[]}' >"$WORK/no-results.json"
expect 1 supply-chain no-results
expect 2 secrets empty

if [ "$failures" -ne 0 ]; then
  printf 'semgrep-gate-test: %d failure(s)\n' "$failures" >&2
  exit 1
fi
printf 'semgrep gate tests passed\n'
