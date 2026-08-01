import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Dart generation is out of scope for Phase 0A (mobile deferred); only
// Go and TS are generated.
const files = ["gen/go/resume.go", "gen/ts/resume.ts"];

describe("generated code", () => {
  it("is byte-identical to a fresh generation", () => {
    const before = files.map((f) => readFileSync(f, "utf8"));
    execFileSync("node", ["scripts/generate.mjs"], { stdio: "inherit" });
    const after = files.map((f) => readFileSync(f, "utf8"));
    expect(after).toEqual(before);
  });

  it("marks every file as generated", () => {
    for (const f of files) {
      expect(readFileSync(f, "utf8")).toContain("DO NOT EDIT");
    }
  });
});
