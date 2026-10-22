#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/runtime/aboutme-vn" "$work/secrets"
export SECRET_LOG=$work/argv
cat >"$work/bin/ssh" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$SECRET_LOG"
cat >/dev/null
exit "${SSH_RESULT:-0}"
STUB
chmod +x "$work/bin/ssh"
export PATH=$work/bin:$PATH XDG_RUNTIME_DIR=$work/runtime DEPLOY_HOST=test.invalid
key=$work/runtime/aboutme-vn/aboutme-media-s3-credentials
printf 'synthetic-key' >"$key"
SSH_RESULT=1 bash "$root/deploy/vn/scripts/secrets.sh" import-s3 media --replace >"$work/out" 2>&1 && exit 1
[[ -f $key ]]
bash "$root/deploy/vn/scripts/secrets.sh" import-s3 media --replace >"$work/out" 2>&1
[[ ! -e $key ]]
grep -q 'import-s3 media --replace' "$SECRET_LOG"
# Load only the bouncer functions; commands use synthetic values and temp paths.
sed -n '/^bouncer_key_valid() {/,/^}/p;/^cmd_bouncer_register() {/,/^}/p' \
  "$root/deploy/vn/scripts/secrets-host.sh" |
  sed 's|/usr/local/lib/aboutme/crowdsec-db.sh|crowdsec_gate|g' >"$work/function"
# The local API accepts only the key cscli generated; curl reads it on stdin.
curl() {
  printf '%s\n' "$*" >>"$SECRET_LOG"
  if grep -q 'X-Api-Key: synthetic-bouncer-key' <&0; then printf 200; else printf 403; fi
}
# shellcheck disable=SC1091
source "$work/function"
dir=$work/secrets
say() { echo "$*"; }
die() { echo "$*" >&2; exit 1; }
cmd_podman_sync() { printf '%s\n' "$*" >>"$work/sync"; }
crowdsec_gate() {
  [[ $* == preflight-ready ]] || return 1
  printf 'gate\n' >>"$work/admission"
  [[ ! -e $work/schema-invalid ]]
}
cscli() {
  printf 'vendor\n' >>"$work/admission"
  printf '%s\n' "$*" >>"$SECRET_LOG"
  if [[ $2 == list ]]; then
    if [[ -f $work/registered ]]; then echo '[{"name":"aboutme-caddy"}]'; else echo '[]'; fi
  else
    [[ $* == 'bouncers add aboutme-caddy -o raw' ]] || return 1
    printf 'synthetic-bouncer-key\n'
    : >"$work/registered"
  fi
}
printf old-key >"$dir/crowdsec-bouncer-key"
cmd_bouncer_register >"$work/out" 2>"$work/err"
[[ $(cat "$work/admission") == $'gate\nvendor\ngate\nvendor' ]] || {
  echo 'FAIL registration-per-call-admission' >&2; exit 1;
}
[[ $(cat "$dir/crowdsec-bouncer-key") == synthetic-bouncer-key ]]
[[ $(stat -c %a "$dir/crowdsec-bouncer-key") == 400 ]]
grep -qx -- '--replace crowdsec-bouncer-key' "$work/sync"
if grep -qE 'synthetic-bouncer-key|old-key|--key' "$SECRET_LOG" "$work/out" "$work/err"; then exit 1; fi
cmd_bouncer_register >"$work/out" 2>"$work/err"
[[ $(grep -c 'bouncers add' "$SECRET_LOG") == 1 ]]
[[ $(grep -c -- '--replace crowdsec-bouncer-key' "$work/sync") == 2 ]]
# A retry after an interrupted registration finds a stale local key and refuses.
rm -f -- "$dir/crowdsec-bouncer-key"
printf stale-key >"$dir/crowdsec-bouncer-key"
if (cmd_bouncer_register) >"$work/out" 2>"$work/err"; then exit 1; fi
grep -q 'does not match' "$work/err"
if grep -qE 'synthetic-bouncer-key|stale-key' "$SECRET_LOG" "$work/out" "$work/err"; then exit 1; fi
# Gate failure must stop even a read-only list before vendor initialization.
: >"$work/admission"
touch "$work/schema-invalid"
if (cmd_bouncer_register) >"$work/out" 2>"$work/err"; then
  echo 'FAIL registration-corrupt-schema-refusal' >&2; exit 1
fi
[[ $(cat "$work/admission") == gate ]] || {
  echo 'FAIL registration-vendor-before-refusal' >&2; exit 1;
}
rm "$work/schema-invalid"
# Corruption after the first call must prevent the following registration.
rm "$work/registered" "$dir/crowdsec-bouncer-key"
: >"$work/admission"
cscli() {
  printf 'vendor\n' >>"$work/admission"
  [[ $* == 'bouncers list -o json' ]] || return 1
  touch "$work/schema-invalid"
  printf '[]\n'
}
if (cmd_bouncer_register) >"$work/out" 2>"$work/err"; then
  echo 'FAIL registration-inter-call-corruption' >&2; exit 1
fi
[[ $(cat "$work/admission") == $'gate\nvendor\ngate' ]] || {
  echo 'FAIL registration-inter-call-admission' >&2; exit 1;
}
[[ ! -e $dir/crowdsec-bouncer-key && ! -e $work/registered ]]
# An existing host key must refuse an unrequested replacement.
sed -n '/^cmd_import_s3() {/,/^}/p' "$root/deploy/vn/scripts/secrets-host.sh" >"$work/import"
# shellcheck disable=SC1091
source "$work/import"
printf existing >"$dir/media-access-key-id"
if (printf '[default]\naws_access_key_id=NEW\naws_secret_access_key=NEW\n' | cmd_import_s3 media) >"$work/out" 2>&1; then exit 1; fi
echo 'secret generation and rotation tests passed'
