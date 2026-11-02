#!/usr/bin/env bash
# One anchor owns the service group until an explicit stop drains it.
start_session() {
  local name generated attempt token
  for name in "${SERVICES[@]}"; do write_service_env "$name" || return 1; done
  generated=$(generate_caddyfile) || return 1
  printf '%s' "$generated" >"$CADDYFILE_GEN" || return 1
  token=$(python3 -I -c 'import secrets; print(secrets.token_hex(16))') || return 1
  STARTED_SESSION_TOKEN=$token
  detach "$BASH_BIN" "$ROOT/scripts/lib/dev-https-session.sh" "$ROOT" "$token" \
    >>"$LOG_DIR/session.log" 2>&1 </dev/null || return 1
  for attempt in $(seq 1 100); do
    if validate_session_anchor && [ "$SESSION_TOKEN" = "$token" ] &&
      [ -f "$RUN_DIR/session.children" ] && [ "$(wc -l <"$RUN_DIR/session.children")" -eq 6 ]; then
      return 0
    fi
    sleep 0.1
  done
  return 1
}

stop_session() {
  local token pid members attempt file deadline
  validate_session_record || return 1
  token=$SESSION_TOKEN pid=$SESSION_PID
  members=$(platform_group_members "$pid") || return 1
  if [ -n "$members" ]; then
    validate_session_anchor || { warn 'missing or mismatched anchor; manual recovery required; state preserved'; return 1; }
    [ "$SESSION_TOKEN" = "$token" ] || return 1
    if session_health && (load_source_digests && validate_effective_config) >/dev/null 2>&1; then
      notify_proofs stopped || return 1
    else
      notify_proofs failed || return 1
    fi
    session_matches "$token" && [ "$SESSION_PID" = "$pid" ] || return 1
    # Remove readiness before an intentional drain, after recording its health.
    rm -f -- "$RUN_DIR/session.ready"
    platform_signal_group TERM "$pid" "$(session_command)" || return 1
    # The supervisor stays alive during this interval, including when TERM
    # reaches it before its children. Its anchor permits the final group KILL.
    deadline=$((SECONDS + 10))
    for attempt in $(seq 1 "$STOP_TERM_ATTEMPTS"); do
      [ "$SECONDS" -lt "$deadline" ] || break
      members=$(platform_group_members "$pid") || return 1
      [ -n "$members" ] || break
      [ "$members" != "$pid" ] || break
      sleep 0.1
    done
    members=$(platform_group_members "$pid") || return 1
    if [ -n "$members" ]; then
      session_matches "$token" && [ "$SESSION_PID" = "$pid" ] || return 1
      platform_signal_group KILL "$pid" "$(session_command)" || return 1
    fi
    deadline=$((SECONDS + 5))
    for attempt in $(seq 1 "$STOP_KILL_ATTEMPTS"); do
      [ "$SECONDS" -lt "$deadline" ] || break
      members=$(platform_group_members "$pid") || return 1
      [ -n "$members" ] || break
      sleep 0.1
    done
  fi
  members=$(platform_group_members "$pid") || return 1
  [ -z "$members" ] && session_matches "$token" || return 1
  # Remove only this validated launch's records. Preserve keys, logs and data.
  for file in "${SERVICES[@]}"; do rm -f -- "$(pidfile "$file")" "$(service_env_file "$file")"; done
  rm -f -- "$RUN_DIR/session.children" "$RUN_DIR/session.ready" "$RUN_DIR/session.failed" "$RUN_DIR/session.failed-service" "$RUN_DIR/session.stopping"
  session_matches "$token" || return 1
  rm -- "$(session_record)"
}

supervise_session() {
  local root=$1 token=$2 name pid status tick failed_service=
  set +m
  cd "$root" || exit 1
  ROOT=$(pwd -P)
  [ "$ROOT" = "$root" ] && [[ $token =~ ^[0-9a-f]{32}$ ]] || exit 1
  RUN_DIR=$ROOT/.dev/native-https/run
  LOG_DIR=$ROOT/.dev/native-https/log
  BIN_DIR=$ROOT/.dev/native-https/bin
  SECRETS_DIR=$ROOT/.dev/native-https/secrets
  CADDYFILE_GEN=$ROOT/.dev/native-https/Caddyfile
  BASH_BIN=$(platform_realpath "$(command -v bash)")
  SERVICES=(mock-oauth mail-capture server web caddy)
  MOCK_PORT=20442 MAIL_CAPTURE_PORT=20444 SERVER_PORT=20441 WEB_PORT=20440 CADDY_PORT=20443
  SESSION_STOPPING=0
  trap 'SESSION_STOPPING=1; if session_matches "$token"; then
    if [ -e "$RUN_DIR/session.stopping" ]; then
      [ -f "$RUN_DIR/session.stopping" ] && [ ! -L "$RUN_DIR/session.stopping" ] || exit 1
    else
      (set -o noclobber; printf "%s\n" "$token" >"$RUN_DIR/session.stopping") || exit 1
    fi
  fi' TERM INT
  # Publish exclusively before the first service. An existing record is never
  # replaced, and a failed publication starts no children.
  python3 -I - "$RUN_DIR/session" "$ROOT" "$$" "$token" <<'PY' || exit 1
import os, sys
path, root, pid, token = sys.argv[1:]
if os.getsid(int(pid)) != int(pid) or os.getpgid(int(pid)) != int(pid):
    sys.exit(1)
fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
with os.fdopen(fd, "w") as record:
    record.write(f"version=2\ncheckout={root}\npid={pid}\npgid={pid}\nlaunch_token={token}\n")
    record.flush()
    os.fsync(record.fileno())
PY
  SESSION_PID=$$ SESSION_TOKEN=$token
  declare -a children=()
  declare -A SESSION_LISTENERS_SEEN=() reaped=() child_names=()
  printf 'launch_token=%s\n' "$token" >"$RUN_DIR/session.children" || exit 1
  for name in "${SERVICES[@]}"; do
    if ! session_matches "$token"; then failed_service=inspection; break; fi
    [ "$SESSION_STOPPING" = 0 ] || break
    case $name in
    mock-oauth) launch_service "$name" "$ROOT/apps/server" "$BIN_DIR/mock-oauth" ;;
    mail-capture) launch_service "$name" "$ROOT/apps/server" "$BIN_DIR/mail-capture" --secret-file "$SECRETS_DIR/auth-email-capture-bearer" --addr 127.0.0.1:20444 ;;
    server) launch_service "$name" "$ROOT/apps/server" "$BIN_DIR/server" ;;
    web) launch_service "$name" "$ROOT/apps/web" npm run dev -- --port 20440 --host 127.0.0.1 ;;
    caddy) launch_service "$name" "$ROOT" caddy run --config "$CADDYFILE_GEN" --adapter caddyfile ;;
    esac
    pid=$LAUNCHED_PID
    children+=("$pid")
    child_names[$pid]=$name
    printf '%s\n' "$pid" >"$(pidfile "$name")" || { failed_service=$name; break; }
    printf '%s=%s\n' "$name" "$pid" >>"$RUN_DIR/session.children" || { failed_service=$name; break; }
  done
  while :; do
    tick=$SECONDS
    for pid in "${children[@]}"; do
      if [ -z "${reaped[$pid]+set}" ] && ! kill -0 "$pid" 2>/dev/null; then
        wait "$pid" || true
        reaped[$pid]=1
        [ "$SESSION_STOPPING" = 1 ] || failed_service=${failed_service:-${child_names[$pid]}}
      fi
    done
    if [ "$SESSION_STOPPING" = 0 ] && [ -z "$failed_service" ] && [ ! -e "$RUN_DIR/session.failed" ]; then
      for name in "${SERVICES[@]}"; do
        status=0
        validate_service_listeners "$name" 1 || status=$?
        [ "$SESSION_STOPPING" = 0 ] || break
        case $status in
        0 | 1) ;;
        *) failed_service=$name; break ;;
        esac
      done
    fi
    # A changed record cannot accept our latch. Retain the failure and live
    # anchor, and persist it only when this launch's record validates again.
    if [ -n "$failed_service" ] && latch_session_failure "$token" "$failed_service"; then
      failed_service=
    fi
    # A group TERM interrupts sleep. Keep the anchor until the operator KILLs
    # the group, even when every service has already exited.
    if [ "$SECONDS" -le "$tick" ]; then
      sleep 1 &
      wait "$!" || true
    fi
  done
}

launch_service() {
  local name=$1 workdir=$2
  shift 2
  "$BASH_BIN" -c 'cd "$1" || exit 127; set -a; source "$2" || exit 127; set +a; shift 2; exec "$@"' \
    "dev-https-$name" "$workdir" "$(service_env_file "$name")" "$@" >>"$(logfile "$name")" 2>&1 </dev/null &
  LAUNCHED_PID=$!
}

# Proof markers survive intentional down/up transitions within one journey.
notify_proofs() {
  local outcome=$1 file temporary
  case $outcome in stopped | failed) ;; *) return 1 ;; esac
  for file in "$RUN_DIR"/proof.*; do
    [[ ${file##*/} =~ ^proof\.[A-Za-z0-9]{10}$ ]] || continue
    [ -f "$file" ] && [ ! -L "$file" ] || continue
    if [ "$outcome" = stopped ]; then
      [[ $SESSION_TOKEN =~ ^[0-9a-f]{32}$ ]] || return 1
      [ ! -L "$file.stopped" ] && { [ ! -e "$file.stopped" ] || [ -f "$file.stopped" ]; } || return 1
      temporary=$(mktemp "$file.stopped.XXXXXXXXXX") || return 1
      # Each validated stop publishes its launch token atomically. Readers can
      # distinguish a new drain from permission left by an earlier session.
      if ! { printf '%s\n' "$SESSION_TOKEN" >"$temporary" &&
        python3 -I -c 'import os,sys; os.replace(sys.argv[1], sys.argv[2])' "$temporary" "$file.stopped"; }; then
        rm -f -- "$temporary"
        return 1
      fi
    else
      (set -o noclobber; : >"$file.failed") 2>/dev/null || [ -f "$file.failed" ] || return 1
    fi
  done
}

proof_stop_observed() {
  local file=$1 previous=$2 ready=${3:-} stopped
  [ -n "$file" ] && [ ! -e "$file.failed" ] && [ -f "$file.stopped" ] && [ ! -L "$file.stopped" ] || return 1
  stopped=$(<"$file.stopped")
  [[ $stopped =~ ^[0-9a-f]{32}$ ]] && { [ "$stopped" != "$previous" ] || [ "$stopped" = "$ready" ]; }
}

proof_health() {
  local file=$1 allow_stop=$2 stopped='' ready='' health_marker=''
  [[ ${file##*/} =~ ^proof\.[A-Za-z0-9]{10}$ ]] && [ "${file%/*}" = "$RUN_DIR" ] || return 1
  validate_state_path_integrity "$allow_stop"
  [ -f "$file" ] && [ ! -e "$file.failed" ] || return 1
  [ ! -f "$file.stopped" ] || stopped=$(<"$file.stopped")
  if [ -e "$RUN_DIR/session.ready" ]; then
    ready=$(cat "$RUN_DIR/session.ready" 2>/dev/null) || ready=
    [ "$allow_stop" != 1 ] || health_marker=$file
    if session_health "$health_marker" "$stopped" && session_ready && validate_effective_config && [ ! -e "$file.failed" ]; then
      return 0
    fi
    [ "$allow_stop" = 1 ] && proof_stop_observed "$file" "$stopped" "$ready"
  else
    [ "$allow_stop" = 1 ] && proof_stop_observed "$file" ''
  fi
}

cmd_guard() {
  local allow_stop=0 marker status=0 monitor_pid=
  if [ "${1:-}" = --allow-stop ]; then allow_stop=1; shift; fi
  [ "${1:-}" = -- ] && [ "$#" -ge 2 ] || die 'guard requires -- COMMAND [ARG...]'
  shift
  cmd_status >/dev/null || return 1
  marker=$(mktemp "$RUN_DIR/proof.XXXXXXXXXX") || return 1
  trap 'if [ -n "$monitor_pid" ]; then kill -TERM "$monitor_pid" 2>/dev/null || true; wait "$monitor_pid" || true; fi; rm -f -- "$marker" "$marker.failed" "$marker.stopped"; exit 1' TERM INT
  python3 -I - "$ROOT/scripts/dev-https.sh" "$marker" "$allow_stop" "$@" <<'PY' &
import os, select, signal, subprocess, sys, time
script, marker, allow_stop = sys.argv[1:4]
# Keep a live anchor while its command exits or drains, so group cleanup never
# infers signal authority from a stale process number.
anchor_code = '''import os, signal, subprocess, sys, time
signal.signal(signal.SIGTERM, lambda *_: None)
fd = int(sys.argv[1])
try:
    child = subprocess.Popen(sys.argv[2:], close_fds=True)
    result = child.wait()
except OSError:
    result = 127
os.write(fd, (str(result) + "\\n").encode())
os.close(fd)
while True:
    time.sleep(1)
'''
reader, writer = os.pipe()
anchor = None
result = 1
clean = False
def interrupted(*_):
    raise KeyboardInterrupt
signal.signal(signal.SIGTERM, interrupted)
signal.signal(signal.SIGINT, interrupted)
def healthy():
    try:
        return subprocess.run(["bash", script, "proof-health", marker, allow_stop],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15).returncode == 0
    except (OSError, subprocess.SubprocessError):
        return False
try:
    anchor = subprocess.Popen([sys.executable, "-I", "-c", anchor_code, str(writer), *sys.argv[4:]],
        start_new_session=True, pass_fds=(writer,))
    os.close(writer)
    while anchor.poll() is None:
        ready, _, _ = select.select([reader], [], [], 1)
        if not healthy():
            break
        if ready:
            value = os.read(reader, 32).strip()
            if value and value.lstrip(b"-").isdigit():
                code = int(value)
                result = code if 0 <= code <= 255 else 1
            break
except (OSError, KeyboardInterrupt):
    result = 1
finally:
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    if anchor is not None and anchor.poll() is None:
        try:
            if os.getpgid(anchor.pid) == anchor.pid and os.getsid(anchor.pid) == anchor.pid:
                os.killpg(anchor.pid, signal.SIGTERM)
                time.sleep(0.2)
                if anchor.poll() is None:
                    os.killpg(anchor.pid, signal.SIGKILL)
                anchor.wait(timeout=5)
                clean = True
        except (OSError, subprocess.SubprocessError):
            pass
    # TERM can finish the command and make the anchor publish its status.
    # Keep its reader alive until group cleanup has killed and joined it.
    os.close(reader)
sys.exit(result if clean else 1)
PY
  monitor_pid=$!
  wait "$monitor_pid" || status=$?
  trap - TERM INT
  proof_health "$marker" "$allow_stop" || status=1
  rm -f -- "$marker" "$marker.failed" "$marker.stopped"
  return "$status"
}

if [[ ${BASH_SOURCE[0]} = "$0" ]]; then
  set -uo pipefail
  umask 077
  source "${BASH_SOURCE[0]%/*}/platform.sh"
  source "${BASH_SOURCE[0]%/*}/dev-https-state.sh"
  source "${BASH_SOURCE[0]%/*}/dev-https-identity.sh"
  supervise_session "$@"
fi
