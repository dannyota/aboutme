#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
set -euo pipefail

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
sender=$repo/deploy/vn/host/bin/alert-telegram.sh
transport=$repo/deploy/vn/host/alert-transport.sh
if [[ ${1-} == root-routing ]]; then
  ((EUID == 0)) || { echo 'root-routing requires a root runner' >&2; exit 1; }
  for test_root in '' /nonexistent/aboutme-telegram-test; do
    if output=$(ABOUTME_ALERT_TEST_ROOT=$test_root ABOUTME_ALERT_TEST_ENDPOINT=http://127.0.0.1:20983 \
      bash "$sender" --test 2>&1); then exit 1; fi
    [[ $output == 'alert-telegram: invalid_secret' ]]
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
export TG_FIXTURE=$work TG_REAL_CURL
TG_REAL_CURL=$(command -v curl)
cat >"$work/bin/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >>"$TG_FIXTURE/argv"
if env | grep -E 'synthetic_BOT-token|-1001234567890' >/dev/null; then exit 1; fi
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
[[ $* == daemon-reload ]] || exit 1
printf 'reload\n' >>"$TG_FIXTURE/reloads"
[[ ! -e $TG_FIXTURE/reload-fails ]]
STUB
cat >"$work/bin/date" <<'STUB'
#!/usr/bin/env bash
case $* in
  '-u +%F') printf '%s\n' "${TG_TEST_DAY:-2030-01-02}" ;;
  '-u +%FT%TZ') printf '%sT03:04:05Z\n' "${TG_TEST_DAY:-2030-01-02}" ;;
  *) exit 1 ;;
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
printf 'approved detail\n' | run_sender host-check 'host diagnostic'
grep -qxF 'alert-telegram: sent host-check' "$work/err"
[[ $(count_requests) == 1 ]]
run_sender host-check 'host diagnostic' </dev/null
grep -qxF 'alert-telegram: suppressed host-check' "$work/err"
[[ $(count_requests) == 1 ]]
private_output
python3 -I - "$work/requests" <<'PY'
import json, sys
r = json.loads(open(sys.argv[1]).readline())
assert r["path"] == "/bot123456:synthetic_BOT-token/sendMessage"
assert set(r["body"]) == {"chat_id", "text"}
assert r["body"]["chat_id"] == "-1001234567890"
t = r["body"]["text"]
assert "[aboutme-prod]" in t and "Key: host-check" in t and "Time (UTC):" in t
assert "approved detail" in t and "Host:" not in t and t.isascii() and len(t) <= 3000
assert "2030-01-02T03:04:05Z" in t
PY
[[ $(stat -c %a "$work/state/alerts/host-check.2030-01-02") == 600 ]]
TG_TEST_DAY=2030-01-03 run_sender host-check diagnostic </dev/null
[[ $(count_requests) == 2 && -e $work/state/alerts/host-check.2030-01-03 ]]
: >"$work/state/alerts/smtp-stamp.2030-01-02"
run_sender smtp-stamp diagnostic </dev/null
[[ $(count_requests) == 2 ]]
# Only credentials, the lock, and daily stamps exist after normal delivery.
[[ -z $(find "$work/state" -type f ! -path '*/secrets/*' ! -path '*/alerts/*' ! -name '.telegram.lock' -print) ]]
grep -qxF -- '-q' "$work/argv"
python3 -I - "$work/argv" <<'PY'
import sys
args = open(sys.argv[1]).read().splitlines()
assert args[0] == "-q"
for flag, value in {"--connect-timeout": "5", "--max-time": "15",
                    "--max-redirs": "0", "--retry": "0", "--proxy": "",
                    "--noproxy": "*"}.items():
    assert args[args.index(flag) + 1] == value
PY
for option in --connect-timeout --max-time --proto --proto-redir --max-redirs --retry --proxy --noproxy; do
  grep -qxF -- "$option" "$work/argv"
done
if grep -qE -- '(^-L$|^--location|^--insecure$|^--verbose$|^--trace)' "$work/argv"; then exit 1; fi

for mode in 201 400 401 403 429 500 503 301 302 307 308 false string array malformed multiple oversized dns tls timeout partial-success; do
  printf '%s' "$mode" >"$work/scenario"
  before=$(count_requests)
  if run_sender "failure-$mode" diagnostic </dev/null; then echo "accepted $mode" >&2; exit 1; fi
  if compgen -G "$work/state/alerts/failure-$mode.*" >/dev/null; then exit 1; fi
  case $mode in
    dns | tls | timeout | partial-success) grep -qxF "alert-telegram: transport_failed failure-$mode" "$work/err" ;;
    oversized) grep -qxE "alert-telegram: (invalid_response|transport_failed) failure-$mode" "$work/err" ;;
    [2345]*) grep -qxF "alert-telegram: rejected failure-$mode" "$work/err" ;;
    *) grep -qxF "alert-telegram: invalid_response failure-$mode" "$work/err" ;;
  esac
  after=$(count_requests)
  ((after - before <= 1)) || { echo 'sender retried or followed a redirect' >&2; exit 1; }
  private_output
  printf success >"$work/scenario"
  run_sender "failure-$mode" diagnostic </dev/null
done
printf boundary >"$work/scenario"
run_sender reply-boundary diagnostic </dev/null

printf success >"$work/scenario"
python3 -I -c 'import sys; sys.stdout.buffer.write(b"a"*5000+b"\x00\t\xff")' |
  LC_ALL=C.UTF-8 run_sender coraza-log-invalid 'Coraza diagnostic'
python3 -I - "$work/requests" <<'PY'
import json, sys
t = json.loads(open(sys.argv[1]).readlines()[-1])["body"]["text"]
assert t.isascii() and len(t) <= 3000 and "[detail truncated]" in t
notice = "\n[detail truncated]"
prefix = 2048 - len(notice)
assert "a" * prefix + notice in t and "a" * (prefix + 1) not in t
assert "/run/aboutme/caddy-log/waf" in t and "journalctl -u aboutme-watch" in t
PY
printf 'line\t\000\377\nnext\r\n' | run_sender controls diagnostic
python3 -I - "$work/requests" <<'PY'
import json, sys
t = json.loads(open(sys.argv[1]).readlines()[-1])["body"]["text"]
assert "line\nnext" in t and all(32 <= ord(c) <= 126 or c == "\n" for c in t)
PY
for bad in '' $'bad\nsubject' $'bad\tname' $'bad\xc3\xa9' "$(printf '%0201d' 1)"; do
  if run_sender invalid-subject "$bad" </dev/null; then exit 1; fi
  grep -qxF 'alert-telegram: invalid_input' "$work/err"
done
printf -v longest_key '%0120d' 1
printf -v longest_subject '%0200d' 1
printf 'quotes: " backslash: \\\n' | run_sender "$longest_key" "$longest_subject"
python3 -I - "$work/requests" <<'PY'
import json, sys
t = json.loads(open(sys.argv[1]).readlines()[-1])["body"]["text"]
assert 'quotes: " backslash: \\' in t and len(t) <= 3000
PY
if run_sender '../key' subject </dev/null; then exit 1; fi
ABOUTME_ALERT_TEST_ENDPOINT=https://example.test run_sender invalid-endpoint subject </dev/null && exit 1

# Unsafe files fail before transport, including values whose trailing newline
# would otherwise be hidden by command substitution.
cp "$work/state/secrets/telegram-bot-token" "$work/token-copy"
for bad in '' 'bad token' $'123:abc\n' "$(printf '%0257d' 1):abc"; do
  chmod 0600 "$work/state/secrets/telegram-bot-token"
  printf '%s' "$bad" >"$work/state/secrets/telegram-bot-token"
  chmod 0400 "$work/state/secrets/telegram-bot-token"
  if run_sender unsafe-secret diagnostic </dev/null; then exit 1; fi
  grep -qxF 'alert-telegram: invalid_secret unsafe-secret' "$work/err"
done
chmod 0600 "$work/state/secrets/telegram-bot-token"
printf '123:ab\000c' >"$work/state/secrets/telegram-bot-token"
chmod 0400 "$work/state/secrets/telegram-bot-token"
if run_sender unsafe-nul diagnostic </dev/null; then exit 1; fi
chmod 0600 "$work/state/secrets/telegram-bot-token"
cp "$work/token-copy" "$work/state/secrets/telegram-bot-token"
if run_sender unsafe-mode diagnostic </dev/null; then exit 1; fi
chmod 0400 "$work/state/secrets/telegram-bot-token"
mv "$work/state/secrets/telegram-bot-token" "$work/state/secrets/token-real"
ln -s token-real "$work/state/secrets/telegram-bot-token"
if run_sender unsafe-link diagnostic </dev/null; then exit 1; fi
rm "$work/state/secrets/telegram-bot-token"
mv "$work/state/secrets/token-real" "$work/state/secrets/telegram-bot-token"
chmod 0755 "$work/state/secrets"
if run_sender unsafe-directory diagnostic </dev/null; then exit 1; fi
chmod 0700 "$work/state/secrets"
cp "$work/state/secrets/telegram-chat-id" "$work/chat-copy"
for bad in '' 'not-a-chat' $'-100123\n' '-123456789012345678901'; do
  chmod 0600 "$work/state/secrets/telegram-chat-id"
  printf '%s' "$bad" >"$work/state/secrets/telegram-chat-id"
  chmod 0400 "$work/state/secrets/telegram-chat-id"
  if run_sender unsafe-chat diagnostic </dev/null; then exit 1; fi
done
chmod 0600 "$work/state/secrets/telegram-chat-id"
cp "$work/chat-copy" "$work/state/secrets/telegram-chat-id"
chmod 0400 "$work/state/secrets/telegram-chat-id"
# A foreign owner is refused without needing chown privileges in the fixture.
(
  # shellcheck source=../host/telegram-state.sh
  source "$repo/deploy/vn/host/telegram-state.sh"
  stat() { printf '0:400\n'; }
  if telegram_private_file "$work/state/secrets/telegram-bot-token" 400; then exit 1; fi
)

printf slow >"$work/scenario"
rm -f "$work/entered"
run_sender concurrent diagnostic </dev/null &
first=$!
for ((i=0; i<100; i++)); do [[ ! -e $work/entered ]] || break; sleep 0.01; done
[[ -e $work/entered ]]
before=$(count_requests)
run_sender concurrent diagnostic </dev/null
[[ $(count_requests) == "$before" ]]
wait "$first"
rm -f "$work/entered"
bash "$sender" interrupted diagnostic </dev/null >"$work/signal.out" 2>"$work/signal.err" &
interrupted=$!
for ((i=0; i<100; i++)); do [[ ! -e $work/entered ]] || break; sleep 0.01; done
kill -TERM "$interrupted"
if wait "$interrupted"; then exit 1; fi
if compgen -G "$work/state/alerts/interrupted.*" >/dev/null; then exit 1; fi

# Staging never switches callers. A test is stamp-free and receipt is explicit.
printf success >"$work/scenario"
bash "$transport" stage
if bash "$transport" confirm-receipt --received >"$work/cutover.out" 2>&1; then exit 1; fi
[[ ! -e $work/state/alert-transport && ! -e $work/state/systemd/telegram.conf ]]
find "$work/state/alerts" -type f -printf '%f\n' | sort >"$work/stamps-before"
run_sender --test </dev/null
run_sender --test </dev/null
find "$work/state/alerts" -type f -printf '%f\n' | sort >"$work/stamps-after"
cmp "$work/stamps-before" "$work/stamps-after"
python3 -I - "$work/requests" <<'PY'
import json, sys
for line in open(sys.argv[1]).readlines()[-2:]:
    assert json.loads(line)["body"]["text"] == "[aboutme-prod] host alert delivery test"
PY
if bash "$transport" confirm-receipt >"$work/cutover.out" 2>&1; then exit 1; fi
[[ ! -e $work/state/alert-transport ]]
touch "$work/reload-fails"
if bash "$transport" confirm-receipt --received >"$work/cutover.out" 2>&1; then exit 1; fi
[[ ! -e $work/state/alert-transport && ! -e $work/state/systemd/telegram.conf ]]
rm "$work/reload-fails"
run_sender --test </dev/null
bash "$transport" confirm-receipt --received
grep -qxF telegram "$work/state/alert-transport"
grep -qxF 'ExecStart=/usr/local/lib/aboutme/alert-telegram.sh unit-%i "unit %i failed"' "$work/state/systemd/telegram.conf"
# Retention and vacuum still enter through alert.sh after activation.
cp "$repo/deploy/vn/host/bin/alert.sh" "$work/state/alert.sh"
cp "$sender" "$work/state/alert-telegram.sh"
cp "$repo/deploy/vn/host/telegram-state.sh" "$work/state/telegram-state.sh"
chmod +x "$work/state/alert-telegram.sh"
bash "$work/state/alert.sh" direct-caller diagnostic </dev/null >"$work/out" 2>"$work/err"
grep -qxF 'alert-telegram: sent direct-caller' "$work/err"
printf 500 >"$work/scenario"
if bash "$work/state/alert.sh" no-fallback diagnostic </dev/null >"$work/out" 2>"$work/err"; then exit 1; fi
grep -qxF 'alert-telegram: rejected no-fallback' "$work/err"
[[ -e $work/state/alert-transport ]]
printf success >"$work/scenario"
# Replacing destination files cannot approve automatic delivery implicitly.
chmod 0600 "$work/state/secrets/telegram-chat-id"
printf '%s' '-1008888888888' >"$work/state/secrets/telegram-chat-id"
chmod 0400 "$work/state/secrets/telegram-chat-id"
if run_sender pending-rotation diagnostic </dev/null; then exit 1; fi
grep -qxF 'alert-telegram: invalid_secret pending-rotation' "$work/err"
run_sender --test </dev/null
if run_sender pending-rotation diagnostic </dev/null; then exit 1; fi
bash "$transport" confirm-receipt --received
run_sender pending-rotation diagnostic </dev/null
grep -qxF 'alert-telegram: sent pending-rotation' "$work/err"
bash "$transport" stage
[[ ! -e $work/state/alert-transport && ! -e $work/state/systemd/telegram.conf ]]
if bash "$transport" confirm-receipt --received >"$work/cutover.out" 2>&1; then exit 1; fi
run_sender --test </dev/null
chmod 0600 "$work/state/secrets/telegram-chat-id"
printf '%s' '-1009999999999' >"$work/state/secrets/telegram-chat-id"
chmod 0400 "$work/state/secrets/telegram-chat-id"
if bash "$transport" confirm-receipt --received >"$work/cutover.out" 2>&1; then exit 1; fi
run_sender --test </dev/null
printf 500 >"$work/scenario"
if run_sender --test </dev/null; then exit 1; fi
if bash "$transport" confirm-receipt --received >"$work/cutover.out" 2>&1; then exit 1; fi
[[ ! -e $work/state/alert-transport ]]
echo 'Telegram alert tests passed'
