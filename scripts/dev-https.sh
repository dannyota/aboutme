#!/usr/bin/env bash
# Native HTTPS authentication harness on https://localhost:20443.
set -Eeuo pipefail
umask 077

# Resolved in pure Bash (no external dirname) and sourced before the cd
# below, so a missing PATH tool or an unresolved working directory can
# never stop the harness from loading its own libraries and reaching the
# tool check that reports the missing tool by name.
case ${BASH_SOURCE[0]} in
*/*) SCRIPT_DIR=${BASH_SOURCE[0]%/*} ;;
*) SCRIPT_DIR=. ;;
esac
readonly SCRIPT_DIR
# shellcheck source=lib/dev-https-state.sh
source "$SCRIPT_DIR/lib/dev-https-state.sh"
# shellcheck source=lib/dev-https-identity.sh
source "$SCRIPT_DIR/lib/dev-https-identity.sh"
# shellcheck source=lib/dev-https-caddy.sh
source "$SCRIPT_DIR/lib/dev-https-caddy.sh"
# shellcheck source=lib/dev-https-preflight.sh
source "$SCRIPT_DIR/lib/dev-https-preflight.sh"
# shellcheck source=lib/dev-https-lifecycle.sh
source "$SCRIPT_DIR/lib/dev-https-lifecycle.sh"

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
readonly PRINT_LISTEN_ADDR=127.0.0.1:20445
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
readonly PUBLIC_ROOTS_REGISTRY=$ROOT/packages/publicroots/public-roots.v8.json
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
TOTP_ACTIVE_KEY_B64=

info() { printf '%s\n' "$*"; }
warn() { printf 'dev-https: %s\n' "$*" >&2; }
die() {
  printf 'dev-https: %s\n' "$*" >&2
  exit 1
}

trap on_exit EXIT

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
  [ -z "${TOTP_ACTIVE_KEY_B64-}" ] || secret_rules+=(-e "s/${TOTP_ACTIVE_KEY_B64}/[REDACTED]/g")
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
