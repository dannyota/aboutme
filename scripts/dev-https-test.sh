#!/usr/bin/env bash
# Hermetic lifecycle contract tests for scripts/dev-https.sh. The production
# script runs only inside temporary repository fixtures with controlled tools.
set -Eeuo pipefail

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
  printf 'service:%s host=%s port=%s origin=%s issuer=%s db=%s\n' \
    "$name" "$LISTEN_HOST" "$PORT" "$PUBLIC_ORIGIN" "$GOOGLE_OIDC_ISSUER_URL" "$DATABASE_URL" >>"$FAKE_EFFECTS"
  ;;
web) printf 'service:web\n' >>"$FAKE_EFFECTS" ;;
caddy)
  printf 'service:caddy\n' >>"$FAKE_EFFECTS"
  install -d -m 0700 "$XDG_DATA_HOME/caddy/pki/authorities/local"
  printf '%s\n' 'fake local root certificate' >"$XDG_DATA_HOME/caddy/pki/authorities/local/root.crt"
  ;;
esac
if [ "${FAKE_EXIT_SERVICE-}" = "$name" ]; then
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
mkdir -p "$(dirname "$out")"
cat >"$out" <<'INNER'
#!/usr/bin/env bash
set -Eeuo pipefail
name=${0##*/}
if [ "$name" = migrate ]; then
  printf 'migrate:db=%s\n' "$DATABASE_URL" >>"$FAKE_EFFECTS"
  exit 0
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
[ "$(count 'reverse_proxy 127.0.0.1:20441 {')" = 1 ]
[ "$(count 'reverse_proxy 127.0.0.1:20440')" = 1 ]
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

  cat >"$fakebin/chmod" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
target=${!#}
if [ "${FAKE_FAIL_IDENTITY_WRITE-}" = server ] && [[ $target == */server.identity ]]; then
  printf '%s\n' 'injected-invalid-identity' >"$target"
  exit 77
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
    "$fixture/repo/apps/server" "$fixture/repo/apps/web/node_modules"
  cp "$SOURCE_SCRIPT" "$fixture/repo/scripts/dev-https.sh"
  cp "$SOURCE_ROOT/deploy/caddy/Caddyfile" "$fixture/repo/deploy/caddy/Caddyfile"
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
  assert_log_line "$FAKE_EFFECTS" 'service:server host=127.0.0.1 port=20441 origin=https://localhost:20443 issuer=http://127.0.0.1:20442/google db=postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable'
  assert_log_line "$FAKE_EFFECTS" 'npm:run dev -- --port 20440 --host 127.0.0.1'
  assert_log_line "$FAKE_EFFECTS" 'caddy-config-validated'

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
  assert_contains "$manifest" 'generated_route_sha256='
  assert_contains "$manifest" 'server_expected_executable='
  assert_contains "$manifest" 'server_expected_cmdline_sha256='
  assert_not_contains "$manifest" 'not-a-secret-local-google'
  for service in mock-oauth server web caddy; do
    mode=$(stat -c '%a' ".dev/native-https/run/$service.identity")
    [ "$mode" = 600 ] || fail "$service identity mode is $mode, want 600"
    mode=$(stat -c '%a' ".dev/native-https/run/$service.launch")
    [ "$mode" = 600 ] || fail "$service launch evidence mode is $mode, want 600"
    mode=$(stat -c '%a' ".dev/native-https/run/$service.env")
    [ "$mode" = 600 ] || fail "$service environment mode is $mode, want 600"
  done

  output=$(bash scripts/dev-https.sh status 2>&1) || fail "status failed: $output"
  assert_contains "$output" 'mock-oauth'
  assert_contains "$output" '20443'
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
  assert_contains "$output" '[REDACTED]'
  assert_contains "$output" 'harmless=ok'
  assert_contains "$output" 'next=safe'
  assert_contains "$output" '"status":"ok"'

  output=$(bash scripts/dev-https.sh down 2>&1) || fail "down failed: $output"
  mapfile -t stops <"$FAKE_STOP_LOG"
  [ "${stops[*]}" = 'stop:caddy stop:web stop:server stop:mock-oauth' ] || \
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

run_foreign_ownership_down_check() (
  local fixture output foreign_pid
  fixture=$(new_fixture)
  trap 'cleanup_fixture "$fixture"' EXIT
  fixture_env "$fixture"
  cd "$fixture/repo"
  mkdir -p .dev/native-https/run
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
  binary-drift) run_binary_drift_check; return ;;
  lifecycle-drift) run_lifecycle_command_drift_down_check; return ;;
  rollback) run_partial_startup_rollback_check; return ;;
  identity-write-rollback) run_identity_write_rollback_check; return ;;
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
  run_foreign_ownership_down_check
  run_identity_field_tamper_check
  run_process_group_drain_check
  run_binary_drift_check
  run_lifecycle_command_drift_down_check
  run_partial_startup_rollback_check
  run_identity_write_rollback_check
  run_missing_tool_check
  printf '%s\n' 'dev-https static tests: PASS'
}

main "$@"
