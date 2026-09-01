#!/usr/bin/env bash
# Native HTTPS authentication harness on https://localhost:20443.
set -Eeuo pipefail
umask 077

cd "$(dirname "${BASH_SOURCE[0]}")/.."
readonly ROOT=$PWD

readonly WEB_PORT=20440
readonly SERVER_PORT=20441
readonly MOCK_PORT=20442
readonly CADDY_PORT=20443
readonly MAIL_CAPTURE_PORT=20444
readonly MAIL_CAPTURE_ADDR="127.0.0.1:${MAIL_CAPTURE_PORT}"
readonly MAIL_CAPTURE_URL="http://${MAIL_CAPTURE_ADDR}"
readonly ACTIVE_KEY_ID=dev-active
readonly PUBLIC_ORIGIN="https://localhost:${CADDY_PORT}"
readonly PUBLIC_RENDER_ORIGIN=http://127.0.0.1:20440
readonly GOOGLE_CLIENT_ID=aboutme-local-google
readonly GOOGLE_CLIENT_SECRET=not-a-secret-local-google
readonly GOOGLE_ISSUER_URL="http://127.0.0.1:${MOCK_PORT}/google"
readonly DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable'
readonly LOG_LEVEL="${ABOUTME_DEV_LOG_LEVEL:-info}"

readonly DEV_DIR=$ROOT/.dev
readonly STATE_DIR=$ROOT/.dev/native-https
readonly RUN_DIR=$STATE_DIR/run
readonly LOG_DIR=$STATE_DIR/log
readonly BIN_DIR=$STATE_DIR/bin
readonly MEDIA_DIR=$STATE_DIR/media
readonly CADDY_DIR=$STATE_DIR/caddy
readonly INPUT_DIR=$STATE_DIR/input
readonly SECRETS_DIR=$STATE_DIR/secrets
readonly CADDYFILE_SRC=$ROOT/deploy/caddy/Caddyfile
readonly CADDYFILE_GEN=$STATE_DIR/Caddyfile
readonly PUBLIC_ROOTS_REGISTRY=$ROOT/packages/publicroots/public-roots.v6.json
readonly PUBLIC_ROOTS_FRAGMENT=$ROOT/deploy/caddy/public-roots.generated.caddy
readonly PUBLIC_ROOTS_MARKER=$'\t# ABOUTME_PUBLIC_ROOTS_GENERATED\n\timport /etc/caddy/generated/public-roots.generated.caddy'
readonly CADDY_ROOT=$CADDY_DIR/data/caddy/pki/authorities/local/root.crt
readonly EXPORTED_ROOT=$INPUT_DIR/caddy-root.crt
readonly EFFECTIVE_CONFIG=$STATE_DIR/effective-config
readonly DB_CONTAINER=aboutme-test-db
readonly SERVICES=(mock-oauth mail-capture server web caddy)
readonly STOP_ORDER=(caddy web server mail-capture mock-oauth)
readonly NORMAL_NATIVE_PORTS=(20030 20080 20081)
readonly STOP_TERM_ATTEMPTS=${DEV_HTTPS_STOP_TERM_ATTEMPTS:-100}
readonly STOP_KILL_ATTEMPTS=${DEV_HTTPS_STOP_KILL_ATTEMPTS:-50}
readonly BASH_BIN=$(readlink -f "$(command -v bash)")
readonly SUPERVISOR_CODE='set -Eeuo pipefail
umask 077
printf "%s\n" "$$" >"$1"
cd "$2" || exit 127
set -a
source "$3"
set +a
shift 3
"$@" &
child=$!
wait "$child"'

STARTUP_ARMED=0
declare -a STARTED_SERVICES=()
APP_BUILD_DIGEST=
PUBLIC_RENDERER_BUILD_DIGEST=
PASSWORD_RATE_HMAC_KEY_B64=
AUTH_EMAIL_ACTIVE_KEY_B64=
AUTH_EMAIL_CAPTURE_BEARER_B64=

info() { printf '%s\n' "$*"; }
warn() { printf 'dev-https: %s\n' "$*" >&2; }
die() {
  printf 'dev-https: %s\n' "$*" >&2
  exit 1
}

pidfile() { printf '%s/%s.pid' "$RUN_DIR" "$1"; }
logfile() { printf '%s/%s.log' "$LOG_DIR" "$1"; }
identity_file() { printf '%s/%s.identity' "$RUN_DIR" "$1"; }
launch_file() { printf '%s/%s.launch' "$RUN_DIR" "$1"; }
service_env_file() { printf '%s/%s.env' "$RUN_DIR" "$1"; }

validate_existing_state_node() {
  local path=$1 kind=$2 mode owner canonical
  [ ! -L "$path" ] || die "state path is a symlink: $path"
  [ -e "$path" ] || die "state path disappeared during validation: $path"
  case $kind in
  directory) [ -d "$path" ] || die "state path is not a directory: $path" ;;
  regular) [ -f "$path" ] || die "state path is not a regular file: $path" ;;
  any)
    [ -d "$path" ] || [ -f "$path" ] || die "state path has an unsupported file type: $path"
    ;;
  *) die "invalid state path validation kind: $kind" ;;
  esac
  canonical=$(readlink -f -- "$path" 2>/dev/null) || die "state path cannot be resolved safely: $path"
  [ "$canonical" = "$path" ] || die "state path is noncanonical: $path"
  owner=$(stat -c '%u' -- "$path" 2>/dev/null) || die "state path ownership cannot be read: $path"
  [ "$owner" = "$EUID" ] || die "state path is not owned by uid $EUID: $path"
  mode=$(stat -c '%a' -- "$path" 2>/dev/null) || die "state path mode cannot be read: $path"
  [[ $mode =~ ^[0-7]{3,4}$ ]] || die "state path has an invalid mode: $path"
  (( (8#$mode & 8#022) == 0 )) || die "state path is group- or other-writable: $path"
}

validate_state_path_integrity() {
  local path unsafe
  if [ -e "$DEV_DIR" ] || [ -L "$DEV_DIR" ]; then
    validate_existing_state_node "$DEV_DIR" directory
  fi
  if [ -e "$STATE_DIR" ] || [ -L "$STATE_DIR" ]; then
    validate_existing_state_node "$STATE_DIR" directory
    [ "$(stat -c '%a' -- "$STATE_DIR")" = 700 ] || die "state path must have mode 700: $STATE_DIR"
  else
    return 0
  fi
  unsafe=$(find -P "$STATE_DIR" -xdev -mindepth 1 \
    \( -type l -o ! -uid "$EUID" -o -perm /022 \
    -o \( ! -type d -a ! -type f \) \) -print -quit 2>/dev/null) || \
    die "state path descendants cannot be inspected safely: $STATE_DIR"
  [ -z "$unsafe" ] || die "state path descendant is unsafe: $unsafe"
  unsafe=$(find -P "$STATE_DIR" -xdev -type f -links +1 -print -quit 2>/dev/null) || \
    die "state path file links cannot be inspected safely: $STATE_DIR"
  [ -z "$unsafe" ] || die "state path regular file has multiple hard links: $unsafe"
  unsafe=$(find -P "$STATE_DIR" -xdev -type d ! -path "$CADDY_DIR" \
    ! -perm 0700 -print -quit 2>/dev/null) || \
    die "state path directory modes cannot be inspected safely: $STATE_DIR"
  [ -z "$unsafe" ] || die "state path directory must have mode 700: $unsafe"
  if [ -d "$BIN_DIR" ]; then
    unsafe=$(find -P "$BIN_DIR" -xdev -type f ! -perm 0755 -print -quit 2>/dev/null) || \
      die "state path binary modes cannot be inspected safely: $BIN_DIR"
    [ -z "$unsafe" ] || die "state path binary must have mode 755: $unsafe"
  fi
  unsafe=$(find -P "$STATE_DIR" -xdev -path "$BIN_DIR" -prune -o \
    -type f ! -perm 0600 -print -quit 2>/dev/null) || \
    die "state path file modes cannot be inspected safely: $STATE_DIR"
  [ -z "$unsafe" ] || die "state path sensitive file must have mode 600: $unsafe"
  for path in "$RUN_DIR" "$LOG_DIR" "$BIN_DIR" "$MEDIA_DIR" "$INPUT_DIR" \
    "$CADDY_DIR/config" "$CADDY_DIR/data"; do
    if [ -e "$path" ] || [ -L "$path" ]; then
      validate_existing_state_node "$path" directory
      [ "$(stat -c '%a' -- "$path")" = 700 ] || die "state path must have mode 700: $path"
    fi
  done
  if [ -e "$CADDY_DIR" ] || [ -L "$CADDY_DIR" ]; then
    validate_existing_state_node "$CADDY_DIR" directory
    case $(stat -c '%a' -- "$CADDY_DIR") in
    700 | 755) ;;
    *) die "state path must have mode 700 (or legacy safe mode 755): $CADDY_DIR" ;;
    esac
  fi
}

prepare_state_directories() {
  local path
  if [ ! -e "$DEV_DIR" ]; then
    install -d -m 0755 -- "$DEV_DIR"
  fi
  validate_existing_state_node "$DEV_DIR" directory
  if [ ! -e "$STATE_DIR" ]; then
    install -d -m 0700 -- "$STATE_DIR"
  fi
  validate_existing_state_node "$STATE_DIR" directory
  for path in "$RUN_DIR" "$LOG_DIR" "$BIN_DIR" "$MEDIA_DIR" "$CADDY_DIR" \
    "$CADDY_DIR/config" "$CADDY_DIR/data" "$INPUT_DIR" "$SECRETS_DIR"; do
    if [ ! -e "$path" ]; then
      install -d -m 0700 -- "$path"
    fi
    validate_existing_state_node "$path" directory
    chmod 0700 -- "$path"
  done
  validate_state_path_integrity
}

port_of() {
  case $1 in
  mock-oauth) printf '%s' "$MOCK_PORT" ;;
  mail-capture) printf '%s' "$MAIL_CAPTURE_PORT" ;;
  server) printf '%s' "$SERVER_PORT" ;;
  web) printf '%s' "$WEB_PORT" ;;
  caddy) printf '%s' "$CADDY_PORT" ;;
  *) die "unknown service $1" ;;
  esac
}

read_pid() {
  local file pid
  file=$(pidfile "$1")
  [ -s "$file" ] || return 1
  pid=$(<"$file")
  [[ $pid =~ ^[0-9]+$ ]] || return 1
  printf '%s' "$pid"
}

is_ours() {
  local pid=$1 sid
  kill -0 "$pid" 2>/dev/null || return 1
  sid=$(ps -o sid= -p "$pid" 2>/dev/null | tr -d ' ') || return 1
  [ "$sid" = "$pid" ]
}

service_pid() {
  local pid
  pid=$(read_pid "$1") || return 1
  validate_service_identity "$1" 1 || return 1
  printf '%s' "$pid"
}

sha256_file() { sha256sum "$1" | awk '{print $1}'; }

hash_argv() {
  printf '%s\0' "$@" | sha256sum | awk '{print $1}'
}

expected_service_cmdline_hash() {
  local name=$1 file environment workdir
  file=$(pidfile "$name")
  environment=$(service_env_file "$name")
  case $name in
  mock-oauth)
    workdir=$ROOT/apps/server
    hash_argv "$BASH_BIN" -c "$SUPERVISOR_CODE" "dev-https-$name" "$file" "$workdir" "$environment" "$BIN_DIR/mock-oauth"
    ;;
  mail-capture)
    workdir=$ROOT/apps/server
    hash_argv "$BASH_BIN" -c "$SUPERVISOR_CODE" "dev-https-$name" "$file" "$workdir" "$environment" "$BIN_DIR/mail-capture" --secret-file "$SECRETS_DIR/auth-email-capture-bearer" --addr "$MAIL_CAPTURE_ADDR"
    ;;
  server)
    workdir=$ROOT/apps/server
    hash_argv "$BASH_BIN" -c "$SUPERVISOR_CODE" "dev-https-$name" "$file" "$workdir" "$environment" "$BIN_DIR/server"
    ;;
  web)
    workdir=$ROOT/apps/web
    hash_argv "$BASH_BIN" -c "$SUPERVISOR_CODE" "dev-https-$name" "$file" "$workdir" "$environment" npm run dev -- --port "$WEB_PORT" --host 127.0.0.1
    ;;
  caddy)
    workdir=$ROOT
    hash_argv "$BASH_BIN" -c "$SUPERVISOR_CODE" "dev-https-$name" "$file" "$workdir" "$environment" caddy run --config "$CADDYFILE_GEN" --adapter caddyfile
    ;;
  *) return 1 ;;
  esac
}

proc_starttime() {
  local stat rest
  stat=$(<"/proc/$1/stat") || return 1
  rest=${stat##*) }
  set -- $rest
  [ "$#" -ge 20 ] || return 1
  printf '%s' "${20}"
}

proc_cmdline_hash() {
  [ -r "/proc/$1/cmdline" ] || return 1
  sha256sum "/proc/$1/cmdline" | awk '{print $1}'
}

identity_field() {
  local file=$1 key=$2 lines
  lines=$(awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print }' "$file") || return 1
  [ "$(wc -l <<<"$lines")" -eq 1 ] || return 1
  printf '%s' "$lines"
}

group_members() {
  local pgid=$1 sid=$2
  ps -eo pid=,pgid=,sid= | awk -v pgid="$pgid" -v sid="$sid" '$2 == pgid && $3 == sid { print $1 }'
}

member_has_token() {
  local pid=$1 token=$2 environment
  [ -r "/proc/$pid/environ" ] || return 1
  environment=$(tr '\0' '\n' <"/proc/$pid/environ") || return 1
  grep -Fx "ABOUTME_DEV_HTTPS_IDENTITY=$token" <<<"$environment" >/dev/null
}

validate_group_member() {
  local pid=$1 pgid=$2 sid=$3 token=$4 actual actual_pgid actual_sid state
  actual=$(ps -o pgid=,sid=,stat= -p "$pid" 2>/dev/null) || return 0
  [ -n "$actual" ] || return 0
  read -r actual_pgid actual_sid state <<<"$actual"
  [ "$actual_pgid" = "$pgid" ] && [ "$actual_sid" = "$sid" ] || return 1
  [[ $state == Z* ]] && return 0
  if member_has_token "$pid" "$token"; then
    return 0
  fi
  # A short-lived child can disappear between the group snapshot and its
  # environ read. Disappearance is safe; a still-live unmarked member is not.
  kill -0 "$pid" 2>/dev/null || return 0
  return 1
}

validate_process_record() {
  local name=$1 file=$2 mode version service pid pidfile_pid starttime sid pgid expected_executable expected_cmdline token
  [ -f "$file" ] || return 1
  mode=$(stat -c '%a' "$file" 2>/dev/null || true)
  [ "$mode" = 600 ] || return 1
  version=$(identity_field "$file" version) || return 1
  service=$(identity_field "$file" service) || return 1
  pid=$(identity_field "$file" pid) || return 1
  starttime=$(identity_field "$file" starttime) || return 1
  sid=$(identity_field "$file" sid) || return 1
  pgid=$(identity_field "$file" pgid) || return 1
  expected_executable=$(identity_field "$file" expected_executable) || return 1
  expected_cmdline=$(identity_field "$file" expected_cmdline_sha256) || return 1
  token=$(identity_field "$file" identity_token) || return 1
  pidfile_pid=$(read_pid "$name") || return 1
  [ "$version" = 1 ] || return 1
  [ "$service" = "$name" ] || return 1
  [ "$pid" = "$pidfile_pid" ] || return 1
  [[ $pid =~ ^[0-9]+$ && $starttime =~ ^[0-9]+$ && $sid =~ ^[0-9]+$ && $pgid =~ ^[0-9]+$ ]] || return 1
  [ "$pid" = "$sid" ] && [ "$pid" = "$pgid" ] || return 1
  [[ $expected_executable == /* ]] || return 1
  [[ $expected_cmdline =~ ^[0-9a-f]{64}$ ]] || return 1
  local token_suffix=${token#"$name-"}
  [ "$token_suffix" != "$token" ] && [[ $token_suffix =~ ^[0-9a-f]{64}$ ]] || return 1
}

validate_identity_record() {
  validate_process_record "$1" "$(identity_file "$1")"
}

validate_prepared_launch_record() {
  local name=$1 file mode version state service expected_executable expected_cmdline token token_suffix
  file=$(launch_file "$name")
  [ -f "$file" ] || return 1
  mode=$(stat -c '%a' "$file" 2>/dev/null || true)
  [ "$mode" = 600 ] || return 1
  version=$(identity_field "$file" version) || return 1
  state=$(identity_field "$file" launch_state) || return 1
  service=$(identity_field "$file" service) || return 1
  expected_executable=$(identity_field "$file" expected_executable) || return 1
  expected_cmdline=$(identity_field "$file" expected_cmdline_sha256) || return 1
  token=$(identity_field "$file" identity_token) || return 1
  [ "$version" = 1 ] && [ "$state" = prepared ] && [ "$service" = "$name" ] || return 1
  [[ $expected_executable == /* ]] || return 1
  [[ $expected_cmdline =~ ^[0-9a-f]{64}$ ]] || return 1
  token_suffix=${token#"$name-"}
  [ "$token_suffix" != "$token" ] && [[ $token_suffix =~ ^[0-9a-f]{64}$ ]] || return 1
}

VALIDATED_PREPARED_PID=
VALIDATED_PREPARED_STARTTIME=
VALIDATED_PREPARED_RECORD_HASH=

validate_prepared_launch_service() {
  local name=$1 require_leader=$2 required_record_hash=${3:-} required_starttime=${4:-}
  local file record_hash pid starttime= expected_executable expected_cmdline token actual_sid actual_pgid members member
  file=$(launch_file "$name")
  validate_prepared_launch_record "$name" || return 1
  record_hash=$(sha256_file "$file") || return 1
  [ -z "$required_record_hash" ] || [ "$record_hash" = "$required_record_hash" ] || return 1
  pid=$(read_pid "$name") || return 1
  expected_executable=$(identity_field "$file" expected_executable) || return 1
  expected_cmdline=$(identity_field "$file" expected_cmdline_sha256) || return 1
  token=$(identity_field "$file" identity_token) || return 1
  if kill -0 "$pid" 2>/dev/null; then
    starttime=$(proc_starttime "$pid") || return 1
    [ -z "$required_starttime" ] || [ "$starttime" = "$required_starttime" ] || return 1
    [ "$(readlink -f "/proc/$pid/exe")" = "$expected_executable" ] || return 1
    [ "$(proc_cmdline_hash "$pid")" = "$expected_cmdline" ] || return 1
    actual_sid=$(ps -o sid= -p "$pid" | tr -d ' ') || return 1
    actual_pgid=$(ps -o pgid= -p "$pid" | tr -d ' ') || return 1
    [ "$actual_sid" = "$pid" ] && [ "$actual_pgid" = "$pid" ] || return 1
    member_has_token "$pid" "$token" || return 1
  elif [ "$require_leader" = 1 ]; then
    return 1
  else
    [ -n "$required_starttime" ] || return 1
    starttime=$required_starttime
  fi
  members=$(group_members "$pid" "$pid") || return 1
  if [ -z "$members" ]; then
    [ "$require_leader" = 0 ] || return 1
  else
    while IFS= read -r member; do
      [[ $member =~ ^[0-9]+$ ]] || return 1
      validate_group_member "$member" "$pid" "$pid" "$token" || return 1
    done <<<"$members"
  fi
  [ "$(sha256_file "$file")" = "$record_hash" ] || return 1
  VALIDATED_PREPARED_PID=$pid
  VALIDATED_PREPARED_STARTTIME=$starttime
  VALIDATED_PREPARED_RECORD_HASH=$record_hash
}

remove_absent_prepared_launch() {
  local name=$1 file identity launch environment record_hash pid attempt members
  file=$(pidfile "$name")
  identity=$(identity_file "$name")
  launch=$(launch_file "$name")
  environment=$(service_env_file "$name")
  validate_prepared_launch_record "$name" || return 1
  record_hash=$(sha256_file "$launch") || return 1
  pid=$(read_pid "$name") || return 1
  for attempt in $(seq 1 "$STOP_KILL_ATTEMPTS"); do
    members=$(group_members "$pid" "$pid") || return 1
    if ! kill -0 "$pid" 2>/dev/null && [ -z "$members" ]; then
      break
    fi
    sleep 0.1
  done
  validate_prepared_launch_record "$name" || return 1
  [ "$(sha256_file "$launch")" = "$record_hash" ] || return 1
  [ "$(read_pid "$name")" = "$pid" ] || return 1
  ! kill -0 "$pid" 2>/dev/null || return 1
  members=$(group_members "$pid" "$pid") || return 1
  [ -z "$members" ] || return 1
  rm -f -- "$file" "$identity" "$launch" "$environment"
}

validate_absent_process_record() {
  local name=$1 record=$2 record_hash pid sid pgid members
  validate_process_record "$name" "$record" || return 1
  record_hash=$(sha256_file "$record") || return 1
  pid=$(identity_field "$record" pid) || return 1
  sid=$(identity_field "$record" sid) || return 1
  pgid=$(identity_field "$record" pgid) || return 1
  ! kill -0 "$pid" 2>/dev/null || return 1
  members=$(group_members "$pgid" "$sid") || return 1
  [ -z "$members" ] || return 1
  validate_process_record "$name" "$record" || return 1
  [ "$(sha256_file "$record")" = "$record_hash" ] || return 1
  [ "$(read_pid "$name")" = "$pid" ] || return 1
  ! kill -0 "$pid" 2>/dev/null || return 1
  members=$(group_members "$pgid" "$sid") || return 1
  [ -z "$members" ]
}

remove_absent_process_record() {
  local name=$1 record=$2 file identity launch environment
  file=$(pidfile "$name")
  identity=$(identity_file "$name")
  launch=$(launch_file "$name")
  environment=$(service_env_file "$name")
  validate_absent_process_record "$name" "$record" || return 1
  rm -f -- "$file" "$identity" "$launch" "$environment"
}

validate_service_identity_from() {
  local name=$1 require_leader=$2 file=$3 pid starttime sid pgid expected_executable expected_cmdline token actual_sid actual_pgid members member
  validate_process_record "$name" "$file" || return 1
  pid=$(identity_field "$file" pid)
  starttime=$(identity_field "$file" starttime)
  sid=$(identity_field "$file" sid)
  pgid=$(identity_field "$file" pgid)
  expected_executable=$(identity_field "$file" expected_executable)
  expected_cmdline=$(identity_field "$file" expected_cmdline_sha256)
  token=$(identity_field "$file" identity_token)
  if kill -0 "$pid" 2>/dev/null; then
    [ "$(proc_starttime "$pid")" = "$starttime" ] || return 1
    [ "$(readlink -f "/proc/$pid/exe")" = "$expected_executable" ] || return 1
    [ "$(proc_cmdline_hash "$pid")" = "$expected_cmdline" ] || return 1
    actual_sid=$(ps -o sid= -p "$pid" | tr -d ' ') || return 1
    actual_pgid=$(ps -o pgid= -p "$pid" | tr -d ' ') || return 1
    [ "$actual_sid" = "$sid" ] && [ "$actual_pgid" = "$pgid" ] || return 1
    member_has_token "$pid" "$token" || return 1
  elif [ "$require_leader" = 1 ]; then
    return 1
  fi
  members=$(group_members "$pgid" "$sid") || return 1
  if [ -z "$members" ]; then
    [ "$require_leader" = 0 ] || return 1
    return 0
  fi
  while IFS= read -r member; do
    [[ $member =~ ^[0-9]+$ ]] || return 1
    validate_group_member "$member" "$pgid" "$sid" "$token" || return 1
  done <<<"$members"
}

validate_service_identity() {
  validate_service_identity_from "$1" "${2:-1}" "$(identity_file "$1")"
}

port_listening() {
  local port=$1 listeners status
  if listeners=$(ss -H -tln "sport = :$port" 2>/dev/null); then
    :
  else
    status=$?
    warn "listener probe for port $port failed because ss exited $status"
    return 2
  fi
  [ -n "$listeners" ] || return 1
  if ! awk -v port="$port" '
    NF < 5 || $1 != "LISTEN" || $4 !~ (":" port "$") { bad=1 }
    END { exit bad }
  ' <<<"$listeners"; then
    warn "listener probe for port $port returned malformed ss output"
    return 2
  fi
  return 0
}

require_port_absent() {
  local port=$1 message=$2 status
  if port_listening "$port"; then
    die "$message"
  else
    status=$?
    [ "$status" -eq 1 ] || die "listener probe for port $port failed; refusing to mutate HTTPS harness state"
  fi
}

require_port_listening() {
  local port=$1 message=$2 status
  if port_listening "$port"; then
    return 0
  else
    status=$?
    [ "$status" -eq 1 ] || die "listener probe for port $port failed; refusing to treat the port as absent"
    die "$message"
  fi
}

count_occurrences() {
  local haystack=$1 needle=$2 count=0
  while [[ $haystack == *"$needle"* ]]; do
    haystack=${haystack#*"$needle"}
    count=$((count + 1))
  done
  printf '%d' "$count"
}

replace_exact() {
  local content=$1 old=$2 new=$3 expected=${4:-1} count
  count=$(count_occurrences "$content" "$old")
  if [ "$count" != "$expected" ]; then
    warn "$CADDYFILE_SRC: found $count occurrence(s) of '$old', want exactly $expected"
    return 1
  fi
  printf '%s' "${content//"$old"/"$new"}"
}

generate_caddyfile() {
  local content fragment generated
  node "$ROOT/scripts/generate-public-roots.mjs" --check >/dev/null || return 1
  content=$(<"$CADDYFILE_SRC") || return 1
  fragment=$(<"$PUBLIC_ROOTS_FRAGMENT") || return 1
  generated=$'\t@uat_google_authorize path /__uat/oauth/google/authorize\n\thandle @uat_google_authorize {\n\t\treverse_proxy 127.0.0.1:'"${MOCK_PORT}"$'\n\t}\n\n'"$fragment"
  content=$(replace_exact "$content" "$PUBLIC_ROOTS_MARKER" "$generated") || return 1
  content=$(replace_exact "$content" $'\tadmin off' \
    $'\tadmin off\n\tskip_install_trust\n\tauto_https disable_redirects') || return 1
  content=$(replace_exact "$content" ':80 {' \
    "https://localhost:${CADDY_PORT} {"$'\n\tbind 127.0.0.1\n\ttls internal') || return 1
  content=$(replace_exact "$content" 'reverse_proxy server:8080 {' \
    "reverse_proxy 127.0.0.1:${SERVER_PORT} {" 2) || return 1
  content=$(replace_exact "$content" 'reverse_proxy web:3000' \
    "reverse_proxy 127.0.0.1:${WEB_PORT}" 2) || return 1
  printf '%s\n' "$content"
}

require_tools() {
  local tool
  for tool in podman go npm node caddy curl ss setsid make sha256sum readlink ps awk grep stat install sed tail chmod mv find base64 tr head; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is not on PATH"
  done
}

load_source_digests() {
  local output line
  output=$(node "$ROOT/scripts/generate-public-roots.mjs" --check) ||
    die "public-root generation check failed"
  APP_BUILD_DIGEST=
  PUBLIC_RENDERER_BUILD_DIGEST=
  while IFS= read -r line; do
    case $line in
    APP_BUILD_DIGEST=sha256:[0-9a-f]*) APP_BUILD_DIGEST=${line#APP_BUILD_DIGEST=} ;;
    PUBLIC_RENDERER_BUILD_DIGEST=sha256:[0-9a-f]*) PUBLIC_RENDERER_BUILD_DIGEST=${line#PUBLIC_RENDERER_BUILD_DIGEST=} ;;
    *) die "public-root generator returned an unexpected digest line" ;;
    esac
  done <<<"$output"
  [[ $APP_BUILD_DIGEST =~ ^sha256:[0-9a-f]{64}$ ]] || die "APP_BUILD_DIGEST is invalid"
  [[ $PUBLIC_RENDERER_BUILD_DIGEST =~ ^sha256:[0-9a-f]{64}$ ]] ||
    die "PUBLIC_RENDERER_BUILD_DIGEST is invalid"
}

# base64url_of encodes a raw 32-byte secret file as unpadded base64url, the
# exact form internal/config decodes. The value is returned, never logged.
base64url_of() {
  base64 -w0 -- "$1" | tr '+/' '-_' | tr -d '='
}

validate_secret_file() {
  local file=$1 mode size canonical owner
  [ ! -L "$file" ] || die "state secret is a symlink: $file"
  [ -f "$file" ] || die "state secret is not a regular file: $file"
  canonical=$(readlink -f -- "$file" 2>/dev/null) || die "state secret cannot be resolved: $file"
  [ "$canonical" = "$file" ] || die "state secret is noncanonical: $file"
  owner=$(stat -c '%u' -- "$file" 2>/dev/null) || die "state secret ownership cannot be read: $file"
  [ "$owner" = "$EUID" ] || die "state secret is not owned by uid $EUID: $file"
  mode=$(stat -c '%a' -- "$file" 2>/dev/null) || die "state secret mode cannot be read: $file"
  [ "$mode" = 600 ] || die "state secret must have mode 600: $file"
  size=$(stat -c '%s' -- "$file" 2>/dev/null) || die "state secret size cannot be read: $file"
  [ "$size" = 32 ] || die "state secret must be 32 bytes: $file"
}

# ensure_secrets creates the mode-0600 random rate/mail/capture secrets once,
# then reuses them across down/up. It never rotates implicitly and never prints
# a value.
ensure_secrets() {
  local name file
  mkdir -p -- "$SECRETS_DIR"
  chmod 0700 -- "$SECRETS_DIR"
  for name in password-rate-hmac-key auth-email-active-key auth-email-capture-bearer; do
    file="$SECRETS_DIR/$name"
    if [ ! -e "$file" ] && [ ! -L "$file" ]; then
      umask 077
      head -c 32 /dev/urandom >"$file" || die "could not generate state secret $file"
      chmod 0600 -- "$file"
    fi
    validate_secret_file "$file"
  done
  PASSWORD_RATE_HMAC_KEY_B64=$(base64url_of "$SECRETS_DIR/password-rate-hmac-key")
  AUTH_EMAIL_ACTIVE_KEY_B64=$(base64url_of "$SECRETS_DIR/auth-email-active-key")
  AUTH_EMAIL_CAPTURE_BEARER_B64=$(base64url_of "$SECRETS_DIR/auth-email-capture-bearer")
}

# load_secrets sets the derived secret values from existing state without
# creating anything, so read-only commands (logs) never mutate state.
load_secrets() {
  PASSWORD_RATE_HMAC_KEY_B64=
  AUTH_EMAIL_ACTIVE_KEY_B64=
  AUTH_EMAIL_CAPTURE_BEARER_B64=
  [ -f "$SECRETS_DIR/password-rate-hmac-key" ] || return 0
  PASSWORD_RATE_HMAC_KEY_B64=$(base64url_of "$SECRETS_DIR/password-rate-hmac-key")
  AUTH_EMAIL_ACTIVE_KEY_B64=$(base64url_of "$SECRETS_DIR/auth-email-active-key")
  AUTH_EMAIL_CAPTURE_BEARER_B64=$(base64url_of "$SECRETS_DIR/auth-email-capture-bearer")
}

mail_secrets_sha256() {
  sha256sum "$SECRETS_DIR/password-rate-hmac-key" "$SECRETS_DIR/auth-email-active-key" "$SECRETS_DIR/auth-email-capture-bearer" \
    | sha256sum | awk '{print $1}'
}

normal_native_active() {
  local name pid file
  for name in server web caddy; do
    file=$ROOT/.dev/$name.pid
    [ -s "$file" ] || continue
    pid=$(<"$file")
    [[ $pid =~ ^[0-9]+$ ]] || continue
    is_ours "$pid" && return 0
  done
  return 1
}

compose_http_active() {
  local containers
  containers=$(podman ps --format '{{.Names}}|{{.Label "com.docker.compose.project"}}|{{.Label "com.docker.compose.service"}}' 2>/dev/null) || \
    die "could not inspect running Podman containers"
  awk -F '|' '$2 == "aboutme" && $3 != "postgres" { found=1 } END { exit !found }' <<<"$containers"
}

any_pidfile() {
  local name
  for name in "${SERVICES[@]}"; do
    [ -e "$(pidfile "$name")" ] && return 0
  done
  return 1
}

assert_owned_complete_state() {
  local generated name pid port mode
  for name in "${SERVICES[@]}"; do
    [ -e "$(pidfile "$name")" ] || die "partial HTTPS harness state: missing $(pidfile "$name"); run 'scripts/dev-https.sh down' after inspecting ownership"
    pid=$(read_pid "$name") || die "$name has an invalid PID file; recovery requires manual inspection because ownership is unproved"
    validate_service_identity "$name" 1 || die "$name pid $pid does not match its exact owned process identity; no process was signalled and ownership evidence was preserved"
    port=$(port_of "$name")
    require_port_listening "$port" "$name pid $pid is owned but port $port is not listening; run 'scripts/dev-https.sh down'"
  done
  generated=$(generate_caddyfile) || die "could not regenerate the HTTPS route table; run 'scripts/dev-https.sh down' before applying config changes"
  [ -f "$CADDYFILE_GEN" ] && [ "$generated" = "$(<"$CADDYFILE_GEN")" ] || \
    die "HTTPS harness configuration changed while it was running; run 'scripts/dev-https.sh down' then 'scripts/dev-https.sh up'"
  [ -r "$EXPORTED_ROOT" ] || die "exported Caddy root is missing or unreadable; run 'scripts/dev-https.sh down' then 'scripts/dev-https.sh up'"
  mode=$(stat -c '%a' "$EXPORTED_ROOT" 2>/dev/null || true)
  [ "$mode" = 600 ] || die "exported Caddy root has mode $mode, want 600; run 'scripts/dev-https.sh down'"
  validate_effective_config || die "effective config, source, binary, route, or process identity drifted; run 'scripts/dev-https.sh down' then rebuild with 'scripts/dev-https.sh up'"
}

reject_http_stack() {
  local port
  normal_native_active && die "the normal native HTTP stack is active; stop it with 'scripts/dev-native.sh down'"
  compose_http_active && die "the Compose HTTP stack is active; stop it with 'make dev-down'"
  for port in "${NORMAL_NATIVE_PORTS[@]}"; do
    require_port_absent "$port" "normal native HTTP port $port is listening; stop the HTTP stack before starting HTTPS"
  done
  return 0
}

preflight_new_stack() {
  local name port
  [ -d "$ROOT/apps/web/node_modules" ] || die "apps/web/node_modules is missing; run 'npm ci' in apps/web first"
  generate_caddyfile >/dev/null || die "the deployed Caddy route table no longer matches the exact HTTPS substitutions"
  for name in "${SERVICES[@]}"; do
    port=$(port_of "$name")
    require_port_absent "$port" "port $port is already in use by a process this script did not start"
  done
  return 0
}

write_process_record() {
  local file=$1 name=$2 pid=$3 starttime=$4 sid=$5 pgid=$6 expected_executable=$7 expected_cmdline=$8 token=$9 temporary
  temporary="$file.tmp.$BASHPID"
  umask 077
  {
    printf 'version=1\n'
    printf 'service=%s\n' "$name"
    printf 'pid=%s\n' "$pid"
    printf 'starttime=%s\n' "$starttime"
    printf 'sid=%s\n' "$sid"
    printf 'pgid=%s\n' "$pgid"
    printf 'expected_executable=%s\n' "$expected_executable"
    printf 'expected_cmdline_sha256=%s\n' "$expected_cmdline"
    printf 'identity_token=%s\n' "$token"
  } >"$temporary" || return 1
  chmod 0600 "$temporary" || { rm -f -- "$temporary"; return 1; }
  mv -f -- "$temporary" "$file" || { rm -f -- "$temporary"; return 1; }
  chmod 0600 "$file"
}

write_launch_intent() {
  local name=$1 token=$2 file expected_cmdline
  file=$(launch_file "$name")
  expected_cmdline=$(expected_service_cmdline_hash "$name") || return 1
  umask 077
  {
    printf 'version=1\n'
    printf 'launch_state=prepared\n'
    printf 'service=%s\n' "$name"
    printf 'expected_executable=%s\n' "$BASH_BIN"
    printf 'expected_cmdline_sha256=%s\n' "$expected_cmdline"
    printf 'identity_token=%s\n' "$token"
  } >"$file" || return 1
  chmod 0600 "$file"
}

complete_launch_record() {
  local name=$1 token=$2 file mode version state service expected_executable expected_cmdline recorded_token
  local pid starttime sid pgid actual_executable actual_cmdline
  file=$(launch_file "$name")
  mode=$(stat -c '%a' "$file" 2>/dev/null || true)
  [ "$mode" = 600 ] || return 1
  version=$(identity_field "$file" version) || return 1
  state=$(identity_field "$file" launch_state) || return 1
  service=$(identity_field "$file" service) || return 1
  expected_executable=$(identity_field "$file" expected_executable) || return 1
  expected_cmdline=$(identity_field "$file" expected_cmdline_sha256) || return 1
  recorded_token=$(identity_field "$file" identity_token) || return 1
  [ "$version" = 1 ] && [ "$state" = prepared ] && [ "$service" = "$name" ] || return 1
  [ "$expected_executable" = "$BASH_BIN" ] || return 1
  [ "$expected_cmdline" = "$(expected_service_cmdline_hash "$name")" ] || return 1
  [ "$recorded_token" = "$token" ] || return 1
  pid=$(read_pid "$name") || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  starttime=$(proc_starttime "$pid") || return 1
  sid=$(ps -o sid= -p "$pid" | tr -d ' ') || return 1
  pgid=$(ps -o pgid= -p "$pid" | tr -d ' ') || return 1
  [ "$pid" = "$sid" ] && [ "$pid" = "$pgid" ] || return 1
  actual_executable=$(readlink -f "/proc/$pid/exe") || return 1
  [ "$actual_executable" = "$expected_executable" ] || return 1
  actual_cmdline=$(proc_cmdline_hash "$pid") || return 1
  [ "$actual_cmdline" = "$expected_cmdline" ] || return 1
  write_process_record "$file" "$name" "$pid" "$starttime" "$sid" "$pgid" \
    "$expected_executable" "$expected_cmdline" "$token" || return 1
  validate_service_identity_from "$name" 1 "$file"
}

write_identity() {
  local name=$1 token=$2 launch identity pid starttime sid pgid expected_executable expected_cmdline recorded_token
  launch=$(launch_file "$name")
  identity=$(identity_file "$name")
  validate_service_identity_from "$name" 1 "$launch" || return 1
  pid=$(identity_field "$launch" pid) || return 1
  starttime=$(identity_field "$launch" starttime) || return 1
  sid=$(identity_field "$launch" sid) || return 1
  pgid=$(identity_field "$launch" pgid) || return 1
  expected_executable=$(identity_field "$launch" expected_executable) || return 1
  expected_cmdline=$(identity_field "$launch" expected_cmdline_sha256) || return 1
  recorded_token=$(identity_field "$launch" identity_token) || return 1
  [ "$recorded_token" = "$token" ] || return 1
  write_process_record "$identity" "$name" "$pid" "$starttime" "$sid" "$pgid" \
    "$expected_executable" "$expected_cmdline" "$token" || return 1
  validate_service_identity "$name" 1
}

write_service_env() {
  local name=$1 file
  file=$(service_env_file "$name")
  umask 077
  case $name in
  mock-oauth)
    printf 'LISTEN_HOST=%q\nPORT=%q\nPUBLIC_ORIGIN=%q\nGOOGLE_CLIENT_ID=%q\nGOOGLE_CLIENT_SECRET=%q\n' \
      127.0.0.1 "$MOCK_PORT" "$PUBLIC_ORIGIN" "$GOOGLE_CLIENT_ID" "$GOOGLE_CLIENT_SECRET" >"$file"
    ;;
  server)
    printf 'PORT=%q\nLISTEN_HOST=%q\nDATABASE_URL=%q\nENV=%q\nPUBLIC_ORIGIN=%q\nPUBLIC_RENDER_ORIGIN=%q\nAPP_BUILD_DIGEST=%q\nPUBLIC_RENDERER_BUILD_DIGEST=%q\nTRUSTED_PROXY_CIDRS=%q\nLOG_LEVEL=%q\nMEDIA_BACKEND=%q\nMEDIA_FS_DIR=%q\nGOOGLE_CLIENT_ID=%q\nGOOGLE_CLIENT_SECRET=%q\nGOOGLE_OIDC_ISSUER_URL=%q\nPASSWORD_RATE_HMAC_KEY=%q\nAUTH_EMAIL_ACTIVE_KEY_ID=%q\nAUTH_EMAIL_ACTIVE_KEY=%q\nAUTH_EMAIL_MODE=%q\nAUTH_EMAIL_CAPTURE_URL=%q\nAUTH_EMAIL_CAPTURE_BEARER=%q\nMCP_ENABLED=%q\n' \
      "$SERVER_PORT" 127.0.0.1 "$DATABASE_URL" dev "$PUBLIC_ORIGIN" "$PUBLIC_RENDER_ORIGIN" "$APP_BUILD_DIGEST" "$PUBLIC_RENDERER_BUILD_DIGEST" 127.0.0.1/32 "$LOG_LEVEL" fs "$MEDIA_DIR" \
      "$GOOGLE_CLIENT_ID" "$GOOGLE_CLIENT_SECRET" "$GOOGLE_ISSUER_URL" \
      "$PASSWORD_RATE_HMAC_KEY_B64" "$ACTIVE_KEY_ID" "$AUTH_EMAIL_ACTIVE_KEY_B64" capture "$MAIL_CAPTURE_URL" "$AUTH_EMAIL_CAPTURE_BEARER_B64" true >"$file"
    ;;
  mail-capture) : >"$file" ;;
  web) : >"$file" ;;
  caddy)
    printf 'XDG_CONFIG_HOME=%q\nXDG_DATA_HOME=%q\n' "$CADDY_DIR/config" "$CADDY_DIR/data" >"$file"
    ;;
  *) return 1 ;;
  esac
  chmod 0600 "$file"
}

start_service() {
  local name=$1 workdir=$2 file environment log pid token
  shift 2
  file=$(pidfile "$name")
  environment=$(service_env_file "$name")
  log=$(logfile "$name")
  [ ! -e "$file" ] || return 1
  write_service_env "$name" || return 1
  printf '\n===== %s started %s =====\n' "$name" "$(date -Is)" >>"$log"
  token="$name-$(printf '%s' "$name:$BASHPID:$(date +%s%N)" | sha256sum | awk '{print $1}')"
  write_launch_intent "$name" "$token" || return 1
  STARTED_SERVICES+=("$name")
  ABOUTME_DEV_HTTPS_IDENTITY="$token" setsid --fork "$BASH_BIN" -c "$SUPERVISOR_CODE" \
    "dev-https-$name" "$file" "$workdir" "$environment" "$@" >>"$log" 2>&1 </dev/null
  local attempt
  for attempt in $(seq 1 50); do
    if [ -s "$file" ]; then
      pid=$(<"$file")
      if [[ $pid =~ ^[0-9]+$ ]] && complete_launch_record "$name" "$token"; then
        write_identity "$name" "$token" || return 1
        return 0
      fi
    fi
    sleep 0.1
  done
  return 1
}

wait_http() {
  local name=$1 url=$2 timeout=$3 pid deadline
  pid=$(service_pid "$name") || return 1
  deadline=$((SECONDS + timeout))
  while [ "$SECONDS" -lt "$deadline" ]; do
    validate_service_identity "$name" 1 || return 1
    curl -fsS -o /dev/null --max-time 5 "$url" 2>/dev/null && return 0
    sleep 0.3
  done
  return 1
}

wait_https() {
  local url=$1 timeout=$2 pid deadline
  pid=$(service_pid caddy) || return 1
  deadline=$((SECONDS + timeout))
  while [ "$SECONDS" -lt "$deadline" ]; do
    validate_service_identity caddy 1 || return 1
    curl -fsS -o /dev/null --max-time 5 --cacert "$EXPORTED_ROOT" "$url" 2>/dev/null && return 0
    sleep 0.3
  done
  return 1
}

wait_for_caddy_root() {
  local pid deadline
  pid=$(service_pid caddy) || return 1
  deadline=$((SECONDS + 30))
  while [ "$SECONDS" -lt "$deadline" ]; do
    validate_service_identity caddy 1 || return 1
    [ -s "$CADDY_ROOT" ] && return 0
    sleep 0.2
  done
  return 1
}

stop_owned_service() {
  local name=$1 rollback=${2:-0} file identity launch environment evidence evidence_kind=full
  local pid sid pgid starttime record_hash attempt members
  file=$(pidfile "$name")
  identity=$(identity_file "$name")
  launch=$(launch_file "$name")
  environment=$(service_env_file "$name")
  if [ ! -e "$file" ]; then
    if [ "$rollback" = 1 ] && validate_prepared_launch_record "$name"; then
      rm -f -- "$identity" "$launch" "$environment"
    fi
    return 0
  fi
  if ! pid=$(read_pid "$name"); then
    return 1
  fi
  if validate_service_identity_from "$name" 1 "$identity"; then
    evidence=$identity
  elif [ "$rollback" = 1 ] && validate_service_identity_from "$name" 1 "$launch"; then
    evidence=$launch
  elif validate_absent_process_record "$name" "$identity"; then
    remove_absent_process_record "$name" "$identity"
    return
  elif validate_absent_process_record "$name" "$launch"; then
    remove_absent_process_record "$name" "$launch"
    return
  elif [ "$rollback" = 1 ] && validate_prepared_launch_service "$name" 1; then
    evidence=$launch
    evidence_kind=prepared
    pid=$VALIDATED_PREPARED_PID
    sid=$pid
    pgid=$pid
    starttime=$VALIDATED_PREPARED_STARTTIME
    record_hash=$VALIDATED_PREPARED_RECORD_HASH
  elif [ "$rollback" = 1 ] && remove_absent_prepared_launch "$name"; then
    return 0
  else
    return 1
  fi
  if [ "$evidence_kind" = full ]; then
    sid=$(identity_field "$evidence" sid)
    pgid=$(identity_field "$evidence" pgid)
  fi
  # The complete exact identity and every current group member are rechecked
  # immediately before the first destructive signal.
  if [ "$evidence_kind" = prepared ]; then
    validate_prepared_launch_service "$name" 1 "$record_hash" "$starttime" || return 1
    [ "$VALIDATED_PREPARED_PID" = "$pid" ] || return 1
  else
    validate_service_identity_from "$name" 1 "$evidence" || return 1
  fi
  kill -TERM -- "-$pgid" 2>/dev/null || return 1
  for attempt in $(seq 1 "$STOP_TERM_ATTEMPTS"); do
    members=$(group_members "$pgid" "$sid") || return 1
    [ -z "$members" ] && break
    sleep 0.1
  done
  members=$(group_members "$pgid" "$sid") || return 1
  if [ -n "$members" ]; then
    if [ "$evidence_kind" = prepared ]; then
      validate_prepared_launch_service "$name" 0 "$record_hash" "$starttime" || return 1
      [ "$VALIDATED_PREPARED_PID" = "$pid" ] || return 1
    else
      validate_service_identity_from "$name" 0 "$evidence" || return 1
    fi
    members=$(group_members "$pgid" "$sid") || return 1
    if [ -n "$members" ]; then
      warn "$name process group $pgid did not drain after SIGTERM; sending SIGKILL after re-proving every remaining member"
      kill -KILL -- "-$pgid" 2>/dev/null || return 1
    fi
    for attempt in $(seq 1 "$STOP_KILL_ATTEMPTS"); do
      members=$(group_members "$pgid" "$sid") || return 1
      [ -z "$members" ] && break
      sleep 0.1
    done
  fi
  members=$(group_members "$pgid" "$sid") || return 1
  [ -z "$members" ] || return 1
  rm -f -- "$file" "$identity" "$launch" "$environment"
  info "stopped $name (pid $pid)"
}

rollback_startup() {
  local index name
  warn "startup failed; rolling back only services started by this invocation"
  for ((index = ${#STARTED_SERVICES[@]} - 1; index >= 0; index--)); do
    name=${STARTED_SERVICES[$index]}
    stop_owned_service "$name" 1 || true
  done
}

on_exit() {
  local status=$?
  if [ "$STARTUP_ARMED" = 1 ]; then
    STARTUP_ARMED=0
    rollback_startup
  fi
  exit "$status"
}
trap on_exit EXIT

build_and_migrate() {
  (
    cd "$ROOT/apps/server"
    go build -o "$BIN_DIR/migrate" ./cmd/migrate
    chmod 0755 -- "$BIN_DIR/migrate"
    go build -o "$BIN_DIR/mock-oauth" ./cmd/mock-oauth
    chmod 0755 -- "$BIN_DIR/mock-oauth"
    go build -o "$BIN_DIR/mail-capture" ./cmd/mail-capture
    chmod 0755 -- "$BIN_DIR/mail-capture"
    go build -o "$BIN_DIR/server" ./cmd/server
    chmod 0755 -- "$BIN_DIR/server"
    env DATABASE_URL="$DATABASE_URL" "$BIN_DIR/migrate"
  )
}

start_mock() {
  start_service mock-oauth "$ROOT/apps/server" "$BIN_DIR/mock-oauth"
  wait_http mock-oauth "$GOOGLE_ISSUER_URL/.well-known/openid-configuration" 30
}

# wait_mail_capture treats a bound loopback listener as readiness: the capture
# server authenticates every route, so an HTTP probe cannot be used without
# putting the bearer secret on a command line.
wait_mail_capture() {
  local name=$1 port=$2 timeout=$3 pid deadline status
  pid=$(service_pid "$name") || return 1
  deadline=$((SECONDS + timeout))
  while [ "$SECONDS" -lt "$deadline" ]; do
    validate_service_identity "$name" 1 || return 1
    if port_listening "$port"; then
      return 0
    else
      status=$?
      [ "$status" -eq 1 ] || return 1
    fi
    sleep 0.3
  done
  return 1
}

start_mail_capture() {
  start_service mail-capture "$ROOT/apps/server" "$BIN_DIR/mail-capture" \
    --secret-file "$SECRETS_DIR/auth-email-capture-bearer" --addr "$MAIL_CAPTURE_ADDR"
  wait_mail_capture mail-capture "$MAIL_CAPTURE_PORT" 30
}

start_server() {
  start_service server "$ROOT/apps/server" "$BIN_DIR/server"
  wait_http server "http://127.0.0.1:${SERVER_PORT}/healthz" 30
}

start_web() {
  [ -d "$ROOT/apps/web/node_modules" ] || return 1
  start_service web "$ROOT/apps/web" npm run dev -- --port "$WEB_PORT" --host 127.0.0.1
  wait_http web "http://127.0.0.1:${WEB_PORT}/" 240
}

start_caddy() {
  local generated
  generated=$(generate_caddyfile) || return 1
  printf '%s' "$generated" >"$CADDYFILE_GEN"
  start_service caddy "$ROOT" caddy run --config "$CADDYFILE_GEN" --adapter caddyfile
  wait_for_caddy_root
  install -m 0600 "$CADDY_ROOT" "$EXPORTED_ROOT"
  wait_https "$PUBLIC_ORIGIN/healthz" 30
}

web_source_hash() {
  local path
  {
    for path in apps/web/package.json apps/web/package-lock.json apps/web/nuxt.config.ts; do
      printf '%s\0' "$path"
      if [ -f "$ROOT/$path" ]; then
        sha256_file "$ROOT/$path"
      else
        printf '%s\n' missing
      fi
    done
  } | sha256sum | awk '{print $1}'
}

render_effective_config() {
  local generated_route name file environment environment_mode caddy_bin npm_bin persisted_executable persisted_cmdline
  generated_route=$(generate_caddyfile) || return 1
  caddy_bin=$(command -v caddy) || return 1
  npm_bin=$(command -v npm) || return 1
  printf 'version=1\n'
  printf 'database_target=127.0.0.1:20432/aboutme_dev?sslmode=disable\n'
  printf 'log_level=%s\n' "$LOG_LEVEL"
  printf 'public_origin=%s\n' "$PUBLIC_ORIGIN"
  printf 'public_render_origin=%s\n' "$PUBLIC_RENDER_ORIGIN"
  printf 'app_build_digest=%s\n' "$APP_BUILD_DIGEST"
  printf 'public_renderer_build_digest=%s\n' "$PUBLIC_RENDERER_BUILD_DIGEST"
  printf 'google_client_id=%s\n' "$GOOGLE_CLIENT_ID"
  printf 'google_issuer_url=%s\n' "$GOOGLE_ISSUER_URL"
  printf 'mcp_enabled=true\n'
  printf 'web_port=%s\nserver_port=%s\nmock_port=%s\ncaddy_port=%s\nmail_capture_port=%s\n' \
    "$WEB_PORT" "$SERVER_PORT" "$MOCK_PORT" "$CADDY_PORT" "$MAIL_CAPTURE_PORT"
  printf 'stop_term_attempts=%s\nstop_kill_attempts=%s\n' "$STOP_TERM_ATTEMPTS" "$STOP_KILL_ATTEMPTS"
  printf 'lifecycle_source_sha256=%s\n' "$(sha256_file "$ROOT/scripts/dev-https.sh")"
  printf 'deployed_route_source_sha256=%s\n' "$(sha256_file "$CADDYFILE_SRC")"
  printf 'registry_source_sha256=%s\n' "$(sha256_file "$PUBLIC_ROOTS_REGISTRY")"
  printf 'generated_fragment_sha256=%s\n' "$(sha256_file "$PUBLIC_ROOTS_FRAGMENT")"
  printf 'generated_route_sha256=%s\n' "$(printf '%s' "$generated_route" | sha256sum | awk '{print $1}')"
  printf 'effective_caddyfile_sha256=%s\n' "$(sha256_file "$CADDYFILE_GEN")"
  printf 'web_source_sha256=%s\n' "$(web_source_hash)"
  printf 'mail_secrets_sha256=%s\n' "$(mail_secrets_sha256)"
  printf 'migrate_binary_sha256=%s\n' "$(sha256_file "$BIN_DIR/migrate")"
  printf 'mock_oauth_binary_sha256=%s\n' "$(sha256_file "$BIN_DIR/mock-oauth")"
  printf 'mail_capture_binary_sha256=%s\n' "$(sha256_file "$BIN_DIR/mail-capture")"
  printf 'server_binary_sha256=%s\n' "$(sha256_file "$BIN_DIR/server")"
  printf 'caddy_tool_sha256=%s\n' "$(sha256_file "$caddy_bin")"
  printf 'npm_tool_sha256=%s\n' "$(sha256_file "$npm_bin")"
  for name in "${SERVICES[@]}"; do
    file=$(identity_file "$name")
    environment=$(service_env_file "$name")
    environment_mode=$(stat -c '%a' "$environment" 2>/dev/null || true)
    [ "$environment_mode" = 600 ] || return 1
    validate_identity_record "$name" || return 1
    persisted_executable=$(identity_field "$file" expected_executable) || return 1
    persisted_cmdline=$(identity_field "$file" expected_cmdline_sha256) || return 1
    [ "$persisted_executable" = "$BASH_BIN" ] || return 1
    [ "$persisted_cmdline" = "$(expected_service_cmdline_hash "$name")" ] || return 1
    printf '%s_environment_sha256=%s\n' "$name" "$(sha256_file "$environment")"
    printf '%s_pid=%s\n' "$name" "$(identity_field "$file" pid)"
    printf '%s_starttime=%s\n' "$name" "$(identity_field "$file" starttime)"
    printf '%s_sid=%s\n' "$name" "$(identity_field "$file" sid)"
    printf '%s_pgid=%s\n' "$name" "$(identity_field "$file" pgid)"
    printf '%s_expected_executable=%s\n' "$name" "$persisted_executable"
    printf '%s_expected_cmdline_sha256=%s\n' "$name" "$persisted_cmdline"
    printf '%s_identity_token=%s\n' "$name" "$(identity_field "$file" identity_token)"
  done
}

write_effective_config() {
  umask 077
  render_effective_config >"$EFFECTIVE_CONFIG" || return 1
  chmod 0600 "$EFFECTIVE_CONFIG"
}

validate_effective_config() {
  local mode expected actual
  [ -f "$EFFECTIVE_CONFIG" ] || return 1
  mode=$(stat -c '%a' "$EFFECTIVE_CONFIG" 2>/dev/null || true)
  [ "$mode" = 600 ] || return 1
  expected=$(render_effective_config) || return 1
  actual=$(<"$EFFECTIVE_CONFIG")
  [ "$actual" = "$expected" ]
}

start_stack() {
  make -C "$ROOT" --no-print-directory tools-check ARGS=dev
  make -C "$ROOT" --no-print-directory test-db-up
  build_and_migrate
  ensure_secrets
  start_mock
  start_mail_capture
  start_server
  start_web
  start_caddy
  write_effective_config
}

cmd_up() {
  [ -z "${ABOUTME_DEV_DATABASE_URL+x}" ] || die "ABOUTME_DEV_DATABASE_URL is not permitted; the HTTPS harness uses only 127.0.0.1:20432/aboutme_dev"
  require_tools
  load_source_digests
  validate_state_path_integrity
  reject_http_stack
  if any_pidfile; then
    assert_owned_complete_state
    cmd_status
    info "native HTTPS harness is already up at $PUBLIC_ORIGIN"
    return 0
  fi
  preflight_new_stack
  prepare_state_directories
  STARTUP_ARMED=1
  start_stack
  STARTUP_ARMED=0
  info "native HTTPS harness is up at $PUBLIC_ORIGIN"
  cmd_status
}

validate_down_targets() {
  local name file identity launch pid
  for name in "${STOP_ORDER[@]}"; do
    file=$(pidfile "$name")
    [ -e "$file" ] || continue
    pid=$(read_pid "$name") || die "$name has an invalid PID file; no process was signalled"
    identity=$(identity_file "$name")
    launch=$(launch_file "$name")
    if validate_service_identity "$name" 1 \
      || validate_absent_process_record "$name" "$identity" \
      || validate_absent_process_record "$name" "$launch"; then
      continue
    fi
    die "$name pid $pid does not match its exact owned process identity; no process was signalled and ownership evidence was preserved"
  done
  return 0
}

cmd_down() {
  local name
  validate_state_path_integrity
  validate_down_targets
  for name in "${STOP_ORDER[@]}"; do
    stop_owned_service "$name"
  done
  info "native HTTPS harness is down (the shared $DB_CONTAINER container is left running)"
}

probe_https() {
  [ -r "$EXPORTED_ROOT" ] || return 1
  curl -fsS -o /dev/null --max-time 5 --cacert "$EXPORTED_ROOT" "$PUBLIC_ORIGIN/healthz" 2>/dev/null
}

cmd_status() {
  local name pid port state listening probe_status failed=0 mode generated
  load_source_digests
  validate_state_path_integrity
  printf '%-12s %-8s %-10s %-6s %s\n' SERVICE PID STATE PORT LISTENING
  for name in "${SERVICES[@]}"; do
    port=$(port_of "$name")
    if pid=$(service_pid "$name" 2>/dev/null); then
      state=running
    elif pid=$(read_pid "$name" 2>/dev/null); then
      state=UNOWNED
      failed=1
    else
      pid=-
      state=stopped
      failed=1
    fi
    if port_listening "$port"; then
      listening=yes
    else
      probe_status=$?
      if [ "$probe_status" -eq 1 ]; then
        listening=no
        [ "$state" = running ] && failed=1
      else
        listening=ERROR
        failed=1
      fi
    fi
    printf '%-12s %-8s %-10s %-6s %s\n' "$name" "$pid" "$state" "$port" "$listening"
  done
  mode=$(stat -c '%a' "$EXPORTED_ROOT" 2>/dev/null || true)
  if [ "$mode" = 600 ] && probe_https; then
    info "TLS          trusted by exported project CA; public readiness=yes"
  else
    info "TLS          exported CA/readiness check failed"
    failed=1
  fi
  if generated=$(generate_caddyfile) && [ -f "$CADDYFILE_GEN" ] && [ "$generated" = "$(<"$CADDYFILE_GEN")" ]; then
    info "CONFIG       generated route table matches its deployed source"
  else
    info "CONFIG       generated route table is missing or mismatched"
    failed=1
  fi
  if validate_effective_config; then
    info "MANIFEST     effective config, source, binaries, and process identities match"
  else
    info "MANIFEST     effective config, source, binary, route, or process identity mismatch"
    failed=1
  fi
  [ "$failed" -eq 0 ] || {
    info "recovery: inspect 'scripts/dev-https.sh logs'; use down only when every PID is proved owned"
    return 1
  }
}

redact_logs() {
  local -a secret_rules=()
  [ -z "${PASSWORD_RATE_HMAC_KEY_B64-}" ] || secret_rules+=(-e "s/${PASSWORD_RATE_HMAC_KEY_B64}/[REDACTED]/g")
  [ -z "${AUTH_EMAIL_ACTIVE_KEY_B64-}" ] || secret_rules+=(-e "s/${AUTH_EMAIL_ACTIVE_KEY_B64}/[REDACTED]/g")
  [ -z "${AUTH_EMAIL_CAPTURE_BEARER_B64-}" ] || secret_rules+=(-e "s/${AUTH_EMAIL_CAPTURE_BEARER_B64}/[REDACTED]/g")
  sed -E \
    -e 's/(GOOGLE_CLIENT_SECRET[=:][[:space:]]*)[^[:space:]]+/\1[REDACTED]/g' \
    -e 's/(__Host-(session|oauth-tx)=)[^;[:space:]]+/\1[REDACTED]/gI' \
    -e 's/("Authorization"[[:space:]]*:[[:space:]]*"Bearer[[:space:]]+)[^"]*(")/\1[REDACTED]\2/gI' \
    -e 's/("(access_token|id_token|refresh_token|X-CSRF-Token|csrfToken|code|state)"[[:space:]]*:[[:space:]]*")[^"]*(")/\1[REDACTED]\3/gI' \
    -e 's/(X-CSRF-Token[[:space:]]*[=:][[:space:]]*)[^,;&[:space:]]+/\1[REDACTED]/gI' \
    -e 's/(Authorization[[:space:]]*[=:][[:space:]]*Bearer[[:space:]]+)[^,;&[:space:]]+/\1[REDACTED]/gI' \
    -e 's/([?&](code|state)=)[^&#[:space:]]+/\1[REDACTED]/g' \
    -e 's/((authorization_code|access_token|id_token|refresh_token|csrf_token|csrfToken|session_cookie)[[:space:]]*[=:][[:space:]]*)[^,;&[:space:]]+/\1[REDACTED]/gI' \
    -e "s/${GOOGLE_CLIENT_SECRET//\//\\\/}/[REDACTED]/g" \
    "${secret_rules[@]}"
}

cmd_logs() {
  local follow=0 arg name
  local -a names=() files=()
  validate_state_path_integrity
  load_secrets
  for arg in "$@"; do
    case $arg in
    -f | --follow) follow=1 ;;
    mock-oauth | mail-capture | server | web | caddy) names+=("$arg") ;;
    *) die "logs: unknown argument '$arg'" ;;
    esac
  done
  [ "${#names[@]}" -gt 0 ] || names=("${SERVICES[@]}")
  for name in "${names[@]}"; do
    [ -f "$(logfile "$name")" ] && files+=("$(logfile "$name")")
  done
  [ "${#files[@]}" -gt 0 ] || die "no harness logs exist under $LOG_DIR"
  if [ "$follow" = 1 ]; then
    tail -n 50 -F "${files[@]}" | redact_logs
  else
    tail -n 200 "${files[@]}" | redact_logs
  fi
}

usage() {
  cat <<EOF
usage: scripts/dev-https.sh <up|down|status|logs> [args]

  up      start or verify the native HTTPS harness at $PUBLIC_ORIGIN
  down    stop caddy, web, server, mail-capture, then mock-oauth when ownership is proved
  status  show bounded liveness, listener, CA, and public readiness checks
  logs    [-f] [mock-oauth|mail-capture|server|web|caddy]  show only harness-owned logs
EOF
}

main() {
  local command=${1:-}
  [[ $STOP_TERM_ATTEMPTS =~ ^[1-9][0-9]*$ ]] || die "DEV_HTTPS_STOP_TERM_ATTEMPTS must be a positive integer"
  [[ $STOP_KILL_ATTEMPTS =~ ^[1-9][0-9]*$ ]] || die "DEV_HTTPS_STOP_KILL_ATTEMPTS must be a positive integer"
  [ "$#" -gt 0 ] && shift
  case $command in
  up) cmd_up "$@" ;;
  down) cmd_down "$@" ;;
  status) cmd_status "$@" ;;
  logs) cmd_logs "$@" ;;
  help | -h | --help | '') usage ;;
  *) usage >&2; die "unknown command '$command'" ;;
  esac
}

main "$@"
