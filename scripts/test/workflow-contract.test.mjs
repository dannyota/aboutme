import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
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
const forkCondition = "${{ github.event_name == 'pull_request' && github.event.pull_request.head.repo.full_name != github.repository }}";

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
    ["shared scan ignored", (_manual, _release, action) => { action.runs.steps.find((step) => step.name === "Scan for fixable HIGH/CRITICAL vulnerabilities")["continue-on-error"] = true; }, /must block/],
    ["shared scan conditional", (_manual, _release, action) => { action.runs.steps.find((step) => step.name === "Scan for fixable HIGH/CRITICAL vulnerabilities").if = "false"; }, /unconditional/],
    ["shared scan omitted", (_manual, _release, action) => { action.runs.steps.find((step) => step.name === "Scan for fixable HIGH/CRITICAL vulnerabilities").run = "true"; }, /scan command/],
    ["shared command probe ignored", (_manual, _release, action) => { action.runs.steps.find((step) => step.name === "Probe host operation dispatch")["continue-on-error"] = true; }, /must block/],
    ["shared command probe skipped", (_manual, _release, action) => { action.runs.steps.find((step) => step.name === "Probe host operation dispatch").if = "false"; }, /server-only check/],
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
  assert.deepEqual(action.runs.steps.map((step) => step.name), ["Build", "Smoke", "Probe host operation dispatch", "Install Trivy", "Scan for fixable HIGH/CRITICAL vulnerabilities", "Upload Trivy report"], "shared build-only steps");
  for (const step of action.runs.steps) {
    assert.equal(step["continue-on-error"], undefined, `${step.name}: must block on failure`);
    if (step.name === "Probe host operation dispatch") {
      assert.equal(step.if, "env.NAME == 'server'", `${step.name}: server-only check`);
    } else {
      assert.equal(step.if, step.name === "Upload Trivy report" ? "always()" : undefined, `${step.name}: unconditional check`);
    }
    if (step.run) assert.equal(step.shell, "bash");
  }
  const scan = action.runs.steps.find((step) => step.name === "Scan for fixable HIGH/CRITICAL vulnerabilities");
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
  const start = steps.findIndex((step) => step.name === "Start setup timing");
  const setup = steps.findIndex((step) => step.id === "setup");
  const record = steps.findIndex((step) => step.name === "Record setup timing");
  assert.ok(start >= 0 && start < setup && setup < record, "setup timing brackets setup");
  assert.equal(steps[record].if, "always()");
  assert.match(steps[start].run, /date \+%s/);
  assert.match(steps[record].run, /date \+%s/);
  assert.equal(steps[record].env.SETUP_RESULT, "${{ steps.setup.outcome }}");
  const upload = steps.find((step) => step.name === "Upload browser proof timings");
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
  const job = ci.jobs["dev-https-proofs"];
  assert.equal(job.if, undefined, "browser groups run on every trigger");
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
      const expected = name === "crowdsec-integration" || name === "crowdsec-cutover" ? 20 : 10;
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

function checkRehearsalArtifact(vn) {
  const job = vn.jobs["crowdsec-rehearsal-fixtures"];
  assert.ok(job, "rehearsal artifact job");
  assert.equal(job["timeout-minutes"], 10, "rehearsal timeout");
  assert.ok(!job.if && !job["continue-on-error"], "rehearsal must block");
  assert.ok(job.steps.every((step) => !step.if && !step["continue-on-error"]), "rehearsal steps must block");
  const reader = job.steps.find((step) => step["working-directory"] === "deploy/vn/rehearsal/allowance-adapter");
  assert.equal(reader?.env?.GOWORK, "off", "rehearsal independent module");
  assert.match(reader.run, /go test -mod=readonly \.\/\.\.\./, "rehearsal reader tests");
  assert.match(reader.run, /go build -mod=readonly/, "rehearsal reader build");
  assert.match(reader.run, /sha256sum render-budget allowance-adapter fixtures\/\*/, "rehearsal artifact hashes");
  const upload = job.steps.find((step) => step.with?.name === "crowdsec-rehearsal-fixtures");
  assert.equal(upload?.with?.["if-no-files-found"], "error", "rehearsal artifact required");
  assert.equal(upload?.with?.["retention-days"], 7, "rehearsal artifact retention");
}

test("rehearsal exports the corpus and an independent allowance reader", () => {
  checkRehearsalArtifact(readWorkflow("vn-infra"));
  for (const mutate of [
    (job) => { job["continue-on-error"] = true; },
    (job) => { job.steps.find((step) => step.env?.GOWORK).env.GOWORK = "auto"; },
    (job) => { job.steps.find((step) => step.env?.GOWORK).run = "go build ."; },
    (job) => {
      const reader = job.steps.find((step) => step.env?.GOWORK);
      reader.run = reader.run.replace("go test -mod=readonly ./...", "go test -mod=readonly ...");
    },
    (job) => { job.steps.find((step) => step.with?.name === "crowdsec-rehearsal-fixtures").with["if-no-files-found"] = "warn"; },
  ]) {
    const vn = readWorkflow("vn-infra");
    mutate(vn.jobs["crowdsec-rehearsal-fixtures"]);
    assert.throws(() => checkRehearsalArtifact(vn), /rehearsal/);
  }
});

function checkJobTimeouts(ci, vn) {
  const timeouts = {
    preflight: 5,
    contracts: 20,
    server: 20,
    "server-integration": 20,
    "server-resumeapi": 20,
    web: 20,
    "web-e2e": 40,
    "dev-https-proofs": 40,
    "semgrep-code": 20,
    "semgrep-supply-chain": 20,
    ci: 5,
  };
  assert.deepEqual(Object.keys(ci.jobs).sort(), [...Object.keys(timeouts), "vn-infra"].sort(), "CI timeout job set");
  for (const [name, minutes] of Object.entries(timeouts)) {
    assert.equal(ci.jobs[name]["timeout-minutes"], minutes, `${name}: timeout-minutes`);
  }
  assert.equal(ci.jobs["vn-infra"]["timeout-minutes"], undefined, "reusable VN caller inherits timeouts");
  for (const [name, job] of Object.entries(vn.jobs)) {
    assert.equal(job["timeout-minutes"], name === "crowdsec-integration" || name === "crowdsec-cutover" ? 20 : 10, `VN ${name}: timeout-minutes`);
  }
}

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
