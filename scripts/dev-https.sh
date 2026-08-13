#!/usr/bin/env bash
# Native HTTPS authentication harness on https://localhost:20443.
set -Eeuo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
readonly ROOT=$PWD

readonly WEB_PORT=20440
readonly SERVER_PORT=20441
readonly MOCK_PORT=20442
readonly CADDY_PORT=20443
readonly PUBLIC_ORIGIN="https://localhost:${CADDY_PORT}"
readonly GOOGLE_CLIENT_ID=aboutme-local-google
readonly GOOGLE_CLIENT_SECRET=not-a-secret-local-google
readonly GOOGLE_ISSUER_URL="http://127.0.0.1:${MOCK_PORT}/google"
readonly DATABASE_URL="${ABOUTME_DEV_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable}"
readonly LOG_LEVEL="${ABOUTME_DEV_LOG_LEVEL:-info}"

readonly STATE_DIR=$ROOT/.dev/native-https
readonly RUN_DIR=$STATE_DIR/run
readonly LOG_DIR=$STATE_DIR/log
readonly BIN_DIR=$STATE_DIR/bin
readonly MEDIA_DIR=$STATE_DIR/media
readonly CADDY_DIR=$STATE_DIR/caddy
readonly INPUT_DIR=$STATE_DIR/input
readonly CADDYFILE_SRC=$ROOT/deploy/caddy/Caddyfile
readonly CADDYFILE_GEN=$STATE_DIR/Caddyfile
readonly CADDY_ROOT=$CADDY_DIR/data/caddy/pki/authorities/local/root.crt
readonly EXPORTED_ROOT=$INPUT_DIR/caddy-root.crt
readonly DB_CONTAINER=aboutme-test-db
readonly SERVICES=(mock-oauth server web caddy)
readonly STOP_ORDER=(caddy web server mock-oauth)

STARTUP_ARMED=0
declare -a STARTED_SERVICES=()

info() { printf '%s\n' "$*"; }
warn() { printf 'dev-https: %s\n' "$*" >&2; }
die() {
  printf 'dev-https: %s\n' "$*" >&2
  exit 1
}

pidfile() { printf '%s/%s.pid' "$RUN_DIR" "$1"; }
logfile() { printf '%s/%s.log' "$LOG_DIR" "$1"; }

port_of() {
  case $1 in
  mock-oauth) printf '%s' "$MOCK_PORT" ;;
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
  is_ours "$pid" || return 1
  printf '%s' "$pid"
}

port_listening() {
  local listeners
  listeners=$(ss -H -tln "sport = :$1" 2>/dev/null || true)
  [ -n "$listeners" ]
}

count_occurrences() {
  local haystack=$1 needle=$2 count=0
  while [[ $haystack == *"$needle"* ]]; do
    haystack=${haystack#*"$needle"}
    count=$((count + 1))
  done
  printf '%d' "$count"
}

replace_once() {
  local content=$1 old=$2 new=$3 count prefix suffix
  count=$(count_occurrences "$content" "$old")
  if [ "$count" != 1 ]; then
    warn "$CADDYFILE_SRC: found $count occurrence(s) of '$old', want exactly 1"
    return 1
  fi
  prefix=${content%%"$old"*}
  suffix=${content#*"$old"}
  printf '%s' "$prefix$new$suffix"
}

generate_caddyfile() {
  local content
  content=$(<"$CADDYFILE_SRC") || return 1
  content=$(replace_once "$content" ':80 {' \
    "https://localhost:${CADDY_PORT} {"$'\n\tbind 127.0.0.1\n\ttls internal') || return 1
  content=$(replace_once "$content" 'reverse_proxy server:8080 {' \
    "reverse_proxy 127.0.0.1:${SERVER_PORT} {") || return 1
  content=$(replace_once "$content" 'reverse_proxy web:3000' \
    "reverse_proxy 127.0.0.1:${WEB_PORT}") || return 1
  content=$(replace_once "$content" $'\t@print path /print /print/*' \
    $'\t@uat_google_authorize path /__uat/oauth/google/authorize\n\thandle @uat_google_authorize {\n\t\treverse_proxy 127.0.0.1:'"${MOCK_PORT}"$'\n\t}\n\n\t@print path /print /print/*') || return 1
  printf '%s\n' "$content"
}

require_tools() {
  local tool
  for tool in podman go npm caddy curl ss setsid make; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is not on PATH"
  done
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
    is_ours "$pid" || die "$name pid $pid is not an owned session leader; no process was signalled and ownership evidence was preserved"
    port=$(port_of "$name")
    port_listening "$port" || die "$name pid $pid is owned but port $port is not listening; run 'scripts/dev-https.sh down'"
  done
  generated=$(generate_caddyfile) || die "could not regenerate the HTTPS route table; run 'scripts/dev-https.sh down' before applying config changes"
  [ -f "$CADDYFILE_GEN" ] && [ "$generated" = "$(<"$CADDYFILE_GEN")" ] || \
    die "HTTPS harness configuration changed while it was running; run 'scripts/dev-https.sh down' then 'scripts/dev-https.sh up'"
  [ -r "$EXPORTED_ROOT" ] || die "exported Caddy root is missing or unreadable; run 'scripts/dev-https.sh down' then 'scripts/dev-https.sh up'"
  mode=$(stat -c '%a' "$EXPORTED_ROOT" 2>/dev/null || true)
  [ "$mode" = 600 ] || die "exported Caddy root has mode $mode, want 600; run 'scripts/dev-https.sh down'"
}

preflight_new_stack() {
  local name port
  normal_native_active && die "the normal native HTTP stack is active; stop it with 'scripts/dev-native.sh down'"
  compose_http_active && die "the Compose HTTP stack is active; stop it with 'make dev-down'"
  [ -d "$ROOT/apps/web/node_modules" ] || die "apps/web/node_modules is missing; run 'npm ci' in apps/web first"
  generate_caddyfile >/dev/null || die "the deployed Caddy route table no longer matches the exact HTTPS substitutions"
  for name in "${SERVICES[@]}"; do
    port=$(port_of "$name")
    port_listening "$port" && die "port $port is already in use by a process this script did not start"
  done
  return 0
}

start_service() {
  local name=$1 workdir=$2 file log pid
  shift 2
  file=$(pidfile "$name")
  log=$(logfile "$name")
  [ ! -e "$file" ] || return 1
  printf '\n===== %s started %s =====\n' "$name" "$(date -Is)" >>"$log"
  setsid --fork bash -c '
    umask 077
    printf "%s\n" "$$" >"$1"
    cd "$2" || exit 127
    shift 2
    exec "$@"
  ' dev-https "$file" "$workdir" "$@" >>"$log" 2>&1 </dev/null
  STARTED_SERVICES+=("$name")
  local attempt
  for attempt in $(seq 1 50); do
    if [ -s "$file" ]; then
      pid=$(<"$file")
      [[ $pid =~ ^[0-9]+$ ]] && return 0
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
    is_ours "$pid" || return 1
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
    is_ours "$pid" || return 1
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
    is_ours "$pid" || return 1
    [ -s "$CADDY_ROOT" ] && return 0
    sleep 0.2
  done
  return 1
}

stop_owned_service() {
  local name=$1 rollback=${2:-0} file pid attempt
  file=$(pidfile "$name")
  [ -e "$file" ] || return 0
  if ! pid=$(read_pid "$name"); then
    [ "$rollback" = 1 ] && rm -f -- "$file"
    return 1
  fi
  if ! is_ours "$pid"; then
    [ "$rollback" = 1 ] && rm -f -- "$file"
    return 1
  fi
  kill -TERM -- "-$pid" 2>/dev/null || return 1
  for attempt in $(seq 1 100); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.1
  done
  if kill -0 "$pid" 2>/dev/null; then
    warn "$name pid $pid ignored SIGTERM; sending SIGKILL to its proved-owned process group"
    kill -KILL -- "-$pid" 2>/dev/null || return 1
    sleep 0.3
  fi
  rm -f -- "$file"
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
    go build -o "$BIN_DIR/mock-oauth" ./cmd/mock-oauth
    go build -o "$BIN_DIR/server" ./cmd/server
    env DATABASE_URL="$DATABASE_URL" "$BIN_DIR/migrate"
  )
}

start_mock() {
  start_service mock-oauth "$ROOT/apps/server" env \
    LISTEN_HOST=127.0.0.1 PORT="$MOCK_PORT" PUBLIC_ORIGIN="$PUBLIC_ORIGIN" \
    GOOGLE_CLIENT_ID="$GOOGLE_CLIENT_ID" GOOGLE_CLIENT_SECRET="$GOOGLE_CLIENT_SECRET" \
    "$BIN_DIR/mock-oauth"
  wait_http mock-oauth "$GOOGLE_ISSUER_URL/.well-known/openid-configuration" 30
}

start_server() {
  start_service server "$ROOT/apps/server" env \
    PORT="$SERVER_PORT" LISTEN_HOST=127.0.0.1 DATABASE_URL="$DATABASE_URL" \
    ENV=dev PUBLIC_ORIGIN="$PUBLIC_ORIGIN" TRUSTED_PROXY_CIDRS=127.0.0.1/32 \
    LOG_LEVEL="$LOG_LEVEL" MEDIA_BACKEND=fs MEDIA_FS_DIR="$MEDIA_DIR" \
    GOOGLE_CLIENT_ID="$GOOGLE_CLIENT_ID" GOOGLE_CLIENT_SECRET="$GOOGLE_CLIENT_SECRET" \
    GOOGLE_OIDC_ISSUER_URL="$GOOGLE_ISSUER_URL" "$BIN_DIR/server"
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
  start_service caddy "$ROOT" env XDG_CONFIG_HOME="$CADDY_DIR/config" \
    XDG_DATA_HOME="$CADDY_DIR/data" caddy run --config "$CADDYFILE_GEN" --adapter caddyfile
  wait_for_caddy_root
  install -m 0600 "$CADDY_ROOT" "$EXPORTED_ROOT"
  wait_https "$PUBLIC_ORIGIN/healthz" 30
}

start_stack() {
  make -C "$ROOT" --no-print-directory tools-check ARGS=dev
  make -C "$ROOT" --no-print-directory test-db-up
  build_and_migrate
  start_mock
  start_server
  start_web
  start_caddy
}

cmd_up() {
  require_tools
  if any_pidfile; then
    assert_owned_complete_state
    cmd_status
    info "native HTTPS harness is already up at $PUBLIC_ORIGIN"
    return 0
  fi
  preflight_new_stack
  install -d -m 0700 "$STATE_DIR" "$RUN_DIR" "$LOG_DIR" "$BIN_DIR" \
    "$MEDIA_DIR" "$CADDY_DIR/config" "$CADDY_DIR/data" "$INPUT_DIR"
  STARTUP_ARMED=1
  start_stack
  STARTUP_ARMED=0
  info "native HTTPS harness is up at $PUBLIC_ORIGIN"
  cmd_status
}

validate_down_targets() {
  local name file pid
  for name in "${STOP_ORDER[@]}"; do
    file=$(pidfile "$name")
    [ -e "$file" ] || continue
    pid=$(read_pid "$name") || die "$name has an invalid PID file; no process was signalled"
    is_ours "$pid" || die "$name pid $pid is not an owned session leader; no process was signalled and ownership evidence was preserved"
  done
  return 0
}

cmd_down() {
  local name
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
  local name pid port state listening failed=0 mode generated
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
      listening=no
      [ "$state" = running ] && failed=1
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
  [ "$failed" -eq 0 ] || {
    info "recovery: inspect 'scripts/dev-https.sh logs'; use down only when every PID is proved owned"
    return 1
  }
}

redact_logs() {
  sed -E \
    -e 's/(GOOGLE_CLIENT_SECRET[=:][[:space:]]*)[^[:space:]]+/\1[REDACTED]/g' \
    -e 's/(authorization_code|access_token|refresh_token|csrf_token|session_cookie)[=:][^[:space:]]+/\1=[REDACTED]/gi' \
    -e "s/${GOOGLE_CLIENT_SECRET//\//\\\/}/[REDACTED]/g"
}

cmd_logs() {
  local follow=0 arg name
  local -a names=() files=()
  for arg in "$@"; do
    case $arg in
    -f | --follow) follow=1 ;;
    mock-oauth | server | web | caddy) names+=("$arg") ;;
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
  down    stop caddy, web, server, then mock-oauth when ownership is proved
  status  show bounded liveness, listener, CA, and public readiness checks
  logs    [-f] [mock-oauth|server|web|caddy]  show only harness-owned logs
EOF
}

main() {
  local command=${1:-}
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
