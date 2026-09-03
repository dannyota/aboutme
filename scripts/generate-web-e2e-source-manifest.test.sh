#!/usr/bin/env bash

# Black-box contract for automatic E2E source-manifest generation.
set -Eeuo pipefail
export LC_ALL=C

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SCRIPT=$ROOT/scripts/generate-web-e2e-source-manifest.sh
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-web-source-manifest.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

fail() {
  printf 'web-e2e-source-manifest-test: %s\n' "$*" >&2
  exit 1
}

[ -x "$SCRIPT" ] || fail "$SCRIPT is missing or not executable"

REPO=$WORK/repo
mkdir -p \
  "$REPO/apps/web/app/assets/fonts/tools" \
  "$REPO/apps/web/e2e" \
  "$REPO/apps/web/public" \
  "$REPO/apps/web/server" \
  "$REPO/apps/web/test/sanitizer" \
  "$REPO/apps/web/test/unit" \
  "$REPO/apps/web/types" \
  "$REPO/docs/api" \
  "$REPO/packages/schema/fixtures/bounds" \
  "$REPO/packages/schema/fixtures" \
  "$REPO/packages/schema/gen/go" \
  "$REPO/packages/schema/gen/ts" \
  "$REPO/packages/schema/validation" \
  "$REPO/scripts"
cp "$SCRIPT" "$REPO/scripts/generate-web-e2e-source-manifest.sh"

for path in \
  apps/web/app/app.vue \
  apps/web/app/assets/fonts/README.md \
  apps/web/app/assets/fonts/tools/generate.py \
  apps/web/e2e/corpus.spec.ts \
  apps/web/nuxt.config.ts \
  apps/web/package-lock.json \
  apps/web/package.json \
  apps/web/public/theme-bootstrap.js \
  apps/web/server/route.ts \
  apps/web/test/sanitizer/neutralization.ts \
  apps/web/test/unit/example.test.ts \
  apps/web/tsconfig.json \
  apps/web/types/public-render-worker.d.ts \
  docs/api/openapi.yaml \
  packages/schema/README.md \
  packages/schema/fixtures/bounds/unused.json \
  packages/schema/fixtures/full.json \
  packages/schema/fixtures/vn-full.json \
  packages/schema/gen/go/go.mod \
  packages/schema/gen/ts/resume.ts \
  packages/schema/package-lock.json \
  packages/schema/package.json \
  packages/schema/resume.schema.json \
  packages/schema/validation/unused.json \
  packages/schema/validation/store.ts; do
  printf 'fixture\n' >"$REPO/$path"
done

git -C "$REPO" init -q
git -C "$REPO" config user.email test@example.invalid
git -C "$REPO" config user.name 'Source Manifest Test'
git -C "$REPO" add -- apps docs packages scripts/generate-web-e2e-source-manifest.sh
git -C "$REPO" commit -qm base
printf '%s\n' apps/web/app/components/Deleted.vue \
  >"$REPO/scripts/web-e2e-source.manifest"

(
  cd "$REPO"
  scripts/generate-web-e2e-source-manifest.sh --update
)

cat >"$WORK/expected" <<'EOF'
apps/web/app/app.vue
apps/web/e2e/corpus.spec.ts
apps/web/nuxt.config.ts
apps/web/package-lock.json
apps/web/package.json
apps/web/public/theme-bootstrap.js
apps/web/server/route.ts
apps/web/test/sanitizer/neutralization.ts
apps/web/tsconfig.json
apps/web/types/public-render-worker.d.ts
docs/api/openapi.yaml
packages/schema/fixtures/full.json
packages/schema/fixtures/vn-full.json
packages/schema/gen/ts/resume.ts
packages/schema/package.json
packages/schema/resume.schema.json
packages/schema/validation/store.ts
EOF
cmp -s "$WORK/expected" "$REPO/scripts/web-e2e-source.manifest" ||
  fail 'update did not produce the closed, sorted source set'

(
  cd "$REPO"
  scripts/generate-web-e2e-source-manifest.sh --check
)

printf 'new source\n' >"$REPO/apps/web/app/new-source.ts"
cp "$REPO/scripts/web-e2e-source.manifest" "$WORK/before-drift-check"
if (
  cd "$REPO"
  scripts/generate-web-e2e-source-manifest.sh --check
) >"$WORK/drift.out" 2>&1; then
  fail 'check accepted an unlisted production source'
fi
cmp -s "$WORK/before-drift-check" "$REPO/scripts/web-e2e-source.manifest" ||
  fail 'check mutated the committed manifest'
grep -Fq 'run make web-source-manifest-update' "$WORK/drift.out" ||
  fail 'drift error did not name the update command'

(
  cd "$REPO"
  scripts/generate-web-e2e-source-manifest.sh --update
  scripts/generate-web-e2e-source-manifest.sh --check
)
grep -Fxq 'apps/web/app/new-source.ts' \
  "$REPO/scripts/web-e2e-source.manifest" ||
  fail 'update omitted an untracked, unignored production source'

rm "$REPO/apps/web/app/app.vue"
(
  cd "$REPO"
  scripts/generate-web-e2e-source-manifest.sh --update
)
if grep -Fxq 'apps/web/app/app.vue' "$REPO/scripts/web-e2e-source.manifest"; then
  fail 'update retained a deleted production source'
fi

printf 'secret-like fixture\n' >"$REPO/apps/web/app/credentials-local.json"
if (
  cd "$REPO"
  scripts/generate-web-e2e-source-manifest.sh --update
) >"$WORK/secret.out" 2>&1; then
  fail 'update accepted a secret-like source path'
fi
grep -Fq 'secret-like source path is forbidden' "$WORK/secret.out" ||
  fail 'secret-like rejection was not explicit'

MAKE_REPO=$WORK/make-repo
MAKE_BIN=$WORK/make-bin
MAKE_CALLS=$WORK/make-calls
mkdir -p "$MAKE_REPO" "$MAKE_BIN"
cp "$ROOT/Makefile" "$MAKE_REPO/Makefile"
git -C "$MAKE_REPO" init -q
git -C "$MAKE_REPO" config user.email test@example.invalid
git -C "$MAKE_REPO" config user.name 'Source Manifest Make Test'
git -C "$MAKE_REPO" commit --allow-empty -qm base
cat >"$MAKE_BIN/bash" <<'EOF'
#!/bin/bash
printf '%s\n' "$*" >>"$MAKE_CALLS"
EOF
chmod 0755 "$MAKE_BIN/bash"
(
  cd "$MAKE_REPO"
  PATH="$MAKE_BIN:/usr/bin:/bin" MAKE_CALLS="$MAKE_CALLS" \
    /usr/bin/make --no-print-directory web-source-build
)
cat >"$WORK/expected-make-calls" <<'EOF'
scripts/generate-web-e2e-source-manifest.sh --check
scripts/web-source-build.sh
EOF
cmp -s "$WORK/expected-make-calls" "$MAKE_CALLS" ||
  fail 'web-source-build did not check manifest drift before building'

printf 'web E2E source-manifest generation tests passed\n'
