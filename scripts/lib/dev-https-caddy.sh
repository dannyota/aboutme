#!/usr/bin/env bash
# Port probing, Caddy route generation, and effective-config rendering
# for the native HTTPS harness.
# Sourced by scripts/dev-https.sh; defines functions and constants only.

port_listening() {
  local status
  platform_listener_pids "$1" >/dev/null && return 0
  status=$?
  [ "$status" = 1 ] || warn "listener probe for port $1 failed; ownership is unproved"
  return "$status"
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
  generated=$'\t@uat_google_authorize path /__uat/oauth/google/authorize\n\thandle @uat_google_authorize {\n\t\treverse_proxy 127.0.0.1:'"${MOCK_PORT}"$'\n\t}\n\n\t@uat_linkedin_authorize path /__uat/oauth/linkedin/authorize\n\thandle @uat_linkedin_authorize {\n\t\treverse_proxy 127.0.0.1:'"${MOCK_PORT}"$'\n\t}\n\n'"$fragment"
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
  # The resolver preserves client.example as Host and SNI while dialing the
  # development-only loopback address. No browser visits this metadata host.
  printf '\nhttps://client.example:%s {\n' "$CADDY_PORT"
  cat <<'CIMD'
	bind 127.0.0.1
	tls internal
	@metadata path_regexp cimd ^/oauth/([0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$
	handle @metadata {
		header Content-Type application/json
		header Cache-Control "public, max-age=300"
		respond `{"client_id":"https://client.example{http.request.uri.path}","client_name":"aboutme MCP UAT {http.regexp.cimd.1}","redirect_uris":["http://localhost/callback"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}` 200
	}
	respond 404
}
CIMD
}

render_effective_config() {
  local generated_route name environment environment_mode caddy_bin npm_bin
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
  printf 'linkedin_client_id=%s\n' "$LINKEDIN_CLIENT_ID"
  printf 'linkedin_issuer_url=%s\n' "$LINKEDIN_ISSUER_URL"
  printf 'mcp_enabled=true\noauth_cimd_enabled=true\noauth_cimd_dev_stub_addr=127.0.0.1:%s\n' "$CADDY_PORT"
  printf 'web_port=%s\nserver_port=%s\nmock_port=%s\ncaddy_port=%s\nmail_capture_port=%s\n' \
    "$WEB_PORT" "$SERVER_PORT" "$MOCK_PORT" "$CADDY_PORT" "$MAIL_CAPTURE_PORT"
  printf 'stop_term_attempts=%s\nstop_kill_attempts=%s\n' "$STOP_TERM_ATTEMPTS" "$STOP_KILL_ATTEMPTS"
  # Covers the entry point and every sourced library file, so editing any
  # of them while the harness is running is still detected as source drift.
  printf 'lifecycle_source_sha256=%s\n' \
    "$(sha256sum "$ROOT/scripts/dev-https.sh" "$ROOT"/scripts/lib/dev-https-*.sh "$ROOT/scripts/lib/platform.sh" | sha256sum | awk '{print $1}')"
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
  printf 'browser_supervisor_binary_sha256=%s\n' "$(sha256_file "$BIN_DIR/render-browser-supervisor")"
  printf 'caddy_tool_sha256=%s\n' "$(sha256_file "$caddy_bin")"
  printf 'npm_tool_sha256=%s\n' "$(sha256_file "$npm_bin")"
  validate_session_record || return 1
  printf 'session_checkout=%s\nsession_pid=%s\nsession_pgid=%s\nsession_launch_token=%s\n' "$ROOT" "$SESSION_PID" "$SESSION_PID" "$SESSION_TOKEN"
  for name in "${SERVICES[@]}"; do
    environment=$(service_env_file "$name")
    environment_mode=$(stat -c '%a' "$environment" 2>/dev/null || true)
    [ "$environment_mode" = 600 ] || return 1
    printf '%s_environment_sha256=%s\n' "$name" "$(sha256_file "$environment")"
    printf '%s_pid=%s\n' "$name" "$(read_pid "$name")"
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
