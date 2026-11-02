#!/usr/bin/env bash
# Environment and readiness helpers for the native HTTPS harness.
# Sourced by scripts/dev-https.sh; defines functions and constants only.

# passkey_enrollment_flag echoes the closed PASSKEY_ENROLLMENT_ENABLED value
# for the server environment. DEV_HTTPS_PASSKEY_ENROLLMENT selects it and
# accepts only "true" or "false"; it defaults to true so the harness can run
# the enrollment journey without an extra switch.
passkey_enrollment_flag() {
  local raw=${DEV_HTTPS_PASSKEY_ENROLLMENT:-true}
  case $raw in
  true | false) printf '%s' "$raw" ;;
  *) return 1 ;;
  esac
}

# totp_enrollment_flag echoes the closed TOTP_ENROLLMENT_ENABLED value for the
# server environment. DEV_HTTPS_TOTP_ENROLLMENT selects it and accepts only
# "true" or "false"; it defaults to true so the harness can run the TOTP
# enrollment journey without an extra switch
# (docs/design/totp-key-management.md "Key ring").
totp_enrollment_flag() {
  local raw=${DEV_HTTPS_TOTP_ENROLLMENT:-true}
  case $raw in
  true | false) printf '%s' "$raw" ;;
  *) return 1 ;;
  esac
}

write_service_env() {
  local name=$1 file
  file=$(service_env_file "$name")
  umask 077
  case $name in
  mock-oauth)
    printf 'LISTEN_HOST=%q\nPORT=%q\nPUBLIC_ORIGIN=%q\nGOOGLE_CLIENT_ID=%q\nGOOGLE_CLIENT_SECRET=%q\nLINKEDIN_CLIENT_ID=%q\nLINKEDIN_CLIENT_SECRET=%q\n' \
      127.0.0.1 "$MOCK_PORT" "$PUBLIC_ORIGIN" "$GOOGLE_CLIENT_ID" "$GOOGLE_CLIENT_SECRET" "$LINKEDIN_CLIENT_ID" "$LINKEDIN_CLIENT_SECRET" >"$file"
    ;;
  server)
    local passkey_enrollment totp_enrollment dev_https_view_pass_key
    # The throwaway viewer-pass key belongs only to this development harness.
    dev_https_view_pass_key=$(printf '%s' "aboutme-dev-only-view-pass-key32" | base64 -w0 | tr '+/' '-_' | tr -d '=')
    passkey_enrollment=$(passkey_enrollment_flag) || return 1
    totp_enrollment=$(totp_enrollment_flag) || return 1
    # PROVIDER_LOGIN_ENABLED lists google and linkedin only, so the harness's
    # GitHub start route stays 404 (docs/design/linkedin-sign-in.md "Design").
    printf 'PORT=%q\nLISTEN_HOST=%q\nDATABASE_URL=%q\nENV=%q\nPUBLIC_ORIGIN=%q\nPUBLIC_RENDER_ORIGIN=%q\nAPP_BUILD_DIGEST=%q\nPUBLIC_RENDERER_BUILD_DIGEST=%q\nTRUSTED_PROXY_CIDRS=%q\nLOG_LEVEL=%q\nMEDIA_BACKEND=%q\nMEDIA_FS_DIR=%q\nGOOGLE_CLIENT_ID=%q\nGOOGLE_CLIENT_SECRET=%q\nGOOGLE_OIDC_ISSUER_URL=%q\nLINKEDIN_CLIENT_ID=%q\nLINKEDIN_CLIENT_SECRET=%q\nLINKEDIN_OIDC_ISSUER_URL=%q\nPASSWORD_RATE_HMAC_KEY=%q\nAUTH_EMAIL_ACTIVE_KEY_ID=%q\nAUTH_EMAIL_ACTIVE_KEY=%q\nAUTH_EMAIL_MODE=%q\nAUTH_EMAIL_CAPTURE_URL=%q\nAUTH_EMAIL_CAPTURE_BEARER=%q\nMCP_ENABLED=%q\nPROVIDER_LOGIN_ENABLED=%q\nPASSKEY_ENROLLMENT_ENABLED=%q\nTOTP_ACTIVE_KEY=%q\nTOTP_ENROLLMENT_ENABLED=%q\nVIEW_PASS_KEY=%q\nSIGN_IN_TO_VIEW_ENABLED=%q\nPREVIEW_CARD_ENABLED=%q\n' \
      "$SERVER_PORT" 127.0.0.1 "$DATABASE_URL" dev "$PUBLIC_ORIGIN" "$PUBLIC_RENDER_ORIGIN" "$APP_BUILD_DIGEST" "$PUBLIC_RENDERER_BUILD_DIGEST" 127.0.0.1/32 "$LOG_LEVEL" fs "$MEDIA_DIR" \
      "$GOOGLE_CLIENT_ID" "$GOOGLE_CLIENT_SECRET" "$GOOGLE_ISSUER_URL" \
      "$LINKEDIN_CLIENT_ID" "$LINKEDIN_CLIENT_SECRET" "$LINKEDIN_ISSUER_URL" \
      "$PASSWORD_RATE_HMAC_KEY_B64" "$ACTIVE_KEY_ID" "$AUTH_EMAIL_ACTIVE_KEY_B64" capture "$MAIL_CAPTURE_URL" "$AUTH_EMAIL_CAPTURE_BEARER_B64" true "google,linkedin" "$passkey_enrollment" \
      "$TOTP_ACTIVE_KEY_B64" "$totp_enrollment" \
      "$dev_https_view_pass_key" true true >"$file"
    # Go reads this root on its first TLS request, after Caddy starts.
    printf 'OAUTH_CIMD_ENABLED=%q\nOAUTH_CIMD_DEV_STUB_ADDR=%q\nSSL_CERT_FILE=%q\n' \
      true "127.0.0.1:$CADDY_PORT" "$CADDY_ROOT" >>"$file"
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

wait_http() {
  local name=$1 url=$2 timeout=$3 pid deadline
  pid=$(service_pid "$name") || return 1
  deadline=$((SECONDS + timeout))
  while [ "$SECONDS" -lt "$deadline" ]; do
    session_startup_health || return 1
    if validate_service_listeners "$name" 1; then
      local probe_status=0
      curl -fsS -o /dev/null --max-time 5 "$url" 2>/dev/null || probe_status=$?
      session_startup_health && validate_service_listeners "$name" || { latch_session_failure "$SESSION_TOKEN"; return 1; }
      [ "$probe_status" -ne 0 ] || return 0
    else
      local status=$?
      [ "$status" = 1 ] || { latch_session_failure "$SESSION_TOKEN"; return 1; }
    fi
    sleep 0.3
  done
  return 1
}

wait_https() {
  local url=$1 timeout=$2 pid deadline
  pid=$(service_pid caddy) || return 1
  deadline=$((SECONDS + timeout))
  while [ "$SECONDS" -lt "$deadline" ]; do
    session_startup_health || return 1
    if validate_service_listeners caddy 1; then
      local probe_status=0
      curl -fsS -o /dev/null --max-time 5 --cacert "$EXPORTED_ROOT" "$url" 2>/dev/null || probe_status=$?
      session_health || return 1
      [ "$probe_status" -ne 0 ] || return 0
    else
      local status=$?
      [ "$status" = 1 ] || { latch_session_failure "$SESSION_TOKEN"; return 1; }
    fi
    sleep 0.3
  done
  return 1
}

wait_for_caddy_root() {
  local pid deadline
  pid=$(service_pid caddy) || return 1
  deadline=$((SECONDS + 30))
  while [ "$SECONDS" -lt "$deadline" ]; do
    session_startup_health || return 1
    [ -s "$CADDY_ROOT" ] && return 0
    sleep 0.2
  done
  return 1
}

report_startup_failure() {
  local log line failed_service
  if [ -f "$RUN_DIR/session.failed-service" ]; then
    failed_service=$(<"$RUN_DIR/session.failed-service")
    case $failed_service in mock-oauth | mail-capture | server | web | caddy) CURRENT_SERVICE=$failed_service ;; esac
  fi
  if [ -z "$CURRENT_SERVICE" ]; then
    warn "startup failed before any service was launched; see the build and migration output above"
    return 0
  fi
  warn "$CURRENT_SERVICE did not finish starting"
  log=$(logfile "$CURRENT_SERVICE")
  if [ -s "$log" ]; then
    warn "$CURRENT_SERVICE: last log lines follow"
    while IFS= read -r line; do
      warn "$CURRENT_SERVICE: $line"
    done < <(tail -n 40 -- "$log" | redact_logs)
  else
    warn "$CURRENT_SERVICE produced no log output before failing"
  fi
}

rollback_startup() {
  warn "startup failed; rolling back this invocation's session"
  report_startup_failure
  if [ -n "${STARTED_SESSION_TOKEN:-}" ] && session_matches "$STARTED_SESSION_TOKEN"; then
    stop_session || warn 'rollback could not prove a drained group; manual recovery required; state preserved'
  fi
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
    env DATABASE_URL="$DATABASE_URL" "$BIN_DIR/migrate"
  )
}

start_mock() {
  CURRENT_SERVICE=mock-oauth
  wait_http mock-oauth "$GOOGLE_ISSUER_URL/.well-known/openid-configuration" 30
  wait_http mock-oauth "$LINKEDIN_ISSUER_URL/.well-known/openid-configuration" 30
  CURRENT_SERVICE=
}

# wait_mail_capture treats a bound loopback listener as readiness: the capture
# server authenticates every route, so an HTTP probe cannot be used without
# putting the bearer secret on a command line.
wait_mail_capture() {
  local name=$1 timeout=$3 pid deadline status
  pid=$(service_pid "$name") || return 1
  deadline=$((SECONDS + timeout))
  while [ "$SECONDS" -lt "$deadline" ]; do
    session_startup_health || return 1
    if validate_service_listeners "$name" 1; then
      validate_service_listeners "$name" || return 1
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
  CURRENT_SERVICE=mail-capture
  wait_mail_capture mail-capture "$MAIL_CAPTURE_PORT" 30
  CURRENT_SERVICE=
}

start_server() {
  CURRENT_SERVICE=server
  wait_http server "http://127.0.0.1:${SERVER_PORT}/healthz" 30
  CURRENT_SERVICE=
}

start_web() {
  CURRENT_SERVICE=web
  [ -d "$ROOT/apps/web/node_modules" ] || return 1
  wait_http web "http://127.0.0.1:${WEB_PORT}/" 240
  CURRENT_SERVICE=
}

start_caddy() {
  CURRENT_SERVICE=caddy
  wait_for_caddy_root
  install -m 0600 "$CADDY_ROOT" "$EXPORTED_ROOT"
  wait_https "$PUBLIC_ORIGIN/healthz" 30
  CURRENT_SERVICE=
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
