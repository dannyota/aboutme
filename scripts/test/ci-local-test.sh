#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-ci-local.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT
WORK=$(cd "$WORK" && pwd -P)

fail() { printf 'ci-local-test: %s\n' "$*" >&2; cat "$WORK/out" >&2; exit 1; }
repo=$WORK/main
mkdir -p "$repo/scripts/lib" "$repo/apps/web" "$repo/deploy/dev-https-browser" "$WORK/bin"
cp "$ROOT/scripts/ci-local.sh" "$repo/scripts/"
cp "$ROOT/scripts/lib/platform.sh" "$ROOT/scripts/lib/local-check.py" "$repo/scripts/lib/"
cat >"$repo/scripts/run-capped.sh" <<'STUB'
#!/usr/bin/env bash
printf 'capped %s report=%s\n' "$*" "$RUN_CAPPED_REPORT" >>"$CALL_LOG"
[ -z "${STUB_CAPPED_STATUS:-}" ] || exit "$STUB_CAPPED_STATUS"
shift 3
exec "$@"
STUB
cat >"$WORK/bin/npm" <<'STUB'
#!/usr/bin/env bash
printf 'npm %s %s\n' "$PWD" "$*" >>"$CALL_LOG"
[ "$PWD" != "${STUB_INSTALL_FAIL:-}" ] || exit 1
mkdir -p node_modules
if [[ $PWD == */apps/web ]]; then mkdir -p .nuxt; touch .nuxt/eslint.config.mjs; fi
STUB
chmod +x "$WORK/bin/npm"
cat >"$repo/scripts/ci.sh" <<'STUB'
#!/usr/bin/env bash
printf 'ci %s\n' "$PWD" >>"$CALL_LOG"
[[ -z $(find . -type l -print -quit) ]] || exit 8
[ -d node_modules ] && [ -d apps/web/node_modules ] && [ -f apps/web/.nuxt/eslint.config.mjs ] &&
  [ -d deploy/dev-https-browser/node_modules ] || exit 9
printf '%s\n' '--- docs-lint OK (1s)' 'Skipping S3 suites: their host-networked MinIO client needs Linux; CI covers them.'
if [ "${STUB_FAIL:-0}" = 1 ]; then printf '%s\n' '--- web FAILED (1s)'; exit 1; fi
printf '%s\n' '--- web OK (1s)'
STUB
git -C "$repo" init -q
git -C "$repo" config user.name Test
git -C "$repo" config user.email test@example.invalid
git -C "$repo" add scripts
git -C "$repo" commit -qm fixture
git -C "$repo" worktree add -q -b fixture "$WORK/tree"
mkdir -p "$WORK/tree/apps/web" "$WORK/tree/deploy/dev-https-browser"
export CALL_LOG=$WORK/calls PATH="$WORK/bin:$PATH"
run() { : >"$CALL_LOG"; (cd "$WORK/tree" && CI_LOCAL_LOCK_WAIT=0 bash scripts/ci-local.sh) >"$WORK/out" 2>&1; }
run || fail 'successful run failed'
grep -q -- '--timeout 3600 -- bash .* --capped report=1' "$CALL_LOG" || fail 'missing watchdog arguments'
printf 'npm %s ci --prefer-offline --no-audit --no-fund\n' "$WORK/tree" "$WORK/tree/apps/web" "$WORK/tree/deploy/dev-https-browser" >"$WORK/expected"
grep '^npm ' "$CALL_LOG" >"$WORK/actual"
cmp -s "$WORK/expected" "$WORK/actual" || fail 'incorrect dependency installs'
[[ -z $(find "$WORK/tree" -type l -print -quit) ]] || fail 'worktree contains a symlink'
grep -q 'PASS.*docs-lint' "$WORK/out" || fail 'missing passed summary'
grep -q 'SKIP.*S3 suites' "$WORK/out" || fail 'missing skipped summary'
grep -q 'PASS: local CI' "$WORK/out" || fail 'missing final pass'
STUB_FAIL=1 run && fail 'failure was accepted'
grep -q 'FAIL.*web' "$WORK/out" || fail 'missing failure summary'
grep -q 'FAIL: local CI exited 1' "$WORK/out" || fail 'missing final failure'
for dir in "$WORK/tree" "$WORK/tree/apps/web" "$WORK/tree/deploy/dev-https-browser"; do
  STUB_INSTALL_FAIL=$dir run && fail 'install failure was accepted'
  grep -q 'refused: dependency installation failed' "$WORK/out" || fail 'missing install refusal'
  if grep -q '^ci ' "$CALL_LOG"; then fail 'CI ran after install failure'; fi
  [[ $(tail -n 1 "$CALL_LOG") == "npm $dir ci --prefer-offline --no-audit --no-fund" ]] || fail 'install continued after failure'
done
STUB_CAPPED_STATUS=125 run && fail 'watchdog refusal was accepted'
grep -q 'watchdog could not start' "$WORK/out" || fail 'missing watchdog refusal'
if grep -qE '^(npm|ci) ' "$CALL_LOG"; then fail 'work ran after watchdog refusal'; fi
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
if [[ -s $CALL_LOG ]]; then fail 'work ran without the lock'; fi
touch "$WORK/locked.done"
wait "$holder"
printf 'ci-local-test: passed\n'
