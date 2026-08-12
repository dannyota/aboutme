#!/usr/bin/env bash

# Adversarial contract tests for local gates and development-stack safety.
# Every exercised script runs from a temporary repository with a fake PATH.
set -u -o pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SYSTEM_PATH=/usr/bin:/bin
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-ci-scan-test.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

passes=0
failures=0

note_failure() {
  printf '    %s\n' "$*" >&2
  return 1
}

run_test() {
  local name=$1
  shift
  if "$@"; then
    printf 'ok - %s\n' "$name"
    passes=$((passes + 1))
  else
    printf 'not ok - %s\n' "$name"
    failures=$((failures + 1))
  fi
}

make_logging_stub() {
  local path=$1 command_name=$2
  mkdir -p "$(dirname "$path")"
  cat >"$path" <<EOF
#!/usr/bin/env bash
printf '%s|${command_name} %s\\n' "\$PWD" "\$*" >>"\$CALL_LOG"
exit 0
EOF
  chmod +x "$path"
}

test_phase_scan_requires_connected_semgrep() {
  local case_dir=$WORK/scan
  local repo=$case_dir/repo
  local fake_bin=$case_dir/bin
  local calls=$case_dir/calls
  local output status failed=0

  mkdir -p "$repo/scripts" "$fake_bin"
  cp "$ROOT/scripts/scan.sh" "$repo/scripts/scan.sh"
  : >"$calls"

  make_logging_stub "$fake_bin/make" make
  make_logging_stub "$fake_bin/semgrep" semgrep
  make_logging_stub "$fake_bin/gitleaks" gitleaks

  output=$(
    /usr/bin/env -u SEMGREP_APP_TOKEN \
      PATH="$fake_bin:$SYSTEM_PATH" CALL_LOG="$calls" \
      /bin/bash "$repo/scripts/scan.sh" 2>&1
  )
  status=$?

  if [ "$status" -eq 0 ]; then
    note_failure "scan exited 0 without SEMGREP_APP_TOKEN; a phase gate must fail closed" || failed=1
  fi
  if ! grep -Fqx "$repo|gitleaks detect --redact --no-banner" "$calls"; then
    note_failure "gitleaks did not run after the missing-token failure" || failed=1
  fi
  if ! grep -Fqx "$repo|make semgrep" "$calls" &&
    ! grep -Fqx "$repo|semgrep ci" "$calls"; then
    note_failure "the Semgrep fixture was not exercised" || failed=1
  fi

  if [ "$failed" -ne 0 ]; then
    printf '    observed status: %s\n' "$status" >&2
    printf '    observed output: %s\n' "${output//$'\n'/ | }" >&2
  fi
  return "$failed"
}

setup_ci_fixture() {
  local repo=$1 fake_bin=$2 calls=$3

  mkdir -p "$repo/scripts" "$repo/apps/server" \
    "$repo/packages/schema/gen/go" "$fake_bin"
  cp "$ROOT/scripts/ci.sh" "$repo/scripts/ci.sh"
  : >"$repo/apps/server/go.mod"
  : >"$repo/apps/server/go.sum"
  : >"$calls"

  make_logging_stub "$fake_bin/make" make
  make_logging_stub "$fake_bin/go" go
  make_logging_stub "$fake_bin/golangci-lint" golangci-lint
  make_logging_stub "$fake_bin/govulncheck" govulncheck

  cat >"$fake_bin/git" <<'EOF'
#!/usr/bin/env bash
# No upstream means the two append-only comparisons skip cleanly.
exit 0
EOF
  chmod +x "$fake_bin/git"

  cat >"$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = ps ]; then
  # Finite output larger than a pipe buffer: grep -q closes the pipe after the
  # first line, while a caller that captures output still completes normally.
  exec awk 'BEGIN {
    print "aboutme-test-db"
    for (i = 0; i < 262144; i++) print "padding-container-" i
  }'
fi
printf '%s|podman %s\n' "$PWD" "$*" >>"$CALL_LOG"
exit 64
EOF
  chmod +x "$fake_bin/podman"
}

go_call_seen() {
  local calls=$1 directory=$2 verb=$3 cwd command
  while IFS='|' read -r cwd command; do
    if [ "$cwd" = "$directory" ] &&
      [[ $command == "go $verb "* ]] && [[ $command == *"./..."* ]]; then
      return 0
    fi
  done <"$calls"
  return 1
}

test_local_ci_preserves_shared_db_and_checks_schema_go() {
  local case_dir=$WORK/ci
  local repo=$case_dir/repo
  local fake_bin=$case_dir/bin
  local calls=$case_dir/calls
  local output status failed=0 verb

  setup_ci_fixture "$repo" "$fake_bin" "$calls"
  output=$(
    /usr/bin/env PATH="$fake_bin:$SYSTEM_PATH" CALL_LOG="$calls" \
      /bin/bash "$repo/scripts/ci.sh" 2>&1
  )
  status=$?

  if [ "$status" -ne 0 ]; then
    note_failure "the all-success CI fixture exited $status" || failed=1
  fi
  if [[ $output != *"aboutme-test-db already running (shared)"* ]]; then
    note_failure "the shared database probe became false after its first matching line" || failed=1
  fi
  if grep -Fqx "$repo|make test-db-down" "$calls"; then
    note_failure "CI cleanup requested test-db-down for a database it did not start" || failed=1
  fi

  for verb in build vet test; do
    if ! go_call_seen "$calls" "$repo/packages/schema/gen/go" "$verb"; then
      note_failure "local CI did not run 'go $verb ./...' in packages/schema/gen/go" || failed=1
    fi
  done

  if [ "$failed" -ne 0 ]; then
    printf '    observed status: %s\n' "$status" >&2
    printf '    cleanup calls: %s\n' "$(grep -F 'test-db-' "$calls" | tr '\n' ';' || true)" >&2
  fi
  return "$failed"
}

test_compose_smoke_refuses_shared_database() {
  local case_dir=$WORK/dev-guard
  local repo=$case_dir/repo
  local fake_bin=$case_dir/bin
  local calls=$case_dir/calls
  local output status failed=0

  mkdir -p "$repo" "$fake_bin"
  cp "$ROOT/Makefile" "$repo/Makefile"
  : >"$repo/.env"
  : >"$calls"

  cat >"$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
printf 'podman %s\n' "$*" >>"$CALL_LOG"
case ${1:-} in
ps)
  printf '%s\n' aboutme-test-db
  ;;
inspect)
  printf '%s\n' true
  ;;
container)
  [ "${2:-}" = exists ]
  ;;
esac
exit 0
EOF
  chmod +x "$fake_bin/podman"

  output=$(
    cd "$repo" &&
      /usr/bin/env PATH="$fake_bin:$SYSTEM_PATH" CALL_LOG="$calls" \
        /usr/bin/make --no-print-directory dev 2>&1
  )
  status=$?

  if [ "$status" -eq 0 ]; then
    note_failure "make dev exited 0 while aboutme-test-db was running" || failed=1
  fi
  if grep -Eq '^podman compose([[:space:]]|$)' "$calls"; then
    note_failure "make dev invoked podman compose despite the shared database" || failed=1
  fi
  if [[ $output != *aboutme-test-db* ]]; then
    note_failure "make dev did not identify the running database in its refusal" || failed=1
  fi

  if [ "$failed" -ne 0 ]; then
    printf '    observed status: %s\n' "$status" >&2
    printf '    podman calls: %s\n' "$(tr '\n' ';' <"$calls")" >&2
  fi
  return "$failed"
}

test_help_describes_compose_as_smoke_only() {
  local case_dir=$WORK/help
  local repo=$case_dir/repo
  local output status dev_line native_line dev_lower native_lower failed=0

  mkdir -p "$repo"
  cp "$ROOT/Makefile" "$repo/Makefile"
  output=$(cd "$repo" && /usr/bin/make --no-print-directory help 2>&1)
  status=$?
  dev_line=$(awk '$1 == "dev" { print; exit }' <<<"$output")
  native_line=$(awk '$1 == "dev-native" { print; exit }' <<<"$output")
  dev_lower=${dev_line,,}
  native_lower=${native_line,,}

  if [ "$status" -ne 0 ] || [ -z "$dev_line" ] || [ -z "$native_line" ]; then
    note_failure "make help did not emit both dev and dev-native entries" || failed=1
  fi
  if [[ $dev_lower != *smoke* ]] || [[ $dev_lower != *self-hosting* ]]; then
    note_failure "dev help must call Compose a smoke/self-hosting stack" || failed=1
  fi
  if [[ $dev_lower == *uat* ]]; then
    note_failure "dev help still calls the current Compose stack UAT" || failed=1
  fi
  if [[ $native_lower == *uat* ]]; then
    note_failure "dev-native help still calls Compose UAT-only" || failed=1
  fi

  if [ "$failed" -ne 0 ]; then
    printf '    dev: %s\n' "$dev_line" >&2
    printf '    dev-native: %s\n' "$native_line" >&2
  fi
  return "$failed"
}

test_native_status_checks_database_host_port() {
  local case_dir=$WORK/native-status
  local repo=$case_dir/repo
  local fake_bin=$case_dir/bin
  local output status db_line failed=0 name ready pid sid attempt fixture_ready
  local -a sleepers=()
  local -a launchers=()

  mkdir -p "$repo/scripts" "$repo/.dev" "$fake_bin"
  cp "$ROOT/scripts/dev-native.sh" "$repo/scripts/dev-native.sh"

  cat >"$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = ps ]; then
  printf '%s\n' aboutme-test-db
  exit 0
fi
exit 64
EOF
  chmod +x "$fake_bin/podman"

  cat >"$fake_bin/ss" <<'EOF'
#!/usr/bin/env bash
case " $* " in
*":20432"*) exit 0 ;;
*) printf '%s\n' 'LISTEN 0 128 127.0.0.1:fixture 0.0.0.0:*' ;;
esac
EOF
  chmod +x "$fake_bin/ss"

  for name in server web caddy; do
    ready=$case_dir/$name.ready
    /usr/bin/setsid /bin/bash -c '
      printf "%s\n" "$$" >"$1"
      exec /bin/sleep 30
    ' native-status-fixture "$ready" >/dev/null 2>&1 &
    launchers+=("$!")

    fixture_ready=0
    for attempt in $(seq 1 100); do
      if [ -s "$ready" ]; then
        pid=$(<"$ready")
        sid=$(/usr/bin/ps -o sid= -p "$pid" 2>/dev/null | tr -d ' ')
        if [[ $pid =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null &&
          [ "$sid" = "$pid" ]; then
          fixture_ready=1
          break
        fi
      fi
      /bin/sleep 0.01
    done
    if [ "$fixture_ready" -ne 1 ]; then
      note_failure "could not establish a session-leader fixture for $name" || failed=1
      break
    fi
    sleepers+=("$pid")
    printf '%s\n' "$pid" >"$repo/.dev/$name.pid"
  done

  if [ "$failed" -eq 0 ]; then
    output=$(
      /usr/bin/env PATH="$fake_bin:$SYSTEM_PATH" \
        /bin/bash "$repo/scripts/dev-native.sh" status 2>&1
    )
    status=$?
  else
    output=
    status=125
  fi

  for pid in "${sleepers[@]}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
  for pid in "${launchers[@]}"; do
    wait "$pid" 2>/dev/null || true
  done

  if [ "$failed" -ne 0 ]; then
    return "$failed"
  fi

  for name in server web caddy; do
    if ! awk -v service="$name" \
      '$1 == service && $3 == "running" && $5 == "yes" { found = 1 } END { exit !found }' \
      <<<"$output"; then
      note_failure "the $name fixture was not reported running and listening" || failed=1
    fi
  done

  db_line=$(awk '$1 == "db" { print; exit }' <<<"$output")
  if [ "$status" -eq 0 ]; then
    note_failure "native status exited 0 with no listener on host port 20432" || failed=1
  fi
  if ! awk '$1 == "db" && $3 == "running" && $4 == "20432" && $5 == "no" { found = 1 } END { exit !found }' \
    <<<"$output"; then
    note_failure "database status did not report LISTENING=no on port 20432" || failed=1
  fi

  if [ "$failed" -ne 0 ]; then
    printf '    observed status: %s\n' "$status" >&2
    printf '    db: %s\n' "$db_line" >&2
  fi
  return "$failed"
}

run_test "phase scan fails closed without connected Semgrep and still runs gitleaks" \
  test_phase_scan_requires_connected_semgrep
run_test "local CI preserves a shared DB under pipefail and checks generated Go" \
  test_local_ci_preserves_shared_db_and_checks_schema_go
run_test "Compose smoke refuses to start beside the shared database" \
  test_compose_smoke_refuses_shared_database
run_test "Makefile help distinguishes Compose smoke from native development" \
  test_help_describes_compose_as_smoke_only
run_test "native status verifies the database host listener" \
  test_native_status_checks_database_host_port

printf '\n%s passed; %s failed\n' "$passes" "$failures"
[ "$failures" -eq 0 ]
