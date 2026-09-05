#!/usr/bin/env bash
# Hermetic lifecycle contract tests for scripts/dev-https.sh. The production
# script runs only inside temporary repository fixtures with controlled tools.
set -Eeuo pipefail
umask 077

cd "$(dirname "${BASH_SOURCE[0]}")/.."
readonly SOURCE_ROOT=$PWD
readonly SOURCE_SCRIPT=$SOURCE_ROOT/scripts/dev-https.sh

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
  local fixture=$1 file pid pgid
  for file in "$fixture"/repo/.dev/native-https/run/*.pid; do
    [ -s "$file" ] || continue
    pid=$(<"$file")
    [[ $pid =~ ^[0-9]+$ ]] || continue
    kill -TERM -- "-$pid" 2>/dev/null || true
    kill -KILL -- "-$pid" 2>/dev/null || true
  done
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
case $name in
mock-oauth)
  printf 'service:%s host=%s port=%s origin=%s client=%s db=%s\n' \
    "$name" "$LISTEN_HOST" "$PORT" "$PUBLIC_ORIGIN" "$GOOGLE_CLIENT_ID" "${DATABASE_URL-}" >>"$FAKE_EFFECTS"
  ;;
server)
  printf 'service:%s host=%s port=%s origin=%s render=%s app=%s renderer=%s issuer=%s db=%s\n' \
    "$name" "$LISTEN_HOST" "$PORT" "$PUBLIC_ORIGIN" "$PUBLIC_RENDER_ORIGIN" "$APP_BUILD_DIGEST" "$PUBLIC_RENDERER_BUILD_DIGEST" "$GOOGLE_OIDC_ISSUER_URL" "$DATABASE_URL" >>"$FAKE_EFFECTS"
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
  printf 'migrate:db=%s\n' "$DATABASE_URL" >>"$FAKE_EFFECTS"
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
[ -f "$PWD/packages/publicroots/public-roots.v6.json" ]
[ -f "$PWD/deploy/caddy/public-roots.generated.caddy" ]
[ "$(sha256sum "$PWD/packages/publicroots/public-roots.v6.json" | awk '{print $1}')" = "$FAKE_REGISTRY_SHA256" ]
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
[ "$(count 'bind 127.0.0.1')" = 1 ]
[ "$(count 'tls internal')" = 1 ]
[ "$(count 'skip_install_trust')" = 1 ]
[ "$(count 'auto_https disable_redirects')" = 1 ]
[ "$(count 'reverse_proxy 127.0.0.1:20441 {')" = 2 ]
[ "$(count 'reverse_proxy 127.0.0.1:20440')" = 2 ]
[ "$(count '@uat_google_authorize path /__uat/oauth/google/authorize')" = 1 ]
[ "$(count 'reverse_proxy 127.0.0.1:20442')" = 1 ]
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
exit 0
EOF

  cat >"$fakebin/ss" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
case ${FAKE_SS_MODE-} in
nonzero)
  printf '%s\n' 'injected ss failure' >&2
  exit 9
  ;;
malformed)
  printf '%s\n' 'this is not an ss listener record'
  exit 0
  ;;
esac
port=${*##*:}
port=${port//[^0-9]/}
if grep -Fqx -- "$port" "$FAKE_FOREIGN_PORTS" 2>/dev/null; then
  printf 'LISTEN 0 1 127.0.0.1:%s 0.0.0.0:*\n' "$port"
  exit 0
fi
case $port in
20440) name=web ;;
20441) name=server ;;
20442) name=mock-oauth ;;
20443) name=caddy ;;
20444) name=mail-capture ;;
*) exit 0 ;;
esac
pidfile="$PWD/.dev/native-https/run/$name.pid"
if [ -s "$pidfile" ]; then
  pid=$(<"$pidfile")
  if kill -0 "$pid" 2>/dev/null; then
    printf 'LISTEN 0 1 127.0.0.1:%s 0.0.0.0:*\n' "$port"
  fi
fi
EOF

  cat >"$fakebin/ps" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [ -n "${FAKE_EXIT_RACE_PID-}" ]; then
  case "$*" in
  '-eo pid=,pgid=,sid=')
    /usr/bin/ps "$@"
    calls=0
    [ ! -f "$FAKE_EXIT_RACE_GROUP_CALLS" ] || calls=$(<"$FAKE_EXIT_RACE_GROUP_CALLS")
    calls=$((calls + 1))
    printf '%s\n' "$calls" >"$FAKE_EXIT_RACE_GROUP_CALLS"
    if [ "$calls" -eq 2 ]; then
      printf '%s %s %s\n' "$FAKE_EXIT_RACE_PID" "$FAKE_EXIT_RACE_PGID" "$FAKE_EXIT_RACE_PGID"
    fi
    exit 0
    ;;
  "-o pgid=,sid=,stat= -p $FAKE_EXIT_RACE_PID")
    calls=0
    [ ! -f "$FAKE_EXIT_RACE_STATE_CALLS" ] || calls=$(<"$FAKE_EXIT_RACE_STATE_CALLS")
    calls=$((calls + 1))
    printf '%s\n' "$calls" >"$FAKE_EXIT_RACE_STATE_CALLS"
    state=S
    [ "$calls" -eq 1 ] || state=Z
    printf '%s %s %s\n' "$FAKE_EXIT_RACE_PGID" "$FAKE_EXIT_RACE_PGID" "$state"
    exit 0
    ;;
  esac
fi
exec /usr/bin/ps "$@"
EOF

  cat >"$fakebin/chmod" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
target=${!#}
if [ "${FAKE_FAIL_IDENTITY_WRITE-}" = server ] && [[ $target == */server.identity ]]; then
  printf '%s\n' 'injected-invalid-identity' >"$target"
  exit 77
fi
if [ "${FAKE_FAIL_LAUNCH_COMPLETE-}" = server ] && [[ $target == */server.launch.tmp.* ]]; then
  exit 78
fi
exec /usr/bin/chmod "$@"
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

  cat >"$fakebin/setsid" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'setsid\n' >>"$FAKE_MUTATIONS"
exec /usr/bin/setsid "$@"
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
  mkdir -p "$fixture/repo/scripts" "$fixture/repo/deploy/caddy" \
    "$fixture/repo/packages/publicroots" \
    "$fixture/repo/apps/server" "$fixture/repo/apps/web/node_modules"
  cp "$SOURCE_SCRIPT" "$fixture/repo/scripts/dev-https.sh"
  cp "$SOURCE_ROOT/scripts/generate-public-roots.mjs" "$fixture/repo/scripts/generate-public-roots.mjs"
  cp "$SOURCE_ROOT/scripts/chromium-path.mjs" "$fixture/repo/scripts/chromium-path.mjs"
  cp "$SOURCE_ROOT/deploy/caddy/Caddyfile" "$fixture/repo/deploy/caddy/Caddyfile"
  cp "$SOURCE_ROOT/deploy/caddy/public-roots.generated.caddy" "$fixture/repo/deploy/caddy/public-roots.generated.caddy"
  cp "$SOURCE_ROOT/packages/publicroots/public-roots.v6.json" "$fixture/repo/packages/publicroots/public-roots.v6.json"
  cat >"$fixture/repo/Makefile" <<'EOF'
.PHONY: tools-check test-db-up
tools-check:
	@printf 'tools-check\n' >>"$(FAKE_MUTATIONS)"
test-db-up:
	@printf 'test-db-up\n' >>"$(FAKE_MUTATIONS)"
EOF
  : >"$fixture/mutations"
  : >"$fixture/effects"
  : >"$fixture/stops"
  : >"$fixture/forbidden"
  : >"$fixture/foreign-ports"
  : >"$fixture/podman-ps"
  : >"$fixture/all-pids"
  write_fake_tools "$fixture"
  printf '%s' "$fixture"
}

fixture_env() {
  local fixture=$1
  export PATH="$fixture/fakebin:/usr/bin:/bin"
  export FAKE_MUTATIONS=$fixture/mutations
  export FAKE_EFFECTS=$fixture/effects
  export FAKE_STOP_LOG=$fixture/stops
  export FAKE_FORBIDDEN=$fixture/forbidden
  export FAKE_FOREIGN_PORTS=$fixture/foreign-ports
  export FAKE_PODMAN_PS=$fixture/podman-ps
  export DEV_HTTPS_PID_AUDIT_FILE=$fixture/all-pids
  export FAKE_GO_FAIL_MARKER=$fixture/go-fail-marker
  export FAKE_REGISTRY_SHA256=$(sha256sum "$fixture/repo/packages/publicroots/public-roots.v6.json" | awk '{print $1}')
  export FAKE_FRAGMENT_SHA256=$(sha256sum "$fixture/repo/deploy/caddy/public-roots.generated.caddy" | awk '{print $1}')
  export DEV_HTTPS_STOP_TERM_ATTEMPTS=5
  export DEV_HTTPS_STOP_KILL_ATTEMPTS=20
}

run_happy_path_and_lifecycle_checks() (
  local fixture output before after mode lines manifest service
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"

  output=$(bash scripts/dev-https.sh up 2>&1) || fail "happy-path up failed: $output"
  assert_not_contains "$output" 'not-a-secret-local-google'
  assert_not_contains "$output" 'GOOGLE_CLIENT_SECRET='
  [ ! -s "$FAKE_FORBIDDEN" ] || fail "up invoked a forbidden command"
  assert_log_line "$FAKE_MUTATIONS" 'test-db-up'
  assert_log_line "$FAKE_MUTATIONS" 'go-build:migrate:./cmd/migrate'
  assert_log_line "$FAKE_MUTATIONS" 'go-build:mock-oauth:./cmd/mock-oauth'
  assert_log_line "$FAKE_MUTATIONS" 'go-build:server:./cmd/server'
  assert_log_line "$FAKE_EFFECTS" 'migrate:db=postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable'
  assert_log_line "$FAKE_EFFECTS" 'service:mock-oauth host=127.0.0.1 port=20442 origin=https://localhost:20443 client=aboutme-local-google db='
  assert_log_line "$FAKE_EFFECTS" 'service:server host=127.0.0.1 port=20441 origin=https://localhost:20443 render=http://127.0.0.1:20440 app=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa renderer=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb issuer=http://127.0.0.1:20442/google db=postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable'
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
  assert_contains "$server_env" 'PROVIDER_LOGIN_ENABLED=true'

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
  assert_contains "$manifest" 'mcp_enabled=true'
  assert_contains "$manifest" 'public_render_origin=http://127.0.0.1:20440'
  assert_contains "$manifest" 'app_build_digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
  assert_contains "$manifest" 'public_renderer_build_digest=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
  assert_contains "$manifest" 'registry_source_sha256='
  assert_contains "$manifest" 'generated_fragment_sha256='
  assert_contains "$manifest" 'effective_caddyfile_sha256='
  assert_contains "$manifest" 'generated_route_sha256='
  assert_contains "$manifest" 'server_expected_executable='
  assert_contains "$manifest" 'server_expected_cmdline_sha256='
  assert_contains "$manifest" 'mail_capture_port=20444'
  assert_contains "$manifest" 'mail_capture_binary_sha256='
  assert_contains "$manifest" 'mail_secrets_sha256='
  assert_not_contains "$manifest" 'not-a-secret-local-google'
  for service in mock-oauth mail-capture server web caddy; do
    mode=$(stat -c '%a' ".dev/native-https/run/$service.identity")
    [ "$mode" = 600 ] || fail "$service identity mode is $mode, want 600"
    mode=$(stat -c '%a' ".dev/native-https/run/$service.launch")
    [ "$mode" = 600 ] || fail "$service launch evidence mode is $mode, want 600"
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

  output=$(bash scripts/dev-https.sh down 2>&1) || fail "down failed: $output"
  mapfile -t stops <"$FAKE_STOP_LOG"
  [ "${stops[*]}" = 'stop:caddy stop:web stop:server stop:mail-capture stop:mock-oauth' ] || \
    fail "stop order was: ${stops[*]}"
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
  /usr/bin/setsid --fork bash -c 'echo $$ >"$1"; exec sleep 300' active-http .dev/server.pid
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

run_foreign_ownership_down_check() (
  local fixture output foreign_pid
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  install -d -m 0755 .dev
  install -d -m 0700 .dev/native-https .dev/native-https/run
  /usr/bin/setsid --fork bash -c 'echo $$ >"$1"; exec sleep 300' foreign .dev/native-https/run/server.pid
  for _ in $(seq 1 50); do [ -s .dev/native-https/run/server.pid ] && break; sleep 0.02; done
  foreign_pid=$(<.dev/native-https/run/server.pid)
  printf '%s\n' "$foreign_pid" >>"$fixture/all-pids"
  printf '%s\n' 'foreign-identity-evidence' >.dev/native-https/run/server.identity
  if output=$(bash scripts/dev-https.sh down 2>&1); then
    fail "down accepted a PID it could not prove it owned"
  fi
  assert_contains "$output" 'does not match its exact owned process identity'
  kill -0 "$foreign_pid" 2>/dev/null || fail "down killed a foreign process"
  [ -f .dev/native-https/run/server.pid ] || fail "down removed foreign ownership evidence"
  assert_log_line .dev/native-https/run/server.identity 'foreign-identity-evidence'
)

run_process_group_drain_check() (
  local fixture output pid member
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  export FAKE_LEADER_EXITS_CHILD_IGNORES=server
  cd "$fixture/repo"
  output=$(bash scripts/dev-https.sh up 2>&1) || fail "group-drain setup failed: $output"
  pid=$(<.dev/native-https/run/server.pid)
  output=$(bash scripts/dev-https.sh down 2>&1) || fail "group-drain down failed: $output"
  while IFS= read -r member; do
    [ -n "$member" ] || continue
    fail "down left process $member in owned process group $pid"
  done < <(ps -eo pid=,pgid= | awk -v pgid="$pid" '$2 == pgid { print $1 }')
  [ ! -e .dev/native-https/run/server.pid ] || fail "down retained server PID after group drained"
  [ ! -e .dev/native-https/run/server.identity ] || fail "down retained server identity after group drained"
)

run_group_member_exit_race_check() (
  local fixture output foreign_pid web_pid
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  output=$(bash scripts/dev-https.sh up 2>&1) || fail "group-member exit-race setup failed: $output"
  web_pid=$(<.dev/native-https/run/web.pid)
  /usr/bin/setsid --fork bash -c 'echo $$ >"$1"; exec sleep 300' foreign "$fixture/foreign-pid"
  for _ in $(seq 1 50); do [ -s "$fixture/foreign-pid" ] && break; sleep 0.02; done
  foreign_pid=$(<"$fixture/foreign-pid")
  printf '%s\n' "$foreign_pid" >>"$fixture/all-pids"
  export FAKE_EXIT_RACE_PID=$foreign_pid
  export FAKE_EXIT_RACE_PGID=$web_pid
  export FAKE_EXIT_RACE_GROUP_CALLS=$fixture/exit-race-group-calls
  export FAKE_EXIT_RACE_STATE_CALLS=$fixture/exit-race-state-calls
  output=$(bash scripts/dev-https.sh down 2>&1) || fail "down rejected a group member that exited during identity validation: $output"
  kill -0 "$foreign_pid" 2>/dev/null || fail 'down signalled the simulated exiting process'
)

run_identity_field_tamper_check() (
  local fixture output identity original pid mutation
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  output=$(bash scripts/dev-https.sh up 2>&1) || fail "identity-tamper setup failed: $output"
  identity=.dev/native-https/run/server.identity
  original=$(<"$identity")
  pid=$(<.dev/native-https/run/server.pid)
  for mutation in \
    's/^starttime=.*/starttime=1/' \
    's#^expected_executable=.*#expected_executable=/bin/false#' \
    's/^expected_cmdline_sha256=.*/expected_cmdline_sha256=0000000000000000000000000000000000000000000000000000000000000000/'; do
    sed -i "$mutation" "$identity"
    if output=$(bash scripts/dev-https.sh down 2>&1); then
      fail "down accepted tampered service identity"
    fi
    assert_contains "$output" 'does not match its exact owned process identity'
    kill -0 "$pid" 2>/dev/null || fail "identity rejection signalled the owned server"
    [ ! -s "$FAKE_STOP_LOG" ] || fail "identity rejection signalled a prevalidated service"
    printf '%s\n' "$original" >"$identity"
  done
  bash scripts/dev-https.sh down >/dev/null 2>&1 || fail "restored identity did not permit safe down"
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
  sed -i 's/child=\$!/child=\$! # desired-source-drift/' scripts/dev-https.sh
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

run_identity_write_rollback_check() (
  local fixture output foreign_pid service member
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  export FAKE_FAIL_IDENTITY_WRITE=server
  cd "$fixture/repo"
  /usr/bin/setsid --fork bash -c 'echo $$ >"$1"; exec sleep 300' foreign "$fixture/foreign-pid"
  for _ in $(seq 1 50); do [ -s "$fixture/foreign-pid" ] && break; sleep 0.02; done
  foreign_pid=$(<"$fixture/foreign-pid")
  printf '%s\n' "$foreign_pid" >>"$fixture/all-pids"
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail 'up accepted injected identity persistence failure'
  fi
  assert_contains "$output" 'startup failed'
  kill -0 "$foreign_pid" 2>/dev/null || fail 'rollback signalled an unrelated process group'
  while IFS= read -r member; do
    [ -n "$member" ] || continue
    [ "$member" = "$foreign_pid" ] && continue
    ! kill -0 "$member" 2>/dev/null || fail "identity-write rollback left owned process alive: $member"
  done <"$fixture/all-pids"
  for service in mock-oauth server web caddy; do
    [ ! -e ".dev/native-https/run/$service.pid" ] || fail "rollback retained $service PID"
    [ ! -e ".dev/native-https/run/$service.identity" ] || fail "rollback retained $service identity"
    [ ! -e ".dev/native-https/run/$service.launch" ] || fail "rollback retained $service launch evidence"
    [ ! -e ".dev/native-https/run/$service.env" ] || fail "rollback retained $service environment"
  done
)

run_launch_completion_failure_rollback_check() (
  local fixture output foreign_pid service member
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  export FAKE_FAIL_LAUNCH_COMPLETE=server
  cd "$fixture/repo"
  /usr/bin/setsid --fork bash -c 'echo $$ >"$1"; exec sleep 300' foreign "$fixture/foreign-pid"
  for _ in $(seq 1 50); do [ -s "$fixture/foreign-pid" ] && break; sleep 0.02; done
  foreign_pid=$(<"$fixture/foreign-pid")
  printf '%s\n' "$foreign_pid" >>"$fixture/all-pids"
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail 'up accepted injected launch-record completion failure'
  fi
  assert_contains "$output" 'startup failed'
  assert_log_line "$FAKE_MUTATIONS" 'start:server'
  kill -0 "$foreign_pid" 2>/dev/null || fail 'prepared-launch rollback signalled an unrelated process group'
  while IFS= read -r member; do
    [ -n "$member" ] || continue
    [ "$member" = "$foreign_pid" ] && continue
    ! kill -0 "$member" 2>/dev/null || fail "prepared-launch rollback left owned process alive: $member"
  done <"$fixture/all-pids"
  for service in mock-oauth server web caddy; do
    [ ! -e ".dev/native-https/run/$service.pid" ] || fail "prepared-launch rollback retained $service PID"
    [ ! -e ".dev/native-https/run/$service.identity" ] || fail "prepared-launch rollback retained $service identity"
    [ ! -e ".dev/native-https/run/$service.launch" ] || fail "prepared-launch rollback retained $service launch evidence"
    [ ! -e ".dev/native-https/run/$service.env" ] || fail "prepared-launch rollback retained $service environment"
  done
)

run_immediate_precompletion_exit_rollback_check() (
  local fixture output foreign_pid service member
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  export FAKE_EXIT_BEFORE_COMPLETION=mock-oauth
  cd "$fixture/repo"
  /usr/bin/setsid --fork bash -c 'echo $$ >"$1"; exec sleep 300' foreign "$fixture/foreign-pid"
  for _ in $(seq 1 50); do [ -s "$fixture/foreign-pid" ] && break; sleep 0.02; done
  foreign_pid=$(<"$fixture/foreign-pid")
  printf '%s\n' "$foreign_pid" >>"$fixture/all-pids"
  if output=$(bash scripts/dev-https.sh up 2>&1); then
    fail 'up accepted a service that exited before launch identity completion'
  fi
  assert_contains "$output" 'startup failed'
  assert_log_line "$FAKE_MUTATIONS" 'start:mock-oauth'
  kill -0 "$foreign_pid" 2>/dev/null || fail 'immediate-exit rollback signalled an unrelated process group'
  while IFS= read -r member; do
    [ -n "$member" ] || continue
    [ "$member" = "$foreign_pid" ] && continue
    ! kill -0 "$member" 2>/dev/null || fail "immediate-exit rollback left owned process alive: $member"
  done <"$fixture/all-pids"
  for service in mock-oauth server web caddy; do
    [ ! -e ".dev/native-https/run/$service.pid" ] || fail "immediate-exit rollback retained $service PID"
    [ ! -e ".dev/native-https/run/$service.identity" ] || fail "immediate-exit rollback retained $service identity"
    [ ! -e ".dev/native-https/run/$service.launch" ] || fail "immediate-exit rollback retained $service launch evidence"
    [ ! -e ".dev/native-https/run/$service.env" ] || fail "immediate-exit rollback retained $service environment"
  done
)

run_absent_completed_launch_cleanup_check() (
  local fixture output pid service
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  output=$(bash scripts/dev-https.sh up 2>&1) || fail "setup up failed: $output"
  pid=$(<.dev/native-https/run/caddy.pid)
  kill -TERM -- "-$pid"
  for _ in $(seq 1 100); do
    ! kill -0 "$pid" 2>/dev/null && break
    sleep 0.02
  done
  ! kill -0 "$pid" 2>/dev/null || fail 'completed-launch fixture did not exit'
  rm -- .dev/native-https/run/caddy.identity
  output=$(bash scripts/dev-https.sh down 2>&1) || \
    fail "down could not clean an absent completed launch: $output"
  for service in mock-oauth server web caddy; do
    [ ! -e ".dev/native-https/run/$service.pid" ] || fail "absent launch cleanup retained $service PID"
    [ ! -e ".dev/native-https/run/$service.identity" ] || fail "absent launch cleanup retained $service identity"
    [ ! -e ".dev/native-https/run/$service.launch" ] || fail "absent launch cleanup retained $service launch evidence"
    [ ! -e ".dev/native-https/run/$service.env" ] || fail "absent launch cleanup retained $service environment"
  done
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
  [ "$(find "$outside" -mindepth 1 -maxdepth 1 -printf '%f\n')" = sentinel ] || \
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

run_mail_capture_group_drain_check() (
  local fixture output pid member
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  export FAKE_LEADER_EXITS_CHILD_IGNORES=mail-capture
  cd "$fixture/repo"
  output=$(bash scripts/dev-https.sh up 2>&1) || fail "mail-capture group-drain setup failed: $output"
  pid=$(<.dev/native-https/run/mail-capture.pid)
  output=$(bash scripts/dev-https.sh down 2>&1) || fail "mail-capture group-drain down failed: $output"
  while IFS= read -r member; do
    [ -n "$member" ] || continue
    fail "down left process $member in mail-capture process group $pid"
  done < <(ps -eo pid=,pgid= | awk -v pgid="$pid" '$2 == pgid { print $1 }')
  [ ! -e .dev/native-https/run/mail-capture.pid ] || fail 'down retained mail-capture PID after group drained'
  [ ! -e .dev/native-https/run/mail-capture.identity ] || fail 'down retained mail-capture identity after group drained'
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

run_missing_tool_check() (
  local fixture output
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  rm "$fixture/fakebin/caddy"
  if output=$(PATH="$fixture/fakebin" /bin/bash scripts/dev-https.sh up 2>&1); then
    fail "up accepted a missing tool"
  fi
  assert_contains "$output" 'caddy is not on PATH'
  [ ! -s "$FAKE_MUTATIONS" ] || fail "missing-tool rejection performed a mutation"
)

main() {
  [ "${1-}" = --static ] && [ "$#" -eq 1 ] || fail 'usage: scripts/dev-https-test.sh --static'
  [ -f "$SOURCE_SCRIPT" ] || fail "missing production script: $SOURCE_SCRIPT"
  case ${DEV_HTTPS_TEST_CASE-} in
  manifest) run_happy_path_and_lifecycle_checks; return ;;
  listener-probe) run_listener_probe_failure_checks; return ;;
  native-ports) run_normal_native_listener_without_pid_check; return ;;
  database) run_database_override_check; return ;;
  identity) run_foreign_ownership_down_check; return ;;
  identity-tamper) run_identity_field_tamper_check; return ;;
  group-drain) run_process_group_drain_check; return ;;
  member-exit-race) run_group_member_exit_race_check; return ;;
  binary-drift) run_binary_drift_check; return ;;
  lifecycle-drift) run_lifecycle_command_drift_down_check; return ;;
  rollback) run_partial_startup_rollback_check; return ;;
  identity-write-rollback) run_identity_write_rollback_check; return ;;
  launch-completion-rollback) run_launch_completion_failure_rollback_check; return ;;
  immediate-exit-rollback) run_immediate_precompletion_exit_rollback_check; return ;;
  absent-completed-launch) run_absent_completed_launch_cleanup_check; return ;;
  path-integrity) run_state_path_integrity_checks; return ;;
  build-retry) run_failed_build_retry_check; return ;;
  mail-capture-rollback) run_mail_capture_failure_rollback_check; return ;;
  server-after-capture) run_server_after_capture_rollback_check; return ;;
  mail-capture-drift) run_mail_capture_binary_drift_check; return ;;
  mail-capture-foreign) run_mail_capture_foreign_listener_check; return ;;
  mail-capture-group-drain) run_mail_capture_group_drain_check; return ;;
  secret-reuse) run_secret_reuse_check; return ;;
  secret-mode) run_secret_mode_check; return ;;
  '') ;;
  *) fail "unknown DEV_HTTPS_TEST_CASE=${DEV_HTTPS_TEST_CASE}" ;;
  esac
  run_happy_path_and_lifecycle_checks
  run_foreign_listener_check
  run_listener_probe_failure_checks
  run_normal_native_listener_without_pid_check
  run_database_override_check
  run_active_http_stack_checks
  run_route_drift_check
  run_missing_fragment_check
  run_fragment_drift_check
  run_foreign_ownership_down_check
  run_identity_field_tamper_check
  run_process_group_drain_check
  run_group_member_exit_race_check
  run_binary_drift_check
  run_lifecycle_command_drift_down_check
  run_partial_startup_rollback_check
  run_identity_write_rollback_check
  run_launch_completion_failure_rollback_check
  run_immediate_precompletion_exit_rollback_check
  run_absent_completed_launch_cleanup_check
  run_state_path_integrity_checks
  run_failed_build_retry_check
  run_mail_capture_failure_rollback_check
  run_server_after_capture_rollback_check
  run_mail_capture_binary_drift_check
  run_mail_capture_foreign_listener_check
  run_mail_capture_group_drain_check
  run_secret_reuse_check
  run_secret_mode_check
  run_missing_tool_check
  printf '%s\n' 'dev-https static tests: PASS'
}

main "$@"
