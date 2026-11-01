#!/usr/bin/env bash
# Local build, test, lint, and drift reproduction without hosted browser proofs.
# The full local run needs owner authorization; connected security scanning uses `make scan`.
#
#   scripts/ci.sh          local reproduction (`make ci`)
#   scripts/ci.sh --fast   subset without web checks, DB/S3 suites, or route-table tests
#                          (`make check`)
#
# The database-backed groups start or reuse the shared Postgres and always leave
# it running. Teardown is an integration-owner action in a declared idle window;
# CI cannot infer container ownership atomically from a separate preflight.
set -Eeuo pipefail

cd "$(dirname "$0")/.."
ROOT="$PWD"

FAST=0
[ "${1:-}" = "--fast" ] && FAST=1

FAILED=""

trap 'exit 130' INT
trap 'exit 143' TERM

group() {
  local name="$1"
  shift
  echo
  echo "=== $name"
  local start
  start=$SECONDS
  # Each group runs in a subshell: several of them cd into apps/server, and
  # without this that cd leaks into every later group -- which silently made
  # `make sqlc-check` and `make web-lint` run from the wrong directory.
  if ("$@"); then
    echo "--- $name OK ($((SECONDS - start))s)"
  else
    echo "--- $name FAILED ($((SECONDS - start))s)" >&2
    FAILED="$FAILED $name"
    return 1
  fi
}

# Everything below runs even if an earlier group failed, so one run reports
# every problem instead of only the first. The script still exits non-zero.
run() { group "$@" || true; }

docs() { make docs-lint; }
lengths() { make lengths-check; }
tool_versions() { make tools-check ARGS=ci; }
operational_contracts() {
  local platform
  platform=$(uname -s) || return
  if [ "$platform" = Linux ]; then
    make operational-test
  else
    echo 'Skipping Linux-specific dev-https static contracts; CI covers them.'
    make operational-test-fast operational-test-deploy operational-test-browser
  fi
}
schema() {
  make schema-check || return
  (
    cd "$ROOT/packages/schema/gen/go" || exit
    go build ./... &&
      go vet ./... &&
    go test ./... -count=1
  )
}
api() { make api-check; }

go_build() { make server-build server-vet server-test; }

# GOGC=50 halves golangci-lint's peak heap (it is the single largest memory
# consumer in the gate) at a small time cost -- this machine has been OOM
# killed under concurrent gate runs before.
go_lint() { cd "$ROOT/apps/server" && GOGC=50 golangci-lint run; }

go_tidy() {
  cd "$ROOT/apps/server" || return
  local tmp
  tmp="$(mktemp -d)" || return
  if ! cp go.mod go.sum "$tmp/"; then
    rm -rf "$tmp"
    return 1
  fi
  if ! go mod tidy; then
    cp "$tmp/go.mod" "$tmp/go.sum" . || true
    rm -rf "$tmp"
    echo "go mod tidy failed; restored go.mod/go.sum" >&2
    return 1
  fi
  if ! diff -q "$tmp/go.mod" go.mod >/dev/null || ! diff -q "$tmp/go.sum" go.sum >/dev/null; then
    cp "$tmp/go.mod" "$tmp/go.sum" .
    rm -rf "$tmp"
    echo "go.mod/go.sum are not tidy -- run 'go mod tidy' in apps/server and commit" >&2
    return 1
  fi
  rm -rf "$tmp"
}

go_vuln() {
  # Mirrors ci.yml's govulncheck steps: every Go module in the repository, with
  # the Caddy build module (outside go.work) forced to GOWORK=off.
  (cd "$ROOT/apps/server" && govulncheck ./...) &&
    (cd "$ROOT/packages/schema/gen/go" && govulncheck ./...) &&
    (cd "$ROOT/deploy/caddy/production/build" && GOWORK=off govulncheck ./...)
}

sqlc_drift() { make sqlc-check; }

web() { make web-lint web-typecheck web-test web-build web-source-build; }

migrations_append_only() {
  bash "$ROOT/scripts/check-migrations-append-only.sh" --local origin/main
}

released_schema_append_only() {
  local base status
  if base=$(git rev-parse --verify --quiet 'origin/main^{commit}'); then
    :
  else
    status=$?
    if [ "$status" -ne 1 ]; then
      echo "could not resolve origin/main" >&2
      return 1
    fi
    if git rev-parse --verify --quiet origin/main >/dev/null; then
      echo "local base is not a commit: origin/main" >&2
      return 1
    fi
    echo "no origin/main to compare against; skipped"
    return 0
  fi
  if ! git cat-file -e "$base:.v1-baseline" 2>/dev/null; then
    return 0
  fi
  if ! git diff --cached --quiet "$base" -- .v1-baseline ||
    ! git diff --quiet "$base" -- .v1-baseline; then
    echo "The v1 compatibility baseline is immutable: .v1-baseline" >&2
    return 1
  fi
  local index_diff worktree_diff index_changed worktree_changed
  if ! index_diff=$(git diff --cached --name-status "$base" -- 'packages/schema/resume.v*.schema.json'); then
    echo "could not compare the released-schema index with origin/main" >&2
    return 1
  fi
  if ! worktree_diff=$(git diff --name-status "$base" -- 'packages/schema/resume.v*.schema.json'); then
    echo "could not compare the released-schema worktree with origin/main" >&2
    return 1
  fi
  index_changed="$(grep -E '^(M|D|R|T)' <<<"$index_diff" || true)"
  worktree_changed="$(grep -E '^(M|D|R|T)' <<<"$worktree_diff" || true)"
  if [ -n "$index_changed" ] || [ -n "$worktree_changed" ]; then
    echo "Released schemas are immutable; only a new version file may be added:" >&2
    if [ -n "$index_changed" ]; then
      echo "index:" >&2
      echo "$index_changed" >&2
    fi
    if [ -n "$worktree_changed" ]; then
      echo "worktree:" >&2
      echo "$worktree_changed" >&2
    fi
    return 1
  fi
}

db_suites() {
  cd "$ROOT" || return
  make server-test-integration &&
    make server-test-db &&
    make server-migration-test &&
    make server-test-resumeapi
}

s3_suites() {
  cd "$ROOT" || return
  local platform
  platform=$(uname -s) || return
  if [ "$platform" != Linux ]; then
    echo 'Skipping S3 suites: their host-networked MinIO client needs Linux; CI covers them.'
    return 0
  fi
  make test-s3-up || return
  local status=0
  make server-test-s3 || status=$?
  if [ "$status" -eq 0 ]; then
    make server-test-resumeapi-s3 || status=$?
  fi
  make test-s3-down || status=$?
  return "$status"
}

route_table() { make route-table-test; }

run "tool versions" tool_versions
run "operational contracts" operational_contracts
run "docs-lint" docs
run "file lengths" lengths
run "schema-check" schema
run "api-check" api
run "go build/vet/test" go_build
run "golangci-lint" go_lint
run "go mod tidy" go_tidy
run "govulncheck" go_vuln
run "sqlc drift" sqlc_drift
run "migrations append-only" migrations_append_only
run "released schemas append-only" released_schema_append_only

if [ "$FAST" = "1" ]; then
  echo
  echo "=== fast mode: skipping web build and database-backed suites"
else
  run "web" web
  echo
  echo "=== starting test database"
  # The target is idempotent. This preflight is informational only: container
  # ownership cannot be inferred across a separate check/start boundary, so CI
  # leaves the shared database running in every case.
  running_containers=$(podman ps --format '{{.Names}}')
  if grep -qx aboutme-test-db <<<"$running_containers"; then
    echo "aboutme-test-db already running (shared); this run will not stop it."
  else
    echo "aboutme-test-db is not running; starting the shared database and leaving it running."
  fi
  make test-db-up
  run "database suites" db_suites
  run "S3 media and resume API suites" s3_suites
  run "route table" route_table
fi

echo
if [ -n "$FAILED" ]; then
  echo "FAILED:$FAILED" >&2
  exit 1
fi
echo "All checks passed."
