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

// withFileTypes + isFile() skips fixtures/store/: that directory holds
// fixtures for the P2A store-layer test (duplicate entry ids can't be
// expressed in JSON Schema), not this schema test.
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

  // Hole 3 (phase-gate review): resume.schema.json:39 used to accept any URI
  // format, so "javascript:alert(1)" validated in a structured link field
  // (employerLink/schoolLink/titleLink/project link). The #/$defs/link
  // pattern now restricts to exactly the sanitizer allowlist's urlSchemes
  // (https, mailto, tel — validation/sanitizer-allowlist.v1.json).
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
      ["", true], // explicitly cleared — still draft-permissive
      ["javascript:alert(1)", false], // the reviewer's exact proof-of-concept
      ["JavaScript:alert(1)", false], // mixed-case scheme obfuscation
      ["JAVASCRIPT:alert(1)", false], // upper-case scheme obfuscation
      ["   javascript:alert(1)", false], // leading-whitespace obfuscation
      ["data:text/html,<script>alert(1)</script>", false], // data: scheme
      ["//evil.example.com/phish", false], // protocol-relative
      ["HTTPS://example.com", false], // only exact lowercase "https://" is accepted
      ["vbscript:msgbox(1)", false], // another non-allowlisted scheme
    ])("%s -> valid=%s", (value, expected) => {
      expect(validate(withEmployerLink(value))).toBe(expected);
    });
  });

  // Finding C1 (phase-gate review of commit 34c21a6): the hole-3 fix above
  // scoped $defs/link down to https/mailto/tel for employerLink/schoolLink/
  // titleLink/project link, but missed personalDetails.details[].value —
  // even though four of its eight `type` values (website, linkedin, github,
  // twitter) are URLs, and the repo's own fixtures/full.json fixture stores
  // them as such (see fixtures/invalid-dangerous-detail-url-scheme.json,
  // which is full.json with exactly that one field swapped to a hostile
  // scheme). resume.schema.json's personalDetail $def now scopes `value` to
  // https:// only (not $defs/link's full https/mailto/tel set — a
  // linkedin/github/twitter/website chip is never a mailto:/tel: target)
  // when `type` is one of those four, via an allOf/if-then, the same
  // conditional-schema pattern dateRange already uses.
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
      ["website", "javascript:alert(document.cookie)", false], // the reviewer's exact proof-of-concept
      ["website", "data:text/html,<script>alert(1)</script>", false],
      ["website", "//evil.example.com", false], // protocol-relative
      ["linkedin", "javascript:alert(1)", false],
      ["github", "vbscript:msgbox(1)", false],
      ["twitter", "JavaScript:alert(1)", false], // mixed-case scheme obfuscation
      ["website", "mailto:ada@example.com", false], // type-appropriate subset: https-only here, unlike $defs/link
      ["website", "https://ada.example.com", true],
      ["linkedin", "", true], // explicitly cleared — still draft-permissive
      ["email", "javascript:alert(1)", true], // out-of-scope type (item 1): value format intentionally left unconstrained
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
    // And nothing extra: every scheme literal in the pattern must be one the
    // allowlist actually names.
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

  // Hole 2 (phase-gate review): resume.schema.json:533's layout.sections
  // arrays had no maxItems, so a request body could pad them arbitrarily.
  it("rejects a layout.sections array padded past maxItems (DoS guard)", () => {
    const doc = fixture("minimal.json");
    const tooMany = Array.from({ length: 25 }, (_, i) =>
      String.fromCharCode(97 + i),
    ); // "a".."y", 25 distinct lowercase-letter keys
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

  // Draft permissiveness (design spec §3, revised 2026-08-01): personalDetails
  // (including fullName), details, and section metadata (displayName,
  // iconKey) must stay optional/emptyable so autosave never blocks on a
  // half-typed document. See fixtures/draft-cleared-name-empty-section.json
  // for the fixture-level assertion; these confirm the specific fields.
  describe("draft permissiveness is not weakened by the new rules", () => {
    it('accepts a cleared fullName ("") and an absent details array', () => {
      expect(
        validate({
          schemaVersion: 1,
          personalDetails: { fullName: "" },
          content: {},
          customization: fixture("minimal.json").customization,
        }),
      ).toBe(true);
    });

    it("accepts a freshly created section with no displayName/iconKey and no entries", () => {
      expect(
        validate({
          schemaVersion: 1,
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
          schemaVersion: 1,
          personalDetails: { fullName: "Ada Lovelace", details: [] },
          content: { work: { entries: [] } },
          customization: fixture("minimal.json").customization,
        }),
      ).toBe(false);
    });
  });

  // Phase-gate re-review finding NEW-M3: photo.key's pattern used to
  // include a negative lookahead ((?!.*\.\.)) to reject a ".." substring,
  // which is outside JSON Schema's portable regex subset and does not
  // compile under Go's RE2 engine (design spec §3 commits any future
  // generated Go pattern-validator to reading this same file). The
  // lookahead is gone; ".." rejection moved to validation/store.ts's
  // validatePhotoKeyTraversal / gen/go/store_validate.go's
  // ValidatePhotoKeyTraversal (see test/store-validation.test.ts).
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
      ["https://evil.example.com/x.jpg", false], // ":" not in the allowed character set
      ["a\nb.jpg", false],
      [".hidden.jpg", false], // first char must be alnum (NEW-M5)
      ["_x.jpg", false], // first char must be alnum (NEW-M5)
    ])("%s -> valid=%s", (key, expected) => {
      expect(validate(withPhotoKey(key))).toBe(expected);
    });

    // Documents the deliberate, reviewed trade-off (NOT a regression): the
    // schema layer alone now accepts a ".." traversal string that doesn't
    // ALSO trip the (unrelated, still-enforced) first-char-must-be-alnum
    // rule — the store layer is where ".." is actually rejected. See
    // test/store-validation.test.ts's "photo.key path-traversal guard"
    // block. ("../../other-user/secret.jpg" is NOT a useful case here: it
    // starts with ".", so the first-char anchor rejects it regardless of
    // the lookahead removal — "a/../b.jpg" isolates the lookahead's absence
    // specifically.)
    it("no longer rejects a mid-string \"..\" at the schema layer (moved to the store layer)", () => {
      expect(validate(withPhotoKey("a/../b.jpg"))).toBe(true);
    });
  });
});
