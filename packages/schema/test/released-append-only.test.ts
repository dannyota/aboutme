import { execFileSync, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, describe, expect, it } from "vitest";

// Released schemas are immutable except for this one approved documentation
// path rewrite. The exception is one-way and byte-exact, so it cannot permit a
// later v1 edit. See docs/design/data.md.
const APPROVED_OLD_V1_SHA256 =
  "2da37bb75297fefe32a920e3fae960100f0a99236ba4dc21ef25ae6b3f61f13f";
const APPROVED_NEW_V1_SHA256 =
  "879858284bc3cb4092d1d671466a9c620e27abf134aecedce070b6f21e4e5866";

const appendOnlyScript = (oldHash: string, newHash: string) =>
  `base="$BASE_SHA"
approved_old_v1_sha256="APPROVED_OLD_HASH"
approved_new_v1_sha256="APPROVED_NEW_HASH"
if ! diff=$(git diff --name-status "$base"...HEAD -- 'packages/schema/resume.v*.schema.json'); then
  echo "Could not compare released schemas with base." >&2
  exit 1
fi
changed=$(printf '%s\\n' "$diff" | grep -E '^(M|D|R|T)' || true)
approved_v1_change=$(printf 'M\\tpackages/schema/resume.v1.schema.json')
if [ "$changed" = "$approved_v1_change" ]; then
  base_v1_sha256=$(git show "$base:packages/schema/resume.v1.schema.json" | sha256sum | cut -d ' ' -f 1)
  head_v1_sha256=$(git show "HEAD:packages/schema/resume.v1.schema.json" | sha256sum | cut -d ' ' -f 1)
  if [ "$base_v1_sha256" = "$approved_old_v1_sha256" ] && [ "$head_v1_sha256" = "$approved_new_v1_sha256" ]; then
    changed=""
  fi
fi
if [ -n "$changed" ]; then
  echo "Released schemas are immutable; only a new version file may be added:"
  echo "$changed"
  exit 1
fi
`
    .replace("APPROVED_OLD_HASH", oldHash)
    .replace("APPROVED_NEW_HASH", newHash);

const APPEND_ONLY_SCRIPT = appendOnlyScript(
  APPROVED_OLD_V1_SHA256,
  APPROVED_NEW_V1_SHA256,
);

const OLD_SCHEMA_BYTES =
  '{"$id": "https://aboutme.vn/schema/resume/v1", "docs": "old"}\n';
const APPROVED_SCHEMA_BYTES =
  '{"$id": "https://aboutme.vn/schema/resume/v1", "docs": "current"}\n';
const UNAPPROVED_OLD_SCHEMA_BYTES =
  '{"$id": "https://aboutme.vn/schema/resume/v1", "docs": "unknown"}\n';
const sha256 = (bytes: string) =>
  createHash("sha256").update(bytes).digest("hex");
const TEST_OLD_V1_SHA256 = sha256(OLD_SCHEMA_BYTES);
const TEST_NEW_V1_SHA256 = sha256(APPROVED_SCHEMA_BYTES);
const TEST_APPEND_ONLY_SCRIPT = appendOnlyScript(
  TEST_OLD_V1_SHA256,
  TEST_NEW_V1_SHA256,
);

const localCiPath = "../../scripts/ci.sh";
const localCi = existsSync(localCiPath)
  ? readFileSync(localCiPath, "utf8")
  : "";
const localGateFunction =
  localCi.match(/^released_schema_append_only\(\) \{[\s\S]*?^\}/m)?.[0] ?? "";
const testLocalGateScript = localGateFunction
  .replaceAll(APPROVED_OLD_V1_SHA256, TEST_OLD_V1_SHA256)
  .replaceAll(APPROVED_NEW_V1_SHA256, TEST_NEW_V1_SHA256)
  .concat("\nreleased_schema_append_only\n");

const gateScripts: Array<{ name: string; script: string }> = [
  { name: "workflow guard contract", script: TEST_APPEND_ONLY_SCRIPT },
];
if (localGateFunction !== "") {
  gateScripts.push({
    name: "local scripts/ci.sh guard",
    script: testLocalGateScript,
  });
}

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
function baseRepo(options: { v1Bytes?: string; v2Bytes?: string } = {}): {
  repo: string;
  base: string;
} {
  const { v1Bytes = OLD_SCHEMA_BYTES, v2Bytes } = options;
  const repo = mkdtempSync(join(tmpdir(), "aboutme-released-append-only-"));
  repos.push(repo);
  git(repo, ["init", "-q", "-b", "main"]);
  git(repo, ["config", "user.email", "worker@aboutme.invalid"]);
  git(repo, ["config", "user.name", "append-only test"]);
  git(repo, ["config", "commit.gpgsign", "false"]);
  mkdirSync(join(repo, "packages", "schema"), { recursive: true });
  writeFileSync(join(repo, "packages/schema/resume.v1.schema.json"), v1Bytes);
  if (v2Bytes !== undefined) {
    writeFileSync(join(repo, "packages/schema/resume.v2.schema.json"), v2Bytes);
  }
  writeFileSync(join(repo, "packages/schema/resume.schema.json"), v1Bytes);
  git(repo, ["add", "-A"]);
  git(repo, ["commit", "-qm", "base: release v1"]);
  const base = git(repo, ["rev-parse", "HEAD"]).trim();
  git(repo, ["update-ref", "refs/remotes/origin/main", base]);
  return { repo, base };
}

function runGate(
  script: string,
  mutate: (repo: string) => void,
  baseOptions: { v1Bytes?: string; v2Bytes?: string } = {},
): {
  status: number;
  output: string;
} {
  const { repo, base } = baseRepo(baseOptions);
  mutate(repo);
  git(repo, ["add", "-A"]);
  git(repo, ["commit", "-qm", "candidate"]);

  const scriptPath = join(repo, "gate.sh");
  writeFileSync(scriptPath, script);
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

// Local make ci runs before integration, so its guard must compare the upstream
// base with the index and worktree rather than assuming candidate bytes are in
// HEAD. The workflow guard remains commit-based and uses runGate above.
function runLocalWorktreeGate(
  mutate: (repo: string) => void,
  baseOptions: { v1Bytes?: string; v2Bytes?: string } = {},
): { status: number; output: string } {
  const { repo } = baseRepo(baseOptions);
  mutate(repo);

  const scriptPath = join(repo, "gate.sh");
  writeFileSync(scriptPath, testLocalGateScript);
  const result = spawnSync("bash", [scriptPath], {
    cwd: repo,
    encoding: "utf8",
    env: process.env,
  });
  return {
    status: result.status ?? -1,
    output: `${result.stdout}${result.stderr}`,
  };
}

describe.each(gateScripts)("$name", ({ script }) => {
  it("allows only the approved old-to-new v1 byte transition", () => {
    const { status, output } = runGate(script, (repo) => {
      writeFileSync(
        join(repo, "packages/schema/resume.v1.schema.json"),
        APPROVED_SCHEMA_BYTES,
      );
    });
    expect(status).toBe(0);
    expect(output).toBe("");
  });

  it("rejects the approved new v1 bytes when the base bytes are not approved", () => {
    const { status, output } = runGate(
      script,
      (repo) => {
        writeFileSync(
          join(repo, "packages/schema/resume.v1.schema.json"),
          APPROVED_SCHEMA_BYTES,
        );
      },
      { v1Bytes: UNAPPROVED_OLD_SCHEMA_BYTES },
    );
    expect(status).toBe(1);
    expect(output).toContain("Released schemas are immutable");
  });

  it("rejects a MODIFIED released schema", () => {
    const { status, output } = runGate(script, (repo) => {
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
    const { status, output } = runGate(script, (repo) => {
      rmSync(join(repo, "packages/schema/resume.v1.schema.json"));
    });
    expect(status).toBe(1);
    expect(output).toMatch(/^D\s+packages\/schema\/resume\.v1\.schema\.json$/m);
  });

  it("rejects a RENAMED released schema", () => {
    const { status, output } = runGate(script, (repo) => {
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

  it("rejects a TYPE-CHANGED released schema", () => {
    const { status, output } = runGate(script, (repo) => {
      const released = join(repo, "packages/schema/resume.v1.schema.json");
      rmSync(released);
      symlinkSync("resume.schema.json", released);
    });
    expect(status).toBe(1);
    expect(output).toMatch(/^T\s+packages\/schema\/resume\.v1\.schema\.json$/m);
  });

  it("rejects the approved v1 transition with another forbidden released-schema change", () => {
    const { status } = runGate(
      script,
      (repo) => {
        writeFileSync(
          join(repo, "packages/schema/resume.v1.schema.json"),
          APPROVED_SCHEMA_BYTES,
        );
        writeFileSync(
          join(repo, "packages/schema/resume.v2.schema.json"),
          '{"tampered": 2}\n',
        );
      },
      { v2Bytes: '{"released": 2}\n' },
    );
    expect(status).toBe(1);
  });

  it("rejects a released schema modified alongside a legitimately added one", () => {
    const { status } = runGate(script, (repo) => {
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
    const { status, output } = runGate(script, (repo) => {
      writeFileSync(
        join(repo, "packages/schema/resume.v2.schema.json"),
        '{"released": 2}\n',
      );
    });
    expect(status).toBe(0);
    expect(output).toBe("");
  });

  it("allows the approved v1 transition alongside an added version", () => {
    const { status, output } = runGate(script, (repo) => {
      writeFileSync(
        join(repo, "packages/schema/resume.v1.schema.json"),
        APPROVED_SCHEMA_BYTES,
      );
      writeFileSync(
        join(repo, "packages/schema/resume.v2.schema.json"),
        '{"released": 2}\n',
      );
    });
    expect(status).toBe(0);
    expect(output).toBe("");
  });

  it("allows ordinary edits to the working resume.schema.json", () => {
    const { status } = runGate(script, (repo) => {
      writeFileSync(
        join(repo, "packages/schema/resume.schema.json"),
        '{"working": true}\n',
      );
    });
    expect(status).toBe(0);
  });
});

describe("local guard against uncommitted released-schema changes", () => {
  it.skipIf(localGateFunction === "")(
    "allows the exact approved transition in the unstaged worktree",
    () => {
      const { status, output } = runLocalWorktreeGate((repo) => {
        writeFileSync(
          join(repo, "packages/schema/resume.v1.schema.json"),
          APPROVED_SCHEMA_BYTES,
        );
      });
      expect(status).toBe(0);
      expect(output).toBe("");
    },
  );

  it.skipIf(localGateFunction === "")(
    "rejects an arbitrary unstaged modification",
    () => {
      const { status, output } = runLocalWorktreeGate((repo) => {
        writeFileSync(
          join(repo, "packages/schema/resume.v1.schema.json"),
          '{"tampered": true}\n',
        );
      });
      expect(status).toBe(1);
      expect(output).toMatch(
        /^M\s+packages\/schema\/resume\.v1\.schema\.json$/m,
      );
    },
  );

  it.skipIf(localGateFunction === "")("rejects an unstaged deletion", () => {
    const { status, output } = runLocalWorktreeGate((repo) => {
      rmSync(join(repo, "packages/schema/resume.v1.schema.json"));
    });
    expect(status).toBe(1);
    expect(output).toMatch(/^D\s+packages\/schema\/resume\.v1\.schema\.json$/m);
  });

  it.skipIf(localGateFunction === "")("rejects an unstaged type change", () => {
    const { status, output } = runLocalWorktreeGate((repo) => {
      const released = join(repo, "packages/schema/resume.v1.schema.json");
      rmSync(released);
      symlinkSync("resume.schema.json", released);
    });
    expect(status).toBe(1);
    expect(output).toMatch(/^T\s+packages\/schema\/resume\.v1\.schema\.json$/m);
  });

  it.skipIf(localGateFunction === "")(
    "rejects approved new bytes over an unapproved worktree base",
    () => {
      const { status, output } = runLocalWorktreeGate(
        (repo) => {
          writeFileSync(
            join(repo, "packages/schema/resume.v1.schema.json"),
            APPROVED_SCHEMA_BYTES,
          );
        },
        { v1Bytes: UNAPPROVED_OLD_SCHEMA_BYTES },
      );
      expect(status).toBe(1);
      expect(output).toContain("Released schemas are immutable");
    },
  );

  it.skipIf(localGateFunction === "")(
    "rejects an arbitrary staged modification while HEAD remains at the base",
    () => {
      const { status, output } = runLocalWorktreeGate((repo) => {
        const released = "packages/schema/resume.v1.schema.json";
        writeFileSync(join(repo, released), '{"tampered": "staged"}\n');
        git(repo, ["add", "--", released]);
      });
      expect(status).toBe(1);
      expect(output).toMatch(
        /^M\s+packages\/schema\/resume\.v1\.schema\.json$/m,
      );
    },
  );

  it.skipIf(localGateFunction === "")(
    "rejects a staged modification hidden by restoring the worktree to base bytes",
    () => {
      const { status, output } = runLocalWorktreeGate((repo) => {
        const released = "packages/schema/resume.v1.schema.json";
        writeFileSync(join(repo, released), '{"tampered": "staged"}\n');
        git(repo, ["add", "--", released]);
        writeFileSync(join(repo, released), OLD_SCHEMA_BYTES);
      });
      expect(status).toBe(1);
      expect(output).toContain("packages/schema/resume.v1.schema.json");
    },
  );
});

describe("installed released-schema guards", () => {
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

  it.skipIf(localCi === "")(
    "pins the same approved transition in scripts/ci.sh",
    () => {
      expect(localGateFunction).toContain(APPROVED_OLD_V1_SHA256);
      expect(localGateFunction).toContain(APPROVED_NEW_V1_SHA256);
    },
  );
});
