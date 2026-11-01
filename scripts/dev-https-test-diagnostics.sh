#!/usr/bin/env bash
# BASH_ENV instrumentation for hermetic startup fixtures. Record locations,
# never commands or environments, because startup commands handle secrets.

if [[ ${0##*/} != dev-https.sh || ${1-} != up || ! -d ${DEV_HTTPS_DIAGNOSTICS_DIR:-} ]]; then
  return 0
fi

# shellcheck source=lib/platform.sh
source "${BASH_SOURCE[0]%/*}/lib/platform.sh"

# Conditional groups keep handler failures from triggering the caller's errexit.
dev_https_test_trace() {
  {
    local function=$1 source=${2##*/} line=$3 key step=''
    case $source in
    dev-https.sh | dev-https-identity.sh | dev-https-lifecycle.sh | dev-https-session.sh) ;;
    *) return 0 ;;
    esac
    if [ "$function" = start_stack ]; then
      case ${4:-} in
      *' tools-check ARGS=dev') step=tools-check ;;
      *' test-db-up') step=test-db-up ;;
      build_and_migrate | ensure_secrets | start_session | start_mock | start_mail_capture | start_server | start_web | start_caddy | write_effective_config) step=$4 ;;
      esac
      if [ -n "$step" ] && [ "$step" != "${DEV_HTTPS_DIAGNOSTIC_STEP:-}" ]; then
        if [ -n "${DEV_HTTPS_DIAGNOSTIC_STEP:-}" ]; then
          printf 'step=%s duration=%ss\n' "$DEV_HTTPS_DIAGNOSTIC_STEP" "$((SECONDS - DEV_HTTPS_DIAGNOSTIC_STEP_START))" >>"$DEV_HTTPS_DIAGNOSTICS_DIR/timeline.$$.log" || true
        fi
        DEV_HTTPS_DIAGNOSTIC_STEP=$step
        DEV_HTTPS_DIAGNOSTIC_STEP_START=$SECONDS
      fi
    fi
    if [ "${4:-}" = 'return 1' ]; then
      case $function in
      validate_* | service_pid | start_session | session_health | session_startup_health)
        key=${CURRENT_SERVICE:-none}:$function:$line
        if [ -z "${DEV_HTTPS_DIAGNOSTIC_REJECTIONS[$key]+set}" ]; then
          DEV_HTTPS_DIAGNOSTIC_REJECTIONS[$key]=1
          printf 'elapsed=%ss service=%s function=%s source=%s:%s rejected\n' \
            "$SECONDS" "${CURRENT_SERVICE:-none}" "$function" "$source" "$line" >>"$DEV_HTTPS_DIAGNOSTICS_DIR/rejections.$$.log" || true
        fi
        ;;
      esac
    fi
    case $function in
    start_stack) key=$function:$line ;;
    build_and_migrate | start_mock | start_mail_capture | start_server | start_web | start_caddy | start_session | wait_http | wait_mail_capture | wait_https | validate_session_anchor)
      key=${CURRENT_SERVICE:-none}:$function ;;
    *) return 0 ;;
    esac
    if [ -z "${DEV_HTTPS_DIAGNOSTIC_STEPS[$key]+set}" ]; then
      DEV_HTTPS_DIAGNOSTIC_STEPS[$key]=1
      printf 'elapsed=%ss service=%s function=%s source=%s:%s\n' \
        "$SECONDS" "${CURRENT_SERVICE:-none}" "$function" "$source" "$line" >>"$DEV_HTTPS_DIAGNOSTICS_DIR/timeline.$$.log" || true
    fi
  } || true
}

dev_https_test_failure_report() {
  {
    local status=$1 function=$2 source=${3##*/} line=$4 name file pid pids='' port
    local -A seen_pids=()
    printf 'dev-https-test: startup failure status=%s function=%s source=%s:%s time=%(%FT%T%z)T elapsed=%ss\n' \
      "$status" "$function" "$source" "$line" -1 "$SECONDS" >&2 || true
    printf 'dev-https-test: startup timeline (elapsed seconds before each step)\n' >&2 || true
    cat -- "$DEV_HTTPS_DIAGNOSTICS_DIR/timeline.$$.log" >&2 || true
    if [ -n "${DEV_HTTPS_DIAGNOSTIC_STEP:-}" ]; then
      printf 'step=%s duration=%ss (failed)\n' "$DEV_HTTPS_DIAGNOSTIC_STEP" "$((SECONDS - DEV_HTTPS_DIAGNOSTIC_STEP_START))" >&2 || true
    fi
    printf 'dev-https-test: first session rejections by location (expected refusals may appear)\n' >&2 || true
    if [ -f "$DEV_HTTPS_DIAGNOSTICS_DIR/rejections.$$.log" ]; then
      cat -- "$DEV_HTTPS_DIAGNOSTICS_DIR/rejections.$$.log" >&2 || true
    fi
    name=${CURRENT_SERVICE:-server}
    if [ -f "$RUN_DIR/session.failed-service" ]; then
      file=$(<"$RUN_DIR/session.failed-service")
      case $file in mock-oauth | mail-capture | server | web | caddy) name=$file ;; esac
    fi
    file=$(logfile "$name") || true
    printf 'dev-https-test: %s combined stdout and stderr (last 200 lines)\n' "$name" >&2 || true
    if [ -f "$file" ]; then
      tail -n 200 -- "$file" | redact_logs >&2 || true
    else
      printf 'dev-https-test: service log absent\n' >&2 || true
    fi
    printf 'dev-https-test: last readiness response (fixture curl, no network request)\n' >&2 || true
    if [ -f "$DEV_HTTPS_DIAGNOSTICS_DIR/probe-$name" ]; then
      cat -- "$DEV_HTTPS_DIAGNOSTICS_DIR/probe-$name" >&2 || true
    else
      printf 'dev-https-test: %s readiness: not reached\n' "$name" >&2 || true
    fi
    cat -- "$DEV_HTTPS_DIAGNOSTICS_DIR/database" >&2 || true
    printf 'dev-https-test: listener state (fixture owners, no real sockets)\n' >&2 || true
    for port in 20440 20441 20442 20443 20444 20445; do
      printf 'dev-https-test: port=%s\n' "$port" >&2 || true
      platform_listener_pids "$port" >&2 || true
    done
    printf 'dev-https-test: process state (PID PPID PGID UID COMMAND; no arguments)\n' >&2 || true
    for file in "${DEV_HTTPS_PID_AUDIT_FILE:-}" "$RUN_DIR/"*.pid; do
      [ -f "$file" ] || continue
      while IFS= read -r pid || [ -n "$pid" ]; do
        [[ $pid =~ ^[0-9]+$ ]] || continue
        [ -z "${seen_pids[$pid]+set}" ] || continue
        seen_pids[$pid]=1
        pids+=${pids:+,}$pid
      done <"$file" || true
    done
    [ -z "$pids" ] || ps -ww -o pid=,ppid=,pgid=,uid=,comm= -p "$pids" >&2 || true
    for name in session session.children; do
      file=$RUN_DIR/$name
      [ -f "$file" ] || continue
      printf 'dev-https-test: %s record (non-secret fields only)\n' "$name" >&2 || true
      awk -F= '$1 ~ /^(version|pid|pgid|mock-oauth|mail-capture|server|web|caddy)$/ && $2 ~ /^[0-9]+$/ { print }' "$file" >&2 || true
    done
  } || true
}

dev_https_test_failure() {
  {
    local report
    [ "${STARTUP_ARMED:-0}" = 1 ] || return 0
    # ERR propagates through function callers. Snapshot once, before EXIT
    # rolls back processes and deletes their ownership records.
    [ "${DEV_HTTPS_DIAGNOSTIC_REPORTED:-0}" = 0 ] || return 0
    mkdir "$DEV_HTTPS_DIAGNOSTICS_DIR/reported.$$" 2>/dev/null || return 0
    DEV_HTTPS_DIAGNOSTIC_REPORTED=1
    trap - DEBUG ERR
    report=$DEV_HTTPS_DIAGNOSTICS_DIR/report.$$.txt
    dev_https_test_failure_report "$@" >"$report" 2>&1 || true
    cat -- "$report" >&2 || true
    if [ -n "${DEV_HTTPS_DIAGNOSTICS_ARTIFACT_DIR:-}" ]; then
      mkdir -p -- "$DEV_HTTPS_DIAGNOSTICS_ARTIFACT_DIR" || return 0
      cp -- "$report" "$DEV_HTTPS_DIAGNOSTICS_ARTIFACT_DIR/startup.$$.txt" || true
    fi
  } || true
}

declare -A DEV_HTTPS_DIAGNOSTIC_STEPS=()
declare -A DEV_HTTPS_DIAGNOSTIC_REJECTIONS=()
DEV_HTTPS_DIAGNOSTIC_REPORTED=0
set -ET
trap 'dev_https_test_trace "${FUNCNAME[0]:-main}" "${BASH_SOURCE[0]}" "$LINENO" "$BASH_COMMAND"' DEBUG
trap 'dev_https_test_failure "$?" "${FUNCNAME[0]:-main}" "${BASH_SOURCE[0]}" "$LINENO"' ERR
