#!/usr/bin/env bash
# Manage the one disposable MinIO instance used by the native S3 conformance
# gate. Credentials stay in the ignored .dev directory and are never printed.
set -Eeuo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
ROOT=$PWD

readonly CONTAINER=aboutme-test-s3
readonly ENV_FILE=$ROOT/.dev/test-s3.env
readonly ENDPOINT=http://127.0.0.1:20091
readonly MINIO_IMAGE=docker.io/minio/minio:RELEASE.2025-09-07T16-13-09Z
readonly MC_IMAGE=docker.io/minio/mc:RELEASE.2025-08-13T08-35-41Z

die() {
  printf 'test-s3: %s\n' "$*" >&2
  exit 1
}

require_tools() {
  local tool
  for tool in "$@"; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is not on PATH"
  done
}

container_state() {
  local rows
  rows=$(podman ps -a --format '{{.Names}}|{{.State}}') ||
    die "cannot inspect containers; refusing to change test S3 state"
  local state
  state=$(awk -F '|' -v name="$CONTAINER" '$1 == name { print $2; exit }' <<<"$rows")
  printf '%s\n' "${state:-absent}"
}

ensure_path_ignored() {
  local path=$1
  local relative_path=${path#"$ROOT"/}
  local ignore_status
  set +e
  git check-ignore -q -- "$relative_path"
  ignore_status=$?
  set -e
  if [ "$ignore_status" -eq 0 ]; then
    return 0
  fi
  [ "$ignore_status" -eq 1 ] ||
    die "cannot verify that a disposable S3 credential file is Git-ignored"

  local exclude_file
  exclude_file=$(git rev-parse --git-path info/exclude) ||
    die "cannot locate the repository-local Git exclude file"
  if [[ $exclude_file != /* ]]; then
    exclude_file=$ROOT/$exclude_file
  fi
  mkdir -p "$(dirname "$exclude_file")"
  printf '%s\n' '/.dev/' >>"$exclude_file" ||
    die "cannot add .dev to the repository-local Git excludes"
  git check-ignore -q -- "$relative_path" ||
    die "a disposable S3 credential file is still not Git-ignored after updating $exclude_file"
}

ensure_env_ignored() {
  ensure_path_ignored .dev/test-s3.env
}

verify_container_contract() {
  local image ports
  image=$(podman inspect "$CONTAINER" --format '{{.Config.Image}}') ||
    die "cannot inspect $CONTAINER image"
  [ "$image" = "$MINIO_IMAGE" ] ||
    die "$CONTAINER uses $image, want pinned $MINIO_IMAGE; after active tests finish, run 'make test-s3-down'"
  ports=$(podman port "$CONTAINER" 9000/tcp) ||
    die "cannot inspect $CONTAINER port mapping"
  [ "$ports" = 127.0.0.1:20091 ] ||
    die "$CONTAINER maps port 9000 as $ports, want 127.0.0.1:20091"
}

load_env_file() {
  [ -f "$ENV_FILE" ] || die "$ENV_FILE is missing; run 'make test-s3-up'"
  local mode
  mode=$(stat -c '%a' "$ENV_FILE") || die "cannot inspect $ENV_FILE"
  [ "$mode" = 600 ] || die "$ENV_FILE must have mode 0600 (found $mode)"

  local line name value
  declare -A seen=()
  while IFS= read -r line || [ -n "$line" ]; do
    [[ $line =~ ^([A-Z0-9_]+)=([A-Za-z0-9:/._-]+)$ ]] ||
      die "$ENV_FILE has an invalid line"
    name=${BASH_REMATCH[1]}
    value=${BASH_REMATCH[2]}
    case $name in
    TEST_S3_ENDPOINT | TEST_S3_REGION | TEST_S3_BUCKET | TEST_S3_ACCESS_KEY_ID | TEST_S3_SECRET_ACCESS_KEY | TEST_S3_FORCE_PATH_STYLE) ;;
    *) die "$ENV_FILE contains an unexpected variable" ;;
    esac
    [ -z "${seen[$name]:-}" ] || die "$ENV_FILE contains a duplicate variable"
    seen[$name]=1
    export "$name=$value"
  done <"$ENV_FILE"

  for name in TEST_S3_ENDPOINT TEST_S3_REGION TEST_S3_BUCKET TEST_S3_ACCESS_KEY_ID TEST_S3_SECRET_ACCESS_KEY TEST_S3_FORCE_PATH_STYLE; do
    [ -n "${seen[$name]:-}" ] || die "$ENV_FILE is missing $name"
  done
  [ "$TEST_S3_ENDPOINT" = "$ENDPOINT" ] || die "$ENV_FILE has the wrong TEST_S3_ENDPOINT"
  [ "$TEST_S3_REGION" = us-east-1 ] || die "$ENV_FILE has the wrong TEST_S3_REGION"
  [ "$TEST_S3_BUCKET" = aboutme-test ] || die "$ENV_FILE has the wrong TEST_S3_BUCKET"
  [ "$TEST_S3_FORCE_PATH_STYLE" = true ] || die "$ENV_FILE has the wrong TEST_S3_FORCE_PATH_STYLE"
}

write_env_file() {
  command -v openssl >/dev/null 2>&1 || die "openssl is not on PATH"
  mkdir -p "$ROOT/.dev"
  local access_key secret_key tmp
  umask 077
  tmp=$(mktemp "$ROOT/.dev/test-s3.env.XXXXXX") || die "could not create a credential file"
  trap 'rm -f -- "$tmp"' EXIT
  chmod 0600 "$tmp"
  ensure_path_ignored "$tmp"
  access_key=$(openssl rand -hex 16) || die "could not generate an access key"
  secret_key=$(openssl rand -hex 32) || die "could not generate a secret key"
  printf '%s\n' \
    "TEST_S3_ENDPOINT=$ENDPOINT" \
    'TEST_S3_REGION=us-east-1' \
    'TEST_S3_BUCKET=aboutme-test' \
    "TEST_S3_ACCESS_KEY_ID=$access_key" \
    "TEST_S3_SECRET_ACCESS_KEY=$secret_key" \
    'TEST_S3_FORCE_PATH_STYLE=true' >"$tmp"
  mv "$tmp" "$ENV_FILE"
  trap - EXIT
}

wait_ready() {
  local attempt
  for attempt in $(seq 1 60); do
    if curl --fail --silent --show-error "$ENDPOINT/minio/health/ready" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  die "$CONTAINER did not become ready within 60 seconds; inspect it with 'podman logs $CONTAINER'"
}

ensure_bucket() {
  local MC_HOST_test
  MC_HOST_test="http://${TEST_S3_ACCESS_KEY_ID}:${TEST_S3_SECRET_ACCESS_KEY}@127.0.0.1:20091"
  export MC_HOST_test
  podman run --rm --network host -e MC_HOST_test "$MC_IMAGE" \
    mb --ignore-existing test/aboutme-test >/dev/null ||
    die "could not create the private test bucket"
  podman run --rm --network host -e MC_HOST_test "$MC_IMAGE" \
    anonymous set none test/aboutme-test >/dev/null ||
    die "could not enforce the private test bucket policy"
}

up() {
  require_tools podman curl stat openssl mktemp git
  ensure_env_ignored
  local state
  state=$(container_state)
  if [ "$state" != absent ] && [ ! -f "$ENV_FILE" ]; then
    die "$CONTAINER exists but $ENV_FILE is missing; after active tests finish, run 'make test-s3-down'"
  fi
  [ -f "$ENV_FILE" ] || write_env_file
  load_env_file

  if [ "$state" = running ]; then
    verify_container_contract
  fi
  if [ "$state" != running ]; then
    if [ "$state" != absent ]; then
      podman rm "$CONTAINER" >/dev/null || die "could not remove stopped $CONTAINER"
    fi
    local MINIO_ROOT_USER MINIO_ROOT_PASSWORD
    MINIO_ROOT_USER=$TEST_S3_ACCESS_KEY_ID
    MINIO_ROOT_PASSWORD=$TEST_S3_SECRET_ACCESS_KEY
    export MINIO_ROOT_USER MINIO_ROOT_PASSWORD
    podman run -d --rm --name "$CONTAINER" --memory 512m \
      -p 127.0.0.1:20091:9000 \
      -e MINIO_ROOT_USER \
      -e MINIO_ROOT_PASSWORD \
      "$MINIO_IMAGE" server /data >/dev/null || die "could not start $CONTAINER"
  fi

  wait_ready
  ensure_bucket
  printf '%s\n' "$CONTAINER is ready at $ENDPOINT with private bucket aboutme-test."
}

run_with_env() {
  require_tools podman curl stat git
  ensure_env_ignored
  local state
  state=$(container_state)
  [ "$state" = running ] || die "$CONTAINER is not running; run 'make test-s3-up'"
  verify_container_contract
  load_env_file
  curl --fail --silent --show-error "$ENDPOINT/minio/health/ready" >/dev/null ||
    die "$CONTAINER is not ready"
  [ "$#" -gt 0 ] || die "run requires a command"
  export REQUIRE_TEST_S3=1
  exec "$@"
}

down() {
  require_tools podman
  local state
  state=$(container_state)
  if [ "$state" != absent ]; then
    podman rm -f "$CONTAINER" >/dev/null || die "could not remove $CONTAINER"
  fi
  if [ -e "$ENV_FILE" ]; then
    rm -f -- "$ENV_FILE"
  fi
  printf '%s\n' "$CONTAINER and its disposable credential file were removed."
}

case ${1:-} in
up) up ;;
down) down ;;
run)
  shift
  run_with_env "$@"
  ;;
*) die "usage: scripts/test-s3.sh {up|down|run command...}" ;;
esac
