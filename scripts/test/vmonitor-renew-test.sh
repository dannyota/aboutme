#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/state"
bash_path=$(command -v bash)
command -v python3 > "$tmp/state/python-path"
cat > "$tmp/bin/python3" <<'SH'
#!/usr/bin/env bash
state=$(dirname "$0")/../state
if [ "${1:-}" = -I ] && [ "${2:-}" = -c ]; then
  [ ! -f "$state/clock-fail" ] || exit 1
  [ ! -f "$state/clock-fail-after" ] || [ ! -f "$state/after-send" ] || exit 1
  cat "$state/clock"
else
  exec "$(cat "$state/python-path")" "$@"
fi
SH
cat > "$tmp/bin/curl" <<'SH'
#!/usr/bin/env bash
state=$(dirname "$0")/../state
printf '%s\n' "$*" >> "$state/curl-argv"
method=GET
out=
write_out=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    -X) method=$2; shift 2 ;;
    -o) out=$2; shift 2 ;;
    --data) data=$2; shift 2 ;;
    --write-out) write_out=true; shift 2 ;;
    *) shift ;;
  esac
done
rc=0
[ ! -f "$state/curl-fail-$method" ] || rc=22
if [ "$method" = GET ]; then
  count=0
  [ ! -f "$state/get-count" ] || count=$(cat "$state/get-count")
  count=$((count + 1))
  echo "$count" > "$state/get-count"
  [ ! -f "$state/fail-get-$count" ] || rc=22
fi
if [ "$method" = PATCH ]; then
  case "$data" in *closed*) [ ! -f "$state/fail-close" ] || rc=22 ;; esac
fi
http=200
[ "$rc" -eq 0 ] || http=500
[ ! -f "$state/http-$method" ] || http=$(cat "$state/http-$method")
case "$http" in 4??|5??|000) rc=22 ;; esac
if "$write_out"; then printf '%s' "$http"; fi
if [ "$rc" -ne 0 ]; then
  if [ -f "$state/curl-error" ]; then cat "$state/curl-error" >&2; else
    printf '%s\n' 'fake-token hostile-secret https://private.invalid/issue' >&2
  fi
  exit "$rc"
fi
if [ "$method" = PATCH ]; then
  if [ ! -f "$state/ignore-patch" ]; then
    case "$data" in *open*) echo open > "$state/issue" ;; *closed*) echo closed > "$state/issue" ;; *) exit 1 ;; esac
  fi
  case "$data" in *open*) if [ -f "$state/interrupt-open" ]; then kill -TERM "$PPID"; exit 1; fi ;; esac
fi
if [ -f "$state/issue-response" ]; then cat "$state/issue-response" > "$out"
elif [ -f "$state/issue-body" ]; then
  jq -cn --arg state "$(cat "$state/issue")" --rawfile body "$state/issue-body" '{state:$state,body:$body}' > "$out"
elif [ -f "$state/invalid-issue" ]; then printf '%s\n' 'invalid' > "$out"; else
  printf '{"state":"%s"}\n' "$(cat "$state/issue")" > "$out"
fi
SH
cat > "$tmp/bin/vngcloud" <<'SH'
#!/usr/bin/env bash
state=$(dirname "$0")/../state
printf '%s\n' "$*" >> "$state/argv"
env | sort > "$state/cli-env"
[ ! -e "$HOME/.vngcloud/config" ] || exit 98
case "$*" in
  *get-synthetic-quota*) fixture=get ;;
  *quota-status*)
    if [ -f "$state/after-send" ] && [ -f "$state/status-after" ]; then fixture=status-after; else fixture=status; fi ;;
  *renew-synthetic-quota*)
    [ "$(cat "$state/issue")" = open ] && [ "$(cat "$state/get-count")" -ge 2 ] || exit 99
    : > "$state/after-send"
    if [ -f "$state/interrupt" ]; then kill -TERM "$PPID"; exit 1; fi
    fixture=outcome ;;
  *) exit 99 ;;
esac
cat "$state/$fixture"
if [ -f "$state/$fixture-error" ]; then cat "$state/$fixture-error" >&2; else
  printf '%s\n' "$VNGCLOUD_CLIENT_ID $VNGCLOUD_CLIENT_SECRET $VNGCLOUD_PROJECT_ID hostile-secret" >&2
fi
if [ -f "$state/$fixture-invalid-exit" ]; then
  (exit "$(cat "$state/$fixture-invalid-exit")") || :
fi
if [ "$fixture" = outcome ]; then exit "$(cat "$state/exit")"; fi
[ ! -f "$state/$fixture-exit" ] || exit "$(cat "$state/$fixture-exit")"
SH
for tool in mktemp mkdir cp sed; do
  command -v "$tool" > "$tmp/state/$tool-path"
  cat > "$tmp/bin/$tool" <<'SH'
#!/usr/bin/env bash
tool=${0##*/}
state=${0%/*}/../state
IFS= read -r real < "$state/$tool-path"
case "$tool" in
  mktemp)
    [ ! -f "$state/tempdir-fail" ] || exit 1
    directory=$("$real" "$@") || exit 1
    [ ! -f "$state/config-fail" ] || mkdir "$directory/curl.conf"
    [ ! -f "$state/validator-fail" ] || mkdir "$directory/validate.py"
    printf '%s\n' "$directory"
    exit 0 ;;
  mkdir) [ ! -f "$state/mkdir-fail" ] || exit 1 ;;
  cp)
    case "${2:-}" in
      */get) [ ! -f "$state/copy-fail-get" ] || exit 1 ;;
      */status) [ ! -f "$state/copy-fail-status" ] || exit 1 ;;
    esac ;;
  sed) [ ! -f "$state/sed-fail-${2:-}" ] || exit 1 ;;
esac
exec "$real" "$@"
SH
done
chmod +x "$tmp/bin/"*
export PATH="$tmp/bin:$PATH" VNGCLOUD_CLIENT_ID=fake-client VNGCLOUD_CLIENT_SECRET=fake-secret
renew_path=$PATH
export VNGCLOUD_REGION=fake-region VNGCLOUD_PROJECT_ID=fake-project GITHUB_TOKEN=fake-token GITHUB_REPOSITORY=example/repository
export GITHUB_STEP_SUMMARY="$tmp/summary"
mkdir -p "$tmp/original-home/.vngcloud"
printf '%s\n' fake-cache > "$tmp/original-home/.vngcloud/config"
export HOME="$tmp/original-home"
export VNGCLOUD_PROFILE=hostile-profile VNGCLOUD_CONFIG_FILE=hostile-config VNGCLOUD_ENDPOINT=hostile-endpoint
reset_case() {
  find "$tmp/state" -type f ! -name '*-path' -delete
  echo 2026-01-01T00:00:00Z > "$tmp/state/clock"
  echo closed > "$tmp/state/issue"
  echo 0 > "$tmp/state/exit"
  : > "$tmp/state/argv"
  cat > "$tmp/state/get" <<'JSON'
{"Quota":{"ID":"quota-syn-1","Name":"synthetic test","Type":"SYNTHETIC_TEST","Status":"ACTIVE","PackageID":"package-1","StartTime":"2025-12-01T00:00:00Z","EndTime":"2026-01-03T12:00:00+00:00","CreatedAt":"2025-12-01T00:00:00Z","UpdatedAt":"2025-12-01T00:00:00Z","AutoRenew":false,"POC":false,"System":false,"AutoRenewPeriod":0},"Detail":{"ChannelLimits":{"Email":0,"SMS":0,"Slack":null,"Webhook":null,"Telegram":null,"Teams":null},"API":10,"Location":1,"Email":0,"SMS":0,"Slack":0,"Webhook":0,"Telegram":0,"Teams":0,"ClassName":"Basic","TierName":"","Price":0}}
JSON
  cat > "$tmp/state/status" <<'JSON'
{"AsOf":"2025-01-01T00:00:00Z","Complete":true,"Items":[{"Kind":"Synthetic","ClassName":"Basic","PackageName":"","Status":"ACTIVE","Quota":{"ID":"quota-syn-1","Name":"synthetic test","Type":"SYNTHETIC_TEST","Status":"ACTIVE","PackageID":"package-1","StartTime":"2025-12-01T00:00:00Z","EndTime":"2026-01-03T12:00:00Z","CreatedAt":"2025-12-01T00:00:00Z","UpdatedAt":"2025-12-01T00:00:00Z","AutoRenew":false,"POC":false,"System":false,"AutoRenewPeriod":0},"Price":0,"StartTime":"2025-12-01T00:00:00Z","EndTime":"2026-01-03T12:00:00Z","TimeLeftSeconds":-1,"AutoRenew":false,"RenewWindowOpen":true,"NextSafeCommand":"hostile-secret","Error":null}]}
JSON
  cat > "$tmp/state/status-after" <<'JSON'
{"AsOf":"2025-01-01T00:00:00Z","Complete":true,"Items":[{"Kind":"Synthetic","ClassName":"Basic","PackageName":"","Status":"ACTIVE","Quota":{"ID":"quota-syn-1","Name":"synthetic test","Type":"SYNTHETIC_TEST","Status":"ACTIVE","PackageID":"package-1","StartTime":"2025-12-01T00:00:00Z","EndTime":"2026-02-01T00:00:00Z","CreatedAt":"2025-12-01T00:00:00Z","UpdatedAt":"2025-12-01T00:00:00Z","AutoRenew":false,"POC":false,"System":false,"AutoRenewPeriod":0},"Price":0,"StartTime":"2025-12-01T00:00:00Z","EndTime":"2026-02-01T00:00:00Z","TimeLeftSeconds":null,"AutoRenew":false,"RenewWindowOpen":false,"NextSafeCommand":"","Error":null}]}
JSON
  cat > "$tmp/state/outcome" <<'JSON'
{"Sent":"yes","Stage":"confirmed","Error":null,"PreviousEndTime":"2026-01-03T12:00:00+00:00","Quota":{"ID":"quota-syn-1","Name":"synthetic test","Type":"SYNTHETIC_TEST","Status":"ACTIVE","PackageID":"package-1","StartTime":"2025-12-01T00:00:00Z","EndTime":"2026-02-01T00:00:00Z","CreatedAt":"2025-12-01T00:00:00Z","UpdatedAt":"2025-12-01T00:00:00Z","AutoRenew":false,"POC":false,"System":false,"AutoRenewPeriod":0},"OrderID":"hostile-secret","PaymentPending":false,"Renewed":true}
JSON
}
run() {
  run_with_args "$1" "${3:-}" "$2"
}
run_with_args() {
  local expected=$1 wanted=$2 rc=0 diagnostic
  shift 2
  : > "$GITHUB_STEP_SUMMARY"
  PATH="$renew_path" "$bash_path" "$root/scripts/vmonitor-renew.sh" "$@" > "$tmp/output" 2> "$tmp/stderr" || rc=$?
  if [ "$rc" -ne "$expected" ]; then printf 'expected %s, got %s\n' "$expected" "$rc" >&2; exit 1; fi
  [ ! -s "$tmp/stderr" ] || { printf '%s\n' 'unexpected stderr' >&2; exit 1; }
  if [ "$expected" -eq 0 ]; then
    printf '%s\n' 'Synthetic quota: healthy' > "$tmp/expected-summary"
    cp "$tmp/expected-summary" "$tmp/expected-output"
  else
    [ "$(grep -c '^Synthetic quota diagnostic:' "$tmp/output")" -eq 1 ]
    diagnostic=$(grep '^Synthetic quota diagnostic:' "$tmp/output")
    [[ "$diagnostic" =~ ^Synthetic\ quota\ diagnostic:\ stage=environment\ reason=(args|missing-(VNGCLOUD_CLIENT_ID|VNGCLOUD_CLIENT_SECRET|VNGCLOUD_REGION|VNGCLOUD_PROJECT_ID|GITHUB_TOKEN|GITHUB_REPOSITORY)|token-format|repository-format|tool-(curl|jq|python3|mktemp|mkdir|cp|sed|cat|env|rm)|cli-path|tempdir|config-write|summary|signal|copy|clock)$ ||
       "$diagnostic" =~ ^Synthetic\ quota\ diagnostic:\ stage=(hold|parse|threshold)$ ||
       "$diagnostic" =~ ^Synthetic\ quota\ diagnostic:\ stage=(issue-read|issue-write)\ http=(2xx|4xx|5xx|none)$ ||
       "$diagnostic" =~ ^Synthetic\ quota\ diagnostic:\ stage=(quota-read|status-read|renew|readback)\ cli-exit=([0-9]|[1-9][0-9]|1[0-9][0-9]|2[0-4][0-9]|25[0-5])\ code=[A-Za-z]+$ ]]
    [ -z "$wanted" ] || [ "$diagnostic" = "$wanted" ] || { printf '%s\n' 'unexpected diagnostic' >&2; exit 1; }
    printf '%s\n' 'Synthetic quota: failed' "$diagnostic" > "$tmp/expected-summary"
    printf '%s\n' '::error::Synthetic quota renewal failed' 'Synthetic quota: failed' "$diagnostic" > "$tmp/expected-output"
  fi
  sed '/^Synthetic quota: hold open$/d' "$tmp/output" > "$tmp/filtered-output"
  cmp -s "$tmp/expected-output" "$tmp/filtered-output" || { printf '%s\n' 'unexpected stdout' >&2; exit 1; }
  cmp -s "$tmp/expected-summary" "$GITHUB_STEP_SUMMARY" || { printf '%s\n' 'unexpected step summary' >&2; exit 1; }
  ! grep -Eq 'fake-client|fake-secret|fake-project|fake-token|hostile-secret|quota-syn-1|package-1|https://' "$tmp/output" "$tmp/stderr" "$GITHUB_STEP_SUMMARY"
}
assert_count() {
  local expected=$1 pattern=$2 count
  count=$(grep -c -- "$pattern" "$tmp/state/argv" 2>/dev/null || :)
  [ "$count" -eq "$expected" ] || { printf 'wrong count for %s: %s\n' "$pattern" "$count" >&2; exit 1; }
}
assert_hold_and_sends() {
  local hold=$1 sends=$2 writes=$3 count
  cmp -s "$tmp/expected-output" "$tmp/output" || { printf '%s\n' 'unexpected stdout' >&2; exit 1; }
  [ "$(cat "$tmp/state/issue")" = "$hold" ]
  assert_count "$sends" renew-synthetic-quota
  count=$(grep -c -- '-X PATCH' "$tmp/state/curl-argv" || :)
  [ "$count" -eq "$writes" ] || { printf '%s\n' 'unexpected issue write count' >&2; exit 1; }
}
reset_case
run 0 renew
assert_count 1 '^--read-only monitor get-synthetic-quota --output json$'
assert_count 2 '^--read-only monitor quota-status --kind synthetic --output json$'
assert_count 1 '^monitor renew-synthetic-quota --expected-end-time 2026-01-03T12:00:00+00:00 --max-days-left 3 --max-price 0 --output json$'
[ "$(cat "$tmp/state/issue")" = closed ]
! grep -E 'GITHUB_TOKEN|GITHUB_STEP_SUMMARY|VNGCLOUD_PROFILE|VNGCLOUD_CONFIG_FILE|VNGCLOUD_ENDPOINT' "$tmp/state/cli-env"
cp "$tmp/state/status-after" "$tmp/state/status"
jq '.Quota.EndTime = "2026-02-01T00:00:00Z"' "$tmp/state/get" > "$tmp/changed"
mv "$tmp/changed" "$tmp/state/get"
run 0 renew
assert_count 1 renew-synthetic-quota
reset_case
echo open > "$tmp/state/issue"
run 0 check
assert_count 0 'renew-synthetic-quota'
[ "$(cat "$tmp/state/issue")" = open ]
! grep -q -- '-X PATCH' "$tmp/state/curl-argv"
run 1 renew
assert_count 0 'renew-synthetic-quota'
reset_case
printf '%s\n' '{"Sent":"unknown","Stage":"unconfirmed","Error":{"Code":"NotSettled","Operation":"monitor.RenewSyntheticQuota","Status":0},"PreviousEndTime":null,"Quota":null,"OrderID":"","PaymentPending":false,"Renewed":false}' > "$tmp/state/outcome"
echo 1 > "$tmp/state/exit"
run 1 renew
[ "$(cat "$tmp/state/issue")" = open ]
run 1 renew
assert_count 1 'renew-synthetic-quota'
reset_case
printf '%s\n' '{"Sent":"no","Stage":"preflight","Error":{"Code":"InvalidConfig","Operation":"monitor.RenewSyntheticQuota","Status":0},"PreviousEndTime":null,"Quota":null,"OrderID":"","PaymentPending":false,"Renewed":false}' > "$tmp/state/outcome"
echo 2 > "$tmp/state/exit"
run 2 renew
[ "$(cat "$tmp/state/issue")" = closed ]
reset_case
cp "$tmp/state/status" "$tmp/state/status-after"
run 1 renew
[ "$(cat "$tmp/state/issue")" = closed ]
reset_case
for name in VNGCLOUD_CLIENT_ID VNGCLOUD_CLIENT_SECRET VNGCLOUD_REGION VNGCLOUD_PROJECT_ID GITHUB_TOKEN GITHUB_REPOSITORY; do
  reset_case
  (unset "$name"; run 1 check "Synthetic quota diagnostic: stage=environment reason=missing-$name")
  [ ! -e "$tmp/state/curl-argv" ]
done
assert_count 0 renew-synthetic-quota
reset_case
run 1 invalid 'Synthetic quota diagnostic: stage=environment reason=args'
run_with_args 1 'Synthetic quota diagnostic: stage=environment reason=args'
run_with_args 1 'Synthetic quota diagnostic: stage=environment reason=args' check renew
assert_count 0 'renew-synthetic-quota'
for token in 'ghs_fake_header.payload.signature-_' 'fake+opaque/token=~'; do
  reset_case
  GITHUB_TOKEN="$token" run 0 check
done
reset_case
printf -v token '%0520d' 0
GITHUB_TOKEN="ghs_fake_${token}.payload.signature" run 0 check
for token in 'fake-token"hostile-secret' 'fake-token\hostile-secret'; do
  reset_case
  GITHUB_TOKEN="$token" run 1 check 'Synthetic quota diagnostic: stage=environment reason=token-format'
  [ ! -e "$tmp/state/curl-argv" ]
done
for byte in {1..32} 127; do
  reset_case
  printf -v control '%b' "\\$(printf '%03o' "$byte")"
  GITHUB_TOKEN="fake-token${control}hostile-secret" run 1 check 'Synthetic quota diagnostic: stage=environment reason=token-format'
  [ ! -e "$tmp/state/curl-argv" ]
done
for repository in no-slash /repository owner/ owner/repository/extra 'owner/hostile-secret"'; do
  reset_case
  GITHUB_REPOSITORY="$repository" run 1 check 'Synthetic quota diagnostic: stage=environment reason=repository-format'
  [ ! -e "$tmp/state/curl-argv" ]
done
mkdir "$tmp/tools"
for tool in curl jq python3 mktemp mkdir cp sed cat env rm vngcloud; do
  ln -s "$(command -v "$tool")" "$tmp/tools/$tool"
done
for tool in curl jq python3 mktemp mkdir cp sed cat env rm vngcloud; do
  reset_case
  mv "$tmp/tools/$tool" "$tmp/disabled-tool"
  reason=tool-$tool
  [ "$tool" != vngcloud ] || reason=cli-path
  renew_path="$tmp/tools" run 1 check "Synthetic quota diagnostic: stage=environment reason=$reason"
  mv "$tmp/disabled-tool" "$tmp/tools/$tool"
  [ ! -e "$tmp/state/curl-argv" ]
done
reset_case
(
  vngcloud() { :; }
  export -f vngcloud
  run 1 check 'Synthetic quota diagnostic: stage=environment reason=cli-path'
)
for fault in tempdir-fail mkdir-fail config-fail validator-fail copy-fail-get copy-fail-status sed-fail-1p sed-fail-2p sed-fail-3p clock-fail clock-fail-after; do
  reset_case
  : > "$tmp/state/$fault"
  action=check
  case "$fault" in
    tempdir-fail|mkdir-fail) reason=tempdir ;;
    config-fail|validator-fail) reason=config-write ;;
    copy-fail-*|sed-fail-*) reason=copy ;;
    clock-fail) reason=clock ;;
    clock-fail-after) reason=clock; action=renew ;;
  esac
  run 1 "$action" "Synthetic quota diagnostic: stage=environment reason=$reason"
  if [ "$fault" = clock-fail-after ]; then
    [ "$(cat "$tmp/state/issue")" = closed ]
    assert_count 1 renew-synthetic-quota
  else
    assert_count 0 renew-synthetic-quota
  fi
done
# An unwritable summary still gets the exact diagnostic in the log.
reset_case
mkdir "$tmp/summary-directory"
rc=0
GITHUB_STEP_SUMMARY="$tmp/summary-directory" "$bash_path" "$root/scripts/vmonitor-renew.sh" check > "$tmp/output" 2> "$tmp/stderr" || rc=$?
[ "$rc" -eq 1 ] && [ ! -s "$tmp/stderr" ]
printf '%s\n' '::error::Synthetic quota renewal failed' 'Synthetic quota: failed' 'Synthetic quota diagnostic: stage=environment reason=summary' > "$tmp/expected-output"
cmp -s "$tmp/expected-output" "$tmp/output"
change() {
  jq "$2" "$tmp/state/$1" > "$tmp/changed"
  mv "$tmp/changed" "$tmp/state/$1"
}
set_end() {
  change get ".Quota.EndTime = \"$1\""
  change status ".Items[0].EndTime = \"$1\""
}
# Decisions use current time and exact expiry, including boundary fractions.
reset_case
set_end 2026-01-04T00:00:00Z
run 0 renew
assert_count 1 renew-synthetic-quota
reset_case
set_end 2026-01-04T00:00:00.000000001Z
run 0 renew
assert_count 0 renew-synthetic-quota
reset_case
set_end 2026-01-03T00:00:00Z
run 1 check
reset_case
set_end 2026-01-03T00:00:00.000000001Z
change status '.Items[0].TimeLeftSeconds = 0'
run 0 check
reset_case
set_end 2026-01-01T00:00:00Z
run 1 check
reset_case
echo 2026-01-03T12:00:01Z > "$tmp/state/clock"
run 1 check
for filter in \
  '.Quota.Status = "active"' 'del(.Quota.EndTime) | .Quota.EndUTC = "2026-01-03T12:00:00Z"' \
  '.Quota.EndTime = "invalid"' '.Quota.EndTime = "2026-01-03T12:00:01Z"'; do
  reset_case
  change get "$filter"
  run 1 check
done
for filter in \
  '.Complete = false' '.Items = []' '.Items += .Items' '.Items[0].Kind = "synthetic"' \
  '.Items[0].Status = "none"' '.Items[0].Status = "unavailable"' \
  '.Items[0].ClassName = "basic"' 'del(.Items[0].ClassName) | .Items[0].Class = "Basic"' \
  '.Items[0].Price = null' '.Items[0].Price = "0"' '.Items[0].StartTime = null' \
  '.Items[0].EndTime = null' '.Items[0].Error = {}' '.Items[0].TimeLeftSeconds = "0"' \
  '.Items[0].RenewWindowOpen = "true"' '.Items[0].Quota = 10' \
  '.Items[0].Quota = "10"' '.AsOf = "bad"'; do
  reset_case
  change status "$filter"
  run 1 check
done
reset_case
change status '.Items[0].Quota = null'
run 0 check
reset_case
change status-after '.Items[0].Quota = 10'
run 1 renew
reset_case
change status '.Items[0].TimeLeftSeconds = null | .Items[0].AutoRenew = null'
run 0 check
reset_case
change status '.Items[0].RenewWindowOpen = null'
run 0 renew
assert_count 0 renew-synthetic-quota
# All recognized output combinations either close or retain the durable hold.
outcome_case() {
  local sent=$1 stage=$2 code=$3 status=$4 cli_exit=$5 expected_hold=$6 expected_exit=$7 pending=${8:-false} renewed=false error=null
  local diagnostic_code=$code
  [ "$code" != null ] || diagnostic_code=other
  [ "$stage" != confirmed ] || renewed=true
  renewed=${9:-$renewed}
  if [ "$code" != null ]; then
    error=$(jq -cn --arg code "$code" --argjson status "$status" '{Code:$code,Operation:"monitor.RenewSyntheticQuota",Status:$status}')
  fi
  reset_case
  jq -cn --arg sent "$sent" --arg stage "$stage" --argjson error "$error" --argjson renewed "$renewed" --argjson pending "$pending" \
    '{Sent:$sent,Stage:$stage,Error:$error,PreviousEndTime:null,Quota:null,OrderID:"",PaymentPending:$pending,Renewed:$renewed}' > "$tmp/state/outcome"
  echo "$cli_exit" > "$tmp/state/exit"
  run "$expected_exit" renew "Synthetic quota diagnostic: stage=renew cli-exit=$cli_exit code=$diagnostic_code"
  [ "$(cat "$tmp/state/issue")" = "$expected_hold" ]
  assert_count 1 renew-synthetic-quota
}
outcome_case no not-due null 0 0 closed 0
outcome_case unknown confirmed null 0 0 closed 0
outcome_case yes accepted null 0 0 open 1
outcome_case yes unconfirmed NotSettled 200 1 open 1
outcome_case yes unconfirmed NotSettled 200 1 open 1 true
outcome_case unknown unconfirmed NotSettled 0 1 open 1
outcome_case unknown unconfirmed NotSettled 200 1 open 1 true
for code in PriceAboveMax Unpriced QuotaChanged TokenCacheFailed RequestFailed UnexpectedStatus; do outcome_case no preflight "$code" 0 1 closed 1; done
for code in InvalidUsage ReadOnly InvalidConfig; do outcome_case no preflight "$code" 0 2 closed 2; done
for code in NoCredentials ServiceAccountLoginFailed; do outcome_case no preflight "$code" 401 3 closed 3; done
outcome_case no preflight ServiceAccountLoginFailed 0 1 closed 1
outcome_case no preflight NotFound 404 4 closed 4
outcome_case no preflight Unauthorized 401 3 closed 3
for code in BadRequest Forbidden Conflict Throttled ServerError ClientError InvalidResponse ResponseTooLarge; do outcome_case no preflight "$code" 500 1 closed 1; done
outcome_case yes refused Conflict 409 1 closed 1
outcome_case yes refused BadRequest 400 1 closed 1
outcome_case yes refused Forbidden 403 1 closed 1
outcome_case yes refused Throttled 429 1 closed 1
outcome_case yes refused Unauthorized 401 3 closed 3
outcome_case yes refused NotFound 404 4 closed 4
# Pending payment cannot authorize closure or an accepted outcome.
outcome_case no not-due null 0 0 open 1 true
outcome_case yes confirmed null 0 0 open 1 true
outcome_case unknown confirmed null 0 0 open 1 true
outcome_case no preflight PriceAboveMax 0 1 open 1 true
outcome_case no preflight InvalidConfig 0 2 open 2 true
outcome_case no preflight NoCredentials 401 3 open 3 true
outcome_case yes refused Conflict 409 1 open 1 true
outcome_case yes refused Unauthorized 401 3 open 3 true
outcome_case yes refused NotFound 404 4 open 4 true
outcome_case yes accepted null 0 0 open 1 true
# Transmission, settlement, error, and exit evidence must agree.
outcome_case no confirmed null 0 0 open 1
outcome_case yes not-due null 0 0 open 1
outcome_case unknown preflight RequestFailed 0 1 open 1
outcome_case no refused Conflict 409 1 open 1
outcome_case yes confirmed NotSettled 200 1 open 1
outcome_case yes confirmed null 0 1 open 1
outcome_case no not-due null 0 2 open 2
outcome_case no preflight InvalidConfig 0 0 open 1
outcome_case yes refused Unauthorized 401 1 open 1
outcome_case yes confirmed null 0 0 open 1 false false
outcome_case no not-due null 0 0 open 1 false true
outcome_case no preflight InvalidConfig 0 2 open 2 false true
outcome_case yes refused Conflict 409 1 open 1 false true
outcome_case yes accepted null 0 0 open 1 false true
outcome_case yes unconfirmed NotSettled 200 1 open 1 false true
# Contradictory confirmation retains the hold even with unchanged expiry.
reset_case
cp "$tmp/state/status" "$tmp/state/status-after"
change outcome '.PaymentPending = true'
run 1 renew
[ "$(cat "$tmp/state/issue")" = open ]
assert_count 1 renew-synthetic-quota
assert_count 1 quota-status
change outcome '.PaymentPending = false'
run 1 renew
[ "$(cat "$tmp/state/issue")" = open ]
assert_count 1 renew-synthetic-quota
assert_count 1 quota-status
! grep -q 'closed' "$tmp/state/curl-argv"
for filter in \
  'del(.Sent)' '.Sent = "YES"' '.Renewed = false' '.Extra = 1' '.Error = {}' \
  '.Stage = "refused" | .Error = {Code:"Conflict",Operation:"wrong",Status:409}' \
  '.Stage = "refused" | .Error = {Code:"Conflict",Operation:"monitor.RenewSyntheticQuota",Status:409}' ; do
  reset_case
  change outcome "$filter"
  run 1 renew
  [ "$(cat "$tmp/state/issue")" = open ]
done
reset_case
printf '%s\n' 'not JSON fake-secret' > "$tmp/state/outcome"
run 1 renew
[ "$(cat "$tmp/state/issue")" = open ]
reset_case
echo 124 > "$tmp/state/exit"
run 1 renew
[ "$(cat "$tmp/state/issue")" = open ]
reset_case
: > "$tmp/state/invalid-issue"
run 1 check
assert_count 0 renew-synthetic-quota
reset_case
printf '%s\n' 'not JSON' > "$tmp/state/get"
run 1 renew
assert_count 0 renew-synthetic-quota
reset_case
printf '%s\n' 'not JSON' > "$tmp/state/status"
run 1 renew
assert_count 0 renew-synthetic-quota
reset_case
: > "$tmp/state/curl-fail-GET"
run 1 renew
assert_count 0 renew-synthetic-quota
reset_case
: > "$tmp/state/curl-fail-PATCH"
run 1 renew
assert_count 0 renew-synthetic-quota
reset_case
: > "$tmp/state/fail-get-2"
run 1 renew
assert_count 0 renew-synthetic-quota
[ "$(cat "$tmp/state/issue")" = open ]
reset_case
: > "$tmp/state/fail-get-3"
run 1 renew
assert_count 1 renew-synthetic-quota
reset_case
: > "$tmp/state/fail-close"
run 1 renew
[ "$(cat "$tmp/state/issue")" = open ]
reset_case
: > "$tmp/state/interrupt-open"
run 143 renew 'Synthetic quota diagnostic: stage=environment reason=signal'
[ "$(cat "$tmp/state/issue")" = open ]
assert_count 0 renew-synthetic-quota
reset_case
: > "$tmp/state/interrupt"
run 143 renew 'Synthetic quota diagnostic: stage=environment reason=signal'
[ "$(cat "$tmp/state/issue")" = open ]
rm "$tmp/state/interrupt"
cp "$tmp/state/status-after" "$tmp/state/status"
change get '.Quota.EndTime = "2026-02-01T00:00:00Z"'
run 1 renew
assert_count 1 renew-synthetic-quota
# Human reconciliation arms the next run.
echo closed > "$tmp/state/issue"
run 0 renew
assert_count 1 renew-synthetic-quota
# Diagnostics identify the boundary without exposing either captured stream.
reset_case
run 1 invalid 'Synthetic quota diagnostic: stage=environment reason=args'
reset_case
echo open > "$tmp/state/issue"
run 1 renew 'Synthetic quota diagnostic: stage=hold'
reset_case
printf '%s\n' 'not JSON fake-secret hostile-secret' > "$tmp/state/get"
run 1 check 'Synthetic quota diagnostic: stage=parse'
reset_case
set_end 2026-01-03T00:00:00Z
run 1 check 'Synthetic quota diagnostic: stage=threshold'
reset_case
set_end 2026-01-01T00:00:00Z
run 1 check 'Synthetic quota diagnostic: stage=threshold'
reset_case
echo 3 > "$tmp/state/get-exit"
printf '%s\n' '{"error":{"code":"ServiceAccountLoginFailed","message":"fake-secret https://private.invalid/login","status":401}}' > "$tmp/state/get-error"
run 1 check 'Synthetic quota diagnostic: stage=quota-read cli-exit=3 code=ServiceAccountLoginFailed'
reset_case
echo 1 > "$tmp/state/status-exit"
change status '.Complete = false | .Items[0].Status = "unavailable" | .Items[0].Error = {Code:"Forbidden",Operation:"monitor.GetSyntheticQuota",Status:403}'
run 1 check 'Synthetic quota diagnostic: stage=status-read cli-exit=1 code=Forbidden'
reset_case
echo 1 > "$tmp/state/status-exit"
change status '.Complete = false | .Items[0].Status = "unavailable" | .Items[0].Error = {Code:"fake-secret",Operation:"monitor.GetSyntheticQuota",Status:403}'
run 1 check 'Synthetic quota diagnostic: stage=status-read cli-exit=1 code=other'
reset_case
echo 1 > "$tmp/state/status-exit"
printf '%s\n' '{"error":{"code":"QuotaStatusIncomplete","message":"fake-secret","operation":"monitor.GetQuotaStatus"}}' > "$tmp/state/status-error"
run 1 check 'Synthetic quota diagnostic: stage=status-read cli-exit=1 code=QuotaStatusIncomplete'
reset_case
change outcome '.Sent = "unknown" | .Stage = "unconfirmed" | .Renewed = false | .Error = {Code:"NotSettled",Operation:"monitor.RenewSyntheticQuota",Status:0}'
echo 1 > "$tmp/state/exit"
run 1 renew 'Synthetic quota diagnostic: stage=renew cli-exit=1 code=NotSettled'
[ "$(cat "$tmp/state/issue")" = open ]
reset_case
echo 4 > "$tmp/state/status-after-exit"
printf '%s\n' '{"error":{"code":"NotFound","message":"fake-project quota-syn-1","status":404}}' > "$tmp/state/status-after-error"
run 1 renew 'Synthetic quota diagnostic: stage=readback cli-exit=4 code=NotFound'
[ "$(cat "$tmp/state/issue")" = closed ]
reset_case
cp "$tmp/state/status" "$tmp/state/status-after"
run 1 renew 'Synthetic quota diagnostic: stage=readback cli-exit=0 code=other'
for method in GET PATCH; do
  for entry in '200 2xx' '403 4xx' '503 5xx' '000 none' '302 none' 'hostile-secret none'; do
    reset_case
    read -r http class <<< "$entry"
    echo "$http" > "$tmp/state/http-$method"
    if [ "$method" = GET ]; then
      : > "$tmp/state/invalid-issue"
      run 1 check "Synthetic quota diagnostic: stage=issue-read http=$class"
    else
      : > "$tmp/state/ignore-patch"
      run 1 renew "Synthetic quota diagnostic: stage=issue-write http=$class"
      assert_count 0 renew-synthetic-quota
    fi
  done
done
reset_case
: > "$tmp/state/fail-get-2"
run 1 renew 'Synthetic quota diagnostic: stage=issue-read http=5xx'
reset_case
change outcome '.Sent = "no" | .Stage = "preflight" | .Renewed = false | .Error = {Code:"NoCredentials",Operation:"monitor.RenewSyntheticQuota",Status:0}'
echo 3 > "$tmp/state/exit"
: > "$tmp/state/fail-close"
run 3 renew 'Synthetic quota diagnostic: stage=issue-write http=5xx'
[ "$(cat "$tmp/state/issue")" = open ]
# Each documented code is accepted only as one complete structured value.
for code in PriceAboveMax Unpriced QuotaChanged NotSettled Conflict BadRequest Forbidden Throttled Unauthorized NotFound RequestFailed InvalidUsage ReadOnly InvalidConfig NoCredentials ServiceAccountLoginFailed TokenCacheFailed UnexpectedStatus ServerError ClientError InvalidResponse ResponseTooLarge QuotaStatusIncomplete; do
  reset_case
  echo 1 > "$tmp/state/get-exit"
  jq -cn --arg code "$code" '{error:{code:$code,message:"fake-secret hostile-secret",status:500,operation:"monitor.GetSyntheticQuota"}}' > "$tmp/state/get-error"
  run 1 check "Synthetic quota diagnostic: stage=quota-read cli-exit=1 code=$code"
done
for payload in \
  '{"error":{"code":"fake-secret","message":"hostile-secret"}}' \
  '{"error":{"code":"Forbidden fake-secret","message":"hostile-secret"}}' \
  '{"error":{"code":"forbidden","message":"hostile-secret"}}' \
  '{"error":{"code":"Forbidden\n","message":"hostile-secret"}}' \
  '{"error":{"code":"Forbidden\u0000","message":"hostile-secret"}}' \
  '{"error":{"code":"Forbidden\n::error::fake-secret","message":"hostile-secret"}}' \
  '{"error":{"code":401,"message":"hostile-secret"}}' \
  '{"error":{"code":["Forbidden"],"message":"hostile-secret"}}' \
  '{"error":{"code":"Forbidden","code":"Unauthorized","message":"fake-secret"}}' \
  '{"error":{"code":"Forbidden","message":"fake-secret","extra":"hostile-secret"}}' \
  '{"nested":{"error":{"code":"Forbidden","message":"fake-secret"}}}' \
  '{"error":{"code":"Forbidden","message":"fake-secret"}} {"error":{"code":"Unauthorized","message":"fake-secret"}}' \
  'hostile-secret {"error":{"code":"Forbidden","message":"fake-secret"}}'; do
  reset_case
  echo 255 > "$tmp/state/get-exit"
  printf '%s\n' "$payload" > "$tmp/state/get-error"
  printf '%s\n' 'fake-client fake-secret fake-project fake-token hostile-secret https://private.invalid/quota' > "$tmp/state/get"
  run 1 check 'Synthetic quota diagnostic: stage=quota-read cli-exit=255 code=other'
done
for code in fake-secret 'Forbidden fake-secret' 'Forbidden\n::error::fake-secret' 'Forbidden\n' 'Forbidden\u0000'; do
  reset_case
  change outcome ".Sent = \"no\" | .Stage = \"preflight\" | .Renewed = false | .Error = {Code:\"$code\",Operation:\"monitor.RenewSyntheticQuota\",Status:0}"
  echo 1 > "$tmp/state/exit"
  run 1 renew 'Synthetic quota diagnostic: stage=renew cli-exit=1 code=other'
  [ "$(cat "$tmp/state/issue")" = open ]
done
reset_case
printf '%s\n' '{"Error":{"Code":"Forbidden","Code":"Unauthorized","Operation":"monitor.RenewSyntheticQuota","Status":403}}' > "$tmp/state/outcome"
echo 1 > "$tmp/state/exit"
run 1 renew 'Synthetic quota diagnostic: stage=renew cli-exit=1 code=other'
reset_case
echo 124 > "$tmp/state/exit"
run 1 renew 'Synthetic quota diagnostic: stage=renew cli-exit=124 code=other'
# Structured codes cannot carry terminal controls or oversized text into diagnostics.
for code in '"Forbidden\u001b[31m"' '("x" * 65536)'; do
  for fixture in status outcome status-after; do
    reset_case
    if [ "$fixture" = outcome ]; then
      change outcome ".Sent = \"no\" | .Stage = \"preflight\" | .Renewed = false | .Error = {Code:$code,Operation:\"monitor.RenewSyntheticQuota\",Status:0}"
      echo 1 > "$tmp/state/exit"
      run 1 renew 'Synthetic quota diagnostic: stage=renew cli-exit=1 code=other'
      assert_hold_and_sends open 1 1
    else
      change "$fixture" ".Complete = false | .Items[0].Status = \"unavailable\" | .Items[0].Error = {Code:$code,Operation:\"monitor.GetSyntheticQuota\",Status:403}"
      echo 1 > "$tmp/state/$fixture-exit"
      if [ "$fixture" = status ]; then
        run 1 check 'Synthetic quota diagnostic: stage=status-read cli-exit=1 code=other'
        assert_hold_and_sends closed 0 0
      else
        run 1 renew 'Synthetic quota diagnostic: stage=readback cli-exit=1 code=other'
        assert_hold_and_sends closed 1 2
      fi
    fi
  done
done
# Exercise shell-generated stderr with an explicit numeric CLI exit.
for fixture in get status outcome status-after; do
  reset_case
  exit_file=$fixture-exit
  [ "$fixture" != outcome ] || exit_file=exit
  printf '%s\n' $'1\n::error::fake-secret' > "$tmp/state/$fixture-invalid-exit"
  echo 2 > "$tmp/state/$exit_file"
  case "$fixture" in
    get)
      run 1 check 'Synthetic quota diagnostic: stage=quota-read cli-exit=2 code=other'
      assert_hold_and_sends closed 0 0 ;;
    status)
      run 1 check 'Synthetic quota diagnostic: stage=status-read cli-exit=2 code=other'
      assert_hold_and_sends closed 0 0 ;;
    outcome)
      run 2 renew 'Synthetic quota diagnostic: stage=renew cli-exit=2 code=other'
      assert_hold_and_sends open 1 1 ;;
    status-after)
      run 1 renew 'Synthetic quota diagnostic: stage=readback cli-exit=2 code=other'
      assert_hold_and_sends closed 1 2 ;;
  esac
done
hostile=$'\n::error::fake-secret\n\033[31mhostile-secret'
for action in check renew; do
  for field in state response body; do
    reset_case
    case "$field" in
      state) jq -cn --arg state "closed$hostile" '{state:$state}' > "$tmp/state/issue-response" ;;
      response) printf '%s\n' "$hostile" > "$tmp/state/issue-response" ;;
      body) printf '%s\n' "$hostile" > "$tmp/state/issue-body" ;;
    esac
    if [ "$field" = body ]; then
      run 0 "$action"
      if [ "$action" = check ]; then assert_hold_and_sends closed 0 0; else assert_hold_and_sends closed 1 2; fi
    else
      run 1 "$action" 'Synthetic quota diagnostic: stage=issue-read http=2xx'
      assert_hold_and_sends closed 0 0
    fi
  done
done
for method in GET PATCH; do
  for stream in http error; do
    reset_case
    class=none
    hold=closed
    if [ "$stream" = http ]; then
      printf '%s\n' "200$hostile" > "$tmp/state/http-$method"
      [ "$method" != PATCH ] || hold=open
    else
      printf '%s\n' "$hostile" > "$tmp/state/curl-error"
      : > "$tmp/state/curl-fail-$method"
      class=5xx
    fi
    if [ "$method" = GET ]; then
      run 1 renew "Synthetic quota diagnostic: stage=issue-read http=$class"
      assert_hold_and_sends closed 0 0
    else
      run 1 renew "Synthetic quota diagnostic: stage=issue-write http=$class"
      assert_hold_and_sends "$hold" 0 1
    fi
  done
done
printf '%s\n' 'vMonitor renewal tests passed' 
