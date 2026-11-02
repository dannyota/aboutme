#!/usr/bin/env bash
# Session ownership and live service/listener checks for the HTTPS harness.
port_of() {
  case $1 in
  mock-oauth) printf '%s' "$MOCK_PORT" ;;
  mail-capture) printf '%s' "$MAIL_CAPTURE_PORT" ;;
  server) printf '%s' "$SERVER_PORT" ;;
  web) printf '%s' "$WEB_PORT" ;;
  caddy) printf '%s' "$CADDY_PORT" ;;
  *) return 1 ;;
  esac
}

sha256_file() { sha256sum "$1" | awk '{print $1}'; }
identity_field() {
  awk -F= -v key="$2" '$1 == key { sub(/^[^=]*=/, ""); value=$0; count++ }
    END { if (count != 1) exit 1; print value }' "$1"
}

read_pid() {
  local file pid
  file=$(pidfile "$1")
  [ -f "$file" ] && [ ! -L "$file" ] || return 1
  pid=$(<"$file")
  [[ $pid =~ ^[1-9][0-9]*$ ]] && [ "$pid" -gt 1 ] || return 1
  printf '%s' "$pid"
}

# HTTP stack exclusion needs no signal authority.
is_ours() {
  local row pid ppid pgid uid sid rest
  row=$(platform_process_snapshot "$1") || return 1
  read -r pid ppid pgid uid sid rest <<<"$row"
  [ "$sid" = "$pid" ]
}

session_record() { printf '%s/session' "$RUN_DIR"; }
session_command() { printf '%s %s/scripts/lib/dev-https-session.sh %s %s' "$BASH_BIN" "$ROOT" "$ROOT" "$SESSION_TOKEN"; }

validate_session_record() {
  local values
  values=$(python3 -I - "$(session_record)" "$ROOT" <<'PYTHON'
import os, re, stat, sys
try:
    fd = os.open(sys.argv[1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd) as record:
        node = os.fstat(record.fileno())
        if not stat.S_ISREG(node.st_mode) or stat.S_IMODE(node.st_mode) != 0o600 or node.st_uid != os.geteuid() or node.st_nlink != 1:
            sys.exit(1)
        text = record.read(4097)
    lines = text.splitlines()
    if len(text) > 4096 or len(lines) != 5 or any("=" not in line for line in lines):
        sys.exit(1)
    values = dict(line.split("=", 1) for line in lines)
    if set(values) != {"version", "checkout", "pid", "pgid", "launch_token"}:
        sys.exit(1)
    if values["version"] != "2" or values["checkout"] != sys.argv[2] or values["pgid"] != values["pid"]:
        sys.exit(1)
    if not re.fullmatch(r"[1-9][0-9]*", values["pid"]) or int(values["pid"]) <= 1 or not re.fullmatch(r"[0-9a-f]{32}", values["launch_token"]):
        sys.exit(1)
    print(values["pid"], values["launch_token"])
except (OSError, UnicodeError):
    sys.exit(1)
PYTHON
) || return 1
  read -r SESSION_PID SESSION_TOKEN <<<"$values"
}

validate_session_anchor() {
  validate_session_record && platform_validate_anchor "$SESSION_PID" "$(session_command)"
}

session_matches() {
  local token=$1
  validate_session_record && [ "$SESSION_TOKEN" = "$token" ]
}

latch_session_failure() {
  local token=$1 name=${2:-inspection}
  session_matches "$token" || return 1
  case $name in mock-oauth | mail-capture | server | web | caddy | inspection) ;; *) return 1 ;; esac
  # Persist the latch before optional diagnostics or proof notifications.
  (set -o noclobber; printf '%s\n' "$token" >"$RUN_DIR/session.failed") 2>/dev/null ||
    [ "$(cat "$RUN_DIR/session.failed" 2>/dev/null)" = "$token" ] || return 1
  (set -o noclobber; printf '%s\n' "$name" >"$RUN_DIR/session.failed-service") 2>/dev/null || true
  notify_proofs failed
}

service_pid() {
  local name=$1 pid recorded row actual ppid pgid uid sid state command
  validate_session_anchor || return 1
  [ ! -e "$RUN_DIR/session.failed" ] && [ ! -e "$RUN_DIR/session.stopping" ] || return 1
  [ "$(identity_field "$RUN_DIR/session.children" launch_token)" = "$SESSION_TOKEN" ] || return 1
  recorded=$(identity_field "$RUN_DIR/session.children" "$name") || return 1
  pid=$(read_pid "$name") || return 1
  [ "$pid" = "$recorded" ] || return 1
  row=$(platform_process_snapshot "$pid") || return 1
  read -r actual ppid pgid uid sid state command <<<"$row"
  [ "$actual" = "$pid" ] && [ "$ppid" = "$SESSION_PID" ] && [ "$pgid" = "$SESSION_PID" ] &&
    [ "$sid" = "$SESSION_PID" ] && [ "$uid" = "$EUID" ] && [[ $state != [ZX]* ]] || return 1
  printf '%s' "$pid"
}

validate_service_identity() { service_pid "$1" >/dev/null; }

# Require every socket owner to descend from this service's live direct child.
validate_service_listeners() {
  local name=$1 allow_absent=${2:-0} child owners status owner row pid ppid pgid uid sid state command seen port absent=0
  local -a ports=("$(port_of "$1")")
  [ "$name" != server ] || ports+=(20445)
  child=$(service_pid "$name") || return 2
  for port in "${ports[@]}"; do
    if owners=$(platform_listener_pids "$port"); then
      SESSION_LISTENERS_SEEN[$port]=1
    else
      status=$?
      if [ "$status" -eq 1 ] && [ "$allow_absent" = 1 ] &&
        [ -z "${SESSION_LISTENERS_SEEN[$port]+set}" ] && [ ! -e "$RUN_DIR/session.ready" ]; then
        absent=1
        continue
      fi
      return 2
    fi
    while IFS= read -r owner; do
      seen=' '
      while :; do
        [[ $owner =~ ^[1-9][0-9]*$ && $seen != *" $owner "* ]] || return 2
        seen+="$owner "
        row=$(platform_process_snapshot "$owner") || return 2
        read -r pid ppid pgid uid sid state command <<<"$row"
        [ "$pgid" = "$SESSION_PID" ] && [ "$sid" = "$SESSION_PID" ] &&
          [ "$uid" = "$EUID" ] && [[ $state != [ZX]* ]] || return 2
        [ "$owner" != "$child" ] || break
        owner=$ppid
      done
    done <<<"$owners"
  done
  service_pid "$name" >/dev/null || return 2
  [ "$absent" = 0 ] || return 1
}

session_health() {
  local name token proof=${1:-} stopped=${2:-}
  validate_session_anchor || return 1
  token=$SESSION_TOKEN
  [ ! -e "$RUN_DIR/session.failed" ] && [ ! -e "$RUN_DIR/session.stopping" ] || return 1
  for name in "${SERVICES[@]}"; do
    if ! validate_service_listeners "$name"; then
      # An allowed proof can inspect a child while a validated stop drains it.
      # A prior session's stop marker cannot excuse an unexpected service loss.
      if [ -z "$proof" ] || ! proof_stop_observed "$proof" "$stopped" "$token"; then
        latch_session_failure "$token" "$name" || true
      fi
      return 1
    fi
  done
  validate_session_anchor && [ "$SESSION_TOKEN" = "$token" ] && [ ! -e "$RUN_DIR/session.failed" ]
}

# Startup permits unbound ports, but never a foreign or unreadable owner.
session_startup_health() {
  local name status token
  validate_session_anchor || return 1
  token=$SESSION_TOKEN
  for name in "${SERVICES[@]}"; do
    status=0
    validate_service_listeners "$name" 1 || status=$?
    if [ "$status" -gt 1 ]; then
      latch_session_failure "$token" "$name" || true
      return 1
    fi
  done
  [ ! -e "$RUN_DIR/session.failed" ]
}

session_ready() {
  [ -f "$RUN_DIR/session.ready" ] && [ "$(cat "$RUN_DIR/session.ready")" = "$SESSION_TOKEN" ]
}
