#!/usr/bin/env bash

# Regression checks for hosted workflow contracts.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORKFLOW=$ROOT/.github/workflows/ci.yml
RELEASE_WORKFLOW=$ROOT/.github/workflows/release-images.yml
IMAGE_CHECK=$ROOT/.github/actions/release-image-check/action.yml
SECURITY_WORKFLOW=$ROOT/.github/workflows/security-scan.yml
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-workflow-safety.XXXXXX")
WORK=$(cd "$WORK" && pwd -P)
trap 'rm -rf -- "$WORK"' EXIT

fail() {
  printf 'workflow-safety-test: %s\n' "$*" >&2
  exit 1
}

# Packaged, cached, and downloaded tools must pass pinned checksums.
node --input-type=module - "$ROOT" "$WORK" <<'NODE'
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { chmodSync, existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
const [root, work] = process.argv.slice(2);
const require = createRequire(join(root, "scripts/test/workflow-contract.test.mjs"));
const { parseDocument } = require("yaml");
const load = (path) => {
  const doc = parseDocument(readFileSync(join(root, path), "utf8"));
  assert.equal(doc.errors.length, 0, `${path}: valid YAML`);
  return doc.toJS();
};
const steps = (doc) => doc.runs?.steps ?? Object.values(doc.jobs ?? {}).flatMap((job) => job.steps ?? []);
const hosted = [
  ...readdirSync(join(root, ".github/workflows")).filter((file) => file.endsWith(".yml")).map((file) => `.github/workflows/${file}`),
  ...readdirSync(join(root, ".github/actions"), { recursive: true }).filter((file) => file.endsWith("/action.yml")).map((file) => `.github/actions/${file}`),
];
function checkRetries(command) {
  assert.match(command, /(?:^|\s)(?:-\w*f\w*|--fail)(?=\s|$)/, "tool curl fails on HTTP errors");
  for (const flag of ["--retry 5", "--retry-delay 5", "--retry-all-errors", "--retry-max-time 120"]) {
    assert.match(command, new RegExp(`(?:^|\\s)${flag.replaceAll(" ", "\\s+")}(?=\\s|$)`), `tool curl requires ${flag}`);
  }
}
for (const file of hosted) {
  for (const step of steps(load(file))) {
    for (const command of (step.run ?? "").replace(/\\\r?\n\s*/g, " ").split("\n")) {
      if (/\bcurl\b/.test(command) && /https:\/\/(?:github\.com\/[^/]+\/[^/]+\/releases\/download\/|api\.github\.com|ghcr\.io|(?:[^/]+\.)?pkg\.github\.com)/.test(command)) {
        checkRetries(command);
      }
    }
  }
}
const validCurl = "curl -fsSL --retry 5 --retry-delay 5 --retry-all-errors --retry-max-time 120 -o tool.zip https://github.com/owner/tool/releases/download/v1/tool.zip";
for (const flag of ["-fsSL", "--retry 5", "--retry-delay 5", "--retry-all-errors", "--retry-max-time 120"]) {
  assert.throws(() => checkRetries(validCurl.replace(flag, "")), /tool curl/);
}
assert.throws(() => checkRetries(validCurl.replace("--retry 5", "--retry 50")), /tool curl/);
const packageImage = "ghcr.io/dannyota/ci-tools@sha256:89b209b970088035112da16c3355b3cad0e930bee8e3cba0778f1788a4e881a6";
const packageArchives = [
  ["caddy_2.11.9_linux_amd64.tar.gz", "5b8cb7faf8726a2028ad96a66e54e6e81e8f361bc6175e5d22bbd997cc5177fb"],
  ["tofu_1.12.6_linux_amd64.zip", "5dc43da4f750f33873dc25e94587128709e819e544b7be9016b255316153c3a8"],
  ["providers/registry.opentofu.org/hashicorp/aws/terraform-provider-aws_6.64.0_linux_amd64.zip", "d7e9cc87ada1314e6d8ecc5849385a8f8f45757bd2c145b8657f015c65e5078d"],
  ["providers/registry.opentofu.org/cloudflare/cloudflare/terraform-provider-cloudflare_5.27.0_linux_amd64.zip", "ae47a9f29594d57e634cc0b5b96a18da5a364f43cbd735622ce5fe808728065f"],
  ["providers/registry.opentofu.org/vngcloud/vngcloud/terraform-provider-vngcloud_1.3.21_linux_amd64.zip", "d0f8ae399a3c7a8a6d8d2b569542e977426498270062f8d286a202923ffa8fc1"],
];
function checkPackage(action) {
  const step = action.runs.steps.find((s) => /podman login/.test(s.run ?? ""));
  assert.ok(step, "ci-tools: authenticated package pull");
  assert.equal(step.env.GHCR_USER, "${{ github.actor }}");
  assert.equal(step.env.GHCR_TOKEN, "${{ github.token }}");
  assert.ok(step.run.includes(`podman create --name ci-tools ${packageImage} /bin/true`), "ci-tools: create the digest-pinned scratch image without running it");
  assert.deepEqual(step.run.match(/ghcr\.io\/dannyota\/ci-tools[^\s]*/g), [packageImage], "ci-tools: no mutable image tag");
  assert.match(step.run, /podman login ghcr\.io --username "\$GHCR_USER" --password-stdin <<<"\$GHCR_TOKEN"/, "ci-tools: token goes to stdin");
  assert.ok(!/\b(?:echo|printf)\b[^\n]*GHCR_TOKEN/.test(step.run), "ci-tools: never print the token");
  for (const [path, digest] of packageArchives) {
    assert.ok(step.run.includes(`${digest}  ${path}`), `ci-tools: pin ${path}`);
  }
  return step;
}
const packageAction = load(".github/actions/ci-tools/action.yml");
const packageStep = checkPackage(packageAction);
for (const [from, to] of [
  [packageImage, "ghcr.io/dannyota/ci-tools:2026-11-08"],
  ["--password-stdin", "--password"],
  ...packageArchives.map(([, digest]) => [digest, "0".repeat(64)]),
]) {
  const changed = structuredClone(packageAction);
  changed.runs.steps[0].run = packageStep.run.replace(from, to);
  assert.throws(() => checkPackage(changed), undefined, "ci-tools: reject unpinned package or unsafe login");
}
const ciTools = "./.github/actions/ci-tools";
const toolInstaller = /^\.\/\.github\/actions\/install-(?:caddy|opentofu)$/;
const localActions = new Map(hosted.filter((file) => file.startsWith(".github/actions/")).map((file) => [
  `./${file.replace(/\/action\.yml$/, "")}`, load(file),
]));
function expandSteps(list, actions, parents = []) {
  return list.flatMap((step) => {
    if (step.uses === ciTools || toolInstaller.test(step.uses ?? "")) return [step];
    const action = actions.get(step.uses);
    if (action?.runs?.using !== "composite") return [step];
    assert.ok(!parents.includes(step.uses), `${step.uses}: composite action cycle`);
    return expandSteps(action.runs.steps, actions, [...parents, step.uses]);
  });
}
function checkPackageSteps(list, label) {
  const loaders = list.filter((step) => step.uses === ciTools);
  assert.ok(loaders.length <= 1, `${label}: load ci-tools only once`);
  const packageIndex = list.findIndex((step) => step.uses === ciTools);
  for (const [index, step] of list.entries()) {
    if (toolInstaller.test(step.uses ?? "")) {
      assert.ok(packageIndex >= 0 && packageIndex < index, `${label}: load ci-tools before ${step.uses}`);
    }
  }
  for (const loader of loaders) {
    assert.equal(loader["continue-on-error"], true, `${label}: allow verified cache or release fallback`);
  }
  return loaders.length > 0;
}
function checkPackagePermissions(doc, actions = localActions, label = "workflow") {
  for (const [id, job] of Object.entries(doc.jobs ?? {})) {
    const packaged = checkPackageSteps(expandSteps(job.steps ?? [], actions), `${label}:${id}`);
    if (!packaged && job.uses !== "./.github/workflows/vn-infra.yml") continue;
    assert.deepEqual(job.permissions, { contents: "read", packages: "read" }, `${id}: explicit read-only package permissions`);
  }
}
const loader = { uses: ciTools, "continue-on-error": true };
for (const tool of ["caddy", "opentofu"]) {
  const installer = { uses: `./.github/actions/install-${tool}` };
  checkPackageSteps([loader, installer], tool);
  for (const invalid of [
    [installer],
    [installer, loader],
    [loader, loader, installer],
    [{ uses: ciTools }, installer],
    [{ ...loader, "continue-on-error": false }, installer],
  ]) {
    assert.throws(() => checkPackageSteps(invalid, tool), /ci-tools|fallback/, `${tool}: reject missing, late, duplicate, or blocking loader`);
  }
  const wrapper = "./.github/actions/fixture-wrapper";
  const nested = "./.github/actions/fixture-nested";
  const fixtureActions = new Map([
    [wrapper, { runs: { using: "composite", steps: [{ uses: nested }] } }],
    [nested, { runs: { using: "composite", steps: [loader, installer] } }],
  ]);
  const fixture = { jobs: { proof: { permissions: { contents: "read", packages: "read" }, steps: [{ uses: wrapper }] } } };
  checkPackagePermissions(fixture, fixtureActions);
  for (const permission of ["contents", "packages"]) {
    const changed = structuredClone(fixture);
    delete changed.jobs.proof.permissions[permission];
    assert.throws(() => checkPackagePermissions(changed, fixtureActions), /package permissions/, `${tool}: nested caller requires ${permission}`);
  }
  const missing = structuredClone(fixtureActions);
  missing.get(nested).runs.steps.shift();
  assert.throws(() => checkPackagePermissions(fixture, missing), /load ci-tools before/, `${tool}: nested installer requires a preceding loader`);
  const duplicate = structuredClone(fixture);
  duplicate.jobs.proof.steps.unshift(loader);
  assert.throws(() => checkPackagePermissions(duplicate, fixtureActions), /only once/, `${tool}: reject loaders in both caller and composite`);
  const separate = { jobs: {
    load: { permissions: { contents: "read", packages: "read" }, steps: [loader] },
    install: { permissions: { contents: "read", packages: "read" }, steps: [installer] },
  } };
  assert.throws(() => checkPackagePermissions(separate), /load ci-tools before/, `${tool}: a loader in another job does not provide tools`);
}
for (const [name, action] of localActions) {
  if (action.runs?.using !== "composite") continue;
  checkPackageSteps(expandSteps(action.runs.steps, localActions), name);
}
for (const file of hosted.filter((path) => path.startsWith(".github/workflows/"))) {
  const doc = load(file);
  checkPackagePermissions(doc, localActions, file);
  for (const [id, job] of Object.entries(doc.jobs ?? {})) {
    if (!expandSteps(job.steps ?? [], localActions).some((s) => s.uses === ciTools) && job.uses !== "./.github/workflows/vn-infra.yml") continue;
    for (const permission of ["contents", "packages"]) {
      const changed = structuredClone(doc);
      delete changed.jobs[id].permissions[permission];
      assert.throws(() => checkPackagePermissions(changed), undefined, `${id}: reject missing ${permission} permission`);
    }
  }
}
const cachePin = "55cc8345863c7cc4c66a329aec7e433d2d1c52a9";
const tools = [
  { name: "caddy", version: "2.11.9", hash: "sha512", digest: "0e85544340290a16ce4bfb76fe7d65e9de1bf3a6e67f5ccfff30b30d27ea4c73e078077b7bf3e58ae60b6b12e44327705287059c775bca5782e8ee85ea7edd75", archive: "caddy.tar.gz", packaged: "caddy_2.11.9_linux_amd64.tar.gz", binary: "caddy", extract: "tar -xzf" },
  { name: "opentofu", version: "1.12.6", hash: "sha256", digest: "5dc43da4f750f33873dc25e94587128709e819e544b7be9016b255316153c3a8", archive: "tofu.zip", packaged: "tofu_1.12.6_linux_amd64.zip", binary: "tofu", extract: "unzip -q" },
];
function checkInstaller(action, tool) {
  const list = action.runs.steps;
  const restore = list.find((step) => step.uses === `actions/cache/restore@${cachePin}`);
  const save = list.find((step) => step.uses === `actions/cache/save@${cachePin}`);
  assert.ok(restore && save, `${tool.name}: separate cache restore and save`);
  assert.equal(restore.with.path, `\${{ runner.temp }}/${tool.archive}`);
  assert.ok(restore.with.key.includes(tool.version) && restore.with.key.includes(tool.digest), `${tool.name}: version and digest cache key`);
  assert.equal(restore.with["restore-keys"], undefined, `${tool.name}: exact archive cache only`);
  const download = list.find((step) => /\bcurl\b/.test(step.run ?? ""));
  assert.equal(download.if, undefined, `${tool.name}: select package before cache or download`);
  assert.equal(download.env.CACHE_HIT, `\${{ steps.${restore.id}.outputs.cache-hit }}`, `${tool.name}: preserve cache hit result`);
  checkRetries(download.run);
  const install = list.find((step) => (step.run ?? "").includes(tool.extract));
  assert.equal(install.if, undefined, `${tool.name}: verify hits and misses`);
  assert.ok(install.run.includes(tool.digest), `${tool.name}: pinned checksum`);
  assert.ok(install.run.indexOf(`${tool.hash}sum -c -`) < install.run.indexOf(tool.extract) && install.run.includes(`${tool.hash}sum -c -`), `${tool.name}: verify before extraction`);
  assert.equal(save.if, `\${{ github.ref == 'refs/heads/main' && steps.${restore.id}.outputs.cache-hit != 'true' }}`, `${tool.name}: only main saves misses`);
  assert.equal(save.with.path, restore.with.path);
  assert.equal(save.with.key, `\${{ steps.${restore.id}.outputs.cache-primary-key }}`);
  assert.ok(list.indexOf(restore) < list.indexOf(download) && list.indexOf(download) < list.indexOf(install) && list.indexOf(install) < list.indexOf(save), `${tool.name}: cache only verified archives`);
  return { download, install };
}
function run(command, args, env = {}) {
  return spawnSync(command, args, { encoding: "utf8", env: { ...process.env, ...env } });
}
const podmanBin = join(work, "podman-bin");
mkdirSync(podmanBin);
writeFileSync(join(podmanBin, "podman"), `#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  login)
    [ "$*" = 'login ghcr.io --username fixture --password-stdin' ]
    IFS= read -r token
    [ "$token" = "$CI_TOOLS_TEST_TOKEN" ]
    printf 'fixture auth' >"$REGISTRY_AUTH_FILE"
    ;;
  create) [ "$*" = 'create --name ci-tools ${packageImage} /bin/true' ] ;;
  cp)
    [ "$2" = 'ci-tools:/tools' ]
    [ "$CI_TOOLS_TEST_SCENARIO" != copy-failure ]
    cp -R "$CI_TOOLS_TEST_SOURCE" "$3"
    ;;
  rm) ;;
  *) exit 1 ;;
esac
`);
chmodSync(join(podmanBin, "podman"), 0o755);
for (const scenario of ["valid", "corrupt-archive", "corrupt-manifest", "copy-failure"]) {
  const temp = join(work, `package-${scenario}`);
  const source = join(work, `package-source-${scenario}`);
  mkdirSync(temp);
  mkdirSync(source);
  let command = packageStep.run;
  const manifest = [];
  for (const [path, pin] of packageArchives) {
    const file = join(source, path);
    mkdirSync(join(file, ".."), { recursive: true });
    const content = `verified ${path}\n`;
    writeFileSync(file, content);
    const hash = createHash("sha256").update(content).digest("hex");
    command = command.replace(pin, hash);
    manifest.push(`${hash}  ${path}`);
  }
  if (scenario === "corrupt-archive") {
    writeFileSync(join(source, packageArchives[0][0]), "changed archive");
    manifest[0] = `${createHash("sha256").update("changed archive").digest("hex")}  ${packageArchives[0][0]}`;
  }
  writeFileSync(join(source, "SHA256SUMS"), scenario === "corrupt-manifest" ? "invalid manifest\n" : `${manifest.join("\n")}\n`);
  const result = run("bash", ["-e", "-o", "pipefail", "-c", command], {
    PATH: `${podmanBin}:${process.env.PATH}`, RUNNER_TEMP: temp,
    GHCR_USER: "fixture", GHCR_TOKEN: "fixture-token", CI_TOOLS_TEST_TOKEN: "fixture-token",
    CI_TOOLS_TEST_SOURCE: source, CI_TOOLS_TEST_SCENARIO: scenario,
  });
  if (scenario === "valid") {
    assert.equal(result.status, 0, result.stderr);
    for (const [path] of packageArchives) assert.equal(readFileSync(join(temp, "ci-tools", path), "utf8"), `verified ${path}\n`);
  } else {
    assert.notEqual(result.status, 0, `ci-tools: reject ${scenario}`);
    assert.ok(!existsSync(join(temp, "ci-tools")), `ci-tools: ${scenario} never exposes unverified tools`);
  }
  assert.ok(!readdirSync(temp).some((name) => name.startsWith("ci-tools.")), "ci-tools: remove staging and registry credentials");
  assert.ok(!`${result.stdout}${result.stderr}`.includes("fixture-token"), "ci-tools: token stays out of logs");
}
for (const tool of tools) {
  const action = load(`.github/actions/install-${tool.name}/action.yml`);
  const { download, install } = checkInstaller(action, tool);
  for (const mutate of [
    (a) => { a.runs.steps.find((s) => s.uses?.includes("cache/save@")).if = "${{ true }}"; },
    (a) => { a.runs.steps.find((s) => s.run?.includes(tool.extract)).if = "${{ false }}"; },
    (a) => { const s = a.runs.steps.find((s) => s.run?.includes(tool.extract)); s.run = s.run.replace(`${tool.hash}sum -c -`, "true"); },
    (a) => { delete a.runs.steps.find((s) => s.run?.includes("curl")).env.CACHE_HIT; },
    (a) => { a.runs.steps.find((s) => s.uses?.includes("cache/restore@")).with.key = tool.version; },
  ]) {
    const changed = structuredClone(action);
    mutate(changed);
    assert.throws(() => checkInstaller(changed, tool), undefined, `${tool.name}: reject unsafe cache drift`);
  }
  const fixture = join(work, `${tool.name}-fixture`);
  mkdirSync(fixture);
  writeFileSync(join(fixture, tool.binary), "#!/bin/sh\nprintf 'verified-tool\\n'\n");
  chmodSync(join(fixture, tool.binary), 0o755);
  const archive = join(work, tool.archive);
  const packed = tool.name === "caddy"
    ? run("tar", ["-czf", archive, "-C", fixture, tool.binary])
    : run("python3", ["-c", "import sys,zipfile; z=zipfile.ZipFile(sys.argv[1],'w'); z.write(sys.argv[2], 'tofu'); z.close()", archive, join(fixture, tool.binary)]);
  assert.equal(packed.status, 0, packed.stderr);
  const fixtureHash = createHash(tool.hash).update(readFileSync(archive)).digest("hex");
  for (const corrupt of [false, true]) {
    const temp = join(work, `${tool.name}-${corrupt ? "corrupt" : "valid"}`);
    mkdirSync(temp);
    writeFileSync(join(temp, tool.archive), corrupt ? "corrupt archive" : readFileSync(archive));
    const pathFile = join(temp, "path");
    const result = run("bash", ["-e", "-o", "pipefail", "-c", install.run.replace(tool.digest, fixtureHash)], { RUNNER_TEMP: temp, GITHUB_PATH: pathFile });
    if (corrupt) {
      assert.notEqual(result.status, 0, `${tool.name}: corrupt cached archive fails`);
      assert.ok(!existsSync(pathFile), `${tool.name}: corrupt archive never reaches PATH`);
      assert.ok(!existsSync(join(temp, tool.name, tool.binary)), `${tool.name}: corrupt archive never extracts`);
    } else {
      assert.equal(result.status, 0, result.stderr);
      const binary = join(readFileSync(pathFile, "utf8").trim(), tool.binary);
      assert.equal(run(binary, []).stdout, "verified-tool\n", `${tool.name}: verified binary reaches PATH`);
    }
  }
  const fakeBin = join(fixture, "bin");
  mkdirSync(fakeBin);
  writeFileSync(join(fakeBin, "curl"), "#!/bin/sh\nexit 22\n");
  chmodSync(join(fakeBin, "curl"), 0o755);
  const selectEnv = { PATH: `${fakeBin}:${process.env.PATH}`, RUNNER_TEMP: fixture, CACHE_HIT: "false" };
  assert.notEqual(run("bash", ["-e", "-o", "pipefail", "-c", download.run], selectEnv).status, 0, `${tool.name}: exhausted download fails`);
  writeFileSync(join(fixture, tool.archive), "cached archive");
  assert.equal(run("bash", ["-e", "-o", "pipefail", "-c", download.run], { ...selectEnv, CACHE_HIT: "true" }).status, 0, `${tool.name}: cache hit avoids download`);
  assert.equal(readFileSync(join(fixture, tool.archive), "utf8"), "cached archive");
  mkdirSync(join(fixture, "ci-tools"));
  writeFileSync(join(fixture, "ci-tools", tool.packaged), readFileSync(archive));
  for (const cacheHit of ["true", "false"]) {
    writeFileSync(join(fixture, tool.archive), "cached archive");
    const result = run("bash", ["-e", "-o", "pipefail", "-c", download.run], { ...selectEnv, CACHE_HIT: cacheHit });
    assert.equal(result.status, 0, result.stderr);
    assert.deepEqual(readFileSync(join(fixture, tool.archive)), readFileSync(archive), `${tool.name}: package wins over cache and failed download`);
  }
}
for (const file of ["ci", "vn-infra"]) {
  const doc = load(`.github/workflows/${file}.yml`);
  const list = steps(doc);
  assert.equal(list.filter((step) => step.uses === "./.github/actions/install-opentofu").length, 1, `${file}: verified OpenTofu installer`);
  assert.ok(!list.some((step) => step.uses?.startsWith("opentofu/setup-opentofu@")), `${file}: no separate unprotected OpenTofu download`);
  const job = doc.jobs[file === "ci" ? "contracts" : "check"];
  const mirror = job.steps.find((s) => s.name === "Configure the provider mirror");
  assert.ok(mirror, `${file}: provider mirror configuration`);
  assert.ok(mirror.if.includes("steps.ci-tools.outcome == 'success'"), `${file}: mirror requires a verified package`);
  const packageIndex = job.steps.findIndex((s) => s.id === "ci-tools" && s.uses === "./.github/actions/ci-tools");
  assert.ok(packageIndex >= 0 && packageIndex < job.steps.indexOf(mirror), `${file}: verify package before mirror setup`);
  assert.ok(job.steps.indexOf(mirror) < job.steps.findIndex((s) => /\btofu\b[^\n]*\binit\b/.test(s.run ?? "")), `${file}: mirror before every init`);
  const temp = join(work, `mirror-${file}`);
  mkdirSync(temp);
  const envFile = join(temp, "env");
  const result = run("bash", ["-e", "-o", "pipefail", "-c", mirror.run], { RUNNER_TEMP: temp, GITHUB_ENV: envFile });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(readFileSync(envFile, "utf8"), `TF_CLI_CONFIG_FILE=${temp}/ci-tools.tfrc\n`);
  const config = readFileSync(join(temp, "ci-tools.tfrc"), "utf8");
  assert.match(config, /provider_installation\s*\{/);
  assert.ok(config.includes(`path = "${temp}/ci-tools/providers"`));
  assert.match(config, /filesystem_mirror\s*\{[^}]*include = \["registry\.opentofu\.org\/\*\/\*"\]/);
  assert.match(config, /direct\s*\{\s*exclude = \["registry\.opentofu\.org\/\*\/\*"\]/);
}
NODE

if grep -Fq 'changed=$(git diff --name-status' "$WORKFLOW"; then
  fail "a hosted append-only job still filters git diff before checking its status"
fi

assert_preflight_guards() { # job-body
  grep -Fq 'bash scripts/check-migrations-append-only.sh --commits "$base" HEAD' <<<"$1" ||
    fail "preflight does not use the tested migration commit-to-commit guard"
  grep -Fq 'bash scripts/check-released-schemas.sh --commits "$base" HEAD' <<<"$1" ||
    fail "preflight does not use the tested released-schema commit-to-commit guard"
  grep -Fq 'base="$BASE_SHA"' <<<"$1" ||
    fail "preflight does not preserve the event comparison base"
  grep -Fq 'base=$(bash scripts/check-migrations-append-only.sh --dispatch-base "$BASE_SHA" HEAD origin/main)' <<<"$1" ||
    fail "preflight does not use the validated dispatch comparison base"
}

assert_migration_guard() { # script-body
  grep -Fq 'BASELINE=.v1-baseline' <<<"$1" ||
    fail "migration guard does not use the application v1 marker"
  grep -Fq 'base=$(resolve_commit "$1") || fail "base is not a commit: $1"' <<<"$1" ||
    fail "migration guard does not reject an invalid base"
  grep -Fq 'git diff --quiet "$base" "$head"' <<<"$1" ||
    fail "migration guard does not compare push and dispatch trees directly"
  if grep -Eq 'git diff .*\.\.\.' <<<"$1"; then
    fail "migration guard uses a merge-base diff"
  fi
}

assert_released_schema_guard() { # script-body
  grep -Fq 'git cat-file -e "$base:.v1-baseline"' <<<"$1" ||
    fail "released-schema guard does not use the trusted v1 base marker"
  grep -Fq 'git diff --quiet "$base" "$head" -- .v1-baseline' <<<"$1" ||
    fail "released-schema guard does not freeze the v1 marker"
  local count
  count=$(grep -Fc 'if ! diff=$(git diff --name-status' <<<"$1" || true)
  [ "$count" -eq 1 ] ||
    fail "want one fail-closed released-schema diff capture, found $count"
  grep -Fq 'Could not compare released schemas with base.' <<<"$1" ||
    fail "released-schema guard lacks an explicit diff-failure result"
  grep -Fq 'git diff --name-status "$base" "$head"' <<<"$1" ||
    fail "released-schema guard does not compare push and dispatch trees directly"
  grep -Fq 'git rev-parse --verify --quiet "$2^{commit}"' <<<"$1" ||
    fail "released-schema guard does not validate the base commit"
  grep -Fq 'git rev-parse --verify --quiet "$3^{commit}"' <<<"$1" ||
    fail "released-schema guard does not validate the head commit"
  if grep -Eq 'git diff .*\.\.\.' <<<"$1"; then
    fail "released-schema guard uses a merge-base diff"
  fi
}

expect_guard_drift() { # validator body diagnostic
  local status=0
  ( "$1" "$2" ) >"$WORK/guard-drift" 2>&1 || status=$?
  [ "$status" -eq 1 ] || fail "guard drift did not fail: $3"
  grep -Fxq "workflow-safety-test: $3" "$WORK/guard-drift" ||
    fail "guard drift lacks the expected diagnostic: $3"
}

PREFLIGHT_JOB=$(awk '/^  preflight:$/{flag=1; next} /^  [a-z]/{flag=0} flag' "$WORKFLOW")
MIGRATION_GUARD=$(cat "$ROOT/scripts/check-migrations-append-only.sh")
SCHEMA_GUARD=$(cat "$ROOT/scripts/check-released-schemas.sh")
assert_preflight_guards "$PREFLIGHT_JOB"
assert_migration_guard "$MIGRATION_GUARD"
assert_released_schema_guard "$SCHEMA_GUARD"

while IFS='|' read -r pattern diagnostic; do
  expect_guard_drift assert_preflight_guards \
    "$(grep -Fv -- "$pattern" <<<"$PREFLIGHT_JOB")" "$diagnostic"
done <<'CASES'
check-migrations-append-only.sh --commits|preflight does not use the tested migration commit-to-commit guard
check-released-schemas.sh|preflight does not use the tested released-schema commit-to-commit guard
base="$BASE_SHA"|preflight does not preserve the event comparison base
--dispatch-base|preflight does not use the validated dispatch comparison base
CASES

while IFS='|' read -r pattern diagnostic; do
  expect_guard_drift assert_migration_guard \
    "$(grep -Fv -- "$pattern" <<<"$MIGRATION_GUARD")" "$diagnostic"
done <<'CASES'
BASELINE=.v1-baseline|migration guard does not use the application v1 marker
base=$(resolve_commit "$1")|migration guard does not reject an invalid base
git diff --quiet "$base" "$head"|migration guard does not compare push and dispatch trees directly
CASES
expect_guard_drift assert_migration_guard \
  "$MIGRATION_GUARD
git diff --name-status \"\$base\"...\"\$head\"" \
  'migration guard uses a merge-base diff'

while IFS='|' read -r pattern diagnostic; do
  expect_guard_drift assert_released_schema_guard \
    "$(grep -Fv -- "$pattern" <<<"$SCHEMA_GUARD")" "$diagnostic"
done <<'CASES'
git cat-file -e "$base:.v1-baseline"|released-schema guard does not use the trusted v1 base marker
git diff --quiet "$base" "$head" -- .v1-baseline|released-schema guard does not freeze the v1 marker
if ! diff=$(git diff --name-status|want one fail-closed released-schema diff capture, found 0
Could not compare released schemas with base.|released-schema guard lacks an explicit diff-failure result
git rev-parse --verify --quiet "$2^{commit}"|released-schema guard does not validate the base commit
git rev-parse --verify --quiet "$3^{commit}"|released-schema guard does not validate the head commit
CASES
expect_guard_drift assert_released_schema_guard \
  "$(sed 's/"$base" "$head"/"$base"/' <<<"$SCHEMA_GUARD")" \
  'released-schema guard does not freeze the v1 marker'
expect_guard_drift assert_released_schema_guard \
  "$SCHEMA_GUARD
git diff --name-status \"\$base\"...\"\$head\"" \
  'released-schema guard uses a merge-base diff'

grep -Fq 'runs-on: ubuntu-24.04' "$WORKFLOW" ||
  fail "hosted S3 conformance does not pin a runner with Podman"
grep -Fq 'run: make test-media-s3-up' "$WORKFLOW" ||
  fail "hosted S3 conformance does not start the pinned test service"
grep -Fq 'make server-test-media-s3' "$WORKFLOW" ||
  fail "hosted media conformance does not run the media storage suite"
grep -Fq 'SERVER_TEST_S3_RUN: ^TestNormalizeAcceptsFrozenCorpusDeterministically$' "$WORKFLOW" ||
  fail "hosted S3 conformance does not isolate the slow corpus test"
grep -Fq 'SERVER_TEST_S3_SKIP: ^(TestNormalizationBudget|TestNormalizeAcceptsFrozenCorpusDeterministically)$' "$WORKFLOW" ||
  fail "hosted S3 conformance does not run the rest of the suite with both split-off tests skipped"
grep -Fq 'run: make test-media-s3-down' "$WORKFLOW" ||
  fail "hosted S3 conformance does not tear down its disposable service"

WEB_E2E_JOB=$(awk '/^  web-e2e:$/{flag=1; next} /^  [a-z]/{flag=0} flag' "$WORKFLOW")
[ -n "$WEB_E2E_JOB" ] || fail "hosted workflow lacks the pinned browser job"
assert_browser_dependencies() { # job-body
  local needs
  needs=$(awk '
    /^    needs:/ { count++; active = 1; sub(/^    needs:[ ]*/, ""); print; next }
    active && /^      - / { sub(/^      - /, ""); print; next }
    /^    [^ ]/ { active = 0 }
    END { if (count > 1) print "duplicate" }
  ' <<<"$1" | tr -d '[:space:][]')
  [ -z "$needs" ] || [ "$needs" = preflight ] ||
    fail "pinned browser job may need only preflight"
  if grep -Eq 'actions/download-artifact@|gh run download' <<<"$1"; then
    fail "pinned browser job depends on another job's artifacts"
  fi
}
assert_browser_dependencies "$WEB_E2E_JOB"
assert_browser_dependencies "$(sed '/^    needs:/d' <<<"$WEB_E2E_JOB")"
assert_browser_dependencies "$(sed 's/needs: \[preflight\]/needs: preflight/' <<<"$WEB_E2E_JOB")"
assert_browser_dependencies "$(sed 's/needs: \[preflight\]/needs:\n      - preflight/' <<<"$WEB_E2E_JOB")"
expect_guard_drift assert_browser_dependencies \
  "$(sed 's/needs: \[preflight\]/needs: [preflight, web]/' <<<"$WEB_E2E_JOB")" \
  'pinned browser job may need only preflight'
expect_guard_drift assert_browser_dependencies \
  "$WEB_E2E_JOB
      - uses: actions/download-artifact@$(printf '%040d' 0)" \
  "pinned browser job depends on another job's artifacts"
expect_guard_drift assert_browser_dependencies \
  "$WEB_E2E_JOB
      - run: gh run download" \
  "pinned browser job depends on another job's artifacts"
grep -Fq '        WEB_E2E_RUN_ID: ci-${{ github.run_id }}-${{ github.run_attempt }}' \
  "$WORKFLOW" || fail "pinned browser job lacks the closed immutable run ID"
grep -Fq '        test -z "${UPDATE_GOLDEN+x}"' "$WORKFLOW" ||
  fail "pinned browser job does not reject UPDATE_GOLDEN by presence"
grep -Fq '        test -z "${PLAYWRIGHT_UPDATE_SNAPSHOTS+x}"' "$WORKFLOW" ||
  fail "pinned browser job does not reject PLAYWRIGHT_UPDATE_SNAPSHOTS by presence"
grep -Fq '        make web-e2e' "$WORKFLOW" ||
  fail "pinned browser job does not run the comparison target"
if grep -Fq 'make web-e2e-update' "$WORKFLOW"; then
  fail "hosted workflow can update browser baselines"
fi
if grep -Fq -- '--update-snapshots' "$WORKFLOW"; then
  fail "hosted workflow passes a browser baseline update flag"
fi

# The matrix runs exactly the Makefile's shards, which together cover every
# spec once (scripts/test/makefile-safety-test.sh checks the partition).
assert_web_e2e_shards() { # job-body makefile-recipe
  local matrix make_shards
  grep -Fxq '        shard: [preview-gap, rest-1, rest-2, rest-3]' <<<"$1" ||
    fail "pinned browser job does not run every shard that together covers every spec"
  matrix=$(sed -n 's/^        shard: \[\(.*\)\]$/\1/p' <<<"$1")
  make_shards=$(awk -F '[)] harness_specs=' '/^\t[a-z0-9-]+\) harness_specs=/ {
      sub(/^\t/, "", $1); if ($1 != "all") print $1 }' <<<"$2" | paste -sd, - | sed 's/,/, /g')
  [ -n "$make_shards" ] && [ "$matrix" = "$make_shards" ] ||
    fail "pinned browser matrix does not list exactly the Makefile web-e2e shards"
}
WEB_E2E_RECIPE=$(awk '/^web-e2e:/{flag=1; next} /^[^\t]/{flag=0} flag' "$ROOT/Makefile")
assert_web_e2e_job_shards() { assert_web_e2e_shards "$1" "$WEB_E2E_RECIPE"; }
assert_web_e2e_recipe_shards() { assert_web_e2e_shards "$WEB_E2E_JOB" "$1"; }
assert_web_e2e_shards "$WEB_E2E_JOB" "$WEB_E2E_RECIPE"
expect_guard_drift assert_web_e2e_job_shards "$(sed 's/, rest-3\]/]/' <<<"$WEB_E2E_JOB")" \
  'pinned browser job does not run every shard that together covers every spec'
expect_guard_drift assert_web_e2e_recipe_shards \
  "$(grep -v 'rest-3) harness_specs=' <<<"$WEB_E2E_RECIPE")" \
  'pinned browser matrix does not list exactly the Makefile web-e2e shards'
expect_guard_drift assert_web_e2e_recipe_shards \
  "$WEB_E2E_RECIPE
$(printf '\t%s' "rest-4) harness_specs=''; normal_specs='' ;; \\")" \
  'pinned browser matrix does not list exactly the Makefile web-e2e shards'
grep -Fq '          WEB_E2E_SHARD: ${{ matrix.shard }}' <<<"$WEB_E2E_JOB" ||
  fail "pinned browser job does not pass its shard to make web-e2e"

PROOF_JOB=$(awk '/^  dev-https-proofs:$/{flag=1; next} /^  [a-z]/{flag=0} flag' "$WORKFLOW")
[ -n "$PROOF_JOB" ] || fail "hosted workflow lacks the dev-https-proofs job"
PROOF_SETUP=$ROOT/.github/actions/browser-proof-setup/action.yml
[ -f "$PROOF_SETUP" ] || fail "the browser-proof-setup action is missing"
PROOF_RUNNER=$ROOT/scripts/ci-browser-proofs.sh
grep -Fq '    runs-on: ubuntu-24.04' <<<"$PROOF_JOB" ||
  fail "dev-https-proofs job does not pin a runner with Podman"
grep -Fq '    timeout-minutes: 40' <<<"$PROOF_JOB" ||
  fail "dev-https-proofs job lacks a fixed 40-minute timeout"
! grep -Fq 'continue-on-error' <<<"$PROOF_JOB" ||
  fail "dev-https-proofs job must block the release"
grep -Fq 'uses: ./.github/actions/browser-proof-setup' <<<"$PROOF_JOB" ||
  fail "dev-https-proofs job does not use the shared proof setup"
grep -Fxq '      run: make test-db-up' "$PROOF_SETUP" ||
  fail "proof setup does not start the shared database container"
grep -Fxq '      run: make dev-https' "$PROOF_SETUP" ||
  fail "proof setup does not start the repository HTTPS harness"
grep -Fxq '      run: make dev-https-browser-image' "$PROOF_SETUP" ||
  fail "proof setup does not build the pinned browser image"
grep -Fq 'bash scripts/ci-browser-proofs.sh "${proofs[@]}"' <<<"$PROOF_JOB" ||
  fail "dev-https-proofs job does not run its group through the proof runner"
grep -Fq 'ABOUTME_TOTP_SHARD=$shard make "$target"' "$PROOF_RUNNER" ||
  fail "proof runner does not run a TOTP shard through its make target"
grep -Fq 'ABOUTME_PASSKEY_SHARD=$shard make "$target"' "$PROOF_RUNNER" ||
  fail "proof runner does not run a passkey shard through its make target"
if grep -Fq 'secrets.' <<<"$PROOF_JOB" || grep -Fq 'secrets.' "$PROOF_SETUP"; then
  fail "dev-https-proofs job references a repository secret"
fi
if grep -Fq '    services:' <<<"$PROOF_JOB"; then
  fail "dev-https-proofs job must not add a second database service beside dev-https"
fi
if grep -Eq 'ABOUTME_RELEASE_APP_IMAGE|ABOUTME_RELEASE_WEB_IMAGE|owner-test\.env' \
  <<<"$PROOF_JOB"; then
  fail "dev-https-proofs job leaks a release image or owner credential input"
fi

# Every proof runs exactly once: each dev-https check below, and every shard
# of the two sharded second-factor journeys, read from their shard maps.
shard_proofs=$(
  cd "$ROOT/deploy/dev-https-browser" &&
    node --input-type=module -e "
      import { PASSKEY_SHARD_ROLES, TOTP_SHARD_ROLES } from './proof-shards.mjs';
      for (const shard of Object.keys(TOTP_SHARD_ROLES)) console.log('totp:' + shard);
      for (const shard of Object.keys(PASSKEY_SHARD_ROLES)) console.log('passkey:' + shard);
    "
) || fail "cannot read the proof shard maps"
[ -n "$shard_proofs" ] || fail "the proof shard maps are empty"
expected_proofs=$(
  printf '%s\n' auth editor entry exports linkedin linkedin-import mcp mcp-sdk \
    mcp-ts-sdk password privacy public publish sample-start transport \
    "$shard_proofs" | LC_ALL=C sort
)
# A TOTP proof may list several shards, comma-separated; each counts once.
actual_proofs=$(awk '$1 == "proofs:" { for (i = 2; i <= NF; i++) {
    if ($i ~ /^totp:/) { n = split(substr($i, 6), s, ","); for (j = 1; j <= n; j++) print "totp:" s[j] }
    else print $i } }' <<<"$PROOF_JOB" | LC_ALL=C sort)
[ "$actual_proofs" = "$expected_proofs" ] ||
  fail "dev-https-proofs groups do not run every proof exactly once: $(tr '\n' ' ' <<<"$actual_proofs")"

# Each cleanup step's own if: always() is asserted against its own named
# block, not a job-wide count, so a check cannot pass by attaching every
# always() to one step and none to the others.
step_block() { # step-name job-body
  awk -v name="      - name: $1" '
    $0 == name { found = 1; print; next }
    found && /^      - / { exit }
    found { print }
  ' <<<"$2"
}

UPLOAD_STEP=$(step_block "Upload shard proof evidence" "$PROOF_JOB")
[ -n "$UPLOAD_STEP" ] || fail "dev-https-proofs job lacks the named upload step"
grep -Fq 'if: always()' <<<"$UPLOAD_STEP" ||
  fail "dev-https-proofs job's upload step does not run on every exit"
grep -Fq 'path: .dev/proof-evidence/' <<<"$UPLOAD_STEP" ||
  fail "dev-https-proofs job does not upload the bounded shard evidence path"
grep -Fq 'include-hidden-files: true' <<<"$UPLOAD_STEP" ||
  fail "dev-https-proofs job's upload step excludes the hidden .dev evidence path by default"
grep -Fq 'staging=.dev/proof-evidence' "$PROOF_RUNNER" ||
  fail "proof runner does not stage shard evidence where the job uploads it"

STOP_HARNESS_STEP=$(step_block "Stop the HTTPS harness" "$PROOF_JOB")
[ -n "$STOP_HARNESS_STEP" ] || fail "dev-https-proofs job lacks the named stop-harness step"
grep -Fq 'if: always()' <<<"$STOP_HARNESS_STEP" ||
  fail "dev-https-proofs job's stop-harness step does not run on every exit"
grep -Fq -- 'run: make dev-https-down' <<<"$STOP_HARNESS_STEP" ||
  fail "dev-https-proofs job's stop-harness step does not stop the HTTPS harness"

STOP_DB_STEP=$(step_block "Stop the runner-local database" "$PROOF_JOB")
[ -n "$STOP_DB_STEP" ] || fail "dev-https-proofs job lacks the named stop-database step"
grep -Fq 'if: always()' <<<"$STOP_DB_STEP" ||
  fail "dev-https-proofs job's stop-database step does not run on every exit"
grep -Fq -- 'run: make test-db-down' <<<"$STOP_DB_STEP" ||
  fail "dev-https-proofs job's stop-database step does not stop the runner-local database"

# The gate checks that the shards' evidence together proves every step.
CI_JOB=$(awk '/^  ci:$/{flag=1; next} /^  [a-z]/{flag=0} flag' "$WORKFLOW")
[ -n "$CI_JOB" ] || fail "hosted workflow lacks the ci gate job"
grep -Fq 'pattern: shard-evidence-*' <<<"$CI_JOB" ||
  fail "ci gate does not download every group's shard evidence"
grep -Fq 'node deploy/dev-https-browser/check-shard-coverage.mjs totp \' <<<"$CI_JOB" ||
  fail "ci gate does not check TOTP shard coverage"
grep -Fq 'node deploy/dev-https-browser/check-shard-coverage.mjs passkey \' <<<"$CI_JOB" ||
  fail "ci gate does not check passkey shard coverage"
grep -Fq '      - dev-https-proofs' <<<"$CI_JOB" ||
  fail "ci gate does not require the dev-https-proofs job"

# The gate permits only named prose omissions on pull requests.
assert_skip_permissions() { # aggregate-job-body
  grep -Fq '$selection == "prose" and $event == "pull_request"' <<<"$1" &&
    grep -Fq '(.key | IN("macos-tooling", "vn-infra", "server", "server-integration", "server-resumeapi", "web", "web-e2e", "dev-https-proofs"))' <<<"$1" ||
    fail "ci gate does not enforce named pull request prose skips"
  [ "$(grep -Fc '.value.result == "skipped"' <<<"$1")" -eq 1 ] ||
    fail "ci gate accepts a skipped result in more than one place"
}
assert_skip_permissions "$CI_JOB"
for pattern in '$selection == "prose" and $event == "pull_request"' '(.key | IN("macos-tooling", "vn-infra", "server", "server-integration", "server-resumeapi", "web", "web-e2e", "dev-https-proofs"))'; do
  expect_guard_drift assert_skip_permissions "$(grep -Fv "$pattern" <<<"$CI_JOB")" \
    'ci gate does not enforce named pull request prose skips'
done

assert_vn_selection() { # caller-job-body
  grep -Fxq "    if: github.event_name != 'pull_request' || needs.preflight.outputs.selection != 'prose'" <<<"$1" ||
    fail "VN caller does not use the exact selection condition"
  grep -Fxq '    needs: [preflight]' <<<"$1" ||
    fail "VN caller does not require preflight"
}
VN_JOB=$(awk '/^  vn-infra:$/{flag=1; next} /^  [a-z]/{flag=0} flag' "$WORKFLOW")
assert_vn_selection "$VN_JOB"
expect_guard_drift assert_vn_selection "$(sed '/^    if:/d' <<<"$VN_JOB")" \
  'VN caller does not use the exact selection condition'
expect_guard_drift assert_vn_selection "$(sed '/^    needs:/d' <<<"$VN_JOB")" \
  'VN caller does not require preflight'

# Only the trusted renewal workflow may also receive its environment credentials.
assert_workflow_secrets() { # workflow-directory
  node --input-type=module - "$1" "$ROOT" <<'NODE'
import { readFileSync, readdirSync } from "node:fs";
import { createHash } from "node:crypto";
import { createRequire } from "node:module";
import { join } from "node:path";
const require = createRequire(join(process.argv[3], "scripts/test/workflow-contract.test.mjs"));
const { parseDocument } = require("yaml");
const directory = process.argv[2];
const yamlPath = (parts) => parts.reduce((path, part) =>
  typeof part === "number" ? `${path}[${part}]` :
    /^[\w-]+$/.test(part) ? `${path}${path ? "." : ""}${part}` :
      `${path}[${JSON.stringify(part)}]`, "");
function inspect(value, parts, file, trusted, isKey) {
  if (typeof value !== "string") return;
  for (const expression of value.matchAll(/\$\{\{((?:'(?:[^']|'')*'|[^'])+?)\}\}/g)) {
    const tokens = expression[1].match(/'(?:[^']|'')*'|[A-Za-z_][\w-]*|[^\s]/g) || [];
    for (const [index, token] of tokens.entries()) {
      if (token.toLowerCase() !== "secrets" || tokens[index - 1] === ".") continue;
      const property = tokens[index + 1] === "." ? tokens[index + 2] : undefined;
      if (property === "GITHUB_TOKEN") continue;
      const allowed = ["VNGCLOUD_CLIENT_ID", "VNGCLOUD_CLIENT_SECRET", "VNGCLOUD_PROJECT_ID"].includes(property) &&
        trusted && !isKey && parts.length === 6 && parts[0] === "jobs" && parts[1] === "renew" &&
        parts[2] === "steps" && parts[3] === 4 && parts[4] === "env" && parts[5] === property &&
        value === "${{ secrets." + property + " }}";
      if (allowed) continue;
      console.error(`workflow-safety-test: ${file}:${yamlPath(parts)} references an unapproved secret`);
      process.exit(1);
    }
  }
}
function walk(value, parts, file, trusted, ancestors = new Set(), isKey = false) {
  inspect(value, parts, file, trusted, isKey);
  if (value === null || typeof value !== "object") return;
  if (ancestors.has(value)) throw new Error(`${file}:${yamlPath(parts)} contains a recursive YAML alias`);
  ancestors.add(value);
  if (Array.isArray(value)) {
    value.forEach((child, index) => walk(child, [...parts, index], file, trusted, ancestors));
  } else if (value instanceof Map) {
    for (const [key, child] of value) {
      const childPath = [...parts, String(key)];
      walk(key, childPath, file, trusted, ancestors, true);
      walk(child, childPath, file, trusted, ancestors);
    }
  }
  ancestors.delete(value);
}
for (const file of readdirSync(directory, { recursive: true, withFileTypes: true })) {
  if (!file.isFile() || !/\.ya?ml$/.test(file.name)) continue;
  const path = join(file.parentPath, file.name);
  const source = readFileSync(path, "utf8");
  const document = parseDocument(source);
  if (document.errors.length) throw new Error(`${path}: ${document.errors.join("; ")}`);
  // Bind the exception to the whole workflow contract, including its execution steps.
  const trusted = path === join(directory, "vmonitor-renew.yml") &&
    createHash("sha256").update(source).digest("hex") === "c60de2da688b5ce085b29ccdb39ae263bad5b18ba50cf714ea63a526bedeb7d0";
  walk(document.toJS({ mapAsMap: true }), [], path, trusted);
}
NODE
}
assert_workflow_secrets "$ROOT/.github/workflows"

PROBE_WORKFLOW=$ROOT/.github/workflows/approval-probe.yml
PROBE_PREPARE=$(awk '/^  prepare-request:$/{flag=1; next} /^  [a-z]/{flag=0} flag' "$PROBE_WORKFLOW")
PROBE_APPROVAL=$(awk '/^  record-approval:$/{flag=1; next} /^  [a-z]/{flag=0} flag' "$PROBE_WORKFLOW")
grep -Fxq 'permissions: {}' "$PROBE_WORKFLOW" || fail "approval probe needs empty default permissions"
grep -Fxq '    permissions: {}' <<<"$PROBE_PREPARE" || fail "approval probe preparation needs no authority"
grep -Fxq '    permissions: {}' <<<"$PROBE_APPROVAL" || fail "approval probe approval needs no authority"
! grep -Fq 'environment:' <<<"$PROBE_PREPARE" || fail "approval probe preparation must precede the gate"
grep -Fxq '    environment: approval-probe' <<<"$PROBE_APPROVAL" || fail "approval probe needs its environment gate"
grep -Fxq '    needs: [prepare-request]' <<<"$PROBE_APPROVAL" || fail "approval probe must need preparation"
! grep -Eq 'secrets|uses:|checkout|curl|gh api' "$PROBE_WORKFLOW" || fail "approval probe must only echo fixed text"

mkdir -p "$WORK/workflows"
cp "$WORKFLOW" "$WORK/workflows/ci.yml"
cat >"$WORK/workflows/ordinary.yml" <<'YAML'
# secrets may appear in comments.
jobs:
  unrelated:
    steps:
      - name: secrets
        env:
          LITERAL: 'secrets'
          TOKEN: ${{ secrets.GITHUB_TOKEN }}
          PROPERTY: ${{ foo.secrets }}
          IDENTIFIER: ${{ other_secrets }}
          QUOTED: ${{ format('secrets and }}', 'secrets') }}
YAML
assert_workflow_secrets "$WORK/workflows"
while IFS= read -r expression; do
  printf 'jobs:\n  unrelated:\n    env:\n      ALL_SECRETS: %s\n' "$expression" \
    >"$WORK/workflows/unapproved.yml"
  expect_guard_drift assert_workflow_secrets "$WORK/workflows" \
    "$WORK/workflows/unapproved.yml:jobs.unrelated.env.ALL_SECRETS references an unapproved secret"
done <<'CASES'
${{ toJSON(secrets) }}
${{ secrets }}
${{ format('{0}', secrets) }}
${{ secrets['X'] }}
${{ secrets.OTHER }}
"${{ toJSON(\u0073ecrets) }}"
CASES
cat >"$WORK/workflows/unapproved.yml" <<'YAML'
jobs:
  unrelated:
    steps:
      - env:
          "${{ secrets.OTHER }}": value
YAML
expect_guard_drift assert_workflow_secrets "$WORK/workflows" \
  "$WORK/workflows/unapproved.yml:jobs.unrelated.steps[0].env[\"\${{ secrets.OTHER }}\"] references an unapproved secret"

rm "$WORK/workflows/unapproved.yml"
cp "$ROOT/.github/workflows/vmonitor-renew.yml" "$WORK/workflows/vmonitor-renew.yml"
assert_workflow_secrets "$WORK/workflows"
for drift in \
  's/^  renew:$/  other:/' \
  's/id: renew/id: other/' \
  's/environment: vmonitor-renew/environment: other/' \
  's/VNGCLOUD_CLIENT_ID: /OTHER: /' \
  's/secrets.VNGCLOUD_CLIENT_ID/secrets["VNGCLOUD_CLIENT_ID"]/' \
  's/VNGCLOUD_PROJECT_ID: /OTHER: /' \
  's/secrets.VNGCLOUD_PROJECT_ID/vars.VNGCLOUD_PROJECT_ID/' \
  's/secrets.VNGCLOUD_PROJECT_ID/secrets.OTHER/' \
  's/secrets.VNGCLOUD_PROJECT_ID/secrets["VNGCLOUD_PROJECT_ID"]/' \
  's/persist-credentials: false/persist-credentials: true/' \
  's/run: bash scripts\/vmonitor-renew.sh "\$ACTION"/run: echo "\${{ secrets.VNGCLOUD_PROJECT_ID }}"/' \
  's/run: bash scripts\/vmonitor-renew.sh "\$ACTION"/run: echo "\${{ secrets.VNGCLOUD_CLIENT_ID }}"/'; do
  sed "$drift" "$ROOT/.github/workflows/vmonitor-renew.yml" >"$WORK/workflows/vmonitor-renew.yml"
  status=0
  ( assert_workflow_secrets "$WORK/workflows" ) >"$WORK/renewal-secret-drift" 2>&1 || status=$?
  [ "$status" -eq 1 ] || fail "renewal secret drift did not fail: $drift"
  grep -Fq 'references an unapproved secret' "$WORK/renewal-secret-drift" ||
    fail "renewal secret drift lacks the expected diagnostic: $drift"
done
cp "$ROOT/.github/workflows/vmonitor-renew.yml" "$WORK/workflows/other.yml"
rm "$WORK/workflows/vmonitor-renew.yml"
status=0
( assert_workflow_secrets "$WORK/workflows" ) >"$WORK/renewal-secret-drift" 2>&1 || status=$?
[ "$status" -eq 1 ] || fail 'renewal credentials in another file did not fail'
rm "$WORK/workflows/other.yml"
for credential in VNGCLOUD_CLIENT_ID VNGCLOUD_CLIENT_SECRET VNGCLOUD_PROJECT_ID; do
  for fixture in job step key run aggregate; do
    case "$fixture" in
      job) printf 'jobs:\n  renew:\n    env:\n      %s: ${{ secrets.%s }}\n' "$credential" "$credential" ;;
      step) printf 'jobs:\n  renew:\n    steps:\n      - env:\n          %s: ${{ secrets.%s }}\n' "$credential" "$credential" ;;
      key) printf 'jobs:\n  renew:\n    steps:\n      - env:\n          "${{ secrets.%s }}": value\n' "$credential" ;;
      run) printf 'jobs:\n  renew:\n    steps:\n      - run: echo "${{ secrets.%s }}"\n' "$credential" ;;
      aggregate) printf 'jobs:\n  renew:\n    env:\n      ALL: ${{ toJSON(secrets) }}\n' ;;
    esac >"$WORK/workflows/vmonitor-renew.yml"
    status=0
    ( assert_workflow_secrets "$WORK/workflows" ) >"$WORK/renewal-secret-drift" 2>&1 || status=$?
    [ "$status" -eq 1 ] || fail "renewal credential fixture did not fail: $credential $fixture"
    grep -Fq 'references an unapproved secret' "$WORK/renewal-secret-drift" ||
      fail "renewal credential fixture lacks the expected diagnostic: $credential $fixture"
  done
done
rm "$WORK/workflows/vmonitor-renew.yml"

# Each observer platform builds in its own native job. The plain version tag
# stays arm64 for Lambda, while amd64 uses a platform suffix. Both jobs smoke,
# scan, attest provenance, attest an SBOM, and upload release evidence.
release_job_body() { # job-id
  awk -v job="  $1:" '$0 == job { flag = 1; next } /^  [a-z]/ { flag = 0 } flag' "$RELEASE_WORKFLOW"
}

assert_observer_build() { # action-body
  grep -Fq 'observer*)' <<<"$1" &&
    grep -Fq 'docker build --platform "linux/$ARCH" --provenance=false' <<<"$1" &&
    grep -Fq -- '--build-arg VERSION="$VERSION" --build-arg COMMIT="$COMMIT"' <<<"$1" ||
    fail "shared observer build must keep a single-platform manifest and build identity"
}
IMAGE_CHECK_BODY=$(cat "$IMAGE_CHECK")
assert_observer_build "$IMAGE_CHECK_BODY"
for pattern in 'observer*)' '--provenance=false' '--build-arg VERSION='; do
  expect_guard_drift assert_observer_build \
    "$(grep -Fv -- "$pattern" <<<"$IMAGE_CHECK_BODY")" \
    'shared observer build must keep a single-platform manifest and build identity'
done

OBSERVER_ARM_JOB=$(release_job_body observer-image)
[ -n "$OBSERVER_ARM_JOB" ] || fail "release-images workflow lacks the arm64 observer job"
grep -Fxq '    runs-on: ubuntu-24.04-arm' <<<"$OBSERVER_ARM_JOB" ||
  fail "arm64 observer image does not use the native arm64 runner"
grep -Fq "IMAGE: ghcr.io/dannyota/aboutme-observer:\${{ github.ref_name }}" <<<"$OBSERVER_ARM_JOB" ||
  fail "arm64 observer image does not keep the plain version tag"
grep -Fxq '      ARCH: arm64' <<<"$OBSERVER_ARM_JOB" &&
  grep -Fq 'uses: ./.github/actions/release-image-check' <<<"$OBSERVER_ARM_JOB" ||
  fail "arm64 observer image is not a single-platform arm64 manifest"

OBSERVER_AMD_JOB=$(release_job_body observer-image-amd64)
[ -n "$OBSERVER_AMD_JOB" ] || fail "release-images workflow lacks the amd64 observer job"
grep -Fxq '    runs-on: ubuntu-24.04' <<<"$OBSERVER_AMD_JOB" ||
  fail "amd64 observer image does not use the native amd64 runner"
grep -Fq "IMAGE: ghcr.io/dannyota/aboutme-observer:\${{ github.ref_name }}-amd64" <<<"$OBSERVER_AMD_JOB" ||
  fail "amd64 observer image does not use the platform-suffixed version tag"
grep -Fxq '      ARCH: amd64' <<<"$OBSERVER_AMD_JOB" &&
  grep -Fq 'uses: ./.github/actions/release-image-check' <<<"$OBSERVER_AMD_JOB" ||
  fail "amd64 observer image is not a single-platform amd64 manifest"
grep -Fq "[ \"\$(docker inspect --format '{{.Os}}/{{.Architecture}}' \"\$IMAGE\")\" = \"linux/\$ARCH\" ]" "$IMAGE_CHECK" ||
  fail "shared image smoke does not check the built architecture"
grep -Fq 'docker run --rm "$IMAGE" version' "$IMAGE_CHECK" ||
  fail "amd64 observer smoke does not start the observer binary"
grep -Fq 'scripts/security-scan.sh scan-image' "$IMAGE_CHECK" ||
  fail "amd64 observer image lacks its Trivy scan"
grep -Fq 'uses: actions/attest-build-provenance@' <<<"$OBSERVER_AMD_JOB" ||
  fail "amd64 observer image lacks provenance attestation"
grep -Fq 'uses: actions/attest@' <<<"$OBSERVER_AMD_JOB" ||
  fail "amd64 observer image lacks SBOM attestation"
grep -Fq 'name: digest-observer-amd64' <<<"$OBSERVER_AMD_JOB" ||
  fail "amd64 observer job does not upload its digest"
grep -Fq 'name: sbom-observer-amd64' <<<"$OBSERVER_AMD_JOB" ||
  fail "amd64 observer job does not upload its SBOM"

# The release job waits for both observer jobs to collect their evidence, but
# only successful app images gate publication. A failed observer scan must not
# block the server, web, and caddy release.
RELEASE_JOB=$(release_job_body release)
[ -n "$RELEASE_JOB" ] || fail "release-images workflow lacks the release job"
grep -Fxq '    needs: [image, observer-image, observer-image-amd64]' <<<"$RELEASE_JOB" ||
  fail "release job must wait on both observer jobs to pick up their digests"
grep -Fxq "    if: \${{ !cancelled() && needs.image.result == 'success' }}" <<<"$RELEASE_JOB" ||
  fail "release job condition lets an observer failure block the app release"
grep -Fq "OBSERVER_ARM_RESULT: \${{ needs['observer-image'].result }}" <<<"$RELEASE_JOB" ||
  fail "release job does not read the arm64 observer result"
grep -Fq "OBSERVER_AMD_RESULT: \${{ needs['observer-image-amd64'].result }}" <<<"$RELEASE_JOB" ||
  fail "release job does not read the amd64 observer result"
grep -Fq '[ "$OBSERVER_ARM_RESULT" = success ] &&' <<<"$RELEASE_JOB" ||
  fail "release job does not require a successful arm64 observer job"
grep -Fq '[ -e artifacts/digest-observer.txt ] &&' <<<"$RELEASE_JOB" ||
  fail "release job does not require the arm64 observer digest"
grep -Fq '[ -e artifacts/aboutme-observer.spdx.json ]' <<<"$RELEASE_JOB" ||
  fail "release job does not require the arm64 observer SBOM"
grep -Fq '[ "$OBSERVER_AMD_RESULT" = success ] &&' <<<"$RELEASE_JOB" ||
  fail "release job does not require a successful amd64 observer job"
grep -Fq '[ -e artifacts/digest-observer-amd64.txt ] &&' <<<"$RELEASE_JOB" ||
  fail "release job does not require the amd64 observer digest"
grep -Fq '[ -e artifacts/aboutme-observer-amd64.spdx.json ]' <<<"$RELEASE_JOB" ||
  fail "release job does not require the amd64 observer SBOM"
grep -Fq 'echo "ghcr.io/dannyota/aboutme-observer@$d linux/$arch" >>release/digests.txt' <<<"$RELEASE_JOB" ||
  fail "release job does not list each observer platform digest"
grep -Fq 'cp "artifacts/aboutme-$name-arm64.spdx.json" "release/aboutme-$name.spdx.json"' <<<"$RELEASE_JOB" ||
  fail "release job does not publish the legacy app SBOM name from the AWS arm64 image"

# Scheduled scans need a contract checked by pull-request CI.
assert_observer_scan() { # govulncheck job-body
  awk '
    function finish_step() { if (scan && directory && off) found = 1 }
    /^      - / {
      finish_step()
      scan = directory = off = in_env = 0
    }
    /^        [^ ]/ { in_env = 0 }
    $0 == "        run: govulncheck ./..." { scan = 1 }
    $0 == "        working-directory: deploy/observer" { directory = 1 }
    $0 == "        env:" { in_env = 1 }
    in_env && $0 == "          GOWORK: \"off\"" { off = 1 }
    END { finish_step(); exit !found }
  ' <<<"$1" || fail "$observer_diagnostic"
}

assert_image_scan() { # image-scan job-body
  local scan_step scan_command scan_env install_step
  scan_step=$(step_block "Scan released images" "$1")
  scan_command=$(awk '
    /^        run: \|$/ { run = 1; next }
    run && /^          / { print; next }
    run { exit }
  ' <<<"$scan_step")
  [ "$scan_command" = '          scripts/security-scan.sh weekly-scan \
            "$RUNNER_TEMP/trivy-bin/trivy" "$RUNNER_TEMP/reports" \
            "$RELEASE_TAG" "$OBSERVER_RELEASE_TAG"' ] ||
    fail "$image_command_diagnostic"
  scan_env=$(awk '
    /^        env:$/ { env = 1; next }
    env && /^          / { print; next }
    env { exit }
  ' <<<"$scan_step")
  if ! grep -Fxq '          RELEASE_TAG: ${{ inputs.release_tag }}' <<<"$scan_env" ||
    ! grep -Fxq '          OBSERVER_RELEASE_TAG: ${{ inputs.observer_release_tag }}' <<<"$scan_env"; then
    fail 'security-scan Scan released images must bind both release tag inputs in its env'
  fi
  install_step=$(step_block "Install Trivy" "$1")
  grep -Fxq '        run: scripts/security-scan.sh install-trivy "$RUNNER_TEMP/trivy-bin"' <<<"$install_step" ||
    fail 'security-scan must install Trivy before Scan released images'
  awk '
    $0 == "      - name: Install Trivy" { install = NR }
    $0 == "      - name: Scan released images" { scan = NR }
    END { exit !(install && scan && install < scan) }
  ' <<<"$1" || fail 'security-scan must install Trivy before Scan released images'
}

SECURITY_GO_JOB=$(awk '/^  govulncheck:$/{flag=1; next} /^  [a-z]/{flag=0} flag' "$SECURITY_WORKFLOW")
SECURITY_IMAGE_JOB=$(awk '/^  image-scan:$/{flag=1; next} /^  [a-z]/{flag=0} flag' "$SECURITY_WORKFLOW")
observer_diagnostic='security-scan govulncheck must run govulncheck ./... in deploy/observer with env GOWORK: "off" in the same step'
image_command_diagnostic='security-scan Scan released images must pass the Trivy binary, reports directory, release tag, and observer release tag to weekly-scan'
assert_observer_scan "$SECURITY_GO_JOB"
assert_image_scan "$SECURITY_IMAGE_JOB"

expect_security_drift() { # validator job-body diagnostic
  local status=0
  ( "$1" "$2" ) >"$WORK/security-drift" 2>&1 || status=$?
  [ "$status" -eq 1 ] || fail "security scan drift did not fail: $3"
  grep -Fxq "workflow-safety-test: $3" "$WORK/security-drift" ||
    fail "security scan drift lacks the expected diagnostic: $3"
}

for drift in \
  's/working-directory: deploy\/observer/working-directory: apps\/server/' \
  's/run: govulncheck .\/\.\.\./run: go version/' \
  's/GOWORK: "off"/GOWORK: "on"/' \
  '/working-directory: deploy\/observer/a\      - name: unrelated' \
  '/        env:/d'; do
  expect_security_drift assert_observer_scan \
    "$(sed "$drift" <<<"$SECURITY_GO_JOB")" "$observer_diagnostic"
done

for drift in \
  's/weekly-scan/scan-image/' \
  's|trivy-bin/trivy|other/trivy|' \
  's|\$RUNNER_TEMP/reports|\$RUNNER_TEMP/other|' \
  's/"\$RELEASE_TAG"/""/' \
  's/"\$OBSERVER_RELEASE_TAG"/""/'; do
  expect_security_drift assert_image_scan \
    "$(sed "$drift" <<<"$SECURITY_IMAGE_JOB")" "$image_command_diagnostic"
done
for input in release_tag observer_release_tag; do
  expect_security_drift assert_image_scan \
    "$(sed "s/inputs\.$input /inputs.other /" <<<"$SECURITY_IMAGE_JOB")" \
    'security-scan Scan released images must bind both release tag inputs in its env'
done
TRIVY_INSTALL_STEP=$(step_block "Install Trivy" "$SECURITY_IMAGE_JOB")
IMAGE_WITHOUT_INSTALL=$(awk '
  /^      - name: Install Trivy$/ { skip = 1; next }
  /^      - / { skip = 0 }
  !skip { print }
' <<<"$SECURITY_IMAGE_JOB")
expect_security_drift assert_image_scan \
  "$IMAGE_WITHOUT_INSTALL
$TRIVY_INSTALL_STEP" \
  'security-scan must install Trivy before Scan released images'

printf 'hosted workflow safety tests passed\n'
