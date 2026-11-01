#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/state"
command -v python3 > "$tmp/state/python-path"
cat > "$tmp/bin/python3" <<'SH'
#!/usr/bin/env bash
state=$(dirname "$0")/../state
if [ "${1:-}" = -I ] && [ "${2:-}" = -c ]; then
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
while [ "$#" -gt 0 ]; do
  case "$1" in
    -X) method=$2; shift 2 ;;
    -o) out=$2; shift 2 ;;
    --data) data=$2; shift 2 ;;
    *) shift ;;
  esac
done
[ ! -f "$state/curl-fail-$method" ] || exit 22
if [ "$method" = GET ]; then
  count=0
  [ ! -f "$state/get-count" ] || count=$(cat "$state/get-count")
  count=$((count + 1))
  echo "$count" > "$state/get-count"
  [ ! -f "$state/fail-get-$count" ] || exit 22
fi
if [ "$method" = PATCH ]; then
  case "$data" in *closed*) [ ! -f "$state/fail-close" ] || exit 22 ;; esac
  case "$data" in *open*) echo open > "$state/issue" ;; *closed*) echo closed > "$state/issue" ;; *) exit 1 ;; esac
  case "$data" in *open*) if [ -f "$state/interrupt-open" ]; then kill -TERM "$PPID"; exit 1; fi ;; esac
fi
if [ -f "$state/invalid-issue" ]; then printf '%s\n' 'invalid' > "$out"; else
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
  *get-synthetic-quota*) cat "$state/get" ;;
  *quota-status*)
    if [ -f "$state/after-send" ] && [ -f "$state/status-after" ]; then cat "$state/status-after"; else cat "$state/status"; fi ;;
  *renew-synthetic-quota*)
    [ "$(cat "$state/issue")" = open ] && [ "$(cat "$state/get-count")" -ge 2 ] || exit 99
    : > "$state/after-send"
    if [ -f "$state/interrupt" ]; then kill -TERM "$PPID"; exit 1; fi
    cat "$state/outcome"
    exit "$(cat "$state/exit")" ;;
  *) exit 99 ;;
esac
SH
chmod +x "$tmp/bin/"*
export PATH="$tmp/bin:$PATH" VNGCLOUD_CLIENT_ID=fake-client VNGCLOUD_CLIENT_SECRET=fake-secret
export VNGCLOUD_REGION=fake-region VNGCLOUD_PROJECT_ID=fake-project GITHUB_TOKEN=fake-token GITHUB_REPOSITORY=example/repository
mkdir -p "$tmp/original-home/.vngcloud"
printf '%s\n' fake-cache > "$tmp/original-home/.vngcloud/config"
export HOME="$tmp/original-home"
export VNGCLOUD_PROFILE=hostile-profile VNGCLOUD_CONFIG_FILE=hostile-config VNGCLOUD_ENDPOINT=hostile-endpoint
reset_case() {
  find "$tmp/state" -type f ! -name python-path -delete
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
  local expected=$1 action=$2 rc=0
  bash "$root/scripts/vmonitor-renew.sh" "$action" > "$tmp/output" 2>&1 || rc=$?
  if [ "$rc" -ne "$expected" ]; then printf 'expected %s, got %s\n' "$expected" "$rc" >&2; exit 1; fi
  if grep -Ev '^($|Synthetic quota: healthy|Synthetic quota: hold open|Synthetic quota: failed|::error::Synthetic quota renewal failed)$' "$tmp/output"; then exit 1; fi
}
assert_count() {
  local expected=$1 pattern=$2 count
  count=$(grep -c -- "$pattern" "$tmp/state/argv" 2>/dev/null || :)
  [ "$count" -eq "$expected" ] || { printf 'wrong count for %s: %s\n' "$pattern" "$count" >&2; exit 1; }
}
reset_case
run 0 renew
assert_count 1 '^--read-only monitor get-synthetic-quota --output json$'
assert_count 2 '^--read-only monitor quota-status --kind synthetic --output json$'
assert_count 1 '^monitor renew-synthetic-quota --expected-end-time 2026-01-03T12:00:00+00:00 --max-days-left 3 --max-price 0 --output json$'
[ "$(cat "$tmp/state/issue")" = closed ]
! grep -E 'GITHUB_TOKEN|VNGCLOUD_PROFILE|VNGCLOUD_CONFIG_FILE|VNGCLOUD_ENDPOINT' "$tmp/state/cli-env"
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
saved_secret=$VNGCLOUD_CLIENT_SECRET
unset VNGCLOUD_CLIENT_SECRET
run 1 check
export VNGCLOUD_CLIENT_SECRET=$saved_secret
assert_count 0 renew-synthetic-quota
reset_case
run 1 invalid
assert_count 0 'renew-synthetic-quota'
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
  local sent=$1 stage=$2 code=$3 status=$4 cli_exit=$5 expected_hold=$6 expected_exit=$7 renewed=false error=null
  [ "$stage" != confirmed ] || renewed=true
  if [ "$code" != null ]; then
    error=$(jq -cn --arg code "$code" --argjson status "$status" '{Code:$code,Operation:"monitor.RenewSyntheticQuota",Status:$status}')
  fi
  reset_case
  jq -cn --arg sent "$sent" --arg stage "$stage" --argjson error "$error" --argjson renewed "$renewed" \
    '{Sent:$sent,Stage:$stage,Error:$error,PreviousEndTime:null,Quota:null,OrderID:"",PaymentPending:false,Renewed:$renewed}' > "$tmp/state/outcome"
  echo "$cli_exit" > "$tmp/state/exit"
  run "$expected_exit" renew
  [ "$(cat "$tmp/state/issue")" = "$expected_hold" ]
  assert_count 1 renew-synthetic-quota
}
outcome_case no not-due null 0 0 closed 0
outcome_case unknown confirmed null 0 0 closed 0
outcome_case yes accepted null 0 0 open 1
outcome_case yes unconfirmed NotSettled 200 1 open 1
outcome_case unknown unconfirmed NotSettled 0 1 open 1
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
run 143 renew
[ "$(cat "$tmp/state/issue")" = open ]
assert_count 0 renew-synthetic-quota
reset_case
: > "$tmp/state/interrupt"
run 143 renew
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
printf '%s\n' 'vMonitor renewal tests passed' 
