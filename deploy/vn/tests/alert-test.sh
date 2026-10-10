#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
alert=$repo_root/deploy/vn/host/bin/alert.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"

cat >"$work/bin/msmtp" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%q\n' "$@" >"$ALERT_TEST_CAPTURE.args"
while (($#)); do
  if [[ $1 == -C ]]; then
    cp "$2" "$ALERT_TEST_CAPTURE.config"
    stat -c %a "$2" >"$ALERT_TEST_CAPTURE.mode"
    shift 2
  else
    shift
  fi
done
cat >"$ALERT_TEST_CAPTURE.message"
: >"$ALERT_TEST_CAPTURE.called"
STUB
chmod +x "$work/bin/msmtp"
export PATH=$work/bin:$PATH

write_env() { # root sender-name [extra-line]
  local root=$1 name=$2 extra=${3-}
  mkdir -p "$root/secrets"
  {
    printf 'SMTP_HOST=smtp.example.test\n'
    printf 'SMTP_PORT=465\n'
    printf 'SMTP_TLS=implicit\n'
    printf 'SES_FROM_ADDRESS=no-reply@example.test\n'
    printf 'SES_FROM_NAME=%s\n' "$name"
    printf 'ALERT_TO=support@example.test\n'
    [[ -z $extra ]] || printf '%s\n' "$extra"
  } >"$root/smtp.env"
  printf 'password' >"$root/secrets/smtp-password"
}

run_alert() { # root capture
  local root=$1 capture=$2
  ABOUTME_ALERT_TEST_ROOT=$root ALERT_TEST_CAPTURE=$capture \
    "$alert" test-key test-subject </dev/null >"$capture.out" 2>"$capture.err"
}

valid=$work/valid
capture=$work/valid-capture
write_env "$valid" 'Danny from aboutme.vn'
printf '%s' ' lead\" user ' >"$valid/secrets/smtp-username"
run_alert "$valid" "$capture"
[[ -e $capture.called ]] || { echo "msmtp was not called" >&2; exit 1; }
[[ $(<"$capture.mode") == 600 ]] || { echo "msmtp config was not private" >&2; exit 1; }
grep -qxF 'user " lead\\\" user "' "$capture.config" || {
  echo "quoted SMTP username was not preserved" >&2
  exit 1
}
if grep -q -- '--user' "$capture.args" || grep -qF 'lead' "$capture.args"; then
  echo "SMTP username appeared in process arguments" >&2
  exit 1
fi
grep -qxF 'From: "Danny from aboutme.vn" <no-reply@example.test>' "$capture.message" || {
  echo "documented sender name did not reach the message" >&2
  exit 1
}

expect_rejected() { # label sender-name extra username-writer
  local label=$1 name=$2 extra=$3 username=$4 root capture
  root=$work/$label
  capture=$work/$label-capture
  write_env "$root" "$name" "$extra"
  printf '%b' "$username" >"$root/secrets/smtp-username"
  if run_alert "$root" "$capture"; then
    echo "$label was accepted" >&2
    exit 1
  fi
  [[ ! -e $capture.called ]] || { echo "$label reached msmtp" >&2; exit 1; }
}

expect_rejected control-c0 $'bad\tname' '' username
expect_rejected control-c1 $'bad\u0085name' '' username
expect_rejected duplicate 'Danny from aboutme.vn' 'SMTP_HOST=other.example.test' username
expect_rejected username-newline 'Danny from aboutme.vn' '' 'username\n'

echo "SMTP alert tests passed"
