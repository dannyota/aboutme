#!/usr/bin/env bash
# Deterministic P5A public HTTP capture. It seeds the frozen fixture into an
# isolated database and media root, launches the native stack against it, and
# records bounded viewer evidence through http://localhost:20080. It is the
# live slice of AC-PUB-003/004, AC-OPS-005/012b, and AC-SEC-001.
#
# The normal native stack is recorded and stopped first, then restored only if
# it was up. Every path touched is below the repository root and removed again;
# only the aboutme_p5a_fixture database is created and dropped.
set -Eeuo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
ROOT=$(pwd -P)

info() { printf '%s\n' "$*"; }
die() {
  printf 'p5a-native-http-capture: %s\n' "$*" >&2
  exit 1
}

readonly FIXTURE_DB=aboutme_p5a_fixture
readonly FIXTURE_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_p5a_fixture?sslmode=disable'
readonly FROZEN_NOW='2035-01-01T00:00:00Z'
readonly FIXTURE_RUNTIME=.dev/p5a-fixture-runtime
readonly FIXTURE_MEDIA=.dev/p5a-fixture-media
readonly FIXTURE_EVIDENCE=.dev/p5a-evidence
readonly PUBLIC_ORIGIN='http://localhost:20080'
readonly DB_CONTAINER=aboutme-test-db

normal_was_up=0
run_id=

cleanup() {
  local status=$?
  set +e
  # Stop the isolated stack first (its state root is the fixture runtime).
  ABOUTME_DEV_STATE_DIR="$FIXTURE_RUNTIME" bash scripts/dev-native.sh down >/dev/null 2>&1
  # Remove fixture rows and the photo object, then the whole fixture database.
  if [ -x "$ROOT/.dev/bin/p5a-native-fixture" ]; then
    p5a-native-fixture cleanup --database-url "$FIXTURE_DATABASE_URL" --media-root "$FIXTURE_MEDIA" --now "$FROZEN_NOW" >/dev/null 2>&1
  fi
  if podman ps --format '{{.Names}}' 2>/dev/null | grep -qx "$DB_CONTAINER"; then
    podman exec "$DB_CONTAINER" psql -U aboutme -d aboutme -tc \
      "SELECT 1 FROM pg_database WHERE datname='$FIXTURE_DB'" 2>/dev/null | grep -q 1 &&
      podman exec "$DB_CONTAINER" psql -U aboutme -d aboutme -c "DROP DATABASE $FIXTURE_DB" >/dev/null 2>&1
  fi
  rm -rf -- "$ROOT/$FIXTURE_RUNTIME" "$ROOT/$FIXTURE_MEDIA"
  # Restore the normal stack only if it was up before this capture began.
  if [ "$normal_was_up" = 1 ]; then
    bash scripts/dev-native.sh up >/dev/null 2>&1
  fi
  exit "$status"
}

# ---- capture helpers ---------------------------------------------------------

fetch() {
  local url=$1 out=$2
  local body headers
  body="$run_id/$out.body"
  headers="$run_id/$out.headers"
  local status
  status=$(curl -sS -o "$body" -D "$headers" -w '%{http_code}' --max-time 15 "$url")
  printf '%s' "$status" >"$run_id/$out.status"
  sha256sum "$body" | awk '{print $1}' >"$run_id/$out.body.sha256"
}

expect_status() {
  local out=$1 want=$2
  local got
  got=$(<"$run_id/$out.status")
  [ "$got" = "$want" ] || die "$out: expected HTTP $want, got $got"
}

no_set_cookie() {
  local out=$1
  if grep -qi '^set-cookie:' "$run_id/$out.headers"; then
    die "$out: public response carried a Set-Cookie header"
  fi
}

# ---- main --------------------------------------------------------------------

trap cleanup EXIT

info "--- recording normal native state"
if bash scripts/dev-native.sh status >/dev/null 2>&1; then
  normal_was_up=1
  bash scripts/dev-native.sh down >/dev/null
  info "--- normal native stack was up; stopped for the isolated capture"
else
  info "--- normal native stack is already down"
fi

info "--- database ($DB_CONTAINER)"
make test-db-up >/dev/null

info "--- creating and migrating $FIXTURE_DB"
if ! podman exec "$DB_CONTAINER" psql -U aboutme -d aboutme -tc \
  "SELECT 1 FROM pg_database WHERE datname='$FIXTURE_DB'" 2>/dev/null | grep -q 1; then
  podman exec "$DB_CONTAINER" psql -U aboutme -d aboutme -c "CREATE DATABASE $FIXTURE_DB" >/dev/null
fi
(cd "$ROOT/apps/server" && DATABASE_URL="$FIXTURE_DATABASE_URL" go run ./cmd/migrate >/dev/null)

mkdir -p "$ROOT/.dev/bin"
export PATH="$ROOT/.dev/bin:$PATH"
(cd "$ROOT/apps/server" && go build -o "$ROOT/.dev/bin/p5a-native-fixture" ./cmd/p5a-native-fixture)

info "--- seeding fixture"
p5a-native-fixture seed --database-url "$FIXTURE_DATABASE_URL" --media-root "$FIXTURE_MEDIA" --now "$FROZEN_NOW"

info "--- starting isolated native stack"
ABOUTME_DEV_DATABASE_URL="$FIXTURE_DATABASE_URL" \
  ABOUTME_DEV_STATE_DIR="$FIXTURE_RUNTIME" \
  ABOUTME_DEV_MEDIA_DIR="$FIXTURE_MEDIA" \
  bash scripts/dev-native.sh up >/dev/null

mkdir -p "$ROOT/$FIXTURE_EVIDENCE"
run_id=$(mktemp -d "$ROOT/$FIXTURE_EVIDENCE/run.XXXXXX")

info "--- capturing public routes"
# JSON, discoverable live
fetch "$PUBLIC_ORIGIN/api/v1/public/resumes/p5a-live-photo" json-live
expect_status json-live 200
no_set_cookie json-live
grep -qi '^etag:' "$run_id/json-live.headers" || die 'json-live: missing ETag'

# Conditional re-fetch proves the strict 304 path live (the "demonstrated live"
# slice of AC-OPS-012b): the exact body-digest ETag must round-trip to an empty
# 304, not a second 200.
json_live_etag=$(sed -n 's/^[Ee][Tt][Aa][Gg]:[[:space:]]*//p' "$run_id/json-live.headers" | tr -d '\r')
[ -n "$json_live_etag" ] || die 'json-live: empty ETag'
cond_status=$(curl -sS -o "$run_id/json-live-304.body" -D "$run_id/json-live-304.headers" \
  -w '%{http_code}' --max-time 15 -H "If-None-Match: $json_live_etag" \
  "$PUBLIC_ORIGIN/api/v1/public/resumes/p5a-live-photo")
[ "$cond_status" = "304" ] || die "json-live: expected HTTP 304 for If-None-Match, got $cond_status"
[ ! -s "$run_id/json-live-304.body" ] || die 'json-live: 304 response must have an empty body'

# JSON, private -> uniform 404
fetch "$PUBLIC_ORIGIN/api/v1/public/resumes/p5a-private" json-private
expect_status json-private 404

# JSON, old tombstone slug -> uniform 404
fetch "$PUBLIC_ORIGIN/api/v1/public/resumes/p5a-renamed-old" json-tombstone
expect_status json-tombstone 404

# Photo, discoverable live
fetch "$PUBLIC_ORIGIN/api/v1/public/resumes/p5a-live-photo/photo" photo-live
expect_status photo-live 200
grep -qi '^content-type: image/png' "$run_id/photo-live.headers" || die 'photo-live: not image/png'

# HTML, discoverable live
fetch "$PUBLIC_ORIGIN/p5a-live-photo" html-live
expect_status html-live 200
no_set_cookie html-live

# HTML, nondiscoverable -> 200 with noindex
fetch "$PUBLIC_ORIGIN/p5a-live-noindex" html-noindex
expect_status html-noindex 200
grep -qi '^x-robots-tag: noindex' "$run_id/html-noindex.headers" || die 'html-noindex: missing X-Robots-Tag noindex'

# HTML, old tombstone slug -> 404
fetch "$PUBLIC_ORIGIN/p5a-renamed-old" html-tombstone
expect_status html-tombstone 404

# Markdown, discoverable -> 200
fetch "$PUBLIC_ORIGIN/p5a-live-photo.md" md-live
expect_status md-live 200

# Markdown, nondiscoverable -> 404 (AC-PUB-004)
fetch "$PUBLIC_ORIGIN/p5a-live-noindex.md" md-noindex
expect_status md-noindex 404

# Discovery aggregate: sitemap lists discoverable, excludes nondiscoverable
fetch "$PUBLIC_ORIGIN/sitemap.xml" sitemap
expect_status sitemap 200
grep -q 'p5a-live-photo' "$run_id/sitemap.body" || die 'sitemap: missing p5a-live-photo'
if grep -q 'p5a-live-noindex' "$run_id/sitemap.body"; then
  die 'sitemap: nondiscoverable slug leaked into the sitemap'
fi

fetch "$PUBLIC_ORIGIN/robots.txt" robots
expect_status robots 200

fetch "$PUBLIC_ORIGIN/llms.txt" llms
expect_status llms 200

# Hostile rich text never survives the projection. Event handlers and
# javascript: URLs are hostile in every representation; JSON and Markdown must
# carry no <script> at all. The HTML page's two structural scripts (external
# hydration + discoverable JSON-LD) are Go-validated, not hostile content.
if grep -rqiE '\bon[a-z]+=|javascript:' "$run_id"/*.body; then
  die 'hostile event handler or javascript: URL leaked into a public representation'
fi
if grep -rqiE '<script' "$run_id/json-live.body" "$run_id/md-live.body"; then
  die 'raw <script> leaked into JSON or Markdown'
fi

# Every captured representation must match its frozen SHA-256, so any drift in
# the projection, renderer, sanitizer, or formats fails the capture.
node --input-type=module - "$run_id" "$ROOT/apps/server/cmd/p5a-native-fixture/testdata/hashes.json" <<'NODE'
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const [runId, hashesPath] = process.argv.slice(2);
const frozen = JSON.parse(readFileSync(hashesPath, 'utf8'));

assert.equal(
  frozen.responses['photo-live'],
  frozen.photo,
  'photo route body drifted from the frozen normalized PNG',
);
for (const [route, want] of Object.entries(frozen.responses)) {
  const got = readFileSync(join(runId, `${route}.body.sha256`), 'utf8').trim();
  assert.equal(got, want, `${route}: response body hash drifted`);
}
process.stdout.write('p5a-native-http-capture: hashes PASS\n');
NODE

info "--- capture complete; evidence: $run_id"
