#!/usr/bin/env bash
set -uo pipefail
# Runner logs admit only fixed messages; shell errors can expose arguments.
exec 2>/dev/null
umask 077
diagnostic='Synthetic quota diagnostic: stage=environment reason=args'
write_summary() {
  [ -n "${GITHUB_STEP_SUMMARY:-}" ] || return 0
  { printf '%s\n' "$1" >> "$GITHUB_STEP_SUMMARY"; } 2>/dev/null
}
fail() {
  trap '' INT TERM
  printf '%s\n' '::error::Synthetic quota renewal failed' 'Synthetic quota: failed' "$diagnostic"
  write_summary 'Synthetic quota: failed' || :
  write_summary "$diagnostic" || :
  exit "${1:-1}"
}
environment_fail() {
  local reason=${1:-args}
  case "$reason" in
    args|missing-VNGCLOUD_CLIENT_ID|missing-VNGCLOUD_CLIENT_SECRET|missing-VNGCLOUD_REGION|missing-VNGCLOUD_PROJECT_ID|missing-GITHUB_TOKEN|missing-GITHUB_REPOSITORY|token-format|repository-format|tool-curl|tool-jq|tool-python3|tool-mktemp|tool-mkdir|tool-cp|tool-sed|tool-cat|tool-env|tool-rm|cli-path|tempdir|config-write|summary|signal|copy|clock) ;;
    *) reason=args ;;
  esac
  diagnostic="Synthetic quota diagnostic: stage=environment reason=$reason"
  fail "${2:-1}"
}
success() {
  write_summary 'Synthetic quota: healthy' || environment_fail summary
  printf '%s\n' 'Synthetic quota: healthy'
  exit 0
}
trap 'environment_fail signal 130' INT
trap 'environment_fail signal 143' TERM
[ "$#" -eq 1 ] || environment_fail args
case "$1" in check|renew) action=$1 ;; *) environment_fail args ;; esac
for name in VNGCLOUD_CLIENT_ID VNGCLOUD_CLIENT_SECRET VNGCLOUD_REGION VNGCLOUD_PROJECT_ID GITHUB_TOKEN GITHUB_REPOSITORY; do
  [ -n "${!name:-}" ] || environment_fail "missing-$name"
done
token_is_safe() {
  local LC_ALL=C
  # GitHub tokens are opaque; exclude bytes unsafe in curl's quoted header.
  case "$GITHUB_TOKEN" in *[![:graph:]]*|*\"*|*\\*) return 1 ;; esac
}
token_is_safe || environment_fail token-format
case "$GITHUB_REPOSITORY" in *[!a-zA-Z0-9_.\/-]*|*/*/*|/*|*/) environment_fail repository-format ;; */*) ;; *) environment_fail repository-format ;; esac
for tool in curl jq python3 mktemp mkdir cp sed cat env rm; do
  command -v "$tool" >/dev/null || environment_fail "tool-$tool"
done
cli=$(command -v vngcloud) || environment_fail cli-path
case "$cli" in /*) ;; *) environment_fail cli-path ;; esac
tmp=$(mktemp -d) || environment_fail tempdir
trap 'rm -rf "$tmp"' EXIT
mkdir -m 700 "$tmp/home" || environment_fail tempdir
printf 'header = "Authorization: Bearer %s"\nheader = "Accept: application/vnd.github+json"\nheader = "X-GitHub-Api-Version: 2022-11-28"\n' "$GITHUB_TOKEN" > "$tmp/curl.conf" || environment_fail config-write
issue_url="https://api.github.com/repos/$GITHUB_REPOSITORY/issues/146"
issue_request() {
  local issue_stage=$1 rc=0 http_class=none
  shift
  case "$issue_stage" in issue-read|issue-write) ;; *) environment_fail args ;; esac
  diagnostic="Synthetic quota diagnostic: stage=$issue_stage http=none"
  curl --disable --config "$tmp/curl.conf" --silent --show-error --fail --max-time 30 \
    --write-out '%{http_code}' -o "$tmp/issue" --proto '=https' "$@" "$issue_url" \
    > "$tmp/issue-http" 2> "$tmp/error" || rc=$?
  case "$(cat "$tmp/issue-http")" in
    2[0-9][0-9]) http_class=2xx ;;
    4[0-9][0-9]) http_class=4xx ;;
    5[0-9][0-9]) http_class=5xx ;;
  esac
  diagnostic="Synthetic quota diagnostic: stage=$issue_stage http=$http_class"
  [ "$rc" -eq 0 ] && [ "$http_class" = 2xx ]
}
issue_read() {
  issue_request issue-read || return 1
  hold=$(jq -er -s 'if length == 1 and (.[0] | type == "object") and (.[0].state == "open" or .[0].state == "closed") then .[0].state else error("invalid") end' "$tmp/issue" 2> "$tmp/error")
}
issue_write() {
  issue_request issue-write -X PATCH --data "{\"state\":\"$1\"}" || return 1
  issue_read || return 1
  diagnostic='Synthetic quota diagnostic: stage=issue-write http=2xx'
  [ "$hold" = "$1" ]
}
run_cli() {
  local cli_stage=$1 rc=0 code
  shift
  case "$cli_stage" in quota-read|status-read|renew|readback) ;; *) environment_fail args ;; esac
  diagnostic="Synthetic quota diagnostic: stage=$cli_stage cli-exit=0 code=other"
  env -i PATH="$PATH" HOME="$tmp/home" LC_ALL=C \
    VNGCLOUD_CLIENT_ID="$VNGCLOUD_CLIENT_ID" VNGCLOUD_CLIENT_SECRET="$VNGCLOUD_CLIENT_SECRET" \
    VNGCLOUD_REGION="$VNGCLOUD_REGION" VNGCLOUD_PROJECT_ID="$VNGCLOUD_PROJECT_ID" \
    "$cli" "$@" > "$tmp/cli-out" 2> "$tmp/cli-error" || rc=$?
  code=$(python3 -I "$tmp/validate.py" diagnostic "$cli_stage" "$tmp/cli-out" "$tmp/cli-error" 2> "$tmp/error") || code=other
  diagnostic="Synthetic quota diagnostic: stage=$cli_stage cli-exit=$rc code=$code"
  return "$rc"
}
cat > "$tmp/validate.py" <<'PY' || environment_fail config-write
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


def error_code(error, lowercase=False):
    assert isinstance(error, dict)
    if lowercase:
        assert {'code', 'message'} <= set(error) <= {'code', 'message', 'operation', 'status'}
        assert isinstance(error['message'], str)
        assert 'operation' not in error or isinstance(error['operation'], str)
        assert 'status' not in error or type(error['status']) is int
        code = error['code']
    else:
        assert set(error) == {'Code', 'Operation', 'Status'}
        assert isinstance(error['Operation'], str) and type(error['Status']) is int
        code = error['Code']
    assert isinstance(code, str)
    return code


def diagnostic_code(stage, stdout, stderr):
    # Read only the pin's result fields or its single lowercase stderr envelope.
    try:
        obj = load(stdout)
        assert isinstance(obj, dict)
        if stage == 'renew':
            assert set(obj) == {'Sent', 'Stage', 'Error', 'PreviousEndTime', 'Quota', 'OrderID', 'PaymentPending', 'Renewed'}
            code = error_code(obj['Error'])
            assert obj['Error']['Operation'] == 'monitor.RenewSyntheticQuota'
            return code
        if stage in ('status-read', 'readback'):
            assert set(obj) == {'AsOf', 'Complete', 'Items'}
            assert isinstance(obj['AsOf'], str) and type(obj['Complete']) is bool
            assert isinstance(obj['Items'], list) and len(obj['Items']) == 1
            item = obj['Items'][0]
            assert isinstance(item, dict) and item['Kind'] == 'Synthetic'
            assert set(item) == {'Kind', 'ClassName', 'PackageName', 'Status', 'Quota', 'Price', 'StartTime', 'EndTime', 'TimeLeftSeconds', 'AutoRenew', 'RenewWindowOpen', 'NextSafeCommand', 'Error'}
            return error_code(item['Error'])
    except (ValueError, KeyError, TypeError, AssertionError, OSError):
        pass
    try:
        obj = load(stderr)
        assert isinstance(obj, dict) and set(obj) == {'error'}
        return error_code(obj['error'], lowercase=True)
    except (ValueError, KeyError, TypeError, AssertionError, OSError):
        return 'other'


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
    if mode == 'diagnostic':
        # Filter before shell substitution can strip newlines or NUL bytes.
        codes = {'PriceAboveMax', 'Unpriced', 'QuotaChanged', 'NotSettled', 'Conflict',
                 'BadRequest', 'Forbidden', 'Throttled', 'Unauthorized', 'NotFound',
                 'RequestFailed', 'InvalidUsage', 'ReadOnly', 'InvalidConfig',
                 'NoCredentials', 'ServiceAccountLoginFailed', 'TokenCacheFailed',
                 'UnexpectedStatus', 'ServerError', 'ClientError', 'InvalidResponse',
                 'ResponseTooLarge', 'QuotaStatusIncomplete'}
        code = diagnostic_code(*sys.argv[2:])
        print(code if code in codes else 'other')
    elif mode == 'initial':
        quota = load(sys.argv[2])['Quota']
        assert isinstance(quota, dict) and quota['Status'] == 'ACTIVE'
        expiry = quota['EndTime']
        end = timestamp(expiry)
        snapshot_end, window = row(sys.argv[3])
        assert end == snapshot_end
        remaining = end - timestamp(sys.argv[4])
        if remaining <= 0:
            sys.exit(2)
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
issue_read || fail
if [ "$hold" = open ]; then
  printf '%s\n' 'Synthetic quota: hold open'
  diagnostic='Synthetic quota diagnostic: stage=hold'
  [ "$action" = check ] || fail
fi
run_cli quota-read --read-only monitor get-synthetic-quota --output json || fail
cp "$tmp/cli-out" "$tmp/get" || environment_fail copy
run_cli status-read --read-only monitor quota-status --kind synthetic --output json || fail
cp "$tmp/cli-out" "$tmp/status" || environment_fail copy
now=$(python3 -I -c 'from datetime import datetime, timezone; print(datetime.now(timezone.utc).isoformat())') || environment_fail clock
diagnostic='Synthetic quota diagnostic: stage=parse'
parse_rc=0
python3 -I "$tmp/validate.py" initial "$tmp/get" "$tmp/status" "$now" > "$tmp/decision" 2> "$tmp/error" || parse_rc=$?
case "$parse_rc" in 0) ;; 2) diagnostic='Synthetic quota diagnostic: stage=threshold'; fail ;; *) fail ;; esac
expiry=$(sed -n '1p' "$tmp/decision") || environment_fail copy
decision=$(sed -n '2p' "$tmp/decision") || environment_fail copy
healthy=$(sed -n '3p' "$tmp/decision") || environment_fail copy
if [ "$action" = check ] || [ "$decision" = skip ]; then
  diagnostic='Synthetic quota diagnostic: stage=threshold'
  [ "$healthy" = healthy ] || fail
  success
fi
issue_write open || fail
rc=0
run_cli renew monitor renew-synthetic-quota --expected-end-time "$expiry" --max-days-left 3 --max-price 0 --output json || rc=$?
renew_diagnostic=$diagnostic
case "$rc" in 0|1|2|3|4) ;; *) fail ;; esac
classification=$(python3 -I "$tmp/validate.py" outcome "$tmp/cli-out" "$rc" 2> "$tmp/error") || { [ "$rc" -eq 0 ] && fail; fail "$rc"; }
case "$classification" in close\ *) issue_write closed || { [ "$rc" -eq 0 ] && fail; fail "$rc"; } ;; esac
diagnostic=$renew_diagnostic
[ "$rc" -eq 0 ] || fail "$rc"
case "$classification" in 'close not-due') stage=not-due ;; 'close confirmed') stage=confirmed ;; *) fail ;; esac
run_cli readback --read-only monitor quota-status --kind synthetic --output json || fail
now=$(python3 -I -c 'from datetime import datetime, timezone; print(datetime.now(timezone.utc).isoformat())') || environment_fail clock
python3 -I "$tmp/validate.py" final "$tmp/cli-out" "$now" "$stage" "$expiry" 2> "$tmp/error" || fail
success
