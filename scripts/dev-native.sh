#!/usr/bin/env bash
# Native development stack: Go server, Nuxt, and Caddy on one origin, backed by
# the shared Postgres container's aboutme_dev database. See
# docs/runbooks/native-development.md.
#
#   scripts/dev-native.sh up      start everything (idempotent)
#   scripts/dev-native.sh down    stop exactly what up started
#   scripts/dev-native.sh status  liveness + ports; non-zero if anything is down
#   scripts/dev-native.sh logs    per-process logs
#
# Open http://localhost:20080 — that is the only URL a browser should use;
# it is PUBLIC_ORIGIN, so cookie scope, CSRF Origin checks, and OAuth
# redirects match what the code expects.
set -Eeuo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
ROOT=$PWD

# Fixed ports keep PUBLIC_ORIGIN-derived cookies, CSRF, and redirects aligned.
readonly CADDY_PORT=20080
readonly SERVER_PORT=20081
readonly WEB_PORT=20030
readonly PUBLIC_ORIGIN="http://localhost:${CADDY_PORT}"

# Native development and tests use separate logical databases.
DEV_DATABASE_URL=${ABOUTME_DEV_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable}
DEV_LOG_LEVEL=${ABOUTME_DEV_LOG_LEVEL:-info}

readonly DEV_DIR=$ROOT/.dev
readonly BIN_DIR=$DEV_DIR/bin
readonly CADDYFILE_SRC=$ROOT/deploy/caddy/Caddyfile
readonly CADDYFILE_GEN=$DEV_DIR/Caddyfile
readonly SERVICES=(server web caddy)
readonly DB_CONTAINER=aboutme-test-db

# --------------------------------------------------------------------------
# output helpers
# --------------------------------------------------------------------------

info() { printf '%s\n' "$*"; }
warn() { printf 'dev-native: %s\n' "$*" >&2; }
die() {
  printf 'dev-native: %s\n' "$*" >&2
  exit 1
}

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
  local content
  content=$(<"$CADDYFILE_SRC")

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

  local i old new n prefix suffix
  for i in "${!olds[@]}"; do
    old=${olds[$i]}
    new=${news[$i]}
    n=$(count_occurrences "$content" "$old")
    if [ "$n" != 1 ]; then
      die "$CADDYFILE_SRC: found $n occurrence(s) of '$old', want exactly 1 — the real Caddyfile's shape changed; update this script's substitution (and route_table_test.go's) to match"
    fi
    prefix=${content%%"$old"*}
    suffix=${content#*"$old"}
    content=$prefix$new$suffix
  done

  printf '%s\n' "$content"
}

# --------------------------------------------------------------------------
# commands
# --------------------------------------------------------------------------

require_tools() {
  local t
  for t in podman go npm caddy curl ss setsid; do
    command -v "$t" >/dev/null 2>&1 || die "$t is not on PATH"
  done
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
  # cmd/migrate loads the same internal/config.Config as the server, so ENV
  # and PUBLIC_ORIGIN are required even though it only uses DATABASE_URL.
  (
    cd "$ROOT/apps/server"
    env DATABASE_URL="$DEV_DATABASE_URL" ENV=dev PUBLIC_ORIGIN="$PUBLIC_ORIGIN" \
      "$BIN_DIR/migrate"
  )
}

port_blocked_by_stranger() {
  local name=$1 port
  port=$(port_of "$name")
  port_listening "$port" || return 1
  service_pid "$name" >/dev/null 2>&1 && return 1
  return 0
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
    TRUSTED_PROXY_CIDRS=127.0.0.1/32 \
    LOG_LEVEL="$DEV_LOG_LEVEL" \
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
  mkdir -p "$DEV_DIR" "$BIN_DIR"
  ensure_database
  run_migrations
  start_server
  start_web
  start_caddy
  info ""
  info "native dev stack is up — open $PUBLIC_ORIGIN"
  cmd_status
}

cmd_down() {
  local name
  # Caddy first: stop accepting traffic before the upstreams disappear.
  for name in caddy web server; do
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

  local db=stopped
  if db_container_running; then
    db=running
  else
    failed=1
  fi
  printf '%-8s %-8s %-10s %-6s %s\n' db container "$db" 20432 "$([ "$db" = running ] && echo yes || echo no)"

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
    server | web | caddy) names+=("$arg") ;;
    *) die "logs: unknown argument '$arg' (expected -f, server, web, or caddy)" ;;
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
usage: scripts/dev-native.sh <up|down|status|logs> [args]

  up      start the native dev stack (idempotent); serves $PUBLIC_ORIGIN
  down    stop exactly the processes up started; never touches the database container
  status  print liveness and ports; exits non-zero if anything is not up
  logs    [-f] [server|web|caddy]  tail per-process logs
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
