import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
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
