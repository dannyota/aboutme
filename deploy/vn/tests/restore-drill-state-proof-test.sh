#!/usr/bin/env bash
set -euo pipefail
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
printf '[default]\naws_access_key_id = fixture\naws_secret_access_key = fixture\n' >"$work/credentials"
chmod 0600 "$work/credentials"
cat >"$work/bin/uuidgen" <<'STUB'
#!/usr/bin/env bash
printf '123e4567-e89b-42d3-a456-426614174000\n'
STUB
cat >"$work/bin/timeout" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ $1 == --foreground && $2 == --signal=KILL && $3 == 60s ]]
shift 3
exec "$@"
STUB
cat >"$work/bin/aws" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
name=$(cat "$root/current.case")
printf '%q ' "$@" >>"$root/$name.aws.log"
printf '\n' >>"$root/$name.aws.log"
[[ $AWS_SHARED_CREDENTIALS_FILE == "$root/credentials" ]]
[[ $(cat "$AWS_CONFIG_FILE") == $'[default]\ns3 =\n    addressing_style = path' ]]
[[ $AWS_MAX_ATTEMPTS == 1 && $AWS_EC2_METADATA_DISABLED == true ]]
[[ $* == '--profile default --endpoint-url https://hcm04.vstorage.vngcloud.vn --region HCM04 --no-cli-pager --no-cli-auto-prompt --cli-error-format legacy s3api get-bucket-versioning --bucket aboutme-tfstate' ]]
while IFS='=' read -r variable _; do
  case $variable in
    PATH | AWS_SHARED_CREDENTIALS_FILE | AWS_CONFIG_FILE | AWS_EC2_METADATA_DISABLED | \
      AWS_MAX_ATTEMPTS | AWS_RETRY_MODE | AWS_PAGER | AWS_CLI_AUTO_PROMPT | PWD | SHLVL | _) : ;;
    *) exit 2 ;;
  esac
done < <(env)
case $name in
  versioned) printf '{"Status":"Enabled"}\n' ;;
  suspended) printf '{"Status":"Suspended"}\n' ;;
  versioning-unknown) printf '{"Status":"Paused"}\n' ;;
  versioning-error) exit 1 ;;
  *) : ;;
esac
STUB
cat >"$work/bin/vngcloud" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
name=$(cat "$root/current.case")
store=$root/$name.object
log=$root/$name.log
printf '%q ' "$@" >>"$log"
printf '\n' >>"$log"
[[ $(cat "$VNGCLOUD_CONFIG_FILE") == '[default]' ]]
[[ $(stat -c %a "$VNGCLOUD_CONFIG_FILE") == 600 ]]
while IFS='=' read -r variable _; do
  case $variable in PATH | VNGCLOUD_CONFIG_FILE | PWD | SHLVL | _) : ;; *) exit 2 ;; esac
done < <(env)
[[ $1 == --profile && $2 == default && $3 == --output && $4 == json && $5 == storage ]]
op=$6
shift 6
bucket= key= body= destination= limit= region= credentials= deadline= yes=0
while (($#)); do
  case $1 in
    --bucket) bucket=$2 ; shift 2 ;;
    --key) key=$2 ; shift 2 ;;
    --body) body=$2 ; shift 2 ;;
    --destination) destination=$2 ; shift 2 ;;
    --max-download-bytes) limit=$2 ; shift 2 ;;
    --s3-region) region=$2 ; shift 2 ;;
    --s3-credentials-file) credentials=$2 ; shift 2 ;;
    --transfer-timeout) deadline=$2 ; shift 2 ;;
    --yes) yes=1 ; shift ;;
    *) exit 2 ;;
  esac
done
[[ $bucket == aboutme-tfstate && $key == restore-drill/probes/123e4567-e89b-42d3-a456-426614174000.json ]]
[[ $region == HCM04 && $credentials == "$root/credentials" && $deadline == 60s ]]
error() { printf '{"error":{"code":"%s","status":%s,"operation":"storage.%s","message":"fixture"}}\n' "$1" "$2" "$3" >&2; }
case $op in
  head-object)
    [[ $yes == 0 ]]
    [[ $name != preexisting && ! -e $store ]] || exit 0
    if [[ $name == final-head-unknown && -f $root/deleted ]]; then error RequestFailed 500 HeadObject; exit 1; fi
    case $name in
      denied) error RequestFailed 403 HeadObject ; exit 1 ;;
      head-text) printf '404 HeadObject\n' >&2 ; exit 4 ;;
      head-malformed) printf '{bad}\n' >&2 ; exit 4 ;;
      head-concat) error NotFound 404 HeadObject ; error NotFound 404 HeadObject ; exit 4 ;;
      head-type) error NotFound '"404"' HeadObject ; exit 4 ;;
      head-code) error RequestFailed 404 HeadObject ; exit 4 ;;
      head-op) error NotFound 404 GetObject ; exit 4 ;;
      head-exit) error NotFound 404 HeadObject ; exit 1 ;;
      head-timeout) exit 137 ;;
      head-array) printf '{"error":[]}\n' >&2 ; exit 4 ;;
    esac
    error NotFound 404 HeadObject ; exit 4
    ;;
  put-object)
    [[ $yes == 0 && -f $body ]]
    if [[ -e $store ]]; then
      case $name in
        overwrite-accepted) cp "$body" "$store" ; exit 0 ;;
        conflict-text) printf '412 ObjectExists\n' >&2 ; exit 1 ;;
        conflict-malformed) printf '{bad}\n' >&2 ; exit 1 ;;
        conflict-concat) error ObjectExists 412 PutObject ; error ObjectExists 412 PutObject ; exit 1 ;;
        conflict-type) error ObjectExists '"412"' PutObject ; exit 1 ;;
        conflict-code) error PreconditionFailed 412 PutObject ; exit 1 ;;
        conflict-op) error ObjectExists 412 HeadObject ; exit 1 ;;
        conflict-exit) error ObjectExists 412 PutObject ; exit 2 ;;
        conflict-timeout) exit 137 ;;
        second-ambiguous) cp "$body" "$store" ; exit 1 ;;
      esac
      error ObjectExists 412 PutObject ; exit 1
    fi
    cp "$body" "$store"
    if [[ $name == create-interrupted ]]; then kill -TERM "$PPID"; fi
    [[ $name != create-ambiguous ]] || { error ObjectWriteUnconfirmed 500 PutObject ; exit 1; }
    ;;
  get-object)
    [[ $yes == 0 && $limit == 66 && ! -e $destination && ! -L $destination ]]
    count=$(cat "$root/get-count" 2>/dev/null || printf 0)
    count=$((count + 1))
    printf '%s\n' "$count" >"$root/get-count"
    [[ $name != cleanup-read-failure || $count != 2 ]] || exit 1
    case $name in
      oversized-foreign) printf '%0200d\n' 0 >"$store" ; head -c 67 "$store" >"$destination" ; exit 1 ;;
      symlink-read) ln -s "$store" "$destination" ; exit 0 ;;
      directory-read) mkdir "$destination" ; exit 0 ;;
      empty-read) : >"$destination" ; exit 0 ;;
      oversized-success) head -c 67 /dev/zero >"$destination" ; exit 0 ;;
    esac
    cp "$store" "$destination"
    [[ $name != read-output-failure ]] || exit 1
    if [[ $name == interrupted && $count == 1 ]]; then kill -TERM "$PPID"; fi
    if [[ $name == foreign-after-read && $count == 1 ]]; then printf '{"foreign":true}\n' >"$store"; fi
    ;;
  delete-object)
    [[ $yes == 1 ]]
    [[ $name != cleanup-failure ]] || exit 1
    rm "$store"
    : >"$root/deleted"
    [[ $name != delete-ambiguous ]] || exit 137
    [[ $name != delete-output-failure ]] || exit 1
    ;;
  *) exit 2 ;;
esac
STUB
chmod +x "$work/bin/"*
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
run_case() {
  local name=$1
  : >"$work/$name.log"
  : >"$work/$name.aws.log"
  printf '%s\n' "$name" >"$work/current.case"
  rm -f "$work/$name.object" "$work/get-count" "$work/deleted"
  PATH=$work/bin:$PATH DRILL_STATE_CREDENTIALS_FILE=$work/credentials \
    AWS_ACCESS_KEY_ID=poison AWS_SECRET_ACCESS_KEY=poison AWS_PROFILE=poison \
    VNGCLOUD_CONFIG_FILE=$work/poison VNGCLOUD_PROFILE=poison \
    VNGCLOUD_READ_ONLY=true VNGCLOUD_CLIENT_SECRET=poison HTTPS_PROXY=http://poison \
    bash "$repo_root/deploy/vn/scripts/drill-state-proof.sh" >"$work/$name.out" 2>&1
}
expect_failure() { if run_case "$1"; then fail "$1 succeeded"; fi; }
no_call() { if grep -q "$2" "$work/$1.log"; then fail "$1 made forbidden $2"; fi; }
recovery() {
  grep -q 's3://aboutme-tfstate/restore-drill/probes/123e4567-e89b-42d3-a456-426614174000.json' "$work/$1.out" || fail "$1 omitted recovery key"
  [[ -e $work/$1.object ]] || fail "$1 lost recovery object"
  no_call "$1" delete-object
}
run_case success || fail 'success failed'
[[ $(wc -l <"$work/success.aws.log") == 1 ]] || fail 'AWS read was not isolated'
for tuple in 'put-object 2' 'get-object 2' 'delete-object 1' 'head-object 2'; do
  read -r operation expected <<<"$tuple"
  [[ $(grep -c "$operation" "$work/success.log") == "$expected" ]] || fail "$operation count"
done
[[ ! -e $work/success.object ]] || fail 'successful probe remains'
for name in versioned suspended versioning-unknown versioning-error preexisting denied \
  head-text head-malformed head-concat head-type head-code head-op head-exit head-timeout head-array; do
  expect_failure "$name"
  no_call "$name" put-object
  no_call "$name" delete-object
done
expect_failure create-ambiguous
recovery create-ambiguous
[[ $(grep -c put-object "$work/create-ambiguous.log") == 1 ]] || fail 'ambiguous create retried'
expect_failure create-interrupted
recovery create-interrupted
[[ $(grep -c put-object "$work/create-interrupted.log") == 1 ]] || fail 'interrupted create retried'
no_call create-interrupted get-object
for name in overwrite-accepted conflict-text conflict-malformed conflict-concat conflict-type conflict-code conflict-op conflict-exit conflict-timeout; do
  expect_failure "$name"
  [[ $(grep -c delete-object "$work/$name.log") == 1 ]] || fail "$name did not clean attributable bytes"
done
for name in cleanup-read-failure read-output-failure foreign-after-read oversized-foreign symlink-read directory-read empty-read oversized-success second-ambiguous; do
  expect_failure "$name"
  recovery "$name"
done
expect_failure cleanup-failure
[[ -e $work/cleanup-failure.object ]] || fail 'failed delete lost recovery object'
[[ $(grep -c delete-object "$work/cleanup-failure.log") == 1 ]] || fail 'delete retried'
expect_failure final-head-unknown
[[ $(grep -c delete-object "$work/final-head-unknown.log") == 1 ]] || fail 'unknown final HEAD retried deletion'
expect_failure interrupted
[[ $(grep -c delete-object "$work/interrupted.log") == 1 && ! -e $work/interrupted.object ]] || fail 'signal cleanup did not run exactly once'
run_case delete-output-failure || fail 'final HEAD did not settle delete output failure'
[[ $(grep -c delete-object "$work/delete-output-failure.log") == 1 ]] || fail 'delete output failure retried'
run_case delete-ambiguous || fail 'final HEAD did not settle ambiguous delete'
[[ $(grep -c delete-object "$work/delete-ambiguous.log") == 1 ]] || fail 'ambiguous delete retried'
printf 'restore drill state proof tests: ok\n'
