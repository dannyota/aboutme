#!/usr/bin/env bash
set -euo pipefail
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
((EUID != 0)) || { echo 'Telegram fixtures require a non-root user' >&2; exit 1; }
umask 077
mkdir -p "$work/repo/deploy/vn/scripts" "$work/bin" "$work/lib" "$work/state/secrets"
cp "$repo/deploy/vn/scripts/telegram-setup.sh" "$repo/deploy/vn/scripts/secrets.sh" \
  "$repo/deploy/vn/scripts/ssh-lib.sh" "$work/repo/deploy/vn/scripts/"
chmod 0700 "$work/state" "$work/state/secrets"
cp "$repo/deploy/vn/host/telegram-state.sh" "$work/lib/"
export TG_SETUP_FIXTURE=$work DEPLOY_HOST=host.invalid ABOUTME_ALERT_TEST_ROOT=$work/state
cat >"$work/bin/ssh" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$TG_SETUP_FIXTURE/argv"
if env | grep -E 'synthetic_BOT-token|-1001234567890' >/dev/null; then exit 1; fi
case $* in
  *'import telegram-bot-token'*) cat >"$TG_SETUP_FIXTURE/token" ;;
  *'import telegram-chat-id'*) cat >"$TG_SETUP_FIXTURE/chat" ;;
  *) exit 1 ;;
esac
exit "${TG_SETUP_SSH_RESULT:-0}"
STUB
cat >"$work/bin/vngcloud" <<'STUB'
#!/usr/bin/env bash
[[ $* == 'monitor list-channels --output json' ]] || exit 1
cat "$TG_SETUP_FIXTURE/channels"
exit "${TG_SETUP_CLI_RESULT:-0}"
STUB
chmod +x "$work/bin/"*
export PATH=$work/bin:$PATH
setup=$work/repo/deploy/vn/scripts/telegram-setup.sh
printf 'UNRELATED=$(touch %s/unexpected)\nTELEGRAM_BOT="123456:synthetic_BOT-token"\n' "$work" >"$work/repo/.env"
printf '%s' '{"Items":[{"Name":"aboutme-alerts","Type":"Telegram","Address":"-1001234567890"}]}' >"$work/channels"
cp "$work/channels" "$work/valid-channels"
cp "$work/repo/.env" "$work/valid-env"
bash "$setup" >"$work/out" 2>"$work/err"
[[ $(<"$work/token") == '123456:synthetic_BOT-token' && $(<"$work/chat") == '-1001234567890' ]]
[[ $(wc -c <"$work/token") == 26 && $(wc -c <"$work/chat") == 14 ]]
[[ ! -e $work/unexpected ]]
[[ $(wc -l <"$work/argv") == 2 ]]
if grep -qE 'synthetic_BOT-token|-1001234567890' "$work/argv" "$work/out" "$work/err"; then exit 1; fi
bash "$setup" --replace >"$work/out" 2>"$work/err"
[[ $(grep -c -- '--replace' "$work/argv") == 2 ]]

expect_setup_rejected() {
  : >"$work/argv"
  if bash "$setup" >"$work/out" 2>"$work/err"; then echo 'unsafe setup accepted' >&2; exit 1; fi
  [[ ! -s $work/argv ]]
  grep -qxF 'telegram-setup: refused' "$work/err"
}
for channels in '{"Items":[]}' \
  '{"Items":[{"Name":"aboutme-alerts","Type":"Email","Address":"-1001234567890"}]}' \
  '{"Items":[{"Name":"aboutme-alerts","Type":"Telegram","Address":-1001234567890}]}' \
  '{"Items":[{"Name":"aboutme-alerts","Type":"Telegram","Address":"123456:token"}]}' \
  '{"Items":[{"Name":"aboutme-alerts","Type":"Telegram","Address":"-1001234567890\n"}]}' \
  '{"Items":[{"Name":"aboutme-alerts","Type":"Telegram","Address":"-1001234567890"},{"Name":"aboutme-alerts","Type":"Telegram","Address":"-1001234567890"}]}' \
  'not JSON'; do
  printf '%s' "$channels" >"$work/channels"
  expect_setup_rejected
done
cp "$work/valid-channels" "$work/channels"
TG_SETUP_CLI_RESULT=1 expect_setup_rejected
for token_line in '' 'TELEGRAM_BOT=' 'TELEGRAM_BOT=$(touch unexpected)' \
  'TELEGRAM_BOT="123456:has space"' $'TELEGRAM_BOT=123456:abc\nTELEGRAM_BOT=123456:def'; do
  printf '%s\n' "$token_line" >"$work/repo/.env"
  expect_setup_rejected
done
printf 'TELEGRAM_BOT=123456:ab\000c\n' >"$work/repo/.env"
expect_setup_rejected
cp "$work/valid-env" "$work/repo/.env"
if TG_SETUP_SSH_RESULT=1 bash "$setup" >"$work/out" 2>"$work/err"; then exit 1; fi

# Source only host import functions into an installed-layout fixture. Actual
# root admission remains in secrets-host.sh; these tests use synthetic files.
sed -n '/^imported=(/,/^  smtp-username/p;/^replaceable=(/,/^  telegram-bot-token/p;/^podman_names=(/,/^  media-secret-access-key/p;/^member() {/,/^}/p;/^cmd_import() {/,/^}/p;/^store_telegram() {/,/^}/p;/^cmd_podman_sync() {/,/^}/p;/^cmd_age_copy() {/,/^}/p' \
  "$repo/deploy/vn/scripts/secrets-host.sh" >"$work/lib/imports.sh"
# shellcheck disable=SC1091
source "$work/lib/imports.sh"
dir=$work/state/secrets
say() { printf '%s\n' "$*"; }
die() { printf '%s\n' "$*" >&2; exit 1; }
podman() { echo 'unexpected Podman call' >&2; exit 1; }
printf '123456:synthetic_BOT-token' | (cmd_import telegram-bot-token) >"$work/out" 2>"$work/err"
printf '%s' '-1001234567890' | (cmd_import telegram-chat-id) >"$work/out" 2>"$work/err"
[[ $(stat -c %a "$dir/telegram-bot-token") == 400 && $(stat -c %a "$dir/telegram-chat-id") == 400 ]]
printf proof >"$work/state/telegram-test-passed"
printf '123456:rotated_synthetic-token' | (cmd_import telegram-bot-token --replace) >"$work/out" 2>"$work/err"
[[ ! -e $work/state/telegram-test-passed && $(<"$dir/telegram-bot-token") == '123456:rotated_synthetic-token' ]]
for name in telegram-bot-token telegram-chat-id; do
  if (cmd_podman_sync --replace "$name") >"$work/out" 2>"$work/err"; then exit 1; fi
done
(cmd_podman_sync) >"$work/out" 2>"$work/err"
printf telegram >"$work/state/alert-transport"
printf approved >"$work/state/telegram-approved"
printf '123456:unconfirmed-token' | (cmd_import telegram-bot-token --replace) >"$work/out" 2>"$work/err"
[[ -e $work/state/alert-transport && ! -e $work/state/telegram-approved ]]
[[ $(<"$dir/telegram-bot-token") == '123456:unconfirmed-token' ]]
rm "$work/state/alert-transport"
for value in '' '123456:has space' $'123456:abc\n'; do
  if printf '%s' "$value" | (cmd_import telegram-bot-token --replace) >"$work/out" 2>"$work/err"; then exit 1; fi
done
if printf '123456:ab\000c' | (cmd_import telegram-bot-token --replace) >"$work/out" 2>"$work/err"; then exit 1; fi
[[ $(<"$dir/telegram-bot-token") == '123456:unconfirmed-token' ]]
[[ -z $(find "$dir" -name '.new.*' -print) ]]
rm "$dir/telegram-chat-id"
printf '%s' '-1001234567890' | (cmd_import telegram-chat-id --replace) >"$work/out" 2>"$work/err"
[[ $(stat -c %a "$dir/telegram-chat-id") == 400 ]]
pgb_conf=$work/pgbackrest.conf
printf 'synthetic pgBackRest config' >"$pgb_conf"
age() {
  [[ $1 == -r && $2 == "$recipient" ]] || return 1
  tar -tf - >"$work/archived-names"
  printf 'synthetic ciphertext'
}
printf -v recipient 'age1%058d' 0
cmd_age_copy "$recipient" >"$work/ciphertext"
[[ $(<"$work/ciphertext") == 'synthetic ciphertext' ]]
grep -q '/telegram-bot-token$' "$work/archived-names"
grep -q '/telegram-chat-id$' "$work/archived-names"
grep -qF '"$bundle/host/telegram-state.sh"' "$repo/deploy/vn/host/install.sh"
grep -qF '"$bundle/host/alert-transport.sh"' "$repo/deploy/vn/host/install.sh"
grep -qxF 'ExecStart=/usr/local/lib/aboutme/alert.sh unit-%i "unit %i failed"' \
  "$repo/deploy/vn/host/etc/systemd/system/aboutme-alert@.service"
echo 'Telegram setup tests passed'
