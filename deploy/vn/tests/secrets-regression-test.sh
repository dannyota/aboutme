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
sed -n '/^bouncer_key_valid() {/,/^}/p;/^cmd_bouncer_register() {/,/^}/p' \
  "$root/deploy/vn/scripts/secrets-host.sh" |
  sed 's|/usr/local/lib/aboutme/crowdsec-db.sh|crowdsec_gate|g' >"$work/function"
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
rm -f -- "$dir/crowdsec-bouncer-key"
printf stale-key >"$dir/crowdsec-bouncer-key"
if (cmd_bouncer_register) >"$work/out" 2>"$work/err"; then exit 1; fi
grep -q 'does not match' "$work/err"
if grep -qE 'synthetic-bouncer-key|stale-key' "$SECRET_LOG" "$work/out" "$work/err"; then exit 1; fi
: >"$work/admission"
touch "$work/schema-invalid"
if (cmd_bouncer_register) >"$work/out" 2>"$work/err"; then
  echo 'FAIL registration-corrupt-schema-refusal' >&2; exit 1
fi
[[ $(cat "$work/admission") == gate ]] || {
  echo 'FAIL registration-vendor-before-refusal' >&2; exit 1;
}
rm "$work/schema-invalid"
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
sed -n '/^cmd_import_s3() {/,/^}/p' "$root/deploy/vn/scripts/secrets-host.sh" >"$work/import"
# shellcheck disable=SC1091
source "$work/import"
printf existing >"$dir/media-access-key-id"
if (printf '[default]\naws_access_key_id=NEW\naws_secret_access_key=NEW\n' | cmd_import_s3 media) >"$work/out" 2>&1; then exit 1; fi
sed -n '/^step_db_bootstrap() {/,/^}/p' "$root/deploy/vn/scripts/deploy-host.sh" >"$work/bootstrap"
# shellcheck disable=SC1091
source "$work/bootstrap"
secrets=$work/db-secrets
mkdir -p "$secrets"
db_password=synthetic-db-password-7f3c
printf '%s\n' "$db_password" >"$secrets/db-admin-password"
: >"$work/db-argv"
runuser() {
  printf 'runuser %s\n' "$*" >>"$work/db-argv"
  shift 3
  if [[ $1 == psql ]]; then cat >"$work/db-stdin"; else "$@"; fi
}
pgbackrest() {
  printf 'pgbackrest %s\n' "$*" >>"$work/db-argv"
  if [[ $* == *' info '* ]]; then printf '[{"status":{"code":0}}]\n'; fi
}
jq() {
  printf 'jq %s\n' "$*" >>"$work/db-argv"
  cat >/dev/null
}
(step_db_bootstrap) >"$work/out" 2>&1 || { echo "FAIL db-bootstrap: $(cat "$work/out")" >&2; exit 1; }
grep -qF "\\set pw '$db_password'" "$work/db-stdin" || { echo 'FAIL db-bootstrap-password-stdin' >&2; exit 1; }
if grep -F "$db_password" "$work/db-argv" "$work/out" >&2; then
  echo 'FAIL db-bootstrap-password-argv' >&2; exit 1
fi
panic_line=$(grep -nxF 'SET log_min_error_statement = panic;' "$work/db-stdin" | cut -d: -f1)
password_line=$(grep -n '^\\set pw ' "$work/db-stdin" | cut -d: -f1)
if ! [[ $panic_line =~ ^[0-9]+$ && $password_line =~ ^[0-9]+$ ]] || ((panic_line > password_line)); then
  echo 'FAIL db-bootstrap-log-level-before-password' >&2; exit 1
fi
echo 'secret generation and rotation tests passed'
