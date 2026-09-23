#!/usr/bin/env bash
# State-directory and secret-file helpers for the native HTTPS harness.
# Sourced by scripts/dev-https.sh; defines functions and constants only.

pidfile() { printf '%s/%s.pid' "$RUN_DIR" "$1"; }
logfile() { printf '%s/%s.log' "$LOG_DIR" "$1"; }
identity_file() { printf '%s/%s.identity' "$RUN_DIR" "$1"; }
launch_file() { printf '%s/%s.launch' "$RUN_DIR" "$1"; }
service_env_file() { printf '%s/%s.env' "$RUN_DIR" "$1"; }

validate_existing_state_node() {
  local path=$1 kind=$2 mode owner canonical
  [ ! -L "$path" ] || die "state path is a symlink: $path"
  [ -e "$path" ] || die "state path disappeared during validation: $path"
  case $kind in
  directory) [ -d "$path" ] || die "state path is not a directory: $path" ;;
  regular) [ -f "$path" ] || die "state path is not a regular file: $path" ;;
  any)
    [ -d "$path" ] || [ -f "$path" ] || die "state path has an unsupported file type: $path"
    ;;
  *) die "invalid state path validation kind: $kind" ;;
  esac
  canonical=$(readlink -f -- "$path" 2>/dev/null) || die "state path cannot be resolved safely: $path"
  [ "$canonical" = "$path" ] || die "state path is noncanonical: $path"
  owner=$(stat -c '%u' -- "$path" 2>/dev/null) || die "state path ownership cannot be read: $path"
  [ "$owner" = "$EUID" ] || die "state path is not owned by uid $EUID: $path"
  mode=$(stat -c '%a' -- "$path" 2>/dev/null) || die "state path mode cannot be read: $path"
  [[ $mode =~ ^[0-7]{3,4}$ ]] || die "state path has an invalid mode: $path"
  (( (8#$mode & 8#022) == 0 )) || die "state path is group- or other-writable: $path"
}

validate_state_path_integrity() {
  local path unsafe
  if [ -e "$DEV_DIR" ] || [ -L "$DEV_DIR" ]; then
    validate_existing_state_node "$DEV_DIR" directory
  fi
  if [ -e "$STATE_DIR" ] || [ -L "$STATE_DIR" ]; then
    validate_existing_state_node "$STATE_DIR" directory
    [ "$(stat -c '%a' -- "$STATE_DIR")" = 700 ] || die "state path must have mode 700: $STATE_DIR"
  else
    return 0
  fi
  unsafe=$(find -P "$STATE_DIR" -xdev -mindepth 1 \
    \( -type l -o ! -uid "$EUID" -o -perm /022 \
    -o \( ! -type d -a ! -type f \) \) -print -quit 2>/dev/null) || \
    die "state path descendants cannot be inspected safely: $STATE_DIR"
  [ -z "$unsafe" ] || die "state path descendant is unsafe: $unsafe"
  unsafe=$(find -P "$STATE_DIR" -xdev -type f -links +1 -print -quit 2>/dev/null) || \
    die "state path file links cannot be inspected safely: $STATE_DIR"
  [ -z "$unsafe" ] || die "state path regular file has multiple hard links: $unsafe"
  unsafe=$(find -P "$STATE_DIR" -xdev -type d ! -path "$CADDY_DIR" \
    ! -perm 0700 -print -quit 2>/dev/null) || \
    die "state path directory modes cannot be inspected safely: $STATE_DIR"
  [ -z "$unsafe" ] || die "state path directory must have mode 700: $unsafe"
  if [ -d "$BIN_DIR" ]; then
    unsafe=$(find -P "$BIN_DIR" -xdev -type f ! -perm 0755 -print -quit 2>/dev/null) || \
      die "state path binary modes cannot be inspected safely: $BIN_DIR"
    [ -z "$unsafe" ] || die "state path binary must have mode 755: $unsafe"
  fi
  unsafe=$(find -P "$STATE_DIR" -xdev -path "$BIN_DIR" -prune -o \
    -type f ! -perm 0600 -print -quit 2>/dev/null) || \
    die "state path file modes cannot be inspected safely: $STATE_DIR"
  [ -z "$unsafe" ] || die "state path sensitive file must have mode 600: $unsafe"
  for path in "$RUN_DIR" "$LOG_DIR" "$BIN_DIR" "$MEDIA_DIR" "$INPUT_DIR" \
    "$CADDY_DIR/config" "$CADDY_DIR/data"; do
    if [ -e "$path" ] || [ -L "$path" ]; then
      validate_existing_state_node "$path" directory
      [ "$(stat -c '%a' -- "$path")" = 700 ] || die "state path must have mode 700: $path"
    fi
  done
  if [ -e "$CADDY_DIR" ] || [ -L "$CADDY_DIR" ]; then
    validate_existing_state_node "$CADDY_DIR" directory
    case $(stat -c '%a' -- "$CADDY_DIR") in
    700 | 755) ;;
    *) die "state path must have mode 700 (or legacy safe mode 755): $CADDY_DIR" ;;
    esac
  fi
}

prepare_state_directories() {
  local path
  if [ ! -e "$DEV_DIR" ]; then
    install -d -m 0755 -- "$DEV_DIR"
  fi
  validate_existing_state_node "$DEV_DIR" directory
  if [ ! -e "$STATE_DIR" ]; then
    install -d -m 0700 -- "$STATE_DIR"
  fi
  validate_existing_state_node "$STATE_DIR" directory
  for path in "$RUN_DIR" "$LOG_DIR" "$BIN_DIR" "$MEDIA_DIR" "$CADDY_DIR" \
    "$CADDY_DIR/config" "$CADDY_DIR/data" "$INPUT_DIR" "$SECRETS_DIR"; do
    if [ ! -e "$path" ]; then
      install -d -m 0700 -- "$path"
    fi
    validate_existing_state_node "$path" directory
    chmod 0700 -- "$path"
  done
  validate_state_path_integrity
}

# base64url_of encodes a raw 32-byte secret file as unpadded base64url, the
# exact form internal/config decodes. The value is returned, never logged.
base64url_of() {
  base64 -w0 -- "$1" | tr '+/' '-_' | tr -d '='
}

validate_secret_file() {
  local file=$1 mode size canonical owner
  [ ! -L "$file" ] || die "state secret is a symlink: $file"
  [ -f "$file" ] || die "state secret is not a regular file: $file"
  canonical=$(readlink -f -- "$file" 2>/dev/null) || die "state secret cannot be resolved: $file"
  [ "$canonical" = "$file" ] || die "state secret is noncanonical: $file"
  owner=$(stat -c '%u' -- "$file" 2>/dev/null) || die "state secret ownership cannot be read: $file"
  [ "$owner" = "$EUID" ] || die "state secret is not owned by uid $EUID: $file"
  mode=$(stat -c '%a' -- "$file" 2>/dev/null) || die "state secret mode cannot be read: $file"
  [ "$mode" = 600 ] || die "state secret must have mode 600: $file"
  size=$(stat -c '%s' -- "$file" 2>/dev/null) || die "state secret size cannot be read: $file"
  [ "$size" = 32 ] || die "state secret must be 32 bytes: $file"
}

# ensure_secrets creates the mode-0600 random rate/mail/capture secrets once,
# then reuses them across down/up. It never rotates implicitly and never prints
# a value.
ensure_secrets() {
  local name file
  mkdir -p -- "$SECRETS_DIR"
  chmod 0700 -- "$SECRETS_DIR"
  for name in password-rate-hmac-key auth-email-active-key auth-email-capture-bearer totp-key-a; do
    file="$SECRETS_DIR/$name"
    if [ ! -e "$file" ] && [ ! -L "$file" ]; then
      umask 077
      head -c 32 /dev/urandom >"$file" || die "could not generate state secret $file"
      chmod 0600 -- "$file"
    fi
    validate_secret_file "$file"
  done
  PASSWORD_RATE_HMAC_KEY_B64=$(base64url_of "$SECRETS_DIR/password-rate-hmac-key")
  AUTH_EMAIL_ACTIVE_KEY_B64=$(base64url_of "$SECRETS_DIR/auth-email-active-key")
  AUTH_EMAIL_CAPTURE_BEARER_B64=$(base64url_of "$SECRETS_DIR/auth-email-capture-bearer")
  # TOTP_ACTIVE_KEY takes the same 32-byte state secret and base64url shape as
  # the keys above (docs/design/totp-key-management.md "Key ring").
  TOTP_ACTIVE_KEY_B64=$(base64url_of "$SECRETS_DIR/totp-key-a")
}

# load_secrets sets the derived secret values from existing state without
# creating anything, so read-only commands (logs) never mutate state.
load_secrets() {
  PASSWORD_RATE_HMAC_KEY_B64=
  AUTH_EMAIL_ACTIVE_KEY_B64=
  AUTH_EMAIL_CAPTURE_BEARER_B64=
  TOTP_ACTIVE_KEY_B64=
  [ -f "$SECRETS_DIR/password-rate-hmac-key" ] || return 0
  PASSWORD_RATE_HMAC_KEY_B64=$(base64url_of "$SECRETS_DIR/password-rate-hmac-key")
  AUTH_EMAIL_ACTIVE_KEY_B64=$(base64url_of "$SECRETS_DIR/auth-email-active-key")
  AUTH_EMAIL_CAPTURE_BEARER_B64=$(base64url_of "$SECRETS_DIR/auth-email-capture-bearer")
  TOTP_ACTIVE_KEY_B64=$(base64url_of "$SECRETS_DIR/totp-key-a")
}

mail_secrets_sha256() {
  sha256sum "$SECRETS_DIR/password-rate-hmac-key" "$SECRETS_DIR/auth-email-active-key" "$SECRETS_DIR/auth-email-capture-bearer" "$SECRETS_DIR/totp-key-a" \
    | sha256sum | awk '{print $1}'
}
