import { readFileSync } from "node:fs";
import { join } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { describe, expect, it } from "vitest";

// Document v4 adds customization.header.photoPosition and a project entry
// subtitle (docs/adr/0044-header-photo-position-and-project-subtitle.md).
// Both are optional: absent keeps the photo on top and the project without a
// subtitle.

const root = new URL("..", import.meta.url).pathname;
const read = (name: string) =>
  JSON.parse(readFileSync(join(root, name), "utf8"));
const ajv = addFormats(new Ajv2020({ allErrors: true, strict: true }));
const validate = ajv.compile(read("resume.schema.json"));
const validateV3 = ajv.compile(read("resume.v3.schema.json"));

const withPosition = (photoPosition: unknown) => {
  const document = read("fixtures/minimal.json");
  document.customization.header = {
    align: "left",
    detailsLayout: "inline",
    iconStyle: "outline",
    photoPosition,
  };
  return document;
};

const withProjectSubtitle = (subtitle: unknown) => {
  const document = read("fixtures/minimal.json");
  document.content.projects = {
    sectionType: "project",
    entries: [
      {
        id: "018f0000-0000-7000-8000-0000000000e1",
        title: "Analytical Engine notes",
        subtitle,
      },
    ],
  };
  document.customization.layout.sections.main.push("projects");
  return document;
};

describe("document v4 header photo position", () => {
  it("is version 4", () => {
    expect(read("fixtures/minimal.json").schemaVersion).toBe(4);
  });

  it("accepts top, left, and right", () => {
    for (const position of ["top", "left", "right"]) {
      expect(
        validate(withPosition(position)),
        ajv.errorsText(validate.errors),
      ).toBe(true);
    }
  });

  it("leaves photoPosition optional", () => {
    const document = withPosition("top");
    delete document.customization.header.photoPosition;
    expect(validate(document), ajv.errorsText(validate.errors)).toBe(true);
  });

  it("rejects any other value", () => {
    for (const position of ["bottom", "Top", "", null, 0, ["left"]]) {
      expect(validate(withPosition(position)), JSON.stringify(position)).toBe(
        false,
      );
    }
  });

  it("stays closed at version 3", () => {
    expect(validateV3({ ...withPosition("left"), schemaVersion: 3 })).toBe(
      false,
    );
  });
});

describe("document v4 project subtitle", () => {
  it("accepts plain text up to 160 code points", () => {
    for (const subtitle of ["", "Go, PostgreSQL", "\u{1F600}".repeat(160)]) {
      expect(
        validate(withProjectSubtitle(subtitle)),
        ajv.errorsText(validate.errors),
      ).toBe(true);
    }
  });

  it("rejects a longer or non-string subtitle", () => {
    for (const subtitle of ["a".repeat(161), null, 1, ["a"], { text: "a" }]) {
      expect(
        validate(withProjectSubtitle(subtitle)),
        JSON.stringify(subtitle).slice(0, 20),
      ).toBe(false);
    }
  });

  it("stays closed at version 3", () => {
    expect(
      validateV3({ ...withProjectSubtitle("Go"), schemaVersion: 3 }),
    ).toBe(false);
  });
});
