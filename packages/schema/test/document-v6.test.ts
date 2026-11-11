import { readFileSync } from "node:fs";
import { join } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { describe, expect, it } from "vitest";

// Document v6 adds the optional projectId on project entries only. It names a
// private library project of the same account (docs/design/projects.md).

const root = new URL("..", import.meta.url).pathname;
const read = (name: string) =>
  JSON.parse(readFileSync(join(root, name), "utf8"));
const ajv = addFormats(new Ajv2020({ allErrors: true, strict: true }));
const validate = ajv.compile(read("resume.schema.json"));
const validateV5 = ajv.compile(read("resume.v5.schema.json"));

const projectId = "01890a5d-ac96-774b-bcce-b302099a8057";

const withEntry = (sectionType: string, entry: Record<string, unknown>) => {
  const document = read("fixtures/minimal.json");
  document.content.section = {
    sectionType,
    entries: [{ id: "6f0d8f5e-6d0b-4b8e-9b55-2f7c2a0c9e11", ...entry }],
  };
  document.customization.layout.sections.main.push("section");
  return document;
};

describe("document v6 project link", () => {
  it("is version 6", () => {
    expect(read("fixtures/minimal.json").schemaVersion).toBe(6);
  });

  it("accepts a UUID projectId on a project entry", () => {
    const document = withEntry("project", { title: "Atlas", projectId });
    expect(validate(document), ajv.errorsText(validate.errors)).toBe(true);
  });

  it("leaves projectId optional", () => {
    const document = withEntry("project", { title: "Atlas" });
    expect(validate(document), ajv.errorsText(validate.errors)).toBe(true);
  });

  it("rejects a projectId that is not a UUID", () => {
    for (const value of ["", "atlas", 7, null, [projectId]]) {
      expect(
        validate(withEntry("project", { projectId: value })),
        JSON.stringify(value),
      ).toBe(false);
    }
  });

  it("rejects projectId on every other entry type", () => {
    for (const sectionType of ["work", "education", "certificate", "custom"]) {
      expect(
        validate(withEntry(sectionType, { projectId })),
        sectionType,
      ).toBe(false);
    }
  });

  it("stays closed at version 5", () => {
    const document = { ...withEntry("project", { projectId }), schemaVersion: 5 };
    expect(validateV5(document)).toBe(false);
  });

  it("validates the retained v5 fixtures at version 5", () => {
    for (const name of ["minimal.json", "full.json"]) {
      const document = read(`fixtures/v5/${name}`);
      expect(document.schemaVersion).toBe(5);
      expect(validateV5(document), ajv.errorsText(validateV5.errors)).toBe(
        true,
      );
    }
  });
});
