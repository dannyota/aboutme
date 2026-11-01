#!/usr/bin/env bash
set -uo pipefail
umask 077
write_summary() {
  [ -n "${GITHUB_STEP_SUMMARY:-}" ] || return 0
  { printf '%s\n' "$1" >> "$GITHUB_STEP_SUMMARY"; } 2>/dev/null
}
fail() {
  printf '%s\n' '::error::Synthetic quota renewal failed' 'Synthetic quota: failed'
  write_summary 'Synthetic quota: failed' || :
  exit "${1:-1}"
}
success() {
  write_summary 'Synthetic quota: healthy' || fail
  printf '%s\n' 'Synthetic quota: healthy'
  exit 0
}
trap 'fail 130' INT
trap 'fail 143' TERM
[ "$#" -eq 1 ] || fail
case "$1" in check|renew) action=$1 ;; *) fail ;; esac
for name in VNGCLOUD_CLIENT_ID VNGCLOUD_CLIENT_SECRET VNGCLOUD_REGION VNGCLOUD_PROJECT_ID GITHUB_TOKEN GITHUB_REPOSITORY; do
  [ -n "${!name:-}" ] || fail
done
case "$GITHUB_TOKEN" in *[!a-zA-Z0-9_-]*) fail ;; esac
case "$GITHUB_REPOSITORY" in *[!a-zA-Z0-9_.\/-]*|*/*/*|/*|*/) fail ;; */*) ;; *) fail ;; esac
cli=$(command -v vngcloud) || fail
case "$cli" in /*) ;; *) fail ;; esac
tmp=$(mktemp -d) || fail
trap 'rm -rf "$tmp"' EXIT
mkdir -m 700 "$tmp/home" || fail
printf 'header = "Authorization: Bearer %s"\nheader = "Accept: application/vnd.github+json"\nheader = "X-GitHub-Api-Version: 2022-11-28"\n' "$GITHUB_TOKEN" > "$tmp/curl.conf"
issue_url="https://api.github.com/repos/$GITHUB_REPOSITORY/issues/146"
issue_read() {
  curl --disable --config "$tmp/curl.conf" --silent --show-error --fail --max-time 30 -o "$tmp/issue" --proto "=https" "$issue_url" 2> "$tmp/error" || return 1
  jq -er -s 'if length == 1 and (.[0] | type == "object") and (.[0].state == "open" or .[0].state == "closed") then .[0].state else error("invalid") end' "$tmp/issue" 2> "$tmp/error"
}
issue_write() {
  curl --disable --config "$tmp/curl.conf" --silent --show-error --fail --max-time 30 -X PATCH --data "{\"state\":\"$1\"}" -o "$tmp/issue" --proto "=https" "$issue_url" 2> "$tmp/error" || return 1
  [ "$(issue_read)" = "$1" ]
}
run_cli() {
  env -i PATH="$PATH" HOME="$tmp/home" LC_ALL=C \
    VNGCLOUD_CLIENT_ID="$VNGCLOUD_CLIENT_ID" VNGCLOUD_CLIENT_SECRET="$VNGCLOUD_CLIENT_SECRET" \
    VNGCLOUD_REGION="$VNGCLOUD_REGION" VNGCLOUD_PROJECT_ID="$VNGCLOUD_PROJECT_ID" \
    "$cli" "$@" > "$tmp/cli-out" 2> "$tmp/cli-error"
}
cat > "$tmp/validate.py" <<'PY'
import calendar
import datetime as dt
from decimal import Decimal, getcontext
import json
import re
import sys


def pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise ValueError('duplicate')
        result[key] = value
    return result


def load(path):
    with open(path, encoding='utf-8') as source:
        return json.load(source, object_pairs_hook=pairs)


def timestamp(value):
    if not isinstance(value, str):
        raise ValueError('time')
    match = re.fullmatch(r'(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d+))?(Z|[+-]\d\d:\d\d)', value)
    if not match:
        raise ValueError('time')
    getcontext().prec = max(getcontext().prec, len(value) + 30)
    if match[3] != 'Z' and (int(match[3][1:3]) > 23 or int(match[3][4:6]) > 59):
        raise ValueError('offset')
    instant = dt.datetime.fromisoformat(match[1] + match[3].replace('Z', '+00:00'))
    seconds = calendar.timegm(instant.astimezone(dt.timezone.utc).utctimetuple())
    return Decimal(seconds) + Decimal('0.' + (match[2] or '0'))


def row(path):
    data = load(path)
    assert isinstance(data, dict) and isinstance(data.get('Complete'), bool) and data['Complete']
    timestamp(data['AsOf'])
    items = data['Items']
    assert isinstance(items, list) and len(items) == 1
    item = items[0]
    assert isinstance(item, dict)
    assert item['Kind'] == 'Synthetic' and item['ClassName'] == 'Basic' and item['PackageName'] == ''
    assert item['Status'] == 'ACTIVE' and item['Error'] is None
    assert type(item['Price']) in (int, float) and item['Price'] == 0
    assert item['Quota'] is None or isinstance(item['Quota'], dict)
    assert item['TimeLeftSeconds'] is None or type(item['TimeLeftSeconds']) is int
    assert item['AutoRenew'] is None or type(item['AutoRenew']) is bool
    assert item['RenewWindowOpen'] is None or type(item['RenewWindowOpen']) is bool
    assert isinstance(item['NextSafeCommand'], str)
    timestamp(item['StartTime'])
    return timestamp(item['EndTime']), item['RenewWindowOpen']


def outcome(path, rc):
    obj = load(path)
    assert isinstance(obj, dict) and set(obj) == {'Sent', 'Stage', 'Error', 'PreviousEndTime', 'Quota', 'OrderID', 'PaymentPending', 'Renewed'}
    sent, stage, error = obj['Sent'], obj['Stage'], obj['Error']
    assert sent in ('no', 'yes', 'unknown') and isinstance(stage, str)
    if obj['PreviousEndTime'] is not None:
        timestamp(obj['PreviousEndTime'])
    assert obj['Quota'] is None or isinstance(obj['Quota'], dict)
    assert isinstance(obj['OrderID'], str) and type(obj['PaymentPending']) is bool and type(obj['Renewed']) is bool
    assert obj['Renewed'] == (stage == 'confirmed')
    assert not obj['PaymentPending'] or stage == 'unconfirmed'
    code, status = None, None
    if error is not None:
        assert isinstance(error, dict) and set(error) == {'Code', 'Operation', 'Status'}
        code, status = error['Code'], error['Status']
        assert isinstance(code, str) and error['Operation'] == 'monitor.RenewSyntheticQuota' and type(status) is int
    valid = False
    if stage == 'not-due':
        valid = sent == 'no' and error is None and rc == 0
    elif stage == 'confirmed':
        valid = sent in ('yes', 'unknown') and error is None and rc == 0
    elif stage == 'accepted':
        valid = sent == 'yes' and error is None and rc == 0
    elif stage == 'unconfirmed':
        valid = sent in ('yes', 'unknown') and code == 'NotSettled' and rc == 1 and status >= 0
    elif stage == 'refused':
        valid = sent == 'yes' and (code, status, rc) in {('Conflict', 409, 1), ('BadRequest', 400, 1), ('Forbidden', 403, 1), ('Throttled', 429, 1), ('Unauthorized', 401, 3), ('NotFound', 404, 4)}
    elif stage == 'preflight' and sent == 'no':
        valid = ((code in ('PriceAboveMax', 'Unpriced', 'QuotaChanged', 'TokenCacheFailed', 'RequestFailed', 'UnexpectedStatus') and status == 0 and rc == 1)
                 or (code in ('InvalidUsage', 'ReadOnly', 'InvalidConfig') and status == 0 and rc == 2)
                 or (code in ('NoCredentials', 'ServiceAccountLoginFailed') and status >= 0 and (rc == 3 or (code == 'ServiceAccountLoginFailed' and rc == 1)))
                 or (code == 'NotFound' and status == 404 and rc == 4)
                 or (code in ('BadRequest', 'Forbidden', 'Conflict', 'Throttled', 'ServerError', 'ClientError', 'InvalidResponse', 'ResponseTooLarge') and status >= 0 and rc == 1)
                 or (code == 'Unauthorized' and status >= 0 and rc == 3)) if isinstance(status, int) else False
    assert valid
    close = (sent == 'no' and stage in ('not-due', 'preflight')) or stage in ('refused', 'confirmed')
    print(('close' if close else 'hold') + ' ' + stage)


try:
    mode = sys.argv[1]
    if mode == 'initial':
        quota = load(sys.argv[2])['Quota']
        assert isinstance(quota, dict) and quota['Status'] == 'ACTIVE'
        expiry = quota['EndTime']
        end = timestamp(expiry)
        snapshot_end, window = row(sys.argv[3])
        assert end == snapshot_end
        remaining = end - timestamp(sys.argv[4])
        assert remaining > 0
        print(expiry)
        print('due' if remaining <= 259200 and window else 'skip')
        print('healthy' if remaining > 172800 else 'unhealthy')
    elif mode == 'final':
        end, _ = row(sys.argv[2])
        assert (end - timestamp(sys.argv[3])) > 172800
        if sys.argv[4] == 'confirmed':
            assert end > timestamp(sys.argv[5])
    else:
        outcome(sys.argv[2], int(sys.argv[3]))
except (ValueError, KeyError, TypeError, AssertionError, OverflowError, OSError, json.JSONDecodeError):
    sys.exit(1)
PY
hold=$(issue_read) || fail
if [ "$hold" = open ]; then
  printf '%s\n' 'Synthetic quota: hold open'
  [ "$action" = check ] || fail
fi
run_cli --read-only monitor get-synthetic-quota --output json || fail
cp "$tmp/cli-out" "$tmp/get"
run_cli --read-only monitor quota-status --kind synthetic --output json || fail
cp "$tmp/cli-out" "$tmp/status"
now=$(python3 -I -c 'from datetime import datetime, timezone; print(datetime.now(timezone.utc).isoformat())') || fail
python3 -I "$tmp/validate.py" initial "$tmp/get" "$tmp/status" "$now" > "$tmp/decision" 2> "$tmp/error" || fail
expiry=$(sed -n '1p' "$tmp/decision")
decision=$(sed -n '2p' "$tmp/decision")
healthy=$(sed -n '3p' "$tmp/decision")
if [ "$action" = check ] || [ "$decision" = skip ]; then
  [ "$healthy" = healthy ] || fail
  success
fi
issue_write open || fail
rc=0
run_cli monitor renew-synthetic-quota --expected-end-time "$expiry" --max-days-left 3 --max-price 0 --output json || rc=$?
case "$rc" in 0|1|2|3|4) ;; *) fail ;; esac
classification=$(python3 -I "$tmp/validate.py" outcome "$tmp/cli-out" "$rc" 2> "$tmp/error") || { [ "$rc" -eq 0 ] && fail; fail "$rc"; }
case "$classification" in close\ *) issue_write closed || { [ "$rc" -eq 0 ] && fail; fail "$rc"; } ;; esac
[ "$rc" -eq 0 ] || fail "$rc"
case "$classification" in 'close not-due') stage=not-due ;; 'close confirmed') stage=confirmed ;; *) fail ;; esac
run_cli --read-only monitor quota-status --kind synthetic --output json || fail
now=$(python3 -I -c 'from datetime import datetime, timezone; print(datetime.now(timezone.utc).isoformat())') || fail
python3 -I "$tmp/validate.py" final "$tmp/cli-out" "$now" "$stage" "$expiry" 2> "$tmp/error" || fail
success
