import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { parseDocument, visit } from "yaml";

const root = fileURLToPath(new URL("../../", import.meta.url));
const read = (path) => readFileSync(`${root}${path}`, "utf8");
const pins = Object.fromEntries(
  read(".tool-versions")
    .trim()
    .split("\n")
    .filter((line) => line.trim() && !line.startsWith("#"))
    .map((line) => line.trim().split(/\s+/)),
);
const workflowFiles = readdirSync(`${root}.github/workflows`)
  .filter((name) => /\.ya?ml$/.test(name))
  .map((name) => `.github/workflows/${name}`);
const actionFiles = readdirSync(`${root}.github/actions`, { recursive: true })
  .filter((name) => /(^|\/)action\.ya?ml$/.test(name))
  .map((name) => `.github/actions/${name}`);
const hostedFiles = [...workflowFiles, ...actionFiles];

const sha = "a".repeat(40);
const actionFixture = (reference, comment = "v1.2.3") =>
  `jobs:\n  check:\n    steps:\n      - uses: ${reference} # ${comment}\n`;
const readWorkflow = (name) =>
  parseYaml(read(`.github/workflows/${name}.yml`), name).toJS();
const forkCondition = "${{ github.event_name == 'pull_request' && github.event.pull_request.head.repo.full_name != github.repository }}";

test("ci requires every job and calls the reusable VN infrastructure workflow", () => {
  checkRequiredGate(readWorkflow("ci"), readWorkflow("vn-infra"));
});

test("required gate rejects incomplete needs and independent VN triggers", () => {
  const fixtures = [
    ["removed need", (ci) => { ci.jobs.ci.needs.pop(); }, /complete job set/],
    ["extra unlisted job", (ci) => { ci.jobs.extra = { "runs-on": "ubuntu-latest" }; }, /complete job set/],
    ["restored push", (_ci, vn) => { vn.on.push = { paths: ["deploy/vn/**"] }; }, /VN triggers/],
    ["restored pull request", (_ci, vn) => { vn.on.pull_request = null; }, /VN triggers/],
    ["missing reusable trigger", (_ci, vn) => { delete vn.on.workflow_call; }, /VN triggers/],
    ["missing manual trigger", (_ci, vn) => { delete vn.on.workflow_dispatch; }, /VN triggers/],
    ["missing reusable call", (ci) => { delete ci.jobs["vn-infra"].uses; }, /VN workflow call/],
    ["conditional reusable call", (ci) => { ci.jobs["vn-infra"].if = "false"; }, /VN workflow must run/],
    ["conditional gate", (ci) => { delete ci.jobs.ci.if; }, /always/],
    ["unrestricted fork skips", (ci) => { aggregateStep(ci).env.ALLOW_SEMGREP_SKIP = "true"; }, /fork condition/],
  ];
  for (const [name, mutate, diagnostic] of fixtures) {
    const ci = readWorkflow("ci");
    const vn = readWorkflow("vn-infra");
    mutate(ci, vn);
    assert.throws(() => checkRequiredGate(ci, vn), diagnostic, name);
  }
});

test("aggregate accepts only successful jobs and fork-only Semgrep skips", () => {
  checkAggregateResults(readWorkflow("ci"));
});

test("aggregate rejects unrestricted job and event skips", () => {
  for (const condition of [
    'or (.key | IN("semgrep-code", "semgrep-supply-chain") | not)',
    'or $allow_semgrep_skip != "true"',
  ]) {
    const ci = readWorkflow("ci");
    aggregateStep(ci).run = aggregateStep(ci).run.replace(condition, "or false");
    assert.throws(() => checkAggregateResults(ci), /skipped/);
  }
});

function aggregateStep(ci) {
  const step = ci.jobs.ci.steps.find((step) => step.env?.NEEDS_JSON);
  assert.ok(step, "ci needs an aggregate result step");
  return step;
}

function checkRequiredGate(ci, vn) {
  assert.deepEqual(Object.keys(vn.on).sort(), ["workflow_call", "workflow_dispatch"], "VN triggers");
  assert.deepEqual(vn.permissions, { contents: "read" });
  assert.equal(vn.concurrency, undefined, "VN workflow must not cancel callers");
  assert.equal(ci.jobs["vn-infra"]?.uses, "./.github/workflows/vn-infra.yml", "VN workflow call");
  assert.equal(ci.jobs["vn-infra"].if, undefined, "VN workflow must run on every trigger");
  assert.deepEqual(
    [...ci.jobs.ci.needs].sort(),
    Object.keys(ci.jobs).filter((job) => job !== "ci").sort(),
    "ci needs must equal the complete job set",
  );
  assert.equal(ci.jobs.ci.if, "always()", "ci must always run");
  const step = aggregateStep(ci);
  assert.equal(step.if, "always()", "aggregate must always run");
  assert.equal(step.env.ALLOW_SEMGREP_SKIP, forkCondition, "Semgrep skip fork condition");
  assert.equal(step.env.NEEDS_JSON, "${{ toJSON(needs) }}");
}

function checkAggregateResults(ci) {
  const step = aggregateStep(ci);
  const jobs = ci.jobs.ci.needs;
  const contexts = [
    ["push", false],
    ["workflow_dispatch", false],
    ["same-repository pull_request", false],
    ["fork pull_request", true],
  ];
  const run = (needs, allowSkip) => {
    const result = spawnSync("bash", ["-e", "-o", "pipefail", "-c", step.run], {
      env: { ...process.env, NEEDS_JSON: JSON.stringify(needs), ALLOW_SEMGREP_SKIP: String(allowSkip) },
      encoding: "utf8",
      timeout: 5000,
    });
    assert.ifError(result.error);
    assert.ok(result.status === 0 || result.status === 1, result.stderr);
    return result.status;
  };
  for (const [context, allowSkip] of contexts) {
    const successful = Object.fromEntries(jobs.map((job) => [job, { result: "success" }]));
    assert.equal(run(successful, allowSkip), 0, `${context}: all jobs succeeded`);
    const bothSkipped = {
      ...successful,
      "semgrep-code": { result: "skipped" },
      "semgrep-supply-chain": { result: "skipped" },
    };
    assert.equal(run(bothSkipped, allowSkip), allowSkip ? 0 : 1, `${context}: both Semgrep jobs skipped`);
    for (const job of jobs) {
      for (const result of ["skipped", "cancelled", "failure", undefined]) {
        const needs = { ...successful, [job]: { result } };
        const allowed = allowSkip && result === "skipped" &&
          ["semgrep-code", "semgrep-supply-chain"].includes(job);
        assert.equal(run(needs, allowSkip), allowed ? 0 : 1, `${context}: ${job}=${result}`);
      }
    }
  }
}

test("external actions and reusable workflows require full commit pins", () => {
  for (const path of hostedFiles) checkActionPins(read(path), path);
});

test("action pin checks reject mutable and abbreviated refs", () => {
  for (const ref of ["v7", "main", "abcdef0", "", "a".repeat(39)]) {
    assert.throws(
      () => checkActionPins(actionFixture(`actions/checkout@${ref}`), "fixture"),
      /full commit pin/,
    );
  }
  assert.throws(
    () => checkActionPins(`jobs:\n  check:\n    uses: owner/repo/.github/workflows/check.yml@main\n`, "fixture"),
    /full commit pin/,
  );
  assert.throws(
    () => checkActionPins(`runs:\n  using: composite\n  steps:\n    - uses: actions/checkout@v7\n`, "fixture"),
    /full commit pin/,
  );
  assert.throws(
    () => checkActionPins(actionFixture(`actions/checkout@${sha}`, ""), "fixture"),
    /version comment/,
  );
});

test("action pin checks accept local references and quoted SHA pins", () => {
  checkActionPins(actionFixture("./.github/actions/setup-go", ""), "fixture");
  checkActionPins(actionFixture(`"actions/checkout@${sha}"`), "fixture");
  checkActionPins(actionFixture(`actions/cache/restore@${sha}`), "fixture");
  checkActionPins(`jobs:\n  check:\n    uses: ./.github/workflows/check.yml\n`, "fixture");
  checkActionPins(`jobs:\n  check:\n    uses: owner/repo/.github/workflows/check.yml@${sha} # v1.2.3\n`, "fixture");
});

test("Dependabot groups weekly GitHub Actions updates only", () => {
  checkDependabot(parseYaml(read(".github/dependabot.yml"), ".github/dependabot.yml").toJS());
});

test("Dependabot contract rejects missing grouping and changed scope or cadence", () => {
  const valid = {
    version: 2,
    updates: [{
      "package-ecosystem": "github-actions",
      directory: "/",
      schedule: { interval: "weekly" },
      groups: { actions: { patterns: ["*"] } },
    }],
  };
  checkDependabot(valid);
  for (const mutate of [
    (config) => { config.updates[0].groups = {}; },
    (config) => { config.updates[0].groups.actions.patterns = ["actions/*"]; },
    (config) => { config.updates[0].schedule.interval = "daily"; },
    (config) => { config.updates[0]["package-ecosystem"] = "npm"; },
    (config) => { config.updates.push({ "package-ecosystem": "npm" }); },
  ]) {
    const config = structuredClone(valid);
    mutate(config);
    assert.throws(() => checkDependabot(config));
  }
});

test("every hosted tool mirror agrees with .tool-versions", () => {
  const seen = new Set();
  for (const path of hostedFiles) checkToolMirrors(read(path), path, pins, seen);
  for (const tool of Object.keys(pins)) {
    assert.ok(seen.has(tool), `no hosted mirror checked for ${tool}`);
  }
});

test("one conflicting mirror fails even when another mirror matches", () => {
  const fixtures = [
    ["nodejs", (v) => `with: {node-version: '${v}'}`],
    ["golang", (v) => `with: {go-version: '${v}'}`],
    ["golangci-lint", (v) => `uses: golangci/golangci-lint-action@${sha}\nwith: {version: v${v}}`],
    ["govulncheck", (v) => `run: go install golang.org/x/vuln/cmd/govulncheck@v${v}`],
    ["sqlc", (v) => `run: go install github.com/sqlc-dev/sqlc/cmd/sqlc@v${v}`],
    ["gitleaks", (v) => `run: go install github.com/zricethezav/gitleaks/v8@v${v}`],
    ["semgrep", (v) => `run: pip install semgrep==${v}`],
    ["semgrep", (v) => `with: {key: 'pip-semgrep-${v}-linux'}`],
    ["caddy", (v) => `run: curl https://github.com/caddyserver/caddy/releases/download/v${v}/caddy_${v}_linux_amd64.tar.gz`],
    ["golang", (v) => `with: {key: 'go-toolchain-${v}-linux'}`],
    ["golang", (v) => `with: {path: '\${{ runner.tool_cache }}/go/${v}'}`],
  ];
  for (const [tool, fixture] of fixtures) {
    const matching = fixture(pins[tool]);
    checkToolMirrors(matching, "fixture", pins);
    const mixed = `steps:\n${[matching, fixture("99.0.0")]
      .map((value) => `  - ${value.replaceAll("\n", "\n    ")}`)
      .join("\n")}\n`;
    assert.throws(() => checkToolMirrors(mixed, "fixture", pins), /99\.0\.0/);
  }
  const changedPins = { ...pins, nodejs: "99.0.0" };
  assert.throws(
    () => checkToolMirrors(`with: {node-version: '${pins.nodejs}'}`, "fixture", changedPins),
    /want 99\.0\.0/,
  );
  checkToolMirrors("value: ${{ steps.setup.outputs.go-toolchain-hit }}", "fixture", pins);
});

function parseYaml(source, path) {
  const document = parseDocument(source);
  assert.deepEqual(document.errors, [], `${path}: invalid YAML`);
  return document;
}

function checkActionPins(source, path) {
  visit(parseYaml(source, path), {
    Pair(_key, pair) {
      if (pair.key?.value !== "uses") return;
      const reference = pair.value?.value;
      assert.equal(typeof reference, "string", `${path}: uses must be a string`);
      if (reference.startsWith("./")) return;
      assert.match(reference, /^[\w.-]+\/[\w./-]+@[a-fA-F0-9]{40}$/, `${path}: ${reference} needs a full commit pin`);
      assert.match(pair.value.comment ?? "", /^\s*v\d+\.\d+\.\d+\S*\s*$/, `${path}: ${reference} needs a version comment`);
    },
  });
}

function checkDependabot(config) {
  assert.equal(config.version, 2);
  assert.equal(config.updates?.length, 1, "only GitHub Actions updates are allowed");
  const update = config.updates[0];
  assert.equal(update["package-ecosystem"], "github-actions");
  assert.equal(update.directory, "/");
  assert.equal(update.schedule?.interval, "weekly");
  const groups = Object.values(update.groups ?? {});
  assert.equal(groups.length, 1, "one group must cover every action");
  assert.deepEqual(groups[0].patterns, ["*"]);
  assert.equal(groups[0]["exclude-patterns"], undefined);
  assert.equal(groups[0]["update-types"], undefined);
}

function checkToolMirrors(source, path, expected, seen = new Set()) {
  const check = (tool, actual) => {
    seen.add(tool);
    assert.equal(actual, expected[tool], `${path}: ${tool} has ${actual}, want ${expected[tool]}`);
  };
  const patterns = [
    ["govulncheck", /golang\.org\/x\/vuln\/cmd\/govulncheck@v?([^\s"';]+)/g],
    ["sqlc", /github\.com\/sqlc-dev\/sqlc\/cmd\/sqlc@v?([^\s"';]+)/g],
    ["gitleaks", /github\.com\/zricethezav\/gitleaks\/v8@v?([^\s"';]+)/g],
    ["semgrep", /semgrep==([^\s"';]+)/g],
    ["semgrep", /^pip-semgrep-([^-\s]+)/g],
    ["caddy", /caddyserver\/caddy\/releases\/download\/v([^/\s]+)/g],
    ["caddy", /caddy_([^_\s/]+)_linux_/g],
    ["golang", /^go-toolchain-([^-\s]+)/g],
    ["golang", /\$\{\{\s*runner\.tool_cache\s*\}\}\/go\/([^/\s]+)/g],
  ];
  const walk = (value) => {
    if (typeof value === "string") {
      for (const [tool, pattern] of patterns) {
        for (const match of value.matchAll(pattern)) check(tool, match[1]);
      }
    } else if (Array.isArray(value)) {
      value.forEach(walk);
    } else if (value && typeof value === "object") {
      for (const [key, nested] of Object.entries(value)) {
        if (key === "node-version") check("nodejs", String(nested));
        if (key === "go-version") check("golang", String(nested));
        walk(nested);
      }
      if (value.uses?.startsWith("golangci/golangci-lint-action@")) {
        check("golangci-lint", String(value.with?.version).replace(/^v/, ""));
      }
    }
  };
  walk(parseYaml(source, path).toJS());
}
