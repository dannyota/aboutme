#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/scratch"
export TMPDIR=$work/scratch
printf '[default]\naws_access_key_id = fixture\naws_secret_access_key = fixture\n' >"$work/credentials"

chmod 0600 "$work/credentials"

cat >"$work/bin/vngcloud" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
mock_root=$(cd "$(dirname "$0")/.." && pwd)
mock_log=$mock_root/native.log
mock_store=$mock_root/object
mock_case=$(cat "$mock_root/case" 2>/dev/null || printf success)
printf '%q ' "$@" >>"$mock_log"
printf '\n' >>"$mock_log"
[[ -n ${VNGCLOUD_CONFIG_FILE:-} && -f $VNGCLOUD_CONFIG_FILE && ! -L $VNGCLOUD_CONFIG_FILE ]]
[[ $VNGCLOUD_CONFIG_FILE == "$mock_root"/scratch/aboutme-drill-native.* ]]
[[ $(stat -c '%a' -- "$VNGCLOUD_CONFIG_FILE") == 600 ]]
[[ $(stat -c '%s' -- "$VNGCLOUD_CONFIG_FILE") == 10 ]]
[[ $(cat "$VNGCLOUD_CONFIG_FILE") == '[default]' ]]
printf '%s\n' "$VNGCLOUD_CONFIG_FILE" >>"$mock_root/config-paths"
[[ -z ${HOME+x} ]]
for name in AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_PROFILE \
  AWS_DEFAULT_PROFILE AWS_ROLE_ARN AWS_WEB_IDENTITY_TOKEN_FILE \
  AWS_CONTAINER_CREDENTIALS_RELATIVE_URI AWS_CONTAINER_CREDENTIALS_FULL_URI \
  AWS_CONTAINER_AUTHORIZATION_TOKEN AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE \
  AWS_CONFIG_FILE AWS_SHARED_CREDENTIALS_FILE VNGCLOUD_PROFILE VNGCLOUD_API_KEY \
  VNGCLOUD_AUTH_TOKEN VNGCLOUD_READ_ONLY VNGCLOUD_OUTPUT VNGCLOUD_DEBUG \
  VNGCLOUD_REGION VNGCLOUD_PROJECT_ID VNGCLOUD_QUERY HTTP_PROXY HTTPS_PROXY \
  ALL_PROXY NO_PROXY http_proxy https_proxy all_proxy no_proxy; do
  [[ -z ${!name+x} ]] || { printf 'ambient variable survived: %s\n' "$name" >&2; exit 2; }
done
[[ $1 == --profile && $2 == default && $3 == --output && $4 == json && $5 == storage ]]
body= destination= region= credentials= bucket= key= bound= transfer_timeout=
shift 5
operation=$1
shift
while (($#)); do
  case $1 in
    --body) body=$2 ;;
    --destination) destination=$2 ;;
    --s3-region) region=$2 ;;
    --s3-credentials-file) credentials=$2 ;;
    --bucket) bucket=$2 ;;
    --key) key=$2 ;;
    --max-download-bytes) bound=$2 ;;
    --transfer-timeout) transfer_timeout=$2 ;;
    *) printf 'unexpected flag: %s\n' "$1" >&2; exit 2 ;;
  esac
  shift 2
done
[[ $region == HCM04 && $credentials == "$mock_root/credentials" ]]
[[ $bucket == aboutme-tfstate && $key == restore-drill/v1/* && $transfer_timeout == 60s ]]
case $operation in
  put-object)
    [[ -n $body && -z $destination && -z $bound ]]
    [[ ! -e $mock_root/mutate-input ]] || printf '{"changed":true}\n' >"$(cat "$mock_root/mutate-input")"
    case $mock_case in
      conflict | empty-message-conflict)
        message='object already exists'
        [[ $mock_case != empty-message-conflict ]] || message=
        printf '{"error":{"code":"ObjectExists","message":"%s","status":412,"operation":"storage.PutObject"}}\n' "$message" >&2
        exit 1
        ;;
      invalid-conflict-*)
        cat "$mock_root/error-envelope" >&2
        exit "$(cat "$mock_root/error-exit")"
        ;;
      completed-put-error | completed-put-cleanup-error)
        cp -- "$body" "$mock_store"
        detail='metadata output failed'
        [[ $mock_case != completed-put-cleanup-error ]] || detail='private snapshot cleanup failed'
        printf '{"error":{"code":"RequestFailed","message":"upload completed; %s; do not retry the transfer"}}\n' "$detail" >&2
        exit 1
        ;;
      lost-match | lost-mismatch | lost-unreadable)
        cp -- "$body" "$mock_store"
        [[ $mock_case != lost-mismatch ]] || printf '{"other":true}\n' >"$mock_store"
        printf '{"error":{"code":"ObjectWriteUnconfirmed","message":"connection closed","operation":"storage.PutObject"}}\n' >&2
        exit 1
        ;;
      *) cp -- "$body" "$mock_store" ;;
    esac
    ;;
  get-object)
    [[ -n $destination && -z $body && $bound == 1048576 ]]
    [[ ! -e $destination && ! -L $destination ]]
    [[ -d $(dirname -- "$destination") && $(stat -c '%a' -- "$(dirname -- "$destination")") == 700 ]]
    [[ $mock_case != lost-unreadable ]] || {
      printf '{"error":{"code":"AccessDenied","status":403,"operation":"storage.GetObject"}}\n' >&2
      exit 1
    }
    [[ $(stat -c '%s' -- "$mock_store") -le $bound ]] || exit 1
    cp -- "$mock_store" "$destination"
    [[ $mock_case != completed-get-error ]] || {
      printf '{"error":{"code":"RequestFailed","message":"download completed; metadata output failed; do not retry the transfer"}}\n' >&2
      exit 1
    }
    if [[ $mock_case == race-* ]]; then
      race_output=$(cat "$mock_root/race-output")
      case $mock_case in
        race-file) printf 'racer\n' >"$race_output" ;;
        race-symlink) ln -s "$mock_root/race-target" "$race_output" ;;
        race-directory) mkdir "$race_output" ;;
      esac
    fi
    ;;
  *) printf 'unexpected operation: %s\n' "$operation" >&2; exit 2 ;;
esac
STUB
cat >"$work/bin/aws" <<'STUB'
#!/usr/bin/env bash
mock_root=$(cd "$(dirname "$0")/.." && pwd)
printf 'unexpected AWS call\n' >>"$mock_root/aws-called"
exit 2
STUB
chmod +x "$work/bin/vngcloud" "$work/bin/aws"

export PATH=$work/bin:$PATH
export DRILL_STATE_CREDENTIALS_FILE=$work/credentials
export AWS_ACCESS_KEY_ID=poison
export AWS_SECRET_ACCESS_KEY=poison
export AWS_SESSION_TOKEN=poison
export AWS_PROFILE=poison
export AWS_DEFAULT_PROFILE=poison
export AWS_ROLE_ARN=poison
export AWS_WEB_IDENTITY_TOKEN_FILE=$work/poison-token
export AWS_CONTAINER_CREDENTIALS_RELATIVE_URI=/poison
export AWS_CONTAINER_CREDENTIALS_FULL_URI=http://127.0.0.1/poison
export AWS_CONTAINER_AUTHORIZATION_TOKEN=poison
export AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE=$work/poison-auth
export AWS_CONFIG_FILE=$work/poison-config AWS_SHARED_CREDENTIALS_FILE=$work/poison-credentials
export VNGCLOUD_CONFIG_FILE=$work/poison-config VNGCLOUD_PROFILE=poison
export VNGCLOUD_API_KEY=poison VNGCLOUD_AUTH_TOKEN=poison VNGCLOUD_READ_ONLY=true
export VNGCLOUD_OUTPUT=text VNGCLOUD_DEBUG=true VNGCLOUD_REGION=poison
export VNGCLOUD_PROJECT_ID=poison VNGCLOUD_QUERY=poison
export HTTP_PROXY=http://127.0.0.1/poison HTTPS_PROXY=http://127.0.0.1/poison
export ALL_PROXY=http://127.0.0.1/poison NO_PROXY=poison
export http_proxy=http://127.0.0.1/poison https_proxy=http://127.0.0.1/poison
export all_proxy=http://127.0.0.1/poison no_proxy=poison
# shellcheck source=../scripts/drill-journal.sh
. "$repo_root/deploy/vn/scripts/drill-journal.sh"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
expect_fail() { "$@" >/dev/null 2>&1 && fail "command succeeded: $*" || return 0; }
reset_mock() {
  : >"$work/native.log"
  printf 'success\n' >"$work/case"
  rm -f -- "$work/object" "$work/mutate-input" "$work/race-output"
}
set_case() { printf '%s\n' "$1" >"$work/case"; }

record=$work/record.json
printf '{"kind":"fixture","text":"line one\\nline two"}\n' >"$record"

reset_mock
drill_journal_put_create 'locks/1.json' "$record"
cmp -s -- "$record" "$work/object" || fail 'successful put changed bytes'
grep -q -- '--key restore-drill/v1/locks/1.json' "$work/native.log" || fail 'put used wrong key'
grep -q -- '--s3-region HCM04' "$work/native.log" || fail 'region missing'
grep -q -- '--profile default --output json storage' "$work/native.log" || fail 'native invocation missing'
grep -q -- '--transfer-timeout 60s' "$work/native.log" || fail 'transfer timeout missing'

for key in \
  '../locks/1.json' \
  'locks/01.json' \
  'locks/1.json/extra' \
  'runs/not-a-uuid/1.json' \
  'runs/123e4567-e89b-42d3-a456-426614174000/../1.json' \
  'restore-drill/v1/locks/1.json' \
  'locks/1.json --debug'; do
  reset_mock
  expect_fail drill_journal_put_create "$key" "$record"
  [[ ! -s $work/native.log ]] || fail "invalid key reached native CLI: $key"
done

reset_mock
DRILL_JOURNAL_BUCKET=wrong DRILL_JOURNAL_PREFIX=wrong DRILL_JOURNAL_ENDPOINT=http://wrong \
  DRILL_JOURNAL_REGION=wrong DRILL_JOURNAL_MAX_BYTES=99999999 \
  drill_journal_put_create \
  'runs/123e4567-e89b-42d3-a456-426614174000/1.json' "$record"
grep -q -- '--bucket aboutme-tfstate' "$work/native.log" || fail 'caller changed bucket scope'
grep -q -- '--key restore-drill/v1/runs/' "$work/native.log" || fail 'caller changed key scope'
drill_journal_put_create \
  'runs/123e4567-e89b-42d3-a456-426614174000/recovery/2.json' "$record"

bad=$work/bad.json
printf 'not json\n' >"$bad"
reset_mock
expect_fail drill_journal_put_create 'releases/1.json' "$bad"
[[ ! -s $work/native.log ]] || fail 'invalid JSON reached native CLI'
ln -s "$record" "$work/link.json"
expect_fail drill_journal_put_create 'releases/1.json' "$work/link.json"
large=$work/large.json
truncate -s 1048577 "$large"
DRILL_JOURNAL_MAX_BYTES=99999999 \
  expect_fail drill_journal_put_create 'releases/1.json' "$large"

for case_name in conflict empty-message-conflict; do
  reset_mock
  set_case "$case_name"
  result=0
  drill_journal_put_create 'releases/1.json' "$record" >/dev/null 2>&1 || result=$?
  [[ $result == 3 ]] || fail "$case_name did not return definite conflict"
  [[ $(wc -l <"$work/native.log") -eq 1 ]] || fail 'conflict caused another request'
done

for envelope in \
  '{"error":{"code":"ObjectExists","status":412,"operation":"storage.GetObject"}}' \
  '{"error":{"code":"Other","status":412,"operation":"storage.PutObject"}}' \
  '{"error":{"code":"ObjectExists","status":"412","operation":"storage.PutObject"}}' \
  '{"error":{"code":"ObjectExists","status":409,"operation":"storage.PutObject"}}' \
  '{"error":{"code":"ObjectExists","operation":"storage.PutObject"}}' \
  '{"error":{"message":"ObjectExists 412 storage.PutObject"}}' \
  '{"error":{"code":["ObjectExists"],"status":412,"operation":"storage.PutObject"}}' \
  '{"error":{"code":"ObjectExists","status":412,"operation":["storage.PutObject"]}}' \
  '{"error":{"code":"ObjectExists","status":412,"operation":"storage.PutObject"}} {}' \
  '{"error":{"code":"ObjectExists","status":412,"operation":"storage.PutObject"}} trailing' \
  'proxy cached ObjectExists 412 storage.PutObject' \
  '{broken'; do
  reset_mock
  set_case invalid-conflict-envelope
  printf '%s\n' "$envelope" >"$work/error-envelope"
  printf '1\n' >"$work/error-exit"
  expect_fail drill_journal_put_create 'releases/1.json' "$record"
  [[ $(grep -c 'get-object' "$work/native.log") -eq 1 ]] || fail 'invalid envelope classified as conflict'
  [[ $(grep -c 'put-object' "$work/native.log") -eq 1 ]] || fail 'invalid envelope retried PUT'
done
for exit_code in 2 3 4 5 124 137 255; do
  reset_mock
  set_case invalid-conflict-exit
  printf '%s\n' '{"error":{"code":"ObjectExists","status":412,"operation":"storage.PutObject"}}' >"$work/error-envelope"
  printf '%s\n' "$exit_code" >"$work/error-exit"
  expect_fail drill_journal_put_create 'releases/1.json' "$record"
  [[ $(grep -c 'get-object' "$work/native.log") -eq 1 ]] || fail 'wrong exit classified as conflict'
done

for case_name in completed-put-error completed-put-cleanup-error; do
  reset_mock
  set_case "$case_name"
  drill_journal_put_create 'releases/2.json' "$record"
  [[ $(grep -c 'put-object' "$work/native.log") -eq 1 ]] || fail 'completed PUT retried'
  [[ $(grep -c 'get-object' "$work/native.log") -eq 1 ]] || fail 'completed PUT not reconciled'
done

reset_mock
set_case lost-match
drill_journal_put_create 'releases/2.json' "$record"
[[ $(wc -l <"$work/native.log") -eq 2 ]] || fail 'lost response was not read back once'
[[ $(grep -c 'put-object' "$work/native.log") -eq 1 ]] || fail 'ambiguous put was retried'
[[ $(grep -c 'get-object' "$work/native.log") -eq 1 ]] || fail 'ambiguous put read-back count differs'

for case_name in lost-mismatch lost-unreadable; do
  reset_mock
  set_case "$case_name"
  expect_fail drill_journal_put_create 'releases/3.json' "$record"
  [[ $(grep -c 'put-object' "$work/native.log") -eq 1 ]] || fail "$case_name retried put"
  [[ $(grep -c 'get-object' "$work/native.log") -eq 1 ]] || fail "$case_name did not read back once"
  grep -q 'delete-object' "$work/native.log" && fail "$case_name deleted a record"
done

reset_mock
cp -- "$record" "$work/object"
out=$work/read.json
drill_journal_get 'runs/123e4567-e89b-42d3-a456-426614174000/4.json' "$out"
cmp -s -- "$record" "$out" || fail 'get changed bytes'
[[ $(stat -c '%a' -- "$out") == 600 ]] || fail 'get output is not private'

reset_mock
set_case lost-unreadable
expect_fail drill_journal_get 'locks/4.json' "$work/denied.json"
[[ ! -e $work/denied.json ]] || fail 'denied get created output'

reset_mock
cp -- "$record" "$work/object"
expect_fail drill_journal_get 'locks/5.json' "$work/read.json"
[[ -e $work/read.json ]] || fail 'existing output was changed'

for json in false null 17 '"scalar"'; do
  reset_mock
  printf '%s\n' "$json" >"$work/value.json"
  drill_journal_put_create 'releases/6.json' "$work/value.json"
  rm -f -- "$work/value-read.json"
  drill_journal_get 'releases/6.json' "$work/value-read.json"
  cmp -s -- "$work/value.json" "$work/value-read.json" || fail "$json JSON bytes changed"
done
for contents in '{} {}' '{} trailing'; do
  reset_mock
  printf '%s\n' "$contents" >"$work/multiple.json"
  expect_fail drill_journal_put_create 'releases/7.json' "$work/multiple.json"
  [[ ! -s $work/native.log ]] || fail 'invalid JSON sequence reached native CLI'
done

reset_mock
printf '%s\n' "$record" >"$work/mutate-input"
drill_journal_put_create 'releases/8.json' "$record"
printf '{"changed":true}\n' >"$work/changed.json"
cmp -s -- "$work/object" "$work/changed.json" && fail 'upload used mutable input path'

for race in file symlink directory; do
  reset_mock
  set_case "race-$race"
  cp -- "$record" "$work/object"
  raced=$work/raced-$race
  printf '%s\n' "$raced" >"$work/race-output"
  expect_fail drill_journal_get 'locks/9.json' "$raced"
  case $race in
    file) [[ $(cat "$raced") == racer ]] || fail 'raced file was overwritten' ;;
    symlink) [[ -L $raced ]] || fail 'raced symlink was replaced' ;;
    directory) [[ -d $raced ]] || fail 'raced directory was replaced' ;;
  esac
done

reset_mock
printf '{}' >"$work/boundary.json"
truncate -s 1048576 "$work/boundary.json"
# JSON permits whitespace padding but not NUL padding.
tr '\000' ' ' <"$work/boundary.json" >"$work/boundary-valid.json"
drill_journal_put_create 'releases/10.json' "$work/boundary-valid.json"
drill_journal_get 'releases/10.json' "$work/boundary-read.json"
cmp -s -- "$work/boundary-valid.json" "$work/boundary-read.json" || fail '1 MiB boundary bytes changed'
reset_mock
: >"$work/empty.json"
expect_fail drill_journal_put_create 'releases/11.json' "$work/empty.json"
[[ ! -s $work/native.log ]] || fail 'empty JSON reached native CLI'

reset_mock
set_case completed-get-error
cp -- "$record" "$work/object"
expect_fail drill_journal_get 'locks/10.json' "$work/completed-get.json"
[[ ! -e $work/completed-get.json ]] || fail 'nonzero GET published output'

for contents in '{} {}' '{} trailing' ''; do
  reset_mock
  printf '%s' "$contents" >"$work/object"
  expect_fail drill_journal_get 'locks/11.json' "$work/invalid-get.json"
  [[ ! -e $work/invalid-get.json ]] || fail 'invalid GET published output'
done
reset_mock
truncate -s 1048577 "$work/object"
expect_fail drill_journal_get 'locks/12.json' "$work/oversized-get.json"
[[ ! -e $work/oversized-get.json ]] || fail 'oversized GET published output'

while IFS= read -r config; do
  [[ ! -e $config ]] || fail 'generated config leaked'
done <"$work/config-paths"
[[ ! -e $work/aws-called ]] || fail 'journal invoked AWS'
[[ -z $(find "$work/scratch" -mindepth 1 -print -quit) ]] || fail 'journal scratch paths leaked'
[[ -z $(find "$work" -maxdepth 1 -name '.drill-journal-read.*' -print -quit) ]] || fail 'download scratch paths leaked'
grep -Eq -- 'delete-object|--replace|--yes|--if-none-match|--content-type|--region|--project-id|--debug|--query|--endpoint-url' "$work/native.log" && fail 'unsupported or unsafe native operation'

# Drill summaries must distinguish guest denial from app reconciliation and
# must never publish epoch, receipt, or restored member state as evidence.
drill=$repo_root/deploy/vn/scripts/restore-drill.sh
for fixed in 'PASS latest notice restore fence' \
  'PASS point-in-time notice restore fence' 'notice reconciliation: not performed'; do
  grep -Fq "$fixed" "$drill" || fail 'missing fixed notice drill result'
done
for forbidden in epoch send.json activation.json restore-reconciliation; do
  if grep -Fq "$forbidden" "$work/native.log"; then
    fail 'notice authority entered the remote journal'
  fi
done
printf 'restore drill journal tests: ok\n'
