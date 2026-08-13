#!/usr/bin/env bash

# Black-box contract for the reviewed Playwright candidate-source boundary.
set -Eeuo pipefail
export LC_ALL=C
export TZ=UTC

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SCRIPT=$ROOT/scripts/web-e2e-source.sh
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-web-e2e-source.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

fail() {
  printf 'web-e2e-source-test: %s\n' "$*" >&2
  exit 1
}

[ -x "$SCRIPT" ] || fail "$SCRIPT is missing or not executable"

new_repo() {
  local name=$1
  REPO=$WORK/$name
  mkdir -p "$REPO/apps/web" "$REPO/packages/schema" "$REPO/scripts"
  git -C "$REPO" init -q
  git -C "$REPO" config user.email test@example.invalid
  git -C "$REPO" config user.name 'Source Boundary Test'
  printf '/.dev/\n' >>"$REPO/.git/info/exclude"
  printf '{"private":true}\n' >"$REPO/package.json"
  printf '{"lockfileVersion":3}\n' >"$REPO/package-lock.json"
  printf 'console.log("fixture")\n' >"$REPO/apps/web/app.ts"
  printf '{"type":"object"}\n' >"$REPO/packages/schema/resume.json"
  git -C "$REPO" add -- package.json package-lock.json apps/web/app.ts \
    packages/schema/resume.json
  git -C "$REPO" commit -qm base
  COMMIT=$(git -C "$REPO" rev-parse HEAD)
  MANIFEST=$REPO/scripts/web-e2e-source.manifest
  OUTPUT=$REPO/.dev/candidate.tar
}

write_manifest() {
  printf '%s\n' "$@" | LC_ALL=C sort >"$MANIFEST"
  commit_manifest
}

commit_manifest() {
  git -C "$REPO" add -- scripts/web-e2e-source.manifest
  git -C "$REPO" commit -qm manifest
  COMMIT=$(git -C "$REPO" rev-parse HEAD)
}

run_boundary() {
  (
    cd "$REPO"
    "$SCRIPT" "$COMMIT" "$MANIFEST" "$OUTPUT"
  )
}

assert_rejected() {
  local name=$1
  shift
  if "$@" >"$WORK/$name.out" 2>&1; then
    fail "$name was accepted"
  fi
  [ ! -e "$OUTPUT" ] || fail "$name left an archive"
}

assert_tar_is_deterministic() {
  local first=$1 second=$2
  local expected=$WORK/expected-members actual=$WORK/actual-members
  cmp -s "$first" "$second" || fail 'identical inputs produced different tar bytes'
  printf '%s\n' apps/web/app.ts package-lock.json package.json \
    packages/schema/resume.json >"$expected"
  tar -tf "$first" >"$actual"
  cmp -s "$expected" "$actual" || fail 'archive member list differs from manifest'
  tar --numeric-owner --full-time -tvf "$first" |
    awk '$2 != "0/0" || $4 != "1970-01-01" || $5 != "00:00:00" {exit 1}' ||
    fail 'archive owner, group, or timestamps are not deterministic'
}

new_repo valid
write_manifest apps/web/app.ts package-lock.json package.json \
  packages/schema/resume.json
run_boundary
[ -f "$OUTPUT" ] || fail 'valid input did not create an archive'
mkdir -p "$WORK/extracted"
tar -xf "$OUTPUT" -C "$WORK/extracted"
cmp -s "$REPO/apps/web/app.ts" "$WORK/extracted/apps/web/app.ts" ||
  fail 'archive did not contain the committed bytes'
[ "$(stat -c %a "$WORK/extracted/apps/web/app.ts")" = 644 ] ||
  fail 'archive did not preserve a regular file mode'
first=$OUTPUT
second=$REPO/.dev/candidate-second.tar
OUTPUT=$second
run_boundary
assert_tar_is_deterministic "$first" "$second"

new_repo committed-bytes
chmod 0755 "$REPO/apps/web/app.ts"
git -C "$REPO" add -- apps/web/app.ts
git -C "$REPO" commit -qm executable
write_manifest apps/web/app.ts
run_boundary
mkdir -p "$WORK/committed-bytes-extracted"
tar -xf "$OUTPUT" -C "$WORK/committed-bytes-extracted"
git -C "$REPO" cat-file blob "$COMMIT:apps/web/app.ts" |
  cmp -s - "$WORK/committed-bytes-extracted/apps/web/app.ts" ||
  fail 'archive bytes came from the worktree instead of the commit'
[ "$(stat -c %a "$WORK/committed-bytes-extracted/apps/web/app.ts")" = 755 ] ||
  fail 'archive did not preserve the committed executable mode'

new_repo assume-unchanged
write_manifest apps/web/app.ts
git -C "$REPO" update-index --assume-unchanged apps/web/app.ts
printf 'hidden assume-unchanged replacement\n' >"$REPO/apps/web/app.ts"
assert_rejected assume-unchanged run_boundary

new_repo skip-worktree
write_manifest apps/web/app.ts
git -C "$REPO" update-index --skip-worktree apps/web/app.ts
printf 'hidden skip-worktree replacement\n' >"$REPO/apps/web/app.ts"
assert_rejected skip-worktree run_boundary

new_repo worktree-bytes
write_manifest apps/web/app.ts
printf 'changed but uncommitted\n' >"$REPO/apps/web/app.ts"
assert_rejected dirty-worktree run_boundary

new_repo dirty-index
write_manifest apps/web/app.ts
printf 'changed in index\n' >"$REPO/apps/web/app.ts"
git -C "$REPO" add -- apps/web/app.ts
assert_rejected dirty-index run_boundary

new_repo untracked
printf 'ordinary untracked\n' >"$REPO/apps/web/untracked.ts"
write_manifest apps/web/untracked.ts
assert_rejected untracked-listed run_boundary

new_repo ignored
printf 'apps/web/ignored.ts\n' >"$REPO/.gitignore"
git -C "$REPO" add -- .gitignore
git -C "$REPO" commit -qm ignore
COMMIT=$(git -C "$REPO" rev-parse HEAD)
printf 'ignored\n' >"$REPO/apps/web/ignored.ts"
write_manifest apps/web/ignored.ts
assert_rejected ignored-listed run_boundary

for path in \
  apps/web/.env \
  apps/web/.env.production \
  apps/web/.Git-metadata/config \
  apps/web/.DEV/state \
  apps/web/.superpowers/task \
  apps/web/node_modules/module.js \
  apps/web/.nuxt/output.js \
  apps/web/.output/server.js \
  apps/web/dist/bundle.js \
  apps/web/coverage/report.json \
  apps/web/test-results/result.json \
  apps/web/playwright-report/index.html \
  apps/web/Credentials-prod.json \
  apps/web/SecretsFixture.txt \
  apps/web/id_rsa_test \
  apps/web/id_ed25519_test \
  apps/web/server.KEY \
  apps/web/client.PEM; do
  case_name=$(printf '%s' "$path" | tr '/.' '__')

  new_repo "tracked-secret-$case_name"
  mkdir -p "$(dirname "$REPO/$path")"
  printf 'fixture secret-like bytes\n' >"$REPO/$path"
  git -C "$REPO" add -f -- "$path"
  git -C "$REPO" commit -qm secret-like
  COMMIT=$(git -C "$REPO" rev-parse HEAD)
  write_manifest "$path"
  assert_rejected "tracked-secret-$case_name" run_boundary

  new_repo "untracked-secret-$case_name"
  mkdir -p "$(dirname "$REPO/$path")"
  printf 'fixture secret-like bytes\n' >"$REPO/$path"
  write_manifest "$path"
  assert_rejected "untracked-secret-$case_name" run_boundary
done

new_repo traversal
write_manifest ../package.json
assert_rejected traversal run_boundary

new_repo absolute
write_manifest /etc/passwd
assert_rejected absolute run_boundary

new_repo outside-allowlist
printf 'not allowed\n' >"$REPO/scripts/unlisted.sh"
git -C "$REPO" add -- scripts/unlisted.sh
git -C "$REPO" commit -qm outside
COMMIT=$(git -C "$REPO" rev-parse HEAD)
write_manifest scripts/unlisted.sh
assert_rejected outside-allowlist run_boundary

new_repo duplicate
printf '%s\n' apps/web/app.ts apps/web/app.ts >"$MANIFEST"
commit_manifest
assert_rejected duplicate run_boundary

new_repo unsorted
printf '%s\n' package.json apps/web/app.ts >"$MANIFEST"
commit_manifest
assert_rejected unsorted run_boundary

new_repo blank
printf '%s\n\n' apps/web/app.ts >"$MANIFEST"
commit_manifest
assert_rejected blank run_boundary

new_repo glob
write_manifest 'apps/web/*.ts'
assert_rejected glob run_boundary

new_repo symlink
ln -s app.ts "$REPO/apps/web/link.ts"
git -C "$REPO" add -- apps/web/link.ts
git -C "$REPO" commit -qm symlink
COMMIT=$(git -C "$REPO" rev-parse HEAD)
write_manifest apps/web/link.ts
assert_rejected symlink run_boundary

new_repo special
mkfifo "$REPO/apps/web/fifo"
write_manifest apps/web/fifo
assert_rejected special run_boundary

new_repo invalid-commit
write_manifest apps/web/app.ts
COMMIT=not-a-commit
assert_rejected invalid-commit run_boundary

new_repo abbreviated-commit
write_manifest apps/web/app.ts
COMMIT=${COMMIT:0:12}
assert_rejected abbreviated-commit run_boundary

new_repo commit-ref
write_manifest apps/web/app.ts
COMMIT=HEAD
assert_rejected commit-ref run_boundary

new_repo uppercase-commit
write_manifest apps/web/app.ts
COMMIT=${COMMIT^^}
assert_rejected uppercase-commit run_boundary

new_repo tree-object
write_manifest apps/web/app.ts
COMMIT=$(git -C "$REPO" rev-parse HEAD^{tree})
assert_rejected tree-object run_boundary

new_repo non-head
old_commit=$COMMIT
printf 'second\n' >"$REPO/apps/web/second.ts"
git -C "$REPO" add -- apps/web/second.ts
git -C "$REPO" commit -qm second
write_manifest apps/web/app.ts
COMMIT=$old_commit
assert_rejected non-head run_boundary

new_repo output-exists
write_manifest apps/web/app.ts
mkdir -p "$(dirname "$OUTPUT")"
printf 'existing\n' >"$OUTPUT"
if run_boundary >"$WORK/output-exists.out" 2>&1; then
  fail 'existing output was accepted'
fi
grep -qx existing "$OUTPUT" || fail 'existing output was changed'

new_repo output-outside-dev
write_manifest apps/web/app.ts
OUTPUT=$REPO/candidate.tar
assert_rejected output-outside-dev run_boundary

new_repo output-symlink-escape
write_manifest apps/web/app.ts
mkdir -p "$REPO/.dev"
ln -s "$WORK" "$REPO/.dev/escape"
OUTPUT=$REPO/.dev/escape/candidate.tar
assert_rejected output-symlink-escape run_boundary

new_repo head-race
write_manifest apps/web/app.ts
pause=$WORK/head-race-pause
(
  cd "$REPO"
  WEB_E2E_SOURCE_TEST_PAUSE_FILE=$pause \
    "$SCRIPT" "$COMMIT" "$MANIFEST" "$OUTPUT"
) >"$WORK/head-race.out" 2>&1 &
boundary_pid=$!
for _ in $(seq 1 500); do
  [ -e "$pause.ready" ] && break
  kill -0 "$boundary_pid" 2>/dev/null || break
  sleep 0.01
done
[ -e "$pause.ready" ] || fail 'HEAD-race seam did not reach validation pause'
printf 'racing commit\n' >"$REPO/apps/web/race.ts"
git -C "$REPO" add -- apps/web/race.ts
git -C "$REPO" commit -qm race
: >"$pause.continue"
if wait "$boundary_pid"; then
  fail 'commit changed during archive creation but was accepted'
fi
[ ! -e "$OUTPUT" ] || fail 'HEAD race left an archive'

printf 'ok - Playwright source boundary rejects non-commit and secret-like inputs\n'
