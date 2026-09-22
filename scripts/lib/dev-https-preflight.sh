#!/usr/bin/env bash
# Preflight checks for the native HTTPS harness: required tools, source
# digests, and conflicting-stack detection before it starts or resumes.
# Sourced by scripts/dev-https.sh; defines functions and constants only.

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
  containers=$(podman ps --format '{{.Names}}|{{index .Labels "com.docker.compose.project"}}|{{index .Labels "com.docker.compose.service"}}' 2>/dev/null) || \
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
  node "$ROOT/scripts/chromium-path.mjs" >/dev/null || die "pinned Chromium is unavailable"
  require_port_absent 20445 "private print port is already in use"
  generate_caddyfile >/dev/null || die "the deployed Caddy route table no longer matches the exact HTTPS substitutions"
  for name in "${SERVICES[@]}"; do
    port=$(port_of "$name")
    require_port_absent "$port" "port $port is already in use by a process this script did not start"
  done
  return 0
}
