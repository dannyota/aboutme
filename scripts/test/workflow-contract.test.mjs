import assert from "node:assert/strict";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
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

const proseJobs = ["macos-tooling", "vn-infra", "server", "server-integration", "server-resumeapi", "web", "web-e2e", "dev-https-proofs"];
const selectionCondition = "github.event_name != 'pull_request' || needs.preflight.outputs.selection != 'prose'";

function checkSelection(ci) {
  const preflight = ci.jobs.preflight;
  assert.equal(preflight.outputs.selection, "${{ steps.selection.outputs.selection }}");
  const index = preflight.steps.findIndex((step) => step.id === "selection");
  const comparison = preflight.steps.findIndex((step) => step.id === "comparison");
  assert.ok(comparison >= 0 && index > comparison, "guards precede selection");
  const step = preflight.steps[index];
  assert.equal(step.if, undefined, "selection requires successful guards");
  assert.equal(step.env.BASE_SHA, "${{ steps.comparison.outputs.base_sha }}");
  assert.match(step.run, /python3 -I scripts\/ci-path-selection.py/);
  for (const name of proseJobs) {
    assert.deepEqual(ci.jobs[name].needs, ["preflight"], `${name}: required preflight`);
    assert.equal(ci.jobs[name].if, selectionCondition, `${name}: exact selection condition`);
  }
  assert.equal(ci.jobs.contracts.if, undefined, "all contracts always run");
  const expression = selectionCondition.replace("needs.preflight.outputs.selection", "selection");
  for (const event of ["pull_request", "push", "workflow_dispatch"]) {
    for (const selection of ["prose", "full", "", undefined, "unknown"]) {
      assert.equal(Function("github", "selection", `return ${expression}`)({ event_name: event }, selection),
        event !== "pull_request" || selection !== "prose");
    }
  }
}

test("selection retains guards, all contracts, and explicit PR omissions", () => {
  checkSelection(readWorkflow("ci"));
  for (const mutate of [
    (steps) => { steps.reverse(); },
    (steps) => { steps.find((step) => step.id === "selection").if = "always()"; },
  ]) {
    const ci = readWorkflow("ci"); mutate(ci.jobs.preflight.steps);
    assert.throws(() => checkSelection(ci), /guards precede selection|successful guards/);
  }
  for (const name of proseJobs) {
    for (const mutate of [
      (job) => { job.if = "needs.preflight.outputs.selection != 'prose'"; },
      (job) => { job.if = "github.event_name != 'pull_request'"; },
      (job) => { delete job.needs; },
    ]) {
      const ci = readWorkflow("ci");
      mutate(ci.jobs[name]);
      assert.throws(() => checkSelection(ci), /selection condition|required preflight/);
    }
  }
});

function selector(directory, base, event = "pull_request") {
  const result = spawnSync("python3", ["-I", join(root, "scripts/ci-path-selection.py"), event, base], {
    cwd: directory, encoding: "utf8", timeout: 5000,
  });
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stderr, "", "paths and errors stay out of logs");
  assert.match(result.stdout, /^(full|prose)\n$/);
  return result.stdout.trim();
}

function withRepository(run) {
  const directory = mkdtempSync(join(tmpdir(), "ci-selection-"));
  const git = (...args) => {
    const result = spawnSync("git", args, { cwd: directory, encoding: "utf8",
      env: { ...process.env, GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null" } });
    assert.equal(result.status, 0, result.stderr);
    return result.stdout.trim();
  };
  const put = (name, content = "prose\n") => {
    mkdirSync(join(directory, name, ".."), { recursive: true });
    writeFileSync(join(directory, name), content);
  };
  const commit = () => { git("add", "--all"); git("commit", "-qm", "Fixture"); return git("rev-parse", "HEAD"); };
  try {
    git("init", "-q"); git("config", "user.name", "Fixture"); git("config", "user.email", "fixture@example.invalid");
    git("config", "core.filemode", "true");
    put("seed", "seed\n"); commit();
    run({ directory, git, put, commit });
  } finally { rmSync(directory, { recursive: true, force: true }); }
}

test("selector classifies real Git paths and every event conservatively", () => {
  for (const [name, expected] of [
    ["docs/adr/1234-choice.md", "prose"], ["docs/plans/root.md", "prose"], ["docs/plans/.md", "prose"],
    ["docs/plans/nested/deep/entry.md", "prose"], ["docs/plans/Unicode tiếng Việt.md", "prose"],
    ["docs/plans/traceability/entry.md", "full"], ["docs/plans/AGENTS.md", "full"],
    ["docs/plans/nested/CLAUDE.md", "full"], ["docs/adr/README.md", "full"],
    ["docs/adr/1234-dir/entry.md", "full"], ["docs/adr/123-choice.md", "full"],
    ["docs/design/entry.md", "full"], ["docs/api/entry.md", "full"],
    ["docs/brand/entry.md", "full"], ["docs/other.md", "full"],
    ["docs/plans/line\nbreak.md", "full"], ["docs/plans/tab\tname.md", "full"],
    ["Docs/plans/entry.md", "full"], ["docs/plans/entry.MD", "full"],
    [".github/workflows/ci.yml", "full"], ["scripts/entry.py", "full"],
    ["Makefile", "full"], [".tool-versions", "full"], ["package-lock.json", "full"],
    ["apps/web/generated.ts", "full"], ["unknown", "full"],
  ]) {
    withRepository(({ directory, git, put, commit }) => {
      const base = git("rev-parse", "HEAD"); put(name); commit();
      for (const event of ["pull_request", "push", "workflow_dispatch", "unknown"]) {
        assert.equal(selector(directory, base, event), event === "pull_request" ? expected : "full", `${event}/${name}`);
      }
    });
  }
});

test("selector checks deletions, modifications, rename sides, and mixed changes", () => {
  for (const [before, after, expected] of [
    ["docs/plans/old.md", "docs/adr/1234-new.md", "prose"],
    ["docs/plans/old.md", "outside.md", "full"],
    ["outside.md", "docs/plans/new.md", "full"],
    ["docs/plans/old.md", null, "prose"], ["outside.md", null, "full"],
  ]) {
    withRepository(({ directory, git, put, commit }) => {
      put(before); const base = commit();
      if (after) { put(after); git("rm", before); } else git("rm", before);
      commit(); assert.equal(selector(directory, base), expected, `${before}/${after}`);
    });
  }
  withRepository(({ directory, put, commit }) => {
    put("docs/plans/entry.md"); const base = commit();
    put("docs/plans/entry.md", "changed\n"); commit();
    assert.equal(selector(directory, base), "prose");
    put("unknown"); commit(); assert.equal(selector(directory, base), "full");
  });
});

test("selector rejects invalid UTF-8 from a real Git diff", () => {
  withRepository(({ directory, git, put, commit }) => {
    const base = git("rev-parse", "HEAD");
    put("docs/plans/entry.md");
    const prose = commit();
    assert.equal(selector(directory, base), "prose");

    const blob = git("hash-object", "-w", "--stdin");
    const path = Buffer.concat([Buffer.from("docs/plans/"), Buffer.from([0xff]), Buffer.from(".md")]);
    const entry = Buffer.concat([Buffer.from(`100644 ${blob}\t`), path, Buffer.from([0])]);
    const index = spawnSync("git", ["update-index", "-z", "--index-info"], {
      cwd: directory, input: entry,
      env: { ...process.env, GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null" },
    });
    assert.ifError(index.error);
    assert.equal(index.status, 0, index.stderr.toString());
    git("commit", "-qm", "Invalid path");
    const diff = spawnSync("git", ["diff", "--no-ext-diff", "--no-textconv", "--name-status", "-z",
      "--find-renames", prose, "HEAD", "--"], { cwd: directory });
    assert.ifError(diff.error);
    assert.equal(diff.status, 0, diff.stderr.toString());
    assert.deepEqual(diff.stdout, Buffer.concat([Buffer.from("A\0"), path, Buffer.from([0])]));
    assert.equal(selector(directory, prose), "full");
  });
});

test("selector rejects unsafe modes, missing refs, and empty diffs", () => {
  for (const kind of ["symlink", "new symlink", "executable", "mode change", "submodule"]) {
    withRepository(({ directory, git, put, commit }) => {
      const name = "docs/plans/entry.md";
      put(name);
      if (kind === "executable") chmodSync(join(directory, name), 0o755);
      const base = ["executable", "new symlink"].includes(kind) ? git("rev-parse", "HEAD") : commit();
      if (["symlink", "new symlink"].includes(kind)) { rmSync(join(directory, name)); symlinkSync("../../../seed", join(directory, name)); }
      if (kind === "mode change") chmodSync(join(directory, name), 0o755);
      if (kind === "submodule") { git("update-index", "--add", "--cacheinfo", `160000,${base},docs/plans/module.md`); git("commit", "-qm", "Gitlink"); }
      else commit();
      assert.equal(selector(directory, base), "full", kind);
    });
  }
  withRepository(({ directory, git }) => {
    assert.equal(selector(directory, git("rev-parse", "HEAD")), "full");
    assert.equal(selector(directory, "unavailable"), "full");
  });
});

test("selector rejects malformed records, unsupported statuses, and bounded Git failures", () => {
  const script = `
import importlib.util, subprocess, sys
assert sys.dont_write_bytecode
spec = importlib.util.spec_from_file_location("selection", sys.argv[1])
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
bad_records = [b"", b"M\\0", b"M\\0docs/plans/a.md", b"R100\\0docs/plans/a.md\\0", b"C100\\0a\\0b\\0", b"T\\0a\\0", b"R101\\0a\\0b\\0", b"R-1\\0a\\0b\\0", b"M\\0\\0"]
original = m.git
for data in bad_records:
    try:
        m.parse_changes(data)
    except ValueError:
        pass
    else:
        raise AssertionError(repr(data))
    def malformed(arguments, deadline):
        if arguments[0] == "diff":
            return data
        return original(arguments, deadline)
    m.git = malformed
    assert m.select("pull_request", sys.argv[2]) == "full"
for name, expected in [(b"docs/plans/a.md", "prose"), (b"docs/plans/\\xff.md", "full")]:
    def simulated_name(arguments, deadline):
        if arguments[0] == "diff":
            return b"M\\0" + name + b"\\0"
        if arguments[0] == "ls-tree":
            return b"100644 blob " + b"0" * 40 + b"\\t" + name + b"\\0"
        return original(arguments, deadline)
    m.git = simulated_name
    for commit in (sys.argv[2], "HEAD"):
        assert m.tree(commit, 0) == {name: (b"100644", b"blob")}
    assert m.select("pull_request", sys.argv[2]) == expected
for score in range(101):
    assert m.parse_changes(("R%d\\0docs/plans/a.md\\0docs/plans/b.md\\0" % score).encode())
assert m.parse_changes(b"R050\\0docs/plans/a.md\\0docs/plans/b.md\\0")
for error in [OSError(), ValueError(), subprocess.TimeoutExpired("git", 30)]:
    def broken(*args, **kwargs):
        raise error
    m.git = broken
    assert m.select("pull_request", "HEAD") == "full"
`;
  withRepository(({ directory, git, put, commit }) => {
    const base = git("rev-parse", "HEAD"); put("docs/plans/a.md"); commit();
    const result = spawnSync("python3", ["-I", "-B", "-c", script, join(root, "scripts/ci-path-selection.py"), base], { cwd: directory, encoding: "utf8", timeout: 5000 });
    assert.ifError(result.error);
    assert.equal(result.status, 0, result.stderr);
  });
  withRepository(({ directory, git }) => {
    for (const [command, expected] of [
      ["printf prose; exit 1", "full"], ["printf unknown", "full"], ["exit 0", "full"],
      ["printf 'prose\\nextra=value'", "full"], ["printf prose", "prose"], ["printf full", "full"],
    ]) {
      const step = readWorkflow("ci").jobs.preflight.steps.find((step) => step.id === "selection");
      const output = join(directory, "output"); const summary = join(directory, "summary");
      writeFileSync(output, ""); writeFileSync(summary, "");
      const result = spawnSync("bash", ["-e", "-o", "pipefail", "-c", `python3() { ${command}; }\n${step.run}`], {
        cwd: directory, env: { ...process.env, GITHUB_EVENT_NAME: "pull_request", BASE_SHA: git("rev-parse", "HEAD"), GITHUB_OUTPUT: output, GITHUB_STEP_SUMMARY: summary }, encoding: "utf8",
      });
      assert.equal(result.status, 0, result.stderr);
      assert.equal(readFileSync(output, "utf8"), `selection=${expected}\n`);
      assert.equal(readFileSync(summary, "utf8"), `selection=${expected}\n`);
      assert.equal(result.stdout, "");
      assert.equal(result.stderr, "");
    }
  });
});

test("selector enforces output and time limits and falls back on diff and tree errors", () => {
  withRepository(({ directory, git, put, commit }) => {
    const base = git("rev-parse", "HEAD"); put("docs/plans/a.md"); commit();
    const script = `
import importlib.util, sys, time
assert sys.dont_write_bytecode
spec = importlib.util.spec_from_file_location("selection", sys.argv[1])
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
assert m.MAX_OUTPUT == 16 * 1024 * 1024
assert m.TIME_LIMIT == 30
assert m.select("pull_request", sys.argv[2]) == "prose"
m.MAX_OUTPUT = 1
try:
    m.git(["rev-parse", "HEAD"], time.monotonic() + 5)
except ValueError:
    pass
else:
    raise AssertionError("oversized output accepted")
assert m.select("pull_request", sys.argv[2]) == "full"
m.MAX_OUTPUT = 16 * 1024 * 1024
popen = m.subprocess.Popen
def delayed(*args, **kwargs):
    return popen([sys.executable, "-I", "-c", "import time; time.sleep(2)"], **kwargs)
m.subprocess.Popen = delayed
try:
    m.git(["rev-parse", "HEAD"], time.monotonic() + 0.01)
except TimeoutError:
    pass
else:
    raise AssertionError("running command exceeded deadline")
finally:
    m.subprocess.Popen = popen
try:
    m.git(["rev-parse", "HEAD"], time.monotonic() - 1)
except TimeoutError:
    pass
else:
    raise AssertionError("expired deadline accepted")
m.TIME_LIMIT = 0
assert m.select("pull_request", sys.argv[2]) == "full"
m.TIME_LIMIT = 30
original = m.git
for command in ["diff", "ls-tree"]:
    seen = []
    def broken(arguments, deadline):
        if arguments[0] == command:
            seen.append(command)
            raise OSError()
        return original(arguments, deadline)
    m.git = broken
    assert m.select("pull_request", sys.argv[2]) == "full"
    assert seen == [command]
`;
    const result = spawnSync("python3", ["-I", "-B", "-c", script, join(root, "scripts/ci-path-selection.py"), base], {
      cwd: directory, encoding: "utf8", timeout: 5000,
    });
    assert.ifError(result.error);
    assert.equal(result.status, 0, result.stderr);
  });
});

test("selector compares the candidate trees directly for merges and nonancestor bases", () => {
  withRepository(({ directory, git, put, commit }) => {
    const base = git("rev-parse", "HEAD");
    git("checkout", "-qb", "first"); put("docs/plans/first.md"); const first = commit();
    git("checkout", "-qb", "second", base); put("docs/plans/second.md"); const second = commit();
    assert.equal(selector(directory, first), "prose", "nonancestor comparison");
    git("merge", "--no-ff", "-qm", "Combined prose", first);
    assert.equal(selector(directory, base), "prose", "train merge candidate");
    git("checkout", "-qb", "mixed", second); put("unknown"); const mixed = commit();
    git("checkout", "first"); git("merge", "--no-ff", "-qm", "Mixed candidate", mixed);
    assert.equal(selector(directory, base), "full", "combined change includes unknown paths");
  });
});

test("selector checks old modes on deletions and renames", () => {
  for (const kind of ["symlink", "executable"]) {
    for (const rename of [false, true]) {
      withRepository(({ directory, git, put, commit }) => {
        const name = "docs/plans/old.md";
        put(name);
        if (["symlink", "new symlink"].includes(kind)) { rmSync(join(directory, name)); symlinkSync("../../../seed", join(directory, name)); }
        else chmodSync(join(directory, name), 0o755);
        const base = commit();
        if (rename) git("mv", name, "docs/plans/new.md");
        else git("rm", name);
        commit(); assert.equal(selector(directory, base), "full", `${kind}/${rename}`);
      });
    }
  }
});

const imageCheck = "./.github/actions/release-image-check";
const readImageCheck = () => parseYaml(read(`${imageCheck.slice(2)}/action.yml`), imageCheck).toJS();

test("manual image checks have no release authority and publication stays bounded", () => {
  checkReleaseContracts(readWorkflow("release-check"), readWorkflow("release-images"), readImageCheck());
});

test("release contracts reject changed or skipped attestation inputs", () => {
  for (const name of ["build", "image", "observer-image", "observer-image-amd64"]) {
    for (const prefix of ["actions/attest-build-provenance@", "actions/attest@"]) {
      if (name === "image" && prefix === "actions/attest@") continue;
      const fixtures = [
        ["subject-name", (step) => { step.with["subject-name"] = "ghcr.io/unreviewed/image"; }, /signing inputs/],
        ["subject-digest", (step) => { step.with["subject-digest"] = "${{ github.sha }}"; }, /signing inputs/],
        ["push-to-registry", (step) => { step.with["push-to-registry"] = false; }, /signing inputs/],
        ["skipped signing", (step) => { step.if = false; }, /signing condition/],
        ["signing after failure", (step) => { step.if = "always()"; }, /signing condition/],
        ["ignored signing failure", (step) => { step["continue-on-error"] = true; }, /signing failure/],
      ];
      if (prefix === "actions/attest@") fixtures.push(
        ["sbom-path", (step) => { step.with["sbom-path"] = "unreviewed.spdx.json"; }, /signing inputs/],
        ["storage record", (step) => { step.with["create-storage-record"] = true; }, /signing inputs/],
      );
      for (const [drift, mutate, diagnostic] of fixtures) {
        const release = readWorkflow("release-images");
        mutate(release.jobs[name].steps.find((step) => step.uses?.startsWith(prefix)));
        assert.throws(() => checkReleaseContracts(readWorkflow("release-check"), release, readImageCheck()), diagnostic, `${name}/${prefix}/${drift}`);
      }
    }
  }
});

test("release contracts reject candidate checkout and local action replacement", () => {
  for (const name of ["build", "observer-image", "observer-image-amd64"]) {
    const fixtures = [
      ["repository", (checkout) => { checkout.with = { repository: "unreviewed/aboutme" }; }, /candidate repository/],
      ["ref", (checkout) => { checkout.with = { ref: "refs/heads/unreviewed" }; }, /candidate event commit/],
      ["path", (checkout) => { checkout.with = { path: "unreviewed" }; }, /candidate workspace/],
      ["checkout replacement", (checkout) => { checkout.uses = `unreviewed/checkout@${sha}`; }, /candidate checkout action/],
      ["skipped checkout", (checkout) => { checkout.if = false; }, /candidate checkout condition/],
      ["ignored checkout failure", (checkout) => { checkout["continue-on-error"] = true; }, /candidate checkout failure/],
      ["composite replacement", (_checkout, check) => { check.uses = "./.github/actions/unreviewed"; }, /shared checks precede push/],
    ];
    for (const [drift, mutate, diagnostic] of fixtures) {
      const release = readWorkflow("release-images");
      const steps = release.jobs[name].steps;
      const check = steps.findIndex((step) => step.uses === imageCheck);
      mutate(steps[check - 1], steps[check]);
      assert.throws(() => checkReleaseContracts(readWorkflow("release-check"), release, readImageCheck()), diagnostic, `${name}/${drift}`);
    }
  }
});

test("release contracts reject authority, dependency, timeout, and signing drift", () => {
  const fixtures = [
    ["manual contents write", (manual) => { manual.permissions.contents = "write"; }, /read-only/],
    ["manual packages write", (manual) => { manual.permissions.packages = "write"; }, /read-only/],
    ["manual identity token", (manual) => { manual.permissions["id-token"] = "write"; }, /read-only/],
    ["manual job write", (manual) => { manual.jobs.build.permissions.packages = "write"; }, /read-only/],
    ["manual job identity token", (manual) => { manual.jobs.build.permissions["id-token"] = "write"; }, /read-only/],
    ["manual unrestricted permissions", (manual) => { manual.jobs.build.permissions = "write-all"; }, /read-only/],
    ["manual stores checkout token", (manual) => { manual.jobs.build.steps[0].with["persist-credentials"] = true; }, /checkout credentials/],
    ["manual omits architecture", (manual) => { manual.jobs.build.strategy.matrix.arch.pop(); }, /manual architectures/],
    ["manual emulated architecture", (manual) => { manual.jobs.build.strategy.matrix.include.find((entry) => entry.arch === "arm64").runner = "ubuntu-24.04"; }, /native arm64/],
    ["manual tag trigger", (manual) => { manual.on.push = { tags: ["v*"] }; }, /manual trigger/],
    ["manual secret", (manual) => { manual.jobs.build.env.TOKEN = "${{ secrets.GITHUB_TOKEN }}"; }, /secret/],
    ["publication manual trigger", (_manual, release) => { release.on.workflow_dispatch = null; }, /tag trigger/],
    ["cancel publication", (_manual, release) => { release.concurrency["cancel-in-progress"] = true; }, /serialize/],
    ["unserialized publication", (_manual, release) => { delete release.concurrency; }, /serialize/],
    ["different group per attempt", (_manual, release) => { release.concurrency.group += "-${{ github.run_attempt }}"; }, /serialize/],
    ["missing app dependency", (_manual, release) => { delete release.jobs.image.needs; }, /app build gate/],
    ["index ignores build failure", (_manual, release) => { release.jobs.image.if = "always()"; }, /app index success gate/],
    ["observer gates app builds", (_manual, release) => { release.jobs.build.needs = ["eligibility", "observer-image"]; }, /independent app builds/],
    ["observer gates indexes", (_manual, release) => { release.jobs.image.needs = ["build", "observer-image"]; }, /app build gate/],
    ["observer gates release", (_manual, release) => { release.jobs.release.if = "success()"; }, /app release gate/],
    ["app failure ignored", (_manual, release) => { release.jobs.build["continue-on-error"] = true; }, /must block/],
    ["push ignores scan failure", (_manual, release) => { release.jobs.build.steps.find((step) => step.id === "push").if = "always()"; }, /push success/],
    ["signing removed", (_manual, release) => { release.jobs.image.steps = release.jobs.image.steps.filter((step) => !step.uses?.startsWith("actions/attest-build-provenance@")); }, /signing identity/],
    ["shared scan ignored", (_manual, _release, action) => { action.runs.steps.find((step) => step.id === "scan-image")["continue-on-error"] = true; }, /must block/],
    ["shared scan conditional", (_manual, _release, action) => { action.runs.steps.find((step) => step.id === "scan-image").if = "false"; }, /unconditional/],
    ["shared scan omitted", (_manual, _release, action) => { action.runs.steps.find((step) => step.id === "scan-image").run = "true"; }, /scan command/],
    ["shared command probe ignored", (_manual, _release, action) => { action.runs.steps.find((step) => step.id === "probe-host-operation-dispatch")["continue-on-error"] = true; }, /must block/],
    ["shared command probe skipped", (_manual, _release, action) => { action.runs.steps.find((step) => step.id === "probe-host-operation-dispatch").if = "false"; }, /server-only check/],
    ["shared publication", (_manual, _release, action) => { action.runs.steps.push({ run: 'docker push "$IMAGE"' }); }, /build-only/],
  ];
  for (const [name, mutate, diagnostic] of fixtures) {
    const manual = readWorkflow("release-check");
    const release = readWorkflow("release-images");
    const action = readImageCheck();
    mutate(manual, release, action);
    assert.throws(() => checkReleaseContracts(manual, release, action), diagnostic, name);
  }
  for (const workflow of ["release-check", "release-images"]) {
    for (const name of Object.keys(readWorkflow(workflow).jobs)) {
      const manual = readWorkflow("release-check");
      const release = readWorkflow("release-images");
      const target = workflow === "release-check" ? manual : release;
      delete target.jobs[name]["timeout-minutes"];
      assert.throws(() => checkReleaseContracts(manual, release, readImageCheck()), /timeout-minutes/, `${workflow}/${name}`);
    }
  }
});

test("app build, smoke, command probe, or scan failure prevents a push", () => {
  const steps = readImageCheck().runs.steps.filter((step) => step.run);
  const directory = mkdtempSync(join(tmpdir(), "release-check-contract-"));
  const trace = join(directory, "trace");
  mkdirSync(join(directory, "scripts"));
  writeFileSync(join(directory, "scripts/security-scan.sh"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\\n' "$1" >> "$TRACE_FILE"
if [ "$1" = scan-image ] && [ "$FAIL_STAGE" = scan ]; then exit 1; fi
`, { mode: 0o700 });
  const stub = `
timeout() {
  [ "$1" = 30 ] || return 125
  shift
  "$@"
}
docker() {
  printf 'docker %s\\n' "$1" >> "$TRACE_FILE"
  case "$1" in
    build) [ "$FAIL_STAGE" != build ] ;;
    inspect) printf 'linux/%s\\n' "$ARCH" ;;
    run)
      if [[ $* == *"--entrypoint /usr/local/bin/server"* ]]; then
        local probe code output
        case "\${*:8}" in
          'account-delete status invalid')
            probe=usage code=2
            output='usage: server account-delete {show <slug> | confirm <slug> <user-id> | status <user-id>}' ;;
          'account-delete status 018f5b6a-9a3e-7c21-8b1e-000000000030')
            probe=status code=1 output='account-delete status failed' ;;
          'media-deletion-status extra')
            probe=queue code=2 output='usage: server media-deletion-status' ;;
          'totp-key-reencrypt extra')
            probe=reencrypt code=1 output='usage: server totp-key-reencrypt' ;;
          *) return 125 ;;
        esac
        printf 'probe %s\\n' "$probe" >> "$TRACE_FILE"
        [ "$FAIL_STAGE" != "probe-$probe" ] || return 99
        [ "$FAIL_STAGE" != probe-timeout ] || return 124
        [ "$FAIL_STAGE" != probe-output ] || output=unexpected
        printf '%s\\n' "$output"
        return "$code"
      fi
      [ "$FAIL_STAGE" != smoke ] ;;
  esac
}
`;
  const script = steps.map((step) => {
    const command = `bash -e -o pipefail <<'CHECK_STEP'\n${stub}\n${step.run}\nCHECK_STEP`;
    if (step.if === undefined) return command;
    assert.equal(step.if, "env.NAME == 'server'", `${step.name}: supported condition`);
    return `if [[ "$NAME" == server ]]; then\n${command}\nfi`;
  }).join("\n") + '\nprintf "push\\n" >> "$TRACE_FILE"\n';
  try {
    for (const name of ["caddy", "server"]) {
      for (const arch of ["arm64", "amd64"]) {
        for (const stage of ["none", "build", "smoke", "scan", "probe-usage", "probe-status", "probe-queue", "probe-reencrypt", "probe-output", "probe-timeout"]) {
          rmSync(trace, { force: true });
          const result = spawnSync("bash", ["-e", "-o", "pipefail", "-c", script], {
            cwd: directory,
            env: { ...process.env, TRACE_FILE: trace, FAIL_STAGE: stage, NAME: name,
              ARCH: arch, IMAGE: "fixture", DOCKERFILE: "fixture", REPORT_NAME: "fixture", RUNNER_TEMP: directory },
            encoding: "utf8", timeout: 5000,
          });
          assert.ifError(result.error);
          const succeeds = stage === "none" || (name !== "server" && stage.startsWith("probe-"));
          assert.equal(result.status, succeeds ? 0 : 1, `${name}/${arch}/${stage}: ${result.stderr}`);
          const commands = readFileSync(trace, "utf8").trim().split("\n");
          assert.equal(commands.includes("push"), succeeds, `${name}/${arch}/${stage}: publication`);
          if (succeeds) {
            assert.ok(commands.includes("scan-image"), "scan must run");
            assert.ok(commands.includes("report"), "report must run");
            assert.ok(commands.indexOf("scan-image") < commands.indexOf("push"));
            assert.deepEqual(commands.filter((command) => command.startsWith("probe ")),
              name === "server" ? ["probe usage", "probe status", "probe queue", "probe reencrypt"] : [],
              `${name}/${arch}/${stage}: server-only dispatch probes`);
          }
        }
      }
    }
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("app release gate accepts observer-only failures and rejects app failures", () => {
  const release = readWorkflow("release-images").jobs.release;
  assert.equal(release.if, "${{ !cancelled() && needs.image.result == 'success' }}");
  const expression = release.if.slice(3, -2);
  for (const image of ["success", "failure", "cancelled", "skipped", undefined]) {
    for (const observer of ["success", "failure", "skipped"]) {
      for (const observerAmd of ["success", "failure", "skipped"]) {
        const needs = { image: { result: image }, "observer-image": { result: observer }, "observer-image-amd64": { result: observerAmd } };
        for (const cancelled of [false, true]) {
          const accepted = Function("needs", "cancelled", `return ${expression}`)(needs, () => cancelled);
          assert.equal(accepted, !cancelled && image === "success", `${image}/${observer}/${observerAmd}/${cancelled}`);
        }
      }
    }
  }
});

function checkReleaseContracts(manual, release, action) {
  assert.deepEqual(Object.keys(manual.on), ["workflow_dispatch"], "manual trigger");
  assert.deepEqual(manual.permissions, { contents: "read" }, "manual read-only permissions");
  assert.deepEqual(Object.keys(manual.jobs), ["build"], "manual build job set");
  for (const job of Object.values(manual.jobs)) {
    assert.deepEqual(job.permissions, { contents: "read" }, "manual job read-only permissions");
    assert.equal(job["timeout-minutes"], 40, "manual timeout-minutes");
    assert.equal(job.strategy["fail-fast"], false);
    assert.deepEqual(job.strategy.matrix.name, ["server", "web", "caddy", "observer"]);
    assert.deepEqual(job.strategy.matrix.arch, ["arm64", "amd64"], "manual architectures");
    for (const [arch, runner, machine] of [["arm64", "ubuntu-24.04-arm", "aarch64"], ["amd64", "ubuntu-24.04", "x86_64"]]) {
      assert.deepEqual(job.strategy.matrix.include.find((entry) => entry.arch === arch), { arch, runner, machine }, `native ${arch} runner`);
    }
    assert.equal(job.steps[0].with["persist-credentials"], false, "manual checkout credentials");
    assert.equal(job.steps[1].uses, imageCheck);
    assert.equal(job.steps.length, 2, "manual build-only steps");
  }
  assert.doesNotMatch(JSON.stringify(manual), /secrets\./, "manual secret access");
  assert.deepEqual(release.on, { push: { tags: ["v*"] } }, "publication tag trigger");
  assert.deepEqual(release.concurrency, { group: "release-images-${{ github.ref }}", "cancel-in-progress": false }, "serialize publication for each tag");
  const timeouts = { eligibility: 5, build: 40, image: 10, "observer-image": 20, "observer-image-amd64": 20, release: 10 };
  assert.deepEqual(Object.keys(release.jobs).sort(), Object.keys(timeouts).sort());
  for (const [name, minutes] of Object.entries(timeouts)) {
    const job = release.jobs[name];
    assert.equal(job["timeout-minutes"], minutes, `${name}: timeout-minutes`);
    assert.equal(job["continue-on-error"], undefined, `${name}: must block on failure`);
  }
  assert.equal(release.jobs.build.needs, "eligibility", "independent app builds");
  assert.equal(release.jobs.image.needs, "build", "app build gate");
  assert.equal(release.jobs.image.if, "github.event_name == 'push'", "app index success gate");
  assert.deepEqual(release.jobs.release.needs, ["image", "observer-image", "observer-image-amd64"]);
  assert.equal(release.jobs.release.if, "${{ !cancelled() && needs.image.result == 'success' }}", "app release gate");
  for (const name of ["build", "observer-image", "observer-image-amd64"]) {
    const job = release.jobs[name];
    const check = job.steps.findIndex((step) => step.uses === imageCheck);
    const push = job.steps.findIndex((step) => step.id === "push");
    assert.ok(check >= 0 && push > check, `${name}: shared checks precede push`);
    const checkout = job.steps[check - 1];
    assert.equal(checkout?.uses, "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1", `${name}: candidate checkout action`);
    assert.equal(checkout.with?.repository ?? "${{ github.repository }}", "${{ github.repository }}", `${name}: candidate repository`);
    assert.equal(checkout.with?.ref ?? "${{ github.sha }}", "${{ github.sha }}", `${name}: candidate event commit`);
    assert.equal(checkout.with?.path ?? "", "", `${name}: candidate workspace`);
    assert.equal(checkout.if, undefined, `${name}: candidate checkout condition`);
    assert.equal(checkout["continue-on-error"], undefined, `${name}: candidate checkout failure`);
    assert.equal(job.steps[check].if, undefined, `${name}: shared checks unconditional`);
    assert.equal(job.steps[check]["continue-on-error"], undefined, `${name}: must block on check failure`);
    assert.equal(job.steps[push].if, "github.event_name == 'push'", `${name}: push success gate`);
    assert.equal(job.needs, "eligibility", `${name}: eligibility gate`);
  }
  for (const name of ["build", "image", "observer-image", "observer-image-amd64"]) {
    const job = release.jobs[name];
    assert.deepEqual(job.permissions, { actions: "read", contents: "read", packages: "write", "id-token": "write", attestations: "write" });
    const subject = name.startsWith("observer-") ? "ghcr.io/dannyota/aboutme-observer" : "ghcr.io/dannyota/aboutme-${{ matrix.name }}";
    const digest = name === "image" ? "${{ steps.index.outputs.digest }}" : "${{ steps.push.outputs.digest }}";
    const sbom = name === "build" ? "aboutme-${{ matrix.name }}-${{ matrix.arch }}.spdx.json" :
      name === "observer-image" ? "aboutme-observer.spdx.json" : "aboutme-observer-amd64.spdx.json";
    for (const prefix of ["actions/attest-build-provenance@", "actions/attest@"]) {
      if (name === "image" && prefix === "actions/attest@") continue;
      const steps = job.steps.filter((step) => step.uses?.startsWith(prefix));
      assert.equal(steps.length, 1, `${name}: signing identity stays in release-images (${prefix})`);
      const step = steps[0];
      assert.equal(step.if, name === "image" ? undefined : "github.event_name == 'push'", `${name}: signing condition (${prefix})`);
      assert.equal(step["continue-on-error"], undefined, `${name}: signing failure (${prefix})`);
      const inputs = { "subject-name": subject, "subject-digest": digest, "push-to-registry": true };
      if (prefix === "actions/attest@") Object.assign(inputs, { "sbom-path": sbom, "create-storage-record": false });
      assert.deepEqual(step.with, inputs, `${name}: signing inputs (${prefix})`);
    }
  }
  assert.deepEqual(release.jobs.eligibility.permissions, { contents: "read", actions: "read" });
  assert.deepEqual(release.jobs.release.permissions, { actions: "read", contents: "write" });
  assert.equal(action.runs.using, "composite");
  assert.deepEqual(action.runs.steps.map((step) => step.id), ["build-release-image", "smoke-test-release-image", "probe-host-operation-dispatch", "install-trivy", "scan-image", "upload-trivy-report"], "shared build-only steps");
  for (const step of action.runs.steps) {
    assert.equal(step["continue-on-error"], undefined, `${step.name}: must block on failure`);
    if (step.id === "probe-host-operation-dispatch") {
      assert.equal(step.if, "env.NAME == 'server'", `${step.name}: server-only check`);
    } else {
      assert.equal(step.if, step.id === "upload-trivy-report" ? "always()" : undefined, `${step.name}: unconditional check`);
    }
    if (step.run) assert.equal(step.shell, "bash");
  }
  const scan = action.runs.steps.find((step) => step.id === "scan-image");
  assert.equal(scan.env.REPORT_NAME, "${{ inputs.report-name }}");
  assert.equal(scan.run, [
    'scripts/security-scan.sh scan-image \\',
    '  "$RUNNER_TEMP/trivy-bin/trivy" "$IMAGE" "" "trivy-$REPORT_NAME.json"',
    'scripts/security-scan.sh report "trivy-$REPORT_NAME.json"',
    "",
  ].join("\n"), "shared scan command");
  assert.doesNotMatch(JSON.stringify(action), /secrets\.|docker (push|login)|actions\/attest|gh release/, "shared action is build-only");
}

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
  ];
  for (const [name, mutate, diagnostic] of fixtures) {
    const ci = readWorkflow("ci");
    const vn = readWorkflow("vn-infra");
    mutate(ci, vn);
    assert.throws(() => checkRequiredGate(ci, vn), diagnostic, name);
  }
});

test("required gate keeps the exact ci check name without a matrix", () => {
  const ci = readWorkflow("ci");
  ci.jobs.ci.name = "ci";
  checkRequiredGate(ci, readWorkflow("vn-infra"));
  const fixtures = [
    ["renamed aggregate id", (ci) => { ci.jobs.gate = ci.jobs.ci; delete ci.jobs.ci; }, /aggregate job id/],
    ["renamed aggregate", (ci) => { ci.jobs.ci.name = "CI"; }, /exact ci check name/],
    ["dynamic aggregate", (ci) => { ci.jobs.ci.name = "${{ github.job }}"; }, /exact ci check name/],
    ["null aggregate name", (ci) => { ci.jobs.ci.name = null; }, /exact ci check name/],
    ["aggregate matrix", (ci) => { ci.jobs.ci.strategy = { matrix: { shard: ["one"] } }; }, /no matrix/],
    ["dynamic aggregate matrix", (ci) => { ci.jobs.ci.strategy = { matrix: "${{ fromJSON(needs.preflight.outputs.matrix) }}" }; }, /no matrix/],
    ["duplicate CI check", (ci) => { ci.jobs.contracts.name = "ci"; }, /sole ci check producer/],
    ["dynamic CI check", (ci) => { ci.jobs.contracts.name = "${{ github.job }}"; }, /static check name/],
  ];
  for (const [name, mutate, diagnostic] of fixtures) {
    const ci = readWorkflow("ci");
    mutate(ci);
    assert.throws(() => checkRequiredGate(ci, readWorkflow("vn-infra")), diagnostic, name);
  }
});

test("required gate rejects competing and unprovable check names in every workflow", () => {
  for (const path of workflowFiles.filter((path) => path !== ".github/workflows/ci.yml")) {
    const fixtures = [
      ["implicit ci", { ci: {} }, /sole ci check producer/],
      ["explicit ci", { other: { name: "ci" } }, /sole ci check producer/],
      ["dynamic name", { other: { name: "${{ matrix.name }}" } }, /static check name/],
      ["partial ci prefix", { other: { name: "c${{ matrix.x }}" } }, /static check name/],
      ["complete ci prefix", { other: { name: "ci${{ matrix.x }}" } }, /static check name/],
      ["non-string name", { other: { name: null } }, /static check name/],
    ];
    for (const [name, jobs, diagnostic] of fixtures) {
      const workflows = Object.fromEntries(workflowFiles.map((path) => [path, parseYaml(read(path), path).toJS()]));
      Object.assign(workflows[path].jobs, jobs);
      assert.throws(
        () => checkRequiredGate(workflows[".github/workflows/ci.yml"], workflows[".github/workflows/vn-infra.yml"], workflows),
        diagnostic,
        `${path}/${name}`,
      );
    }
  }
  const vn = readWorkflow("vn-infra");
  vn.jobs.ci = { name: "Infrastructure checks" };
  checkRequiredGate(readWorkflow("ci"), vn);
});

test("required gate rejects YAML labels without a distinct visible prefix", () => {
  for (const [name, scalar] of [
    ["leading space", '" ${{ x }}"'],
    ["uppercase ci", '"CI ${{ x }}"'],
    ["mixed case ci", '"Ci${{ x }}"'],
    ["folded empty prefix", ">-\n\n  ${{ x }}\n"],
    ["quoted empty prefix", '"\\t${{ x }}"'],
  ]) {
    const ci = readWorkflow("ci");
    ci.jobs.contracts.name = parseYaml(`name: ${scalar}\n`, name).toJS().name;
    assert.throws(() => checkRequiredGate(ci, readWorkflow("vn-infra")), /static check name/, name);
  }
});

test("required gate accepts matrix labels with a distinct static prefix", () => {
  const ci = readWorkflow("ci");
  ci.jobs["web-e2e"].name = parseYaml('name: "Web E2E (${{ matrix.shard }})"\n', "fixture").toJS().name;
  checkRequiredGate(ci, readWorkflow("vn-infra"));
});

test("aggregate accepts success and only explicit prose skips", () => {
  checkAggregateResults(readWorkflow("ci"));
});

test("aggregate rejects unrestricted job and event skips", () => {
  for (const condition of [
    '$event == "pull_request"',
    '(.key | IN("macos-tooling", "vn-infra", "server", "server-integration", "server-resumeapi", "web", "web-e2e", "dev-https-proofs"))',
  ]) {
    const ci = readWorkflow("ci");
    const step = aggregateStep(ci);
    assert.ok(step.run.includes(condition), `mutation target: ${condition}`);
    step.run = step.run.replace(condition, "true");
    assert.throws(() => checkAggregateResults(ci));
  }
});

test("browser groups retain every proof and shard on PR and main", () => {
  checkProofCoverage(readWorkflow("ci"));
});

test("browser coverage rejects omitted proofs and lost shards", () => {
  for (const omitted of ["linkedin-import", "mcp-ts-sdk", "totp:skew", "passkey:recovery-attempts"]) {
    const ci = readWorkflow("ci");
    for (const group of ci.jobs["dev-https-proofs"].strategy.matrix.include) {
      group.proofs = expandProofs(group.proofs).filter((proof) => proof !== omitted).join(" ");
    }
    assert.throws(() => checkProofCoverage(ci), /complete proof coverage/, omitted);
  }
  for (const kind of ["passkey", "totp"]) {
    const ci = readWorkflow("ci");
    const step = ci.jobs.ci.steps.find((step) => step.run?.includes(`check-shard-coverage.mjs ${kind}`));
    step.run = "true";
    assert.throws(() => checkProofCoverage(ci), /shard evidence gate/, kind);
  }
});

test("browser setup timings survive setup failure and reach an artifact", () => {
  const steps = readWorkflow("ci").jobs["dev-https-proofs"].steps;
  const start = steps.findIndex((step) => step.id === "start-setup-timing");
  const setup = steps.findIndex((step) => step.id === "setup");
  const record = steps.findIndex((step) => step.id === "record-setup-timing");
  assert.ok(start >= 0 && start < setup && setup < record, "setup timing brackets setup");
  assert.equal(steps[record].if, "always()");
  assert.match(steps[start].run, /date \+%s/);
  assert.match(steps[record].run, /date \+%s/);
  assert.equal(steps[record].env.SETUP_RESULT, "${{ steps.setup.outcome }}");
  const upload = steps.find((step) => step.id === "upload-browser-proof-timings");
  assert.equal(upload.if, "always()");
  assert.equal(upload.with.path, ".dev/proof-timing/");
  assert.equal(upload.with["include-hidden-files"], true);
});

function expandProofs(proofs) {
  return proofs.trim().split(/\s+/).flatMap((proof) => {
    if (!proof.startsWith("totp:")) return [proof];
    return proof.slice(5).split(",").map((shard) => `totp:${shard}`);
  });
}

function checkProofCoverage(ci) {
  assert.ok(Object.hasOwn(ci.on, "push") && Object.hasOwn(ci.on, "pull_request"), "PR and main triggers");
  assert.equal(ci.on.push.paths, undefined, "full main coverage");
  assert.equal(ci.on.push["paths-ignore"], undefined, "full main coverage");
  assert.equal(ci.on.pull_request?.paths, undefined, "full PR coverage");
  assert.equal(ci.on.pull_request?.["paths-ignore"], undefined, "full PR coverage");
  const evidenceSteps = ci.jobs.ci.steps.filter((step) =>
    step.uses?.startsWith("actions/checkout@") || step.uses?.startsWith("actions/download-artifact@"));
  assert.equal(evidenceSteps.length, 2, "shard evidence checkout and download");
  for (const step of evidenceSteps) assert.equal(step.if, "${{ needs.dev-https-proofs.result == 'success' }}");
  assert.equal(evidenceSteps[1].with.pattern, "shard-evidence-*");
  const job = ci.jobs["dev-https-proofs"];
  assert.equal(job.if, selectionCondition, "browser groups use explicit selection");
  assert.equal(job.strategy["fail-fast"], false, "all groups finish");
  const groups = job.strategy.matrix.include;
  assert.equal(groups.length, 9, "existing browser group count");
  assert.equal(new Set(groups.map((group) => group.group)).size, 9, "unique browser groups");
  assert.deepEqual(groups.flatMap((group) => expandProofs(group.proofs)).sort(), [
    "totp:replay-concurrent", "totp:primary", "totp:skew", "totp:locale-attempts",
    "totp:replace-recovery", "totp:epoch-disabled", "passkey:primary-disabled",
    "passkey:recovery-attempts", "public", "auth", "editor", "exports", "entry",
    "publish", "linkedin-import", "password", "privacy", "transport", "mcp",
    "mcp-sdk", "mcp-ts-sdk", "sample-start", "linkedin",
  ].sort(), "complete proof coverage");
  const run = job.steps.find((step) => step.env?.PROOFS);
  assert.equal(run.env.PROOFS, "${{ matrix.proofs }}");
  assert.match(run.run, /bash scripts\/ci-browser-proofs\.sh "\$\{proofs\[@\]\}"/);
  for (const kind of ["passkey", "totp"]) {
    const gate = ci.jobs.ci.steps.find((step) => step.run?.includes(`check-shard-coverage.mjs ${kind}`));
    assert.ok(gate, `${kind}: shard evidence gate`);
    assert.equal(gate.if, "${{ needs.dev-https-proofs.result == 'success' }}");
    assert.match(gate.run, /"\$\{evidence_files\[@\]\}"/);
    assert.match(gate.run, /exit 1/, "empty shard evidence fails");
  }
}

test("preflight gates every large job and publishes the validated comparison base", () => {
  const ci = readWorkflow("ci");
  const preflight = ci.jobs.preflight;
  assert.ok(preflight, "comparison guards need preflight");
  assert.equal(preflight["timeout-minutes"], 5);
  assert.equal(preflight.outputs.base_sha, "${{ steps.comparison.outputs.base_sha }}");
  const comparison = preflight.steps.find((step) => step.id === "comparison");
  assert.equal(comparison.env.BASE_SHA, "${{ github.event_name == 'workflow_dispatch' && inputs.base_sha || github.event.pull_request.base.sha || github.event.before }}");
  assert.match(comparison.run, /git fetch --no-tags origin \+refs\/heads\/main:refs\/remotes\/origin\/main/);
  assert.match(comparison.run, /--dispatch-base "\$BASE_SHA" HEAD origin\/main/);
  assert.match(comparison.run, /check-migrations-append-only.sh --commits "\$base" HEAD/);
  assert.match(comparison.run, /check-released-schemas.sh --commits "\$base" HEAD/);
  for (const [name, job] of Object.entries(ci.jobs)) {
    if (["ci", "preflight"].includes(name)) continue;
    assert.deepEqual(job.needs, ["preflight"], `${name}: must wait for preflight`);
  }
  assert.equal(ci.jobs.ci["timeout-minutes"], 5);
  const docs = ci.jobs.contracts.steps.find((step) => step.env?.DOC_LINK_BASE);
  assert.equal(docs.env.DOC_LINK_BASE, "${{ needs.preflight.outputs.base_sha }}");
});

test("preflight derives the comparison base for each event", () => {
  checkPreflightBases(readWorkflow("ci"));
});

test("preflight rejects a renamed dispatch event condition", () => {
  const ci = readWorkflow("ci");
  const comparison = comparisonStep(ci);
  comparison.run = comparison.run.replace(
    '"$GITHUB_EVENT_NAME" = workflow_dispatch',
    '"$GITHUB_EVENT_NAME" = workflow_dispatch_renamed',
  );
  assert.throws(() => checkPreflightBases(ci), /dispatch event guard/);
});

test("CI job timeouts match their workloads and retain VN timeouts", () => {
  checkJobTimeouts(readWorkflow("ci"), readWorkflow("vn-infra"));
});

test("CI rejects changed job timeouts", () => {
  for (const name of Object.keys(readWorkflow("ci").jobs)) {
    if (name === "vn-infra") continue;
    const ci = readWorkflow("ci");
    ci.jobs[name]["timeout-minutes"] = 1;
    assert.throws(() => checkJobTimeouts(ci, readWorkflow("vn-infra")), /timeout-minutes/, name);
  }
  for (const name of Object.keys(readWorkflow("vn-infra").jobs)) {
    const vn = readWorkflow("vn-infra");
    for (const minutes of [1, 10, 20, 21]) {
      const expected = name === "crowdsec-integration" ? 20 : 10;
      if (minutes === expected) continue;
      vn.jobs[name]["timeout-minutes"] = minutes;
      assert.throws(() => checkJobTimeouts(readWorkflow("ci"), vn), /timeout-minutes/, `${name}/${minutes}`);
    }
  }
});

test("CI separates event concurrency and keeps every main push run", () => {
  const ci = readWorkflow("ci");
  assert.equal(ci.concurrency.group, "ci-${{ github.event_name }}-${{ github.event_name == 'push' && github.run_id || github.ref }}");
  assert.equal(ci.concurrency["cancel-in-progress"], "${{ github.event_name != 'push' }}");
});

test("browser artifacts contain only staged diagnostics and candidate metadata", () => {
  checkBrowserArtifacts(readWorkflow("ci"), readWorkflow("baselines"));
});

function checkTemplatePages(workflow, ci) {
  assert.deepEqual(workflow.on, { workflow_dispatch: null });
  assert.deepEqual(workflow.permissions, { contents: "read" });
  assert.deepEqual(Object.keys(workflow.jobs), ["template-pages"]);
  const job = workflow.jobs["template-pages"];
  assert.equal(job["timeout-minutes"], 30);
  assert.equal(job["continue-on-error"], undefined);
  assert.equal(job.permissions, undefined);
  assert.equal(job.steps.length, 4);
  assert.ok(job.steps.every((step) => !step["continue-on-error"]));
  assert.ok(job.steps.every((step) => !/\bgit\s+(commit|push)\b/.test(step.run ?? "")));
  assert.match(job.steps[0].uses, /^actions\/checkout@[a-f0-9]{40}$/);
  assert.equal(job.steps[2].run, "make web-template-pages");
  assert.equal(job.steps[2].env.TEMPLATE_PAGES_RUN_ID,
    "pages-${{ github.run_id }}-${{ github.run_attempt }}");
  const upload = job.steps[3];
  assert.equal(upload.if, "success()");
  assert.match(upload.uses, /^actions\/upload-artifact@[a-f0-9]{40}$/);
  assert.equal(upload.with.name, "candidate-template-pages");
  assert.deepEqual(upload.with.path.trim().split("\n"), [
    ".dev/web-template-pages/*/*/candidate/*.png",
    ".dev/web-template-pages/*/*/candidate/manifest.json",
    ".dev/web-template-pages/*/*/candidate/source-metadata.txt",
  ]);
  assert.equal(upload.with["retention-days"], 14);
  assert.equal(upload.with["if-no-files-found"], "error");
  assert.equal(upload.with["include-hidden-files"], true);
  const validate = ci.jobs.web.steps.find((step) =>
    step.run === "node scripts/validate-template-pages.mts --test");
  assert.ok(validate, "web checks must validate committed product images");
  assert.equal(validate["working-directory"], "apps/web");
  assert.equal(validate.if, "${{ !cancelled() }}");
}

test("product image workflow is manual, read-only, bounded and success-only", () => {
  checkTemplatePages(readWorkflow("template-pages"), readWorkflow("ci"));
  for (const mutate of [
    (w) => { w.on.push = {}; },
    (w) => { w.permissions.contents = "write"; },
    (w) => { w.jobs["template-pages"].permissions = { contents: "write" }; },
    (w) => { w.jobs["template-pages"]["continue-on-error"] = true; },
    (w) => { w.jobs["template-pages"].steps.push({ run: "git push" }); },
    (w) => { w.jobs["template-pages"].steps[1].run = "git commit -am unsafe"; },
    (w) => { w.jobs["template-pages"].steps[3].if = "always()"; },
    (w) => { w.jobs["template-pages"].steps[3].with.path = ".dev/**"; },
    (w) => { w.jobs["template-pages"].steps[3].with["retention-days"] = 90; },
  ]) {
    const workflow = readWorkflow("template-pages");
    mutate(workflow);
    assert.throws(() => checkTemplatePages(workflow, readWorkflow("ci")));
  }
  const ci = readWorkflow("ci");
  ci.jobs.web.steps = ci.jobs.web.steps.filter((step) =>
    step.run !== "node scripts/validate-template-pages.mts --test");
  assert.throws(() => checkTemplatePages(readWorkflow("template-pages"), ci));
});

test("contracts retains sanitized startup evidence when a check fails", () => {
  const steps = readWorkflow("ci").jobs.contracts.steps;
  const start = steps.find((step) => step.name === "Start the dev-https static suite shards");
  assert.equal(start.env.DEV_HTTPS_DIAGNOSTICS_ARTIFACT_DIR, "${{ runner.temp }}/dev-https-startup-diagnostics");
  const upload = steps.find((step) => step.with?.name === "dev-https-startup-diagnostics");
  assert.ok(upload, "contracts needs startup evidence after fixture cleanup");
  assert.equal(upload.if, "${{ failure() && !cancelled() }}");
  assert.equal(upload.with.path, "${{ runner.temp }}/dev-https-startup-diagnostics/*.txt");
  assert.equal(upload.with["retention-days"], 14);
  assert.equal(upload.with["if-no-files-found"], "ignore");
  assert.ok(!upload["continue-on-error"]);
});

test("browser artifact contract rejects unsafe uploads and masked failures", () => {
  const fixtures = [
    ["raw results", (ci) => { browserUpload(ci).with.path = ".dev/web-e2e-results/"; }],
    ["success-only upload", (ci) => { browserUpload(ci).if = "success()"; }],
    ["shared shard name", (ci) => { browserUpload(ci).with.name = "browser-diagnostics"; }],
    ["hidden files excluded", (ci) => { browserUpload(ci).with["include-hidden-files"] = false; }],
    ["masked browser failure", (ci) => { ci.jobs["web-e2e"]["continue-on-error"] = true; }],
    ["raw candidate upload", (_ci, baselines) => { baselines.jobs["web-e2e-update"].steps.at(-1).with.path = ".dev/web-e2e-results/"; }],
    ["failed candidate upload", (_ci, baselines) => { baselines.jobs["web-e2e-update"].steps.at(-1).if = "always()"; }],
    ["write permission", (ci) => { ci.jobs["web-e2e"].permissions = { contents: "write" }; }],
  ];
  for (const [name, mutate] of fixtures) {
    const ci = readWorkflow("ci");
    const baselines = readWorkflow("baselines");
    mutate(ci, baselines);
    assert.throws(() => checkBrowserArtifacts(ci, baselines), undefined, name);
  }
});

function browserUpload(ci) {
  const upload = ci.jobs["web-e2e"].steps.find((step) => step.with?.name === "browser-diagnostics-${{ matrix.shard }}");
  assert.ok(upload, "both browser shards need diagnostics");
  return upload;
}

function checkBrowserArtifacts(ci, baselines) {
  for (const [workflow, name] of [[ci, "web-e2e"], [baselines, "web-e2e-update"]]) {
    assert.deepEqual(workflow.permissions, { contents: "read" });
    assert.ok(!workflow.jobs[name].permissions || JSON.stringify(workflow.jobs[name].permissions) === JSON.stringify({ contents: "read" }));
    assert.ok(!workflow.jobs[name]["continue-on-error"]);
    assert.ok(workflow.jobs[name].steps.every((step) => !step["continue-on-error"]));
  }
  const upload = browserUpload(ci);
  assert.equal(upload.if, "always()");
  assert.equal(upload.with.path, ".dev/web-e2e-results/*/*/compare/diagnostics/");
  assert.equal(upload.with["include-hidden-files"], true);
  assert.equal(upload.with["retention-days"], 14);
  const candidate = baselines.jobs["web-e2e-update"].steps.at(-1);
  assert.equal(candidate.if, "success()");
  assert.equal(candidate.with.path, ".dev/web-e2e-results/*/*/update/candidate-baselines/");
  const updateDiagnostics = baselines.jobs["web-e2e-update"].steps.find((step) => step.with?.name === "browser-diagnostics-update");
  assert.ok(updateDiagnostics);
  assert.equal(updateDiagnostics.if, "always()");
  assert.equal(updateDiagnostics.with.path, ".dev/web-e2e-results/*/*/update/diagnostics/");
  assert.equal(updateDiagnostics.with["include-hidden-files"], true);
  assert.equal(updateDiagnostics.with["retention-days"], 14);
}

function comparisonStep(ci) {
  const step = ci.jobs.preflight.steps.find((step) => step.id === "comparison");
  assert.ok(step, "preflight needs a comparison step");
  return step;
}

function checkPreflightBases(ci) {
  const step = comparisonStep(ci);
  assert.equal(step.env.BASE_SHA, "${{ github.event_name == 'workflow_dispatch' && inputs.base_sha || github.event.pull_request.base.sha || github.event.before }}", "event-provided comparison bases");
  assert.match(
    step.run,
    /if \[ "\$GITHUB_EVENT_NAME" = workflow_dispatch \]; then\s+git fetch --no-tags origin \+refs\/heads\/main:refs\/remotes\/origin\/main\s+base=\$\(bash scripts\/check-migrations-append-only\.sh --dispatch-base "\$BASE_SHA" HEAD origin\/main\)\s+fi/,
    "dispatch event guard must enclose fetched-main merge-base validation",
  );
  const directory = mkdtempSync(join(tmpdir(), "preflight-contract-"));
  const trace = join(directory, "trace");
  const output = join(directory, "output");
  const stubCommands = `
    git() { printf 'git %s\\n' "$*" >> "$TRACE_FILE"; }
    bash() {
      printf 'bash %s\\n' "$*" >> "$TRACE_FILE"
      if [ "$2" = --dispatch-base ]; then printf '%s\\n' "$VALIDATED_BASE"; fi
    }
    export -f git bash
  `;
  try {
    for (const [event, eventBase, validatedBase] of [
      ["pull_request", "a".repeat(40), "a".repeat(40)],
      ["push", "b".repeat(40), "b".repeat(40)],
      ["workflow_dispatch", "c".repeat(40), "d".repeat(40)],
    ]) {
      rmSync(trace, { force: true });
      rmSync(output, { force: true });
      const result = spawnSync("bash", ["-e", "-o", "pipefail", "-c", stubCommands + step.run], {
        cwd: root,
        env: { ...process.env, GITHUB_EVENT_NAME: event, BASE_SHA: eventBase,
          VALIDATED_BASE: validatedBase, TRACE_FILE: trace, GITHUB_OUTPUT: output },
        encoding: "utf8",
        timeout: 5000,
      });
      assert.ifError(result.error);
      assert.equal(result.status, 0, `${event}: ${result.stderr}`);
      const expected = event === "workflow_dispatch" ? [
        "git fetch --no-tags origin +refs/heads/main:refs/remotes/origin/main",
        `bash scripts/check-migrations-append-only.sh --dispatch-base ${eventBase} HEAD origin/main`,
      ] : [];
      expected.push(
        `bash scripts/check-migrations-append-only.sh --commits ${validatedBase} HEAD`,
        `bash scripts/check-released-schemas.sh --commits ${validatedBase} HEAD`,
      );
      assert.deepEqual(readFileSync(trace, "utf8").trim().split("\n"), expected, `${event}: comparison commands`);
      assert.equal(readFileSync(output, "utf8"), `base_sha=${validatedBase}\n`, `${event}: published base`);
    }
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
}

test("VN keeps database migration in the PostgreSQL job", () => {
  const vn = readWorkflow("vn-infra");
  assert.deepEqual(Object.keys(vn.jobs).sort(), [
    "check", "crowdsec-integration", "crowdsec-postgresql", "crowdsec-privacy-notice",
  ]);
  const job = vn.jobs["crowdsec-postgresql"];
  const step = job.steps.find((entry) => entry.run === "sudo bash deploy/vn/tests/crowdsec-migrate-test.sh");
  assert.ok(step, "migration test step");
  assert.ok(!step.if && !step["continue-on-error"], "migration test must block");
});

function checkJobTimeouts(ci, vn) {
  const timeouts = {
    preflight: 5,
    contracts: 20,
    "macos-tooling": 5,
    server: 20,
    "server-integration": 20,
    "server-resumeapi": 20,
    web: 20,
    "web-e2e": 40,
    "dev-https-proofs": 40,
    ci: 5,
  };
  assert.deepEqual(Object.keys(ci.jobs).sort(), [...Object.keys(timeouts), "vn-infra"].sort(), "CI timeout job set");
  for (const [name, minutes] of Object.entries(timeouts)) {
    assert.equal(ci.jobs[name]["timeout-minutes"], minutes, `${name}: timeout-minutes`);
  }
  assert.equal(ci.jobs["vn-infra"]["timeout-minutes"], undefined, "reusable VN caller inherits timeouts");
  for (const [name, job] of Object.entries(vn.jobs)) {
    assert.equal(job["timeout-minutes"], name === "crowdsec-integration" ? 20 : 10, `VN ${name}: timeout-minutes`);
  }
}

function aggregateStep(ci) {
  const step = ci.jobs.ci.steps.find((step) => step.env?.NEEDS_JSON);
  assert.ok(step, "ci needs an aggregate result step");
  return step;
}

function checkRequiredGate(ci, vn, workflows = Object.fromEntries(
  workflowFiles.map((path) => [path, parseYaml(read(path), path).toJS()]),
)) {
  assert.ok(ci.jobs.ci, "aggregate job id must be ci");
  assert.equal(Object.hasOwn(ci.jobs.ci, "name") ? ci.jobs.ci.name : "ci", "ci", "exact ci check name");
  assert.ok(!Object.hasOwn(ci.jobs.ci.strategy ?? {}, "matrix"), "ci must have no matrix");
  for (const [path, workflow] of Object.entries({
    ...workflows,
    ".github/workflows/ci.yml": ci,
    ".github/workflows/vn-infra.yml": vn,
  })) {
    for (const [id, job] of Object.entries(workflow.jobs)) {
      const name = Object.hasOwn(job, "name") ? job.name : id;
      const prefix = typeof name === "string" ? name.split("${{", 1)[0].trim().toLowerCase() : "";
      assert.ok(
        typeof name === "string" && (!name.includes("${{") || (prefix.length > 0 && !"ci".startsWith(prefix))),
        `${path}/${id}: static check name prefix required to prove ci uniqueness`,
      );
      if (path === ".github/workflows/ci.yml" && id === "ci") continue;
      assert.notEqual(name, "ci", `${path}/${id}: aggregate must be the sole ci check producer`);
    }
  }
  assert.deepEqual(Object.keys(vn.on).sort(), ["workflow_call", "workflow_dispatch"], "VN triggers");
  assert.deepEqual(vn.permissions, { contents: "read" });
  assert.equal(vn.concurrency, undefined, "VN workflow must not cancel callers");
  assert.equal(ci.jobs["vn-infra"]?.uses, "./.github/workflows/vn-infra.yml", "VN workflow call");
  assert.equal(ci.jobs["vn-infra"].if, selectionCondition, "VN workflow must run under exact selection condition");
  assert.deepEqual(
    [...ci.jobs.ci.needs].sort(),
    Object.keys(ci.jobs).filter((job) => job !== "ci").sort(),
    "ci needs must equal the complete job set",
  );
  assert.equal(ci.jobs.ci.if, "always()", "ci must always run");
  const step = aggregateStep(ci);
  assert.equal(step.if, "always()", "aggregate must always run");
  assert.equal(step.env.NEEDS_JSON, "${{ toJSON(needs) }}");
  for (const job of Object.values(vn.jobs)) assert.equal(job.if, undefined, "VN internal jobs always run when called");
}

function checkAggregateResults(ci) {
  const step = aggregateStep(ci);
  const jobs = ci.jobs.ci.needs;
  const run = (needs, event) => {
    const result = spawnSync("bash", ["-e", "-o", "pipefail", "-c", step.run], {
      env: { ...process.env, NEEDS_JSON: JSON.stringify(needs), GITHUB_EVENT_NAME: event },
      encoding: "utf8", timeout: 5000,
    });
    assert.ifError(result.error);
    assert.ok(result.status === 0 || result.status === 1, result.stderr);
    return result.status;
  };
  for (const [context, event] of [
    ["main push", "push"], ["main dispatch", "workflow_dispatch"],
    ["branch push", "push"], ["branch dispatch", "workflow_dispatch"],
    ["unknown event", "unknown"],
    ...["same repository", "fork", "Dependabot", "train"].map((name) => [name, "pull_request"]),
  ]) {
    for (const selection of ["full", "prose", "unknown", "", undefined]) {
      const successful = Object.fromEntries(jobs.map((job) => [job, { result: "success" }]));
      successful.preflight.outputs = { selection };
      const valid = selection === "full" || (selection === "prose" && event === "pull_request");
      assert.equal(run(successful, event), valid ? 0 : 1, `${context}/${selection}: all success`);
      if (!valid) continue;
      const omitted = structuredClone(successful);
      for (const job of proseJobs) omitted[job].result = "skipped";
      assert.equal(run(omitted, event), selection === "prose" ? 0 : 1, `${context}: prose omissions`);
      for (const job of jobs) {
        for (const result of ["success", "skipped", "cancelled", "failure", "unknown", null, undefined]) {
          const needs = structuredClone(successful); needs[job].result = result;
          const allowed = result === "success" || (result === "skipped" && (
            (selection === "prose" && proseJobs.includes(job))));
          assert.equal(run(needs, event), allowed ? 0 : 1, `${context}/${selection}: ${job}=${result}`);
        }
        const missing = structuredClone(successful); delete missing[job];
        assert.equal(run(missing, event), 1, `${context}: missing ${job}`);
      }
      for (const value of [null, {}, { result: "success" }, { result: "success", outputs: null }, { result: "success", outputs: { selection: false } }]) {
        assert.equal(run({ ...successful, preflight: value }, event), 1, "missing output");
      }
      assert.equal(run({ ...successful, extra: { result: "success" } }, event), 1, "unknown job");
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

test("Dependabot covers weekly Actions, Go modules, and npm lockfile roots", () => {
  checkDependabot(parseYaml(read(".github/dependabot.yml"), ".github/dependabot.yml").toJS());
});

test("Dependabot contract rejects missing grouping and changed scope or cadence", () => {
  const valid = {
    version: 2,
    updates: [
      {
        "package-ecosystem": "github-actions",
        directory: "/",
        schedule: { interval: "weekly" },
        "open-pull-requests-limit": 2,
        groups: { "github-actions": { patterns: ["*"] } },
      },
      {
        "package-ecosystem": "gomod",
        directories: ["/apps/server", "/packages/schema/gen/go", "/deploy/observer"],
        schedule: { interval: "weekly" },
        "open-pull-requests-limit": 2,
        groups: { gomod: { patterns: ["*"], "update-types": ["minor", "patch"] } },
        ignore: [{ "dependency-name": "github.com/modelcontextprotocol/go-sdk" }],
      },
      {
        "package-ecosystem": "gomod",
        directories: ["/deploy/caddy/production/build"],
        schedule: { interval: "weekly" },
        "open-pull-requests-limit": 2,
        groups: { gomod: { patterns: ["*"], "update-types": ["minor", "patch"] } },
        ignore: [
          { "dependency-name": "github.com/caddyserver/caddy/v2" },
          { "dependency-name": "github.com/google/cel-go", versions: [">= 0.29.0"] },
        ],
      },
      {
        "package-ecosystem": "npm",
        directories: ["/", "/packages/schema"],
        schedule: { interval: "weekly" },
        "open-pull-requests-limit": 2,
        groups: { npm: { patterns: ["*"], "update-types": ["minor", "patch"] } },
      },
      {
        "package-ecosystem": "npm",
        directories: ["/apps/web"],
        schedule: { interval: "weekly" },
        "open-pull-requests-limit": 2,
        groups: { npm: { patterns: ["*"], "update-types": ["minor", "patch"] } },
        ignore: [
          { "dependency-name": "@playwright/test" },
          { "dependency-name": "playwright" },
          { "dependency-name": "playwright-core" },
          { "dependency-name": "typescript" },
          { "dependency-name": "pdfjs-dist" },
        ],
      },
      {
        "package-ecosystem": "npm",
        directories: ["/deploy/dev-https-browser"],
        schedule: { interval: "weekly" },
        "open-pull-requests-limit": 2,
        groups: { npm: { patterns: ["*"], "update-types": ["minor", "patch"] } },
        ignore: [
          { "dependency-name": "@playwright/test" },
          { "dependency-name": "playwright" },
          { "dependency-name": "playwright-core" },
          { "dependency-name": "@axe-core/playwright" },
        ],
      },
    ],
  };
  checkDependabot(valid);
  for (const mutate of [
    (config) => { config.version = 1; },
    (config) => { config.registries = {}; },
    (config) => { config.updates.pop(); },
    (config) => { config.updates.push({ "package-ecosystem": "docker" }); },
    (config) => { config.updates.push(structuredClone(config.updates[1])); },
  ]) {
    const config = structuredClone(valid);
    mutate(config);
    assert.throws(() => checkDependabot(config));
  }
  for (const [index, update] of valid.updates.entries()) {
    for (const mutate of [
      (entry) => { entry.groups = {}; },
      (entry) => { Object.values(entry.groups)[0].patterns = ["limited/*"]; },
      (entry) => { Object.values(entry.groups)[0]["exclude-patterns"] = ["*"]; },
      (entry) => { Object.values(entry.groups)[0]["update-types"] = ["major", "minor", "patch"]; },
      ...(index === 0 ? [] : [(entry) => { delete Object.values(entry.groups)[0]["update-types"]; }]),
      (entry) => { entry.schedule.interval = "daily"; },
      (entry) => { entry.schedule.day = "monday"; },
      (entry) => { entry["package-ecosystem"] = "docker"; },
      (entry) => { entry["open-pull-requests-limit"] = 10; },
      (entry) => { delete entry["open-pull-requests-limit"]; },
      (entry) => { if (entry.directories) entry.directories.pop(); else entry.directory = "/apps/web"; },
      (entry) => { entry.directories = ["/**"]; },
      (entry) => { entry.allow = [{ "dependency-type": "production" }]; },
      (entry) => { entry.ignore = [{ "dependency-name": "*" }]; },
    ]) {
      const config = structuredClone(valid);
      mutate(config.updates[index]);
      assert.throws(() => checkDependabot(config), `${update["package-ecosystem"]}/${index}`);
    }
    for (const [ignoreIndex] of (update.ignore ?? []).entries()) {
      for (const mutate of [
        (entry) => { entry.ignore.splice(ignoreIndex, 1); },
        (entry) => { entry.ignore[ignoreIndex]["update-types"] = ["version-update:semver-major"]; },
        (entry) => { entry.ignore[ignoreIndex].versions = ["1.0.0"]; },
        ...(update.ignore[ignoreIndex].versions ? [(entry) => { delete entry.ignore[ignoreIndex].versions; }] : []),
      ]) {
        const config = structuredClone(valid);
        mutate(config.updates[index]);
        assert.throws(() => checkDependabot(config), update.ignore[ignoreIndex]["dependency-name"]);
      }
    }
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
  const browserPackages = ["@playwright/test", "playwright", "playwright-core"]
    .map((name) => ({ "dependency-name": name }));
  const scopes = [
    ["github-actions", ["/"], []],
    ["gomod", ["/apps/server", "/packages/schema/gen/go", "/deploy/observer"], [
      { "dependency-name": "github.com/modelcontextprotocol/go-sdk" },
    ]],
    ["gomod", ["/deploy/caddy/production/build"], [
      { "dependency-name": "github.com/caddyserver/caddy/v2" },
      { "dependency-name": "github.com/google/cel-go", versions: [">= 0.29.0"] },
    ]],
    ["npm", ["/", "/packages/schema"], []],
    ["npm", ["/apps/web"], [...browserPackages, { "dependency-name": "typescript" }, { "dependency-name": "pdfjs-dist" }]],
    ["npm", ["/deploy/dev-https-browser"], [...browserPackages, { "dependency-name": "@axe-core/playwright" }]],
  ];
  const updates = scopes.map(([ecosystem, directories, ignores]) => ({
    "package-ecosystem": ecosystem,
    ...(ecosystem === "github-actions" ? { directory: "/" } : { directories }),
    schedule: { interval: "weekly" },
    "open-pull-requests-limit": 2,
    groups: {
      [ecosystem]: {
        patterns: ["*"],
        ...(ecosystem === "github-actions" ? {} : { "update-types": ["minor", "patch"] }),
      },
    },
    ...(ignores.length ? { ignore: ignores } : {}),
  }));
  assert.deepEqual(config, { version: 2, updates }, "Dependabot must keep weekly coverage, bounded groups, and manual pins");
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

test("macOS tooling installs missing prerequisites before publishing paths", () => {
  const job = readWorkflow("ci").jobs["macos-tooling"];
  const setup = job.steps.find((step) => step.run?.includes("brew --prefix"));
  assert.ok(setup, "macOS tooling needs prerequisite setup");
  const directory = mkdtempSync(join(tmpdir(), "macos-tooling-contract-"));
  const output = join(directory, "github-path");
  const fixture = `
    brew() {
      case "$*" in
        "install bash coreutils")
          mkdir -p "$BREW_FIXTURE/bash/bin" "$BREW_FIXTURE/coreutils/libexec/gnubin"
          ln -s "$BASH" "$BREW_FIXTURE/bash/bin/bash"
          ;;
        "--prefix bash" | "--prefix coreutils")
          printf '%s/%s\\n' "$BREW_FIXTURE" "$2"
          ;;
        "--prefix --installed bash" | "--prefix --installed coreutils")
          [ -d "$BREW_FIXTURE/$3" ] || return 1
          printf '%s/%s\\n' "$BREW_FIXTURE" "$3"
          ;;
        *) return 64 ;;
      esac
    }
    export -f brew
  `;
  try {
    const result = spawnSync("bash", ["-e", "-o", "pipefail", "-c", fixture + setup.run], {
      cwd: root,
      env: { ...process.env, BREW_FIXTURE: directory, GITHUB_PATH: output },
      encoding: "utf8",
      timeout: 5000,
    });
    assert.ifError(result.error);
    assert.equal(result.status, 0, result.stderr);
    const paths = readFileSync(output, "utf8").trim().split("\n");
    assert.deepEqual(paths, [join(directory, "bash/bin"), join(directory, "coreutils/libexec/gnubin")]);
    for (const path of paths) {
      assert.doesNotThrow(() => readdirSync(path), "published tool directory must exist");
    }
    const version = spawnSync(join(paths[0], "bash"), ["-c", "((BASH_VERSINFO[0] >= 4))"]);
    assert.equal(version.status, 0, "published Bash must support the wrapper");
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("server cross-compiles every package for Darwin arm64", () => {
  const step = readWorkflow("ci").jobs.server.steps.find((entry) =>
    entry.run === "GOOS=darwin GOARCH=arm64 go build ./...");
  assert.ok(step, "Darwin server build is required");
  assert.equal(step["working-directory"], "apps/server");
  assert.equal(step["continue-on-error"], undefined);
});

test("CodeQL scans main and weekly without joining the required gate", () => {
  const workflow = readWorkflow("codeql");
  assert.equal(workflow.name, "CodeQL");
  assert.deepEqual(workflow.on, {
    push: { branches: ["main"] },
    schedule: [{ cron: "0 3 * * 1" }],
    workflow_dispatch: null,
  });
  assert.deepEqual(workflow.permissions, { contents: "read" });
  assert.equal(workflow.concurrency.group, "codeql-${{ github.ref }}");
  assert.equal(workflow.concurrency["cancel-in-progress"], "${{ github.ref != 'refs/heads/main' }}");
  assert.deepEqual(Object.keys(workflow.jobs), ["analyze"]);
  const job = workflow.jobs.analyze;
  assert.equal(job.name, "CodeQL (${{ matrix.language }})");
  assert.deepEqual(job.permissions, { contents: "read", "security-events": "write" });
  assert.deepEqual(job.strategy.matrix.language, ["go", "javascript-typescript"]);
  const init = job.steps.find((step) => step.uses?.startsWith("github/codeql-action/init@"));
  const analyze = job.steps.find((step) => step.uses?.startsWith("github/codeql-action/analyze@"));
  const pin = "6f2ac81f5cd81fe351a197ba0b00d91d03cd2aee";
  assert.equal(init.uses, `github/codeql-action/init@${pin}`);
  assert.equal(analyze.uses, `github/codeql-action/analyze@${pin}`);
  assert.equal(init.with.languages, "${{ matrix.language }}");
  assert.equal(init.with["build-mode"], "${{ matrix.language == 'go' && 'manual' || 'none' }}");
  assert.equal(init.with.queries, "security-extended");
  assert.equal(init.with["config-file"], "./.github/codeql/codeql-config.yml");
  const setup = job.steps.find((step) => step.uses === "./.github/actions/setup-go");
  assert.equal(setup.if, "matrix.language == 'go'");
  assert.ok(!job.steps.some((step) => step.uses?.includes("save-go-cache")), "no cache saves");
  const build = job.steps.find((step) => step.name === "Build Go modules");
  assert.equal(build.if, "matrix.language == 'go'");
  for (const module of ["apps/server", "packages/schema/gen/go", "deploy/observer", "deploy/caddy/production/build"]) {
    assert.ok(build.run.includes(`cd ${module} && `), `build ${module}`);
  }
  assert.ok(!readWorkflow("ci").jobs.ci.needs.includes("analyze"));
  const config = parseYaml(read(".github/codeql/codeql-config.yml"), "CodeQL config").toJS();
  for (const path of ["**/node_modules/**", "**/vendor/**", "apps/web/app/api/generated/**", "packages/schema/gen/**"]) {
    assert.ok(config["paths-ignore"].includes(path), `exclude ${path}`);
  }
});

const renewalInstall = `set -euo pipefail
work=$(mktemp -d "$RUNNER_TEMP/vmonitor.XXXXXX")
chmod 700 "$work"
export GOBIN="$work/bin" GOPATH="$work/gopath" GOMODCACHE="$work/mod" GOCACHE="$work/cache"
export GOTOOLCHAIN=local GOSUMDB=sum.golang.org GOPRIVATE= GONOSUMDB= GOINSECURE=
mkdir -p "$GOBIN" "$GOPATH" "$GOMODCACHE" "$GOCACHE"
go install danny.vn/vngcloud/cmd/vngcloud@v0.89.0
identity=$(go version -m "$GOBIN/vngcloud" | awk '$1 == "mod" { print $2, $3, $4 }')
[ "$identity" = 'danny.vn/vngcloud v0.89.0 h1:o9YirnefFhEqpEJtalX41uHci6PR6P4AcotwPrWiqkA=' ]
printf '%s\\n' "$GOBIN" >> "$GITHUB_PATH"
`;

function checkRenewalWorkflow(workflow) {
  assert.deepEqual(workflow, {
    name: "Renew vMonitor quota",
    on: { workflow_dispatch: { inputs: { action: {
      description: "Check or renew the Synthetic quota", type: "choice", required: true,
      default: "renew", options: ["renew", "check"],
    } } } },
    permissions: {},
    concurrency: { group: "vmonitor-renew", "cancel-in-progress": false },
    jobs: { renew: {
      name: "Renew Synthetic quota",
      if: "github.repository == 'dannyota/aboutme' && github.ref == 'refs/heads/main' && github.event_name == 'workflow_dispatch'",
      "runs-on": "ubuntu-24.04", "timeout-minutes": 10,
      environment: "vmonitor-renew", permissions: { issues: "write" },
      steps: [
        { name: "Check out trusted main", uses: "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
          with: { repository: "dannyota/aboutme", ref: "refs/heads/main", "persist-credentials": false } },
        { name: "Log checkout commit", run: "git rev-parse HEAD" },
        { name: "Set up Go", uses: "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
          with: { "go-version": pins.golang, cache: false } },
        { name: "Install verified CLI", run: renewalInstall },
        { name: "Process quota", id: "renew", env: {
          ACTION: "${{ inputs.action }}", VNGCLOUD_CLIENT_ID: "${{ secrets.VNGCLOUD_CLIENT_ID }}",
          VNGCLOUD_CLIENT_SECRET: "${{ secrets.VNGCLOUD_CLIENT_SECRET }}",
          VNGCLOUD_REGION: "${{ vars.VNGCLOUD_REGION }}", VNGCLOUD_PROJECT_ID: "${{ secrets.VNGCLOUD_PROJECT_ID }}",
          GITHUB_TOKEN: "${{ secrets.GITHUB_TOKEN }}",
        }, run: 'bash scripts/vmonitor-renew.sh "$ACTION"' },
      ],
    } },
  }, "renewal workflow must preserve the trusted execution contract");
}

test("renewal workflow keeps its trusted dispatch and credential boundary", () => {
  checkRenewalWorkflow(readWorkflow("vmonitor-renew"));
  const mutations = [
    (w) => { w.on.schedule = [{ cron: "37 5,17 * * *" }]; },
    (w) => { w.on.workflow_dispatch.inputs.action.options.push("other"); },
    (w) => { w.jobs.renew.if = "always()"; },
    (w) => { w.permissions.contents = "write"; },
    (w) => { w.jobs.renew.permissions.actions = "write"; },
    (w) => { w.jobs.renew.environment = "other"; },
    (w) => { w.concurrency["cancel-in-progress"] = true; },
    (w) => { w.jobs.renew["timeout-minutes"] = 20; },
    (w) => { w.jobs.other = {}; },
    (w) => { w.jobs.renew.steps[0].with.ref = "${{ github.sha }}"; },
    (w) => { w.jobs.renew.steps[0].with["persist-credentials"] = true; },
    (w) => { w.jobs.renew.steps[0].uses = "./local-action"; },
    (w) => { w.jobs.renew.steps[2].with["go-version"] = "0.0.0"; },
    (w) => { w.jobs.renew.steps[2].with.cache = true; },
    (w) => { w.jobs.renew.steps[3].run = w.jobs.renew.steps[3].run.replace("go install", "vngcloud; go install"); },
    (w) => { w.jobs.renew.steps[3].run = w.jobs.renew.steps[3].run.replace('[ "$identity" =', 'true || [ "$identity" ='); },
    (w) => { w.jobs.renew.steps[3].run = w.jobs.renew.steps[3].run.replace("GOSUMDB=sum.golang.org", "GOSUMDB=off"); },
    (w) => { w.jobs.renew.steps[3].env = { VNGCLOUD_CLIENT_ID: "${{ secrets.VNGCLOUD_CLIENT_ID }}" }; },
    (w) => { w.jobs.renew.env = { VNGCLOUD_PROJECT_ID: "${{ secrets.VNGCLOUD_PROJECT_ID }}" }; },
    (w) => { w.jobs.renew.steps[3].env = { VNGCLOUD_PROJECT_ID: "${{ secrets.VNGCLOUD_PROJECT_ID }}" }; },
    (w) => { w.jobs.renew.steps[4].env.VNGCLOUD_PROFILE = "default"; },
    (w) => { w.jobs.renew.steps[4].env.VNGCLOUD_CLIENT_ID = "${{ secrets.OTHER }}"; },
    (w) => { w.jobs.renew.steps[4].env.VNGCLOUD_PROJECT_ID = "${{ vars.VNGCLOUD_PROJECT_ID }}"; },
    (w) => { w.jobs.renew.steps[4].env.VNGCLOUD_PROJECT_ID = "${{ secrets.OTHER }}"; },
    (w) => { w.jobs.renew.steps[4].env.VNGCLOUD_PROJECT_ID = "${{ secrets['VNGCLOUD_PROJECT_ID'] }}"; },
    (w) => { delete w.jobs.renew.steps[4].env.VNGCLOUD_PROJECT_ID; },
    (w) => { w.jobs.renew.steps[4].env.OTHER = w.jobs.renew.steps[4].env.VNGCLOUD_PROJECT_ID; },
    (w) => { w.jobs.renew.steps[4].run = 'echo "${{ secrets.VNGCLOUD_CLIENT_SECRET }}"'; },
    (w) => { w.jobs.renew.steps[4].run = 'echo "${{ secrets.VNGCLOUD_PROJECT_ID }}"'; },
    (w) => { w.jobs.renew.steps[4]["continue-on-error"] = true; },
    (w) => { w.jobs.renew.steps[4].run += " --debug"; },
    (w) => { w.jobs.renew.steps.push({ uses: "actions/upload-artifact@" + sha }); },
  ];
  for (const mutate of mutations) {
    const workflow = readWorkflow("vmonitor-renew");
    mutate(workflow);
    assert.throws(() => checkRenewalWorkflow(workflow));
  }
  assert.ok(!JSON.stringify(readWorkflow("ci")).includes("vmonitor-renew"));
});
