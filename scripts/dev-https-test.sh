#!/usr/bin/env bash
# Hermetic lifecycle contract tests for scripts/dev-https.sh. The production
# script runs only inside temporary repository fixtures with controlled tools.
set -Eeuo pipefail
umask 077

cd "$(dirname "${BASH_SOURCE[0]}")/.."
readonly SOURCE_ROOT=$(pwd -P)
readonly SOURCE_SCRIPT=$SOURCE_ROOT/scripts/dev-https.sh
readonly SOURCE_LIB_DIR=$SOURCE_ROOT/scripts/lib
source "$SOURCE_LIB_DIR/platform.sh"
# shellcheck source=scripts/dev-https-test-diagnostics.sh
source "$SOURCE_ROOT/scripts/dev-https-test-diagnostics.sh"
readonly FIXTURE_PATH=$PATH
readonly FIXTURE_BASH=$(command -v bash)

fail() {
  printf 'dev-https-test: %s\n' "$*" >&2
  exit 1
}

assert_contains() {
  local haystack=$1 needle=$2
  [[ $haystack == *"$needle"* ]] || fail "output did not contain: $needle"
}

assert_not_contains() {
  local haystack=$1 needle=$2
  [[ $haystack != *"$needle"* ]] || fail "output contained forbidden value: $needle"
}

assert_log_line() {
  local file=$1 expected=$2
  grep -Fqx -- "$expected" "$file" || fail "$file did not contain exact line: $expected"
}

cleanup_fixture() {
  local fixture=$1 pid pgid status=0
  if [ -n "${guard_pid:-}" ]; then
    kill -TERM "$guard_pid" 2>/dev/null || true
    wait "$guard_pid" 2>/dev/null || status=$?
    if [ -f "$fixture/diagnostics/admission-report.$guard_pid.txt" ]; then
      dev_https_test_admission_report "$fixture" "$guard_pid" exited "$status" "$((SECONDS - ${admission_start:-SECONDS}))"
    fi
    guard_pid=
  fi
  if [ -s "$fixture/repo/.dev/native-https/run/session" ]; then
    pid=$(sed -n 's/^pgid=//p' "$fixture/repo/.dev/native-https/run/session")
    [[ $pid =~ ^[1-9][0-9]*$ ]] && [ "$pid" -gt 1 ] && kill -KILL -- "-$pid" 2>/dev/null || true
  fi
  if [ -f "$fixture/all-pids" ]; then
    while IFS= read -r pid; do
      [[ $pid =~ ^[0-9]+$ ]] || continue
      pgid=$(ps -o pgid= -p "$pid" 2>/dev/null | tr -d ' ' || true)
      [[ $pgid =~ ^[0-9]+$ ]] && kill -KILL -- "-$pgid" 2>/dev/null || true
      kill -TERM "$pid" 2>/dev/null || true
      kill -KILL "$pid" 2>/dev/null || true
    done <"$fixture/all-pids"
  fi
  rm -rf -- "$fixture"
}

write_fake_tools() {
  local fixture=$1 fakebin=$fixture/fakebin
  mkdir -p "$fakebin"

  cat >"$fakebin/fake-service" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
name=$1
printf '%s\n' "$$" >>"$DEV_HTTPS_PID_AUDIT_FILE"
printf 'start:%s\n' "$name" >>"$FAKE_MUTATIONS"
printf 'fixture service=%s started=%(%FT%T%z)T pid=%s\n' "$name" -1 "$$"
case $name in
mock-oauth)
  printf 'service:%s host=%s port=%s origin=%s client=%s linkedin=%s db=%s\n' \
    "$name" "$LISTEN_HOST" "$PORT" "$PUBLIC_ORIGIN" "$GOOGLE_CLIENT_ID" "$LINKEDIN_CLIENT_ID" "${DATABASE_URL-}" >>"$FAKE_EFFECTS"
  ;;
server)
  printf 'service:%s host=%s port=%s origin=%s render=%s app=%s renderer=%s issuer=%s linkedin=%s db=%s\n' \
    "$name" "$LISTEN_HOST" "$PORT" "$PUBLIC_ORIGIN" "$PUBLIC_RENDER_ORIGIN" "$APP_BUILD_DIGEST" "$PUBLIC_RENDERER_BUILD_DIGEST" "$GOOGLE_OIDC_ISSUER_URL" "$LINKEDIN_OIDC_ISSUER_URL" "$DATABASE_URL" >>"$FAKE_EFFECTS"
  ;;
web) printf 'service:web\n' >>"$FAKE_EFFECTS" ;;
caddy)
  printf 'service:caddy\n' >>"$FAKE_EFFECTS"
  install -d -m 0700 "$XDG_DATA_HOME/caddy" "$XDG_DATA_HOME/caddy/pki" \
    "$XDG_DATA_HOME/caddy/pki/authorities" "$XDG_DATA_HOME/caddy/pki/authorities/local"
  printf '%s\n' 'fake local root certificate' >"$XDG_DATA_HOME/caddy/pki/authorities/local/root.crt"
  ;;
esac
if [ "${FAKE_EXIT_SERVICE-}" = "$name" ]; then
  exit 7
fi
if [ "${FAKE_EXIT_BEFORE_COMPLETION-}" = "$name" ]; then
  kill -TERM "$PPID" 2>/dev/null || true
  exit 7
fi
trap 'printf "stop:%s\n" "$name" >>"$FAKE_STOP_LOG"; exit 0' TERM INT
if [ "${FAKE_LISTENER_CHILD:-}" = "$name" ]; then
  /bin/sleep 300 &
  printf '%s\n' "$!" >"${FAKE_FOREIGN_PORTS%/*}/listener-child"
  printf '%s\n' "$!" >>"$DEV_HTTPS_PID_AUDIT_FILE"
fi
if [ "${FAKE_LEADER_EXITS_CHILD_IGNORES-}" = "$name" ]; then
  bash -c 'trap "" TERM; while :; do sleep 1; done' &
  printf '%s\n' "$!" >>"$DEV_HTTPS_PID_AUDIT_FILE"
fi
while :; do sleep 1; done
EOF

  cat >"$fakebin/go" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
[ "${1-}" = build ] || exit 0
shift
out=
package=
while [ "$#" -gt 0 ]; do
  case $1 in
  -o) out=$2; shift 2 ;;
  *) package=$1; shift ;;
  esac
done
[ -n "$out" ] || exit 64
name=${out##*/}
if [ "${FAKE_GO_FAIL_ONCE-}" = "$name" ] && [ ! -e "$FAKE_GO_FAIL_MARKER" ]; then
  : >"$FAKE_GO_FAIL_MARKER"
  printf 'go-build-failed:%s:%s\n' "$name" "$package" >>"$FAKE_MUTATIONS"
  exit 70
fi
mkdir -p "$(dirname "$out")"
cat >"$out" <<'INNER'
#!/usr/bin/env bash
set -Eeuo pipefail
name=${0##*/}
if [ "$name" = migrate ]; then
  # The real command takes no positional arguments; a stray one (e.g. a
  # leftover "provision") must fail here the same way.
  if [ "$#" -gt 0 ]; then
    printf 'migrate: arguments are not accepted\n' >&2
    exit 1
  fi
  printf 'migrate:db=%s\n' "$DATABASE_URL" >>"$FAKE_EFFECTS"
  printf 'migrate:operation=apply\n' >>"$FAKE_EFFECTS"
  printf 'database=stub provisioned=yes migrated=yes\n' >"$DEV_HTTPS_DIAGNOSTICS_DIR/database"
  exit 0
fi
if [ "$name" = mail-capture ]; then
  printf 'mail-capture-args:%s\n' "$*" >>"$FAKE_EFFECTS"
fi
exec fake-service "$name"
INNER
  chmod 0700 "$out"
printf 'go-build:%s:%s\n' "${out##*/}" "$package" >>"$FAKE_MUTATIONS"
EOF

  cat >"$fakebin/npm" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'npm:%s\n' "$*" >>"$FAKE_EFFECTS"
exec fake-service web
EOF

  cat >"$fakebin/node" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [ "$#" -eq 1 ] && [[ $1 == */scripts/chromium-path.mjs ]]; then
  printf '%s/fake-service\n' "${0%/*}"
  exit 0
fi
[ "$#" -eq 2 ]
[[ $1 == */scripts/generate-public-roots.mjs ]]
[ "$2" = --check ]
[ -f "$PWD/packages/publicroots/public-roots.v11.json" ]
[ -f "$PWD/deploy/caddy/public-roots.generated.caddy" ]
[ "$(sha256sum "$PWD/packages/publicroots/public-roots.v11.json" | awk '{print $1}')" = "$FAKE_REGISTRY_SHA256" ]
[ "$(sha256sum "$PWD/deploy/caddy/public-roots.generated.caddy" | awk '{print $1}')" = "$FAKE_FRAGMENT_SHA256" ]
printf '%s\n' \
  'APP_BUILD_DIGEST=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
  'PUBLIC_RENDERER_BUILD_DIGEST=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
EOF

  cat >"$fakebin/caddy" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
config=
while [ "$#" -gt 0 ]; do
  case $1 in
  --config) config=$2; shift 2 ;;
  *) shift ;;
  esac
done
[ -f "$config" ] || exit 64
count() { grep -Foc -- "$1" "$config"; }
[ "$(count 'https://localhost:20443 {')" = 1 ]
[ "$(count 'https://client.example:20443 {')" = 1 ]
[ "$(count 'bind 127.0.0.1')" = 2 ]
[ "$(count 'tls internal')" = 2 ]
[ "$(count 'skip_install_trust')" = 1 ]
[ "$(count 'auto_https disable_redirects')" = 1 ]
[ "$(count 'reverse_proxy 127.0.0.1:20441 {')" = 2 ]
[ "$(count 'reverse_proxy 127.0.0.1:20440')" = 2 ]
[ "$(count '@uat_google_authorize path /__uat/oauth/google/authorize')" = 1 ]
[ "$(count '@uat_linkedin_authorize path /__uat/oauth/linkedin/authorize')" = 1 ]
[ "$(count 'reverse_proxy 127.0.0.1:20442')" = 2 ]
printf 'caddy-config-validated\n' >>"$FAKE_EFFECTS"
exec fake-service caddy
EOF

  cat >"$fakebin/curl" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'curl:%s\n' "$*" >>"$FAKE_EFFECTS"
case " $* " in
*' --max-time '*) ;;
*) exit 65 ;;
esac
url=${!#}
case $url in
http://127.0.0.1:20441/healthz) service=server ;;
http://127.0.0.1:20440/) service=web ;;
http://127.0.0.1:20442/*) service=mock-oauth ;;
https://localhost:20443/healthz) service=caddy ;;
*) service= ;;
esac
if [ -n "$service" ]; then
  printf '%s readiness: time=%(%FT%T%z)T url=%s exit=0 response=stubbed-success body=empty\n' \
    "$service" -1 "$url" >"$DEV_HTTPS_DIAGNOSTICS_DIR/probe-$service"
fi
if [ "${FAKE_FOREIGN_AFTER_PROBE:-0}" = 1 ]; then
  printf '20441\n' >"$FAKE_FOREIGN_PORTS"
fi
exit 0
EOF

  cat >"$fakebin/lsof" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
mode=${FAKE_SS_MODE-}
[ ! -f "${FAKE_FOREIGN_PORTS%/*}/listener-mode" ] || mode=$(<"${FAKE_FOREIGN_PORTS%/*}/listener-mode")
case $mode in
nonzero) printf 'injected inspection failure\n' >&2; exit 9 ;;
malformed) printf 'malformed listener\n'; exit 0 ;;
esac
port=
for arg in "$@"; do [[ $arg != -iTCP:* ]] || port=${arg#-iTCP:}; done
if grep -Fqx -- "$port" "$FAKE_FOREIGN_PORTS" 2>/dev/null; then
  printf 'p%s\nn127.0.0.1:%s\n' "$PPID" "$port"
  exit 0
fi
case $port in
20440) name=web ;;
20441 | 20445) name=server ;;
20442) name=mock-oauth ;;
20443) name=caddy ;;
20444) name=mail-capture ;;
*) exit 1 ;;
esac
file="$PWD/.dev/native-https/run/$name.pid"
if [ -s "$file" ]; then
  pid=$(<"$file")
  if [ "$name" = web ] && [ -s "${FAKE_FOREIGN_PORTS%/*}/listener-child" ]; then
    pid=$(<"${FAKE_FOREIGN_PORTS%/*}/listener-child")
  fi
  if [ "$name" = web ] && [ -f "${FAKE_FOREIGN_PORTS%/*}/sibling-listener" ]; then
    pid=$(<"$PWD/.dev/native-https/run/server.pid")
  fi
  if kill -0 "$pid" 2>/dev/null; then
    printf 'p%s\nn127.0.0.1:%s\n' "$pid" "$port"
    exit 0
  fi
fi
exit 1
EOF

  cat >"$fakebin/ss" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
port=${!#}
port=${port##*:}
status=0
rows=$("${0%/*}/lsof" -nP "-iTCP:$port" -sTCP:LISTEN -Fpn) || status=$?
if [ "$status" = 1 ]; then exit 0; fi
[ "$status" = 0 ] || exit "$status"
pid=
while IFS= read -r row; do
  case $row in
  p[0-9]*) pid=${row#p} ;;
  n*) printf 'LISTEN 0 4096 %s *:* users:(("fixture",pid=%s,fd=8))\n' "${row#n}" "$pid" ;;
  *) printf '%s\n' "$row" ;;
  esac
done <<<"$rows"
EOF

  cat >"$fakebin/podman" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [ "${1-}" = ps ]; then
  cat "$FAKE_PODMAN_PS"
  exit 0
fi
printf 'podman:%s\n' "$*" >>"$FAKE_FORBIDDEN"
exit 66
EOF

  cat >"$fakebin/sudo" <<'EOF'
#!/usr/bin/env bash
printf 'sudo:%s\n' "$*" >>"$FAKE_FORBIDDEN"
exit 99
EOF

  cat >"$fakebin/sysctl" <<'EOF'
#!/usr/bin/env bash
printf 'sysctl:%s\n' "$*" >>"$FAKE_FORBIDDEN"
exit 99
EOF

  chmod 0700 "$fakebin"/*
}

new_fixture() {
  local fixture
  fixture=$(mktemp -d)
  fixture=$(cd "$fixture" && pwd -P)
  mkdir -p "$fixture/repo/scripts/lib" "$fixture/repo/deploy/caddy" \
    "$fixture/repo/packages/publicroots" \
    "$fixture/repo/apps/server" "$fixture/repo/apps/web/node_modules"
  cp "$SOURCE_SCRIPT" "$fixture/repo/scripts/dev-https.sh"
  cp "$SOURCE_LIB_DIR"/dev-https-*.sh "$SOURCE_LIB_DIR/platform.sh" "$fixture/repo/scripts/lib/"
  cp "$SOURCE_ROOT/scripts/generate-public-roots.mjs" "$fixture/repo/scripts/generate-public-roots.mjs"
  cp "$SOURCE_ROOT/scripts/chromium-path.mjs" "$fixture/repo/scripts/chromium-path.mjs"
  cp "$SOURCE_ROOT/deploy/caddy/Caddyfile" "$fixture/repo/deploy/caddy/Caddyfile"
  cp "$SOURCE_ROOT/deploy/caddy/public-roots.generated.caddy" "$fixture/repo/deploy/caddy/public-roots.generated.caddy"
  cp "$SOURCE_ROOT/packages/publicroots/public-roots.v11.json" "$fixture/repo/packages/publicroots/public-roots.v11.json"
  cat >"$fixture/repo/Makefile" <<'EOF'
.PHONY: tools-check test-db-up
tools-check:
	@printf 'tools-check\n' >>"$(FAKE_MUTATIONS)"
test-db-up:
	@printf 'test-db-up\n' >>"$(FAKE_MUTATIONS)"
	@printf 'database=stub provisioned=yes migrated=no\n' >"$(DEV_HTTPS_DIAGNOSTICS_DIR)/database"
EOF
  : >"$fixture/mutations"
  : >"$fixture/effects"
  : >"$fixture/stops"
  : >"$fixture/forbidden"
  : >"$fixture/foreign-ports"
  : >"$fixture/podman-ps"
  : >"$fixture/all-pids"
  mkdir -p "$fixture/diagnostics"
  printf 'database=stub provisioned=no migrated=no\n' >"$fixture/diagnostics/database"
  write_fake_tools "$fixture"
  printf '%s' "$fixture"
}

fixture_env() {
  local fixture=$1
  export PATH="$fixture/fakebin:$FIXTURE_PATH"
  export FAKE_MUTATIONS=$fixture/mutations
  export FAKE_EFFECTS=$fixture/effects
  export FAKE_STOP_LOG=$fixture/stops
  export FAKE_FORBIDDEN=$fixture/forbidden
  export FAKE_FOREIGN_PORTS=$fixture/foreign-ports
  export FAKE_PODMAN_PS=$fixture/podman-ps
  export DEV_HTTPS_PID_AUDIT_FILE=$fixture/all-pids
  export DEV_HTTPS_DIAGNOSTICS_DIR=$fixture/diagnostics
  export BASH_ENV=$SOURCE_ROOT/scripts/dev-https-test-diagnostics.sh
  export FAKE_GO_FAIL_MARKER=$fixture/go-fail-marker
  export FAKE_REGISTRY_SHA256=$(sha256sum "$fixture/repo/packages/publicroots/public-roots.v11.json" | awk '{print $1}')
  export FAKE_FRAGMENT_SHA256=$(sha256sum "$fixture/repo/deploy/caddy/public-roots.generated.caddy" | awk '{print $1}')
  export DEV_HTTPS_STOP_TERM_ATTEMPTS=5
  export DEV_HTTPS_STOP_KILL_ATTEMPTS=20
}

run_happy_path_and_lifecycle_checks() (
  local fixture output before after mode lines manifest service caddy_config
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"

  output=$(bash scripts/dev-https.sh up 2>&1) || fail "happy-path up failed: $output"
  assert_not_contains "$output" 'not-a-secret-local-google'
  assert_not_contains "$output" 'GOOGLE_CLIENT_SECRET='
  assert_not_contains "$output" 'not-a-secret-local-linkedin'
  assert_not_contains "$output" 'LINKEDIN_CLIENT_SECRET='
  [ ! -s "$FAKE_FORBIDDEN" ] || fail "up invoked a forbidden command"
  assert_log_line "$FAKE_MUTATIONS" 'test-db-up'
  assert_log_line "$FAKE_MUTATIONS" 'go-build:migrate:./cmd/migrate'
  assert_log_line "$FAKE_MUTATIONS" 'go-build:mock-oauth:./cmd/mock-oauth'
  assert_log_line "$FAKE_MUTATIONS" 'go-build:server:./cmd/server'
  assert_log_line "$FAKE_MUTATIONS" 'go-build:render-browser-supervisor:./cmd/render-browser-supervisor'
  assert_log_line "$FAKE_EFFECTS" 'migrate:db=postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable'
  assert_log_line "$FAKE_EFFECTS" 'migrate:operation=apply'
  assert_log_line "$FAKE_EFFECTS" 'service:mock-oauth host=127.0.0.1 port=20442 origin=https://localhost:20443 client=aboutme-local-google linkedin=aboutme-local-linkedin db='
  assert_log_line "$FAKE_EFFECTS" 'service:server host=127.0.0.1 port=20441 origin=https://localhost:20443 render=http://127.0.0.1:20440 app=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa renderer=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb issuer=http://127.0.0.1:20442/google linkedin=http://127.0.0.1:20442/linkedin db=postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable'
  assert_log_line "$FAKE_EFFECTS" 'npm:run dev -- --port 20440 --host 127.0.0.1'
  assert_log_line "$FAKE_EFFECTS" 'caddy-config-validated'
  assert_log_line "$FAKE_MUTATIONS" 'go-build:mail-capture:./cmd/mail-capture'
  assert_log_line "$FAKE_MUTATIONS" 'start:mail-capture'
  grep -F 'mail-capture-args:' "$FAKE_EFFECTS" | grep -Fq -- '--addr 127.0.0.1:20444' || \
    fail 'mail-capture was not started with the exact capture address'
  grep -F 'mail-capture-args:' "$FAKE_EFFECTS" | grep -Fq -- 'secrets/auth-email-capture-bearer' || \
    fail 'mail-capture was not started with the harness capture secret file'

  [ "$(stat -c '%a' .dev/native-https/secrets)" = 700 ] || fail 'secrets directory is not mode 700'
  for secret_name in password-rate-hmac-key auth-email-active-key auth-email-capture-bearer; do
    secret_file=".dev/native-https/secrets/$secret_name"
    [ -f "$secret_file" ] || fail "missing secret file $secret_file"
    [ "$(stat -c '%a' "$secret_file")" = 600 ] || fail "$secret_file mode is $(stat -c '%a' "$secret_file"), want 600"
    [ "$(stat -c '%s' "$secret_file")" = 32 ] || fail "$secret_file size is $(stat -c '%s' "$secret_file"), want 32"
  done
  server_env=$(<.dev/native-https/run/server.env)
  assert_contains "$server_env" 'PRINT_LISTEN_ADDR=127.0.0.1:20445'
  assert_contains "$server_env" "CHROMIUM_PATH=$fixture/fakebin/fake-service"
  assert_contains "$(<.dev/native-https/run/web.env)" 'NUXT_PRINT_ORIGIN=http://127.0.0.1:20445'
  assert_contains "$server_env" 'PASSWORD_RATE_HMAC_KEY='
  assert_contains "$server_env" 'AUTH_EMAIL_ACTIVE_KEY_ID=dev-active'
  assert_contains "$server_env" 'AUTH_EMAIL_ACTIVE_KEY='
  assert_contains "$server_env" 'AUTH_EMAIL_MODE=capture'
  assert_contains "$server_env" 'AUTH_EMAIL_CAPTURE_URL=http://127.0.0.1:20444'
  assert_contains "$server_env" 'AUTH_EMAIL_CAPTURE_BEARER='
  assert_contains "$server_env" 'MCP_ENABLED=true'
  assert_contains "$server_env" 'OAUTH_CIMD_ENABLED=true'
  assert_contains "$server_env" 'OAUTH_CIMD_DEV_STUB_ADDR=127.0.0.1:20443'
  assert_contains "$server_env" "SSL_CERT_FILE=$fixture/repo/.dev/native-https/caddy/data/caddy/pki/authorities/local/root.crt"
  caddy_config=$(<.dev/native-https/Caddyfile)
  assert_contains "$caddy_config" 'https://client.example:20443 {'
  assert_contains "$caddy_config" 'client_id'
  assert_contains "$caddy_config" 'http://localhost/callback'
  assert_not_contains "$caddy_config" 'tls_insecure_skip_verify'
  # The env file is written with printf %q, which escapes the comma; the
  # supervisor sources it, so check the value the server receives.
  [ "$(set -a && source .dev/native-https/run/server.env && printf '%s' "$PROVIDER_LOGIN_ENABLED")" = google,linkedin ] ||
    fail 'server PROVIDER_LOGIN_ENABLED is not google,linkedin'
  assert_contains "$server_env" 'PASSKEY_ENROLLMENT_ENABLED=true'
  assert_contains "$server_env" 'TOTP_ACTIVE_KEY='
  assert_contains "$server_env" 'TOTP_ENROLLMENT_ENABLED=true'
  assert_contains "$server_env" 'PREVIEW_CARD_ENABLED=true'

  [ -f .dev/native-https/input/caddy-root.crt ] || fail "exported Caddy root is missing"
  mode=$(stat -c '%a' .dev/native-https/input/caddy-root.crt)
  [ "$mode" = 600 ] || fail "Caddy root mode is $mode, want 600"
  [ -f .dev/native-https/effective-config ] || fail "effective config manifest is missing"
  mode=$(stat -c '%a' .dev/native-https/effective-config)
  [ "$mode" = 600 ] || fail "effective config manifest mode is $mode, want 600"
  manifest=$(<.dev/native-https/effective-config)
  assert_contains "$manifest" 'database_target=127.0.0.1:20432/aboutme_dev?sslmode=disable'
  assert_contains "$manifest" 'log_level=info'
  assert_contains "$manifest" 'google_client_id=aboutme-local-google'
  assert_contains "$manifest" 'google_issuer_url=http://127.0.0.1:20442/google'
  assert_contains "$manifest" 'linkedin_client_id=aboutme-local-linkedin'
  assert_contains "$manifest" 'linkedin_issuer_url=http://127.0.0.1:20442/linkedin'
  assert_contains "$manifest" 'mcp_enabled=true'
  assert_contains "$manifest" 'oauth_cimd_enabled=true'
  assert_contains "$manifest" 'oauth_cimd_dev_stub_addr=127.0.0.1:20443'
  assert_contains "$manifest" 'public_render_origin=http://127.0.0.1:20440'
  assert_contains "$manifest" 'app_build_digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
  assert_contains "$manifest" 'public_renderer_build_digest=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
  assert_contains "$manifest" 'registry_source_sha256='
  assert_contains "$manifest" 'generated_fragment_sha256='
  assert_contains "$manifest" 'effective_caddyfile_sha256='
  assert_contains "$manifest" 'generated_route_sha256='
  assert_contains "$manifest" 'session_checkout='
  assert_contains "$manifest" 'session_launch_token='
  assert_contains "$manifest" 'mail_capture_port=20444'
  assert_contains "$manifest" 'mail_capture_binary_sha256='
  assert_contains "$manifest" 'mail_secrets_sha256='
  assert_not_contains "$manifest" 'not-a-secret-local-google'
  assert_not_contains "$manifest" 'not-a-secret-local-linkedin'
  for service in mock-oauth mail-capture server web caddy; do
    mode=$(stat -c '%a' ".dev/native-https/run/$service.env")
    [ "$mode" = 600 ] || fail "$service environment mode is $mode, want 600"
  done

  output=$(bash scripts/dev-https.sh status 2>&1) || fail "status failed: $output"
  assert_contains "$output" 'mock-oauth'
  assert_contains "$output" 'mail-capture'
  assert_contains "$output" '20443'
  assert_contains "$output" '20444'
  grep -F 'curl:' "$FAKE_EFFECTS" | grep -F -- '--cacert' | grep -Fq 'https://localhost:20443/healthz' || \
    fail "status did not probe public HTTPS with the exported CA"
  grep -F 'curl:' "$FAKE_EFFECTS" | grep -Fq 'http://127.0.0.1:20442/linkedin/.well-known/openid-configuration' || \
    fail "mock-oauth readiness did not wait for LinkedIn discovery"

  before=$(wc -l <"$FAKE_MUTATIONS")
  output=$(bash scripts/dev-https.sh up 2>&1) || fail "owned idempotent up failed: $output"
  after=$(wc -l <"$FAKE_MUTATIONS")
  [ "$before" = "$after" ] || fail "idempotent up performed build/start/database mutation"

  printf '20080\n' >"$FAKE_FOREIGN_PORTS"
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail "idempotent up accepted a normal native listener without a PID file"
  fi
  assert_contains "$output" '20080'
  : >"$FAKE_FOREIGN_PORTS"

  before=$(wc -l <"$FAKE_MUTATIONS")
  if output=$(ABOUTME_DEV_LOG_LEVEL=debug bash scripts/dev-https.sh up 2>&1); then
    fail "up accepted effective-config drift"
  fi
  assert_contains "$output" 'effective config'
  after=$(wc -l <"$FAKE_MUTATIONS")
  [ "$before" = "$after" ] || fail "effective-config rejection mutated the stack"

  printf '\n# drift\n' >>deploy/caddy/Caddyfile
  before=$(wc -l <"$FAKE_MUTATIONS")
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail "up accepted changed source configuration"
  fi
  assert_contains "$output" "down"
  after=$(wc -l <"$FAKE_MUTATIONS")
  [ "$before" = "$after" ] || fail "config-drift rejection mutated the stack"

  for i in $(seq 1 260); do printf 'safe-log-line-%03d\n' "$i"; done >>.dev/native-https/log/server.log
  printf '%s\n' 'GOOGLE_CLIENT_SECRET=not-a-secret-local-google' \
    'LINKEDIN_CLIENT_SECRET=not-a-secret-local-linkedin' \
    'access_token=token-sentinel-that-must-not-escape' \
    'Cookie: harmless=ok; __Host-session=cookie-session-sentinel; __Host-oauth-tx=cookie-oauth-sentinel' \
    'Set-Cookie: __Host-session=set-cookie-session-sentinel; Path=/; Secure; HttpOnly' \
    'Set-Cookie: __Host-oauth-tx=set-cookie-oauth-sentinel; Path=/; Secure; HttpOnly' \
    'X-CSRF-Token: csrf-header-sentinel' \
    'GET /api/v1/auth/google/callback?code=query-code-sentinel&state=query-state-sentinel&next=safe HTTP/1.1' \
    'Authorization: Bearer bearer-sentinel' \
    '{"access_token":"json-access-sentinel","id_token":"json-id-sentinel","refresh_token":"json-refresh-sentinel","status":"ok"}' \
    'id_token=id-assignment-sentinel' \
    'access_token: access-colon-space-sentinel' \
    'X-CSRF-Token=csrf-equals-sentinel' \
    'Authorization=Bearer authorization-equals-sentinel' \
    'cookie: __Host-session=lower-cookie-session-sentinel; harmless-lower=ok' \
    'COOKIE: __Host-oauth-tx=upper-cookie-oauth-sentinel; harmless-upper=ok' \
    '{"Authorization":"Bearer json-authorization-credential","X-CSRF-Token":"json-csrf-sentinel","csrfToken":"json-csrf-token-sentinel","code":"json-code-sentinel","state":"json-state-sentinel","status":"ok2"}' \
    >>.dev/native-https/log/server.log
  output=$(bash scripts/dev-https.sh logs server 2>&1) || fail "bounded logs failed: $output"
  lines=$(wc -l <<<"$output")
  [ "$lines" -le 200 ] || fail "logs returned $lines lines, want at most 200"
  assert_not_contains "$output" 'not-a-secret-local-google'
  assert_not_contains "$output" 'not-a-secret-local-linkedin'
  assert_not_contains "$output" 'token-sentinel-that-must-not-escape'
  assert_not_contains "$output" 'cookie-session-sentinel'
  assert_not_contains "$output" 'cookie-oauth-sentinel'
  assert_not_contains "$output" 'set-cookie-session-sentinel'
  assert_not_contains "$output" 'set-cookie-oauth-sentinel'
  assert_not_contains "$output" 'csrf-header-sentinel'
  assert_not_contains "$output" 'query-code-sentinel'
  assert_not_contains "$output" 'query-state-sentinel'
  assert_not_contains "$output" 'bearer-sentinel'
  assert_not_contains "$output" 'json-access-sentinel'
  assert_not_contains "$output" 'json-id-sentinel'
  assert_not_contains "$output" 'json-refresh-sentinel'
  assert_not_contains "$output" 'id-assignment-sentinel'
  assert_not_contains "$output" 'access-colon-space-sentinel'
  assert_not_contains "$output" 'csrf-equals-sentinel'
  assert_not_contains "$output" 'authorization-equals-sentinel'
  assert_not_contains "$output" 'lower-cookie-session-sentinel'
  assert_not_contains "$output" 'upper-cookie-oauth-sentinel'
  assert_not_contains "$output" 'json-authorization-credential'
  assert_not_contains "$output" 'json-csrf-sentinel'
  assert_not_contains "$output" 'json-csrf-token-sentinel'
  assert_not_contains "$output" 'json-code-sentinel'
  assert_not_contains "$output" 'json-state-sentinel'
  assert_contains "$output" '[REDACTED]'
  assert_contains "$output" 'harmless=ok'
  assert_contains "$output" 'next=safe'
  assert_contains "$output" '"status":"ok"'
  assert_contains "$output" 'harmless-lower=ok'
  assert_contains "$output" 'harmless-upper=ok'
  assert_contains "$output" '"status":"ok2"'

  capture_bearer=$(sed -n 's/^AUTH_EMAIL_CAPTURE_BEARER=//p' .dev/native-https/run/server.env)
  [ -n "$capture_bearer" ] || fail 'capture bearer is missing from the server environment'
  printf 'capture_bearer=%s\n' "$capture_bearer" >>.dev/native-https/log/server.log
  output=$(bash scripts/dev-https.sh logs server 2>&1) || fail "secret redaction logs failed: $output"
  assert_not_contains "$output" "$capture_bearer"
  assert_contains "$output" '[REDACTED]'

  totp_active_key=$(sed -n 's/^TOTP_ACTIVE_KEY=//p' .dev/native-https/run/server.env)
  [ -n "$totp_active_key" ] || fail 'TOTP active key is missing from the server environment'
  printf 'totp_active_key=%s\n' "$totp_active_key" >>.dev/native-https/log/server.log
  output=$(bash scripts/dev-https.sh logs server 2>&1) || fail "TOTP key redaction logs failed: $output"
  assert_not_contains "$output" "$totp_active_key"
  assert_contains "$output" '[REDACTED]'

  output=$(bash scripts/dev-https.sh down 2>&1) || fail "down failed: $output"
  [ "$(LC_ALL=C sort "$FAKE_STOP_LOG")" = "$(printf 'stop:%s\n' mock-oauth mail-capture server web caddy | LC_ALL=C sort)" ] || fail 'stop did not drain every service'
  before=$(wc -l <"$FAKE_STOP_LOG")
  bash scripts/dev-https.sh down >/dev/null 2>&1 || fail "absent-state down was not idempotent"
  after=$(wc -l <"$FAKE_STOP_LOG")
  [ "$before" = "$after" ] || fail "absent-state down targeted another process"
)

run_foreign_listener_check() (
  local fixture output
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  printf '20441\n' >"$FAKE_FOREIGN_PORTS"
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail "up accepted a foreign listener"
  fi
  assert_contains "$output" '20441'
  [ ! -s "$FAKE_MUTATIONS" ] || fail "foreign-listener rejection performed a mutation"
)

run_listener_probe_failure_checks() (
  local fixture output mode
  for mode in nonzero malformed; do
    fixture=$(new_fixture)
    trap 'cleanup_fixture "$fixture"' EXIT
    fixture_env "$fixture"
    export FAKE_SS_MODE=$mode
    cd "$fixture/repo"
    if output=$(bash scripts/dev-https.sh up 2>&1); then
      fail "up accepted $mode ss output"
    fi
    assert_contains "$output" 'listener probe'
    [ ! -s "$FAKE_MUTATIONS" ] || fail "$mode ss rejection performed a mutation"
    [ ! -e .dev/native-https ] || fail "$mode ss rejection created HTTPS harness state"
    cleanup_fixture "$fixture"
    trap - EXIT
  done
)

run_normal_native_listener_without_pid_check() (
  local fixture output port
  for port in 20030 20080 20081; do
    fixture=$(new_fixture)
    trap 'cleanup_fixture "$fixture"' EXIT
    fixture_env "$fixture"
    cd "$fixture/repo"
    printf '%s\n' "$port" >"$FAKE_FOREIGN_PORTS"
    if output=$(bash scripts/dev-https.sh up 2>&1); then
      fail "up accepted normal native listener on $port without a PID file"
    fi
    assert_contains "$output" "$port"
    [ ! -s "$FAKE_MUTATIONS" ] || fail "normal-native listener rejection mutated the stack"
    cleanup_fixture "$fixture"
    trap - EXIT
  done
)

run_database_override_check() (
  local fixture output
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  if output=$(ABOUTME_DEV_DATABASE_URL='postgres://foreign:foreign@192.0.2.1:5432/other' \
    bash scripts/dev-https.sh up 2>&1); then
    fail "up accepted ABOUTME_DEV_DATABASE_URL"
  fi
  assert_contains "$output" 'ABOUTME_DEV_DATABASE_URL'
  [ ! -s "$FAKE_MUTATIONS" ] || fail "database-override rejection performed a mutation"
)

run_active_http_stack_checks() (
  local fixture output native_pid
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"

  printf 'aboutme-web|aboutme|web\n' >"$FAKE_PODMAN_PS"
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail "up accepted an active Compose HTTP stack"
  fi
  assert_contains "$output" 'Compose HTTP stack'
  [ ! -s "$FAKE_MUTATIONS" ] || fail "Compose rejection performed a mutation"

  : >"$FAKE_PODMAN_PS"
  mkdir -p .dev
  detach bash -c 'echo $$ >"$1"; exec sleep 300' active-http .dev/server.pid
  for _ in $(seq 1 50); do [ -s .dev/server.pid ] && break; sleep 0.02; done
  native_pid=$(<.dev/server.pid)
  printf '%s\n' "$native_pid" >>"$fixture/all-pids"
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail "up accepted the active normal native stack"
  fi
  assert_contains "$output" 'normal native HTTP stack'
  kill -0 "$native_pid" 2>/dev/null || fail "HTTP-stack rejection killed the foreign stack"
)

run_route_drift_check() (
  local fixture output
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  printf '\n:80 {\n}\n' >>deploy/caddy/Caddyfile
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail "up accepted an ambiguous Caddyfile substitution"
  fi
  assert_contains "$output" 'want exactly 1'
  [ ! -s "$FAKE_MUTATIONS" ] || fail "route-drift rejection performed a mutation"
)

run_fragment_drift_check() (
  local fixture output
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  printf '\n# stale generated fragment\n' >>deploy/caddy/public-roots.generated.caddy
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail "up accepted a stale generated public-root fragment"
  fi
  assert_contains "$output" 'public-root generation check failed'
  [ ! -s "$FAKE_MUTATIONS" ] || fail "fragment-drift rejection performed a mutation"
)

run_missing_fragment_check() (
  local fixture output
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  rm deploy/caddy/public-roots.generated.caddy
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail "up accepted a missing generated public-root fragment"
  fi
  assert_contains "$output" 'public-root generation check failed'
  [ ! -s "$FAKE_MUTATIONS" ] || fail "missing-fragment rejection performed a mutation"
)

run_binary_drift_check() (
  local fixture output binary
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  output=$(bash scripts/dev-https.sh up 2>&1) || fail "binary-drift setup failed: $output"
  binary=.dev/native-https/bin/server
  printf '\n# drift\n' >>"$binary"
  if output=$(bash scripts/dev-https.sh status 2>&1); then
    fail "status accepted built-binary drift"
  fi
  assert_contains "$output" 'effective config'
  bash scripts/dev-https.sh down >/dev/null 2>&1 || fail "binary drift prevented safe down"
)

run_lifecycle_command_drift_down_check() (
  local fixture output
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  output=$(bash scripts/dev-https.sh up 2>&1) || fail "lifecycle-drift setup failed: $output"
  printf '\n# source drift\n' >>scripts/lib/dev-https-session.sh
  if output=$(bash scripts/dev-https.sh status 2>&1); then
    fail 'status accepted desired lifecycle command drift'
  fi
  assert_contains "$output" 'effective config'
  output=$(bash scripts/dev-https.sh down 2>&1) || \
    fail "desired lifecycle command drift prevented safe down: $output"
)

run_partial_startup_rollback_check() (
  local fixture output pidfile pid
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  export FAKE_EXIT_SERVICE=caddy
  cd "$fixture/repo"
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail "up accepted partial startup"
  fi
  assert_contains "$output" 'startup failed'
  assert_log_line "$FAKE_MUTATIONS" 'start:server'
  for pidfile in .dev/native-https/run/*.pid; do
    [ -e "$pidfile" ] || continue
    pid=$(<"$pidfile")
    ! kill -0 "$pid" 2>/dev/null || fail "rollback left pid $pid alive"
  done
  [ ! -s "$FAKE_FORBIDDEN" ] || fail "rollback invoked a forbidden command"
)

run_state_path_integrity_checks() (
  local fixture output sentinel outside

  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  outside=$fixture/outside-state
  install -d -m 0700 "$outside"
  printf '%s\n' untouched >"$outside/sentinel"
  mkdir -p .dev
  ln -s "$outside" .dev/native-https
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail 'up accepted a redirected state root'
  fi
  assert_contains "$output" 'state path'
  [ "$(<"$outside/sentinel")" = untouched ] || fail 'redirected state root changed outside data'
  [ "$(find "$outside" -mindepth 1 -maxdepth 1 -exec basename {} \;)" = sentinel ] || \
    fail 'redirected state root wrote outside state'
  [ ! -s "$FAKE_MUTATIONS" ] || fail 'redirected state root performed a mutation'
  cleanup_fixture "$fixture"
  trap - EXIT
  hash -r

  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  install -d -m 0755 .dev
  install -d -m 0700 .dev/native-https .dev/native-https/run
  sentinel=$fixture/outside-env
  printf '%s\n' untouched >"$sentinel"
  chmod 0600 "$sentinel"
  ln -s "$sentinel" .dev/native-https/run/server.env
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail 'up accepted a redirected owned file'
  fi
  assert_contains "$output" 'state path'
  [ "$(<"$sentinel")" = untouched ] || fail 'redirected owned file changed outside data'
  [ ! -s "$FAKE_MUTATIONS" ] || fail 'redirected owned file performed a mutation'
  cleanup_fixture "$fixture"
  trap - EXIT
  hash -r

  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  install -d -m 0755 .dev
  install -d -m 0700 .dev/native-https
  install -d -m 0770 .dev/native-https/run
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail 'up accepted a group-writable owned directory'
  fi
  assert_contains "$output" 'state path'
  [ ! -s "$FAKE_MUTATIONS" ] || fail 'unsafe state mode performed a mutation'
  cleanup_fixture "$fixture"
  trap - EXIT
  hash -r

  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  install -d -m 0755 .dev
  install -d -m 0700 .dev/native-https .dev/native-https/run
  printf '%s\n' harmless >.dev/native-https/run/server.env
  chmod 0644 .dev/native-https/run/server.env
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail 'up accepted a world-readable sensitive state file'
  fi
  assert_contains "$output" 'state path'
  [ ! -s "$FAKE_MUTATIONS" ] || fail 'unsafe state file mode performed a mutation'
  cleanup_fixture "$fixture"
  trap - EXIT
  hash -r

  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  install -d -m 0755 .dev
  install -d -m 0700 .dev/native-https .dev/native-https/run
  sentinel=$fixture/outside-hardlink
  printf '%s\n' untouched >"$sentinel"
  chmod 0600 "$sentinel"
  ln "$sentinel" .dev/native-https/run/server.env
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail 'up accepted a hard-linked owned state file'
  fi
  assert_contains "$output" 'state path'
  [ "$(<"$sentinel")" = untouched ] || fail 'hard-linked state file changed outside data'
  [ ! -s "$FAKE_MUTATIONS" ] || fail 'hard-linked state file performed a mutation'
  cleanup_fixture "$fixture"
  trap - EXIT
  hash -r

  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  install -d -m 0755 .dev
  output=$(bash scripts/dev-https.sh up 2>&1) || fail "safe 0755 .dev prevented up: $output"
  [ "$(stat -c '%a' .dev)" = 755 ] || fail 'up changed the shared .dev mode'
  for outside in .dev/native-https .dev/native-https/run .dev/native-https/log \
    .dev/native-https/bin .dev/native-https/media .dev/native-https/caddy \
    .dev/native-https/caddy/config .dev/native-https/caddy/data .dev/native-https/input; do
    [ "$(stat -c '%a' "$outside")" = 700 ] || fail "$outside was not created with mode 700"
  done
  output=$(bash scripts/dev-https.sh down 2>&1) || fail "safe state paths prevented down: $output"
)

run_failed_build_retry_check() (
  local fixture output
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  export FAKE_GO_FAIL_ONCE=mock-oauth
  cd "$fixture/repo"
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail 'up accepted an injected mock-oauth build failure'
  fi
  assert_contains "$(<"$FAKE_MUTATIONS")" 'go-build-failed:mock-oauth:./cmd/mock-oauth'
  [ "$(stat -c '%a' .dev/native-https/bin/migrate)" = 755 ] || \
    fail 'successful migrate build was not normalized before later build failure'
  output=$(bash scripts/dev-https.sh up 2>&1) || fail "retry after later build failure failed: $output"
  output=$(bash scripts/dev-https.sh down 2>&1) || fail "down after build retry failed: $output"
)

run_mail_capture_failure_rollback_check() (
  local fixture output pidfile pid
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  export FAKE_EXIT_SERVICE=mail-capture
  cd "$fixture/repo"
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail 'up accepted mail-capture startup failure'
  fi
  assert_contains "$output" 'startup failed'
  assert_log_line "$FAKE_MUTATIONS" 'start:mock-oauth'
  assert_log_line "$FAKE_MUTATIONS" 'start:mail-capture'
  for pidfile in .dev/native-https/run/*.pid; do
    [ -e "$pidfile" ] || continue
    pid=$(<"$pidfile")
    ! kill -0 "$pid" 2>/dev/null || fail "mail-capture rollback left pid $pid alive"
  done
  [ ! -s "$FAKE_FORBIDDEN" ] || fail 'mail-capture rollback invoked a forbidden command'
)

run_server_after_capture_rollback_check() (
  local fixture output pidfile pid
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  export FAKE_EXIT_SERVICE=server
  cd "$fixture/repo"
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail 'up accepted server failure after mail-capture start'
  fi
  assert_contains "$output" 'startup failed'
  assert_contains "$output" 'startup failure status='
  assert_contains "$output" 'startup timeline'
  assert_contains "$output" 'server combined stdout and stderr'
  assert_contains "$output" 'server readiness: not reached'
  assert_contains "$output" 'database=stub provisioned=yes migrated=yes'
  assert_contains "$output" 'process state'
  assert_log_line "$FAKE_MUTATIONS" 'start:mail-capture'
  assert_log_line "$FAKE_MUTATIONS" 'start:server'
  for pidfile in .dev/native-https/run/*.pid; do
    [ -e "$pidfile" ] || continue
    pid=$(<"$pidfile")
    ! kill -0 "$pid" 2>/dev/null || fail "server-after-capture rollback left pid $pid alive"
  done
  [ ! -s "$FAKE_FORBIDDEN" ] || fail 'server-after-capture rollback invoked a forbidden command'
)

run_mail_capture_binary_drift_check() (
  local fixture output binary
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  output=$(bash scripts/dev-https.sh up 2>&1) || fail "mail-capture binary-drift setup failed: $output"
  binary=.dev/native-https/bin/mail-capture
  printf '\n# drift\n' >>"$binary"
  if output=$(bash scripts/dev-https.sh status 2>&1); then
    fail 'status accepted mail-capture binary drift'
  fi
  assert_contains "$output" 'effective config'
  bash scripts/dev-https.sh down >/dev/null 2>&1 || fail 'mail-capture binary drift prevented safe down'
)

run_mail_capture_foreign_listener_check() (
  local fixture output
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  printf '20444\n' >"$FAKE_FOREIGN_PORTS"
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail 'up accepted a foreign listener on the mail-capture port'
  fi
  assert_contains "$output" '20444'
  [ ! -s "$FAKE_MUTATIONS" ] || fail 'mail-capture foreign-listener rejection performed a mutation'
)

run_secret_reuse_check() (
  local fixture output before_secrets after_secrets
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  output=$(bash scripts/dev-https.sh up 2>&1) || fail "secret-reuse up failed: $output"
  before_secrets=$(sha256sum .dev/native-https/secrets/* | sha256sum | awk '{print $1}')
  output=$(bash scripts/dev-https.sh down 2>&1) || fail "secret-reuse down failed: $output"
  output=$(bash scripts/dev-https.sh up 2>&1) || fail "secret-reuse re-up failed: $output"
  after_secrets=$(sha256sum .dev/native-https/secrets/* | sha256sum | awk '{print $1}')
  [ "$before_secrets" = "$after_secrets" ] || fail 'down/up rotated the mail secret keyring'
  output=$(bash scripts/dev-https.sh down 2>&1) || fail "secret-reuse final down failed: $output"
)

run_secret_mode_check() (
  local fixture output
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  install -d -m 0755 .dev
  install -d -m 0700 .dev/native-https .dev/native-https/secrets
  head -c 32 /dev/urandom >.dev/native-https/secrets/password-rate-hmac-key
  chmod 0644 .dev/native-https/secrets/password-rate-hmac-key
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail 'up accepted a world-readable mail secret'
  fi
  assert_contains "$output" 'state path'
  [ ! -s "$FAKE_MUTATIONS" ] || fail 'unsafe secret mode performed a mutation'
)

# The passkey browser proof runs one phase with enrollment open and one with
# it closed, so the server environment flag must follow
# DEV_HTTPS_PASSKEY_ENROLLMENT exactly and reject anything but true or false.
run_passkey_enrollment_flag_check() (
  local value
  # shellcheck source=lib/dev-https-lifecycle.sh
  source "$SOURCE_LIB_DIR/dev-https-lifecycle.sh"
  value=$(DEV_HTTPS_PASSKEY_ENROLLMENT= passkey_enrollment_flag) ||
    fail 'empty passkey enrollment selector was rejected'
  [ "$value" = true ] || fail "default passkey enrollment flag is $value, want true"
  value=$(DEV_HTTPS_PASSKEY_ENROLLMENT=true passkey_enrollment_flag) ||
    fail 'explicit true passkey enrollment selector was rejected'
  [ "$value" = true ] || fail "explicit true flag is $value, want true"
  value=$(DEV_HTTPS_PASSKEY_ENROLLMENT=false passkey_enrollment_flag) ||
    fail 'explicit false passkey enrollment selector was rejected'
  [ "$value" = false ] || fail "explicit false flag is $value, want false"
  for value in TRUE 1 yes ' ' 'true false'; do
    if DEV_HTTPS_PASSKEY_ENROLLMENT=$value passkey_enrollment_flag >/dev/null
    then
      fail "passkey enrollment selector accepted '$value'"
    fi
  done
)

# The TOTP browser proof runs one phase with enrollment open and one with it
# closed, so the server environment flag must follow DEV_HTTPS_TOTP_ENROLLMENT
# exactly and reject anything but true or false
# (docs/design/totp-key-management.md "Key ring").
run_totp_enrollment_flag_check() (
  local value
  # shellcheck source=lib/dev-https-lifecycle.sh
  source "$SOURCE_LIB_DIR/dev-https-lifecycle.sh"
  value=$(DEV_HTTPS_TOTP_ENROLLMENT= totp_enrollment_flag) ||
    fail 'empty TOTP enrollment selector was rejected'
  [ "$value" = true ] || fail "default TOTP enrollment flag is $value, want true"
  value=$(DEV_HTTPS_TOTP_ENROLLMENT=true totp_enrollment_flag) ||
    fail 'explicit true TOTP enrollment selector was rejected'
  [ "$value" = true ] || fail "explicit true flag is $value, want true"
  value=$(DEV_HTTPS_TOTP_ENROLLMENT=false totp_enrollment_flag) ||
    fail 'explicit false TOTP enrollment selector was rejected'
  [ "$value" = false ] || fail "explicit false flag is $value, want false"
  for value in TRUE 1 yes ' ' 'true false'; do
    if DEV_HTTPS_TOTP_ENROLLMENT=$value totp_enrollment_flag >/dev/null
    then
      fail "TOTP enrollment selector accepted '$value'"
    fi
  done
)

run_missing_tool_check() (
  local fixture output tool
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  rm "$fixture/fakebin/caddy"
  if output=$(PATH="$fixture/fakebin" "$FIXTURE_BASH" scripts/dev-https.sh up 2>&1); then
    fail "up accepted a missing tool"
  fi
  assert_contains "$output" 'caddy is not on PATH'
  [ ! -s "$FAKE_MUTATIONS" ] || fail "missing-tool rejection performed a mutation"

  write_fake_tools "$fixture"
  for tool in bash dirname make sha256sum readlink ps awk grep stat install sed tail chmod mv find base64 tr head python3 uname realpath date; do
    [ -e "$fixture/fakebin/$tool" ] || ln -s "$(command -v "$tool")" "$fixture/fakebin/$tool"
  done
  rm "$fixture/fakebin/ss" "$fixture/fakebin/uname"
  printf '#!/bin/sh\nprintf "Linux\\n"\n' >"$fixture/fakebin/uname"
  chmod 0700 "$fixture/fakebin/uname"
  if output=$(PATH="$fixture/fakebin" "$FIXTURE_BASH" scripts/dev-https.sh up 2>&1); then
    fail 'up accepted Linux without ss'
  fi
  assert_contains "$output" 'ss is not on PATH'
  [ ! -s "$FAKE_MUTATIONS" ] || fail 'missing ss performed a stack mutation'
  [ ! -e .dev ] || fail 'missing ss created stack state'
)

run_browser_staging_checks() (
  work=$(mktemp -d)
  trap 'rm -rf -- "$work"' EXIT
  mkdir -p "$work/context" "$work/staged"
  cp "$SOURCE_ROOT/deploy/dev-https-browser/spec-sources.txt" "$work/context/"
  mapfile -t sources <"$work/context/spec-sources.txt"
  for name in "${sources[@]}"; do printf 'fixture\n' >"$work/context/$name"; done
  # A new list member must be staged without changing another shell array.
  printf 'additional.spec.ts\n' >>"$work/context/spec-sources.txt"
  printf 'fixture\n' >"$work/context/additional.spec.ts"
  # shellcheck source=scripts/lib/dev-https-browser-stage.sh
  source "$SOURCE_LIB_DIR/dev-https-browser-stage.sh"
  stage_dev_https_browser_specs "$work/context" "$work/staged"
  [ "$(stat -c %a "$work/staged")" = 700 ] || fail 'staging directory is not private'
  [ "$(find "$work/staged" -mindepth 1 -maxdepth 1 -exec basename {} \; | LC_ALL=C sort)" = \
    "$(sort "$work/context/spec-sources.txt")" ] || fail 'staging differs from the source list'
  for path in "$work/staged/"*; do
    [ "$(stat -c '%u %a' "$path")" = "$(id -u) 600" ] || fail 'staged source owner or mode differs'
  done
  for operation in copy directory-mode file-mode; do
    mkdir "$work/failing-stage"
    if output=$({
      trap 'rm -rf -- "$work/failing-stage"' EXIT
      cp() {
        if [ "$operation" = copy ] && [ "${!#}" = "$work/failing-stage/${sources[0]}" ]; then
          printf 'partial\n' >"${!#}"
          printf 'injected staging failure\n' >&2
          return 1
        fi
        command cp "$@"
      }
      chmod() {
        if { [ "$operation" = directory-mode ] && [ "$1" = 0700 ]; } ||
          { [ "$operation" = file-mode ] && [ "${!#}" = "$work/failing-stage/${sources[0]}" ]; }; then
          printf 'injected staging failure\n' >&2
          return 1
        fi
        command chmod "$@"
      }
      # Match callers that guard staging with || and remove it on exit.
      stage_dev_https_browser_specs "$work/context" "$work/failing-stage" || exit 1
      touch "$work/accepted-stage"
    } 2>&1); then
      fail "staging accepted a failed $operation"
    fi
    assert_contains "$output" 'injected staging failure'
    [ ! -e "$work/accepted-stage" ] || fail 'failed staging reached the consumer'
    [ ! -e "$work/failing-stage" ] || fail 'caller left failed staging files behind'
  done
  rm "$work/context/additional.spec.ts"
  if output=$(stage_dev_https_browser_specs "$work/context" "$work/staged" 2>&1); then
    fail 'staging accepted a missing source'
  fi
  assert_contains "$output" 'missing or not a regular file'
  ln -s "$work/context/${sources[0]}" "$work/context/additional.spec.ts"
  if output=$(stage_dev_https_browser_specs "$work/context" "$work/staged" 2>&1); then
    fail 'staging accepted a symlink source'
  fi
  assert_contains "$output" 'missing or not a regular file'
  mkdir -p "$work/context/nested" "$work/invalid-stage"
  printf 'fixture\n' >"$work/source.ts"
  printf 'fixture\n' >"$work/context/nested/source.mjs"
  for name in ../source.ts nested/source.mjs; do
    printf '%s\n' "${sources[0]}" "$name" >"$work/context/spec-sources.txt"
    : >"$work/copy-calls"
    if output=$({
      cp() {
        printf 'copy\n' >>"$work/copy-calls"
        return 1
      }
      stage_dev_https_browser_specs "$work/context" "$work/invalid-stage"
    } 2>&1); then
      fail "staging accepted an invalid source name: $name"
    fi
    [ ! -s "$work/copy-calls" ] || fail 'invalid source name reached copy'
    assert_contains "$output" "dev-https-browser-stage: spec source $name is not a plain .ts or .mjs basename"
  done
)

# All five service exits must remain failures even when probes return success.
run_session_monitor_checks() (
  local fixture service pid output guard_pid status admission_start
  for service in mock-oauth mail-capture server web caddy; do
    fixture=$(new_fixture)
    trap 'cleanup_fixture "$fixture"' EXIT
    fixture_env "$fixture"
    cd "$fixture/repo"
    bash scripts/dev-https.sh up >"$fixture/up" 2>&1 || fail "monitor setup: $(cat "$fixture/up")"
    pid=$(<".dev/native-https/run/$service.pid")
    # A running proof must abort while its command is still sleeping.
    admission_start=$SECONDS
    bash scripts/dev-https.sh guard -- bash -c 'echo started >"$1"; sleep 120; echo unsafe >"$2"' proof "$fixture/proof-started" "$fixture/proof-completed" >"$fixture/guard" 2>&1 &
    guard_pid=$!
    status=0
    dev_https_test_wait_for_admission "$fixture" "$guard_pid" || status=$?
    [ "$status" != 1 ] || fail 'guard rejected proof admission'
    [ "$status" = 0 ] || fail 'guard is still starting at the admission deadline'
    kill -TERM "$pid"
    for _ in $(seq 1 60); do
      [ -s .dev/native-https/run/session.failed ] && break
      sleep 0.1
    done
    [ -s .dev/native-https/run/session.failed ] || fail "$service exit was not latched"
    for _ in $(seq 1 100); do ! kill -0 "$guard_pid" 2>/dev/null && break; sleep 0.1; done
    ! kill -0 "$guard_pid" 2>/dev/null || fail 'proof did not abort after service exit'
    status=0
    wait "$guard_pid" || status=$?
    dev_https_test_admission_report "$fixture" "$guard_pid" exited "$status" "$((SECONDS - admission_start))"
    [ "$status" != 0 ] || fail 'aborted proof passed'
    guard_pid=
    [ ! -e "$fixture/proof-completed" ] || fail 'unhealthy proof reached completion'
    if output=$(bash scripts/dev-https.sh status 2>&1); then fail "$service exit admitted status"; fi
    if output=$(bash scripts/dev-https.sh up 2>&1); then fail "$service exit admitted reuse"; fi
    output=$(bash scripts/dev-https.sh down 2>&1) || fail "$service exit prevented group cleanup: $output"
    [ ! -e .dev/native-https/run/session ] || fail "$service exit retained the drained session record"
    cleanup_fixture "$fixture"
    trap - EXIT
  done
)

run_session_anchor_checks() (
  local fixture record original output pid mutation foreign_pid phase tampered
  fixture=$(new_fixture)
  mv "$fixture" "$fixture with spaces"
  fixture+=" with spaces"
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  export FAKE_SIGNAL_LOG=$fixture/signals FAKE_LATCH_LOG=$fixture/latch-attempts
  export FAKE_ANCHOR_EXIT=$fixture/anchor-exited FAKE_DIAGNOSTICS_ENV=$BASH_ENV
  export BASH_ENV=$fixture/signal-audit.sh
  : >"$FAKE_SIGNAL_LOG"
  : >"$FAKE_LATCH_LOG"
  cat >"$BASH_ENV" <<'EOF'
source "$FAKE_DIAGNOSTICS_ENV"
kill() {
  if [ "${1:-}" != -0 ]; then printf '%s\n' "$*" >>"$FAKE_SIGNAL_LOG"; fi
  builtin kill "$@"
}
if [[ ${0##*/} = dev-https-session.sh ]]; then
  trap 'printf "exited\n" >"$FAKE_ANCHOR_EXIT"' EXIT
fi
EOF
  # Audit the real latch result so tampering spans monitor iterations on both
  # hosts. A quick down alone can finish before the supervisor inspects it.
  sed -n '/^latch_session_failure() {$/,/^}$/p' "$SOURCE_LIB_DIR/dev-https-identity.sh" |
    sed '1s/latch_session_failure/fixture_latch_session_failure/' >>"$fixture/repo/scripts/lib/dev-https-identity.sh"
  cat >>"$fixture/repo/scripts/lib/dev-https-identity.sh" <<'EOF'
latch_session_failure() {
  local status=0
  fixture_latch_session_failure "$@" || status=$?
  printf 'status=%s\n' "$status" >>"$FAKE_LATCH_LOG"
  return "$status"
}
EOF
  cd "$fixture/repo"
  bash scripts/dev-https.sh up >"$fixture/up" 2>&1 || fail "anchor setup: $(cat "$fixture/up")"
  detach bash -c 'echo $$ >"$1"; exec sleep 300' foreign "$fixture/foreign-pid"
  for _ in $(seq 1 50); do [ -s "$fixture/foreign-pid" ] && break; sleep 0.02; done
  foreign_pid=$(<"$fixture/foreign-pid")
  printf '%s\n' "$foreign_pid" >>"$fixture/all-pids"
  record=.dev/native-https/run/session
  original=$(<"$record")
  pid=$(sed -n 's/^pid=//p' "$record")
  for phase in before-stop during-health; do
    for mutation in 's/^launch_token=.*/launch_token=00000000000000000000000000000000/' \
      's#^checkout=.*#checkout=/foreign checkout#' 's/^pgid=.*/pgid=1/' \
      "s/^pid=.*/pid=$foreign_pid/;s/^pgid=.*/pgid=$foreign_pid/"; do
      : >"$FAKE_LATCH_LOG"
      if [ "$phase" = before-stop ]; then
        sed "$mutation" <<<"$original" >"$record"
        if output=$(bash scripts/dev-https.sh down 2>&1); then fail 'tampered anchor permitted stop'; fi
      else
        # Change the record after initial anchor validation, while stop checks
        # health. Keep the real group and final signal helper in this path.
        if output=$(FAKE_ANCHOR_MUTATION="$mutation" bash -c '
          source scripts/dev-https.sh help >/dev/null
          BASH_BIN=$(platform_realpath "$(command -v bash)")
          session_health() {
            local record
            record=$(session_record)
            sed "$FAKE_ANCHOR_MUTATION" "$record" >"$record.changed"
            cat "$record.changed" >"$record"
            rm "$record.changed"
            return 1
          }
          stop_session
        ' 2>&1); then fail 'anchor tampered during health permitted stop'; fi
      fi
      tampered=$(sed "$mutation" <<<"$original")
      [ ! -s "$FAKE_SIGNAL_LOG" ] || fail 'tampered anchor reached a signal call'
      [ "$(<"$record")" = "$tampered" ] || fail 'refused stop changed tampered evidence'
      for _ in $(seq 1 100); do
        [ ! -e "$FAKE_ANCHOR_EXIT" ] || fail 'supervisor exited after refusing a tampered record'
        [ ! -s .dev/native-https/run/session.failed ] || break
        [ "$(wc -l <"$FAKE_LATCH_LOG")" -ge 2 ] && break
        sleep 0.1
      done
      [ -s .dev/native-https/run/session.failed ] || [ "$(wc -l <"$FAKE_LATCH_LOG")" -ge 2 ] ||
        fail 'monitor did not retain or persist the observed failure'
      kill -0 "$pid" || fail 'tampered anchor signalled the stack'
      [ ! -s "$FAKE_SIGNAL_LOG" ] || fail 'tampered anchor reached a signal call'
      [ ! -s "$FAKE_STOP_LOG" ] || fail 'tampered anchor signalled services'
      printf '%s\n' "$original" >"$record"
    done
  done
  for _ in $(seq 1 100); do [ -s .dev/native-https/run/session.failed ] && break; sleep 0.1; done
  [ -s .dev/native-https/run/session.failed ] || fail 'restoring the record cleared the observed failure'
  bash scripts/dev-https.sh down >/dev/null 2>&1 || fail 'restored anchor prevented safe group stop'
  assert_log_line "$FAKE_SIGNAL_LOG" "-TERM -- -$pid"
  assert_log_line "$FAKE_SIGNAL_LOG" "-KILL -- -$pid"
  bash scripts/dev-https.sh up >"$fixture/up" 2>&1 || fail "service identity setup: $(cat "$fixture/up")"
  pid=$(sed -n 's/^pid=//p' "$record")
  # A service PID must be a direct child of the anchor in the same group.
  printf '%s\n' "$pid" >.dev/native-https/run/server.pid
  if bash scripts/dev-https.sh status >/dev/null 2>&1; then fail 'anchor substituted for service admitted'; fi
  bash scripts/dev-https.sh down >/dev/null 2>&1 || fail 'service record tampering prevented safe group stop'
  assert_log_line "$FAKE_SIGNAL_LOG" "-TERM -- -$pid"
  assert_log_line "$FAKE_SIGNAL_LOG" "-KILL -- -$pid"
  kill -0 "$foreign_pid" || fail 'owned stop signalled foreign group'
  printf '123\n' >.dev/native-https/run/server.pid
  if bash scripts/dev-https.sh up >/dev/null 2>&1; then fail 'legacy PID state admitted'; fi
  [ -s .dev/native-https/run/server.pid ] || fail 'legacy evidence removed'
)

run_session_listener_checks() (
  local fixture output phase
  for phase in descendant before after inspection sibling completion; do
    fixture=$(new_fixture)
    trap 'cleanup_fixture "$fixture"' EXIT
    fixture_env "$fixture"
    cd "$fixture/repo"
    [ "$phase" != descendant ] || export FAKE_LISTENER_CHILD=web
    bash scripts/dev-https.sh up >"$fixture/up" 2>&1 || fail "listener setup: $(cat "$fixture/up")"
    if [ "$phase" = descendant ]; then
      bash scripts/dev-https.sh status >/dev/null 2>&1 || fail 'live descendant listener was rejected'
      bash scripts/dev-https.sh down >/dev/null 2>&1 || fail 'descendant listener prevented stop'
      unset FAKE_LISTENER_CHILD
      cleanup_fixture "$fixture"
      trap - EXIT
      continue
    fi
    case $phase in
    completion)
      if bash scripts/dev-https.sh guard -- bash -c 'printf "20441\n" >"$1"' proof "$FAKE_FOREIGN_PORTS" >"$fixture/completion" 2>&1; then
        fail 'successful command bypassed completion health check'
      fi
      ;;
    sibling) : >"$fixture/sibling-listener" ;;
    before) printf '20441\n' >"$FAKE_FOREIGN_PORTS" ;;
    after) export FAKE_FOREIGN_AFTER_PROBE=1 ;;
    inspection) printf 'malformed\n' >"$fixture/listener-mode" ;;
    esac
    if output=$(bash scripts/dev-https.sh status 2>&1); then fail "$phase listener loss admitted HTTP success"; fi
    for _ in $(seq 1 60); do [ -s .dev/native-https/run/session.failed ] && break; sleep 0.1; done
    [ -s .dev/native-https/run/session.failed ] || fail "$phase listener loss not latched"
    : >"$FAKE_FOREIGN_PORTS"
    rm -f "$fixture/listener-mode" "$fixture/sibling-listener"
    unset FAKE_FOREIGN_AFTER_PROBE
    if bash scripts/dev-https.sh status >/dev/null 2>&1; then fail 'readiness cleared listener latch'; fi
    bash scripts/dev-https.sh down >/dev/null 2>&1 || fail 'listener latch prevented safe stop'
    cleanup_fixture "$fixture"
    trap - EXIT
  done
)

run_session_recovery_checks() (
  local fixture pid output record status
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  export FAKE_LEADER_EXITS_CHILD_IGNORES=server
  cd "$fixture/repo"
  bash scripts/dev-https.sh up >"$fixture/up" 2>&1 || fail "drain setup: $(cat "$fixture/up")"
  status=0
  bash scripts/dev-https.sh guard -- bash -c 'exit 7' >"$fixture/proof-exit" 2>&1 || status=$?
  [ "$status" = 7 ] || fail 'guard lost command failure status'
  pid=$(sed -n 's/^pid=//p' .dev/native-https/run/session)
  kill -TERM "$pid"
  bash scripts/dev-https.sh down >/dev/null 2>&1 || fail 'TERM-resistant descendant prevented group drain'
  ! ps -axo pgid= | awk -v g="$pid" '$1 == g { found=1 } END { exit !found }' || fail 'stop left group members'
  [ ! -e .dev/native-https/run/session ] || fail 'drained record retained'
  bash scripts/dev-https.sh up >"$fixture/up" 2>&1 || fail "recovery setup: $(cat "$fixture/up")"
  record=$(<.dev/native-https/run/session)
  pid=$(sed -n 's/^pid=//p' <<<"$record")
  kill -KILL "$pid"
  sleep 0.2
  if output=$(bash scripts/dev-https.sh down 2>&1); then fail 'anchor loss allowed automatic signaling'; fi
  [ "$(<.dev/native-https/run/session)" = "$record" ] || fail 'anchor loss discarded evidence'
  grep -Fq 'manual recovery' <<<"$output" || fail 'anchor loss omitted recovery guidance'
  kill -KILL -- "-$pid" 2>/dev/null || true
  for _ in $(seq 1 50); do
    if bash scripts/dev-https.sh down >/dev/null 2>&1; then break; fi
    sleep 0.1
  done
  [ ! -e .dev/native-https/run/session ] || fail 'absent group record not cleaned'
  unset FAKE_LEADER_EXITS_CHILD_IGNORES
  bash scripts/dev-https.sh up >"$fixture/up" 2>&1 || fail "cycle setup: $(cat "$fixture/up")"
  bash scripts/dev-https.sh guard --allow-stop -- bash -ec 'bash scripts/dev-https.sh down; bash scripts/dev-https.sh up; bash scripts/dev-https.sh down' >"$fixture/cycle" 2>&1 || fail "intentional stop/start failed: $(cat "$fixture/cycle")"
  [ ! -e .dev/native-https/run/session ] || fail 'guarded cycle retained session'
  [ ! -s "$FAKE_FORBIDDEN" ] || fail 'cleanup touched shared database'
)

# Force stop boundaries inside health inspection without launching a stack.
run_proof_transition_checks() (
  local fixture marker scenario allow_stop output fixture_python token=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa old=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  fixture=$(mktemp -d)
  trap 'rm -rf -- "$fixture"' EXIT
  ROOT=$(cd "$fixture" && pwd -P)
  DEV_DIR=$ROOT/.dev STATE_DIR=$ROOT/.dev/native-https
  RUN_DIR=$STATE_DIR/run LOG_DIR=$STATE_DIR/log BIN_DIR=$STATE_DIR/bin
  MEDIA_DIR=$STATE_DIR/media INPUT_DIR=$STATE_DIR/input CADDY_DIR=$STATE_DIR/caddy
  mkdir -p "$RUN_DIR"
  marker=$RUN_DIR/proof.abcdefghij
  # shellcheck disable=SC2034 # The sourced health helper reads the service list.
  SERVICES=(server)
  # shellcheck source=lib/dev-https-session.sh
  source "$SOURCE_LIB_DIR/dev-https-session.sh"
  # shellcheck source=lib/dev-https-identity.sh
  source "$SOURCE_LIB_DIR/dev-https-identity.sh"
  # shellcheck source=lib/dev-https-state.sh
  source "$SOURCE_LIB_DIR/dev-https-state.sh"
  die() { fail "$@"; }
  fixture_python=$(command -v python3)
  # Delete an enumerated file immediately before the real lstat. The complete
  # production validator still inspects every surviving entry.
  python3() {
    if [ "${3:-}" = "$STATE_DIR" ] && [ -n "${SCAN_VANISH_PATH:-}" ]; then
      {
        cat <<'PY'
import os
scan_lstat = os.lstat
def delete_before_stat(path):
    with open(os.environ["SCAN_AUDIT_PATH"], "a") as audit:
        audit.write(path + "\n")
    if path == os.environ["SCAN_VANISH_PATH"]:
        os.unlink(path)
    return scan_lstat(path)
os.lstat = delete_before_stat
PY
        cat
      } | "$fixture_python" "$@"
    else
      "$fixture_python" "$@"
    fi
  }
  : >"$marker"
  : >"$RUN_DIR/survivor.env"
  SESSION_TOKEN=$token
  notify_proofs stopped
  export SCAN_VANISH_PATH=$RUN_DIR/session.ready SCAN_AUDIT_PATH=$ROOT/scan-audit
  for allow_stop in 1 0; do
    printf '%s\n' "$token" >"$RUN_DIR/session.ready"
    if (proof_health "$marker" "$allow_stop") >"$ROOT/scan-output" 2>&1; then
      [ "$allow_stop" = 1 ] || fail 'unapproved file disappearance passed validation'
    else
      [ "$allow_stop" = 0 ] || fail "allowed stop rejected deletion during scan: $(cat "$ROOT/scan-output")"
    fi
    [ ! -e "$RUN_DIR/session.ready" ] || fail 'scan did not delete the enumerated file'
  done
  assert_log_line "$ROOT/scan-audit" "$RUN_DIR/survivor.env"
  unset SCAN_VANISH_PATH
  for scenario in mode symlink hardlink; do
    case $scenario in
    mode) : >"$RUN_DIR/unsafe"; chmod 0644 "$RUN_DIR/unsafe" ;;
    symlink) ln -s missing "$RUN_DIR/unsafe" ;;
    hardlink) ln "$RUN_DIR/survivor.env" "$RUN_DIR/unsafe" ;;
    esac
    if (proof_health "$marker" 1) >"$ROOT/scan-output" 2>&1; then
      fail "allowed stop accepted unsafe $scenario entry"
    fi
    rm -f -- "$RUN_DIR/unsafe"
  done
  validate_session_anchor() { validate_session_record; }
  validate_service_listeners() {
    case $scenario in
    health | published-stop | forbidden-stop)
      notify_proofs stopped || return 1
      rm -f -- "$RUN_DIR/session.ready"
      return 1
      ;;
    failed-stop) notify_proofs failed; return 1 ;;
    unexpected-exit | stale-stop) return 1 ;;
    esac
  }
  validate_effective_config() {
    notify_proofs stopped || return 1
    rm -f -- "$RUN_DIR/session.ready"
    return 1
  }
  for scenario in health config published-stop forbidden-stop failed-stop unexpected-exit stale-stop; do
    rm -f -- "$marker.stopped" "$marker.failed" "$RUN_DIR/session.failed" "$RUN_DIR/session.failed-service"
    : >"$marker"
    printf 'version=2\ncheckout=%s\npid=2\npgid=2\nlaunch_token=%s\n' "$ROOT" "$token" >"$RUN_DIR/session"
    printf '%s\n' "$token" >"$RUN_DIR/session.ready"
    # shellcheck disable=SC2034 # The sourced notifier reads the launch token.
    SESSION_TOKEN=$token
    case $scenario in
    published-stop) notify_proofs stopped ;;
    stale-stop) printf '%s\n' "$old" >"$marker.stopped" ;;
    esac
    allow_stop=1
    [ "$scenario" != forbidden-stop ] || allow_stop=0
    output=1
    proof_health "$marker" "$allow_stop" && output=0
    case $scenario in
    health | config | published-stop)
      [ "$output" = 0 ] || fail "$scenario rejected an intentional stop during inspection"
      [ ! -e "$marker.failed" ] && [ ! -e "$RUN_DIR/session.failed" ] || fail 'intentional drain latched a service failure'
      ;;
    *)
      [ "$output" = 1 ] || fail "$scenario admitted an unhealthy proof"
      [ -e "$marker.failed" ] && [ -e "$RUN_DIR/session.failed" ] || fail "$scenario lost the failure latch"
      ;;
    esac
  done
)

# A rejected proof must retain its anchor until TERM-resistant children die.
run_proof_guard_checks() (
  local fixture status group row pid state scenario fixture_python
  fixture=$(mktemp -d)
  trap 'if [ -s "$fixture/command-group" ]; then group=$(<"$fixture/command-group"); kill -KILL -- "-$group" 2>/dev/null || true; fi; rm -rf -- "$fixture"' EXIT
  ROOT=$fixture RUN_DIR=$fixture/run
  mkdir -p "$ROOT/scripts" "$RUN_DIR"
  export PROOF_FIXTURE=$fixture
  # shellcheck source=lib/dev-https-session.sh
  source "$SOURCE_LIB_DIR/dev-https-session.sh"
  cmd_status() { :; }
  proof_health() { [ ! -e "$fixture/unhealthy" ]; }
  fixture_python=$(command -v python3)
  # Deliver a real signal while the monitor sleeps during cleanup.
  python3() {
    if [ "${3:-}" = "$ROOT/scripts/dev-https.sh" ] && [ -n "${PROOF_DRAIN_SIGNAL:-}" ]; then
      {
        cat <<'PY'
import os, pathlib, signal, time
drain_sleep = time.sleep
def interrupt_drain(*_):
    (pathlib.Path(os.environ["PROOF_FIXTURE"]) / "drain-signalled").touch()
    os.kill(os.getpid(), getattr(signal, "SIG" + os.environ["PROOF_DRAIN_SIGNAL"]))
def signal_during_drain(seconds):
    if seconds == 0.2:
        signal.signal(signal.SIGALRM, interrupt_drain)
        signal.setitimer(signal.ITIMER_REAL, seconds / 2)
    try:
        drain_sleep(seconds)
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
time.sleep = signal_during_drain
PY
        cat
      } | "$fixture_python" "$@"
    else
      "$fixture_python" "$@"
    fi
  }
  cat >"$ROOT/scripts/dev-https.sh" <<'EOF'
#!/usr/bin/env bash
[ "$1" = proof-health ] && [ ! -e "$PROOF_FIXTURE/unhealthy" ]
EOF
  status=0
  cmd_guard -- bash -c 'exit 7' >"$fixture/output" 2>&1 || status=$?
  [ "$status" = 7 ] || fail 'guard lost the command failure status without a stack'
  cat >"$fixture/command.py" <<'PY'
import os, pathlib, signal, subprocess, sys, time
root = pathlib.Path(sys.argv[1])
signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
(root / "command-group").write_text(str(os.getpgrp()))
child = subprocess.Popen([sys.executable, "-I", "-c", '''import pathlib, signal, sys, time
signal.signal(signal.SIGTERM, signal.SIG_IGN)
pathlib.Path(sys.argv[1]).touch()
while True:
    time.sleep(1)
''', str(root / "child-ready")])
(root / "child-pid").write_text(str(child.pid))
deadline = time.monotonic() + 5
while not (root / "child-ready").exists():
    if time.monotonic() >= deadline:
        sys.exit(2)
    time.sleep(0.01)
(root / "unhealthy").touch()
while True:
    time.sleep(1)
PY
  for scenario in rejected TERM INT; do
    rm -f -- "$fixture/unhealthy" "$fixture/child-ready" "$fixture/drain-signalled"
    export PROOF_DRAIN_SIGNAL=
    [ "$scenario" = rejected ] || PROOF_DRAIN_SIGNAL=$scenario
    status=0
    cmd_guard -- python3 -I "$fixture/command.py" "$fixture" >"$fixture/output" 2>&1 || status=$?
    [ "$status" = 1 ] || fail "$scenario unhealthy proof command returned $status"
    [ -s "$fixture/child-pid" ] && [ -e "$fixture/child-ready" ] || fail 'guard cancellation did not reach the child handshake'
    [ "$scenario" = rejected ] || [ -e "$fixture/drain-signalled" ] || fail "$scenario did not reach cleanup"
    assert_not_contains "$(cat "$fixture/output")" BrokenPipeError
    assert_not_contains "$(cat "$fixture/output")" KeyboardInterrupt
    group=$(<"$fixture/command-group")
    ! kill -0 "$group" 2>/dev/null || fail "$scenario cleanup left its anchor alive or unjoined"
    pid=$(<"$fixture/child-pid")
    for _ in $(seq 1 50); do
      row=$(platform_process_snapshot "$pid") || break
      read -r _ _ _ _ _ state _ <<<"$row"
      [[ $state != [ZX]* ]] || break
      sleep 0.02
    done
    [ -z "$row" ] || [[ $state == [ZX]* ]] || fail "$scenario cleanup left a live TERM-resistant descendant"
  done
)

run_session_record_failure_checks() (
  local fixture
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  install -d -m 0700 .dev/native-https/run/session
  if bash scripts/dev-https.sh up >"$fixture/up" 2>&1; then fail 'invalid session record permitted launch'; fi
  [ ! -s "$fixture/all-pids" ] || fail 'record failure launched a service'
  [ ! -s "$FAKE_MUTATIONS" ] || fail 'record failure changed the stack'
  rmdir .dev/native-https/run/session
  export FIXTURE_PYTHON=$(command -v python3)
  cat >"$fixture/fakebin/python3" <<'EOF'
#!/usr/bin/env bash
if [[ ${3:-} == */run/session ]]; then exit 88; fi
exec "$FIXTURE_PYTHON" "$@"
EOF
  chmod 0700 "$fixture/fakebin/python3"
  if bash scripts/dev-https.sh up >"$fixture/up" 2>&1; then fail 'failed publication admitted launch'; fi
  [ ! -s "$fixture/all-pids" ] || fail 'publication failure started a service'
)

# Every check function in this file, in the order an unsharded run uses.
# DEV_HTTPS_TEST_SHARD=<i>/<n> runs only the checks whose index in this array
# (0-based) is congruent to i mod n; unset runs every one, in this order.
readonly CHECKS=(
  run_session_monitor_checks
  run_session_anchor_checks
  run_session_listener_checks
  run_session_recovery_checks
  run_session_record_failure_checks
  run_proof_transition_checks
  run_proof_guard_checks
  run_happy_path_and_lifecycle_checks
  run_foreign_listener_check
  run_listener_probe_failure_checks
  run_normal_native_listener_without_pid_check
  run_database_override_check
  run_active_http_stack_checks
  run_route_drift_check
  run_missing_fragment_check
  run_fragment_drift_check
  run_binary_drift_check
  run_lifecycle_command_drift_down_check
  run_partial_startup_rollback_check
  run_state_path_integrity_checks
  run_failed_build_retry_check
  run_mail_capture_failure_rollback_check
  run_server_after_capture_rollback_check
  run_mail_capture_binary_drift_check
  run_mail_capture_foreign_listener_check
  run_secret_reuse_check
  run_secret_mode_check
  run_passkey_enrollment_flag_check
  run_totp_enrollment_flag_check
  run_missing_tool_check
  run_browser_staging_checks
)

# Fails if a run_* function defined in this file is missing from CHECKS, or
# if CHECKS lists one more than once, so a new check cannot be silently left
# out of every shard.
check_checks_array_is_complete() {
  local defined listed
  defined=$(declare -F | awk '{print $3}' | grep '^run_' | sort)
  listed=$(printf '%s\n' "${CHECKS[@]}" | sort)
  [ "$(printf '%s\n' "${CHECKS[@]}" | sort -u | wc -l)" -eq "${#CHECKS[@]}" ] ||
    fail 'CHECKS lists a check more than once'
  [ "$defined" = "$listed" ] ||
    fail 'CHECKS does not list exactly the run_* checks defined in this script'
}

# Runs every check, or the slice DEV_HTTPS_TEST_SHARD=<i>/<n> selects. Named
# without a run_ prefix so it is not itself a check declare -F would collect.
select_and_run_checks() {
  local shard=${DEV_HTTPS_TEST_SHARD-} i n idx=0 check
  if [ -z "$shard" ]; then
    for check in "${CHECKS[@]}"; do "$check"; done
    return
  fi
  [[ $shard =~ ^([0-9]+)/([0-9]+)$ ]] ||
    fail 'DEV_HTTPS_TEST_SHARD must be i/n, both non-negative integers'
  i=${BASH_REMATCH[1]}
  n=${BASH_REMATCH[2]}
  [ "$n" -ge 1 ] || fail 'DEV_HTTPS_TEST_SHARD: n must be at least 1'
  [ "$i" -lt "$n" ] || fail 'DEV_HTTPS_TEST_SHARD: i must be less than n'
  for check in "${CHECKS[@]}"; do
    if [ $((idx % n)) -eq "$i" ]; then
      "$check"
    fi
    idx=$((idx + 1))
  done
}

main() {
  [ "${1-}" = --static ] && [ "$#" -eq 1 ] || fail 'usage: scripts/dev-https-test.sh --static'
  [ -f "$SOURCE_SCRIPT" ] || fail "missing production script: $SOURCE_SCRIPT"
  compgen -G "$SOURCE_LIB_DIR/dev-https-*.sh" >/dev/null || fail "missing production library files: $SOURCE_LIB_DIR"
  bash "$SOURCE_ROOT/scripts/test/dev-https-diagnostics-test.sh"
  check_checks_array_is_complete
  case ${DEV_HTTPS_TEST_CASE-} in
  session-anchor) run_session_anchor_checks; return ;;
  session-monitor) run_session_monitor_checks; return ;;
  session-listeners) run_session_listener_checks; return ;;
  session-recovery) run_session_recovery_checks; return ;;
  session-record) run_session_record_failure_checks; return ;;
  proof-transitions) run_proof_transition_checks; return ;;
  proof-guard) run_proof_guard_checks; return ;;
  browser-staging) run_browser_staging_checks; return ;;
  manifest) run_happy_path_and_lifecycle_checks; return ;;
  listener-probe) run_listener_probe_failure_checks; return ;;
  native-ports) run_normal_native_listener_without_pid_check; return ;;
  database) run_database_override_check; return ;;
  identity) run_session_anchor_checks; return ;;
  identity-tamper) run_session_anchor_checks; return ;;
  group-drain) run_session_recovery_checks; return ;;
  member-exit-race) run_session_anchor_checks; return ;;
  member-identity-races) run_session_anchor_checks; return ;;
  binary-drift) run_binary_drift_check; return ;;
  lifecycle-drift) run_lifecycle_command_drift_down_check; return ;;
  rollback) run_partial_startup_rollback_check; return ;;
  identity-write-rollback) run_session_record_failure_checks; return ;;
  launch-completion-rollback) run_session_record_failure_checks; return ;;
  immediate-exit-rollback) run_partial_startup_rollback_check; return ;;
  absent-completed-launch) run_session_recovery_checks; return ;;
  path-integrity) run_state_path_integrity_checks; return ;;
  build-retry) run_failed_build_retry_check; return ;;
  mail-capture-rollback) run_mail_capture_failure_rollback_check; return ;;
  server-after-capture) run_server_after_capture_rollback_check; return ;;
  mail-capture-drift) run_mail_capture_binary_drift_check; return ;;
  mail-capture-foreign) run_mail_capture_foreign_listener_check; return ;;
  mail-capture-group-drain) run_session_recovery_checks; return ;;
  secret-reuse) run_secret_reuse_check; return ;;
  secret-mode) run_secret_mode_check; return ;;
  passkey-flag) run_passkey_enrollment_flag_check; return ;;
  totp-flag) run_totp_enrollment_flag_check; return ;;
  missing-tool) run_missing_tool_check; return ;;
  '') ;;
  *) fail "unknown DEV_HTTPS_TEST_CASE=${DEV_HTTPS_TEST_CASE}" ;;
  esac
  select_and_run_checks
  printf '%s\n' 'dev-https static tests: PASS'
}

main "$@"
