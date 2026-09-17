#!/usr/bin/env bash
# Process-identity tracking for the native HTTPS harness: proves a PID
# file, launch record, or identity record still names the exact process
# this script started before any signal or state mutation.
# Sourced by scripts/dev-https.sh; defines functions and constants only.

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
  # A short-lived child can disappear or become a zombie between the group
  # snapshot and its environ read. Recheck its state before treating it as an
  # unmarked live member of the owned group.
  actual=$(ps -o pgid=,sid=,stat= -p "$pid" 2>/dev/null) || return 0
  [ -n "$actual" ] || return 0
  read -r actual_pgid actual_sid state <<<"$actual"
  [ "$actual_pgid" = "$pgid" ] && [ "$actual_sid" = "$sid" ] || return 0
  [[ $state == Z* ]] && return 0
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
