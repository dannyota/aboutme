#!/usr/bin/env node
// Regenerates packages/schema/gen/{go,ts}/resume.{go,ts} from
// resume.schema.json. Run: npm run generate (from packages/schema), or
// `make schema-gen` from the repo root.
//
// TypeScript uses json-schema-to-typescript (jstt): it understands `$ref`,
// `allOf`, and `required`/optional natively and emits `oneOf` as a real
// discriminated union, so `Section` comes out as eight distinct variants
// (`{sectionType: "work"; entries: WorkEntry[]}` | ...). Entries themselves
// are draft-permissive (design spec §3, revised 2026-08-01): every domain
// field is optional, only `id` is required, so each entry type's fields are
// all `field?: T` except `id: Uuid`.
//
// Go still uses quicktype: it has no discriminated-union output for Go
// (nothing does — Go has no sum types), so `content`'s eight-variant oneOf
// is generated as eight separate named structs (ProfileEntry, WorkEntry,
// ...) and section.go (hand-written, NOT generated — see that file) adds a
// dispatch layer on top: a `Section` type with one typed slice per
// sectionType (WorkEntries []WorkEntry, ...), of which exactly one is
// populated, plus MarshalJSON/UnmarshalJSON translating to/from the wire
// format's single "entries" array. `gen/go/resume.go` generates every type
// `section.go` builds on (the 8 entry structs, SectionType, and the shared
// envelope types) but not `Section` itself.
//
// Determinism: both tools' raw output is byte-identical across repeated
// runs on an unchanged input (verified empirically — no timestamps, no
// random ordering), so no output-side normalization pass is needed. What
// *does* need help is each tool's understanding of the schema itself; see
// buildSharedCodegenSchema and buildGoCodegenSchema below for what's
// adjusted and why.
//
// Codegen fidelity (design spec §3, "Codegen fidelity" row): every
// discriminator and entry $def Go generates is *derived from the schema at
// generation time* by deriveSectionVariants, below — reading
// $defs.section.oneOf, never a hardcoded list. A hardcoded list would
// reproduce identically on every regeneration even if it silently omitted a
// sectionType the schema declares, since "regenerate and byte-compare"
// (test/gen.test.ts) only proves the generator is deterministic, not that
// it's faithful to the schema — a hardcoded omission IS deterministic.
// test/conformance.test.ts is the faithfulness check: it enumerates every
// sectionType from the schema independently and asserts ajv, the generated
// TS union, and the Go dispatch (gen/go/section.go, hand-written — see that
// file) each handle it.
//
// Investigated and ruled out, in case someone re-treads this (all against
// quicktype 26.0.0 / json-schema-to-typescript 15.0.4, resume.schema.json
// as of this script's last edit — Go's map[string]<oneOf> collapse is the
// hard problem here; jstt sidesteps it entirely by emitting a real union):
//   - `quicktype --no-combine-classes` and `--no-maps`, on the unmodified
//     schema, hoping either would stop the entries item type from
//     collapsing when content's map value fans out over 8 different entry
//     $defs: no effect on either flag, output identical to the baseline
//     (an empty `type EntryElement struct{}`). The collapse happens where
//     quicktype unifies 8 different item types into one Go map-value type;
//     neither flag touches that step.
//   - Feeding quicktype each entry $def as its own top-level input (`-S
//     resume.schema.json profileEntry.schema.json workEntry.schema.json
//     ...`, each pointer file `{"$ref": "<$id>#/$defs/workEntry"}`) without
//     first stripping `unevaluatedProperties`: still every generated struct
//     came out empty — confirms the empty-struct bug is really about
//     `unevaluatedProperties` (fix 1 below), independent of the map
//     collapse, since it reproduces even for a single isolated entry type
//     with no map/oneOf involved.
//   - Naming those same per-$def pointer files `<key>.schema.json` (e.g.
//     `workEntry.schema.json`): quicktype named the resulting Go type
//     `WorkEntrySchema`, not `WorkEntry` — for a positional multi-file
//     input, quicktype names the top level after the *file's own
//     basename*, ignoring the referenced $def's `title` (title only wins
//     when quicktype reaches the same $def by walking properties from a
//     single main schema file, e.g. YearMonth/DateRange below). Renaming
//     the pointer files to `<DesiredName>.json` (no `.schema` segment)
//     fixed it — see generateGo's pointerFiles construction.

import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { compile as compileTypeScript } from "json-schema-to-typescript";

const packageRoot = dirname(dirname(fileURLToPath(import.meta.url)));
const schemaPath = join(packageRoot, "resume.schema.json");
const quicktypeBin = join(packageRoot, "node_modules", ".bin", "quicktype");

const GENERATED_HEADER = "// Code generated from resume.schema.json. DO NOT EDIT.";
const GENERATED_HEADER_TS = "// Code generated from resume.schema.json. DO NOT EDIT.\n";

// The $def backing the (otherwise-unreferenced) SectionType enum — see
// deriveSectionVariants for the per-sectionType entry list, which is
// derived from the schema rather than named here.
const SECTION_TYPE_DEF = ["sectionType", "SectionType"];

function toPascalCase(key) {
  return key.charAt(0).toUpperCase() + key.slice(1);
}

// Derives the entry $def backing each sectionType straight from the
// schema's own `$defs.section.oneOf` — never a hardcoded list (design spec
// §3, "Codegen fidelity"; see this file's header comment). A hardcoded list
// would silently and *deterministically* reproduce an omitted sectionType
// on every regeneration; deriving from the schema means a 9th oneOf branch
// is picked up automatically the next time this script runs, and
// test/conformance.test.ts independently enumerates the same oneOf to
// prove gen/go/section.go's hand-written dispatch was updated to match.
//
// Returns variants ordered as they appear in the schema (also their
// declaration order in gen/go/resume.go), and throws if $defs.section.oneOf
// disagrees with $defs.sectionType's enum — an inconsistency in the schema
// itself, not something codegen should silently paper over.
function deriveSectionVariants(schema) {
  const oneOf = schema.$defs?.section?.oneOf;
  if (!Array.isArray(oneOf) || oneOf.length === 0) {
    throw new Error("generate.mjs: resume.schema.json's $defs.section.oneOf is missing or empty.");
  }

  const variants = oneOf.map((branch, index) => {
    const sectionType = branch?.properties?.sectionType?.const;
    const itemsRef = branch?.properties?.entries?.items?.$ref;
    if (typeof sectionType !== "string" || typeof itemsRef !== "string") {
      throw new Error(
        `generate.mjs: $defs.section.oneOf[${index}] doesn't have the expected shape ` +
          "(properties.sectionType.const + properties.entries.items.$ref).",
      );
    }
    const match = itemsRef.match(/^#\/\$defs\/([A-Za-z0-9_]+)$/);
    if (!match) {
      throw new Error(
        `generate.mjs: unexpected entries.items.$ref "${itemsRef}" on sectionType "${sectionType}".`,
      );
    }
    return { sectionType, defKey: match[1], typeName: toPascalCase(match[1]) };
  });

  const fromOneOf = new Set(variants.map((v) => v.sectionType));
  const fromEnum = new Set(schema.$defs?.sectionType?.enum ?? []);
  const disagreement = [
    ...[...fromOneOf].filter((v) => !fromEnum.has(v)),
    ...[...fromEnum].filter((v) => !fromOneOf.has(v)),
  ];
  if (disagreement.length > 0) {
    throw new Error(
      "generate.mjs: $defs.section.oneOf and $defs.sectionType.enum disagree on sectionType " +
        `values: ${disagreement.join(", ")}.`,
    );
  }

  return variants;
}

// Shared preprocessing for both languages. Only the in-memory copy fed to
// the generators is touched; resume.schema.json on disk is never modified,
// and ajv (packages/schema/test/schema.test.ts) still validates the real
// file at runtime — everything here is about type *shape*, not validation.
function buildSharedCodegenSchema(schema) {
  const clone = structuredClone(schema);

  // 1. `unevaluatedProperties` (JSON Schema 2020-12) isn't evaluated by
  // either quicktype's or jstt's schema readers. On the eight entry $defs
  // (each an `allOf` of entryBase + its own fields, closed with
  // `unevaluatedProperties: false`) quicktype collapses the type to empty
  // rather than merging the allOf branches; jstt instead just ignores the
  // keyword and leaves the type open (an `[k: string]: unknown` index
  // signature — see fix 3). Stripping it is a no-op for validation (that's
  // ajv's job against the real file) and lets both tools fall back to their
  // normal allOf handling.
  const stripUnevaluatedProperties = (node) => {
    if (Array.isArray(node)) {
      node.forEach(stripUnevaluatedProperties);
      return;
    }
    if (node && typeof node === "object") {
      delete node.unevaluatedProperties;
      for (const value of Object.values(node)) {
        stripUnevaluatedProperties(value);
      }
    }
  };
  stripUnevaluatedProperties(clone);

  // 2. dateRange's `allOf` is JSON Schema's if/then conditional validation
  // (present ⇒ end === null), not type composition — the only other allOf
  // in this schema is the entry $defs' entryBase-plus-fields composition.
  // jstt treats any object with both `properties` and `allOf` as needing to
  // merge in the allOf branches, and if/then branches have no `properties`
  // of their own, jstt's merge collapses dateRange to `{ [k: string]:
  // unknown }`, discarding start/end/present entirely. quicktype already
  // handles this fine on its own (dateRange came out correct in every
  // quicktype trial while developing this generator), so this strip only
  // matters for jstt. It's runtime-only logic either way (AC-DOC-003 defers
  // the present/end relationship to the store layer, same as this schema's
  // own comment on dateRange says), so dropping it for codegen purposes is
  // safe.
  for (const def of Object.values(clone.$defs ?? {})) {
    if (
      def &&
      typeof def === "object" &&
      Array.isArray(def.allOf) &&
      def.allOf.some((branch) => branch && typeof branch === "object" && "if" in branch)
    ) {
      delete def.allOf;
    }
  }

  // 3. `link`'s `anyOf: [{const: ""}, {format: "uri"}]` refines a sibling
  // `"type": "string"` for ajv's benefit (validation only — TS/Go types
  // don't need to distinguish "empty string" from "URI string", both are
  // just `string`). jstt reads the "type" and the "anyOf" as two separate
  // partial schemas and intersects them at every use site, producing
  // `employerLink: Link & Link1` (Link = `"" | {}`, Link1 = `string`) —
  // technically sound (the intersection still reduces to `string`) but
  // confusing, and it exports two dangling aliases for what's just a
  // string. Replacing the $def with a plain string schema keeps the
  // description but drops the refinement quicktype and jstt don't need.
  if (clone.$defs?.link) {
    clone.$defs.link = {
      description: clone.$defs.link.description,
      type: "string",
      maxLength: clone.$defs.link.maxLength,
    };
  }

  // 4. `$defs` entries without a "title" get each tool's best-guess name.
  // For types shared by several properties (e.g. the {y, m} shape used by
  // both dateRange.start/end and certificateEntry.date) quicktype can't
  // derive a name from any single property and falls back to one
  // synthesized from the *input filename* — unstable if this script's temp
  // file name ever changes. jstt instead already names every $def after its
  // own key by default, so this step is load-bearing for quicktype and
  // redundant-but-harmless for jstt. Doing it once here keeps one codegen
  // schema serving both.
  for (const [key, def] of Object.entries(clone.$defs ?? {})) {
    if (def && typeof def === "object" && !("title" in def) && !("const" in def)) {
      def.title = toPascalCase(key);
    }
  }

  // Force the root document's generated name to "Resume" regardless of the
  // schema's own human-readable title ("Resume document"), matching
  // quicktype's `--top-level Resume` flag (belt and suspenders — jstt
  // prefers a root "title" over the name passed to compile()).
  clone.title = "Resume";

  return clone;
}

// Go-only: content's map values point at `section`, an eight-way oneOf.
// Go has no representation for that beyond structural collapse (see the
// file header), so section.go hand-writes the real `Section` type instead.
// This function retargets `content`'s value schema at a trivial empty
// placeholder titled "Section", so quicktype still generates
// `Content map[string]Section` on Resume (referencing the hand-written
// type) without also generating a competing, empty `type Section struct{}`
// — generateGo strips that placeholder struct out of the raw output below,
// since section.go already declares the real one.
function buildGoCodegenSchema(sharedSchema) {
  const clone = structuredClone(sharedSchema);
  clone.$defs.__sectionPlaceholder = {
    title: "Section",
    type: "object",
    additionalProperties: false,
  };
  clone.$defs.content.additionalProperties = { $ref: "#/$defs/__sectionPlaceholder" };
  return clone;
}

function runQuicktype(args) {
  execFileSync(quicktypeBin, args, { stdio: "inherit" });
}

function generateGo(sharedSchema, tmpDir, outFile) {
  const goSchema = buildGoCodegenSchema(sharedSchema);
  const id = goSchema.$id;
  const variants = deriveSectionVariants(sharedSchema);

  // quicktype names a positional multi-file input after the *file's own
  // basename*, not the referenced $def's title (unlike a $ref reached by
  // walking properties from a single main file, where title wins — see this
  // file's "Investigated and ruled out" header comment). So each pointer
  // file here is named exactly after the Go type it should produce.
  const schemaFilePath = join(tmpDir, "resume.schema.json");
  writeFileSync(schemaFilePath, JSON.stringify(goSchema, null, 2));

  const entryDefs = variants.map((v) => [v.defKey, v.typeName]);
  const pointerFiles = [];
  for (const [key, name] of [...entryDefs, SECTION_TYPE_DEF]) {
    const pointerPath = join(tmpDir, `${name}.json`);
    writeFileSync(pointerPath, JSON.stringify({ $ref: `${id}#/$defs/${key}` }));
    pointerFiles.push(pointerPath);
  }

  runQuicktype([
    "--src-lang",
    "schema",
    "-S",
    schemaFilePath,
    "--lang",
    "go",
    "--package",
    "schema",
    "--top-level",
    "Resume",
    "--just-types-and-package",
    "--alphabetize-properties",
    "--no-date-times",
    "--no-uuids",
    "--telemetry",
    "disable",
    "-o",
    outFile,
    schemaFilePath,
    ...pointerFiles,
  ]);

  let raw = readFileSync(outFile, "utf8");

  // Drop the placeholder's empty struct (see buildGoCodegenSchema) — the
  // real Section lives in section.go. Matched exactly (not a general regex)
  // so a future schema change that breaks this assumption fails loudly
  // (the drift test's byte-compare, or this replace() finding nothing changed).
  const placeholderStruct = "\ntype Section struct {\n}\n";
  if (!raw.includes(placeholderStruct)) {
    throw new Error(
      "generate.mjs: expected placeholder 'type Section struct {}' not found in quicktype's Go output — buildGoCodegenSchema or quicktype's formatting may have changed.",
    );
  }
  raw = raw.replace(placeholderStruct, "");

  const body = `${GENERATED_HEADER}\n\npackage schema\n\n${raw.replace(/^package schema\n\n/, "")}`;
  writeFileSync(outFile, body);

  // quicktype's Go column-alignment pass misaligns struct fields when an
  // inline comment sits between them (visible on Customization pre-gofmt);
  // gofmt fixes that and is the canonical formatter for committed Go anyway.
  execFileSync("gofmt", ["-w", outFile], { stdio: "inherit" });
}

async function generateTs(sharedSchema, outFile) {
  const ts = await compileTypeScript(sharedSchema, "Resume", {
    bannerComment: "",
    style: { semi: true },
    // Every object $def in resume.schema.json except entryBase (folded away
    // once nothing references it — see below) already sets its own
    // `additionalProperties` explicitly; this only supplies the default for
    // the couple of places that rely on it being closed implicitly.
    additionalProperties: false,
    // Without this, `maxItems: 16`/`64` array fields (personalDetails,
    // section entries) expand into a union of every fixed-length tuple from
    // 0 to maxItems instead of `T[]` — unreadable and not what maxItems
    // means here (a runtime bound ajv enforces, not a type-level one).
    ignoreMinAndMaxItems: true,
    // Prefer `unknown` over `any` for any residual underspecified schema
    // (there shouldn't be any after buildSharedCodegenSchema's fixes, but
    // `unknown` fails safe if a future schema change reintroduces one).
    unknownAny: true,
  });

  writeFileSync(outFile, GENERATED_HEADER_TS + "\n" + ts);
}

async function main() {
  const schema = JSON.parse(readFileSync(schemaPath, "utf8"));
  const sharedSchema = buildSharedCodegenSchema(schema);

  const tmpDir = mkdtempSync(join(tmpdir(), "aboutme-schema-codegen-"));
  try {
    const goDir = join(packageRoot, "gen", "go");
    const tsDir = join(packageRoot, "gen", "ts");
    mkdirSync(goDir, { recursive: true });
    mkdirSync(tsDir, { recursive: true });

    generateGo(sharedSchema, tmpDir, join(goDir, "resume.go"));
    await generateTs(sharedSchema, join(tsDir, "resume.ts"));
  } finally {
    rmSync(tmpDir, { recursive: true, force: true });
  }

  console.log("Generated gen/go/resume.go and gen/ts/resume.ts from resume.schema.json");
}

await main();
