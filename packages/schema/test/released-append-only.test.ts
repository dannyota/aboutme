import { execFileSync, spawnSync } from "node:child_process";
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

const APPEND_ONLY_SCRIPT = readFileSync(
  "../../scripts/check-released-schemas.sh",
  "utf8",
);
const TEST_APPEND_ONLY_SCRIPT = 'set -- --commits "$BASE_SHA" HEAD\n'.concat(
  APPEND_ONLY_SCRIPT,
);

const OLD_SCHEMA_BYTES =
  '{"$id": "https://aboutme.vn/schema/resume/v1", "docs": "old"}\n';
const CHANGED_SCHEMA_BYTES =
  '{"$id": "https://aboutme.vn/schema/resume/v1", "docs": "current"}\n';
const OTHER_BASE_SCHEMA_BYTES =
  '{"$id": "https://aboutme.vn/schema/resume/v1", "docs": "unknown"}\n';
const DIVERGED_BASE_SCHEMA_BYTES =
  '{"$id": "https://aboutme.vn/schema/resume/v1", "docs": "diverged-base"}\n';
const localCiPath = "../../scripts/ci.sh";
const localCi = existsSync(localCiPath)
  ? readFileSync(localCiPath, "utf8")
  : "";
const localGateFunction =
  localCi.match(/^released_schema_append_only\(\) \{[\s\S]*?^\}/m)?.[0] ?? "";
const testLocalGateScript = localGateFunction.concat(
  "\nreleased_schema_append_only\n",
);

const gateScripts: Array<{ name: string; script: string }> = [
  { name: "hosted released-schema script", script: TEST_APPEND_ONLY_SCRIPT },
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
type BaseOptions = {
  v1Bytes?: string;
  v2Bytes?: string;
  marked?: boolean;
  oldMarker?: boolean;
};

function baseRepo(options: BaseOptions = {}): {
  repo: string;
  base: string;
} {
  const {
    v1Bytes = OLD_SCHEMA_BYTES,
    v2Bytes,
    marked = true,
    oldMarker = false,
  } = options;
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
  if (oldMarker) {
    mkdirSync(join(repo, "apps/server/migrations"), { recursive: true });
    writeFileSync(
      join(repo, "apps/server/migrations/.uat-baseline"),
      "obsolete\n",
    );
  }
  if (marked)
    writeFileSync(join(repo, ".v1-baseline"), "Application v1 baseline\n");
  git(repo, ["add", "-A"]);
  git(repo, ["commit", "-qm", "base: release v1"]);
  const base = git(repo, ["rev-parse", "HEAD"]).trim();
  git(repo, ["update-ref", "refs/remotes/origin/main", base]);
  return { repo, base };
}

function runGate(
  script: string,
  mutate: (repo: string) => void,
  baseOptions: BaseOptions = {},
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

function runDivergedHostedGate(
  script: string,
  mutateBase: (repo: string) => void,
): {
  status: number;
  output: string;
  tripleDotDiff: string;
  directDiff: string;
} {
  const { repo } = baseRepo();
  const mergeBase = git(repo, ["rev-parse", "HEAD"]).trim();

  git(repo, ["switch", "-qc", "remote-base"]);
  mutateBase(repo);
  git(repo, ["add", "-A"]);
  git(repo, ["commit", "-qm", "remote base advances"]);
  const base = git(repo, ["rev-parse", "HEAD"]).trim();

  git(repo, ["switch", "-qc", "force-pushed-head", mergeBase]);
  writeFileSync(join(repo, "candidate.txt"), "force-pushed history\n");
  git(repo, ["add", "-A"]);
  git(repo, ["commit", "-qm", "replacement head"]);

  const pathspec = "packages/schema/resume.v*.schema.json";
  const tripleDotDiff = git(repo, [
    "diff",
    "--name-status",
    `${base}...HEAD`,
    "--",
    pathspec,
  ]);
  const directDiff = git(repo, [
    "diff",
    "--name-status",
    base,
    "HEAD",
    "--",
    pathspec,
  ]);

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
    tripleDotDiff,
    directDiff,
  };
}

function runHostedGateWithInvalidRef(
  script: string,
  invalidRef: "base" | "head",
): { status: number; output: string } {
  const { repo, base } = baseRepo({ marked: false });
  if (invalidRef === "head") {
    git(repo, ["update-ref", "-d", "refs/heads/main"]);
  }

  const scriptPath = join(repo, "gate.sh");
  writeFileSync(scriptPath, script);
  const result = spawnSync("bash", [scriptPath], {
    cwd: repo,
    encoding: "utf8",
    env: {
      ...process.env,
      BASE_SHA: invalidRef === "base" ? "refs/heads/missing-base" : base,
    },
  });
  return {
    status: result.status ?? -1,
    output: `${result.stdout}${result.stderr}`,
  };
}

function runHostedGateWithNonCommitBase(script: string): {
  status: number;
  output: string;
} {
  const { repo } = baseRepo({ marked: false });
  const tree = git(repo, ["rev-parse", "HEAD^{tree}"]).trim();
  const scriptPath = join(repo, "gate.sh");
  writeFileSync(scriptPath, script);
  const result = spawnSync("bash", [scriptPath], {
    cwd: repo,
    encoding: "utf8",
    env: { ...process.env, BASE_SHA: tree },
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
  baseOptions: BaseOptions = {},
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
  it.each(["modified", "deleted", "renamed", "type-changed"])(
    "allows a pre-v1 %s despite an obsolete UAT marker",
    (mode) => {
      const { status } = runGate(
        script,
        (repo) => {
          const path = join(repo, "packages/schema/resume.v1.schema.json");
          if (mode === "modified") writeFileSync(path, "changed\n");
          if (mode === "deleted") rmSync(path);
          if (mode === "renamed")
            git(repo, [
              "mv",
              "packages/schema/resume.v1.schema.json",
              "packages/schema/resume.v3.schema.json",
            ]);
          if (mode === "type-changed") {
            rmSync(path);
            symlinkSync("resume.schema.json", path);
          }
        },
        { marked: false, oldMarker: true },
      );
      expect(status).toBe(0);
    },
  );

  it("allows schema edits while establishing the first v1 baseline", () => {
    const { status } = runGate(
      script,
      (repo) => {
        writeFileSync(join(repo, ".v1-baseline"), "Application v1 baseline\n");
        writeFileSync(
          join(repo, "packages/schema/resume.v1.schema.json"),
          "changed\n",
        );
      },
      { marked: false },
    );
    expect(status).toBe(0);
  });

  it.each(["deleted", "modified"])("rejects a %s v1 marker", (mode) => {
    const { status } = runGate(script, (repo) => {
      const marker = join(repo, ".v1-baseline");
      if (mode === "deleted") rmSync(marker);
      else writeFileSync(marker, "weakened\n");
    });
    expect(status).toBe(1);
  });

  it("rejects the changed v1 bytes over another base", () => {
    const { status, output } = runGate(
      script,
      (repo) => {
        writeFileSync(
          join(repo, "packages/schema/resume.v1.schema.json"),
          CHANGED_SCHEMA_BYTES,
        );
      },
      { v1Bytes: OTHER_BASE_SCHEMA_BYTES },
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

  it("rejects the v1 edit with another released-schema change", () => {
    const { status } = runGate(
      script,
      (repo) => {
        writeFileSync(
          join(repo, "packages/schema/resume.v1.schema.json"),
          CHANGED_SCHEMA_BYTES,
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

describe("hosted guard across rewritten history", () => {
  it("rejects marker tampering across non-ancestor history", () => {
    const result = runDivergedHostedGate(TEST_APPEND_ONLY_SCRIPT, (repo) => {
      writeFileSync(join(repo, ".v1-baseline"), "different baseline\n");
    });
    expect(result.status).toBe(1);
    expect(result.output).toContain("baseline is immutable");
  });

  it("rejects a released schema reverted by a non-ancestor head", () => {
    const result = runDivergedHostedGate(TEST_APPEND_ONLY_SCRIPT, (repo) => {
      writeFileSync(
        join(repo, "packages/schema/resume.v1.schema.json"),
        DIVERGED_BASE_SCHEMA_BYTES,
      );
    });

    expect(result.tripleDotDiff).toBe("");
    expect(result.directDiff).toMatch(
      /^M\s+packages\/schema\/resume\.v1\.schema\.json$/m,
    );
    expect(result.status).toBe(1);
    expect(result.output).toContain("Released schemas are immutable");
  });

  it("rejects a released schema deleted by a non-ancestor head", () => {
    const result = runDivergedHostedGate(TEST_APPEND_ONLY_SCRIPT, (repo) => {
      writeFileSync(
        join(repo, "packages/schema/resume.v2.schema.json"),
        '{"released": 2}\n',
      );
    });

    expect(result.tripleDotDiff).toBe("");
    expect(result.directDiff).toMatch(
      /^D\s+packages\/schema\/resume\.v2\.schema\.json$/m,
    );
    expect(result.status).toBe(1);
    expect(result.output).toContain("Released schemas are immutable");
  });

  it.each(["base", "head"] as const)(
    "fails closed when the %s endpoint is invalid",
    (invalidRef) => {
      const { status, output } = runHostedGateWithInvalidRef(
        TEST_APPEND_ONLY_SCRIPT,
        invalidRef,
      );
      expect(status).toBe(1);
      expect(output).toContain(
        "Could not resolve released-schema comparison commits.",
      );
    },
  );

  it("fails closed when the base resolves to a non-commit object", () => {
    const { status, output } = runHostedGateWithNonCommitBase(
      TEST_APPEND_ONLY_SCRIPT,
    );
    expect(status).toBe(1);
    expect(output).toContain(
      "Could not resolve released-schema comparison commits.",
    );
  });
});

describe("local guard against uncommitted released-schema changes", () => {
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
    "rejects changed bytes over another worktree base",
    () => {
      const { status, output } = runLocalWorktreeGate(
        (repo) => {
          writeFileSync(
            join(repo, "packages/schema/resume.v1.schema.json"),
            CHANGED_SCHEMA_BYTES,
          );
        },
        { v1Bytes: OTHER_BASE_SCHEMA_BYTES },
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

describe("local baseline enforcement", () => {
  it("allows hidden staged schema edits before v1", () => {
    const { status } = runLocalWorktreeGate(
      (repo) => {
        const path = "packages/schema/resume.v1.schema.json";
        writeFileSync(join(repo, path), "changed\n");
        git(repo, ["add", "--", path]);
        writeFileSync(join(repo, path), OLD_SCHEMA_BYTES);
      },
      { marked: false, oldMarker: true },
    );
    expect(status).toBe(0);
  });

  it.each(["deleted", "modified"])(
    "rejects hidden staged marker %s",
    (mode) => {
      const { status } = runLocalWorktreeGate((repo) => {
        const path = join(repo, ".v1-baseline");
        const original = readFileSync(path, "utf8");
        if (mode === "deleted") rmSync(path);
        else writeFileSync(path, "changed\n");
        git(repo, ["add", "-A", "--", ".v1-baseline"]);
        writeFileSync(path, original);
      });
      expect(status).toBe(1);
    },
  );

  it("rejects a local non-commit base before v1", () => {
    const { status } = runLocalWorktreeGate(
      (repo) => {
        git(repo, [
          "update-ref",
          "refs/remotes/origin/main",
          git(repo, ["rev-parse", "HEAD^{tree}"]).trim(),
        ]);
      },
      { marked: false },
    );
    expect(status).toBe(1);
  });
});
