#!/usr/bin/env bash
set -Eeuo pipefail

# Static contract for the P5A native public HTTP capture. It proves the
# fixture command's opt-in, loopback, rooted-path, and frozen-clock rules, and
# that the capture script and Make target exist with the required isolated
# lifecycle. It never touches the database or starts a stack.
#
#   scripts/test/p5a-native-http-capture-test.sh              # fixture + lifecycle
#   scripts/test/p5a-native-http-capture-test.sh -k fixture    # fixture only
#   scripts/test/p5a-native-http-capture-test.sh -k lifecycle  # lifecycle only

ROOT=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
cd "$ROOT"

fail() {
  printf 'p5a-native-http-capture-test: %s\n' "$*" >&2
  exit 1
}

selector=all
if [ $# -gt 0 ]; then
  case $1 in
  -k)
    [ $# -ge 2 ] || fail "-k requires fixture or lifecycle"
    selector=$2
    ;;
  *) fail "unknown argument '$1' (usage: $0 [-k fixture|lifecycle])" ;;
  esac
fi
case "$selector" in
all | fixture | lifecycle) ;;
*) fail "unknown -k value '$selector' (want fixture or lifecycle)" ;;
esac

FROZEN_DSN='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_p5a_fixture?sslmode=disable'
FROZEN_NOW='2035-01-01T00:00:00Z'

fixture_tmp=
cleanup_fixture_tmp() {
  if [ -n "${fixture_tmp:-}" ]; then
    rm -rf -- "$fixture_tmp"
  fi
}
trap cleanup_fixture_tmp EXIT

# ---------------------------------------------------------------------------
# fixture: the seed/cleanup command validates its inputs before any I/O
# ---------------------------------------------------------------------------

test_fixture() {
  local bin
  fixture_tmp=$(mktemp -d "$ROOT/p5a-native-http-capture-test.XXXXXX")
  bin=$fixture_tmp/p5a-native-fixture
  (cd "$ROOT/apps/server" && go build -o "$bin" ./cmd/p5a-native-fixture) ||
    fail 'p5a-native-fixture does not build'

  # A valid invocation passes validation and fails later at the database
  # connection. Port 1 is loopback but nothing listens there, so this proves
  # validation accepted the input without requiring a live database.
  local closed_dsn closed_out
  closed_dsn='postgres://aboutme:aboutme_dev@127.0.0.1:1/aboutme_p5a_fixture?sslmode=disable'
  set +e
  closed_out=$("$bin" seed --database-url "$closed_dsn" --media-root .dev/p5a-fixture-media --now "$FROZEN_NOW" 2>&1)
  closed_status=$?
  set -e
  [ "$closed_status" -ne 0 ] || fail 'valid seed unexpectedly succeeded against a closed port'
  if [[ $closed_out == *loopback* ||
    $closed_out == *'below the repository root'* ||
    $closed_out == *frozen* || $closed_out == *subcommand* ]]; then
    fail "valid seed rejected with a validation error: $closed_out"
  fi

  rejects() {
    local desc=$1
    shift
    set +e
    local out
    out=$("$bin" "$@" 2>&1)
    local status=$?
    set -e
    [ "$status" -ne 0 ] || fail "$desc unexpectedly succeeded"
    printf '%s' "$out"
  }

  out=$(rejects 'non-loopback database' seed --database-url 'postgres://aboutme:aboutme_dev@10.90.0.2:20432/aboutme_p5a_fixture' --media-root .dev/p5a-fixture-media --now "$FROZEN_NOW")
  [[ $out == *loopback* ]] || fail "non-loopback database error should mention loopback: $out"

  out=$(rejects 'non-fixture database' seed --database-url 'postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable' --media-root .dev/p5a-fixture-media --now "$FROZEN_NOW")
  [[ $out == *aboutme_p5a_fixture* ]] || fail "non-fixture database error should name the fixture database: $out"

  out=$(rejects 'filesystem-root media root' seed --database-url "$FROZEN_DSN" --media-root / --now "$FROZEN_NOW")
  [[ $out == *'below the repository root'* ]] || fail "filesystem-root media error: $out"

  out=$(rejects 'escaping media root' seed --database-url "$FROZEN_DSN" --media-root /tmp/p5a-outside --now "$FROZEN_NOW")
  [[ $out == *'below the repository root'* ]] || fail "escaping media error: $out"

  out=$(rejects 'non-frozen clock' seed --database-url "$FROZEN_DSN" --media-root .dev/p5a-fixture-media --now '2020-01-01T00:00:00Z')
  [[ $out == *frozen* ]] || fail "non-frozen clock error should mention frozen: $out"

  out=$(rejects 'unknown subcommand' frobnicate --database-url "$FROZEN_DSN")
  [[ $out == *subcommand* ]] || fail "unknown subcommand error: $out"

  printf '%s\n' 'p5a-native-http-capture-test: fixture PASS'
}

# ---------------------------------------------------------------------------
# lifecycle: the capture script and Make target exist with the isolated rules
# ---------------------------------------------------------------------------

test_lifecycle() {
  local capture=$ROOT/scripts/p5a-native-http-capture.sh
  [ -f "$capture" ] || fail 'scripts/p5a-native-http-capture.sh is missing'
  [ -x "$capture" ] || fail 'scripts/p5a-native-http-capture.sh is not executable'

  local content makefile
  content=$(<"$capture")
  makefile=$(<"$ROOT/Makefile")

  [[ $content == *aboutme_p5a_fixture* ]] ||
    fail 'capture script must name the aboutme_p5a_fixture database'
  [[ $content == *127.0.0.1:20432* ]] ||
    fail 'capture script must use the loopback 127.0.0.1:20432 database'
  [[ $content == *'.dev/p5a-fixture-runtime'* ]] ||
    fail 'capture script must use the rooted .dev/p5a-fixture-runtime state dir'
  [[ $content == *'.dev/p5a-fixture-media'* ]] ||
    fail 'capture script must use the rooted .dev/p5a-fixture-media media dir'
  [[ $content == *'trap'* ]] ||
    fail 'capture script must install a cleanup trap'
  [[ $content == *'p5a-native-fixture seed'* ]] ||
    fail 'capture script must seed via p5a-native-fixture'
  [[ $content == *'p5a-native-fixture cleanup'* ]] ||
    fail 'capture script must clean via p5a-native-fixture'
  [[ $content == *'ABOUTME_DEV_DATABASE_URL'* ]] ||
    fail 'capture script must isolate the database via ABOUTME_DEV_DATABASE_URL'

  grep -q '^p5a-native-http-check:' <<<"$makefile" ||
    fail 'Makefile is missing the p5a-native-http-check target'

  for f in resume-v2.json photo.png hashes.json; do
    [ -f "$ROOT/apps/server/cmd/p5a-native-fixture/testdata/$f" ] ||
      fail "fixture testdata/$f is missing"
  done

  printf '%s\n' 'p5a-native-http-capture-test: lifecycle PASS'
}

if [ "$selector" = all ] || [ "$selector" = fixture ]; then
  test_fixture
fi
if [ "$selector" = all ] || [ "$selector" = lifecycle ]; then
  test_lifecycle
fi
