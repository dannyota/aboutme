import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { describe, expect, it } from "vitest";

// bounds-parity.test.ts is the TypeScript half of D1(e), the cross-language
// verdict-parity check the P2A task brief calls the centerpiece of the
// jsonschema/v6 adoption, not a bolt-on: apps/server/internal/resume's
// bounds_test.go runs jsonschema/v6 over this same committed corpus
// (fixtures/bounds/*.json + manifest.json) and every existing
// fixtures/**/*.json fixture; this file runs ajv over the IDENTICAL files
// and asserts the SAME verdicts. A disagreement in either direction is a
// red build here or there -- this pair of tests, not resume.schema.json
// itself, is what makes "one schema, both languages" true. Before this
// task, nothing machine-checked that the Go and TypeScript validators
// agreed at all; parity rested on two hand-written test files matching by
// convention.
//
// ajv is constructed identically to test/schema.test.ts and
// test/conformance.test.ts (addFormats(new Ajv2020({allErrors:true,
// strict:true})), which ASSERTS format -- the posture
// apps/server/internal/resume's compiler pins via AssertFormat() to match).

const root = new URL("..", import.meta.url).pathname;
const schema = JSON.parse(
  readFileSync(join(root, "resume.schema.json"), "utf8"),
);
const ajv = addFormats(new Ajv2020({ allErrors: true, strict: true }));
const validate = ajv.compile(schema);

interface ManifestRow {
  file: string;
  boundPath?: string;
  limit?: number;
  expect: "valid" | "invalid";
  storeExpect: "valid" | "invalid";
}

interface Manifest {
  bounds: ManifestRow[];
  storeFixtures: ManifestRow[];
}

const manifest: Manifest = JSON.parse(
  readFileSync(join(root, "fixtures", "bounds", "manifest.json"), "utf8"),
);

function loadFixture(relativePath: string): unknown {
  return JSON.parse(
    readFileSync(join(root, "fixtures", relativePath), "utf8"),
  );
}

function checkVerdict(label: string, instance: unknown, wantValid: boolean) {
  const gotValid = validate(instance) as boolean;
  expect(
    gotValid,
    `${label}: ajv says valid=${gotValid}, manifest/convention says valid=${wantValid}` +
      (gotValid !== wantValid ? ` (${ajv.errorsText(validate.errors)})` : ""),
  ).toBe(wantValid);
}

describe("bounds/fixtures cross-language JSON-Schema verdict parity (ajv)", () => {
  it("has a non-empty manifest", () => {
    expect(manifest.bounds.length).toBeGreaterThan(0);
    expect(manifest.storeFixtures.length).toBeGreaterThan(0);
  });

  it("agrees with the generated bounds corpus's recorded verdicts", () => {
    for (const row of manifest.bounds) {
      // manifest "file" entries are rooted at fixtures/ (e.g.
      // "bounds/richtext-maxlength-codepoints-valid.json").
      const instance = loadFixture(row.file);
      checkVerdict(row.file, instance, row.expect === "valid");
    }
  });

  it("agrees with the recorded verdicts for fixtures/store/*.json (naming convention alone is not enough there)", () => {
    for (const row of manifest.storeFixtures) {
      const instance = loadFixture(row.file);
      checkVerdict(row.file, instance, row.expect === "valid");
    }
  });

  it("agrees with the naming-convention verdict for every top-level fixtures/*.json fixture", () => {
    // isFile() alone would also match a stray non-JSON file (e.g. a
    // README) dropped into fixtures/ -- the Go side (listJSONFixtures in
    // apps/server/internal/resume/validate_test.go) additionally requires
    // a ".json" suffix; this must match it exactly, or a non-JSON file
    // would break only this side (round-2 review minor finding).
    const names = readdirSync(join(root, "fixtures"), { withFileTypes: true })
      .filter((entry) => entry.isFile() && entry.name.endsWith(".json"))
      .map((entry) => entry.name);
    expect(names.length).toBeGreaterThan(0);
    for (const name of names) {
      const instance = loadFixture(name);
      checkVerdict(name, instance, !name.startsWith("invalid-"));
    }
  });
});
