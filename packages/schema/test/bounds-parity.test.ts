import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { describe, expect, it } from "vitest";

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
  return JSON.parse(readFileSync(join(root, "fixtures", relativePath), "utf8"));
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
