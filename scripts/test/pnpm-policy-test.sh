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
running=$(cd "$ROOT" && pnpm --version </dev/null)
[[ $running == "$pin" ]] || fail "pnpm $running runs in the repository, .tool-versions pins $pin"
npm_installed=1
[[ $(realpath "$(command -v pnpm)") != */corepack/* ]] || npm_installed=0

isolated=(--registry http://127.0.0.1:9/ --store-dir "$WORK/store"
  --state-dir "$WORK/state" "--config.cache-dir=$WORK/cache")

project() {
  local dir=$1 manifest=$2 settings=$3
  mkdir -p "$WORK/$dir"
  printf '%s\n' "$manifest" >"$WORK/$dir/package.json"
  case $settings in
  committed) sed '/^packages:$/,/^$/d' "$ROOT/pnpm-workspace.yaml" >"$WORK/$dir/pnpm-workspace.yaml" ;;
  without-pm-on-fail)
    sed -e '/^packages:$/,/^$/d' -e '/^pmOnFail:/d' "$ROOT/pnpm-workspace.yaml" \
      >"$WORK/$dir/pnpm-workspace.yaml"
    ;;
  pin-unrecorded)
    sed -e '/^packages:$/,/^$/d' -e 's/^pmOnFail: .*/pmOnFail: ignore/' "$ROOT/pnpm-workspace.yaml" \
      >"$WORK/$dir/pnpm-workspace.yaml"
    ;;
  esac
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

external_drift() {
  node - "$1" "$2" "$3" <<'NODE'
const fs = require('node:fs');
const [lockPath, manifestPath, listPath] = process.argv.slice(2);
const declared = JSON.parse(fs.readFileSync(manifestPath, 'utf8')).dependencies ?? {};
const list = fs.readFileSync(listPath, 'utf8').split('\n').filter(Boolean).map((line) => line.split(/\s+/));
const docs = fs.readFileSync(lockPath, 'utf8').split(/^---$/m);
const lines = docs[docs.length - 1].split('\n');
const versions = new Map();
const consumers = new Map();
const note = (map, name, version) => map.set(name, (map.get(name) ?? new Set()).add(version));
const bare = (version) => version.replace(/^'|'$/g, '').split('(')[0];
let section = '', key = '', importer = '', entry = '', webVersions = new Map();
for (const line of lines) {
  if (/^\S/.test(line)) { section = line.replace(/:$/, ''); continue; }
  let match;
  if ((match = /^  (?! )'?(.+?)'?:(?: \{\})?$/.exec(line))) {
    key = match[1];
    if (section === 'importers') importer = key;
    if (section === 'packages' || section === 'snapshots') {
      const at = key.indexOf('@', 1);
      note(versions, key.slice(0, at), bare(key.slice(at + 1)));
    }
    continue;
  }
  if (section === 'importers' && (match = /^      '?([^':]+)'?:$/.exec(line))) { entry = match[1]; continue; }
  if (section === 'importers' && (match = /^        version: (.+)$/.exec(line))) {
    if (!match[1].startsWith('link:')) note(versions, entry, bare(match[1]));
    if (importer === 'apps/web') webVersions.set(entry, bare(match[1]));
    continue;
  }
  if (section === 'snapshots' && (match = /^      '?([^':]+)'?: (.+)$/.exec(line))) {
    note(versions, match[1], bare(match[2]));
    const at = key.indexOf('@', 1);
    note(consumers, `${key.slice(0, at)}@${bare(key.slice(at + 1))}>${match[1]}`, bare(match[2]));
  }
}
for (const [name, consumer] of list) {
  const want = declared[name];
  const resolved = [...(versions.get(name) ?? [])].sort();
  if (!want) { console.log(`apps/web does not declare ${name}`); continue; }
  if (webVersions.get(name) !== want) console.log(`apps/web declares ${name} ${want}, its lockfile importer resolves ${webVersions.get(name) ?? 'nothing'}`);
  if (!consumer) {
    if (resolved.length !== 1 || resolved[0] !== want) console.log(`${name} resolves to ${resolved.join(', ') || 'nothing'} in the lockfile, apps/web declares ${want}`);
    continue;
  }
  const at = `${consumer}@${webVersions.get(consumer)}>${name}`;
  const used = [...(consumers.get(at) ?? [])].sort();
  if (used.length !== 1 || used[0] !== want) console.log(`${consumer} ${webVersions.get(consumer)} resolves ${name} ${used.join(', ') || 'nothing'}, apps/web declares ${want}`);
}
NODE
}

externals="$ROOT/scripts/test/pnpm-externals.txt"
drift=$(external_drift "$ROOT/pnpm-lock.yaml" "$ROOT/apps/web/package.json" "$externals") ||
  fail 'cannot compare the externalized packages with pnpm-lock.yaml'
[[ -z $drift ]] || fail "externalized packages drifted: $drift"
awk '{ print } /^packages:$/ { print ""; print "  defu@6.1.6:"; print "    resolution: {integrity: sha512-drift}" }' \
  "$ROOT/pnpm-lock.yaml" >"$WORK/second-version.yaml"
drift=$(external_drift "$WORK/second-version.yaml" "$ROOT/apps/web/package.json" "$externals")
[[ $drift == *'defu resolves to 6.1.6, '* ]] || fail "a second defu version was not reported: ${drift:-nothing}"
node -e 'const fs = require("node:fs"); const [from, to] = process.argv.slice(1);
  const manifest = JSON.parse(fs.readFileSync(from, "utf8"));
  manifest.dependencies.ufo = "0.0.1";
  fs.writeFileSync(to, JSON.stringify(manifest));' "$ROOT/apps/web/package.json" "$WORK/declared.json"
drift=$(external_drift "$ROOT/pnpm-lock.yaml" "$WORK/declared.json" "$externals")
[[ $drift == *'ufo resolves to '*', apps/web declares 0.0.1'* ]] ||
  fail "a declared ufo version that differs from the lockfile was not reported: ${drift:-nothing}"
awk '/^snapshots:$/ { s = 1 } s && /^  [^ ]/ { n = ($0 ~ /^  nuxt@/) }
  n && /^      cookie-es: / { sub(/cookie-es: .*/, "cookie-es: 2.0.1") } { print }' \
  "$ROOT/pnpm-lock.yaml" >"$WORK/consumer.yaml"
drift=$(external_drift "$WORK/consumer.yaml" "$ROOT/apps/web/package.json" "$externals")
[[ $drift == 'nuxt '*' resolves cookie-es 2.0.1, apps/web declares '* ]] ||
  fail "nuxt resolving another cookie-es was not reported: ${drift:-nothing}"

grep -Fqx 'pmOnFail: error' "$ROOT/pnpm-workspace.yaml" ||
  fail 'pnpm-workspace.yaml must set pmOnFail: error'
grep -Fqx 'verifyDepsBeforeRun: error' "$ROOT/pnpm-workspace.yaml" ||
  fail 'pnpm-workspace.yaml must set verifyDepsBeforeRun: error'

if ((npm_installed)); then
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
else
  printf 'pnpm-policy-test: skipping the version-mismatch refusal: pnpm on PATH is the corepack shim, which switches versions itself; CI and images install pnpm with npm\n'
fi

project stale "{\"name\":\"stale\",\"private\":true,\"packageManager\":\"pnpm@$pin\",\"scripts\":{\"probe\":\"node -e 0\"}}" pin-unrecorded
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
