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
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { compile as compileTypeScript } from "json-schema-to-typescript";

const packageRoot = dirname(dirname(fileURLToPath(import.meta.url)));
const schemaPath = join(packageRoot, "resume.schema.json");
const manifestPath = join(packageRoot, "released-versions.json");
const quicktypeBin = join(packageRoot, "node_modules", ".bin", "quicktype");

// The Go import path of the module gen/go/go.mod declares. The released-schema
// registry (gen/go/released.go) imports one retained package per released
// version from underneath it — they are packages inside that same module, not
// modules of their own, so nothing in go.work changes when a version is added.
const GO_MODULE_PATH = "github.com/dannyota/aboutme/packages/schema/gen/go";

const generatedHeader = (sourceName) => `// Code generated from ${sourceName}. DO NOT EDIT.`;

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

// TypeScript-only: a `$ref` carrying a sibling `description` (added
// 2026-08-11 by customization.colors.surface, the schema's first documented
// reference) is an annotation on the *use site*, not a different shape. jstt
// treats such a node as its own schema, so it emits a SECOND alias for the
// referenced $def and points the property at the duplicate — `surface?:
// HexColor1` beside `accent?: HexColor`, with `export type HexColor1 =
// string` dangling next to the identical `HexColor`. That is the same wart
// buildSharedCodegenSchema's fix 3 removes for `link`, arriving by a
// different route. quicktype has no such problem: it resolves the $ref to
// the same Go type and keeps the description as the field's doc comment, so
// this strip is deliberately TS-only and the Go output stays documented.
// Dropping an annotation cannot change a type, and resume.schema.json keeps
// the description either way — ajv validates the real file, and
// gen/go/rawschema.go embeds it verbatim.
function buildTsCodegenSchema(sharedSchema) {
  const clone = structuredClone(sharedSchema);

  const stripDescriptionBesideRef = (node) => {
    if (Array.isArray(node)) {
      node.forEach(stripDescriptionBesideRef);
      return;
    }
    if (node && typeof node === "object") {
      if (typeof node.$ref === "string" && "description" in node) {
        delete node.description;
      }
      for (const value of Object.values(node)) {
        stripDescriptionBesideRef(value);
      }
    }
  };
  stripDescriptionBesideRef(clone);

  return clone;
}

function runQuicktype(args) {
  execFileSync(quicktypeBin, args, { stdio: "inherit" });
}

// `sectionMode` decides what happens to the placeholder `type Section
// struct{}` quicktype emits for buildGoCodegenSchema's stand-in:
//
//   "dispatch"  — the CURRENT convenience output (gen/go/resume.go): strip the
//                 placeholder, because the hand-written gen/go/section.go in
//                 the same package declares the real typed-dispatch Section.
//   "rawSection" — a RETAINED released snapshot (gen/go/v<N>/resume.go):
//                 replace the placeholder with `type Section =
//                 json.RawMessage`. A retained package holds only generated
//                 files, so there is no hand-written dispatch to point at, and
//                 raw JSON is the honest representation anyway: D13 defines a
//                 converter as func(json.RawMessage) (json.RawMessage, error)
//                 over the full document precisely because typed dispatch only
//                 exists for the current version. Keeping the empty struct
//                 instead would compile while silently claiming a v<N> section
//                 has no fields.
function generateGo(sharedSchema, tmpDir, outFile, { packageName, sourceName, sectionMode }) {
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
    packageName,
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

  // Deal with the placeholder's empty struct (see buildGoCodegenSchema and
  // this function's sectionMode comment). Matched exactly (not a general
  // regex) so a future schema change that breaks this assumption fails loudly
  // (the drift test's byte-compare, or this replace() finding nothing changed).
  const placeholderStruct = "\ntype Section struct {\n}\n";
  if (!raw.includes(placeholderStruct)) {
    throw new Error(
      "generate.mjs: expected placeholder 'type Section struct {}' not found in quicktype's Go output — buildGoCodegenSchema or quicktype's formatting may have changed.",
    );
  }
  raw = raw.replace(placeholderStruct, "");

  let preamble = "";
  if (sectionMode === "rawSection") {
    preamble =
      'import "encoding/json"\n\n' +
      "// Section is this released version's `content[key]` value: an eight-way\n" +
      "// oneOf on sectionType that Go cannot express as a type. The CURRENT\n" +
      "// package's hand-written section.go supplies a typed dispatch for it; a\n" +
      "// retained snapshot holds generated files only, and raw JSON matches how\n" +
      "// an adjacent-version converter actually handles a non-current document\n" +
      "// (D13: converters are func(json.RawMessage) (json.RawMessage, error) over\n" +
      "// the whole document, never a typed decode).\n" +
      "type Section = json.RawMessage\n\n";
  } else if (sectionMode !== "dispatch") {
    throw new Error(`generate.mjs: unknown sectionMode ${JSON.stringify(sectionMode)}.`);
  }

  const body =
    `${generatedHeader(sourceName)}\n\npackage ${packageName}\n\n${preamble}` +
    raw.replace(new RegExp(`^package ${packageName}\\n\\n`), "");
  writeFileSync(outFile, body);

  // quicktype's Go column-alignment pass misaligns struct fields when an
  // inline comment sits between them (visible on Customization pre-gofmt);
  // gofmt fixes that and is the canonical formatter for committed Go anyway.
  execFileSync("gofmt", ["-w", outFile], { stdio: "inherit" });
}

async function generateTs(sharedSchema, outFile, { sourceName }) {
  const ts = await compileTypeScript(buildTsCodegenSchema(sharedSchema), "Resume", {
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

  writeFileSync(outFile, `${generatedHeader(sourceName)}\n\n${ts}`);
}

// Splits a base64 string into fixed-width lines so the generated Go source
// doesn't put resume.schema.json's ~29 KB of encoded bytes on a single line
// (readability/diffability only — Go itself doesn't care about line length
// here).
function chunkBase64(base64, width = 96) {
  const lines = [];
  for (let i = 0; i < base64.length; i += width) {
    lines.push(base64.slice(i, i + width));
  }
  return lines;
}

// Go-only (decision D2): resume.schema.json lives outside gen/go's own
// module (see gen/go/go.mod), so go:embed cannot reach it — a Go source file
// can only //go:embed a path inside (or below) its own module root. This
// generates gen/go/rawschema.go instead: a plain Go source file exposing
// schema.RawSchema []byte, base64-encoding resume.schema.json's exact bytes
// at generation time.
//
// Base64, not a Go string literal: resume.schema.json contains a literal
// backtick (inside a description string), which raw string literals cannot
// contain at all, and non-ASCII bytes that would need per-rune escaping in
// an interpreted string literal — both are exactly the kind of fragile,
// easy-to-get-subtly-wrong transcription this generator should not need to
// hand-roll. Base64's alphabet has neither problem, so the embedding is a
// straight, deterministic transcoding of rawSchemaBytes with no escaping
// logic at all.
function generateRawSchema(rawSchemaBytes, outFile, { packageName, sourceName, verifiedBy }) {
  const base64Lines = chunkBase64(rawSchemaBytes.toString("base64"));
  const literal = base64Lines.map((line) => `\t"${line}" +\n`).join("");

  const body = `${generatedHeader(sourceName)}

package ${packageName}

import "encoding/base64"

// rawSchemaBase64 is ${sourceName}'s exact bytes, base64-encoded at
// generation time (decision D2) — see this file's generator
// (scripts/generate.mjs's generateRawSchema) for why base64 instead of a
// plain Go string literal.
const rawSchemaBase64 = "" +
${literal}\t""

// RawSchema is ${sourceName}'s exact byte content, decoded once at
// package init from rawSchemaBase64 above. ${sourceName} lives outside
// this module (gen/go/go.mod), so go:embed cannot reach it directly — this
// generated constant is the substitute. ${verifiedBy}
var RawSchema = mustDecodeRawSchemaBase64()

func mustDecodeRawSchemaBase64() []byte {
	decoded, err := base64.StdEncoding.DecodeString(rawSchemaBase64)
	if err != nil {
		// Unreachable for a generated, unedited file: rawSchemaBase64 above is
		// produced by encoding/base64's own encoder in scripts/generate.mjs's
		// generateRawSchema (Node's Buffer#toString("base64") — the same
		// alphabet, no hand-editing in between).
		panic("schema: rawSchemaBase64 failed to decode: " + err.Error())
	}
	return decoded
}
`;

  writeFileSync(outFile, body);
  execFileSync("gofmt", ["-w", outFile], { stdio: "inherit" });
}

// Reads released-versions.json — the ONLY thing that tells this script which
// released schemas exist and where each one lives. Deliberately no directory
// scan and no "highest resume.v*.schema.json wins" rule (design spec §3,
// "Wire-version compatibility"): implicit discovery would quietly promote a
// stray, half-finished, or reverted file to a released contract, and it would
// make "which schema did this build generate from?" depend on the working
// tree's filenames rather than on a reviewed, append-only declaration.
//
// Everything structural is validated here rather than assumed, because a
// malformed manifest would otherwise surface as confusing codegen output
// instead of an error naming the actual problem.
function readReleasedManifest() {
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
  const versions = manifest.versions;
  if (!Array.isArray(versions) || versions.length === 0) {
    throw new Error("generate.mjs: released-versions.json's `versions` is missing or empty.");
  }

  let previous = 0;
  for (const entry of versions) {
    const { version, schema, goPackage, tsTypes } = entry ?? {};
    if (!Number.isInteger(version) || version < 1) {
      throw new Error(
        `generate.mjs: released-versions.json has a non-integer or non-positive version ${JSON.stringify(version)}.`,
      );
    }
    if (version <= previous) {
      throw new Error(
        `generate.mjs: released-versions.json's versions must ascend; ${version} follows ${previous}.`,
      );
    }
    previous = version;

    // The conventional paths are asserted, not derived: a released entry
    // states its own file explicitly, and this catches a typo pointing an
    // entry at another version's schema (which would silently regenerate v<N>
    // from v<M>'s bytes — the exact drift the immutability policy forbids).
    const expected = {
      schema: `resume.v${version}.schema.json`,
      goPackage: `gen/go/v${version}`,
      tsTypes: `gen/ts/v${version}/resume.ts`,
    };
    for (const [field, want] of Object.entries(expected)) {
      const got = { schema, goPackage, tsTypes }[field];
      if (got !== want) {
        throw new Error(
          `generate.mjs: released-versions.json version ${version} declares ${field} ${JSON.stringify(got)}, want ${JSON.stringify(want)}.`,
        );
      }
    }
    if (!existsSync(join(packageRoot, schema))) {
      throw new Error(
        `generate.mjs: released-versions.json version ${version} names ${schema}, which does not exist.`,
      );
    }
  }

  if (!versions.some((entry) => entry.version === manifest.currentVersion)) {
    throw new Error(
      `generate.mjs: released-versions.json's currentVersion ${JSON.stringify(manifest.currentVersion)} is not one of the released versions.`,
    );
  }

  return manifest;
}

// The Go half of the released-version registry. It lives in the CURRENT
// package (gen/go, package schema) and imports one retained package per
// released version, so `go build ./...` in that module is itself the proof
// that every retained snapshot still compiles.
//
// Lookup fails closed: an unreleased version returns ErrUnknownSchemaVersion
// rather than the nearest or newest match, mirroring docmigrate's identity
// projector, which errors on a stored schema_version it has no converter for
// instead of passing the document through unconverted.
function generateReleasedGo(manifest, outFile) {
  const imports = manifest.versions
    .map((entry) => `\tschemav${entry.version} "${GO_MODULE_PATH}/v${entry.version}"`)
    .join("\n");

  const entries = manifest.versions
    .map(
      (entry) => `\t{
\t\tVersion:   ${entry.version},
\t\tSchema:    "${entry.schema}",
\t\tGoPackage: "${entry.goPackage}",
\t\tTSTypes:   "${entry.tsTypes}",
\t\tRawSchema: schemav${entry.version}.RawSchema,
\t},`,
    )
    .join("\n");

  const body = `${generatedHeader("released-versions.json")}

package schema

import (
	"bytes"
	"errors"
	"fmt"

${imports}
)

// CurrentVersion is the document-shape version resume.schema.json currently
// describes, and the version every stored resume is projected to on read and
// persisted at on write. It matches apps/server's docmigrate.CurrentVersion
// by construction: both move only when a new version is released here.
const CurrentVersion = ${manifest.currentVersion}

// ReleasedSchema is one released document-shape version: its immutable schema
// file and the retained generated types derived from that file. Released
// entries are append-only (design spec §3, "Wire-version compatibility"), so
// a value of this type describes a contract that can never change, only be
// superseded.
type ReleasedSchema struct {
	// Version is the released schema_version this entry describes.
	Version int
	// Schema is the immutable schema file's path, relative to packages/schema.
	Schema string
	// GoPackage is the retained Go types' path, relative to packages/schema.
	GoPackage string
	// TSTypes is the retained TypeScript types' path, relative to
	// packages/schema.
	TSTypes string
	// RawSchema is the schema file's exact byte content.
	RawSchema []byte
}

// ErrUnknownSchemaVersion is returned for a version this build has no
// released schema for. Callers must treat it as a hard failure: a document
// claiming an unreleased version cannot be validated, converted, or emitted,
// and guessing a nearby version would persist a document under a contract
// nothing describes.
var ErrUnknownSchemaVersion = errors.New("schema: unknown released schema version")

// releasedSchemas is generated from released-versions.json, ascending by
// version.
var releasedSchemas = []ReleasedSchema{
${entries}
}

// ReleasedVersions returns every released document-shape version in
// ascending order. The returned slice is freshly allocated, so a caller
// cannot reorder or truncate the registry for everyone else.
func ReleasedVersions() []int {
	versions := make([]int, len(releasedSchemas))
	for i, released := range releasedSchemas {
		versions[i] = released.Version
	}
	return versions
}

// ReleasedSchemaFor returns the released schema for version. It fails closed:
// an unreleased version yields an error wrapping ErrUnknownSchemaVersion and
// a zero ReleasedSchema, never a fallback to the current or nearest version.
// The returned RawSchema is a copy, so a caller cannot mutate the immutable
// bytes the retained package holds.
func ReleasedSchemaFor(version int) (ReleasedSchema, error) {
	for _, released := range releasedSchemas {
		if released.Version == version {
			released.RawSchema = bytes.Clone(released.RawSchema)
			return released, nil
		}
	}
	return ReleasedSchema{}, fmt.Errorf("%w: %d", ErrUnknownSchemaVersion, version)
}
`;

  writeFileSync(outFile, body);
  execFileSync("gofmt", ["-w", outFile], { stdio: "inherit" });
}

// The TypeScript half of the released-version registry. It carries the
// manifest rather than the schema bytes: apps/web imports this package for
// types, and inlining every released schema's ~36 KB into the bundle to
// satisfy a build-time registry would be a real cost for no runtime gain. The
// Go side (gen/go/released.go) is where the bytes live, because that is the
// side that actually compiles and validates documents.
function generateReleasedTs(manifest, outFile) {
  const entries = manifest.versions
    .map(
      (entry) => `  Object.freeze({
    version: ${entry.version},
    schema: "${entry.schema}",
    goPackage: "${entry.goPackage}",
    tsTypes: "${entry.tsTypes}",
  }),`,
    )
    .join("\n");

  const body = `${generatedHeader("released-versions.json")}

/**
 * One released document-shape version: its immutable schema file and the
 * retained generated types derived from that file. Paths are relative to
 * packages/schema. Released entries are append-only (design spec §3,
 * "Wire-version compatibility").
 */
export interface ReleasedSchema {
  readonly version: number;
  readonly schema: string;
  readonly goPackage: string;
  readonly tsTypes: string;
}

/**
 * The document-shape version resume.schema.json currently describes.
 */
export const CURRENT_VERSION = ${manifest.currentVersion};

/**
 * Every released version, ascending. Frozen: the registry is a contract, not
 * a mutable cache.
 */
export const RELEASED_SCHEMAS: readonly ReleasedSchema[] = Object.freeze([
${entries}
]);

/**
 * Reports whether \`version\` has been released. Non-integer, negative, and
 * NaN inputs are simply not released, so callers need no separate guard.
 */
export function isReleasedVersion(version: number): boolean {
  return RELEASED_SCHEMAS.some((released) => released.version === version);
}

/**
 * Resolves a released version. Fails closed: an unreleased version throws
 * rather than falling back to the current or nearest one, because a document
 * claiming an unreleased version cannot be validated or converted at all.
 * The result is a copy, so a caller cannot retarget the registry.
 */
export function releasedSchema(version: number): ReleasedSchema {
  const released = RELEASED_SCHEMAS.find((candidate) => candidate.version === version);
  if (released === undefined) {
    throw new Error(\`schema: unknown released schema version \${version}\`);
  }
  return { ...released };
}
`;

  writeFileSync(outFile, body);
}

async function main() {
  const manifest = readReleasedManifest();
  const schemaBytes = readFileSync(schemaPath);
  const schema = JSON.parse(schemaBytes.toString("utf8"));
  const sharedSchema = buildSharedCodegenSchema(schema);

  const tmpDir = mkdtempSync(join(tmpdir(), "aboutme-schema-codegen-"));
  const written = [];
  try {
    const goDir = join(packageRoot, "gen", "go");
    const tsDir = join(packageRoot, "gen", "ts");
    mkdirSync(goDir, { recursive: true });
    mkdirSync(tsDir, { recursive: true });

    // The current convenience outputs: what apps/server and apps/web actually
    // compile against today, generated from the working resume.schema.json.
    generateGo(sharedSchema, tmpDir, join(goDir, "resume.go"), {
      packageName: "schema",
      sourceName: "resume.schema.json",
      sectionMode: "dispatch",
    });
    await generateTs(sharedSchema, join(tsDir, "resume.ts"), {
      sourceName: "resume.schema.json",
    });
    generateRawSchema(schemaBytes, join(goDir, "rawschema.go"), {
      packageName: "schema",
      sourceName: "resume.schema.json",
      verifiedBy:
        "rawschema_test.go asserts this\n// byte-equals ../../resume.schema.json read directly at test time, closing\n// the copy-drift loop from the Go side (the TypeScript side is\n// test/gen.test.ts's existing regenerate-and-byte-compare check, which\n// already exercises this file too).",
    });
    written.push("gen/go/resume.go", "gen/ts/resume.ts", "gen/go/rawschema.go");

    // The retained per-version snapshots. Each one is regenerated from its
    // OWN immutable schema file, never from resume.schema.json — that is what
    // makes regenerating an old namespace mechanically derived rather than a
    // silent re-cut against whatever the contract has since become.
    for (const entry of manifest.versions) {
      const versionSchemaPath = join(packageRoot, entry.schema);
      const versionBytes = readFileSync(versionSchemaPath);
      const versionShared = buildSharedCodegenSchema(JSON.parse(versionBytes.toString("utf8")));
      const versionGoDir = join(packageRoot, entry.goPackage);
      const versionTsFile = join(packageRoot, entry.tsTypes);
      mkdirSync(versionGoDir, { recursive: true });
      mkdirSync(dirname(versionTsFile), { recursive: true });

      const versionTmpDir = mkdtempSync(join(tmpdir(), `aboutme-schema-codegen-v${entry.version}-`));
      try {
        generateGo(versionShared, versionTmpDir, join(versionGoDir, "resume.go"), {
          packageName: `schemav${entry.version}`,
          sourceName: entry.schema,
          sectionMode: "rawSection",
        });
      } finally {
        rmSync(versionTmpDir, { recursive: true, force: true });
      }
      await generateTs(versionShared, versionTsFile, { sourceName: entry.schema });
      generateRawSchema(versionBytes, join(versionGoDir, "rawschema.go"), {
        packageName: `schemav${entry.version}`,
        sourceName: entry.schema,
        verifiedBy: `gen/go/released_test.go asserts this\n// byte-equals ../../../${entry.schema} read directly at test time, and\n// test/gen.test.ts's regenerate-and-byte-compare check covers this file\n// too.`,
      });
      written.push(`${entry.goPackage}/resume.go`, `${entry.goPackage}/rawschema.go`, entry.tsTypes);
    }

    generateReleasedGo(manifest, join(goDir, "released.go"));
    generateReleasedTs(manifest, join(tsDir, "released.ts"));
    written.push("gen/go/released.go", "gen/ts/released.ts");
  } finally {
    rmSync(tmpDir, { recursive: true, force: true });
  }

  console.log(`Generated ${written.join(", ")}`);
}

await main();
