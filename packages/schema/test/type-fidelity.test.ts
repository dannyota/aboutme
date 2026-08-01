import { execFileSync } from "node:child_process";
import { describe, expect, it } from "vitest";

// gen.test.ts proves the generated files reproduce byte-for-byte; it would
// pass even if the generated shape were wrong. This test proves the shape
// itself is right: type-fidelity.fixture.ts exercises gen/ts/resume.ts's
// discriminated union (switch(section.sectionType) narrows to the matching
// entry type) and each entry type's required/optional fields — draft-
// permissive (design spec §3, revised 2026-08-01): only `id` is required,
// every domain field is optional. `tsc --noEmit --strict` both fails the
// build on any *unexpected* type error and fails on any `@ts-expect-error`
// that doesn't actually error (TypeScript's own semantics for the
// directive), so a passing run here means every assertion in the fixture
// held, not just that nothing crashed.
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
