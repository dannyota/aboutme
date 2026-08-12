#!/usr/bin/env bash

# Author regression tests for failure propagation inside scripts/ci.sh groups.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-ci-failure.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

REPO=$WORK/repo
BIN=$WORK/bin
CALLS=$WORK/calls
mkdir -p "$REPO/scripts" "$REPO/apps/server" \
  "$REPO/packages/schema/gen/go" "$BIN"
cp "$ROOT/scripts/ci.sh" "$REPO/scripts/ci.sh"
: >"$REPO/apps/server/go.mod"
: >"$REPO/apps/server/go.sum"
: >"$CALLS"

cat >"$BIN/make" <<'EOF'
#!/usr/bin/env bash
printf 'make %s\n' "$*" >>"$CALL_LOG"
case " $* " in
*" ${FAIL_MAKE_TARGET:-__none__} "*) exit 23 ;;
esac
exit 0
EOF

cat >"$BIN/go" <<'EOF'
#!/usr/bin/env bash
printf 'go %s\n' "$*" >>"$CALL_LOG"
if [ "${FAIL_GO_TIDY:-0}" = 1 ] && [ "$*" = 'mod tidy' ]; then
  exit 29
fi
exit 0
EOF

for name in golangci-lint govulncheck; do
  cat >"$BIN/$name" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
done

cat >"$BIN/git" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = rev-parse ]; then
  if [ "${FAIL_GIT_REVPARSE:-0}" = 1 ]; then
    exit 128
  fi
  if [ -n "${FAIL_GIT_DIFF_PATH:-}" ]; then
    printf '%s\n' deadbeef
  fi
  exit 0
fi
if [ "${1:-}" = diff ] && [[ " $* " == *" ${FAIL_GIT_DIFF_PATH:-__none__} "* ]]; then
  exit 31
fi
exit 0
EOF

cat >"$BIN/podman" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = ps ]; then
  printf '%s\n' aboutme-test-db
  exit 0
fi
exit 64
EOF
chmod +x "$BIN"/*

assert_ci_failure() {
  local label=$1 expected_group=$2 mode=$3
  shift 3
  local output=$WORK/${label// /-}.out

  : >"$CALLS"
  if /usr/bin/env PATH="$BIN:/usr/bin:/bin" CALL_LOG="$CALLS" "$@" \
    /bin/bash "$REPO/scripts/ci.sh" "$mode" >"$output" 2>&1; then
    printf 'ci-failure-propagation-test: %s was reported successful\n' "$label" >&2
    exit 1
  fi
  grep -Fq -- "--- $expected_group FAILED" "$output" || {
    printf 'ci-failure-propagation-test: %s did not fail group %s\n' \
      "$label" "$expected_group" >&2
    exit 1
  }
}

assert_ci_failure 'schema first command' schema-check --fast \
  /usr/bin/env FAIL_MAKE_TARGET=schema-check
assert_ci_failure 'go tidy middle command' 'go mod tidy' --fast \
  /usr/bin/env FAIL_GO_TIDY=1
assert_ci_failure 'database first command' 'database suites' '' \
  /usr/bin/env FAIL_MAKE_TARGET=server-test-integration
assert_ci_failure 'migration diff command' 'migrations append-only' --fast \
  /usr/bin/env FAIL_GIT_DIFF_PATH=apps/server/migrations
assert_ci_failure 'released schema diff command' 'released schemas append-only' --fast \
  /usr/bin/env FAIL_GIT_DIFF_PATH=packages/schema/resume.v\*.schema.json
assert_ci_failure 'upstream resolution command' 'migrations append-only' --fast \
  /usr/bin/env FAIL_GIT_REVPARSE=1

printf 'CI failure-propagation tests passed\n'
