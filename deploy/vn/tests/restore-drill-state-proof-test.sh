#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
printf '[default]\naws_access_key_id = fixture\naws_secret_access_key = fixture\n' >"$work/credentials"

cat >"$work/bin/uuidgen" <<'STUB'
#!/usr/bin/env bash
printf '123e4567-e89b-42d3-a456-426614174000\n'
STUB

cat >"$work/bin/aws" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
mock_root=$(cd "$(dirname "$0")/.." && pwd)
mock_case=$(cat "$mock_root/current.case")
mock_log=$mock_root/$mock_case.log
mock_store=$mock_root/$mock_case.object
printf '%q ' "$@" >>"$mock_log"
printf '\n' >>"$mock_log"
grep -q 'addressing_style = path' "$AWS_CONFIG_FILE"
[[ $AWS_SHARED_CREDENTIALS_FILE == "$mock_root/credentials" ]]
[[ -z ${HOME+x} ]]
for name in AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_PROFILE \
  AWS_DEFAULT_PROFILE AWS_ROLE_ARN AWS_WEB_IDENTITY_TOKEN_FILE \
  AWS_CONTAINER_CREDENTIALS_RELATIVE_URI AWS_CONTAINER_CREDENTIALS_FULL_URI \
  AWS_CONTAINER_AUTHORIZATION_TOKEN AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE; do
  [[ -z ${!name+x} ]] || { printf 'ambient credential variable survived: %s\n' "$name" >&2; exit 2; }
done
operation=
for arg in "$@"; do
  case $arg in
    get-bucket-versioning | head-object | put-object | get-object | delete-object) operation=$arg ;;
  esac
done
case $operation in
  get-bucket-versioning)
    case $mock_case in
      versioned) printf '{"Status":"Enabled"}\n' ;;
      suspended) printf '{"Status":"Suspended"}\n' ;;
      versioning-unknown) printf '{"Status":"Paused"}\n' ;;
      *) : ;;
    esac
    ;;
  head-object)
    if [[ $mock_case == preexisting || -e $mock_store ]]; then exit 0; fi
    if [[ $mock_case == denied ]]; then
      printf 'An error occurred (AccessDenied) when calling the HeadObject operation: denied\n' >&2
      exit 1
    fi
    if [[ $mock_case == misleading-absence ]]; then
      printf 'proxy failed after unrelated 404 Not Found HeadObject text\n' >&2
      exit 1
    fi
    if [[ $mock_case == wrong-retry-absence ]]; then
      printf 'aws: [ERROR]: An error occurred (404) when calling the HeadObject operation (reached max retries: 1): Not Found\n' >&2
      exit 1
    fi
    if [[ $mock_case == plain-errors ]]; then
      printf 'An error occurred (404) when calling the HeadObject operation: Not Found\n' >&2
      exit 1
    fi
    printf 'aws: [ERROR]: An error occurred (404) when calling the HeadObject operation (reached max retries: 0): Not Found\n' >&2
    exit 1
    ;;
  put-object)
    body=
    while (($#)); do
      [[ $1 != --body ]] || { body=$2; break; }
      shift
    done
    if [[ -e $mock_store ]]; then
      if [[ $mock_case == overwrite-accepted ]]; then cp -- "$body" "$mock_store"; exit 0; fi
      if [[ $mock_case == misleading-conflict ]]; then
        printf 'proxy cached 412 PreconditionFailed after PutObject transport failure\n' >&2
        exit 1
      fi
      if [[ $mock_case == wrong-retry-conflict ]]; then
        printf 'aws: [ERROR]: An error occurred (PreconditionFailed) when calling the PutObject operation (reached max retries: 1): conflict\n' >&2
        exit 1
      fi
      if [[ $mock_case == plain-errors ]]; then
        printf 'An error occurred (PreconditionFailed) when calling the PutObject operation: conflict\n' >&2
        exit 1
      fi
      printf 'aws: [ERROR]: An error occurred (PreconditionFailed) when calling the PutObject operation (reached max retries: 0): conflict\n' >&2
      exit 1
    fi
    cp -- "$body" "$mock_store"
    if [[ $mock_case == create-ambiguous ]]; then
      printf 'connection closed\n' >&2
      exit 1
    fi
    ;;
  get-object)
    count=$(cat "$mock_root/get-count" 2>/dev/null || printf 0)
    count=$((count + 1))
    printf '%s\n' "$count" >"$mock_root/get-count"
    if [[ $mock_case == cleanup-read-failure && $count -eq 2 ]]; then
      printf 'An error occurred (AccessDenied) when calling the GetObject operation: denied\n' >&2
      exit 1
    fi
    range=
    for ((i = 1; i <= $#; i++)); do
      if [[ ${!i} == --range ]]; then
        next=$((i + 1))
        range=${!next}
      fi
    done
    [[ $range == bytes=0-66 ]] || {
      printf 'expected GetObject range bytes=0-66, got %s\n' "$range" >&2
      exit 2
    }
    if [[ $mock_case == oversized-foreign ]]; then
      printf '%0200d\n' 0 >"$mock_store"
    fi
    head -c 67 -- "$mock_store" >"${!#}"
    if [[ $mock_case == foreign-after-read && $count -eq 1 ]]; then
      printf '{"foreign":true}\n' >"$mock_store"
    fi
    ;;
  delete-object)
    [[ $mock_case != cleanup-failure ]] || {
      printf 'An error occurred (AccessDenied) when calling the DeleteObject operation: denied\n' >&2
      exit 1
    }
    rm -f -- "$mock_store"
    ;;
  *) printf 'unexpected operation: %s\n' "$operation" >&2; exit 2 ;;
esac
STUB
chmod +x "$work/bin/"*

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
run_case() {
  local name=$1
  : >"$work/$name.log"
  printf '%s\n' "$name" >"$work/current.case"
  rm -f -- "$work/$name.object" "$work/get-count"
  PATH=$work/bin:$PATH DRILL_STATE_CREDENTIALS_FILE=$work/credentials \
    AWS_ACCESS_KEY_ID=poison AWS_SECRET_ACCESS_KEY=poison AWS_SESSION_TOKEN=poison \
    AWS_PROFILE=poison AWS_DEFAULT_PROFILE=poison AWS_ROLE_ARN=poison \
    AWS_WEB_IDENTITY_TOKEN_FILE=$work/poison-token \
    AWS_CONTAINER_CREDENTIALS_RELATIVE_URI=/poison \
    AWS_CONTAINER_CREDENTIALS_FULL_URI=http://127.0.0.1/poison \
    AWS_CONTAINER_AUTHORIZATION_TOKEN=poison \
    AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE=$work/poison-auth \
    bash "$repo_root/deploy/vn/scripts/drill-state-proof.sh"
}
expect_case_failure() { run_case "$1" >/dev/null 2>&1 && fail "$1 succeeded" || return 0; }

run_case success >/dev/null
success_log=$work/success.log
[[ $(grep -c 'put-object' "$success_log") -eq 2 ]] || fail 'proof did not make two puts'
grep -q -- '--if-none-match \\*' "$success_log" || fail 'proof put was not conditional'
grep -q -- '--profile default' "$success_log" || fail 'proof did not select the default profile'
grep -q -- '--cli-error-format legacy' "$success_log" || fail 'proof did not select legacy errors'
[[ $(grep -c 'get-object' "$success_log") -eq 2 ]] || fail 'proof did not verify bytes before cleanup'
[[ $(grep -c 'delete-object' "$success_log") -eq 1 ]] || fail 'proof did not clean its object'
[[ $(grep -c 'head-object' "$success_log") -eq 2 ]] || fail 'proof did not prove initial and final absence'
grep -q -- '--key restore-drill/probes/123e4567-e89b-42d3-a456-426614174000.json' "$success_log" ||
  fail 'proof key escaped disposable namespace'
[[ ! -e $work/success.object ]] || fail 'proof object remains after success'

run_case plain-errors >/dev/null
[[ ! -e $work/plain-errors.object ]] || fail 'plain-error proof object remains after success'

expect_case_failure preexisting
grep -q 'delete-object' "$work/preexisting.log" && fail 'proof deleted a preexisting object'
grep -q 'put-object' "$work/preexisting.log" && fail 'proof wrote over a preexisting object'

expect_case_failure denied
grep -q 'put-object' "$work/denied.log" && fail 'denied head was treated as absence'

expect_case_failure misleading-absence
grep -q 'put-object' "$work/misleading-absence.log" && fail 'misleading 404 was treated as absence'

expect_case_failure wrong-retry-absence
grep -q 'put-object' "$work/wrong-retry-absence.log" &&
  fail 'nonzero retry annotation was treated as absence'

expect_case_failure create-ambiguous
[[ -e $work/create-ambiguous.object ]] || fail 'ambiguous create fixture lost recovery object'
grep -q 'delete-object' "$work/create-ambiguous.log" && fail 'ambiguous create was deleted'

expect_case_failure cleanup-failure
[[ -e $work/cleanup-failure.object ]] || fail 'cleanup failure fixture lost recovery object'

expect_case_failure cleanup-read-failure
grep -q 'delete-object' "$work/cleanup-read-failure.log" && fail 'unreadable cleanup object was deleted'
[[ -e $work/cleanup-read-failure.object ]] || fail 'unreadable cleanup object was not retained'

expect_case_failure versioned
grep -q 'put-object' "$work/versioned.log" && fail 'versioned bucket received probe object'

expect_case_failure suspended
grep -q 'put-object' "$work/suspended.log" && fail 'suspended bucket received probe object'

expect_case_failure versioning-unknown
grep -q 'put-object' "$work/versioning-unknown.log" && fail 'unknown versioning state received probe object'

expect_case_failure overwrite-accepted
[[ $(grep -c 'delete-object' "$work/overwrite-accepted.log") -eq 1 ]] ||
  fail 'failed overwrite proof did not clean its owned key'

expect_case_failure misleading-conflict
[[ $(grep -c 'delete-object' "$work/misleading-conflict.log") -eq 1 ]] ||
  fail 'misleading conflict failure did not clean its owned object'

expect_case_failure wrong-retry-conflict
[[ $(grep -c 'delete-object' "$work/wrong-retry-conflict.log") -eq 1 ]] ||
  fail 'nonzero retry conflict failure did not clean its owned object'

printf '%s\n' foreign-after-read >"$work/current.case"
: >"$work/foreign-after-read.log"
rm -f -- "$work/foreign-after-read.object" "$work/get-count"
if PATH=$work/bin:$PATH DRILL_STATE_CREDENTIALS_FILE=$work/credentials \
  bash "$repo_root/deploy/vn/scripts/drill-state-proof.sh" \
  >"$work/foreign.out" 2>&1; then
  fail 'foreign replacement succeeded'
fi
grep -q 's3://aboutme-tfstate/restore-drill/probes/123e4567-e89b-42d3-a456-426614174000.json' \
  "$work/foreign.out" || fail 'foreign replacement omitted recovery key'
grep -q 'delete-object' "$work/foreign-after-read.log" && fail 'foreign bytes were deleted'
[[ -e $work/foreign-after-read.object ]] || fail 'foreign bytes were not retained'

printf '%s\n' oversized-foreign >"$work/current.case"
: >"$work/oversized-foreign.log"
rm -f -- "$work/oversized-foreign.object" "$work/get-count"
if PATH=$work/bin:$PATH DRILL_STATE_CREDENTIALS_FILE=$work/credentials \
  bash "$repo_root/deploy/vn/scripts/drill-state-proof.sh" \
  >"$work/oversized.out" 2>&1; then
  fail 'oversized foreign object succeeded'
fi
[[ $(grep -c -- '--range bytes=0-66' "$work/oversized-foreign.log") -eq 2 ]] ||
  fail 'proof reads were not both bounded'
grep -q 's3://aboutme-tfstate/restore-drill/probes/123e4567-e89b-42d3-a456-426614174000.json' \
  "$work/oversized.out" || fail 'oversized object omitted recovery key'
grep -q 'delete-object' "$work/oversized-foreign.log" && fail 'oversized foreign bytes were deleted'
[[ -e $work/oversized-foreign.object ]] || fail 'oversized foreign bytes were not retained'

printf 'restore drill state proof tests: ok\n'
