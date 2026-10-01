import { readFileSync } from "node:fs";
import { join } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { describe, expect, it } from "vitest";

// Document v5 adds the optional customization.colorScheme, an enum of light,
// dark, and system. Absent means light (docs/design/public-page-theme.md).

const root = new URL("..", import.meta.url).pathname;
const read = (name: string) =>
  JSON.parse(readFileSync(join(root, name), "utf8"));
const ajv = addFormats(new Ajv2020({ allErrors: true, strict: true }));
const validate = ajv.compile(read("resume.schema.json"));
const validateV4 = ajv.compile(read("resume.v4.schema.json"));

const withScheme = (colorScheme: unknown) => {
  const document = read("fixtures/minimal.json");
  document.customization.colorScheme = colorScheme;
  return document;
};

describe("document v5 color scheme", () => {
  it("is version 5", () => {
    expect(read("fixtures/minimal.json").schemaVersion).toBe(5);
  });

  it("accepts light, dark, and system", () => {
    for (const scheme of ["light", "dark", "system"]) {
      expect(
        validate(withScheme(scheme)),
        ajv.errorsText(validate.errors),
      ).toBe(true);
    }
  });

  it("leaves colorScheme optional", () => {
    const document = read("fixtures/minimal.json");
    expect(document.customization.colorScheme).toBeUndefined();
    expect(validate(document), ajv.errorsText(validate.errors)).toBe(true);
  });

  it("rejects any other value", () => {
    for (const scheme of ["auto", "Dark", "", null, 0, ["dark"]]) {
      expect(validate(withScheme(scheme)), JSON.stringify(scheme)).toBe(false);
    }
  });

  it("stays closed at version 4", () => {
    expect(validateV4({ ...withScheme("dark"), schemaVersion: 4 })).toBe(false);
  });

  it("validates the retained v4 fixtures at version 4", () => {
    for (const name of ["minimal.json", "full.json"]) {
      const document = read(`fixtures/v4/${name}`);
      expect(document.schemaVersion).toBe(4);
      expect(validateV4(document), ajv.errorsText(validateV4.errors)).toBe(
        true,
      );
    }
  });
});
