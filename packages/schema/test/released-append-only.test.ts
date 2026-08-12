import { execFileSync, spawnSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, describe, expect, it } from "vitest";

// Released schemas are immutable; only a new version file may be added. This
// suite runs the workflow's exact shell guard against isolated repositories and
// pins the workflow copy when that job exists. See docs/design/data.md.
const APPEND_ONLY_SCRIPT = `base="$BASE_SHA"
changed=$(git diff --name-status "$base"...HEAD -- 'packages/schema/resume.v*.schema.json' \\
  | grep -E '^(M|D|R)' || true)
if [ -n "$changed" ]; then
  echo "Released schemas are immutable; only a new version file may be added:"
  echo "$changed"
  exit 1
fi
`;

const SCHEMA_BYTES = '{"$id": "https://aboutme.vn/schema/resume/v1"}\n';

const repos: string[] = [];

afterAll(() => {
  for (const repo of repos) {
    rmSync(repo, { recursive: true, force: true });
  }
});

function git(repo: string, args: string[]): string {
  return execFileSync("git", args, { cwd: repo, encoding: "utf8" });
}

// Build a repository with released v1 and working schema files, then return the
// base commit used by the append-only comparison.
function baseRepo(): { repo: string; base: string } {
  const repo = mkdtempSync(join(tmpdir(), "aboutme-released-append-only-"));
  repos.push(repo);
  git(repo, ["init", "-q", "-b", "main"]);
  git(repo, ["config", "user.email", "worker@aboutme.invalid"]);
  git(repo, ["config", "user.name", "append-only test"]);
  git(repo, ["config", "commit.gpgsign", "false"]);
  mkdirSync(join(repo, "packages", "schema"), { recursive: true });
  writeFileSync(
    join(repo, "packages/schema/resume.v1.schema.json"),
    SCHEMA_BYTES,
  );
  writeFileSync(join(repo, "packages/schema/resume.schema.json"), SCHEMA_BYTES);
  git(repo, ["add", "-A"]);
  git(repo, ["commit", "-qm", "base: release v1"]);
  return { repo, base: git(repo, ["rev-parse", "HEAD"]).trim() };
}

function runGate(mutate: (repo: string) => void): {
  status: number;
  output: string;
} {
  const { repo, base } = baseRepo();
  mutate(repo);
  git(repo, ["add", "-A"]);
  git(repo, ["commit", "-qm", "candidate"]);

  const scriptPath = join(repo, "gate.sh");
  writeFileSync(scriptPath, APPEND_ONLY_SCRIPT);
  const result = spawnSync("bash", [scriptPath], {
    cwd: repo,
    encoding: "utf8",
    env: { ...process.env, BASE_SHA: base },
  });
  return {
    status: result.status ?? -1,
    output: `${result.stdout}${result.stderr}`,
  };
}

describe("released-schema append-only gate", () => {
  it("rejects a MODIFIED released schema", () => {
    const { status, output } = runGate((repo) => {
      writeFileSync(
        join(repo, "packages/schema/resume.v1.schema.json"),
        '{"tampered": true}\n',
      );
    });
    expect(status).toBe(1);
    expect(output).toContain("Released schemas are immutable");
    expect(output).toMatch(/^M\s+packages\/schema\/resume\.v1\.schema\.json$/m);
  });

  it("rejects a DELETED released schema", () => {
    const { status, output } = runGate((repo) => {
      rmSync(join(repo, "packages/schema/resume.v1.schema.json"));
    });
    expect(status).toBe(1);
    expect(output).toMatch(/^D\s+packages\/schema\/resume\.v1\.schema\.json$/m);
  });

  it("rejects a RENAMED released schema", () => {
    const { status, output } = runGate((repo) => {
      git(repo, [
        "mv",
        "packages/schema/resume.v1.schema.json",
        "packages/schema/resume.v3.schema.json",
      ]);
    });
    expect(status).toBe(1);
    // Rename detection is on by default, so this surfaces as R<score>; if it
    // ever were not, the same change surfaces as D plus A and is still
    // rejected on the D. Either name-status is a failure, which is the point.
    expect(output).toMatch(
      /^[RD]\d*\s+packages\/schema\/resume\.v1\.schema\.json/m,
    );
  });

  it("rejects a released schema modified alongside a legitimately added one", () => {
    const { status } = runGate((repo) => {
      writeFileSync(
        join(repo, "packages/schema/resume.v2.schema.json"),
        '{"released": 2}\n',
      );
      writeFileSync(
        join(repo, "packages/schema/resume.v1.schema.json"),
        '{"tampered": true}\n',
      );
    });
    expect(status).toBe(1);
  });

  it("allows a newly ADDED released schema version", () => {
    const { status, output } = runGate((repo) => {
      writeFileSync(
        join(repo, "packages/schema/resume.v2.schema.json"),
        '{"released": 2}\n',
      );
    });
    expect(status).toBe(0);
    expect(output).toBe("");
  });

  it("allows ordinary edits to the working resume.schema.json", () => {
    const { status } = runGate((repo) => {
      writeFileSync(
        join(repo, "packages/schema/resume.schema.json"),
        '{"working": true}\n',
      );
    });
    expect(status).toBe(0);
  });

  // Keep package tests runnable in a checkout without the workflow. When the
  // job exists, its script must match the tested guard byte for byte.
  const ciWorkflowPath = "../../.github/workflows/ci.yml";
  const ciWorkflow = existsSync(ciWorkflowPath)
    ? readFileSync(ciWorkflowPath, "utf8")
    : "";
  const jobPresent = ciWorkflow.includes("released-schema-append-only:");

  it.skipIf(!jobPresent)(
    "keeps ci.yml's job script identical to the script proven here",
    () => {
      const indented = APPEND_ONLY_SCRIPT.split("\n")
        .map((line) => (line === "" ? line : `          ${line}`))
        .join("\n");
      expect(ciWorkflow).toContain(indented);
    },
  );
});
