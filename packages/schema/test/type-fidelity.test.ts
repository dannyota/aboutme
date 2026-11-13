import { execFileSync } from "node:child_process";
import { describe, expect, it } from "vitest";

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
