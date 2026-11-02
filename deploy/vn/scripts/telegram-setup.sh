#!/usr/bin/env bash
# Pipe the owner's literal .env token and vMonitor destination to host secrets.
# Usage: DEPLOY_HOST=<host> bash deploy/vn/scripts/telegram-setup.sh [--replace]
set +xv
set -euo pipefail
export LC_ALL=C
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../../.." && pwd)
die() { echo 'telegram-setup: refused' >&2; exit 1; }
(($# == 0)) || [[ $# == 1 && $1 == --replace ]] || die
replace=("$@")
[[ -n ${DEPLOY_HOST:-} && -f $repo/.env && ! -L $repo/.env ]] || die

literal_token() {
  jq -Rrse '
    [split("\n")[] | select(test("^[[:blank:]]*(export[[:blank:]]+)?TELEGRAM_BOT="))
      | sub("^[[:blank:]]*(export[[:blank:]]+)?TELEGRAM_BOT="; "")
      | if (startswith("\"") and endswith("\"")) or
           (startswith("\u0027") and endswith("\u0027")) then .[1:-1] else . end]
    | if length == 1 and (.[0] | utf8bytelength <= 256 and test("\\A[0-9]+:[A-Za-z0-9_-]+\\z"))
      then .[0] else error("invalid") end' <"$repo/.env"
}

# Buffer only validated values in shell memory. A failed CLI/filter never
# reaches the importer, even if an upstream command emitted partial output.
unset token chat
token=$(literal_token 2>/dev/null) || die
chat=$(vngcloud monitor list-channels --output json 2>/dev/null |
  jq -jse '
    if length != 1 or (.[0].Items | type) != "array" then error("invalid") else .[0].Items end
    | [.[] | select(.Name == "aboutme-alerts")]
    | if length == 1 and .[0].Type == "Telegram" and (.[0].Address | type) == "string"
        and (.[0].Address | test("\\A-[0-9]{1,20}\\z"))
      then .[0].Address else error("invalid") end' 2>/dev/null) || die
printf '%s' "$token" | bash "$here/secrets.sh" import telegram-bot-token "${replace[@]}" >/dev/null 2>&1 || die
printf '%s' "$chat" | bash "$here/secrets.sh" import telegram-chat-id "${replace[@]}" >/dev/null 2>&1 || die
echo 'telegram-setup: imported' >&2
