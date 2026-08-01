import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { afterAll, beforeAll, describe, expect, it } from "vitest";

// This test exists because gen.test.ts's byte-compare drift check only
// proves the generator is *deterministic*, not *faithful* to the schema
// (design spec §3, "Codegen fidelity"): scripts/generate.mjs used to
// hardcode the eight entry $defs it fed to quicktype, so a ninth
// sectionType added to the schema would be silently dropped from Go on
// every regeneration — reproducing byte-identically, since the hardcoded
// list never changed. AJV and the generated TS union would both still be
// correct (both are schema-driven already: AJV validates the real file
// directly, and json-schema-to-typescript reads oneOf natively), so the gap
// was Go-only and easy to miss.
//
// scripts/generate.mjs now derives its entry-def list from
// $defs.section.oneOf instead (deriveSectionVariants) — but that alone only
// fixes *generated* Go (gen/go/resume.go). gen/go/section.go's dispatch
// layer (Section.Validate/MarshalJSON/UnmarshalJSON) is hand-written and
// switches on SectionType explicitly; nothing forces it to grow a case for
// a new sectionType just because the schema and gen/go/resume.go now know
// about one. This test is what catches that: it enumerates every
// sectionType from the schema *independently* of generate.mjs (deliberately
// not importing deriveSectionVariants — the point is to verify the pipeline
// against the schema, not trust the same derivation the pipeline uses), and
// for each one asserts all three languages actually handle it — both that
// they *accept* a valid sample of their own sectionType, and that they
// *reject* an entry carrying a field foreign to it (design spec §3,
// "Codegen fidelity" requires both directions: acceptance alone doesn't
// prove a language enforces the entry-shape boundary, only that it doesn't
// crash). The one documented exception — languageEntry's fields are an
// exact subset of skillEntry's, so a language-shaped entry under
// sectionType "skill" is structurally indistinguishable from a partial
// skill entry and is accepted by design, not rejected — is asserted
// separately at the bottom of this file, so it doesn't collide with the
// generic per-sectionType rejection loop below (which always picks a field
// genuinely foreign to the type under test, never the skill/language pair).
const root = new URL("..", import.meta.url).pathname;
const schema = JSON.parse(readFileSync(join(root, "resume.schema.json"), "utf8"));
const ajv = addFormats(new Ajv2020({ allErrors: true, strict: true }));
const validate = ajv.compile(schema);
const tsSource = readFileSync(join(root, "gen/ts/resume.ts"), "utf8");

function sectionTypesFromSchema(): string[] {
  const oneOf = schema.$defs?.section?.oneOf;
  if (!Array.isArray(oneOf) || oneOf.length === 0) {
    throw new Error("conformance.test.ts: $defs.section.oneOf is missing or empty");
  }
  return oneOf.map((branch: unknown, index: number) => {
    const sectionType = (branch as { properties?: { sectionType?: { const?: unknown } } })?.properties
      ?.sectionType?.const;
    if (typeof sectionType !== "string") {
      throw new Error(`conformance.test.ts: $defs.section.oneOf[${index}] has no properties.sectionType.const`);
    }
    return sectionType;
  });
}

// sectionType -> { fieldName: fieldSchema }, read straight from each entry
// $def's non-entryBase allOf branch (entryBase's branch is a bare $ref with
// no "properties" of its own, so it's naturally excluded — this only
// collects domain fields, not id/isHidden).
function entryFieldSchemasFromSchema(): Record<string, Record<string, unknown>> {
  const oneOf = schema.$defs?.section?.oneOf as Array<{
    properties: { sectionType: { const: string }; entries: { items: { $ref: string } } };
  }>;
  const result: Record<string, Record<string, unknown>> = {};
  for (const branch of oneOf) {
    const sectionType = branch.properties.sectionType.const;
    const defKey = branch.properties.entries.items.$ref.replace(/^#\/\$defs\//, "");
    const def = schema.$defs[defKey] as { allOf?: Array<{ properties?: Record<string, unknown> }> };
    const fields: Record<string, unknown> = {};
    for (const sub of def.allOf ?? []) {
      if (sub && typeof sub === "object" && sub.properties) {
        Object.assign(fields, sub.properties);
      }
    }
    result[sectionType] = fields;
  }
  return result;
}

// Picks a field genuinely foreign to `sectionType`: present on some other
// entry type, absent from this one. Deterministic (first match in schema
// order) — never the skill/language pair specifically, since it always
// finds *some* field the target type doesn't declare at all, and the
// skill/language relationship is a subset (every language field IS a valid
// skill field), not a foreign one.
function foreignFieldFor(
  sectionType: string,
  entryFields: Record<string, Record<string, unknown>>,
): { field: string; fieldSchema: unknown; fromType: string } {
  const ownFields = entryFields[sectionType];
  for (const [otherType, fields] of Object.entries(entryFields)) {
    if (otherType === sectionType) continue;
    for (const [field, fieldSchema] of Object.entries(fields)) {
      if (!(field in ownFields)) {
        return { field, fieldSchema, fromType: otherType };
      }
    }
  }
  throw new Error(`conformance.test.ts: no field foreign to sectionType "${sectionType}" found among the others`);
}

function sampleValueForFieldSchema(fieldSchema: unknown): unknown {
  let resolved = fieldSchema as { $ref?: string; type?: string };
  if (resolved?.$ref) {
    const key = resolved.$ref.replace(/^#\/\$defs\//, "");
    resolved = schema.$defs[key];
  }
  switch (resolved?.type) {
    case "integer":
    case "number":
      return 1;
    case "boolean":
      return true;
    default:
      return "foreign-value";
  }
}

function minimalSectionPayload(sectionType: string) {
  return {
    sectionType,
    displayName: "Section",
    iconKey: "star",
    entries: [{ id: "018f0000-0000-7000-8000-000000000099" }],
  };
}

function minimalValidResume(sectionType: string) {
  return {
    schemaVersion: 1,
    personalDetails: { fullName: "Ada Lovelace", details: [] },
    content: { [sectionType]: minimalSectionPayload(sectionType) },
    customization: {
      font: { family: "Inter", baseSizePx: 14 },
      colors: { primary: "#1a1a1a", text: "#1a1a1a", background: "#ffffff" },
      spacing: { sectionGap: 16, entryGap: 8, lineHeight: 1.4 },
      heading: { style: "normal", showRule: false },
      layout: { columns: 1, sections: { main: [sectionType], sidebar: [] } },
      sectionDisplay: { skill: { style: "text" }, language: { style: "text" } },
      pageFormat: "a4",
      dateFormat: "MM/YYYY",
    },
  };
}

let tmpDir: string;
let conformanceBinary: string;

beforeAll(() => {
  tmpDir = mkdtempSync(join(tmpdir(), "aboutme-schema-conformance-"));
  conformanceBinary = join(tmpDir, "conformance-check");
  // Built once, run once per sectionType below — the point is to run the
  // real gen/go/section.go dispatch logic, not reimplement it in JS.
  execFileSync("go", ["build", "-o", conformanceBinary, "./cmd/conformance"], {
    cwd: join(root, "gen/go"),
    stdio: "inherit",
  });
  // This hook compiles a Go binary from scratch. On a warm build cache
  // (local dev) that is near-instant, but on CI's cold cache the first
  // `go build` of the conformance command and its deps routinely exceeds
  // vitest's default 10s hook timeout — a determinism bug, not a slow test.
  // Give the one-time compile enough headroom to finish deterministically.
}, 120_000);

afterAll(() => {
  rmSync(tmpDir, { recursive: true, force: true });
});

// Writes a throwaway .ts fixture asserting `foreignField` is a compile
// error on `sectionType`'s entries, runs `tsc --noEmit --strict` on it, and
// cleans up. Mirrors type-fidelity.fixture.ts's `@ts-expect-error` pattern:
// that directive itself errors if the following line does NOT error, so a
// clean tsc run here proves the rejection is real, not just an unused,
// unverified comment.
function assertTsRejectsForeignField(sectionType: string, foreignField: string, foreignValue: unknown, fromType: string) {
  const fixturePath = join(root, "test", `conformance-reject-${sectionType}.tmp.ts`);
  const source = [
    `import type { Section } from "../gen/ts/resume";`,
    ``,
    `const section: Section = {`,
    `  sectionType: ${JSON.stringify(sectionType)},`,
    `  displayName: "Section",`,
    `  iconKey: "star",`,
    `  entries: [`,
    `    {`,
    `      id: "018f0000-0000-7000-8000-000000000099",`,
    `      // @ts-expect-error: ${foreignField} belongs to the "${fromType}" entry type, not "${sectionType}".`,
    `      ${foreignField}: ${JSON.stringify(foreignValue)},`,
    `    },`,
    `  ],`,
    `};`,
    `void section;`,
    ``,
  ].join("\n");

  writeFileSync(fixturePath, source);
  try {
    expect(() =>
      execFileSync("node_modules/.bin/tsc", ["--noEmit", "--strict", fixturePath], { stdio: "pipe" }),
    ).not.toThrow();
  } finally {
    rmSync(fixturePath, { force: true });
  }
}

describe("codegen conformance: every sectionType across ajv, TS, and Go", () => {
  const sectionTypes = sectionTypesFromSchema();
  const entryFields = entryFieldSchemasFromSchema();

  it("the schema declares at least one sectionType", () => {
    expect(sectionTypes.length).toBeGreaterThan(0);
  });

  for (const sectionType of sectionTypes) {
    const { field: foreignField, fieldSchema: foreignFieldSchema, fromType } = foreignFieldFor(sectionType, entryFields);
    const foreignValue = sampleValueForFieldSchema(foreignFieldSchema);

    describe(`sectionType ${JSON.stringify(sectionType)}`, () => {
      it("ajv accepts a minimal valid sample", () => {
        const doc = minimalValidResume(sectionType);
        expect(validate(doc), ajv.errorsText(validate.errors)).toBe(true);
      });

      it("the generated TS union has a matching variant", () => {
        expect(tsSource).toContain(`sectionType: "${sectionType}";`);
      });

      it("the Go dispatch (gen/go/section.go) decodes it", () => {
        const payload = JSON.stringify(minimalSectionPayload(sectionType));
        let output: string;
        try {
          output = execFileSync(conformanceBinary, { input: payload }).toString();
        } catch (err) {
          const e = err as { stderr?: Buffer; message: string };
          throw new Error(
            `Go dispatch rejected sectionType ${JSON.stringify(sectionType)}: ${e.stderr?.toString() ?? e.message}`,
          );
        }
        expect(output.trim()).toBe("OK");
      });

      it(`ajv rejects an entry carrying a field foreign to it ("${foreignField}", valid on "${fromType}")`, () => {
        const doc = minimalValidResume(sectionType) as {
          content: Record<string, { entries: unknown[] }>;
        };
        doc.content[sectionType].entries = [
          { id: "018f0000-0000-7000-8000-000000000099", [foreignField]: foreignValue },
        ];
        expect(validate(doc)).toBe(false);
      });

      it(`the generated TS union rejects that same foreign field at compile time`, () => {
        assertTsRejectsForeignField(sectionType, foreignField, foreignValue, fromType);
      });

      it("the Go dispatch (gen/go/section.go) rejects an entry carrying a foreign field", () => {
        const payload = JSON.stringify({
          sectionType,
          displayName: "Section",
          iconKey: "star",
          entries: [{ id: "018f0000-0000-7000-8000-000000000099", [foreignField]: foreignValue }],
        });
        let stderr = "";
        let threw = false;
        try {
          execFileSync(conformanceBinary, { input: payload });
        } catch (err) {
          threw = true;
          stderr = (err as { stderr?: Buffer }).stderr?.toString() ?? "";
        }
        expect(threw, "expected the Go dispatch to reject a foreign field, but it accepted the entry").toBe(true);
        expect(stderr).toContain(foreignField);
      });
    });
  }
});

// Documented exception (gen/go/section.go's package doc,
// resume.schema.json's languageEntry description): languageEntry's fields
// ({name, level}) are an exact subset of skillEntry's ({name, level,
// infoHtml}), so a language-shaped entry is ALSO a valid partial skill
// entry — there is no field-based check, foreign or otherwise, that can
// reject it, because nothing about the entry itself is invalid. Asserted
// here as an explicit acceptance (not a rejection) so this file states the
// exception plainly, next to the generic rejection loop above that would
// otherwise look like it implies skill/language should reject each other.
describe('documented exception: a language-shaped entry under sectionType "skill" is accepted, not rejected', () => {
  const languageShapedEntry = { id: "018f0000-0000-7000-8000-000000000099", name: "English", level: 5 };

  it("ajv accepts it", () => {
    const doc = minimalValidResume("skill") as { content: Record<string, { entries: unknown[] }> };
    doc.content.skill.entries = [languageShapedEntry];
    expect(validate(doc), ajv.errorsText(validate.errors)).toBe(true);
  });

  it("the Go dispatch (gen/go/section.go) accepts it", () => {
    const payload = JSON.stringify({
      sectionType: "skill",
      displayName: "Skills",
      iconKey: "star",
      entries: [languageShapedEntry],
    });
    const output = execFileSync(conformanceBinary, { input: payload }).toString();
    expect(output.trim()).toBe("OK");
  });
});
