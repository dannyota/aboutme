import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Dart generation is out of scope for Phase 0A (mobile deferred); only
// Go and TS are generated. gen/go/rawschema.go (decision D2) is included here
// too — this is the only check that would catch a committed rawschema.go
// going stale relative to resume.schema.json without this file's own
// regeneration step silently overwriting the drift first (see
// gen/go/rawschema_test.go's header for the complementary, narrower content
// check: it proves RawSchema's bytes match resume.schema.json, but only
// AFTER whatever ran immediately before it — if that was this very test,
// the drift is already gone by the time rawschema_test.go looks).
const files = ["gen/go/resume.go", "gen/ts/resume.ts", "gen/go/rawschema.go"];

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
