#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
set -euo pipefail

fail() { echo "$1" >&2; exit 1; }

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
sender=$repo/deploy/vn/host/bin/alert-telegram.sh
transport=$repo/deploy/vn/host/alert-transport.sh
if [[ ${1-} == root-routing ]]; then
  ((EUID == 0)) || { echo 'root-routing requires a root runner' >&2; exit 1; }
  for test_root in '' /nonexistent/aboutme-telegram-test; do
    if output=$(ABOUTME_ALERT_TEST_ROOT=$test_root ABOUTME_ALERT_TEST_ENDPOINT=http://127.0.0.1:20983 \
      bash "$sender" --test 2>&1); then fail 'root test routing was accepted'; fi
    [[ $output == 'alert-telegram: invalid_secret' ]] || fail 'root test routing did not report invalid_secret'
  done
  echo 'root Telegram routing refusal passed'
  exit 0
fi
work=$(mktemp -d)
server_pid=
cleanup() {
  [[ -z $server_pid ]] || { kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; }
  rm -rf "$work"
}
trap cleanup EXIT
((EUID != 0)) || { echo 'Telegram fixtures require a non-root user' >&2; exit 1; }
mkdir -m 0700 "$work/state" "$work/state/secrets" "$work/bin" "$work/clock-reads"
printf '123456:synthetic_BOT-token' >"$work/state/secrets/telegram-bot-token"
printf '%s' '-1001234567890' >"$work/state/secrets/telegram-chat-id"
chmod 0400 "$work/state/secrets/"*
export ABOUTME_ALERT_TEST_ROOT=$work/state
export ABOUTME_ALERT_TEST_ENDPOINT=http://127.0.0.1:20983
export TG_FIXTURE=$work TG_REAL_CURL TG_REAL_STAT TG_REAL_SHA256SUM TG_REAL_SLEEP TG_REAL_DATE TG_TEST_PID=$$
TG_REAL_CURL=$(command -v curl) || fail 'curl is unavailable'
TG_REAL_STAT=$(command -v stat) || fail 'stat is unavailable'
TG_REAL_SHA256SUM=$(command -v sha256sum) || fail 'sha256sum is unavailable'
TG_REAL_SLEEP=$(command -v sleep) || fail 'sleep is unavailable'
TG_REAL_DATE=$(command -v date) || fail 'date is unavailable'
cat >"$work/bin/sha256sum" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$@" >>"$TG_FIXTURE/hash-argv"
[[ $# == 0 ]] || exit 1
if env | grep -E 'synthetic_BOT-token|-1001234567890' >/dev/null; then
  : >"$TG_FIXTURE/hash-secret-exported"
  exit 1
fi
exec "$TG_REAL_SHA256SUM" "$@"
STUB
cat >"$work/bin/stat" <<'STUB'
#!/usr/bin/env bash
if env | grep -E 'synthetic_BOT-token|-1001234567890' >/dev/null; then
  : >"$TG_FIXTURE/stat-secret-exported"
  exit 1
fi
"$TG_REAL_STAT" "$@" || exit
# Remove the directory after admission to force a real stamp creation error.
if [[ ${TG_STAMP_CREATE_FAIL:-0} == 1 && $* == "-c %u:%a -- $TG_FIXTURE/state/alerts" ]]; then
  mv "$TG_FIXTURE/state/alerts" "$TG_FIXTURE/alerts-unavailable"
fi
STUB
cat >"$work/bin/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >>"$TG_FIXTURE/argv"
if env | grep -E 'synthetic_BOT-token|-1001234567890' >/dev/null; then
  echo 'Telegram credential reached the curl environment' >&2; exit 1
fi
reply() {
  local body=$1
  shift
  while (($#)); do
    if [[ $1 == --output ]]; then printf '%s' "$body" >"$2"; break; fi
    shift
  done
}
mode=$(<"$TG_FIXTURE/scenario")
if [[ $mode == *-then-success ]]; then
  failures=0
  [[ ! -e $TG_FIXTURE/failures ]] || failures=$(<"$TG_FIXTURE/failures")
  if ((failures < ${TG_FAILURES_BEFORE_SUCCESS:-1})); then
    printf '%s\n' "$((failures + 1))" >"$TG_FIXTURE/failures"
    mode=${mode%-then-success}
  else
    mode=success
  fi
fi
printf '%s\n' "$mode" >>"$TG_FIXTURE/attempts"
case $mode in
  dns | connect | preupload-timeout)
    cat >/dev/null
    reply '' "$@"
    printf 'http:000\nupload:0\nrequest:0\n'
    echo 'synthetic_BOT-token provider diagnostic' >&2
    case $mode in dns) exit 6 ;; connect) exit 7 ;; *) exit 28 ;; esac ;;
  headers-timeout | unknown-timeout)
    cat >/dev/null
    reply '' "$@"
    if [[ $mode == headers-timeout ]]; then printf 'http:000\nupload:0\nrequest:200\n'; fi
    exit 28 ;;
  tls) cat >/dev/null; echo 'synthetic_BOT-token provider diagnostic' >&2; exit 60 ;;
  partial-success | connect-with-response)
    cat >/dev/null
    reply '{"ok":true}' "$@"
    printf 'http:200\nupload:100\nrequest:200\n'
    [[ $mode != partial-success ]] || exit 18
    exit 7 ;;
  timeout) exec "$TG_REAL_CURL" "$@" --max-time 0.2 ;;
  *) exec "$TG_REAL_CURL" "$@" ;;
esac
STUB
cat >"$work/bin/sleep" <<'STUB'
#!/usr/bin/env bash
if [[ $# == 1 && $1 == 10 ]]; then
  printf '10\n' >>"$TG_FIXTURE/retry-delays"
  if [[ ${TG_RETRY_PAUSE:-0} == 1 ]]; then
    : >"$TG_FIXTURE/retry-waiting"
    for ((i=0; i<500; i++)); do
      [[ ! -e $TG_FIXTURE/retry-release ]] || exit 0
      "$TG_REAL_SLEEP" 0.01
    done
    exit 1
  fi
  exit 0
fi
exec "$TG_REAL_SLEEP" "$@"
STUB
cat >"$work/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
[[ $* == daemon-reload ]] || { echo 'unexpected systemctl fixture arguments' >&2; exit 1; }
printf 'reload\n' >>"$TG_FIXTURE/reloads"
if [[ -e $TG_FIXTURE/reload-fails ]]; then
  echo 'systemctl fixture reload failure' >&2; exit 1
fi
STUB
cat >"$work/bin/date" <<'STUB'
#!/usr/bin/env bash
if ! [[ $# -ge 3 && $1 == -u && $2 == -d && $3 =~ ^@[0-9]+$ ]]; then
  sender='' pid=$PPID
  while ((pid > 1 && pid != TG_TEST_PID)) && stat=$(<"/proc/$pid/stat"); do
    read -r -a fields <<<"${stat##*) }"
    mapfile -d '' -t args <"/proc/$pid/cmdline"
    for arg in "${args[@]}"; do [[ $arg != */alert-telegram.sh ]] || sender=$pid-${fields[19]}; done
    pid=${fields[1]}
  done
  if [[ -n $sender ]] && ! (set -o noclobber; : >"$TG_FIXTURE/clock-reads/$sender") 2>/dev/null; then
    printf '%s\n' "$*" >>"$TG_FIXTURE/clock-read-twice"
    echo 'the sender read the clock twice' >&2; exit 1
  fi
fi
case $* in
  '-u +%s') exec "$TG_REAL_DATE" -u -d "${TG_TEST_DAY:-2030-01-02} ${TG_TEST_TIME:-03:04:05}" +%s ;;
  '-u -d @'[0-9]*' +%F') exec "$TG_REAL_DATE" "$@" ;;
  *) echo 'unexpected date fixture arguments' >&2; exit 1 ;;
esac
STUB
chmod +x "$work/bin/"*
export PATH=$work/bin:$PATH
export CURL_HOME=$work/curl-home
mkdir "$CURL_HOME"
printf 'invalid-curlrc-option\n' >"$CURL_HOME/.curlrc"
cat >"$work/endpoint.py" <<'PY'
import http.server
import json
import pathlib
import sys
import time

root = pathlib.Path(sys.argv[1])

class Endpoint(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        data = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        with (root / "requests").open("a") as out:
            out.write(json.dumps({"path": self.path, "body": json.loads(data)}) + "\n")
        mode = (root / "scenario").read_text().strip()
        status = int(mode) if mode.isdigit() else 200
        bodies = {"false": b'{"ok":false,"description":"synthetic_BOT-token"}',
                  "string": b'{"ok":"true"}', "array": b'[{"ok":true}]',
                  "malformed": b'not JSON synthetic_BOT-token', "empty": b'',
                  "multiple": b'{"ok":true}\n{"ok":true}',
                  "boundary": b'{"ok":true}' + b' ' * (16384 - 11),
                  "oversized": b'{"ok":true}' + b' ' * 65536}
        if mode in ("timeout", "slow"):
            (root / "entered").touch()
            time.sleep(0.5 if mode == "timeout" else 1)
        self.send_response(status)
        if status in (301, 302, 307, 308):
            self.send_header("Location", "http://127.0.0.1:20983/redirected")
        self.end_headers()  # Deliberately omit Content-Length.
        try:
            self.wfile.write(bodies.get(mode, b'{"ok":true}'))
        except (BrokenPipeError, ConnectionResetError):
            pass

server = http.server.ThreadingHTTPServer(("127.0.0.1", 20983), Endpoint)
(root / "ready").touch()
server.serve_forever()
PY
printf success >"$work/scenario"
python3 -I -B "$work/endpoint.py" "$work" >"$work/endpoint.out" 2>&1 &
server_pid=$!
for ((i=0; i<100; i++)); do [[ ! -e $work/ready ]] || break; sleep 0.01; done
[[ -e $work/ready ]] || { echo 'fake Telegram endpoint did not start' >&2; exit 1; }

run_sender() { bash "$sender" "$@" >"$work/out" 2>"$work/err"; }
count_requests() { if [[ -f $work/requests ]]; then wc -l <"$work/requests"; else echo 0; fi; }
count_attempts() { if [[ -f $work/attempts ]]; then wc -l <"$work/attempts"; else echo 0; fi; }
count_delays() { if [[ -f $work/retry-delays ]]; then wc -l <"$work/retry-delays"; else echo 0; fi; }
private_output() {
  if grep -qE 'synthetic_BOT-token|-1001234567890|/bot123456:' \
    "$work/out" "$work/err" "$work/argv" "$work/hash-argv" "$work/cutover.out"; then
    echo 'Telegram credential escaped its pipe' >&2; exit 1
  fi
  [[ ! -e $work/hash-secret-exported ]] || fail 'Telegram credential reached the hash environment'
  [[ ! -e $work/stat-secret-exported ]] || fail 'Telegram credential reached the stat environment'
  [[ ! -e $work/clock-read-twice ]] || fail 'the sender read the clock more than once'
}
clock='10:04 ICT · 03:04 UTC'
msg() {
  printf '%s <b>aboutme</b> · %s\n%s\n%s\n<i>%s · %s · <a href="https://github.com/dannyota/aboutme/blob/main/docs/runbooks/%s">runbook</a></i>' \
    "$1" "$2" "$3" "$4" "$clock" "$5" "$6"
}
expect_text() {
  python3 -I - "$work/requests" "$1" <<'PY' || fail "$2"
import json, sys
body = json.loads(open(sys.argv[1], encoding="utf-8").readlines()[-1])["body"]
assert set(body) == {"chat_id", "text", "parse_mode", "link_preview_options"}, "unexpected Telegram body fields"
assert body["parse_mode"] == "HTML", "HTML parse mode missing"
assert body["link_preview_options"] == {"is_disabled": True}, "link previews are not disabled"
assert len(body["text"].encode()) <= 1500, "message exceeds 1500 bytes"
assert body["text"] == sys.argv[2], "message text differs:\n" + body["text"] + "\nexpected:\n" + sys.argv[2]
PY
}
send_case() {
  printf '%s' "$4" | TG_TEST_DAY=$1 run_sender "$2" "$3" || fail "delivery failed for $2"
  grep -qxF "alert-telegram: sent $2" "$work/err" || fail "delivery did not report sent for $2"
  expect_text "$5" "message text changed for $2"
}
delivery_test=$(msg 🟢 'alert delivery test' 'This message tests host alert delivery.' 'System: no action needed.' \
  alert-telegram production.md#host-telegram-setup-and-rotation)
if run_sender unconfirmed diagnostic </dev/null; then fail 'unconfirmed delivery was accepted'; fi
grep -qxF 'alert-telegram: invalid_secret unconfirmed' "$work/err" || fail 'unconfirmed delivery did not report invalid_secret'
[[ $(count_requests) == 0 && ! -e $work/state/alerts ]] || fail 'unconfirmed delivery sent or created stamps'
telegram_token=inherited telegram_chat=inherited digest=inherited run_sender --test </dev/null || fail 'initial test delivery failed'
expect_text "$delivery_test" 'delivery test message changed'
touch -d '2030-01-01 00:00:00 UTC' "$work/state/secrets/telegram-bot-token"
telegram_token=inherited telegram_chat=inherited digest=inherited \
  bash "$transport" confirm-receipt --received >"$work/cutover.out" 2>&1 || fail 'receipt rejected unchanged credentials after a metadata change'
[[ -s $work/hash-argv ]] || fail 'credential identity did not use the hash pipe'
grep -qxE '[0-9a-f]{64}' "$work/state/telegram-approved" || fail 'approval did not store a credential digest'
[[ $(stat -c %a "$work/state/telegram-approved") == 400 ]] || fail 'credential digest was not private'
mv "$work/state/alert-transport" "$work/selector"
if run_sender missing-selector diagnostic </dev/null; then fail 'delivery without a selector was accepted'; fi
grep -qxF 'alert-telegram: invalid_secret missing-selector' "$work/err" || fail 'missing selector did not report invalid_secret'
[[ $(count_requests) == 1 && ! -e $work/state/alerts ]] || fail 'missing selector sent or created stamps'
mv "$work/selector" "$work/state/alert-transport"
: >"$work/requests"
: >"$work/argv"
attempts_before=$(count_attempts)
delays_before=$(count_delays)
printf 'approved detail\n' | run_sender host-check 'host diagnostic' || fail 'approved host delivery failed'
grep -qxF 'alert-telegram: sent host-check' "$work/err" || fail 'host delivery did not report sent'
[[ $(count_requests) == 1 ]] || fail 'host delivery did not send exactly once'
[[ $(count_attempts) == $((attempts_before + 1)) && $(count_delays) == "$delays_before" ]] || fail 'successful delivery retried or waited'
expect_text "$(msg 🔴 'host alert' 'host diagnostic' 'Action: read the journal on the host.' aboutme-host production.md#alarms)" \
  'unknown key message changed'
run_sender host-check 'host diagnostic' </dev/null || fail 'duplicate host delivery failed'
grep -qxF 'alert-telegram: suppressed host-check' "$work/err" || fail 'duplicate host delivery did not report suppressed'
[[ $(count_requests) == 1 ]] || fail 'duplicate host delivery sent another request'
[[ $(count_attempts) == $((attempts_before + 1)) && $(count_delays) == "$delays_before" ]] || fail 'suppressed delivery attempted transport or waited'
private_output
python3 -I - "$work/requests" <<'PY' || fail 'Telegram request validation failed'
import json, sys
r = json.loads(open(sys.argv[1]).readline())
assert r["path"] == "/bot123456:synthetic_BOT-token/sendMessage", "unexpected Telegram API path"
assert r["body"]["chat_id"] == "-1001234567890", "unexpected Telegram destination"
assert "approved detail" not in r["body"]["text"], "unknown key detail reached the message"
PY
[[ $(stat -c %a "$work/state/alerts/host-check.2030-01-02") == 600 ]] || fail 'daily stamp was not private'
TG_TEST_DAY=2030-01-03 run_sender host-check diagnostic </dev/null || fail 'next-day delivery failed'
[[ $(count_requests) == 2 && -e $work/state/alerts/host-check.2030-01-03 ]] || fail 'next-day delivery or stamp missing'
: >"$work/state/alerts/smtp-stamp.2030-01-02"
chmod 0600 "$work/state/alerts/smtp-stamp.2030-01-02"
run_sender smtp-stamp diagnostic </dev/null || fail 'SMTP stamp suppression failed'
[[ $(count_requests) == 2 ]] || fail 'existing SMTP stamp allowed another request'
[[ -z $(find "$work/state" -type f ! -path '*/secrets/*' ! -path '*/alerts/*' ! -path '*/systemd/*' \
  ! -name '.telegram.lock' ! -name 'telegram-approved' ! -name 'alert-transport' -print) ]] || fail 'delivery left unexpected state files'
grep -qxF -- '-q' "$work/argv" || fail 'curl config suppression option missing'
python3 -I - "$work/argv" <<'PY' || fail 'curl argument validation failed'
import sys
args = open(sys.argv[1]).read().splitlines()
assert args[0] == "-q", "curl config suppression was not the first option"
for flag, value in {"--connect-timeout": "5", "--max-time": "15",
                    "--max-redirs": "0", "--retry": "0", "--proxy": "",
                    "--noproxy": "*"}.items():
    assert flag in args, "curl safety option missing"
    index = args.index(flag) + 1
    assert index < len(args) and args[index] == value, "curl safety option value changed"
assert args[args.index("--write-out") + 1] == r"http:%{http_code}\nupload:%{size_upload}\nrequest:%{size_request}\n", "curl delivery evidence missing"
PY
for option in --connect-timeout --max-time --proto --proto-redir --max-redirs --retry --proxy --noproxy; do
  grep -qxF -- "$option" "$work/argv" || fail 'curl safety option missing'
done
if grep -qE -- '(^-L$|^--location|^--insecure$|^--verbose$|^--trace)' "$work/argv"; then fail 'unsafe curl option used'; fi

for change in mode timestamp restore; do
  case $change in
    mode)
      chmod 0600 "$work/state/secrets/telegram-bot-token"
      chmod 0400 "$work/state/secrets/telegram-bot-token" ;;
    timestamp) touch -d '2030-01-02 00:00:00 UTC' "$work/state/secrets/telegram-bot-token" ;;
    restore)
      cp "$work/state/secrets/telegram-bot-token" "$work/restored-token"
      chmod 0400 "$work/restored-token"
      mv -f "$work/restored-token" "$work/state/secrets/telegram-bot-token" ;;
  esac
  before=$(count_requests)
  run_sender "metadata-$change" diagnostic </dev/null || fail 'metadata-only credential change lost approval'
  grep -qxF "alert-telegram: sent metadata-$change" "$work/err" || fail 'metadata-only change did not report sent'
  [[ $(count_requests) == $((before + 1)) ]] || fail 'metadata-only change did not send exactly once'
  private_output
done

for name in telegram-bot-token telegram-chat-id; do
  original=$(<"$work/state/secrets/$name")
  chmod 0600 "$work/state/secrets/$name"
  case $name in
    telegram-bot-token) printf '123456:changed_synthetic-token' >"$work/state/secrets/$name" ;;
    telegram-chat-id) printf '%s' '-1008888888888' >"$work/state/secrets/$name" ;;
  esac
  chmod 0400 "$work/state/secrets/$name"
  before=$(count_requests)
  if run_sender "changed-$name" diagnostic </dev/null; then fail 'changed credentials reused approval'; fi
  grep -qxF "alert-telegram: invalid_secret changed-$name" "$work/err" || fail 'changed content did not report invalid_secret'
  [[ $(count_requests) == "$before" && ! -e $work/state/alerts/changed-$name.2030-01-02 ]] || fail 'changed credentials sent or created a stamp'
  private_output
  chmod 0600 "$work/state/secrets/$name"
  printf '%s' "$original" >"$work/state/secrets/$name"
  chmod 0400 "$work/state/secrets/$name"
  run_sender "restored-$name" diagnostic </dev/null || fail 'restoring identical credentials lost approval'
  private_output
done

before=$(count_requests)
if TG_STAMP_CREATE_FAIL=1 run_sender stamp-create diagnostic </dev/null; then fail 'stamp creation failure was accepted'; fi
grep -qxF 'alert-telegram: stamp_failed stamp-create' "$work/err" || fail 'stamp creation failure did not report stamp_failed'
[[ ! -e $work/state/alerts/stamp-create.2030-01-02 && $(count_requests) == "$before" ]] || fail 'stamp creation failure sent or retained a stamp'
mv "$work/alerts-unavailable" "$work/state/alerts"
run_sender stamp-create diagnostic </dev/null || fail 'delivery after stamp directory recovery failed'
for shape in directory symlink mode content; do
  bad_stamp=$work/state/alerts/invalid-stamp-$shape.2030-01-02
  case $shape in
    directory) mkdir "$bad_stamp" ;;
    symlink) ln -s host-check.2030-01-02 "$bad_stamp" ;;
    mode) : >"$bad_stamp"; chmod 0644 "$bad_stamp" ;;
    content) printf invalid >"$bad_stamp"; chmod 0600 "$bad_stamp" ;;
  esac
  before=$(count_requests)
  if run_sender "invalid-stamp-$shape" diagnostic </dev/null; then fail 'invalid stamp was accepted'; fi
  grep -qxF "alert-telegram: stamp_failed invalid-stamp-$shape" "$work/err" || fail 'invalid stamp did not report stamp_failed'
  [[ $(count_requests) == "$before" ]] || fail 'invalid stamp allowed a request'
  rm -rf -- "$bad_stamp"
done

for mode in dns connect preupload-timeout; do
  printf '%s' "$mode" >"$work/scenario"
  before=$(count_requests)
  attempts_before=$(count_attempts)
  delays_before=$(count_delays)
  if run_sender "ordinary-$mode" diagnostic </dev/null; then fail 'ordinary transport failure was accepted'; fi
  grep -qxF "alert-telegram: transport_failed ordinary-$mode" "$work/err" || fail 'ordinary transport failure outcome missing'
  [[ $(count_requests) == "$before" && $(count_attempts) == $((attempts_before + 1)) &&
    $(count_delays) == "$delays_before" ]] || fail 'ordinary alert retried or waited'
  [[ ! -e $work/state/alerts/ordinary-$mode.2030-01-02 ]] || fail 'ordinary failure retained its stamp'
  if run_sender --test </dev/null; then fail 'test transport failure was accepted'; fi
  grep -qxF 'alert-telegram: transport_failed' "$work/err" || fail 'test transport failure outcome missing'
  [[ $(count_attempts) == $((attempts_before + 2)) && $(count_delays) == "$delays_before" &&
    ! -e $work/state/telegram-test-passed ]] || fail 'test alert retried, waited, or retained its proof'
  private_output
done
printf success >"$work/scenario"
attempts_before=$(count_attempts)
delays_before=$(count_delays)
run_sender unit-first-attempt diagnostic </dev/null || fail 'first unit alert attempt failed'
[[ $(count_attempts) == $((attempts_before + 1)) && $(count_delays) == "$delays_before" ]] || fail 'successful unit alert retried or waited'

for mode in dns connect preupload-timeout; do
  printf '%s-then-success' "$mode" >"$work/scenario"
  rm -f "$work/failures"
  before=$(count_requests)
  attempts_before=$(count_attempts)
  delays_before=$(count_delays)
  run_sender "unit-transient-$mode" diagnostic </dev/null || fail 'transient transport failure did not recover'
  grep -qxF "alert-telegram: sent unit-transient-$mode" "$work/err" || fail 'transport recovery did not report sent'
  [[ $(count_requests) == $((before + 1)) && $(count_attempts) == $((attempts_before + 2)) &&
    $(count_delays) == $((delays_before + 1)) ]] || fail 'transient failure did not retry once before success'
  [[ -e $work/state/alerts/unit-transient-$mode.2030-01-02 ]] || fail 'transport recovery did not retain its stamp'
  run_sender "unit-transient-$mode" diagnostic </dev/null || fail 'recovered delivery suppression failed'
  [[ $(count_attempts) == $((attempts_before + 2)) && $(count_delays) == $((delays_before + 1)) ]] || fail 'recovered delivery was sent again'
  private_output
done
printf dns-then-success >"$work/scenario"
rm -f "$work/failures"
attempts_before=$(count_attempts)
delays_before=$(count_delays)
TG_FAILURES_BEFORE_SUCCESS=2 run_sender unit-third-attempt diagnostic </dev/null || fail 'third transport attempt did not recover'
[[ $(count_attempts) == $((attempts_before + 3)) && $(count_delays) == $((delays_before + 2)) ]] || fail 'third-attempt recovery exceeded its attempt or wait budget'

for mode in 201 400 401 403 429 500 503 301 302 307 308 false string array malformed empty multiple oversized dns connect preupload-timeout headers-timeout unknown-timeout tls timeout partial-success connect-with-response; do
  printf '%s' "$mode" >"$work/scenario"
  before=$(count_requests)
  attempts_before=$(count_attempts)
  delays_before=$(count_delays)
  if run_sender "unit-failure-$mode" diagnostic </dev/null; then fail 'failed Telegram response was accepted'; fi
  if compgen -G "$work/state/alerts/unit-failure-$mode.*" >/dev/null; then fail 'failed delivery retained a stamp'; fi
  case $mode in
    dns | connect | preupload-timeout | headers-timeout | unknown-timeout | tls | timeout | partial-success | connect-with-response) grep -qxF "alert-telegram: transport_failed unit-failure-$mode" "$work/err" || fail 'transport failure outcome missing' ;;
    oversized) grep -qxE "alert-telegram: (invalid_response|transport_failed) unit-failure-$mode" "$work/err" || fail 'oversized response failure outcome missing' ;;
    [2345]*) grep -qxF "alert-telegram: rejected unit-failure-$mode" "$work/err" || fail 'HTTP rejection outcome missing' ;;
    *) grep -qxF "alert-telegram: invalid_response unit-failure-$mode" "$work/err" || fail 'invalid response outcome missing' ;;
  esac
  attempts=1 delays=0
  case $mode in dns | connect | preupload-timeout) attempts=3 delays=2 ;; esac
  [[ $(count_attempts) == $((attempts_before + attempts)) && $(count_delays) == $((delays_before + delays)) ]] || fail 'failed delivery exceeded or skipped its retry budget'
  after=$(count_requests)
  ((after - before <= 1)) || { echo 'sender retried or followed a redirect' >&2; exit 1; }
  private_output
  printf success >"$work/scenario"
  run_sender "unit-failure-$mode" diagnostic </dev/null || fail 'delivery after a failed response did not recover'
done
printf boundary >"$work/scenario"
run_sender reply-boundary diagnostic </dev/null || fail 'response at the size limit was rejected'

printf success >"$work/scenario"
watcher=production.md#host-watcher backups=production.md#backups-and-restore crowdsec=crowdsec-postgresql.md
grow='Action: grow the volume with vngcloud volume resize-volume, then growpart and resize2fs.'
units_action="Action: read each failed unit's journal on the host."
unit_action="Action: read the unit's journal on the host."
pg_action='Action: check the PostgreSQL service and its journal on the host.'
archive_action='Action: check pgBackRest archiving and the backup bucket from the host.'
missing_action='Action: check pgBackRest and the backup bucket from the host.'
stale_action='Action: check the backup timers and the last pgBackRest run on the host.'
bans_action='Action: review the CrowdSec alerts on the host for an attack or false positives.'
coraza_action='Action: read the Coraza metadata on the host.'
totp_action='Action: read the totp_unavailable reasons in the aboutme-server journal.'
retention_action='Action: follow the CrowdSec failure recovery before reopening the edge.'
overdue_system='System: this retention pass deletes it.'
vacuum_system='System: the next hourly vacuum tries again.'
unknown_action='Action: read the journal on the host.'
failed_names='' listed=''
for ((i=0; i<20; i++)); do printf -v failed_names '%saboutme-long-failed-unit-%03d.service\n' "$failed_names" "$i"; done
for ((i=0; i<7; i++)); do printf -v listed '%s%saboutme-long-failed-unit-%03d.service' "$listed" "${listed:+, }" "$i"; done
hostile='<b>bold</b> & "quoted" '\''single'\'' \back <a href="x">link</a> >'
hostile_html='&lt;b&gt;bold&lt;/b&gt; &amp; "quoted" '\''single'\'' \back &lt;a href="x"&gt;link&lt;/a&gt; &gt;'

events() {
  send_case "$1" unit-aboutme-server 'unit aboutme-server failed' '' \
    "$(msg 🔴 'unit failed' 'Unit aboutme-server failed.' "$unit_action" aboutme-server production.md#unit-failures)"
  send_case "$1" unit-aboutme-crowdsec-retention 'unit aboutme-crowdsec-retention failed' '' \
    "$(msg 🔴 'CrowdSec retention failed' 'CrowdSec retention failed, so the host stops Caddy and CrowdSec.' \
      "$retention_action" aboutme-crowdsec-retention "$crowdsec#failure-recovery")"
  send_case "$1" unit-crowdsec_retention_overdue 'unit crowdsec_retention_overdue failed' '' \
    "$(msg 🟠 'CrowdSec retention overdue' 'A CrowdSec record passed its retention deadline before deletion.' \
      "$overdue_system" aboutme-crowdsec-retention "$crowdsec#retention-and-maintenance")"
  send_case "$1" crowdsec-vacuum 'CrowdSec vacuum failed' '' \
    "$(msg 🔴 'CrowdSec vacuum failed' 'The hourly CrowdSec vacuum failed.' "$vacuum_system" \
      aboutme-crowdsec-vacuum "$crowdsec#retention-and-maintenance")"
  send_case "$1" hostile-subject "$hostile" '<i>detail</i>' \
    "$(msg 🔴 'host alert' "$hostile_html" "$unknown_action" aboutme-host production.md#alarms)"
}

day=2030-01-05
send_case $day disk-root '/ is 85% full' $'grow it: vngcloud volume resize-volume, then growpart and resize2fs\n' \
  "$(msg 🟠 'disk almost full' '/ is 85% full.' "$grow" aboutme-watch "$watcher")"
send_case $day disk-data '/srv/data is ?% full' '' \
  "$(msg 🟠 'disk almost full' 'Disk use of /srv/data cannot be read.' "$grow" aboutme-watch "$watcher")"
send_case $day failed-units 'failed units: 101' "${failed_names}omitted units: 81"$'\n' \
  "$(msg 🔴 'units failed' "101 units failed: $listed and 94 more." "$units_action" aboutme-watch "$watcher")"
send_case $day postgres-down 'PostgreSQL does not answer on /run/postgresql' '' \
  "$(msg 🔴 'PostgreSQL down' 'PostgreSQL does not answer on /run/postgresql.' "$pg_action" aboutme-watch "$watcher")"
send_case $day archive-failing 'WAL archiving to the backup bucket is failing' '' \
  "$(msg 🔴 'WAL archiving failing' 'WAL archiving to the backup bucket is failing.' "$archive_action" aboutme-watch "$backups")"
send_case $day backup-missing 'no pgBackRest backup can be read' '' \
  "$(msg 🔴 'backup missing' 'No pgBackRest backup can be read.' "$missing_action" aboutme-watch "$backups")"
send_case $day backup-stale 'the newest backup is 27 hours old' '' \
  "$(msg 🟠 'backup stale' 'The newest backup is 27 hours old.' "$stale_action" aboutme-watch "$backups")"
send_case $day crowdsec-bans 'CrowdSec raised 51 alerts in the last hour' '' \
  "$(msg 🟠 'many CrowdSec alerts' 'CrowdSec raised 51 alerts in the last hour.' "$bans_action" aboutme-watch "$watcher")"
send_case $day coraza-log-invalid 'the Coraza match log has 1 unknown records in the last hour' '' \
  "$(msg 🟠 'Coraza log invalid' 'The Coraza match log has 1 unknown record in the last hour.' "$coraza_action" \
    aboutme-watch production.md#coraza-logs)"
send_case $day totp-unavailable 'the server logged totp_unavailable 3 times' '' \
  "$(msg 🔴 'TOTP unavailable' 'The server logged totp_unavailable 3 times.' "$totp_action" aboutme-watch "$watcher")"
events $day
send_case $day disk-hostile '/mnt/<b>&"x"</b> is 9% full' '' \
  "$(msg 🟠 'disk almost full' '/mnt/&lt;b&gt;&amp;"x"&lt;/b&gt; is 9% full.' "$grow" aboutme-watch "$watcher")"
send_case $day disk-fallback '<i>x</i> & "y" is full' '' \
  "$(msg 🟠 'disk almost full' '&lt;i&gt;x&lt;/i&gt; &amp; "y" is full' "$grow" aboutme-watch "$watcher")"

day=2030-01-06
since=', still failing since the alert on 2030-01-05 UTC'
send_case $day disk-root '/ is 86% full' '' \
  "$(msg 🟠 "disk almost full$since" '/ is 86% full.' "$grow" aboutme-watch "$watcher")"
send_case $day disk-data '/srv/data is 71% full' '' \
  "$(msg 🟠 "disk almost full$since" '/srv/data is 71% full.' "$grow" aboutme-watch "$watcher")"
send_case $day failed-units 'failed units: 1' $'a&<b>.service\nomitted units: 0\n' \
  "$(msg 🟠 "units failed$since" '1 unit failed: a&amp;&lt;b&gt;.service.' "$units_action" aboutme-watch "$watcher")"
send_case $day postgres-down 'PostgreSQL does not answer on /run/postgresql' '' \
  "$(msg 🟠 "PostgreSQL down$since" 'PostgreSQL does not answer on /run/postgresql.' "$pg_action" aboutme-watch "$watcher")"
send_case $day archive-failing 'WAL archiving to the backup bucket is failing' '' \
  "$(msg 🟠 "WAL archiving failing$since" 'WAL archiving to the backup bucket is failing.' "$archive_action" aboutme-watch "$backups")"
send_case $day backup-missing 'no pgBackRest backup can be read' '' \
  "$(msg 🟠 "backup missing$since" 'No pgBackRest backup can be read.' "$missing_action" aboutme-watch "$backups")"
send_case $day backup-stale 'the newest backup is 1 hours old' '' \
  "$(msg 🟠 "backup stale$since" 'The newest backup is 1 hour old.' "$stale_action" aboutme-watch "$backups")"
send_case $day crowdsec-bans 'CrowdSec raised 1 alerts in the last hour' '' \
  "$(msg 🟠 "many CrowdSec alerts$since" 'CrowdSec raised 1 alert in the last hour.' "$bans_action" aboutme-watch "$watcher")"
send_case $day coraza-log-invalid 'the Coraza match log is malformed' '' \
  "$(msg 🟠 "Coraza log invalid$since" 'The Coraza match log is malformed.' "$coraza_action" aboutme-watch production.md#coraza-logs)"
send_case $day totp-unavailable 'the server logged totp_unavailable 1 times' '' \
  "$(msg 🟠 "TOTP unavailable$since" 'The server logged totp_unavailable 1 time.' "$totp_action" aboutme-watch "$watcher")"
events $day

day=2030-01-07
chmod 0644 "$work/state/alerts/postgres-down.2030-01-06"
send_case $day postgres-down 'PostgreSQL does not answer on /run/postgresql' '' \
  "$(msg 🔴 'PostgreSQL down' 'PostgreSQL does not answer on /run/postgresql.' "$pg_action" aboutme-watch "$watcher")"
mv "$work/state/alerts/totp-unavailable.2030-01-06" "$work/state/alerts/totp-real"
ln -s totp-real "$work/state/alerts/totp-unavailable.2030-01-06"
send_case $day totp-unavailable 'the server logged totp_unavailable 2 times' '' \
  "$(msg 🔴 'TOTP unavailable' 'The server logged totp_unavailable 2 times.' "$totp_action" aboutme-watch "$watcher")"
private_output

day=2030-01-09
printf -v amps '%200s' ''
amps=${amps// /"&"}
send_case $day worst-subject "$amps" '' \
  "$(msg 🔴 'host alert' "${amps//&/"&amp;"}" "$unknown_action" aboutme-host production.md#alarms)"
printf -v longest_key '%0120d' 1
printf -v longest_subject '%0200d' 1
send_case $day "$longest_key" "$longest_subject" $'quotes: " backslash: \\\n' \
  "$(msg 🔴 'host alert' "$longest_subject" "$unknown_action" aboutme-host production.md#alarms)"
printf -v longest_unit 'u%0114d' 1
send_case $day "unit-$longest_unit" "unit $longest_unit failed" '' \
  "$(msg 🔴 'unit failed' "Unit $longest_unit failed." "$unit_action" "$longest_unit" production.md#unit-failures)"
printf 'aboutme-web\t\000\377.service\nnext\r.service\nomitted units: 0\n' |
  TG_TEST_DAY=$day run_sender failed-units 'failed units: 2' || fail 'control-byte filtering delivery failed'
expect_text "$(msg 🔴 'units failed' '2 units failed: aboutme-web.service, next.service.' "$units_action" aboutme-watch "$watcher")" \
  'control-byte filtering changed'
python3 -I -c 'import sys; sys.stdout.write("\n" * 5000 + "late.service\n")' |
  TG_TEST_DAY=2030-01-11 run_sender failed-units 'failed units: 1' || fail 'oversized detail delivery failed'
expect_text "$(msg 🔴 'units failed' '1 unit failed.' "$units_action" aboutme-watch "$watcher")" 'detail read bound changed'

stamps=$work/state/alerts
pg_down='PostgreSQL does not answer on /run/postgresql'
: >"$stamps/postgres-down.2030-12-30"
chmod 0600 "$stamps/postgres-down.2030-12-30"
clock='00:00 ICT · 17:00 UTC'
TG_TEST_TIME=17:00:00 send_case 2030-12-31 postgres-down "$pg_down" '' \
  "$(msg 🟠 'PostgreSQL down, still failing since the alert on 2030-12-30 UTC' "$pg_down." "$pg_action" aboutme-watch "$watcher")"
[[ -e $stamps/postgres-down.2030-12-31 && ! -e $stamps/postgres-down.2031-01-01 ]] || fail 'ICT midnight stamp did not use the UTC day'
clock='06:59 ICT · 23:59 UTC'
TG_TEST_TIME=23:59:59 send_case 2030-12-31 totp-unavailable 'the server logged totp_unavailable 2 times' '' \
  "$(msg 🔴 'TOTP unavailable' 'The server logged totp_unavailable 2 times.' "$totp_action" aboutme-watch "$watcher")"
[[ -e $stamps/totp-unavailable.2030-12-31 && ! -e $stamps/totp-unavailable.2031-01-01 ]] || fail 'last UTC second stamp did not use the UTC day'
clock='07:00 ICT · 00:00 UTC'
since=', still failing since the alert on 2030-12-31 UTC'
TG_TEST_TIME=00:00:00 send_case 2031-01-01 postgres-down "$pg_down" '' \
  "$(msg 🟠 "PostgreSQL down$since" "$pg_down." "$pg_action" aboutme-watch "$watcher")"
TG_TEST_TIME=00:00:00 send_case 2031-01-01 totp-unavailable 'the server logged totp_unavailable 2 times' '' \
  "$(msg 🟠 "TOTP unavailable$since" 'The server logged totp_unavailable 2 times.' "$totp_action" aboutme-watch "$watcher")"
[[ -e $stamps/postgres-down.2031-01-01 && -e $stamps/totp-unavailable.2031-01-01 ]] || fail 'year rollover stamp did not use the UTC day'
clock='10:04 ICT · 03:04 UTC'
private_output

(
  # shellcheck source=../host/bin/alert-telegram.sh
  source "$sender"
  mark=🔴 title='host alert' before='' after='' third=$unknown_action job=aboutme-host anchor=production.md#alarms
  ict=10:04 utc=03:04
  for ((pad=0; pad<26; pad++)); do
    printf -v field '%*s' "$pad" ''
    field=${field// /x}
    for ((i=0; i<400; i++)); do field+='<b>&"x"</b>'; done
    alert_fit || exit 1
    printf '%s' "$text" >"$work/fit-$pad"
    alert_html "$field"
    printf '%s' "$alert_escaped" >"$work/fit-$pad.field"
  done
) || fail 'oversized message was not shortened'
python3 -I - "$work" "$(msg 🔴 'host alert' '' "$unknown_action" aboutme-host production.md#alarms)" <<'PY' ||
import pathlib, re, sys
work, expected = pathlib.Path(sys.argv[1]), sys.argv[2].split("\n")
for pad in range(26):
    text = (work / f"fit-{pad}").read_text(encoding="utf-8")
    field = (work / f"fit-{pad}.field").read_text(encoding="utf-8")
    assert 1496 <= len(text.encode()) <= 1500, "shortened message size is outside 1496 to 1500 bytes"
    lines = text.split("\n")
    assert len(lines) == 4, "shortened message lost a line"
    assert [lines[0], lines[2], lines[3]] == [expected[0], expected[2], expected[3]], "fixed lines changed"
    assert lines[1].endswith("…") and field.startswith(lines[1][:-1]), "field was not shortened at its end"
    assert [text.count(t) for t in ("<b>", "</b>", "<i>", "</i>", "<a ", "</a>")] == [1] * 6, "markup changed"
    bare = re.sub(r'</?[bi]>|<a href="[^"<>&]*">|</a>', "", text)
    assert "<" not in bare and ">" not in bare, "a tag was cut or injected"
    assert re.fullmatch(r'(?:[^&]|&(?:amp|lt|gt|quot);)*', bare, re.S), "an entity was cut"
PY
  fail 'message shortening validation failed'

for bad in '' $'bad\nsubject' $'bad\tname' $'bad\xc3\xa9' "$(printf '%0201d' 1)"; do
  if run_sender invalid-subject "$bad" </dev/null; then fail 'invalid subject was accepted'; fi
  grep -qxF 'alert-telegram: invalid_input' "$work/err" || fail 'invalid subject outcome missing'
done
if run_sender '../key' subject </dev/null; then fail 'invalid key was accepted'; fi
if ABOUTME_ALERT_TEST_ENDPOINT=https://example.test run_sender invalid-endpoint subject </dev/null; then
  fail 'invalid endpoint was accepted'
fi

cp "$work/state/secrets/telegram-bot-token" "$work/token-copy"
for bad in '' 'bad token' $'123:abc\n' "$(printf '%0257d' 1):abc"; do
  chmod 0600 "$work/state/secrets/telegram-bot-token"
  printf '%s' "$bad" >"$work/state/secrets/telegram-bot-token"
  chmod 0400 "$work/state/secrets/telegram-bot-token"
  if run_sender unsafe-secret diagnostic </dev/null; then fail 'invalid token was accepted'; fi
  grep -qxF 'alert-telegram: invalid_secret unsafe-secret' "$work/err" || fail 'invalid token outcome missing'
done
chmod 0600 "$work/state/secrets/telegram-bot-token"
printf '123:ab\000c' >"$work/state/secrets/telegram-bot-token"
chmod 0400 "$work/state/secrets/telegram-bot-token"
if run_sender unsafe-nul diagnostic </dev/null; then fail 'token with a NUL byte was accepted'; fi
chmod 0600 "$work/state/secrets/telegram-bot-token"
cp "$work/token-copy" "$work/state/secrets/telegram-bot-token"
if run_sender unsafe-mode diagnostic </dev/null; then fail 'token with unsafe permissions was accepted'; fi
chmod 0400 "$work/state/secrets/telegram-bot-token"
mv "$work/state/secrets/telegram-bot-token" "$work/state/secrets/token-real"
ln -s token-real "$work/state/secrets/telegram-bot-token"
if run_sender unsafe-link diagnostic </dev/null; then fail 'symlink token was accepted'; fi
rm "$work/state/secrets/telegram-bot-token"
mv "$work/state/secrets/token-real" "$work/state/secrets/telegram-bot-token"
chmod 0755 "$work/state/secrets"
if run_sender unsafe-directory diagnostic </dev/null; then fail 'unsafe secrets directory was accepted'; fi
chmod 0700 "$work/state/secrets"
cp "$work/state/secrets/telegram-chat-id" "$work/chat-copy"
for bad in '' 'not-a-chat' $'-100123\n' '-123456789012345678901'; do
  chmod 0600 "$work/state/secrets/telegram-chat-id"
  printf '%s' "$bad" >"$work/state/secrets/telegram-chat-id"
  chmod 0400 "$work/state/secrets/telegram-chat-id"
  if run_sender unsafe-chat diagnostic </dev/null; then fail 'invalid chat ID was accepted'; fi
done
chmod 0600 "$work/state/secrets/telegram-chat-id"
cp "$work/chat-copy" "$work/state/secrets/telegram-chat-id"
chmod 0400 "$work/state/secrets/telegram-chat-id"
(
  # shellcheck source=../host/telegram-state.sh
  source "$repo/deploy/vn/host/telegram-state.sh"
  stat() { printf '0:400\n'; }
  if telegram_private_file "$work/state/secrets/telegram-bot-token" 400; then fail 'foreign-owned token was accepted'; fi
)

before=$(count_requests)
run_sender restored-approved diagnostic </dev/null || fail 'restored credentials lost approval'
grep -qxF 'alert-telegram: sent restored-approved' "$work/err" || fail 'restored credentials did not report sent'
[[ $(count_requests) == $((before + 1)) && -e $work/state/alerts/restored-approved.2030-01-02 ]] || fail 'restored credentials did not send and create a stamp'
private_output

printf dns-then-success >"$work/scenario"
rm -f "$work/failures"
before=$(count_requests)
attempts_before=$(count_attempts)
delays_before=$(count_delays)
TG_RETRY_PAUSE=1 run_sender unit-retry-concurrent diagnostic </dev/null &
first=$!
for ((i=0; i<100; i++)); do [[ ! -e $work/retry-waiting ]] || break; sleep 0.01; done
[[ -e $work/retry-waiting && -e $work/state/alerts/unit-retry-concurrent.2030-01-02 ]] || fail 'retry did not wait while retaining its stamp'
run_sender unit-retry-concurrent diagnostic </dev/null || fail 'duplicate during a retry wait failed'
grep -qxF 'alert-telegram: suppressed unit-retry-concurrent' "$work/err" || fail 'duplicate during a retry wait was not suppressed'
[[ $(count_requests) == "$before" && $(count_attempts) == $((attempts_before + 1)) &&
  $(count_delays) == $((delays_before + 1)) ]] || fail 'duplicate during a retry wait attempted transport'
touch "$work/retry-release"
wait "$first" || fail 'retrying concurrent sender did not recover'
[[ $(count_requests) == $((before + 1)) && $(count_attempts) == $((attempts_before + 2)) &&
  $(count_delays) == $((delays_before + 1)) && -e $work/state/alerts/unit-retry-concurrent.2030-01-02 ]] || fail 'retrying concurrent sender did not send once and retain its stamp'
private_output

printf slow >"$work/scenario"
rm -f "$work/entered"
run_sender concurrent diagnostic </dev/null &
first=$!
for ((i=0; i<100; i++)); do [[ ! -e $work/entered ]] || break; sleep 0.01; done
[[ -e $work/entered ]] || fail 'concurrent sender did not enter the endpoint'
before=$(count_requests)
run_sender concurrent diagnostic </dev/null || fail 'concurrent duplicate delivery failed'
[[ $(count_requests) == "$before" ]] || fail 'concurrent duplicate sent another request'
wait "$first" || fail 'first concurrent delivery failed'
rm -f "$work/entered"
bash "$sender" interrupted diagnostic </dev/null >"$work/signal.out" 2>"$work/signal.err" &
interrupted=$!
for ((i=0; i<100; i++)); do [[ ! -e $work/entered ]] || break; sleep 0.01; done
[[ -e $work/entered ]] || fail 'interrupted sender did not enter the endpoint'
kill -TERM "$interrupted" || fail 'interrupted sender could not be signaled'
if wait "$interrupted"; then fail 'interrupted sender reported success'; fi
if compgen -G "$work/state/alerts/interrupted.*" >/dev/null; then fail 'interrupted sender retained its stamp'; fi

printf success >"$work/scenario"
bash "$transport" stage || fail 'SMTP staging failed'
if bash "$transport" confirm-receipt --received >"$work/cutover.out" 2>&1; then fail 'cutover without a test proof was accepted'; fi
[[ ! -e $work/state/alert-transport && ! -e $work/state/systemd/telegram.conf ]] || fail 'staging left Telegram selected'
find "$work/state/alerts" -type f -printf '%f\n' | sort >"$work/stamps-before"
run_sender --test </dev/null || fail 'first staged test delivery failed'
run_sender --test </dev/null || fail 'second staged test delivery failed'
find "$work/state/alerts" -type f -printf '%f\n' | sort >"$work/stamps-after"
cmp -s "$work/stamps-before" "$work/stamps-after" || fail 'test delivery changed daily stamps'
python3 -I - "$work/requests" "$delivery_test" <<'PY' || fail 'test delivery content validation failed'
import json, sys
for line in open(sys.argv[1], encoding="utf-8").readlines()[-2:]:
    assert json.loads(line)["body"]["text"] == sys.argv[2], "test delivery content changed"
PY
if bash "$transport" confirm-receipt >"$work/cutover.out" 2>&1; then fail 'cutover without explicit receipt was accepted'; fi
[[ ! -e $work/state/alert-transport ]] || fail 'missing receipt selected Telegram'
touch "$work/reload-fails"
if bash "$transport" confirm-receipt --received >"$work/cutover.out" 2>&1; then fail 'cutover with failed systemd reload was accepted'; fi
[[ ! -e $work/state/alert-transport && ! -e $work/state/systemd/telegram.conf ]] || fail 'failed reload left Telegram selected'
rm "$work/reload-fails"
run_sender --test </dev/null || fail 'test delivery after failed reload failed'
bash "$transport" confirm-receipt --received || fail 'receipt after failed reload was rejected'
grep -qxF telegram "$work/state/alert-transport" || fail 'receipt did not select Telegram'
grep -qxF 'ExecStart=/usr/local/lib/aboutme/alert-telegram.sh unit-%i "unit %i failed"' "$work/state/systemd/telegram.conf" || fail 'systemd did not select the Telegram sender'
cp "$repo/deploy/vn/host/bin/alert.sh" "$work/state/alert.sh"
cp "$sender" "$work/state/alert-telegram.sh"
cp "$repo/deploy/vn/host/telegram-state.sh" "$work/state/telegram-state.sh"
chmod +x "$work/state/alert-telegram.sh"
bash "$work/state/alert.sh" direct-caller diagnostic </dev/null >"$work/out" 2>"$work/err" || fail 'direct alert caller delivery failed'
grep -qxF 'alert-telegram: sent direct-caller' "$work/err" || fail 'direct alert caller did not report sent'
printf 500 >"$work/scenario"
if bash "$work/state/alert.sh" no-fallback diagnostic </dev/null >"$work/out" 2>"$work/err"; then fail 'direct caller hid a Telegram rejection'; fi
grep -qxF 'alert-telegram: rejected no-fallback' "$work/err" || fail 'direct caller rejection outcome missing'
[[ -e $work/state/alert-transport ]] || fail 'Telegram rejection cleared the selector'
printf success >"$work/scenario"
chmod 0600 "$work/state/secrets/telegram-chat-id"
printf '%s' '-1008888888888' >"$work/state/secrets/telegram-chat-id"
chmod 0400 "$work/state/secrets/telegram-chat-id"
if run_sender pending-rotation diagnostic </dev/null; then fail 'unapproved destination rotation was accepted'; fi
grep -qxF 'alert-telegram: invalid_secret pending-rotation' "$work/err" || fail 'unapproved rotation outcome missing'
run_sender --test </dev/null || fail 'rotated destination test delivery failed'
if run_sender pending-rotation diagnostic </dev/null; then fail 'destination test implicitly approved rotation'; fi
bash "$transport" confirm-receipt --received || fail 'rotated destination receipt failed'
run_sender pending-rotation diagnostic </dev/null || fail 'approved destination rotation failed'
grep -qxF 'alert-telegram: sent pending-rotation' "$work/err" || fail 'approved rotation did not report sent'
bash "$transport" stage || fail 'Telegram rollback failed'
[[ ! -e $work/state/alert-transport && ! -e $work/state/systemd/telegram.conf ]] || fail 'rollback left Telegram selected'
before=$(count_requests)
if run_sender rolled-back diagnostic </dev/null; then fail 'delivery after rollback was accepted'; fi
grep -qxF 'alert-telegram: invalid_secret rolled-back' "$work/err" || fail 'rollback refusal outcome missing'
[[ $(count_requests) == "$before" && ! -e $work/state/alerts/rolled-back.2030-01-02 ]] || fail 'rolled-back sender sent or created a stamp'
if bash "$transport" confirm-receipt --received >"$work/cutover.out" 2>&1; then fail 'rollback left a usable test proof'; fi
run_sender --test </dev/null || fail 'test delivery after rollback failed'
chmod 0600 "$work/state/secrets/telegram-chat-id"
printf '%s' '-1009999999999' >"$work/state/secrets/telegram-chat-id"
chmod 0400 "$work/state/secrets/telegram-chat-id"
if bash "$transport" confirm-receipt --received >"$work/cutover.out" 2>&1; then fail 'changed destination reused a stale test proof'; fi
run_sender --test </dev/null || fail 'replacement destination test delivery failed'
printf 500 >"$work/scenario"
if run_sender --test </dev/null; then fail 'failed test delivery was accepted'; fi
if bash "$transport" confirm-receipt --received >"$work/cutover.out" 2>&1; then fail 'failed test left a usable proof'; fi
[[ ! -e $work/state/alert-transport ]] || fail 'failed test selected Telegram'
[[ ! -e $work/clock-read-twice ]] || fail 'the sender read the clock more than once'
compgen -G "$work/clock-reads/*" >/dev/null || fail 'the clock-read counter saw no sender'
echo 'Telegram alert tests passed'
