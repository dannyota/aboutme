#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-pnpm-policy.XXXXXX")
WORK=$(cd "$WORK" && pwd -P)
trap 'rm -rf -- "$WORK"' EXIT

fail() {
  printf 'pnpm-policy-test: %s\n' "$*" >&2
  [[ -f $WORK/out ]] && sed 's/^/  | /' "$WORK/out" >&2
  exit 1
}

pin=$(awk '$1 == "pnpm" { print $2 }' "$ROOT/.tool-versions")
[[ $pin =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]] || fail '.tool-versions has no exact pnpm pin'
other=${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.$((BASH_REMATCH[3] + 1))
command -v pnpm >/dev/null || fail 'pnpm is not installed'
running=$(cd "$WORK" && pnpm --version </dev/null)
[[ $running == "$pin" ]] || fail "pnpm $running is running, .tool-versions pins $pin"

isolated=(--registry http://127.0.0.1:9/ --store-dir "$WORK/store"
  --state-dir "$WORK/state" "--config.cache-dir=$WORK/cache")

project() {
  local dir=$1 manifest=$2 settings=$3
  mkdir -p "$WORK/$dir"
  printf '%s\n' "$manifest" >"$WORK/$dir/package.json"
  sed '/^packages:$/,/^$/d' "$ROOT/pnpm-workspace.yaml" >"$WORK/$dir/pnpm-workspace.yaml"
  [[ $settings == committed ]] ||
    sed -i '/^pmOnFail:/d' "$WORK/$dir/pnpm-workspace.yaml"
}

run_pnpm() {
  local dir=$1 status=0
  shift
  (cd "$WORK/$dir" && pnpm "$@" </dev/null) >"$WORK/out" 2>&1 || status=$?
  return "$status"
}

override_drift() {
  node - "$1" "$2" <<'NODE'
const fs = require('node:fs');
const [workspace, manifestPath] = process.argv.slice(2);
const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
const declared = { ...manifest.dependencies, ...manifest.devDependencies };
const text = fs.readFileSync(workspace, 'utf8');
for (const [selector, name] of [['openapi-typescript>typescript', 'typescript'], ['altcha-lib>h3', 'h3']]) {
  const match = new RegExp(`^  '?${selector}'?: '?([^'\\s]+)'?$`, 'm').exec(text);
  if (!match) console.log(`missing override ${selector}`);
  else if (match[1] !== declared[name]) console.log(`${selector} is ${match[1]}, apps/web declares ${name} ${declared[name]}`);
}
NODE
}

drift=$(override_drift "$ROOT/pnpm-workspace.yaml" "$ROOT/apps/web/package.json") ||
  fail 'cannot compare the overrides with apps/web/package.json'
[[ -z $drift ]] || fail "override literals drifted: $drift"
sed 's/^  altcha-lib>h3: .*/  altcha-lib>h3: 0.0.1/' "$ROOT/pnpm-workspace.yaml" >"$WORK/drifted.yaml"
drift=$(override_drift "$WORK/drifted.yaml" "$ROOT/apps/web/package.json")
[[ $drift == 'altcha-lib>h3 is 0.0.1, apps/web declares h3 '* ]] ||
  fail "a drifted h3 override was not reported: ${drift:-nothing}"
sed 's/^  openapi-typescript>typescript: .*/  openapi-typescript>typescript: 0.0.1/' \
  "$ROOT/pnpm-workspace.yaml" >"$WORK/drifted.yaml"
drift=$(override_drift "$WORK/drifted.yaml" "$ROOT/apps/web/package.json")
[[ $drift == 'openapi-typescript>typescript is 0.0.1, apps/web declares typescript '* ]] ||
  fail "a drifted typescript override was not reported: ${drift:-nothing}"

grep -Fqx 'pmOnFail: error' "$ROOT/pnpm-workspace.yaml" ||
  fail 'pnpm-workspace.yaml must set pmOnFail: error'
grep -Fqx 'verifyDepsBeforeRun: error' "$ROOT/pnpm-workspace.yaml" ||
  fail 'pnpm-workspace.yaml must set verifyDepsBeforeRun: error'

project mismatch "{\"name\":\"mismatch\",\"private\":true,\"packageManager\":\"pnpm@$other\"}" committed
if run_pnpm mismatch install --offline "${isolated[@]}"; then
  fail "pnpm $pin ran a project pinned to pnpm $other"
fi
grep -Fq 'ERR_PNPM_BAD_PM_VERSION' "$WORK/out" || fail 'missing the version mismatch refusal'
if grep -Fq "resolve pnpm@$other" "$WORK/out"; then
  fail 'pnpm tried to fetch the pinned pnpm'
fi
[[ ! -e $WORK/store && ! -e $WORK/cache/v12 ]] || fail 'the refused command wrote the pnpm store or cache'

project control "{\"name\":\"control\",\"private\":true,\"packageManager\":\"pnpm@$other\"}" without-pm-on-fail
if run_pnpm control install --offline "${isolated[@]}"; then
  fail 'the control project installed without its pinned pnpm'
fi
grep -Fq "resolve pnpm@$other" "$WORK/out" ||
  fail 'without pmOnFail pnpm should try to fetch the pinned pnpm; the control no longer proves the setting'

project stale '{"name":"stale","private":true,"scripts":{"probe":"node -e \"process.exit(0)\""}}' committed
run_pnpm stale install --offline "${isolated[@]}" || fail 'the fresh fixture did not install offline'
run_pnpm stale run "${isolated[@]}" probe || fail 'pnpm run refused current dependencies'
cp "$WORK/stale/pnpm-lock.yaml" "$WORK/stale.lock"
node -e 'const fs = require("node:fs"); const file = process.argv[1];
  const manifest = JSON.parse(fs.readFileSync(file, "utf8"));
  manifest.dependencies = {"left-pad": "1.3.0"};
  fs.writeFileSync(file, JSON.stringify(manifest));' "$WORK/stale/package.json"
for command in "run probe" "exec node -e 0"; do
  read -r -a words <<<"$command"
  if run_pnpm stale "${words[0]}" "${isolated[@]}" "${words[@]:1}"; then
    fail "pnpm ${words[0]} ran with stale dependencies"
  fi
  grep -Fq 'ERR_PNPM_VERIFY_DEPS_BEFORE_RUN' "$WORK/out" ||
    fail "pnpm ${words[0]} did not report stale dependencies"
  [[ ! -e $WORK/stale/node_modules/left-pad ]] || fail "pnpm ${words[0]} installed a dependency"
  cmp -s "$WORK/stale.lock" "$WORK/stale/pnpm-lock.yaml" || fail "pnpm ${words[0]} rewrote the lockfile"
done

printf 'pnpm-policy-test: passed\n'
