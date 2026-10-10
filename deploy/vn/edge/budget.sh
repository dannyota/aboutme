#!/usr/bin/env bash
# Creates the monthly cost budget `aboutme-prod` with alert thresholds at 80 and
# 100 percent of the limit (docs/design/vietnam-production.md, "Logs, metrics,
# and alarms", the Spend row; "Infrastructure code and state" puts billing
# budgets in the vngcloud CLI).
#
#   budget.sh --limit-vnd <amount>
#
# Values come from the vngcloud wiki, Billing-and-Pricing.md, "Budgets": the
# period type is MONTHLY, the budget type is ACTUAL (the other value is
# FORECASTED), and each threshold type is ACTUAL. The account holds at most one
# budget of each type, so the script stops if another ACTUAL budget exists.
# Re-running changes nothing that already exists, and never changes a limit.
#
# Unconfirmed: that --limit-amount is in VND (a live budget reads
# Currency "credit"), and which address the budget alerts go to.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=../scripts/vng-lib.sh
. "$here/../scripts/vng-lib.sh"

NAME=aboutme-prod
THRESHOLDS=(80 100)

usage() {
  say "usage: budget.sh --limit-vnd <amount>" >&2
  exit 2
}

limit=
while (($#)); do
  case $1 in
    --limit-vnd)
      (($# >= 2)) || usage
      limit=$2
      shift 2
      ;;
    *) usage ;;
  esac
done
[[ $limit =~ ^[1-9][0-9]*$ ]] || usage

need_tools

budgets=$(vng billing list-budgets)
uuid=$(jq -r --arg n "$NAME" \
  '[.Items[]? | select(.Name == $n) | .UUID] | .[0] // empty' <<<"$budgets")

if [[ -z $uuid ]]; then
  other=$(jq -r '[.Items[]? | select(.Type == "ACTUAL") | .Name] | .[0] // empty' <<<"$budgets")
  [[ -z $other ]] ||
    die "the account already has an ACTUAL budget named $other and holds one per type; change it by hand or delete it first"
  vng billing create-budget --name "$NAME" --period-type MONTHLY \
    --type ACTUAL --limit-amount "$limit" >/dev/null
  uuid=$(vng billing list-budgets |
    jq -r --arg n "$NAME" '[.Items[]? | select(.Name == $n) | .UUID] | .[0] // empty')
  [[ -n $uuid ]] || die "budget $NAME not found after create"
  say "budget $NAME: created with limit $limit"
else
  current=$(jq -r --arg n "$NAME" \
    '[.Items[]? | select(.Name == $n) | .LimitAmount] | .[0]' <<<"$budgets")
  say "budget $NAME: exists with limit $current"
  [[ $current == "$limit" ]] ||
    say "warning: the limit differs from $limit; this script does not change it"
fi

have=$(vng billing list-budget-thresholds --budget-uuid "$uuid")
for pct in "${THRESHOLDS[@]}"; do
  if jq -e --argjson p "$pct" \
    '[.Items[]? | select(.ThresholdType == "ACTUAL" and .ThresholdPercentage == $p)] | length > 0' \
    <<<"$have" >/dev/null; then
    say "threshold $pct percent: exists"
  else
    vng billing create-budget-threshold --budget-uuid "$uuid" \
      --threshold-type ACTUAL --threshold-percentage "$pct" >/dev/null
    say "threshold $pct percent: created"
  fi
done
