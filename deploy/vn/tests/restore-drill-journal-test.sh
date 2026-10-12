#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
printf '[default]\naws_access_key_id = fixture\naws_secret_access_key = fixture\n' >"$work/credentials"

cat >"$work/bin/aws" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
mock_root=$(cd "$(dirname "$0")/.." && pwd)
mock_log=$mock_root/aws.log
mock_store=$mock_root/object
mock_case=$(cat "$mock_root/case" 2>/dev/null || printf success)
printf '%q ' "$@" >>"$mock_log"
printf '\n' >>"$mock_log"
grep -q 'addressing_style = path' "$AWS_CONFIG_FILE"
[[ $AWS_SHARED_CREDENTIALS_FILE == "$mock_root/credentials" ]]
[[ ${AWS_EC2_METADATA_DISABLED:-} == true ]]
[[ -z ${HOME+x} ]]
for name in AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_PROFILE \
  AWS_DEFAULT_PROFILE AWS_ROLE_ARN AWS_WEB_IDENTITY_TOKEN_FILE \
  AWS_CONTAINER_CREDENTIALS_RELATIVE_URI AWS_CONTAINER_CREDENTIALS_FULL_URI \
  AWS_CONTAINER_AUTHORIZATION_TOKEN AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE; do
  [[ -z ${!name+x} ]] || { printf 'ambient credential variable survived: %s\n' "$name" >&2; exit 2; }
done
operation=
for arg in "$@"; do
  case $arg in get-object | put-object | delete-object) operation=$arg ;; esac
done
case $operation in
  put-object)
    body=
    while (($#)); do
      [[ $1 != --body ]] || { body=$2; break; }
      shift
    done
    [[ ! -e $mock_root/mutate-input ]] || printf '{"changed":true}\n' >"$(cat "$mock_root/mutate-input")"
    case $mock_case in
      conflict)
        printf 'aws: [ERROR]: An error occurred (PreconditionFailed) when calling the PutObject operation (reached max retries: 0): conflict\n' >&2
        exit 1
        ;;
      plain-conflict)
        printf 'An error occurred (PreconditionFailed) when calling the PutObject operation: conflict\n' >&2
        exit 1
        ;;
      wrong-retry-conflict)
        printf 'aws: [ERROR]: An error occurred (PreconditionFailed) when calling the PutObject operation (reached max retries: 1): conflict\n' >&2
        exit 1
        ;;
      misleading-conflict)
        printf 'proxy cached 412 PreconditionFailed while another operation failed\n' >&2
        exit 1
        ;;
      lost-match | lost-mismatch | lost-unreadable)
        cp -- "$body" "$mock_store"
        [[ $mock_case != lost-mismatch ]] || printf '{"other":true}\n' >"$mock_store"
        printf 'connection closed\n' >&2
        exit 1
        ;;
      *) cp -- "$body" "$mock_store" ;;
    esac
    ;;
  get-object)
    [[ $mock_case != lost-unreadable ]] || {
      printf 'An error occurred (AccessDenied) when calling GetObject: denied\n' >&2
      exit 1
    }
    cp -- "$mock_store" "${!#}"
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
chmod +x "$work/bin/aws"

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
# shellcheck source=../scripts/drill-journal.sh
. "$repo_root/deploy/vn/scripts/drill-journal.sh"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
expect_fail() { "$@" >/dev/null 2>&1 && fail "command succeeded: $*" || return 0; }
reset_mock() {
  : >"$work/aws.log"
  printf 'success\n' >"$work/case"
  rm -f -- "$work/object" "$work/mutate-input" "$work/race-output"
}
set_case() { printf '%s\n' "$1" >"$work/case"; }

record=$work/record.json
printf '{"kind":"fixture","text":"line one\\nline two"}\n' >"$record"

reset_mock
drill_journal_put_create 'locks/1.json' "$record"
cmp -s -- "$record" "$work/object" || fail 'successful put changed bytes'
grep -q -- '--key restore-drill/v1/locks/1.json' "$work/aws.log" || fail 'put used wrong key'
grep -q -- '--if-none-match \\*' "$work/aws.log" || fail 'put omitted create-only precondition'
grep -q -- '--endpoint-url https://hcm04.vstorage.vngcloud.vn' "$work/aws.log" || fail 'endpoint missing'
grep -q -- '--region HCM04' "$work/aws.log" || fail 'region missing'
grep -q -- '--profile default' "$work/aws.log" || fail 'default profile was not selected'
grep -q -- '--no-cli-pager' "$work/aws.log" || fail 'pager was not disabled'
grep -q -- '--cli-error-format legacy' "$work/aws.log" || fail 'legacy error format was not selected'

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
  [[ ! -s $work/aws.log ]] || fail "invalid key reached AWS CLI: $key"
done

reset_mock
DRILL_JOURNAL_BUCKET=wrong DRILL_JOURNAL_PREFIX=wrong DRILL_JOURNAL_ENDPOINT=http://wrong \
  DRILL_JOURNAL_REGION=wrong DRILL_JOURNAL_MAX_BYTES=99999999 \
  drill_journal_put_create \
  'runs/123e4567-e89b-42d3-a456-426614174000/1.json' "$record"
grep -q -- '--bucket aboutme-tfstate' "$work/aws.log" || fail 'caller changed bucket scope'
grep -q -- '--key restore-drill/v1/runs/' "$work/aws.log" || fail 'caller changed key scope'
drill_journal_put_create \
  'runs/123e4567-e89b-42d3-a456-426614174000/recovery/2.json' "$record"

bad=$work/bad.json
printf 'not json\n' >"$bad"
reset_mock
expect_fail drill_journal_put_create 'releases/1.json' "$bad"
[[ ! -s $work/aws.log ]] || fail 'invalid JSON reached AWS CLI'
ln -s "$record" "$work/link.json"
expect_fail drill_journal_put_create 'releases/1.json' "$work/link.json"
large=$work/large.json
truncate -s 1048577 "$large"
DRILL_JOURNAL_MAX_BYTES=99999999 \
  expect_fail drill_journal_put_create 'releases/1.json' "$large"

reset_mock
set_case conflict
expect_fail drill_journal_put_create 'releases/1.json' "$record"
[[ $(wc -l <"$work/aws.log") -eq 1 ]] || fail 'conflict caused another request'
grep -q 'put-object' "$work/aws.log" || fail 'conflict test did not put'
grep -q 'delete-object' "$work/aws.log" && fail 'journal transport issued delete'

reset_mock
set_case plain-conflict
expect_fail drill_journal_put_create 'releases/1.json' "$record"
[[ $(wc -l <"$work/aws.log") -eq 1 ]] || fail 'plain conflict caused another request'

reset_mock
set_case wrong-retry-conflict
expect_fail drill_journal_put_create 'releases/1.json' "$record"
[[ $(grep -c 'get-object' "$work/aws.log") -eq 1 ]] ||
  fail 'nonzero retry annotation was treated as a definite conflict'

reset_mock
set_case misleading-conflict
expect_fail drill_journal_put_create 'releases/1.json' "$record"
[[ $(grep -c 'get-object' "$work/aws.log") -eq 1 ]] || fail 'misleading 412 was treated as conflict'

reset_mock
set_case lost-match
drill_journal_put_create 'releases/2.json' "$record"
[[ $(wc -l <"$work/aws.log") -eq 2 ]] || fail 'lost response was not read back once'
[[ $(grep -c 'put-object' "$work/aws.log") -eq 1 ]] || fail 'ambiguous put was retried'
[[ $(grep -c 'get-object' "$work/aws.log") -eq 1 ]] || fail 'ambiguous put read-back count differs'

for case_name in lost-mismatch lost-unreadable; do
  reset_mock
  set_case "$case_name"
  expect_fail drill_journal_put_create 'releases/3.json' "$record"
  [[ $(grep -c 'put-object' "$work/aws.log") -eq 1 ]] || fail "$case_name retried put"
  [[ $(grep -c 'get-object' "$work/aws.log") -eq 1 ]] || fail "$case_name did not read back once"
  grep -q 'delete-object' "$work/aws.log" && fail "$case_name deleted a record"
done

reset_mock
cp -- "$record" "$work/object"
out=$work/read.json
drill_journal_get 'runs/123e4567-e89b-42d3-a456-426614174000/4.json' "$out"
cmp -s -- "$record" "$out" || fail 'get changed bytes'

reset_mock
set_case lost-unreadable
expect_fail drill_journal_get 'locks/4.json' "$work/denied.json"
[[ ! -e $work/denied.json ]] || fail 'denied get created output'

reset_mock
cp -- "$record" "$work/object"
expect_fail drill_journal_get 'locks/5.json' "$work/read.json"
[[ -e $work/read.json ]] || fail 'existing output was changed'

for json in false null; do
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
  [[ ! -s $work/aws.log ]] || fail 'invalid JSON sequence reached AWS CLI'
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

printf 'restore drill journal tests: ok\n'
