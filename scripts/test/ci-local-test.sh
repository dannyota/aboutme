#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-ci-local.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT
WORK=$(cd "$WORK" && pwd -P)

fail() { printf 'ci-local-test: %s\n' "$*" >&2; cat "$WORK/out" >&2; exit 1; }
repo=$WORK/main
mkdir -p "$repo/scripts/lib" "$repo/apps/web"
cp "$ROOT/scripts/ci-local.sh" "$repo/scripts/"
cp "$ROOT/scripts/lib/platform.sh" "$ROOT/scripts/lib/local-check.py" "$repo/scripts/lib/"
cat >"$repo/scripts/run-capped.sh" <<'STUB'
#!/usr/bin/env bash
printf '%s report=%s\n' "$*" "$RUN_CAPPED_REPORT" >>"$CALL_LOG"
[ -z "${STUB_CAPPED_STATUS:-}" ] || exit "$STUB_CAPPED_STATUS"
shift 3
exec "$@"
STUB
cat >"$repo/scripts/ci.sh" <<'STUB'
#!/usr/bin/env bash
[ -d node_modules ] && [ -d apps/web/node_modules ] && [ -f apps/web/.nuxt/eslint.config.mjs ] || exit 9
printf '%s\n' '--- docs-lint OK (1s)' 'Skipping S3 suites: their host-networked MinIO client needs Linux; CI covers them.'
if [ "${STUB_FAIL:-0}" = 1 ]; then printf '%s\n' '--- web FAILED (1s)'; exit 1; fi
printf '%s\n' '--- web OK (1s)'
STUB
printf '{}\n' >"$repo/package.json"
printf '{}\n' >"$repo/apps/web/package.json"
root_packages=(prettier markdownlint-cli2 markdownlint)
web_packages=(eslint @nuxt/eslint @nuxt/eslint-config @typescript-eslint/parser eslint-plugin-vue vue-eslint-parser @stylistic/eslint-plugin)
install_fixture() {
  local dir=$1 package
  shift
  mkdir -p "$dir/node_modules"
  printf '{"packages":{' >"$dir/package-lock.json"
  local sep=
  for package in "$@"; do
    mkdir -p "$dir/node_modules/$package"
    printf '{"version":"1.0.0"}\n' >"$dir/node_modules/$package/package.json"
    printf '%s"node_modules/%s":{"version":"1.0.0"}' "$sep" "$package" >>"$dir/package-lock.json"
    sep=,
  done
  printf '}}\n' >>"$dir/package-lock.json"
  cp "$dir/package-lock.json" "$dir/node_modules/.package-lock.json"
}
install_fixture "$repo" "${root_packages[@]}"
install_fixture "$repo/apps/web" "${web_packages[@]}"
mkdir -p "$repo/apps/web/.nuxt"
printf 'export default []\n' >"$repo/apps/web/.nuxt/eslint.config.mjs"
git -C "$repo" init -q
git -C "$repo" config user.name Test
git -C "$repo" config user.email test@example.invalid
printf 'node_modules\n.nuxt\n' >"$repo/.git/info/exclude"
git -C "$repo" add scripts package.json package-lock.json apps/web/package.json apps/web/package-lock.json
git -C "$repo" commit -qm fixture
git -C "$repo" worktree add -q -b fixture "$WORK/tree"
export CALL_LOG=$WORK/calls
run() { (cd "$WORK/tree" && CI_LOCAL_LOCK_WAIT=0 bash scripts/ci-local.sh) >"$WORK/out" 2>&1; }
assert_clean() { [[ ! -e $WORK/tree/node_modules && ! -e $WORK/tree/apps/web/node_modules && ! -e $WORK/tree/apps/web/.nuxt ]] || fail 'borrowed paths remain'; }
run || fail 'successful run failed'
grep -q -- '--timeout 3600 -- bash .* --capped report=1' "$CALL_LOG" || fail 'missing watchdog arguments'
grep -q 'PASS.*docs-lint' "$WORK/out" || fail 'missing passed summary'
grep -q 'SKIP.*S3 suites' "$WORK/out" || fail 'missing skipped summary'
assert_clean
STUB_FAIL=1 run && fail 'failure was accepted'
grep -q 'FAIL.*web' "$WORK/out" || fail 'missing failure summary'
assert_clean
STUB_CAPPED_STATUS=125 run && fail 'watchdog refusal was accepted'
grep -q 'watchdog could not start' "$WORK/out" || fail 'missing watchdog refusal'
python3 - "$repo/.git/aboutme-local-check.lock" "$WORK/locked" <<'PY' &
import fcntl, pathlib, sys, time
with open(sys.argv[1], 'w') as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    pathlib.Path(sys.argv[2]).touch()
    while not pathlib.Path(sys.argv[2] + '.done').exists():
        time.sleep(0.05)
PY
holder=$!
while [[ ! -f $WORK/locked ]]; do sleep 0.05; done
run && fail 'held lock was accepted'
grep -q 'another local check' "$WORK/out" || fail 'missing lock refusal'
touch "$WORK/locked.done"
wait "$holder"
mv "$repo/node_modules" "$repo/missing-modules"
run && fail 'missing dependencies were accepted'
grep -q 'node_modules' "$WORK/out" || fail 'missing dependency refusal'
mv "$repo/missing-modules" "$repo/node_modules"
mv "$repo/apps/web/.nuxt" "$repo/apps/web/missing-nuxt"
run && fail 'missing generated Nuxt configuration was accepted'
grep -q 'no generated' "$WORK/out" || fail 'missing Nuxt refusal'
assert_clean
mv "$repo/apps/web/missing-nuxt" "$repo/apps/web/.nuxt"
printf '{"version":"0.9.0"}\n' >"$repo/apps/web/node_modules/eslint/package.json"
run && fail 'stale dependencies were accepted'
grep -q 'stale' "$WORK/out" || fail 'missing stale dependency refusal'
assert_clean
printf '{"version":"1.0.0"}\n' >"$repo/apps/web/node_modules/eslint/package.json"
printf 'changed\n' >>"$WORK/tree/apps/web/package-lock.json"
run && fail 'different lockfile was accepted'
grep -q 'lockfile differs' "$WORK/out" || fail 'missing lockfile refusal'
assert_clean
cp "$repo/apps/web/package-lock.json" "$WORK/tree/apps/web/package-lock.json"
mkdir -p "$repo/apps/web/node_modules/@aboutme" "$repo/packages/schema" "$WORK/tree/packages/schema"
printf 'main\n' >"$repo/packages/schema/fixture"
printf 'different\n' >"$WORK/tree/packages/schema/fixture"
ln -s "$repo/packages/schema" "$repo/apps/web/node_modules/@aboutme/schema"
run && fail 'different linked schema was accepted'
grep -q 'different main checkout schema' "$WORK/out" || fail 'missing schema refusal'
assert_clean
printf 'ci-local-test: passed\n'
