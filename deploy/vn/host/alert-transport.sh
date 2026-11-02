#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
# Explicit SMTP staging and owner-confirmed Telegram activation.
set +xv
set -euo pipefail
export LC_ALL=C
umask 077
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=telegram-state.sh
source "$here/telegram-state.sh"
die() { echo 'alert-transport: refused' >&2; exit 1; }
telegram_init exclusive 2>/dev/null || die

smtp() {
  [[ ! -L $telegram_active && ! -L $telegram_dropin ]] || return 1
  rm -f -- "$telegram_active" "$telegram_dropin" "$telegram_proof" "$telegram_approved" || return 1
  systemctl daemon-reload >/dev/null 2>&1
}

approve() {
  local approved
  approved=$(mktemp "$telegram_base/.telegram-approved.XXXXXX") || return 1
  if ! printf '%s\n' "$identity" >"$approved" || ! chmod 0400 "$approved" ||
    ! mv -fT -- "$approved" "$telegram_approved"; then
    rm -f -- "$approved"
    return 1
  fi
  rm -f -- "$telegram_proof" || return 1
}

activating=no
trap 'if [[ $activating == yes ]]; then smtp || true; fi' EXIT
trap 'exit 1' HUP INT TERM

case "${1-}:$#" in
  stage:1)
    smtp || die
    echo 'alert-transport: smtp' >&2
    ;;
  confirm-receipt:2)
    [[ $2 == --received ]] || die
    telegram_private_file "$telegram_proof" 400 || die
    identity=$(telegram_identity) || die
    [[ $(<"$telegram_proof") == "$identity" ]] || die
    [[ ! -L $telegram_active && ! -L $telegram_dropin && ! -L $telegram_approved ]] || die
    # Rotation keeps Telegram selected. Automatic sends remain refused until
    # this receipt approves the replacement credentials; SMTP is not a fallback.
    if [[ -e $telegram_active ]]; then
      telegram_private_file "$telegram_active" 600 && [[ $(<"$telegram_active") == telegram ]] || die
      approve || die
      echo 'alert-transport: telegram' >&2
      exit 0
    fi
    dropin_dir=${telegram_dropin%/*}
    [[ ! -L $dropin_dir ]] || die
    install -d -m 0755 "$dropin_dir"
    [[ $(stat -c '%u:%a' "$dropin_dir") == "$EUID:755" ]] || die
    activating=yes
    cat >"$telegram_dropin" <<'UNIT'
[Service]
ExecStart=
ExecStart=/usr/local/lib/aboutme/alert-telegram.sh unit-%i "unit %i failed"
UNIT
    chmod 0644 "$telegram_dropin"
    systemctl daemon-reload >/dev/null 2>&1 || die
    approve || die
    printf 'telegram\n' >"$telegram_active"
    chmod 0600 "$telegram_active"
    activating=no
    echo 'alert-transport: telegram' >&2
    ;;
  *) die ;;
esac
