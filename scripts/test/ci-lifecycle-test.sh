#!/usr/bin/env bash

# Author regression tests for CI signal handling and shared-DB ownership.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-ci-lifecycle.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

make_fixture() {
  local repo=$1 bin=$2 calls=$3
  mkdir -p "$repo/scripts" "$repo/apps/server" \
    "$repo/packages/schema/gen/go" "$bin"
  cp "$ROOT/scripts/ci.sh" "$repo/scripts/ci.sh"
  : >"$repo/apps/server/go.mod"
  : >"$repo/apps/server/go.sum"
  : >"$calls"

  for name in go golangci-lint govulncheck; do
    cat >"$bin/$name" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  done
  cat >"$bin/git" <<'EOF'
#!/usr/bin/env bash
# Missing origin/main is the supported local-skip case.
exit 1
EOF
  chmod +x "$bin"/*
}

test_ci_never_tears_down_shared_db() {
  local case_dir=$WORK/db-race
  local repo=$case_dir/repo bin=$case_dir/bin calls=$case_dir/calls
  make_fixture "$repo" "$bin" "$calls"

  cat >"$bin/make" <<'EOF'
#!/usr/bin/env bash
printf 'make %s\n' "$*" >>"$CALL_LOG"
exit 0
EOF
  cat >"$bin/podman" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = ps ]; then
  # Empty at CI's preflight. A concurrent owner wins the name before
  # test-db-up, which then reuses it; CI must never infer teardown ownership.
  exit 0
fi
exit 64
EOF
  chmod +x "$bin/make" "$bin/podman"

  PATH="$bin:/usr/bin:/bin" CALL_LOG="$calls" \
    /bin/bash "$repo/scripts/ci.sh" >/dev/null 2>&1
  if grep -Fqx 'make test-db-down' "$calls"; then
    printf 'ci-lifecycle-test: CI tore down a container it could not own atomically\n' >&2
    return 1
  fi
}

test_signal_stops_before_later_groups() {
  local case_dir=$WORK/signal
  local repo=$case_dir/repo bin=$case_dir/bin calls=$case_dir/calls
  local ready=$case_dir/ready output=$case_dir/output
  local pid status=0 i
  make_fixture "$repo" "$bin" "$calls"

  cat >"$bin/make" <<'EOF'
#!/usr/bin/env bash
printf 'make %s\n' "$*" >>"$CALL_LOG"
if [ "$*" = 'tools-check ARGS=ci' ]; then
  : >"$READY_FILE"
  trap 'exit 143' TERM
  while :; do sleep 1; done
fi
exit 0
EOF
  chmod +x "$bin/make"

  /usr/bin/setsid /usr/bin/env PATH="$bin:/usr/bin:/bin" \
    CALL_LOG="$calls" READY_FILE="$ready" \
    /bin/bash "$repo/scripts/ci.sh" --fast >"$output" 2>&1 &
  pid=$!

  for i in $(seq 1 100); do
    if [ -f "$ready" ] && [ "$(ps -o sid= -p "$pid" 2>/dev/null | tr -d ' ')" = "$pid" ]; then
      break
    fi
    sleep 0.02
  done
  [ -f "$ready" ] || {
    kill -KILL -- "-$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    printf 'ci-lifecycle-test: signal fixture never reached its long group\n' >&2
    return 1
  }

  kill -TERM -- "-$pid"
  for i in $(seq 1 100); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.02
  done
  if kill -0 "$pid" 2>/dev/null; then
    kill -KILL -- "-$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    printf 'ci-lifecycle-test: CI did not exit after SIGTERM\n' >&2
    return 1
  fi
  wait "$pid" || status=$?

  [ "$status" -eq 143 ] || {
    printf 'ci-lifecycle-test: SIGTERM exited %s, want 143\n' "$status" >&2
    return 1
  }
  [ "$(wc -l <"$calls")" -eq 1 ] || {
    printf 'ci-lifecycle-test: CI ran commands after SIGTERM: %s\n' \
      "$(tr '\n' ';' <"$calls")" >&2
    return 1
  }
}

test_ci_never_tears_down_shared_db
test_signal_stops_before_later_groups
printf 'CI lifecycle tests passed\n'
