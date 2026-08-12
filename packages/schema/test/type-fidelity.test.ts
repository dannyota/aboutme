import { execFileSync } from "node:child_process";
import { describe, expect, it } from "vitest";

// Byte-stable generation does not prove the emitted shape. The compile-time
// fixture checks discriminated-union narrowing and the draft-permissive entry
// fields under `tsc --strict`. See docs/design/data.md#draft-and-publish-validation.
describe("generated TypeScript type fidelity", () => {
  it("compiles the discriminated-union narrowing fixture under tsc --strict", () => {
    expect(() =>
      execFileSync(
        "node_modules/.bin/tsc",
        ["--noEmit", "--strict", "test/type-fidelity.fixture.ts"],
        { stdio: "pipe" },
      ),
    ).not.toThrow();
  });
});
