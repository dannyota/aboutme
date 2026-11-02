# shellcheck shell=bash
# shellcheck disable=SC2034
# Shared host paths and admission for Telegram delivery, imports, and cutover.
# The lock keeps credential replacement outside sends and receipt confirmation.

telegram_private_file() { # path mode
  [[ -f $1 && ! -L $1 && $(stat -c '%u:%a' -- "$1" 2>/dev/null) == "$EUID:$2" ]]
}

telegram_init() { # shared|exclusive
  if [[ -n ${ABOUTME_ALERT_TEST_ROOT:-} ]]; then
    ((EUID != 0)) || return 1
    telegram_base=$ABOUTME_ALERT_TEST_ROOT
    telegram_stamps=$telegram_base/alerts
    telegram_dropin=$telegram_base/systemd/telegram.conf
  else
    ((EUID == 0)) || return 1
    [[ -z ${ABOUTME_ALERT_TEST_ENDPOINT:-} ]] || return 1
    telegram_base=/etc/aboutme
    telegram_stamps=/var/lib/aboutme/alerts
    telegram_dropin=/etc/systemd/system/aboutme-alert@.service.d/telegram.conf
  fi
  telegram_secrets=$telegram_base/secrets
  telegram_proof=$telegram_base/telegram-test-passed
  telegram_approved=$telegram_base/telegram-approved
  telegram_active=$telegram_base/alert-transport
  [[ -d $telegram_base && ! -L $telegram_base &&
    $(stat -c %u -- "$telegram_base" 2>/dev/null) == "$EUID" ]] || return 1
  local mode
  mode=$(stat -c %a -- "$telegram_base") || return 1
  [[ $mode == 755 || $mode == 700 ]] || return 1
  [[ -d $telegram_secrets && ! -L $telegram_secrets &&
    $(stat -c '%u:%a' -- "$telegram_secrets" 2>/dev/null) == "$EUID:700" ]] || return 1
  [[ ! -L $telegram_base/.telegram.lock ]] || return 1
  if [[ ! -e $telegram_base/.telegram.lock ]]; then
    (umask 077; set -o noclobber; : >"$telegram_base/.telegram.lock") 2>/dev/null || true
  fi
  telegram_private_file "$telegram_base/.telegram.lock" 600 || return 1
  exec {telegram_lock_fd}<>"$telegram_base/.telegram.lock" || return 1
  case $1 in
    shared) flock -s -w 20 "$telegram_lock_fd" ;;
    exclusive) flock -x -w 20 "$telegram_lock_fd" ;;
    *) return 1 ;;
  esac
}

telegram_secret_valid() { # name value byte-size
  [[ ${#2} == "$3" ]] || return 1
  case $1 in
    telegram-bot-token) ((${#2} <= 256)) && [[ $2 =~ ^[0-9]+:[A-Za-z0-9_-]+$ ]] ;;
    telegram-chat-id) [[ $2 =~ ^-?[0-9]{1,20}$ ]] ;;
    *) return 1 ;;
  esac
}

telegram_credentials() {
  local name size value
  export -n value
  for name in telegram-bot-token telegram-chat-id; do
    telegram_private_file "$telegram_secrets/$name" 400 || return 1
    size=$(stat -c %s -- "$telegram_secrets/$name") || return 1
    ((size > 0 && size <= 256)) || return 1
    value=$(<"$telegram_secrets/$name")
    telegram_secret_valid "$name" "$value" "$size" || return 1
    case $name in
      telegram-bot-token) telegram_token=$value ;;
      telegram-chat-id) telegram_chat=$value ;;
    esac
  done
  # Inherited environment names must not turn these shell variables into exports.
  export -n telegram_token telegram_chat
}

telegram_identity() {
  local name
  for name in telegram-bot-token telegram-chat-id; do
    telegram_private_file "$telegram_secrets/$name" 400 || return 1
    stat -c '%d:%i:%s:%y:%z' -- "$telegram_secrets/$name" || return 1
  done
}
