#!/usr/bin/env bash
# Service start/stop supervision for the native HTTPS harness:
# per-service env files, launch, readiness waits, and owned-process
# shutdown.
# Sourced by scripts/dev-https.sh; defines functions and constants only.

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
    printf 'PORT=%q\nLISTEN_HOST=%q\nDATABASE_URL=%q\nENV=%q\nPUBLIC_ORIGIN=%q\nPUBLIC_RENDER_ORIGIN=%q\nAPP_BUILD_DIGEST=%q\nPUBLIC_RENDERER_BUILD_DIGEST=%q\nTRUSTED_PROXY_CIDRS=%q\nLOG_LEVEL=%q\nMEDIA_BACKEND=%q\nMEDIA_FS_DIR=%q\nGOOGLE_CLIENT_ID=%q\nGOOGLE_CLIENT_SECRET=%q\nGOOGLE_OIDC_ISSUER_URL=%q\nPASSWORD_RATE_HMAC_KEY=%q\nAUTH_EMAIL_ACTIVE_KEY_ID=%q\nAUTH_EMAIL_ACTIVE_KEY=%q\nAUTH_EMAIL_MODE=%q\nAUTH_EMAIL_CAPTURE_URL=%q\nAUTH_EMAIL_CAPTURE_BEARER=%q\nMCP_ENABLED=%q\nPROVIDER_LOGIN_ENABLED=%q\n' \
      "$SERVER_PORT" 127.0.0.1 "$DATABASE_URL" dev "$PUBLIC_ORIGIN" "$PUBLIC_RENDER_ORIGIN" "$APP_BUILD_DIGEST" "$PUBLIC_RENDERER_BUILD_DIGEST" 127.0.0.1/32 "$LOG_LEVEL" fs "$MEDIA_DIR" \
      "$GOOGLE_CLIENT_ID" "$GOOGLE_CLIENT_SECRET" "$GOOGLE_ISSUER_URL" \
      "$PASSWORD_RATE_HMAC_KEY_B64" "$ACTIVE_KEY_ID" "$AUTH_EMAIL_ACTIVE_KEY_B64" capture "$MAIL_CAPTURE_URL" "$AUTH_EMAIL_CAPTURE_BEARER_B64" true true >"$file"
    local chromium_path
    chromium_path=$(node "$ROOT/scripts/chromium-path.mjs") || return 1
    printf 'PRINT_LISTEN_ADDR=%q\nCHROMIUM_PATH=%q\n' "$PRINT_LISTEN_ADDR" "$chromium_path" >>"$file"
    ;;
  mail-capture) : >"$file" ;;
  web) printf 'NUXT_PRINT_ORIGIN=%q\n' "http://$PRINT_LISTEN_ADDR" >"$file" ;;
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
    go build -o "$BIN_DIR/render-browser-supervisor" ./cmd/render-browser-supervisor
    chmod 0755 -- "$BIN_DIR/render-browser-supervisor"
    env DATABASE_URL="$DATABASE_URL" MIGRATION_IDENTITY=local-aboutme "$BIN_DIR/migrate" provision
    env DATABASE_URL="$DATABASE_URL" MIGRATION_IDENTITY=local-aboutme "$BIN_DIR/migrate"
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
