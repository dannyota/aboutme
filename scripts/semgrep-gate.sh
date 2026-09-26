#!/usr/bin/env bash
# Fails a hosted Semgrep job on the findings this repository blocks on,
# whatever the Semgrep platform policy marks as blocking. It reads the JSON
# that `semgrep ci --json-output=<file>` writes. That file already leaves out
# findings suppressed in place with `nosemgrep`; the filter below also skips
# any result marked ignored.
#
#   code          blocks every finding of severity ERROR, HIGH, or CRITICAL
#   supply-chain  blocks a reachable finding of severity ERROR, HIGH, or
#                 CRITICAL
#
# Semgrep rules use ERROR/WARNING/INFO or, since 1.72.0, CRITICAL/HIGH/
# MEDIUM/LOW, where HIGH replaces ERROR and CRITICAL sits above it.
#
# Usage: scripts/semgrep-gate.sh code|supply-chain <semgrep-json>
set -Eeuo pipefail

usage() {
  echo "usage: $0 code|supply-chain <semgrep-json>" >&2
  exit 2
}

[ "$#" -eq 2 ] || usage
mode=$1
report=$2
case "$mode" in
code | supply-chain) ;;
*) usage ;;
esac

if [ ! -s "$report" ]; then
  echo "semgrep-gate: $report is missing or empty, so the scan results cannot be checked" >&2
  exit 1
fi

# One line per blocking finding: rule, path, line, severity, tab-separated.
# shellcheck disable=SC2016 # a jq program; $mode is a jq variable
filter='
  if (.results | type) != "array" then error("the report has no results array") else . end
  | .results[]
  | select(.extra.is_ignored != true)
  | select(.extra.severity | IN("ERROR", "HIGH", "CRITICAL"))
  | select($mode == "code"
      or .extra.sca_info.reachable == true
      or ((.extra.sca_info.kind // "") | if type == "array" then .[0] else . end
          | IN("DirectReachable", "TransitiveReachable")))
  | [.check_id, .path, (.start.line | tostring), .extra.severity] | @tsv'

if ! blocking=$(jq -r --arg mode "$mode" "$filter" "$report"); then
  echo "semgrep-gate: cannot read $report as Semgrep JSON" >&2
  exit 1
fi
total=$(jq '.results | length' "$report")

if [ -z "$blocking" ]; then
  echo "semgrep-gate: no blocking $mode findings among $total"
  exit 0
fi

count=$(wc -l <<<"$blocking")
echo "semgrep-gate: $count blocking $mode finding(s) among $total:" >&2
while IFS=$'\t' read -r rule path line severity; do
  echo "  $severity $rule $path:$line" >&2
  echo "::error file=$path,line=$line,title=Semgrep $severity::$rule"
done <<<"$blocking"
exit 1
