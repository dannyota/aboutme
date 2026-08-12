#!/usr/bin/env bash

# Author regression tests for fail-closed container discovery in Make targets.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-make-safety.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

REPO=$WORK/repo
BIN=$WORK/bin
CALLS=$WORK/calls
mkdir -p "$REPO" "$BIN"
cp "$ROOT/Makefile" "$REPO/Makefile"
: >"$REPO/.env"
: >"$CALLS"

cat >"$BIN/podman" <<'EOF'
#!/usr/bin/env bash
printf 'podman %s\n' "$*" >>"$CALL_LOG"
if [ "${1:-}" = ps ]; then
  if [ "${PODMAN_PS_MODE:-fail}" = compose-db ]; then
    printf '%s\n' 'aboutme-postgres-1|aboutme|postgres'
    exit 0
  fi
  exit 17
fi
exit 0
EOF
chmod +x "$BIN/podman"

assert_guarded() {
  local target=$1
  : >"$CALLS"
  if (
    cd "$REPO"
    PATH="$BIN:/usr/bin:/bin" CALL_LOG="$CALLS" \
      /usr/bin/make --no-print-directory "$target"
  ) >"$WORK/$target.out" 2>&1; then
    printf 'makefile-safety-test: make %s passed after podman ps failed\n' "$target" >&2
    exit 1
  fi
  if grep -Eq '^podman (compose|run)([[:space:]]|$)' "$CALLS"; then
    printf 'makefile-safety-test: make %s mutated containers after discovery failed\n' "$target" >&2
    exit 1
  fi
}

assert_guarded dev
assert_guarded test-db-up

: >"$CALLS"
if (
  cd "$REPO"
  PATH="$BIN:/usr/bin:/bin" CALL_LOG="$CALLS" PODMAN_PS_MODE=compose-db \
    /usr/bin/make --no-print-directory test-db-up
) >"$WORK/compose-db.out" 2>&1; then
  printf 'makefile-safety-test: test-db-up passed beside Compose Postgres\n' >&2
  exit 1
fi
if grep -Eq '^podman run([[:space:]]|$)' "$CALLS"; then
  printf 'makefile-safety-test: test-db-up started a second Postgres\n' >&2
  exit 1
fi

printf 'Makefile container-discovery safety tests passed\n'
