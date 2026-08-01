#!/usr/bin/env bash
# check-data-drift.sh — CI drift gate for aboutme's single-source-of-truth
# data-layer contract (docs/specs/aboutme-design.md §3 "Schema management";
# task item 5 of review-datalayer.txt's "Important" findings).
#
# sql/schema.sql is the one declared source of truth for both sqlc's
# generated internal/store and apps/server/migrations' goose SQL. Nothing
# in the build enforces that automatically — this script is the check that
# does, and CI should run it on every pull request:
#
#   1. Regenerate sqlc's output and fail on ANY diff, including brand-new
#      untracked files (git status --porcelain --untracked-files=all, not
#      just git diff — the previous Makefile-only check only caught
#      modifications to already-tracked files, never a new generated file
#      nobody committed yet).
#   2. Replay every goose migration and diff the result against
#      sql/schema.sql with Atlas in goose directory format, failing if
#      they disagree — i.e. sql/schema.sql has a change no migration
#      captures yet, or vice versa. This also cross-checks every
#      hand-written CREATE EXTENSION statement under apps/server/migrations
#      against sql/schema.sql's own extension declarations, since Atlas's
#      differ can never see CREATE EXTENSION itself (see
#      apps/server/migrations/00001_extensions.sql's doc comment).
#   3. Verify the Atlas CLI on PATH is the exact pinned release this
#      pipeline was verified against — not whatever "latest" happened to
#      resolve to when it was installed — so an unpinned/upgraded Atlas
#      can never silently change this gate's behavior out from under it.
#
# Requires:
#   - sqlc on PATH.
#   - The pinned Atlas CLI on PATH — install with:
#       ATLAS_VERSION=v1.2.0 curl -sSf https://atlasgo.sh | sh -s -- -y -o "$HOME/.local/bin/atlas" --no-install --community
#     (keep this in sync with apps/server/cmd/migrate/gen/main.go's
#     atlasVersion constant and this script's want_atlas_version below).
#   - Go on PATH (for `go run ./cmd/migrate/gen -check`).
#   - DATABASE_URL pointing at a reachable, disposable Postgres server
#     (a throwaway "<dbname>_atlasdev" database is created on it and
#     dropped when the check finishes — see cmd/migrate/gen's own doc
#     comment). Defaults to the same local/CI Postgres every other
#     live-database Makefile target uses.
#
# Usage:
#   scripts/check-data-drift.sh
#   DATABASE_URL=postgres://... scripts/check-data-drift.sh
set -euo pipefail

# Keep in sync with apps/server/cmd/migrate/gen/main.go's atlasVersion
# constant: that file is the single source of truth for which Atlas
# release this pipeline is verified against.
want_atlas_version="v1.2.0"

default_database_url="postgres://aboutme:aboutme_dev@127.0.0.1:5432/aboutme?sslmode=disable"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root/apps/server"

echo "==> verifying the Atlas CLI is pinned to ${want_atlas_version}"
if ! command -v atlas >/dev/null 2>&1; then
  echo "check-data-drift: atlas CLI not found on PATH" >&2
  exit 1
fi
# Captures the FULL version token, including any -commit-canary suffix,
# not just the leading v#.#.# — a bare v#.#.# capture would let a canary
# build of the pinned release (e.g. v1.2.0-9a6bc60-canary) reduce to
# "v1.2.0" and pass as if it were the real tagged release.
got_atlas_version="$(atlas version | head -n1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+[^[:space:]]*' | head -n1 || true)"
if [ "$got_atlas_version" != "$want_atlas_version" ]; then
  echo "check-data-drift: atlas CLI version is '${got_atlas_version:-unknown}', want the pinned '${want_atlas_version}'" >&2
  echo "  install: ATLAS_VERSION=${want_atlas_version} curl -sSf https://atlasgo.sh | sh -s -- -y -o \"\$HOME/.local/bin/atlas\" --no-install --community" >&2
  exit 1
fi

echo "==> regenerating sqlc and checking for drift (including untracked files)"
if ! command -v sqlc >/dev/null 2>&1; then
  echo "check-data-drift: sqlc not found on PATH" >&2
  exit 1
fi
sqlc generate
drift="$(git status --porcelain --untracked-files=all -- internal/store)"
if [ -n "$drift" ]; then
  echo "check-data-drift: internal/store is out of date with sql/*.sql; run 'make sqlc-gen' and commit the result:" >&2
  echo "$drift" >&2
  exit 1
fi

echo "==> replaying goose migrations and diffing against sql/schema.sql (also validates hand-written extension declarations)"
export DATABASE_URL="${DATABASE_URL:-$default_database_url}"
go run ./cmd/migrate/gen -check

echo "check-data-drift: OK — sqlc output, migrations, and sql/schema.sql all agree"
