import { readFileSync } from "node:fs";
import { join } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { describe, expect, it } from "vitest";

// Document v3 adds a per-detail link display choice and a body text alignment
// (docs/adr/0041-contact-link-display-and-body-justify.md). Both are optional,
// and absent keeps the v2 rendering.

const root = new URL("..", import.meta.url).pathname;
const read = (name: string) =>
  JSON.parse(readFileSync(join(root, name), "utf8"));
const ajv = addFormats(new Ajv2020({ allErrors: true, strict: true }));
const validate = ajv.compile(read("resume.schema.json"));
const validateV2 = ajv.compile(read("resume.v2.schema.json"));

const minimal = () => read("fixtures/minimal.json");

const withDetail = (detail: Record<string, unknown>) => {
  const document = minimal();
  document.personalDetails.details = [
    {
      id: "018f0000-0000-7000-8000-0000000000d1",
      type: "custom",
      label: "Google Scholar",
      value: "https://scholar.example.com/ada",
      isHidden: false,
      ...detail,
    },
  ];
  return document;
};

const withTextAlign = (textAlign: unknown) => {
  const document = minimal();
  document.customization.font.textAlign = textAlign;
  return document;
};

describe("document v3 contact link display", () => {
  it("is the current version", () => {
    expect(minimal().schemaVersion).toBe(4);
    expect(validate(minimal()), ajv.errorsText(validate.errors)).toBe(true);
  });

  it("accepts each display mode on every detail type", () => {
    for (const display of ["short", "full", "label"]) {
      for (const type of ["email", "website", "github", "twitter", "custom"]) {
        const value = type === "email" ? "ada@example.com" : "https://a.example";
        expect(
          validate(withDetail({ type, value, display })),
          `${type}/${display}: ${ajv.errorsText(validate.errors)}`,
        ).toBe(true);
      }
    }
  });

  it("leaves display optional", () => {
    expect(validate(withDetail({})), ajv.errorsText(validate.errors)).toBe(
      true,
    );
  });

  it("rejects any other display value", () => {
    for (const display of ["", "Short", "icon", null, 1, true, ["full"]]) {
      expect(
        validate(withDetail({ display })),
        JSON.stringify(display),
      ).toBe(false);
    }
  });

  it("keeps custom values free text, so hostile values validate and render as text", () => {
    for (const value of [
      "javascript:alert(1)",
      "data:text/html,<b>x</b>",
      "//evil.example",
      "HTTPS://example.com",
      " https://example.com",
    ]) {
      expect(
        validate(withDetail({ value, display: "label" })),
        value,
      ).toBe(true);
    }
  });
});

describe("document v3 body text alignment", () => {
  it("accepts left and justify under customization.font.textAlign", () => {
    for (const textAlign of ["left", "justify"]) {
      expect(
        validate(withTextAlign(textAlign)),
        ajv.errorsText(validate.errors),
      ).toBe(true);
    }
  });

  it("rejects any other alignment", () => {
    for (const textAlign of ["right", "center", "Justify", "", null, 0]) {
      expect(validate(withTextAlign(textAlign)), String(textAlign)).toBe(
        false,
      );
    }
  });
});

describe("document v2 stays closed to v3 fields", () => {
  const asV2 = (document: Record<string, unknown>) => ({
    ...document,
    schemaVersion: 2,
  });

  it("rejects display and textAlign at version 2", () => {
    expect(validateV2(asV2(withDetail({ display: "full" })))).toBe(false);
    expect(validateV2(asV2(withTextAlign("justify")))).toBe(false);
    expect(validateV2(asV2(withDetail({})))).toBe(true);
  });
});
