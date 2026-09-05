// generate-wiring.test.ts guards the two Makefile edges that make the
// generated API client trustworthy, and that nothing else can prove:
//
//   1. `make generate` must regenerate the API client along with every
//      other committed artifact. If `api-gen` is ever dropped from
//      `generate`'s prerequisites, `make generate` starts leaving the
//      client stale — and the only symptom is a drift failure in a later,
//      unrelated commit.
//   2. `make api-check` must run the drift gate. A drift script nobody
//      invokes is not a gate.
//
// Both are read out of the Makefile as text, deliberately: running
// `make -n` would execute recipe-level shell and drag the whole toolchain
// into a contract test. MAKEFILE_PATH overrides the file under test so
// this check itself can be exercised against a fixture.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const makefilePath = process.env.MAKEFILE_PATH ?? "Makefile";
const makefile = readFileSync(makefilePath, "utf8");
const generatedClient = readFileSync(
  "apps/web/app/api/generated/openapi.ts",
  "utf8",
);

/** The prerequisites declared on a target line: `name: a b c ## help`. */
function prerequisites(target: string): string[] {
  const line = makefile
    .split("\n")
    .find((l) => l.startsWith(`${target}:`) && !l.startsWith(`${target}::`));
  if (line === undefined) return [];
  return line
    .slice(target.length + 1)
    .split("##")[0]
    .trim()
    .split(/\s+/)
    .filter(Boolean);
}

/** The recipe body (tab-indented lines) following a target. */
function recipe(target: string): string {
  const lines = makefile.split("\n");
  const start = lines.findIndex((l) => l.startsWith(`${target}:`));
  if (start === -1) return "";
  const body: string[] = [];
  for (const line of lines.slice(start + 1)) {
    if (line.startsWith("\t")) body.push(line);
    else if (line.trim() === "") continue;
    else break;
  }
  return body.join("\n");
}

const hasApiGen =
  prerequisites("api-gen").length > 0 || recipe("api-gen") !== "";

describe("generated-artifact wiring", () => {
  it("keeps `make generate` the one command that regenerates everything", () => {
    // The targets that exist today. api-gen is asserted separately below,
    // because it is added by the P0F Makefile patch.
    expect(prerequisites("generate")).toEqual(
      expect.arrayContaining(["schema-gen", "sqlc-gen"]),
    );
  });
});

// Skipped, loudly, until the P0F Makefile patch lands — see
// docs/api/README.md. The check is committed ahead of the patch so that
// wiring the target and guarding it cannot become two separate decisions.
describe.skipIf(!hasApiGen)("generated API client wiring", () => {
  it("`make generate` regenerates the API client too", () => {
    expect(
      prerequisites("generate"),
      "api-gen exists but `make generate` does not run it — the committed " +
        "API client would go stale without anyone noticing",
    ).toContain("api-gen");
  });

  it("`make api-check` runs the non-mutating drift gate", () => {
    expect(
      recipe("api-check"),
      "api-check must invoke apps/web/scripts/api-drift-check.sh, or " +
        "generated-client drift is not gated anywhere",
    ).toContain("api-drift-check.sh");
  });

  it("`api-gen` is declared .PHONY", () => {
    // Without this, a directory or file named api-gen would make the
    // target a silent no-op.
    const phony =
      makefile.split("\n").find((l) => l.startsWith(".PHONY:")) ?? "";
    expect(phony).toContain("api-gen");
  });
});

describe("Phase 5A generated public contract", () => {
  it("keeps optional public links constrained to string values", () => {
    const alias = generatedClient.match(/PublicLink: ([^;\n]+);/u)?.[1];
    expect(alias).toMatch(/^string(?: \| (?:string|""))*$/u);
  });

  it("contains the publish request and closed public resume types", () => {
    expect(generatedClient).toContain("PublishResumeRequest:");
    expect(generatedClient).toContain("PublicResume:");
    expect(generatedClient).toContain("getPublicResume:");
    expect(generatedClient).toContain("getPublicResumePhoto:");
  });
});
