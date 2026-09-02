#!/usr/bin/env bash
# Native development stack: Go server, Nuxt, and Caddy on one origin, backed by
# the shared Postgres container's aboutme_dev database. See
# docs/runbooks/native-development.md.
#
#   scripts/dev-native.sh up      start everything (idempotent)
#   scripts/dev-native.sh down    stop exactly what up started
#   scripts/dev-native.sh status  liveness + ports; non-zero if anything is down
#   scripts/dev-native.sh logs    per-process logs
#   scripts/dev-native.sh seed    seed the development account and sample resume
#
# Open http://localhost:20080 — that is the only URL a browser should use;
# it is PUBLIC_ORIGIN, so cookie scope, CSRF Origin checks, and OAuth
# redirects match what the code expects.
set -Eeuo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
ROOT=$(pwd -P)

info() { printf '%s\n' "$*"; }
warn() { printf 'dev-native: %s\n' "$*" >&2; }
die() {
  printf 'dev-native: %s\n' "$*" >&2
  exit 1
}

# Fixed ports keep PUBLIC_ORIGIN-derived cookies, CSRF, and redirects aligned.
readonly CADDY_PORT=20080
readonly SERVER_PORT=20081
readonly WEB_PORT=20030
readonly MAIL_CAPTURE_PORT=20091
readonly MAIL_CAPTURE_ADDR="127.0.0.1:${MAIL_CAPTURE_PORT}"
readonly MAIL_CAPTURE_URL="http://${MAIL_CAPTURE_ADDR}"
readonly ACTIVE_KEY_ID=dev-active
readonly PUBLIC_ORIGIN="http://localhost:${CADDY_PORT}"
readonly PUBLIC_RENDER_ORIGIN=http://127.0.0.1:20030

# Native development and tests use separate logical databases.
DEV_DATABASE_URL=${ABOUTME_DEV_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable}
DEV_LOG_LEVEL=${ABOUTME_DEV_LOG_LEVEL:-info}

resolve_repository_path() {
  local raw=$1 label=$2 candidate lexical resolved
  [ -n "$raw" ] || die "$label must not be empty"
  if [[ $raw = /* ]]; then
    candidate=$raw
  else
    candidate=$ROOT/$raw
  fi
  lexical=$(realpath -ms -- "$candidate") || die "$label could not be resolved"
  resolved=$(realpath -m -- "$candidate") || die "$label could not be resolved"
  [ "$resolved" = "$lexical" ] || die "$label must not traverse a symlink"
  case $resolved in
  / | "$ROOT") die "$label must name a directory below the repository root" ;;
  "$ROOT"/*) ;;
  *) die "$label must stay below the repository root" ;;
  esac
  printf '%s' "$resolved"
}

DEV_DIR=$(resolve_repository_path "${ABOUTME_DEV_STATE_DIR:-.dev}" ABOUTME_DEV_STATE_DIR)
readonly DEV_DIR
readonly BIN_DIR=$DEV_DIR/bin
MEDIA_DIR=$(resolve_repository_path "${ABOUTME_DEV_MEDIA_DIR:-.dev/media}" ABOUTME_DEV_MEDIA_DIR)
readonly MEDIA_DIR
readonly CADDYFILE_SRC=$ROOT/deploy/caddy/Caddyfile
readonly CADDYFILE_GEN=$DEV_DIR/Caddyfile
readonly CADDY_HASHES=$DEV_DIR/caddy-hashes
readonly PUBLIC_ROOTS_REGISTRY=$ROOT/packages/publicroots/public-roots.v6.json
readonly PUBLIC_ROOTS_FRAGMENT=$ROOT/deploy/caddy/public-roots.generated.caddy
readonly PUBLIC_ROOTS_MARKER=$'\t# ABOUTME_PUBLIC_ROOTS_GENERATED\n\timport /etc/caddy/generated/public-roots.generated.caddy'
readonly SERVICES=(mail-capture server web caddy)
readonly DB_CONTAINER=aboutme-test-db
readonly SECRETS_DIR=$DEV_DIR/secrets

APP_BUILD_DIGEST=
PUBLIC_RENDERER_BUILD_DIGEST=
PASSWORD_RATE_HMAC_KEY_B64=
AUTH_EMAIL_ACTIVE_KEY_B64=
AUTH_EMAIL_CAPTURE_BEARER_B64=

# --------------------------------------------------------------------------
# output helpers
# --------------------------------------------------------------------------

# --------------------------------------------------------------------------
# process bookkeeping
#
# Every service is started through `setsid --fork`, so its pid is also its
# session and process-group id. That is what makes `down` able to kill a
# whole tree (npm -> nuxt -> forked child) and what lets us tell our own
# process from an unrelated one that happens to have reused the pid.
# --------------------------------------------------------------------------

pidfile() { printf '%s/%s.pid' "$DEV_DIR" "$1"; }
logfile() { printf '%s/%s.log' "$DEV_DIR" "$1"; }

port_of() {
  case $1 in
  mail-capture) printf '%s' "$MAIL_CAPTURE_PORT" ;;
  server) printf '%s' "$SERVER_PORT" ;;
  web) printf '%s' "$WEB_PORT" ;;
  caddy) printf '%s' "$CADDY_PORT" ;;
  *) die "unknown service $1" ;;
  esac
}

read_pid() {
  local f
  f=$(pidfile "$1")
  [ -s "$f" ] || return 1
  local pid
  pid=$(<"$f")
  [[ $pid =~ ^[0-9]+$ ]] || return 1
  printf '%s' "$pid"
}

# is_ours reports whether pid is alive and is still the session leader we
# started, rather than an unrelated process that inherited a recycled pid.
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

# Output is captured before it is matched: under `pipefail`, a `grep -q`
# that exits on its first match can SIGPIPE the producer and turn a
# successful match into a failed pipeline.
port_listening() {
  local listeners
  listeners=$(ss -H -tln "sport = :$1" 2>/dev/null || true)
  [ -n "$listeners" ]
}

db_container_running() {
  local names
  names=$(podman ps --format '{{.Names}}' 2>/dev/null || true)
  grep -qx "$DB_CONTAINER" <<<"$names"
}

# base64url_of encodes a raw 32-byte secret file as unpadded base64url, the
# exact form internal/config decodes. The value is returned, never logged.
base64url_of() {
  base64 -w0 -- "$1" | tr '+/' '-_' | tr -d '='
}

validate_secret_file() {
  local file=$1 mode size canonical
  [ ! -L "$file" ] || die "state secret is a symlink: $file"
  [ -f "$file" ] || die "state secret is not a regular file: $file"
  canonical=$(realpath -m -- "$file" 2>/dev/null) || die "state secret cannot be resolved: $file"
  [ "$canonical" = "$file" ] || die "state secret is noncanonical: $file"
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
  mkdir -p "$SECRETS_DIR"
  chmod 0700 "$SECRETS_DIR"
  for name in password-rate-hmac-key auth-email-active-key auth-email-capture-bearer; do
    file="$SECRETS_DIR/$name"
    if [ ! -e "$file" ] && [ ! -L "$file" ]; then
      umask 077
      head -c 32 /dev/urandom >"$file" || die "could not generate state secret $file"
      chmod 0600 "$file"
    fi
    validate_secret_file "$file"
  done
  PASSWORD_RATE_HMAC_KEY_B64=$(base64url_of "$SECRETS_DIR/password-rate-hmac-key")
  AUTH_EMAIL_ACTIVE_KEY_B64=$(base64url_of "$SECRETS_DIR/auth-email-active-key")
  AUTH_EMAIL_CAPTURE_BEARER_B64=$(base64url_of "$SECRETS_DIR/auth-email-capture-bearer")
}

start_service() {
  local name=$1 workdir=$2
  shift 2
  local pf lf
  pf=$(pidfile "$name")
  lf=$(logfile "$name")
  rm -f "$pf"
  {
    printf '\n===== %s started %s =====\n' "$name" "$(date -Is)"
  } >>"$lf"
  # The child records its own pid before exec, so the pid in the file is
  # the process itself (== its process-group id), not a wrapper's.
  setsid --fork bash -c '
    echo $$ >"$1"
    cd "$2" || exit 127
    shift 2
    exec "$@"
  ' dev-native "$pf" "$workdir" "$@" >>"$lf" 2>&1 </dev/null

  local i
  for i in $(seq 1 50); do
    [ -s "$pf" ] && return 0
    sleep 0.1
  done
  die "$name: no pid file appeared at $pf"
}

stop_service() {
  local name=$1 pid
  if ! pid=$(read_pid "$name"); then
    return 0
  fi
  if ! is_ours "$pid"; then
    warn "$name: pid $pid is not our process any more; removing stale pid file"
    rm -f "$(pidfile "$name")"
    return 0
  fi
  kill -TERM -- "-$pid" 2>/dev/null || true
  local i
  for i in $(seq 1 100); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.1
  done
  if kill -0 "$pid" 2>/dev/null; then
    warn "$name (pid $pid) ignored SIGTERM; sending SIGKILL"
    kill -KILL -- "-$pid" 2>/dev/null || true
    sleep 0.3
  fi
  rm -f "$(pidfile "$name")"
  info "stopped $name (pid $pid)"
}

# wait_http polls url until it answers, or fails with the tail of the
# service's log. A crash during startup is reported, never waited out.
wait_http() {
  local name=$1 url=$2 timeout=$3 pid deadline
  pid=$(service_pid "$name") || die "$name exited before it began serving; see $(logfile "$name")"
  deadline=$((SECONDS + timeout))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if ! is_ours "$pid"; then
      tail -n 30 "$(logfile "$name")" >&2 || true
      die "$name (pid $pid) exited during startup; full log: $(logfile "$name")"
    fi
    if curl -fsS -o /dev/null --max-time 5 "$url" 2>/dev/null; then
      return 0
    fi
    sleep 0.3
  done
  tail -n 30 "$(logfile "$name")" >&2 || true
  die "$name did not answer $url within ${timeout}s; full log: $(logfile "$name")"
}

# wait_capture treats a bound loopback listener as readiness: the capture
# server authenticates every route, so an HTTP probe cannot be used without
# putting the bearer secret on a command line.
wait_capture() {
  local name=$1 port=$2 timeout=$3 pid deadline
  pid=$(service_pid "$name") || die "$name exited before it began serving; see $(logfile "$name")"
  deadline=$((SECONDS + timeout))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if ! is_ours "$pid"; then
      tail -n 30 "$(logfile "$name")" >&2 || true
      die "$name (pid $pid) exited during startup; full log: $(logfile "$name")"
    fi
    if port_listening "$port"; then
      return 0
    fi
    sleep 0.3
  done
  tail -n 30 "$(logfile "$name")" >&2 || true
  die "$name did not start listening on port $port within ${timeout}s; full log: $(logfile "$name")"
}

# --------------------------------------------------------------------------
# Caddy config
#
# The dev route table is deploy/caddy/Caddyfile itself, with only its three
# environment-specific tokens rewritten — the same three the route-table
# test rewrites in apps/server/internal/routetable/route_table_test.go
# (adaptedCaddyfile): the site address and the two compose-service
# upstreams. Every matcher, handle block, and directive is byte-for-byte
# the shipped file. Each token must occur exactly once; a different count
# means the Caddyfile's shape changed and this script refuses to run rather
# than silently serve a route table that no longer resembles the real one.
# --------------------------------------------------------------------------

count_occurrences() {
  local hay=$1 needle=$2 n=0
  while [[ $hay == *"$needle"* ]]; do
    hay=${hay#*"$needle"}
    n=$((n + 1))
  done
  printf '%d' "$n"
}

generate_caddyfile() {
  local content fragment marker_count prefix suffix
  node "$ROOT/scripts/generate-public-roots.mjs" --check >/dev/null ||
    die "public-root generation check failed"
  content=$(<"$CADDYFILE_SRC")
  fragment=$(<"$PUBLIC_ROOTS_FRAGMENT")
  marker_count=$(count_occurrences "$content" "$PUBLIC_ROOTS_MARKER")
  [ "$marker_count" = 1 ] ||
    die "$CADDYFILE_SRC: generated marker count is $marker_count, want exactly 1"
  prefix=${content%%"$PUBLIC_ROOTS_MARKER"*}
  suffix=${content#*"$PUBLIC_ROOTS_MARKER"}
  content=$prefix$fragment$suffix

  # Site address: `bind` restricts the listener to loopback while leaving
  # the site's host matcher empty, so both http://localhost:20080 and
  # http://127.0.0.1:20080 still match. A `127.0.0.1:20080` site address
  # would bind correctly but reject the Host header a browser sends.
  local -a olds=(
    ':80 {'
    'reverse_proxy server:8080 {'
    'reverse_proxy web:3000'
  )
  local -a news=(
    ":${CADDY_PORT} {"$'\n\tbind 127.0.0.1'
    "reverse_proxy 127.0.0.1:${SERVER_PORT} {"
    "reverse_proxy 127.0.0.1:${WEB_PORT}"
  )
  local -a counts=(1 2 2)

  local i old new n prefix suffix
  for i in "${!olds[@]}"; do
    old=${olds[$i]}
    new=${news[$i]}
    n=$(count_occurrences "$content" "$old")
    if [ "$n" != "${counts[$i]}" ]; then
      die "$CADDYFILE_SRC: found $n occurrence(s) of '$old', want exactly ${counts[$i]} — the real Caddyfile's shape changed; update this script's substitution (and route_table_test.go's) to match"
    fi
    content=${content//"$old"/"$new"}
  done

  printf '%s\n' "$content"
}

# --------------------------------------------------------------------------
# commands
# --------------------------------------------------------------------------

require_tools() {
  local t
  for t in podman go npm node caddy curl ss setsid realpath sha256sum awk base64 tr head stat; do
    command -v "$t" >/dev/null 2>&1 || die "$t is not on PATH"
  done
  make -C "$ROOT" --no-print-directory tools-check ARGS=dev
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

ensure_database() {
  info "--- database (${DB_CONTAINER})"
  make -C "$ROOT" test-db-up
}

run_migrations() {
  info "--- migrations (aboutme_dev)"
  (
    cd "$ROOT/apps/server"
    go build -o "$BIN_DIR/migrate" ./cmd/migrate
  )
  (
    cd "$ROOT/apps/server"
    env DATABASE_URL="$DEV_DATABASE_URL" "$BIN_DIR/migrate"
  )
}

seed_dev_account() {
  info "--- seed (dev@aboutme.invalid)"
  (
    cd "$ROOT/apps/server"
    go build -o "$BIN_DIR/dev-seed" ./cmd/dev-seed
  )
  "$BIN_DIR/dev-seed" seed --database-url "$DEV_DATABASE_URL"
  info "dev account: dev@aboutme.invalid / aboutme-dev-password-1"
}

port_blocked_by_stranger() {
  local name=$1 port
  port=$(port_of "$name")
  port_listening "$port" || return 1
  service_pid "$name" >/dev/null 2>&1 && return 1
  return 0
}

start_mail_capture() {
  if service_pid mail-capture >/dev/null 2>&1; then
    info "mail-capture already running (pid $(service_pid mail-capture))"
    return 0
  fi
  ! port_blocked_by_stranger mail-capture || die "port $MAIL_CAPTURE_PORT is already in use by a process this script did not start"
  info "--- mail-capture (:$MAIL_CAPTURE_PORT)"
  (
    cd "$ROOT/apps/server"
    go build -o "$BIN_DIR/mail-capture" ./cmd/mail-capture
  )
  start_service mail-capture "$ROOT/apps/server" \
    "$BIN_DIR/mail-capture" --secret-file "$SECRETS_DIR/auth-email-capture-bearer" --addr "$MAIL_CAPTURE_ADDR"
  wait_capture mail-capture "$MAIL_CAPTURE_PORT" 30
  info "mail-capture ready on $MAIL_CAPTURE_URL"
}

start_server() {
  if service_pid server >/dev/null 2>&1; then
    info "server already running (pid $(service_pid server))"
    return 0
  fi
  ! port_blocked_by_stranger server || die "port $SERVER_PORT is already in use by a process this script did not start"
  info "--- server (:$SERVER_PORT)"
  (
    cd "$ROOT/apps/server"
    go build -o "$BIN_DIR/server" ./cmd/server
  )
  start_service server "$ROOT/apps/server" \
    env \
    PORT="$SERVER_PORT" \
    LISTEN_HOST=127.0.0.1 \
    DATABASE_URL="$DEV_DATABASE_URL" \
    ENV=dev \
    PUBLIC_ORIGIN="$PUBLIC_ORIGIN" \
    PUBLIC_RENDER_ORIGIN="$PUBLIC_RENDER_ORIGIN" \
    APP_BUILD_DIGEST="$APP_BUILD_DIGEST" \
    PUBLIC_RENDERER_BUILD_DIGEST="$PUBLIC_RENDERER_BUILD_DIGEST" \
    TRUSTED_PROXY_CIDRS=127.0.0.1/32 \
    LOG_LEVEL="$DEV_LOG_LEVEL" \
    MEDIA_BACKEND=fs \
    MEDIA_FS_DIR="$MEDIA_DIR" \
    PASSWORD_RATE_HMAC_KEY="$PASSWORD_RATE_HMAC_KEY_B64" \
    AUTH_EMAIL_ACTIVE_KEY_ID="$ACTIVE_KEY_ID" \
    AUTH_EMAIL_ACTIVE_KEY="$AUTH_EMAIL_ACTIVE_KEY_B64" \
    AUTH_EMAIL_MODE=capture \
    AUTH_EMAIL_CAPTURE_URL="$MAIL_CAPTURE_URL" \
    AUTH_EMAIL_CAPTURE_BEARER="$AUTH_EMAIL_CAPTURE_BEARER_B64" \
    "$BIN_DIR/server"
  wait_http server "http://127.0.0.1:$SERVER_PORT/healthz" 30
  info "server ready on http://127.0.0.1:$SERVER_PORT"
}

start_web() {
  if service_pid web >/dev/null 2>&1; then
    info "web already running (pid $(service_pid web))"
    return 0
  fi
  ! port_blocked_by_stranger web || die "port $WEB_PORT is already in use by a process this script did not start"
  [ -d "$ROOT/apps/web/node_modules" ] || die "apps/web/node_modules is missing; run 'npm ci' in apps/web first"
  info "--- web (:$WEB_PORT)"
  # --port/--host on the command line beat nuxt.config.ts's devServer.port,
  # so the Nuxt config stays untouched.
  start_service web "$ROOT/apps/web" \
    npm run dev -- --port "$WEB_PORT" --host 127.0.0.1
  # First request compiles the app, so this is the slow one.
  wait_http web "http://127.0.0.1:$WEB_PORT/" 240
  info "web ready on http://127.0.0.1:$WEB_PORT"
}

start_caddy() {
  local generated
  generated=$(generate_caddyfile)
  if service_pid caddy >/dev/null 2>&1; then
    if [ "$generated" != "$(cat "$CADDYFILE_GEN" 2>/dev/null)" ]; then
      warn "deploy/caddy/Caddyfile changed since caddy started; run 'down' then 'up' to apply it (the admin API is off, so there is no live reload)"
    fi
    info "caddy already running (pid $(service_pid caddy))"
    return 0
  fi
  ! port_blocked_by_stranger caddy || die "port $CADDY_PORT is already in use by a process this script did not start"
  info "--- caddy (:$CADDY_PORT)"
  printf '%s\n' "$generated" >"$CADDYFILE_GEN"
  {
    printf 'registry_source_sha256=%s\n' "$(sha256sum "$PUBLIC_ROOTS_REGISTRY" | awk '{print $1}')"
    printf 'generated_fragment_sha256=%s\n' "$(sha256sum "$PUBLIC_ROOTS_FRAGMENT" | awk '{print $1}')"
    printf 'effective_caddyfile_sha256=%s\n' "$(sha256sum "$CADDYFILE_GEN" | awk '{print $1}')"
  } >"$CADDY_HASHES"
  mkdir -p "$DEV_DIR/caddy-state/config" "$DEV_DIR/caddy-state/data"
  # Keep caddy's instance uuid and autosave out of the developer's real
  # ~/.config/caddy and ~/.local/share/caddy.
  start_service caddy "$ROOT" \
    env \
    XDG_CONFIG_HOME="$DEV_DIR/caddy-state/config" \
    XDG_DATA_HOME="$DEV_DIR/caddy-state/data" \
    caddy run --config "$CADDYFILE_GEN" --adapter caddyfile
  wait_http caddy "http://localhost:$CADDY_PORT/healthz" 30
  info "caddy ready on $PUBLIC_ORIGIN"
}

cmd_up() {
  require_tools
  load_source_digests
  mkdir -p "$DEV_DIR" "$BIN_DIR" "$MEDIA_DIR"
  ensure_database
  run_migrations
  seed_dev_account
  ensure_secrets
  start_mail_capture
  start_server
  start_web
  start_caddy
  info ""
  info "native dev stack is up — open $PUBLIC_ORIGIN"
  cmd_status
}

cmd_seed() {
  require_tools
  mkdir -p "$DEV_DIR" "$BIN_DIR"
  ensure_database
  run_migrations
  seed_dev_account
}

cmd_down() {
  local name
  # Caddy first: stop accepting traffic before the upstreams disappear.
  for name in caddy web server mail-capture; do
    stop_service "$name"
  done
  info "native dev stack is down (the shared $DB_CONTAINER container is left running)"
}

cmd_status() {
  local name pid port state listening failed=0
  printf '%-8s %-8s %-10s %-6s %s\n' SERVICE PID STATE PORT LISTENING
  for name in "${SERVICES[@]}"; do
    port=$(port_of "$name")
    if pid=$(service_pid "$name" 2>/dev/null); then
      state=running
    elif pid=$(read_pid "$name" 2>/dev/null); then
      # A pid file with no live process is a crash, not an absence.
      state=CRASHED
      failed=1
    else
      pid=-
      state=stopped
      failed=1
    fi
    if port_listening "$port"; then listening=yes; else
      listening=no
      [ "$state" = running ] && failed=1
    fi
    printf '%-8s %-8s %-10s %-6s %s\n' "$name" "$pid" "$state" "$port" "$listening"
  done

  local db=stopped db_listening=no
  if db_container_running; then
    db=running
  else
    failed=1
  fi
  if port_listening 20432; then
    db_listening=yes
  else
    failed=1
  fi
  printf '%-8s %-8s %-10s %-6s %s\n' db container "$db" 20432 "$db_listening"

  if [ "$failed" -ne 0 ]; then
    info ""
    info "not everything is up — 'scripts/dev-native.sh logs <service>' shows why; CRASHED means the process died after up started it"
    return 1
  fi
  return 0
}

cmd_logs() {
  local follow=0
  local -a names=()
  local arg
  for arg in "$@"; do
    case $arg in
    -f | --follow) follow=1 ;;
    mail-capture | server | web | caddy) names+=("$arg") ;;
    *) die "logs: unknown argument '$arg' (expected -f, mail-capture, server, web, or caddy)" ;;
    esac
  done
  [ ${#names[@]} -gt 0 ] || names=("${SERVICES[@]}")

  local -a files=()
  local name
  for name in "${names[@]}"; do
    [ -f "$(logfile "$name")" ] || continue
    files+=("$(logfile "$name")")
  done
  [ ${#files[@]} -gt 0 ] || die "no logs yet under $DEV_DIR"

  if [ "$follow" = 1 ]; then
    tail -n 50 -F "${files[@]}"
  else
    tail -n 200 "${files[@]}"
  fi
}

usage() {
  cat <<EOF
usage: scripts/dev-native.sh <up|down|status|logs|seed> [args]

  up      start the native dev stack (idempotent); serves $PUBLIC_ORIGIN
  down    stop exactly the processes up started; never touches the database container
  status  print liveness and ports; exits non-zero if anything is not up
  logs    [-f] [mail-capture|server|web|caddy]  tail per-process logs
  seed    create or refresh the development account and sample resume; idempotent
EOF
}

main() {
  local cmd=${1:-}
  [ $# -gt 0 ] && shift
  case $cmd in
  up) cmd_up "$@" ;;
  down) cmd_down "$@" ;;
  status) cmd_status "$@" ;;
  logs) cmd_logs "$@" ;;
  seed) cmd_seed "$@" ;;
  -h | --help | help | '')
    usage
    [ -n "$cmd" ]
    ;;
  *)
    usage >&2
    die "unknown command '$cmd'"
    ;;
  esac
}

main "$@"
