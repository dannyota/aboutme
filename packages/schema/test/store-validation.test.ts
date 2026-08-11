import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  MAX_RICH_TEXT_BYTES,
  utf8ByteLength,
  validateDateRange,
  validateDocument,
  validateEntryIdUniqueness,
  validateLayoutSections,
  validatePersonalDetailUrlSchemes,
  validatePhotoKeyTraversal,
  validateRichTextByteLength,
} from "../validation/store.ts";

const root = new URL("..", import.meta.url).pathname;
const fixture = (...parts: string[]) =>
  JSON.parse(readFileSync(join(root, "fixtures", ...parts), "utf8"));

// Issues by rule id, for readable assertions below.
const rules = (issues: { rule: string }[]) => issues.map((i) => i.rule);

describe("store-layer validator: clean documents produce zero issues", () => {
  // Every one of these is schema-valid AND store-valid — draft permissiveness
  // (design spec §3, revised 2026-08-01) must never trip these new rules on a
  // half-typed document.
  const cleanFixtures = [
    "minimal.json",
    "full.json",
    "draft-partial.json",
    "draft-cleared-name-empty-section.json",
    "draft-cleared-contact-value.json",
  ];

  for (const name of cleanFixtures) {
    it(`${name}`, () => {
      expect(validateDocument(fixture(name))).toEqual([]);
    });
  }
});

describe("store-layer validator: hole 1 — rich-text byte length (chars vs. bytes)", () => {
  it("rejects 9,000 'é' characters (9,000 code points, 18,000 UTF-8 bytes) — the reviewer's exact proof-of-concept", () => {
    const doc = fixture("store", "invalid-oversize-richtext-bytes.json");
    const text = doc.content.profile.entries[0].text;
    expect([...text].length).toBe(9000); // passes resume.schema.json's maxLength: 16384 (code points)
    expect(utf8ByteLength(text)).toBe(18000); // exceeds the real 16 KB byte limit

    const issues = validateDocument(doc);
    expect(rules(issues)).toContain("rich-text-byte-length");
    expect(issues[0].message).toContain(
      "18000 UTF-8 bytes exceeds the 16384-byte limit",
    );
  });

  it("validateRichTextByteLength accepts a string exactly at the byte limit and rejects one byte over", () => {
    expect(
      validateRichTextByteLength("a".repeat(MAX_RICH_TEXT_BYTES), "p"),
    ).toEqual([]);
    expect(
      validateRichTextByteLength("a".repeat(MAX_RICH_TEXT_BYTES + 1), "p"),
    ).toHaveLength(1);
  });

  it("accepts an ASCII string where code points and bytes coincide, up to the limit", () => {
    expect(validateDocument(fixture("full.json"))).toEqual([]);
  });
});

describe("store-layer validator: hole 2 — layout aggregate invariant", () => {
  it("rejects a section key placed in both main and sidebar (cross-array duplicate; uniqueItems alone cannot catch this)", () => {
    const issues = validateDocument(
      fixture("store", "invalid-layout-duplicate-across-arrays.json"),
    );
    expect(rules(issues)).toContain("layout-exactly-once");
  });

  it("rejects a layout entry referencing a section key missing from content", () => {
    const issues = validateDocument(
      fixture("store", "invalid-layout-missing-content-key.json"),
    );
    expect(rules(issues)).toContain("layout-missing-content-key");
  });

  it("rejects a content section that is placed in neither main nor sidebar", () => {
    const issues = validateDocument(
      fixture("store", "invalid-layout-orphan-content-key.json"),
    );
    expect(rules(issues)).toContain("layout-orphan-content-key");
  });

  it("validateLayoutSections accepts a document where every content key appears exactly once", () => {
    const doc = fixture("full.json");
    expect(
      validateLayoutSections(doc.content, doc.customization.layout.sections),
    ).toEqual([]);
  });

  // Phase-gate re-review finding M1 (integration-owner ruling): TS used to
  // iterate a Map in placement order while gen/go/store_validate.go iterated
  // sortedKeys/sortedIntKeys, so the two halves emitted the same 4 issues in
  // a different order for this exact document (the re-review's own
  // reproduction case). validateDocument's return-boundary sort
  // canonicalizes this — gen/go/store_validate_test.go's
  // TestValidateDocument_LayoutIssueOrderMatchesTS asserts the Go half
  // produces the SAME order for the SAME document.
  it("emits layout issues in a canonical (path, rule, message) order, not Map placement order", () => {
    const doc = {
      content: {
        zebra: { sectionType: "work", entries: [] },
        alpha: { sectionType: "work", entries: [] },
        orphan: { sectionType: "work", entries: [] },
      },
      customization: {
        layout: {
          sections: {
            main: ["zebra", "zebra", "missingOne"],
            sidebar: ["missingTwo", "alpha"],
          },
        },
      },
    };
    const issues = validateDocument(doc);
    expect(rules(issues)).toEqual([
      "layout-exactly-once",
      "layout-missing-content-key",
      "layout-missing-content-key",
      "layout-orphan-content-key",
    ]);
    // The two missing-content-key issues (same path, same rule) are
    // tie-broken by message text — "missingOne" < "missingTwo".
    expect(issues[1].message).toContain("missingOne");
    expect(issues[2].message).toContain("missingTwo");
  });
});

describe("store-layer validator: reversed date range (start > end)", () => {
  it("rejects a work entry whose start year is after its end year — currently passes resume.schema.json (JSON Schema cannot compare sibling values)", () => {
    const issues = validateDocument(
      fixture("store", "invalid-reversed-date-range.json"),
    );
    expect(rules(issues)).toContain("date-range-order");
  });

  it("validateDateRange accepts start === end and start < end, and ignores an open (present: true, end: null) range", () => {
    expect(
      validateDateRange(
        { start: { y: 2020 }, end: { y: 2020 }, present: false },
        "p",
      ),
    ).toEqual([]);
    expect(
      validateDateRange(
        { start: { y: 2020 }, end: { y: 2022 }, present: false },
        "p",
      ),
    ).toEqual([]);
    expect(
      validateDateRange({ start: { y: 2020 }, end: null, present: true }, "p"),
    ).toEqual([]);
  });

  it("compares by month within the same year", () => {
    const issues = validateDateRange(
      { start: { y: 2020, m: 6 }, end: { y: 2020, m: 3 }, present: false },
      "p",
    );
    expect(issues).toHaveLength(1);
    expect(issues[0].message).toContain("2020-06");
    expect(issues[0].message).toContain("2020-03");
  });
});

describe("store-layer validator: entry-id uniqueness across the whole resume (AC-DOC-002)", () => {
  it("rejects the same entry id reused in a different section — a single section's uniqueItems could never catch this", () => {
    const issues = validateDocument(
      fixture("store", "invalid-duplicate-entry-id.json"),
    );
    expect(rules(issues)).toContain("duplicate-entry-id");
    // One issue per occurrence, sorted by path — "content.skill..." < "content.work...".
    const duplicateIssues = issues.filter((i) => i.rule === "duplicate-entry-id");
    // Exact message text (not just a substring): the two halves' message
    // text is part of the store layer's cross-language parity contract —
    // gen/go/store_validate_test.go's TestValidateDocument_DuplicateEntryID
    // asserts these same two strings.
    expect(duplicateIssues).toEqual([
      {
        rule: "duplicate-entry-id",
        path: "content.skill.entries[0].id",
        message:
          'content.skill.entries[0].id: entry id "dd89bd8a-ba7d-4bec-9c43-f1b296c56fac" is not unique across the whole resume — also used at content.work.entries[0].id',
      },
      {
        rule: "duplicate-entry-id",
        path: "content.work.entries[0].id",
        message:
          'content.work.entries[0].id: entry id "dd89bd8a-ba7d-4bec-9c43-f1b296c56fac" is not unique across the whole resume — also used at content.skill.entries[0].id',
      },
    ]);
  });

  it("accepts the same document shape when every entry id is unique", () => {
    expect(
      validateDocument(fixture("store", "valid-unique-entry-id.json")),
    ).toEqual([]);
  });

  it("validateEntryIdUniqueness flags every occurrence of a 3-way duplicate id, not just the first repeat", () => {
    const doc = {
      content: {
        a: { sectionType: "work", entries: [{ id: "dup" }] },
        b: { sectionType: "skill", entries: [{ id: "dup" }] },
        c: { sectionType: "language", entries: [{ id: "dup" }] },
      },
    };
    const issues = validateEntryIdUniqueness(doc.content);
    expect(issues).toEqual([
      {
        rule: "duplicate-entry-id",
        path: "content.a.entries[0].id",
        message:
          'content.a.entries[0].id: entry id "dup" is not unique across the whole resume — also used at content.b.entries[0].id, content.c.entries[0].id',
      },
      {
        rule: "duplicate-entry-id",
        path: "content.b.entries[0].id",
        message:
          'content.b.entries[0].id: entry id "dup" is not unique across the whole resume — also used at content.a.entries[0].id, content.c.entries[0].id',
      },
      {
        rule: "duplicate-entry-id",
        path: "content.c.entries[0].id",
        message:
          'content.c.entries[0].id: entry id "dup" is not unique across the whole resume — also used at content.a.entries[0].id, content.b.entries[0].id',
      },
    ]);
  });

  // Minor 5: the case this rule's own doc comment names as the one
  // uniqueItems could never cover — two entries in the SAME section's own
  // entries array sharing an id — was previously untested in both
  // languages. gen/go/store_validate_test.go has the mirror case.
  it("rejects the same id used twice within a single section's own entries array", () => {
    const doc = {
      content: {
        w: { sectionType: "work", entries: [{ id: "dup" }, { id: "dup" }] },
      },
    };
    const issues = validateEntryIdUniqueness(doc.content);
    expect(issues).toEqual([
      {
        rule: "duplicate-entry-id",
        path: "content.w.entries[0].id",
        message:
          'content.w.entries[0].id: entry id "dup" is not unique across the whole resume — also used at content.w.entries[1].id',
      },
      {
        rule: "duplicate-entry-id",
        path: "content.w.entries[1].id",
        message:
          'content.w.entries[1].id: entry id "dup" is not unique across the whole resume — also used at content.w.entries[0].id',
      },
    ]);
  });

  // Important 2: the message must stay bounded even when an id is shared by
  // far more than a handful of entries. resume.schema.json permits up to
  // content.maxProperties=24 sections, so 50 sections sharing one id is a
  // realistic stand-in for "many, not just a couple" without approaching
  // the full 24 × entries.maxItems=64 = 1536-entry worst case this bound
  // exists for.
  it("caps the interpolated 'also used at' list and summarizes the rest as 'and N more'", () => {
    const sectionCount = 50;
    const content: Record<string, { sectionType: string; entries: { id: string }[] }> = {};
    for (let i = 0; i < sectionCount; i++) {
      const key = `s${String(i).padStart(2, "0")}`;
      content[key] = { sectionType: "work", entries: [{ id: "dup" }] };
    }

    const issues = validateEntryIdUniqueness(content);
    expect(issues).toHaveLength(sectionCount);

    const first = issues[0];
    expect(first.path).toBe("content.s00.entries[0].id");
    expect(first.message).toBe(
      'content.s00.entries[0].id: entry id "dup" is not unique across the whole resume — also used at content.s01.entries[0].id, content.s02.entries[0].id, content.s03.entries[0].id, and 46 more',
    );

    const last = issues[sectionCount - 1];
    expect(last.path).toBe("content.s49.entries[0].id");
    expect(last.message).toBe(
      'content.s49.entries[0].id: entry id "dup" is not unique across the whole resume — also used at content.s00.entries[0].id, content.s01.entries[0].id, content.s02.entries[0].id, and 46 more',
    );

    // The whole point: message length must not scale with the number of
    // occurrences (49 others here) — every message stays short and bounded.
    for (const issue of issues) {
      expect(issue.message.length).toBeLessThan(220);
    }
  });

  it("validateEntryIdUniqueness accepts an empty or undefined content map without throwing", () => {
    expect(validateEntryIdUniqueness(undefined)).toEqual([]);
    expect(validateEntryIdUniqueness({})).toEqual([]);
  });
});

// Phase-gate review finding I1: validateDocument used to throw uncaught
// TypeErrors on inputs Go's ValidateDocument handles cleanly, because every
// existing fixtures/store/*.json entry was schema-VALID by construction — no
// fixture was malformed enough to reach either crash path. These hostile
// fixtures are deliberately schema-INVALID (ajv would reject them) so the
// class of bug is actually exercised, not just the two functions in
// isolation below.
describe("store-layer validator: hostile inputs are handled without throwing (I1)", () => {
  it.each([
    "invalid-hostile-sectiontype-constructor.json",
    "invalid-hostile-sectiontype-proto.json",
    "invalid-hostile-sectiontype-hasownproperty.json",
  ])(
    "%s: a sectionType shadowing Object.prototype does not crash the rich-text lookup",
    (name) => {
      const doc = fixture("store", name);
      expect(() => validateDocument(doc)).not.toThrow();
      // Same tolerance as any other unrecognized sectionType: no rich-text
      // field table entry, so nothing to check — zero issues, not a crash.
      expect(validateDocument(doc)).toEqual([]);
    },
  );

  it("invalid-missing-dates-start.json: a dates object missing start does not crash date-range ordering", () => {
    const doc = fixture("store", "invalid-missing-dates-start.json");
    expect(() => validateDocument(doc)).not.toThrow();
    // Mirrors Go's tolerance: Start is a zero-value {y:0} there (ordinal 1),
    // which is never > a real end date, so no date-range-order issue either.
    expect(rules(validateDocument(doc))).not.toContain("date-range-order");
  });

  it("validateDateRange itself tolerates a missing/malformed start without throwing", () => {
    expect(
      validateDateRange(
        { end: { y: 2020, m: 6 }, present: false } as never,
        "p",
      ),
    ).toEqual([]);
    expect(
      validateDateRange({ start: null, end: { y: 2020 }, present: false } as never, "p"),
    ).toEqual([]);
  });

  // Blanket regression guard (item 2's "never exceptions" requirement):
  // every fixture in the shared corpus, valid or hostile, must be handled by
  // validateDocument without throwing — not just the specific cases above.
  const storeFixtureNames = readdirSync(join(root, "fixtures", "store"));
  it.each(storeFixtureNames)(
    "%s is handled by validateDocument without throwing",
    (name) => {
      const doc = fixture("store", name);
      expect(() => validateDocument(doc)).not.toThrow();
    },
  );
});

// Phase-gate re-review finding NEW-I1: the fixture sweep above can only ever
// prove "well-formed JSON documents don't crash the validator" — every file
// under fixtures/store/ has array-shaped `entries`/`details`/`main`/
// `sidebar` fields, because that's the only shape either half's fixture
// LOADER can parse into something meaningful in the first place. A document
// where an array-shaped FIELD instead carries a non-array value (an object,
// a string, a number) is not something a shared fixture can express — Go's
// typed decode would reject it before ever reaching ValidateDocument, so
// there is no way to author a *.json file that is simultaneously "loadable
// by loadResumeFixture" and "has entries:{a:1}". These cases are therefore
// built inline, TS-only, exactly as the re-review constructed them — this is
// the "structurally malformed input" class `?? []` alone does not catch
// (only null/undefined), which toArray() in validation/store.ts now does.
describe("store-layer validator: structurally malformed documents do not throw (NEW-I1)", () => {
  it("a section's entries carrying a non-array object does not crash rich-text-length or date-range checks", () => {
    const doc = {
      content: { w: { sectionType: "work", entries: { a: 1 } } },
    };
    expect(() => validateDocument(doc as never)).not.toThrow();
    // Not a crash, but also nothing to iterate: the malformed entries value
    // contributes zero rich-text-length / date-range-order issues (the
    // unrelated layout-orphan-content-key issue below is expected — "w" is
    // a real content key this minimal doc never places in a layout — and
    // proves the rest of validateDocument still runs to completion).
    const issues = validateDocument(doc as never);
    expect(rules(issues)).not.toContain("rich-text-byte-length");
    expect(rules(issues)).not.toContain("date-range-order");
    expect(rules(issues)).toEqual(["layout-orphan-content-key"]);
  });

  it("layout.sections.main carrying a non-array object does not crash the layout invariant", () => {
    const doc = {
      content: {},
      customization: { layout: { sections: { main: { a: 1 }, sidebar: [] } } },
    };
    expect(() => validateDocument(doc as never)).not.toThrow();
  });

  it.each([{ a: 1 }, 5, "x", true])(
    "personalDetails.details carrying %j (not an array) does not crash the URL-scheme check",
    (malformedDetails) => {
      const doc = { personalDetails: { details: malformedDetails } };
      expect(() => validateDocument(doc as never)).not.toThrow();
      expect(validateDocument(doc as never)).toEqual([]);
      expect(
        validatePersonalDetailUrlSchemes({ details: malformedDetails } as never),
      ).toEqual([]);
    },
  );

  it.each([null, undefined, 5, "x", true, []])(
    "validateDocument(%j) — a non-object top-level document — returns [] instead of throwing",
    (malformedDoc) => {
      expect(() => validateDocument(malformedDoc as never)).not.toThrow();
      expect(validateDocument(malformedDoc as never)).toEqual([]);
    },
  );
});

// Phase-gate review finding C1 follow-up: resume.schema.json's personalDetail
// $def now restricts `value` to https:// (or "") when `type` is one of
// website/linkedin/github/twitter — but that check is ajv-only
// (TypeScript). Go has no JSON-Schema pattern validator at all (gen/go's
// PersonalDetail.Value is a bare, unconstrained string), so without this
// store-layer rule a document that reaches Go's write path without first
// passing through ajv would let a hostile scheme straight into
// content.details and, from there, ResumeHeader's contact-chip href (design
// spec §5). This is the one rule in this file that duplicates something
// JSON Schema COULD already express — see validatePersonalDetailUrlSchemes's
// own comment in validation/store.ts for why.
describe("store-layer validator: personal detail URL-scheme hardening (C1 follow-up — Go has no ajv equivalent)", () => {
  it("rejects a hostile scheme on a website/linkedin/github/twitter chip", () => {
    const issues = validateDocument(
      fixture("store", "invalid-personal-detail-url-scheme.json"),
    );
    expect(rules(issues)).toContain("personal-detail-url-scheme");
    expect(issues).toHaveLength(1); // only the hostile detail (index 0), not the clean linkedin one (index 1)
    expect(issues[0].path).toBe("personalDetails.details[0].value");
  });

  it.each([
    ["website", "javascript:alert(document.cookie)", false],
    ["website", "data:text/html,<script>alert(1)</script>", false],
    ["website", "//evil.example.com", false],
    ["linkedin", "javascript:alert(1)", false],
    ["github", "vbscript:msgbox(1)", false],
    ["twitter", "JavaScript:alert(1)", false],
    ["website", "mailto:ada@example.com", false], // type-appropriate subset: https-only, not mailto/tel
    ["website", "https://ada.example.com", true],
    ["linkedin", "", true], // draft-permissive: explicitly cleared
    ["email", "javascript:alert(1)", true], // out of scope type (item 1): unaffected
  ])("type=%s value=%s -> valid=%s", (type, value, expectValid) => {
    const issues = validatePersonalDetailUrlSchemes({
      details: [{ type, value }],
    });
    expect(issues.length === 0).toBe(expectValid);
  });

  // Phase-gate re-review finding NEW-M1: an out-of-enum `type` used to
  // bypass this rule entirely (DETAIL_TYPES_REQUIRING_HTTPS.has(type) is
  // false for anything not exactly one of the four lowercase URL types, so
  // the whole check was skipped) — exactly the "reaches Go's write path
  // without first passing through ajv" scenario this rule exists to guard,
  // since that same document also never saw ajv's `type: enum` check. The
  // fix is fail-closed: only the four KNOWN non-URL types (email, phone,
  // location, custom) are exempt; everything else, including a perturbed or
  // missing `type`, is held to the https-or-"" rule.
  it.each([
    ["WEBSITE", "javascript:alert(1)"], // wrong case
    ["url", "javascript:alert(1)"], // out of enum entirely
    ["", "javascript:alert(1)"], // empty string
    [undefined, "javascript:alert(1)"], // missing type key
  ])(
    "type=%s no longer bypasses the https requirement (was accepted before NEW-M1)",
    (type, value) => {
      const issues = validatePersonalDetailUrlSchemes({
        details: [{ type, value }],
      });
      expect(issues.length).toBeGreaterThan(0);
    },
  );

  // The four exempt types must stay genuinely unconstrained — the
  // fail-closed default above must not accidentally widen to cover them.
  it.each(["email", "phone", "location", "custom"])(
    "type=%s stays unconstrained (design spec defines no value format for it)",
    (type) => {
      const issues = validatePersonalDetailUrlSchemes({
        details: [{ type, value: "javascript:alert(1)" }],
      });
      expect(issues).toEqual([]);
    },
  );
});

// Phase-gate re-review finding NEW-M3: resume.schema.json's photo.key
// pattern used to reject a ".." substring via a negative lookahead, which
// does not compile under Go's RE2 engine. The lookahead was removed from
// the schema pattern (which now only enforces the safe-character-class +
// first-char anchor, both RE2-portable); this store-layer rule is the new
// home for the ".." rejection.
describe("store-layer validator: photo.key path-traversal guard (NEW-M3)", () => {
  it.each([
    "../../other-user/secret.jpg",
    "a/../b.jpg",
    "a..b.jpg",
    "resumes/../etc/passwd",
  ])("rejects a photo.key containing \"..\": %s", (key) => {
    const issues = validatePhotoKeyTraversal({ photo: { key } });
    expect(issues).toHaveLength(1);
    expect(issues[0].rule).toBe("photo-key-path-traversal");
    expect(issues[0].path).toBe("personalDetails.photo.key");
  });

  it("accepts a normal photo.key with no \"..\"", () => {
    expect(
      validatePhotoKeyTraversal({
        photo: { key: "resumes/ada/photo-original.jpg" },
      }),
    ).toEqual([]);
  });

  it("tolerates an absent photo/key without throwing", () => {
    expect(validatePhotoKeyTraversal(undefined)).toEqual([]);
    expect(validatePhotoKeyTraversal({})).toEqual([]);
    expect(validatePhotoKeyTraversal({ photo: {} })).toEqual([]);
  });

  it("is wired into validateDocument", () => {
    const doc = {
      personalDetails: { photo: { key: "../../other-user/secret.jpg" } },
    };
    const issues = validateDocument(doc);
    expect(rules(issues)).toContain("photo-key-path-traversal");
  });
});

// AC-DOC-009 traceability gap (docs/plans/traceability/ac-doc.md, design spec
// §3: "Fixtures must cover a cleared name, cleared contact values, and a
// freshly created empty section"). draft-cleared-name-empty-section.json
// covers the first and third; until this fixture, nothing covered a
// personalDetails.details ENTRY that is present with its own `value`
// explicitly cleared to "" -- distinct from an absent details array
// (draft-cleared-name-empty-section.json) or a details array that was never
// populated. The cleanFixtures loop above already proves this fixture trips
// no store-layer rule; this block additionally proves the entry itself
// survives JSON.parse exactly as authored -- present, not dropped, and not
// fabricated back to some other value -- so the "preserved" half of AC-DOC-009
// is asserted directly, not merely inferred from a zero-issues result.
describe("store-layer validator: a present personal-detail entry with an explicitly cleared value (AC-DOC-009)", () => {
  it('draft-cleared-contact-value.json preserves the entry with value === "", not absent or fabricated', () => {
    const doc = fixture("draft-cleared-contact-value.json");
    expect(doc.personalDetails.details).toHaveLength(1);
    expect(doc.personalDetails.details[0].value).toBe("");
    expect(doc.personalDetails.details[0].type).toBe("linkedin");
    expect(validateDocument(doc)).toEqual([]);
  });
});
