#!/usr/bin/env bash
# Local CI gate. Runs the same checks as .github/workflows/ci.yml on this
# laptop, so a handoff does not have to wait on GitHub Actions.
#
#   scripts/ci.sh          full gate (this is `make ci`)
#   scripts/ci.sh --fast   everything that needs no database and no web build
#                          (this is `make check`)
#
# The database-backed groups start a throwaway Postgres and always stop it
# again, including on failure or Ctrl-C -- a leaked container would make the
# next run's `test-db-up` fail on a port clash.
set -Eeuo pipefail

cd "$(dirname "$0")/.."
ROOT="$PWD"

FAST=0
[ "${1:-}" = "--fast" ] && FAST=1

STARTED_DB=0
FAILED=""

cleanup() {
  if [ "$STARTED_DB" = "1" ]; then
    echo "--- stopping test database"
    make test-db-down >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

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
schema() { make schema-check; }
api() { make api-check; }

go_build() { make server-build server-vet server-test; }

# GOGC=50 halves golangci-lint's peak heap (it is the single largest memory
# consumer in the gate) at a small time cost -- this machine has been OOM
# killed under concurrent gate runs before.
go_lint() { cd "$ROOT/apps/server" && GOGC=50 golangci-lint run; }

go_tidy() {
  cd "$ROOT/apps/server"
  local tmp
  tmp="$(mktemp -d)"
  cp go.mod go.sum "$tmp/"
  go mod tidy
  if ! diff -q "$tmp/go.mod" go.mod >/dev/null || ! diff -q "$tmp/go.sum" go.sum >/dev/null; then
    cp "$tmp/go.mod" "$tmp/go.sum" .
    rm -rf "$tmp"
    echo "go.mod/go.sum are not tidy -- run 'go mod tidy' in apps/server and commit" >&2
    return 1
  fi
  rm -rf "$tmp"
}

go_vuln() { cd "$ROOT/apps/server" && govulncheck ./...; }

sqlc_drift() { make sqlc-check; }

web() { make web-lint web-typecheck web-test web-build; }

migrations_append_only() {
  # CI compares against the PR base; locally the useful comparison is the
  # upstream branch. Skip cleanly when there is no upstream to compare to.
  local base
  base="$(git rev-parse --verify --quiet origin/main || true)"
  if [ -z "$base" ]; then
    echo "no origin/main to compare against; skipped"
    return 0
  fi
  local changed
  changed="$(git diff --name-status "$base"...HEAD -- apps/server/migrations |
    grep -E '^(M|D|R)' | grep -E '\.sql$' || true)"
  if [ -n "$changed" ]; then
    echo "Applied migrations are immutable; only new files may be added:" >&2
    echo "$changed" >&2
    return 1
  fi
}

released_schema_append_only() {
  # Mirrors ci.yml's released-schema-append-only job: released resume schemas
  # are immutable; only a new version file may be added. The two hashes permit
  # one owner-approved documentation-citation rewrite and no other v1 bytes.
  local base status
  if base=$(git rev-parse --verify --quiet origin/main); then
    :
  else
    status=$?
    if [ "$status" -ne 1 ]; then
      echo "could not resolve origin/main" >&2
      return 1
    fi
    echo "no origin/main to compare against; skipped"
    return 0
  fi
  local approved_old_v1_sha256="2da37bb75297fefe32a920e3fae960100f0a99236ba4dc21ef25ae6b3f61f13f"
  local approved_new_v1_sha256="879858284bc3cb4092d1d671466a9c620e27abf134aecedce070b6f21e4e5866"
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
  local approved_v1_change
  approved_v1_change=$(printf 'M\tpackages/schema/resume.v1.schema.json')
  if [ "$index_changed" = "$approved_v1_change" ] || [ "$worktree_changed" = "$approved_v1_change" ]; then
    local base_v1_sha256
    if ! base_v1_sha256=$(git show "$base:packages/schema/resume.v1.schema.json" | sha256sum | cut -d ' ' -f 1); then
      echo "could not hash origin/main's released v1 schema" >&2
      return 1
    fi
    if [ "$base_v1_sha256" = "$approved_old_v1_sha256" ]; then
      if [ "$index_changed" = "$approved_v1_change" ]; then
        local index_v1_sha256
        if ! index_v1_sha256=$(git show :packages/schema/resume.v1.schema.json | sha256sum | cut -d ' ' -f 1); then
          echo "could not hash the released v1 schema in the index" >&2
          return 1
        fi
        if [ "$index_v1_sha256" = "$approved_new_v1_sha256" ]; then
          index_changed=""
        fi
      fi
      if [ "$worktree_changed" = "$approved_v1_change" ]; then
        local worktree_v1_sha256
        if ! worktree_v1_sha256=$(sha256sum packages/schema/resume.v1.schema.json | cut -d ' ' -f 1); then
          echo "could not hash the released v1 schema in the worktree" >&2
          return 1
        fi
        if [ "$worktree_v1_sha256" = "$approved_new_v1_sha256" ]; then
          worktree_changed=""
        fi
      fi
    fi
  fi
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
  cd "$ROOT"
  make server-test-integration
  make server-test-db
  make server-migration-test
}

route_table() { make route-table-test; }

run "docs-lint" docs
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
  # The container is shared with concurrently running workers. Only tear it
  # down at exit if THIS run started it -- `make test-db-up` is idempotent and
  # succeeds on an already-running container, so track prior existence
  # explicitly rather than inferring it from the target's exit code.
  if podman ps --format '{{.Names}}' | grep -qx aboutme-test-db; then
    STARTED_DB=0
    echo "aboutme-test-db already running (shared); this run will not stop it."
  else
    STARTED_DB=1
  fi
  make test-db-up
  run "database suites" db_suites
  run "route table" route_table
fi

echo
if [ -n "$FAILED" ]; then
  echo "FAILED:$FAILED" >&2
  exit 1
fi
echo "All checks passed."
