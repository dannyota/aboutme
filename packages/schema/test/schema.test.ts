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

const allowlist = JSON.parse(
  readFileSync(join(root, "validation", "sanitizer-allowlist.v1.json"), "utf8"),
);

const fixture = (name: string) =>
  JSON.parse(readFileSync(join(root, "fixtures", name), "utf8"));

const names = readdirSync(join(root, "fixtures"), { withFileTypes: true })
  .filter((entry) => entry.isFile())
  .map((entry) => entry.name);

describe("resume schema", () => {
  it("accepts every valid fixture", () => {
    for (const name of names.filter((n) => !n.startsWith("invalid-"))) {
      expect(
        validate(fixture(name)),
        `${name}: ${ajv.errorsText(validate.errors)}`,
      ).toBe(true);
    }
  });

  it("rejects every invalid fixture", () => {
    for (const name of names.filter((n) => n.startsWith("invalid-"))) {
      expect(validate(fixture(name)), `${name} should be invalid`).toBe(false);
    }
  });

  it("pins the sanitizer allowlist version", () => {
    expect(schema.$defs.sanitizerAllowlistVersion.const).toBe(1);
  });

  it("requires present entries to omit an end date", () => {
    expect(
      validate({
        ...fixture("minimal.json"),
        content: {
          work: {
            sectionType: "work",
            displayName: "Experience",
            iconKey: "briefcase",
            entries: [
              {
                id: "018f0000-0000-7000-8000-000000000001",
                isHidden: false,
                jobTitle: "Engineer",
                employer: "Acme",
                dates: { start: { y: 2020 }, end: { y: 2022 }, present: true },
              },
            ],
          },
        },
      }),
    ).toBe(false);
  });

  it("rejects a non-present date range with a null end", () => {
    expect(
      validate({
        ...fixture("minimal.json"),
        content: {
          work: {
            sectionType: "work",
            displayName: "Experience",
            iconKey: "briefcase",
            entries: [
              {
                id: "018f0000-0000-7000-8000-000000000002",
                isHidden: false,
                jobTitle: "Engineer",
                employer: "Acme",
                employerLink: "",
                city: "Hanoi",
                country: "Vietnam",
                dates: { start: { y: 2020 }, end: null, present: false },
                description: "",
              },
            ],
          },
        },
      }),
    ).toBe(false);
  });

  it("accepts a closed date range", () => {
    expect(
      validate({
        ...fixture("minimal.json"),
        content: {
          work: {
            sectionType: "work",
            displayName: "Experience",
            iconKey: "briefcase",
            entries: [
              {
                id: "018f0000-0000-7000-8000-000000000003",
                isHidden: false,
                jobTitle: "Engineer",
                employer: "Acme",
                employerLink: "",
                city: "Hanoi",
                country: "Vietnam",
                dates: {
                  start: { y: 2020 },
                  end: { y: 2022 },
                  present: false,
                },
                description: "",
              },
            ],
          },
        },
      }),
    ).toBe(true);
  });

  describe("link scheme allowlist (hole 3: dangerous URL schemes)", () => {
    const withEmployerLink = (employerLink: string) => ({
      ...fixture("minimal.json"),
      content: {
        work: {
          sectionType: "work",
          entries: [
            { id: "018f0000-0000-7000-8000-000000000004", employerLink },
          ],
        },
      },
      customization: {
        ...fixture("minimal.json").customization,
        layout: { columns: 1, sections: { main: ["work"], sidebar: [] } },
      },
    });

    it.each([
      ["https://example.com", true],
      ["mailto:ada@example.com", true],
      ["tel:+84900000000", true],
      ["", true],
      ["javascript:alert(1)", false],
      ["JavaScript:alert(1)", false],
      ["JAVASCRIPT:alert(1)", false],
      ["   javascript:alert(1)", false],
      ["data:text/html,<script>alert(1)</script>", false],
      ["//evil.example.com/phish", false],
      ["HTTPS://example.com", false],
      ["vbscript:msgbox(1)", false],
    ])("%s -> valid=%s", (value, expected) => {
      expect(validate(withEmployerLink(value))).toBe(expected);
    });
  });

  describe("personalDetail.value URL-scheme allowlist (finding C1: dangerous URL schemes in contact chips)", () => {
    const withDetail = (type: string, value: string) => ({
      ...fixture("minimal.json"),
      personalDetails: {
        fullName: "Ada Lovelace",
        details: [
          {
            id: "018f0000-0000-7000-8000-000000000701",
            type,
            value,
            isHidden: false,
          },
        ],
      },
    });

    it.each([
      ["website", "javascript:alert(document.cookie)", false],
      ["website", "data:text/html,<script>alert(1)</script>", false],
      ["website", "//evil.example.com", false],
      ["linkedin", "javascript:alert(1)", false],
      ["github", "vbscript:msgbox(1)", false],
      ["twitter", "JavaScript:alert(1)", false],
      ["website", "mailto:ada@example.com", false],
      ["website", "https://ada.example.com", true],
      ["linkedin", "", true],
      ["email", "javascript:alert(1)", true],
    ])("type=%s value=%s -> valid=%s", (type, value, expected) => {
      expect(validate(withDetail(type, value))).toBe(expected);
    });
  });

  it("the link pattern's permitted schemes never drift from the sanitizer allowlist's urlSchemes", () => {
    const linkPattern: string = schema.$defs.link.anyOf[1].pattern;
    for (const scheme of allowlist.urlSchemes) {
      expect(
        linkPattern,
        `link pattern missing allowlisted scheme "${scheme}"`,
      ).toContain(`${scheme}:`);
    }
    const schemesInPattern = [...linkPattern.matchAll(/([a-z]+):/g)].map(
      (m) => m[1],
    );
    for (const scheme of schemesInPattern) {
      expect(
        allowlist.urlSchemes,
        `link pattern has scheme "${scheme}" not in the allowlist`,
      ).toContain(scheme);
    }
  });

  it("rejects a layout.sections array padded past maxItems (DoS guard)", () => {
    const doc = fixture("minimal.json");
    const tooMany = Array.from({ length: 25 }, (_, i) =>
      String.fromCharCode(97 + i),
    );
    expect(tooMany).toHaveLength(25);
    expect(
      validate({
        ...doc,
        customization: {
          ...doc.customization,
          layout: { columns: 1, sections: { main: tooMany, sidebar: [] } },
        },
      }),
    ).toBe(false);
  });

  describe("draft permissiveness is not weakened by the new rules", () => {
    it('accepts a cleared fullName ("") and an absent details array', () => {
      expect(
        validate({
          schemaVersion: 6,
          personalDetails: { fullName: "" },
          content: {},
          customization: fixture("minimal.json").customization,
        }),
      ).toBe(true);
    });

    it("accepts a freshly created section with no displayName/iconKey and no entries", () => {
      expect(
        validate({
          schemaVersion: 6,
          personalDetails: { fullName: "Ada Lovelace", details: [] },
          content: { work: { sectionType: "work", entries: [] } },
          customization: {
            ...fixture("minimal.json").customization,
            layout: { columns: 1, sections: { main: ["work"], sidebar: [] } },
          },
        }),
      ).toBe(true);
    });

    it("still rejects a section entirely missing its sectionType discriminator", () => {
      expect(
        validate({
          schemaVersion: 6,
          personalDetails: { fullName: "Ada Lovelace", details: [] },
          content: { work: { entries: [] } },
          customization: fixture("minimal.json").customization,
        }),
      ).toBe(false);
    });
  });

  describe("photo.key pattern (finding NEW-M3: RE2 portability)", () => {
    const withPhotoKey = (key: string) => ({
      ...fixture("minimal.json"),
      personalDetails: { fullName: "Ada Lovelace", photo: { key } },
    });

    it("the pattern contains no lookahead (RE2-incompatible) syntax", () => {
      const pattern: string = schema.$defs.photo.properties.key.pattern;
      expect(pattern).not.toContain("(?!");
      expect(pattern).not.toContain("(?=");
      expect(pattern).not.toContain("(?<");
    });

    it.each([
      ["resumes/ada/photo-original.jpg", true],
      ["resumes/018f0000-0000-7000-8000-000000000001/photo-original.jpg", true],
      ["https://evil.example.com/x.jpg", false],
      ["a\nb.jpg", false],
      [".hidden.jpg", false],
      ["_x.jpg", false],
    ])("%s -> valid=%s", (key, expected) => {
      expect(validate(withPhotoKey(key))).toBe(expected);
    });

    it('no longer rejects a mid-string ".." at the schema layer (moved to the store layer)', () => {
      expect(validate(withPhotoKey("a/../b.jpg"))).toBe(true);
    });
  });

  describe("customization additions: pageMargin, surface, header", () => {
    const customization = () => fixture("minimal.json").customization;

    const withCustomization = (
      patch: Record<string, unknown>,
    ): Record<string, unknown> => {
      const base = customization();
      const merged: Record<string, unknown> = { ...base };
      for (const [key, value] of Object.entries(patch)) {
        const existing = merged[key];
        merged[key] =
          existing &&
          typeof existing === "object" &&
          !Array.isArray(existing) &&
          value &&
          typeof value === "object" &&
          !Array.isArray(value)
            ? { ...(existing as object), ...(value as object) }
            : value;
      }
      return { ...fixture("minimal.json"), customization: merged };
    };

    const custProps = () => schema.$defs.customization.properties;

    describe("optional-by-default (design spec §3: no all-document migration)", () => {
      it("customization still requires exactly the original eight keys", () => {
        expect([...schema.$defs.customization.required].sort()).toEqual(
          [
            "colors",
            "dateFormat",
            "font",
            "heading",
            "layout",
            "pageFormat",
            "sectionDisplay",
            "spacing",
          ].sort(),
        );
      });

      it("spacing.required does not include pageMargin", () => {
        expect([...custProps().spacing.required].sort()).toEqual(
          ["entryGap", "lineHeight", "sectionGap"].sort(),
        );
      });

      it("colors.required does not include surface", () => {
        expect([...custProps().colors.required].sort()).toEqual(
          ["background", "primary", "text"].sort(),
        );
      });

      it("layout.required does not include surfaceTarget", () => {
        expect([...custProps().layout.required].sort()).toEqual(
          ["columns", "sections"].sort(),
        );
      });

      it("a document carrying none of the three additions is still valid", () => {
        expect(
          validate(fixture("minimal.json")),
          ajv.errorsText(validate.errors),
        ).toBe(true);
      });
    });

    describe("spacing.pageMargin", () => {
      const margin = (value: unknown) =>
        withCustomization({ spacing: { pageMargin: value } });

      const axis = custProps().spacing.properties?.pageMargin?.properties?.x;
      const min: number = axis?.minimum ?? 0;
      const max: number = axis?.maximum ?? 40;

      it("declares an explicit min/max on both axes, like every other spacing field", () => {
        const pageMargin = custProps().spacing.properties.pageMargin;
        for (const axis of ["x", "y"]) {
          expect(pageMargin.properties[axis].type).toBe("number");
          expect(typeof pageMargin.properties[axis].minimum).toBe("number");
          expect(typeof pageMargin.properties[axis].maximum).toBe("number");
        }
        expect(pageMargin.properties.y.minimum).toBe(min);
        expect(pageMargin.properties.y.maximum).toBe(max);
        expect(pageMargin.additionalProperties).toBe(false);
        expect([...pageMargin.required].sort()).toEqual(["x", "y"]);
      });

      it("names the renderer's 15 mm fallback in its description (no schema-level default)", () => {
        const pageMargin = custProps().spacing.properties.pageMargin;
        expect(pageMargin).not.toHaveProperty("default");
        expect(pageMargin.description).toContain("15");
      });

      it.each([
        [{ x: 15, y: 15 }, true],
        [{ x: min, y: min }, true],
        [{ x: max, y: max }, true],
        [{ x: 12.5, y: 20 }, true],
        [{ x: max + 1, y: 15 }, false],
        [{ x: 15, y: max + 1 }, false],
        [{ x: min - 1, y: 15 }, false],
        [{ x: 15 }, false],
        [{ y: 15 }, false],
        [{ x: 15, y: 15, z: 15 }, false],
        [{ x: "15", y: "15" }, false],
        ["15mm", false],
      ])("%o -> valid=%s", (value, expected) => {
        expect(validate(margin(value))).toBe(expected);
      });
    });

    describe("colors.surface and layout.surfaceTarget", () => {
      it("colors.surface reuses the hexColor $def", () => {
        expect(custProps().colors.properties.surface).toEqual({
          description: expect.any(String),
          $ref: "#/$defs/hexColor",
        });
      });

      it.each([
        ["#f4f4f5", true],
        ["#FFF", false],
        ["", false],
        ["rebeccapurple", false],
      ])("colors.surface %s -> valid=%s", (value, expected) => {
        expect(
          validate(withCustomization({ colors: { surface: value } })),
        ).toBe(expected);
      });

      it("surfaceTarget is exactly none | header | sidebar", () => {
        expect(custProps().layout.properties.surfaceTarget.enum).toEqual([
          "none",
          "header",
          "sidebar",
        ]);
      });

      it.each([
        ["none", true],
        ["header", true],
        ["sidebar", true],
        ["main", false],
        ["Header", false],
        ["", false],
      ])("surfaceTarget %s -> valid=%s", (value, expected) => {
        expect(
          validate(withCustomization({ layout: { surfaceTarget: value } })),
        ).toBe(expected);
      });

      it('accepts surfaceTarget "sidebar" with columns: 1 (degrades to "none" at render time, never an error)', () => {
        expect(
          validate(
            withCustomization({
              colors: { surface: "#eef2ff" },
              layout: { columns: 1, surfaceTarget: "sidebar" },
            }),
          ),
          ajv.errorsText(validate.errors),
        ).toBe(true);
      });

      it("accepts a surfaceTarget with no colors.surface, and a colors.surface with no surfaceTarget", () => {
        expect(
          validate(withCustomization({ layout: { surfaceTarget: "header" } })),
          ajv.errorsText(validate.errors),
        ).toBe(true);
        expect(
          validate(withCustomization({ colors: { surface: "#eef2ff" } })),
          ajv.errorsText(validate.errors),
        ).toBe(true);
      });

      it("states the columns: 1 degradation rule in surfaceTarget's own description", () => {
        const description: string =
          custProps().layout.properties.surfaceTarget.description;
        expect(description).toMatch(/columns/);
        expect(description).toMatch(/sidebar/);
        expect(description).toMatch(/renders? as .?none.?/i);
        expect(description).toMatch(
          /never an error|rather than erroring|not an error/i,
        );
      });
    });

    describe("customization.header", () => {
      const header = (value: unknown) => withCustomization({ header: value });
      const complete = {
        align: "left",
        detailsLayout: "inline",
        iconStyle: "outline",
      };

      it("is a closed object whose three fields are all required once it is present", () => {
        const def = custProps().header;
        expect(def.type).toBe("object");
        expect(def.additionalProperties).toBe(false);
        expect([...def.required].sort()).toEqual(
          ["align", "detailsLayout", "iconStyle"].sort(),
        );
        expect(def.properties.align.enum).toEqual(["left", "center"]);
        expect(def.properties.detailsLayout.enum).toEqual([
          "inline",
          "stacked",
        ]);
        expect(def.properties.iconStyle.enum).toEqual(["none", "outline"]);
      });

      it.each([
        [complete, true],
        [{ ...complete, align: "center" }, true],
        [{ ...complete, detailsLayout: "stacked" }, true],
        [{ ...complete, iconStyle: "none" }, true],
        [{ ...complete, iconStyle: "solid" }, false],
        [{ align: "left", detailsLayout: "inline" }, false],
        [{ align: "left", iconStyle: "outline" }, false],
        [{ detailsLayout: "inline", iconStyle: "outline" }, false],
        [{}, false],
        [{ ...complete, style: "uppercase" }, false],
        [{ ...complete, align: "right" }, false],
        [{ ...complete, align: "justify" }, false],
        [{ ...complete, detailsLayout: "grid" }, false],
        [{ ...complete, iconStyle: "filled" }, false],
      ])("%o -> valid=%s", (value, expected) => {
        expect(validate(header(value))).toBe(expected);
      });
    });

    describe("heading vs header: both descriptions state the distinction", () => {
      it("heading's description scopes it to section headings and points at header", () => {
        const description: string = custProps().heading.description;
        expect(description).toMatch(/section/i);
        expect(description).toContain("customization.header");
      });

      it("header's description scopes it to the resume's top block and points at heading", () => {
        const description: string = custProps().header.description;
        expect(description).toContain("customization.heading");
        expect(description).toMatch(/section headings/i);
        expect(description).toMatch(/fullName|name/);
      });
    });
  });
});
