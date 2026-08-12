#!/usr/bin/env bash

# Black-box contract for secret-free live-database Make transcripts. The fake
# Go command records environment propagation without running tests or a DB.
set -u -o pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SYSTEM_PATH=/usr/bin:/bin
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-live-db-transcript.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

repo=$WORK/repo
fake_bin=$WORK/bin
calls=$WORK/calls
sentinel_dsn='postgres://sentinel_user:SENTINEL_DB_SECRET@127.0.0.1:20999/sentinel?sslmode=disable'
mkdir -p "$repo/apps/server" "$fake_bin"
cp "$ROOT/Makefile" "$repo/Makefile"

cat >"$fake_bin/go" <<'EOF'
#!/usr/bin/env bash
if [ "${TEST_DATABASE_URL+x}" != x ]; then
  printf 'go stub: TEST_DATABASE_URL was not set\n' >&2
  exit 65
fi
if [ "$TEST_DATABASE_URL" != "$EXPECTED_TEST_DATABASE_URL" ]; then
  printf 'go stub: TEST_DATABASE_URL did not match the injected value\n' >&2
  exit 66
fi
if [ "${REQUIRE_TEST_DB-<unset>}" != "$EXPECTED_REQUIRE_TEST_DB" ]; then
  printf 'go stub: REQUIRE_TEST_DB was %s, want %s\n' \
    "${REQUIRE_TEST_DB-<unset>}" "$EXPECTED_REQUIRE_TEST_DB" >&2
  exit 67
fi
printf '%s|go %s|dsn=exact|require=%s\n' \
  "$EXPECTED_TARGET" "$*" "${REQUIRE_TEST_DB-<unset>}" >>"$CALL_LOG"
EOF
chmod +x "$fake_bin/go"

failures=0

fail() {
  printf 'not ok - %s\n' "$1" >&2
  failures=$((failures + 1))
}

transcript_is_secret_free() {
  local transcript=$1

  ! grep -Fq -- "$sentinel_dsn" <<<"$transcript" &&
    ! grep -Fq -- 'SENTINEL_DB_SECRET' <<<"$transcript" &&
    ! grep -Fq -- 'TEST_DATABASE_URL=' <<<"$transcript"
}

run_case() {
  local target=$1 expected_require=$2
  local output status=0 call_count failures_before

  : >"$calls"
  failures_before=$failures
  output=$(
    cd "$repo" || exit
    /usr/bin/env \
      PATH="$fake_bin:$SYSTEM_PATH" \
      CALL_LOG="$calls" \
      EXPECTED_TARGET="$target" \
      EXPECTED_TEST_DATABASE_URL="$sentinel_dsn" \
      EXPECTED_REQUIRE_TEST_DB="$expected_require" \
      TEST_DATABASE_URL="$sentinel_dsn" \
      /usr/bin/make --no-print-directory "$target" 2>&1
  ) || status=$?

  if [ "$status" -ne 0 ]; then
    fail "$target returned $status with the recording Go stub"
  fi
  if ! transcript_is_secret_free "$output"; then
    fail "$target printed a TEST_DATABASE_URL value"
  fi
  if ! grep -Fq -- "$target:" <<<"$output"; then
    fail "$target transcript lacks its target name"
  fi
  if ! grep -Fq -- 'go test' <<<"$output"; then
    fail "$target transcript lacks the child command name"
  fi

  call_count=$(grep -Fc "$target|go test " "$calls" || true)
  if [ "$call_count" -ne 1 ]; then
    fail "$target made $call_count recorded Go test calls; expected one"
  fi
  if ! grep -Fq "|dsn=exact|require=$expected_require" "$calls"; then
    fail "$target did not pass the expected live-DB environment to Go"
  fi

  if [ "$failures" -ne "$failures_before" ]; then
    printf '    %s status: %s\n' "$target" "$status" >&2
    if grep -Fq -- 'TEST_DATABASE_URL=' <<<"$output"; then
      printf '    %s output exposed a TEST_DATABASE_URL assignment\n' "$target" >&2
    fi
    if grep -Fq -- 'go test' <<<"$output"; then
      printf '    %s output retained the go test command name\n' "$target" >&2
    fi
    printf '    %s calls: %s\n' "$target" "$(tr '\n' ';' <"$calls")" >&2
  fi
}

run_case server-test-db 1
run_case server-test-integration '<unset>'
run_case server-migration-test '<unset>'

if transcript_is_secret_free "negative control: $sentinel_dsn"; then
  fail 'the transcript leak detector accepted its sentinel negative control'
fi

if [ "$failures" -ne 0 ]; then
  exit 1
fi

printf 'ok - live-DB Make targets keep transcripts secret-free and pass the DSN only to children\n'
