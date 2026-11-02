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
mkdir -m 0700 "$work/state" "$work/state/secrets" "$work/bin"
printf '123456:synthetic_BOT-token' >"$work/state/secrets/telegram-bot-token"
printf '%s' '-1001234567890' >"$work/state/secrets/telegram-chat-id"
chmod 0400 "$work/state/secrets/"*
export ABOUTME_ALERT_TEST_ROOT=$work/state
export ABOUTME_ALERT_TEST_ENDPOINT=http://127.0.0.1:20983
export TG_FIXTURE=$work TG_REAL_CURL TG_REAL_STAT
TG_REAL_CURL=$(command -v curl) || fail 'curl is unavailable'
TG_REAL_STAT=$(command -v stat) || fail 'stat is unavailable'
cat >"$work/bin/stat" <<'STUB'
#!/usr/bin/env bash
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
case $(<"$TG_FIXTURE/scenario") in
  dns) cat >/dev/null; echo 'synthetic_BOT-token provider diagnostic' >&2; exit 6 ;;
  tls) cat >/dev/null; echo 'synthetic_BOT-token provider diagnostic' >&2; exit 60 ;;
  partial-success)
    cat >/dev/null
    while (($#)); do
      if [[ $1 == --output ]]; then printf '{"ok":true}' >"$2"; break; fi
      shift
    done
    printf 'http:200\n'
    exit 18 ;;
  timeout) exec "$TG_REAL_CURL" "$@" --max-time 0.2 ;;
  *) exec "$TG_REAL_CURL" "$@" ;;
esac
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
case $* in
  '-u +%F') printf '%s\n' "${TG_TEST_DAY:-2030-01-02}" ;;
  '-u +%FT%TZ') printf '%sT03:04:05Z\n' "${TG_TEST_DAY:-2030-01-02}" ;;
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
                  "malformed": b'not JSON synthetic_BOT-token',
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
private_output() {
  if grep -qE 'synthetic_BOT-token|-1001234567890|/bot123456:' "$work/out" "$work/err" "$work/argv"; then
    echo 'Telegram credential escaped its pipe' >&2; exit 1
  fi
}
if run_sender unconfirmed diagnostic </dev/null; then fail 'unconfirmed delivery was accepted'; fi
grep -qxF 'alert-telegram: invalid_secret unconfirmed' "$work/err" || fail 'unconfirmed delivery did not report invalid_secret'
[[ $(count_requests) == 0 && ! -e $work/state/alerts ]] || fail 'unconfirmed delivery sent or created stamps'
run_sender --test </dev/null || fail 'initial test delivery failed'
bash "$transport" confirm-receipt --received || fail 'initial receipt confirmation failed'
# A matching approval without its selector cannot authorize a cached caller.
mv "$work/state/alert-transport" "$work/selector"
if run_sender missing-selector diagnostic </dev/null; then fail 'delivery without a selector was accepted'; fi
grep -qxF 'alert-telegram: invalid_secret missing-selector' "$work/err" || fail 'missing selector did not report invalid_secret'
[[ $(count_requests) == 1 && ! -e $work/state/alerts ]] || fail 'missing selector sent or created stamps'
mv "$work/selector" "$work/state/alert-transport"
: >"$work/requests"
: >"$work/argv"
printf 'approved detail\n' | run_sender host-check 'host diagnostic' || fail 'approved host delivery failed'
grep -qxF 'alert-telegram: sent host-check' "$work/err" || fail 'host delivery did not report sent'
[[ $(count_requests) == 1 ]] || fail 'host delivery did not send exactly once'
run_sender host-check 'host diagnostic' </dev/null || fail 'duplicate host delivery failed'
grep -qxF 'alert-telegram: suppressed host-check' "$work/err" || fail 'duplicate host delivery did not report suppressed'
[[ $(count_requests) == 1 ]] || fail 'duplicate host delivery sent another request'
private_output
python3 -I - "$work/requests" <<'PY' || fail 'Telegram request validation failed'
import json, sys
r = json.loads(open(sys.argv[1]).readline())
assert r["path"] == "/bot123456:synthetic_BOT-token/sendMessage", "unexpected Telegram API path"
assert set(r["body"]) == {"chat_id", "text"}, "unexpected Telegram body fields"
assert r["body"]["chat_id"] == "-1001234567890", "unexpected Telegram destination"
t = r["body"]["text"]
assert "[aboutme-prod]" in t and "Key: host-check" in t and "Time (UTC):" in t, "alert metadata missing"
assert "approved detail" in t and "Host:" not in t and t.isascii() and len(t) <= 3000, "alert content bounds violated"
assert "2030-01-02T03:04:05Z" in t, "alert UTC timestamp missing"
PY
[[ $(stat -c %a "$work/state/alerts/host-check.2030-01-02") == 600 ]] || fail 'daily stamp was not private'
TG_TEST_DAY=2030-01-03 run_sender host-check diagnostic </dev/null || fail 'next-day delivery failed'
[[ $(count_requests) == 2 && -e $work/state/alerts/host-check.2030-01-03 ]] || fail 'next-day delivery or stamp missing'
: >"$work/state/alerts/smtp-stamp.2030-01-02"
chmod 0600 "$work/state/alerts/smtp-stamp.2030-01-02"
run_sender smtp-stamp diagnostic </dev/null || fail 'SMTP stamp suppression failed'
[[ $(count_requests) == 2 ]] || fail 'existing SMTP stamp allowed another request'
# Normal delivery creates no request or response files.
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
PY
for option in --connect-timeout --max-time --proto --proto-redir --max-redirs --retry --proxy --noproxy; do
  grep -qxF -- "$option" "$work/argv" || fail 'curl safety option missing'
done
if grep -qE -- '(^-L$|^--location|^--insecure$|^--verbose$|^--trace)' "$work/argv"; then fail 'unsafe curl option used'; fi

# Failed creation is an error, while only a private empty regular stamp dedups.
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

for mode in 201 400 401 403 429 500 503 301 302 307 308 false string array malformed multiple oversized dns tls timeout partial-success; do
  printf '%s' "$mode" >"$work/scenario"
  before=$(count_requests)
  if run_sender "failure-$mode" diagnostic </dev/null; then fail 'failed Telegram response was accepted'; fi
  if compgen -G "$work/state/alerts/failure-$mode.*" >/dev/null; then fail 'failed delivery retained a stamp'; fi
  case $mode in
    dns | tls | timeout | partial-success) grep -qxF "alert-telegram: transport_failed failure-$mode" "$work/err" || fail 'transport failure outcome missing' ;;
    oversized) grep -qxE "alert-telegram: (invalid_response|transport_failed) failure-$mode" "$work/err" || fail 'oversized response failure outcome missing' ;;
    [2345]*) grep -qxF "alert-telegram: rejected failure-$mode" "$work/err" || fail 'HTTP rejection outcome missing' ;;
    *) grep -qxF "alert-telegram: invalid_response failure-$mode" "$work/err" || fail 'invalid response outcome missing' ;;
  esac
  after=$(count_requests)
  ((after - before <= 1)) || { echo 'sender retried or followed a redirect' >&2; exit 1; }
  private_output
  printf success >"$work/scenario"
  run_sender "failure-$mode" diagnostic </dev/null || fail 'delivery after a failed response did not recover'
done
printf boundary >"$work/scenario"
run_sender reply-boundary diagnostic </dev/null || fail 'response at the size limit was rejected'

printf success >"$work/scenario"
python3 -I -c 'import sys; sys.stdout.buffer.write(b"a"*5000+b"\x00\t\xff")' |
  LC_ALL=C.UTF-8 run_sender coraza-log-invalid 'Coraza diagnostic' || fail 'oversized detail delivery failed'
python3 -I - "$work/requests" <<'PY' || fail 'detail truncation validation failed'
import json, sys
t = json.loads(open(sys.argv[1]).readlines()[-1])["body"]["text"]
assert t.isascii() and len(t) <= 3000 and "[detail truncated]" in t, "detail bounds or truncation notice missing"
notice = "\n[detail truncated]"
prefix = 2048 - len(notice)
assert "a" * prefix + notice in t and "a" * (prefix + 1) not in t, "detail truncation boundary changed"
assert "/run/aboutme/caddy-log/waf" in t and "journalctl -u aboutme-watch" in t, "Coraza guidance missing"
PY
printf 'line\t\000\377\nnext\r\n' | run_sender controls diagnostic || fail 'control-byte filtering delivery failed'
python3 -I - "$work/requests" <<'PY' || fail 'control-byte filtering validation failed'
import json, sys
t = json.loads(open(sys.argv[1]).readlines()[-1])["body"]["text"]
assert "line\nnext" in t and all(32 <= ord(c) <= 126 or c == "\n" for c in t), "control-byte filtering changed"
PY
for bad in '' $'bad\nsubject' $'bad\tname' $'bad\xc3\xa9' "$(printf '%0201d' 1)"; do
  if run_sender invalid-subject "$bad" </dev/null; then fail 'invalid subject was accepted'; fi
  grep -qxF 'alert-telegram: invalid_input' "$work/err" || fail 'invalid subject outcome missing'
done
printf -v longest_key '%0120d' 1
printf -v longest_subject '%0200d' 1
printf 'quotes: " backslash: \\\n' | run_sender "$longest_key" "$longest_subject" || fail 'maximum key and subject delivery failed'
python3 -I - "$work/requests" <<'PY' || fail 'JSON escaping validation failed'
import json, sys
t = json.loads(open(sys.argv[1]).readlines()[-1])["body"]["text"]
assert 'quotes: " backslash: \\' in t and len(t) <= 3000, "JSON escaping or message bound changed"
PY
if run_sender '../key' subject </dev/null; then fail 'invalid key was accepted'; fi
if ABOUTME_ALERT_TEST_ENDPOINT=https://example.test run_sender invalid-endpoint subject </dev/null; then
  fail 'invalid endpoint was accepted'
fi

# Unsafe files fail before transport, including values whose trailing newline
# would otherwise be hidden by command substitution.
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
# A foreign owner is refused without needing chown privileges in the fixture.
(
  # shellcheck source=../host/telegram-state.sh
  source "$repo/deploy/vn/host/telegram-state.sh"
  stat() { printf '0:400\n'; }
  if telegram_private_file "$work/state/secrets/telegram-bot-token" 400; then fail 'foreign-owned token was accepted'; fi
)

# Restoring valid bytes changes file identity and still requires a fresh receipt.
before=$(count_requests)
if run_sender restored-unapproved diagnostic </dev/null; then
  fail 'restored credentials reused stale approval'
fi
grep -qxF 'alert-telegram: invalid_secret restored-unapproved' "$work/err" || fail 'restored credentials did not report stale approval'
[[ $(count_requests) == "$before" && ! -e $work/state/alerts/restored-unapproved.2030-01-02 ]] || fail 'restored credentials sent or created a stamp before receipt'
run_sender --test </dev/null || fail 'restored credentials test delivery failed'
if run_sender restored-unapproved diagnostic </dev/null; then fail 'restored credentials test implicitly approved delivery'; fi
bash "$transport" confirm-receipt --received || fail 'restored credentials receipt confirmation failed'

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

# Staging never switches callers. A test is stamp-free and receipt is explicit.
printf success >"$work/scenario"
bash "$transport" stage || fail 'SMTP staging failed'
if bash "$transport" confirm-receipt --received >"$work/cutover.out" 2>&1; then fail 'cutover without a test proof was accepted'; fi
[[ ! -e $work/state/alert-transport && ! -e $work/state/systemd/telegram.conf ]] || fail 'staging left Telegram selected'
find "$work/state/alerts" -type f -printf '%f\n' | sort >"$work/stamps-before"
run_sender --test </dev/null || fail 'first staged test delivery failed'
run_sender --test </dev/null || fail 'second staged test delivery failed'
find "$work/state/alerts" -type f -printf '%f\n' | sort >"$work/stamps-after"
cmp -s "$work/stamps-before" "$work/stamps-after" || fail 'test delivery changed daily stamps'
python3 -I - "$work/requests" <<'PY' || fail 'test delivery content validation failed'
import json, sys
for line in open(sys.argv[1]).readlines()[-2:]:
    assert json.loads(line)["body"]["text"] == "[aboutme-prod] host alert delivery test", "test delivery content changed"
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
# Retention and vacuum still enter through alert.sh after activation.
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
# Replacing destination files cannot approve automatic delivery implicitly.
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
echo 'Telegram alert tests passed'
