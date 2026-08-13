#!/usr/bin/env node
// Generates current and retained Go and TypeScript types from the declared
// JSON Schema files. Run `npm run generate` here or `make schema-gen` at the
// repository root.
//
// json-schema-to-typescript emits the TypeScript discriminated union directly.
// quicktype emits the Go entry structs, while hand-written section.go provides
// the Section dispatch that Go cannot represent as a sum type. In-memory schema
// transforms below compensate for generator limits; AJV validates the unchanged
// source schema. Section variants are derived from $defs.section.oneOf, and
// test/conformance.test.ts checks generator fidelity independently. See
// docs/design/repository.md.

import { execFileSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { compile as compileTypeScript } from "json-schema-to-typescript";

const packageRoot = dirname(dirname(fileURLToPath(import.meta.url)));
const schemaPath = join(packageRoot, "resume.schema.json");
const manifestPath = join(packageRoot, "released-versions.json");
const templateDirectory = process.env.ABOUTME_TEMPLATE_DIR
  ? fileURLToPath(new URL(`file://${process.env.ABOUTME_TEMPLATE_DIR}`))
  : join(packageRoot, "templates");
const sanitizerAllowlistPath = join(
  packageRoot,
  "validation",
  "sanitizer-allowlist.v1.json",
);
const hostileCorpusPath = join(
  packageRoot,
  "validation",
  "hostile-corpus.json",
);
const quicktypeBin = join(packageRoot, "node_modules", ".bin", "quicktype");
// json-schema-to-typescript already installs the formatter it uses for the
// generated resume types. Reuse that pinned binary for the sanitizer artifact.
const prettierBin = join(packageRoot, "node_modules", ".bin", "prettier");

// The Go import path of the module gen/go/go.mod declares. The released-schema
// registry (gen/go/released.go) imports one retained package per released
// version from underneath it — they are packages inside that same module, not
// modules of their own, so nothing in go.work changes when a version is added.
const GO_MODULE_PATH = "github.com/dannyota/aboutme/packages/schema/gen/go";

const generatedHeader = (sourceName) =>
  `// Code generated from ${sourceName}. DO NOT EDIT.`;

function failTemplate(file, message) {
  throw new Error(`generate.mjs: template ${basename(file)} ${message}`);
}

function exactKeys(file, value, required, optional = []) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    failTemplate(file, "must be an object.");
  }
  const allowed = new Set([...required, ...optional]);
  const keys = Object.keys(value);
  const missing = required.filter((key) => !keys.includes(key));
  const unknown = keys.filter((key) => !allowed.has(key));
  if (missing.length > 0 || unknown.length > 0) {
    failTemplate(
      file,
      `has invalid keys (missing: ${missing.join(", ") || "none"}; unknown: ${unknown.join(", ") || "none"}).`,
    );
  }
}

function readTemplatePresets(schema) {
  const files = readdirSync(templateDirectory)
    .filter((name) => name.endsWith(".json"))
    .sort()
    .map((name) => join(templateDirectory, name));
  if (files.length === 0) {
    throw new Error("generate.mjs: no template presets found.");
  }

  const fonts = new Set(
    schema.$defs?.customization?.properties?.font?.properties?.family?.enum ??
      [],
  );
  const sectionTypes = new Set(schema.$defs?.sectionType?.enum ?? []);
  const surfaceTargets = new Set(
    schema.$defs?.customization?.properties?.layout?.properties?.surfaceTarget
      ?.enum ?? [],
  );
  const ajv = addFormats(new Ajv2020({ allErrors: true, strict: true }));
  const validateCustomization = ajv.compile({
    $schema: schema.$schema,
    $defs: schema.$defs,
    $ref: "#/$defs/customization",
  });
  const ids = new Set();

  return files.map((file) => {
    let preset;
    try {
      preset = JSON.parse(readFileSync(file, "utf8"));
    } catch (error) {
      failTemplate(file, `is not valid JSON: ${error.message}.`);
    }
    exactKeys(file, preset, ["id", "name", "description", "customization"]);
    if (
      typeof preset.id !== "string" ||
      preset.id.length === 0 ||
      preset.id !== basename(file, ".json")
    ) {
      failTemplate(file, "must have an id equal to its filename.");
    }
    if (ids.has(preset.id)) {
      failTemplate(file, `duplicates id ${JSON.stringify(preset.id)}.`);
    }
    ids.add(preset.id);
    for (const key of ["name", "description"]) {
      if (typeof preset[key] !== "string" || preset[key].length === 0) {
        failTemplate(file, `must have a non-empty ${key}.`);
      }
    }

    const customization = preset.customization;
    exactKeys(
      file,
      customization,
      [
        "font",
        "colors",
        "spacing",
        "heading",
        "layout",
        "sectionDisplay",
        "pageFormat",
        "dateFormat",
      ],
      ["header"],
    );
    if (!fonts.has(customization?.font?.family)) {
      failTemplate(
        file,
        `uses unknown font ${JSON.stringify(customization?.font?.family)}.`,
      );
    }

    const layout = customization.layout;
    exactKeys(
      file,
      layout,
      ["columns", "placement"],
      ["sidebarSectionTypes", "surfaceTarget"],
    );
    if (
      layout.surfaceTarget !== undefined &&
      !surfaceTargets.has(layout.surfaceTarget)
    ) {
      failTemplate(
        file,
        `uses unknown surfaceTarget ${JSON.stringify(layout.surfaceTarget)}.`,
      );
    }
    if (layout.placement === "keep") {
      if (layout.sidebarSectionTypes !== undefined) {
        failTemplate(
          file,
          "must not define sidebarSectionTypes for placement keep.",
        );
      }
    } else if (layout.placement === "byType") {
      if (!Array.isArray(layout.sidebarSectionTypes)) {
        failTemplate(
          file,
          "must define sidebarSectionTypes for placement byType.",
        );
      }
      const selectors = layout.sidebarSectionTypes;
      if (new Set(selectors).size !== selectors.length) {
        failTemplate(file, "has duplicate sidebarSectionTypes.");
      }
      for (const selector of selectors) {
        if (!sectionTypes.has(selector) || selector === "custom") {
          failTemplate(
            file,
            `uses invalid section selector ${JSON.stringify(selector)}.`,
          );
        }
      }
    } else {
      failTemplate(
        file,
        `uses unknown placement ${JSON.stringify(layout.placement)}.`,
      );
    }

    const {
      placement: _placement,
      sidebarSectionTypes: _selectors,
      ...storedLayout
    } = layout;
    const storedCustomization = {
      ...customization,
      layout: { ...storedLayout, sections: { main: [], sidebar: [] } },
    };
    if (!validateCustomization(storedCustomization)) {
      failTemplate(
        file,
        `does not form a valid customization: ${ajv.errorsText(validateCustomization.errors)}.`,
      );
    }
    return preset;
  });
}

export function generateTemplatesTs(
  presets,
  sectionTypes,
  surfaceTargets,
  outFile,
) {
  const sectionTypeUnion = sectionTypes.map(JSON.stringify).join(" | ");
  const surfaceTargetUnion = surfaceTargets.map(JSON.stringify).join(" | ");
  const body = `${generatedHeader("templates/*.json and resume.schema.json")}

import type { Customization } from "./resume";

export type SectionType = ${sectionTypeUnion};

export interface TemplatePreset {
  readonly id: string;
  readonly name: string;
  readonly description: string;
  readonly customization: Omit<Customization, "layout"> & {
    readonly layout: {
      readonly columns: 1 | 2;
      readonly placement: "keep" | "byType";
      readonly sidebarSectionTypes?: readonly SectionType[];
      readonly surfaceTarget?: ${surfaceTargetUnion};
    };
  };
}

function deepFreeze<T>(value: T): Readonly<T> {
  if (value !== null && typeof value === "object") {
    for (const nested of Object.values(value)) {
      deepFreeze(nested);
    }
    Object.freeze(value);
  }
  return value;
}

export const TEMPLATES: readonly Readonly<TemplatePreset>[] = deepFreeze(
  ${JSON.stringify(presets, null, 2)} satisfies TemplatePreset[],
);
`;
  writeFileSync(outFile, body);
  execFileSync(prettierBin, ["--write", outFile], { stdio: "ignore" });
}

// The $def backing the (otherwise-unreferenced) SectionType enum — see
// deriveSectionVariants for the per-sectionType entry list, which is
// derived from the schema rather than named here.
const SECTION_TYPE_DEF = ["sectionType", "SectionType"];

function toPascalCase(key) {
  return key.charAt(0).toUpperCase() + key.slice(1);
}

// Derive entry definitions from the schema, in declaration order. Fail when
// section.oneOf and sectionType.enum disagree instead of generating a partial
// contract.
function deriveSectionVariants(schema) {
  const oneOf = schema.$defs?.section?.oneOf;
  if (!Array.isArray(oneOf) || oneOf.length === 0) {
    throw new Error(
      "generate.mjs: resume.schema.json's $defs.section.oneOf is missing or empty.",
    );
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

  // Neither generator evaluates JSON Schema 2020-12 unevaluatedProperties.
  // Removing it from the type-only copy lets both merge entry allOf branches;
  // AJV still enforces it against the source schema.
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

  // dateRange allOf contains if/then validation, not type composition. jstt
  // otherwise collapses its properties to unknown. The store layer enforces
  // the relationship against real values.
  for (const def of Object.values(clone.$defs ?? {})) {
    if (
      def &&
      typeof def === "object" &&
      Array.isArray(def.allOf) &&
      def.allOf.some(
        (branch) => branch && typeof branch === "object" && "if" in branch,
      )
    ) {
      delete def.allOf;
    }
  }

  // link's anyOf refines string validation but not its generated type. Use a
  // plain string in the type-only copy to avoid duplicate intersection aliases.
  if (clone.$defs?.link) {
    clone.$defs.link = {
      description: clone.$defs.link.description,
      type: "string",
      maxLength: clone.$defs.link.maxLength,
    };
  }

  // Give every reusable definition a stable name. quicktype otherwise derives
  // some names from the temporary input filename.
  for (const [key, def] of Object.entries(clone.$defs ?? {})) {
    if (
      def &&
      typeof def === "object" &&
      !("title" in def) &&
      !("const" in def)
    ) {
      def.title = toPascalCase(key);
    }
  }

  // jstt prefers the schema title over compile()'s requested root name.
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
  clone.$defs.content.additionalProperties = {
    $ref: "#/$defs/__sectionPlaceholder",
  };
  return clone;
}

// jstt treats a $ref with a sibling description as a new shape and emits a
// duplicate alias. Drop that use-site annotation only from the TypeScript
// type-copy; the source schema and embedded raw schema retain it.
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

// sectionMode controls the placeholder Section emitted by quicktype:
//
//   "dispatch"  — the CURRENT convenience output (gen/go/resume.go): strip the
//                 placeholder, because the hand-written gen/go/section.go in
//                 the same package declares the real typed-dispatch Section.
//   "rawSection" — a RETAINED released snapshot (gen/go/v<N>/resume.go):
//                 replace the placeholder with `type Section =
//                 json.RawMessage`. A retained package holds only generated
//                 files, so there is no hand-written dispatch to point at, and
//                 converters operate on raw full-document JSON. Keeping the
//                 empty struct would falsely claim a released section has no
//                 fields.
function generateGo(
  sharedSchema,
  tmpDir,
  outFile,
  { packageName, sourceName, sectionMode },
) {
  const goSchema = buildGoCodegenSchema(sharedSchema);
  const id = goSchema.$id;
  const variants = deriveSectionVariants(sharedSchema);

  // quicktype names positional inputs from their basenames, so each pointer
  // filename must match the intended Go type.
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
  } else if (sectionMode === "dispatch") {
    preamble = 'import "encoding/json"\n\n';
  } else {
    throw new Error(
      `generate.mjs: unknown sectionMode ${JSON.stringify(sectionMode)}.`,
    );
  }

  const packagePrefix = `package ${packageName}\n\n`;
  if (!raw.startsWith(packagePrefix)) {
    throw new Error(
      `generate.mjs: quicktype output did not start with ${JSON.stringify(packagePrefix)}.`,
    );
  }
  raw = raw.slice(packagePrefix.length);

  // V1 was released before this custom marshal behavior existed. Retained
  // generated outputs are immutable, so only current and later releases may
  // carry it.
  const presenceMarshal =
    sourceName === "resume.v1.schema.json"
      ? ""
      : `
// MarshalJSON preserves the schema's absent-versus-explicit-empty distinction
// for personalDetails.details. encoding/json's ordinary omitempty rule would
// collapse a non-nil empty slice to absence.
func (p PersonalDetails) MarshalJSON() ([]byte, error) {
	type personalDetailsJSON PersonalDetails
	if p.Details == nil {
		return json.Marshal(personalDetailsJSON(p))
	}
	return json.Marshal(struct {
		Details []PersonalDetail \`json:"details"\`
		personalDetailsJSON
	}{Details: p.Details, personalDetailsJSON: personalDetailsJSON(p)})
}
`;
  const body =
    `${generatedHeader(sourceName)}\n\npackage ${packageName}\n\n${preamble}` +
    raw +
    presenceMarshal;
  writeFileSync(outFile, body);

  // quicktype's Go column-alignment pass misaligns struct fields when an
  // inline comment sits between them (visible on Customization pre-gofmt);
  // gofmt fixes that and is the canonical formatter for committed Go anyway.
  execFileSync("gofmt", ["-w", outFile], { stdio: "inherit" });
}

async function generateTs(sharedSchema, outFile, { sourceName }) {
  const ts = await compileTypeScript(
    buildTsCodegenSchema(sharedSchema),
    "Resume",
    {
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
      // Prefer `unknown` over `any` for any residual underspecified schema.
      unknownAny: true,
    },
  );

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

// resume.schema.json is outside gen/go's module, so go:embed cannot reach it.
// Generate a Go source constant containing the schema's exact bytes instead.
//
// Base64, not a Go string literal: resume.schema.json contains a literal
// backtick (inside a description string), which raw string literals cannot
// contain at all, and non-ASCII bytes that would need per-rune escaping in
// an interpreted string literal — both are exactly the kind of fragile,
// easy-to-get-subtly-wrong transcription this generator should not need to
// hand-roll. Base64's alphabet has neither problem, so the embedding is a
// straight, deterministic transcoding of rawSchemaBytes with no escaping
// logic at all.
function generateRawSchema(
  rawSchemaBytes,
  outFile,
  { packageName, sourceName, verifiedBy },
) {
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

// released-versions.json is the only release registry. Do not infer releases
// from filenames, because a stray file must not become a contract. Validate its
// structure here so failures name the manifest field, not generated output.
function readReleasedManifest() {
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
  const versions = manifest.versions;
  if (!Array.isArray(versions) || versions.length === 0) {
    throw new Error(
      "generate.mjs: released-versions.json's `versions` is missing or empty.",
    );
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

  const releasedVersions = new Set(versions.map((entry) => entry.version));
  for (const field of ["acceptedVersions", "emittedVersions"]) {
    const declared = manifest[field];
    if (!Array.isArray(declared) || declared.length === 0) {
      throw new Error(
        `generate.mjs: released-versions.json's ${field} must be a non-empty array.`,
      );
    }
    let previousDeclared = 0;
    for (const version of declared) {
      if (!Number.isInteger(version) || version < 1) {
        throw new Error(
          `generate.mjs: released-versions.json's ${field} contains invalid version ${JSON.stringify(version)}.`,
        );
      }
      if (version <= previousDeclared) {
        throw new Error(
          `generate.mjs: released-versions.json's ${field} must ascend without duplicates; ${version} follows ${previousDeclared}.`,
        );
      }
      if (!releasedVersions.has(version)) {
        throw new Error(
          `generate.mjs: released-versions.json's ${field} declares unreleased version ${version}.`,
        );
      }
      previousDeclared = version;
    }
    if (!declared.includes(manifest.currentVersion)) {
      throw new Error(
        `generate.mjs: released-versions.json's currentVersion ${manifest.currentVersion} is absent from ${field}.`,
      );
    }
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
    .map(
      (entry) =>
        `\tschemav${entry.version} "${GO_MODULE_PATH}/v${entry.version}"`,
    )
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
// because the production projector consumes this generated declaration.
const CurrentVersion = ${manifest.currentVersion}

var acceptedVersions = []int{${manifest.acceptedVersions.join(", ")}}
var emittedVersions = []int{${manifest.emittedVersions.join(", ")}}

// AcceptedVersions returns the independently declared wire versions accepted
// by production. The returned slice is a copy.
func AcceptedVersions() []int { return append([]int(nil), acceptedVersions...) }

// EmittedVersions returns the independently declared wire versions emitted by
// production. The returned slice is a copy.
func EmittedVersions() []int { return append([]int(nil), emittedVersions...) }

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

/** Wire versions accepted by production, authored independently of releases. */
export const ACCEPTED_VERSIONS: readonly number[] = Object.freeze([${manifest.acceptedVersions.join(", ")}]);

/** Wire versions emitted by production, authored independently of releases. */
export const EMITTED_VERSIONS: readonly number[] = Object.freeze([${manifest.emittedVersions.join(", ")}]);

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

function requireStringArray(value, name) {
  if (
    !Array.isArray(value) ||
    value.some((entry) => typeof entry !== "string")
  ) {
    throw new Error(`generate.mjs: ${name} must be an array of strings.`);
  }
  if (new Set(value).size !== value.length) {
    throw new Error(`generate.mjs: ${name} contains duplicate values.`);
  }
  return value;
}

function readSanitizerSources(schema) {
  const allowlist = JSON.parse(readFileSync(sanitizerAllowlistPath, "utf8"));
  const corpus = JSON.parse(readFileSync(hostileCorpusPath, "utf8"));
  const schemaVersion = schema.$defs?.sanitizerAllowlistVersion?.const;

  if (
    !Number.isInteger(allowlist.version) ||
    allowlist.version < 1 ||
    allowlist.version !== corpus.version ||
    allowlist.version !== schemaVersion
  ) {
    throw new Error(
      "generate.mjs: sanitizer allowlist, hostile corpus, and resume schema versions must be the same positive integer.",
    );
  }

  const tags = requireStringArray(allowlist.tags, "sanitizer tags");
  const globalAttributes = requireStringArray(
    allowlist.globalAttributes,
    "sanitizer globalAttributes",
  );
  if (globalAttributes.length !== 0) {
    throw new Error(
      "generate.mjs: sanitizer globalAttributes must stay empty until the generated interface defines them.",
    );
  }

  if (
    allowlist.attributes === null ||
    typeof allowlist.attributes !== "object" ||
    Array.isArray(allowlist.attributes)
  ) {
    throw new Error("generate.mjs: sanitizer attributes must be an object.");
  }
  const attributes = Object.fromEntries(
    Object.keys(allowlist.attributes)
      .sort()
      .map((tag) => {
        if (!tags.includes(tag)) {
          throw new Error(
            `generate.mjs: sanitizer attributes names unallowed tag ${JSON.stringify(tag)}.`,
          );
        }
        return [
          tag,
          requireStringArray(
            allowlist.attributes[tag],
            `sanitizer attributes.${tag}`,
          ),
        ];
      }),
  );

  const forbidden = allowlist.forbidden;
  if (forbidden === null || typeof forbidden !== "object") {
    throw new Error("generate.mjs: sanitizer forbidden policy is missing.");
  }
  const linkHardening = allowlist.linkHardening;
  if (
    linkHardening === null ||
    typeof linkHardening !== "object" ||
    typeof linkHardening.externalRel !== "string" ||
    linkHardening.externalRel.length === 0
  ) {
    throw new Error(
      "generate.mjs: sanitizer linkHardening.externalRel must be a non-empty string.",
    );
  }

  if (!Array.isArray(corpus.payloads) || corpus.payloads.length === 0) {
    throw new Error(
      "generate.mjs: hostile corpus payloads is missing or empty.",
    );
  }
  const seenIDs = new Set();
  const payloads = corpus.payloads.map((entry, index) => {
    if (
      entry === null ||
      typeof entry !== "object" ||
      typeof entry.id !== "string" ||
      entry.id.length === 0 ||
      typeof entry.category !== "string" ||
      entry.category.length === 0 ||
      typeof entry.payload !== "string"
    ) {
      throw new Error(
        `generate.mjs: hostile corpus payload ${index} has an invalid id, category, or payload.`,
      );
    }
    if (seenIDs.has(entry.id)) {
      throw new Error(
        `generate.mjs: hostile corpus id ${JSON.stringify(entry.id)} is duplicated.`,
      );
    }
    seenIDs.add(entry.id);
    return { id: entry.id, category: entry.category, payload: entry.payload };
  });

  return {
    version: allowlist.version,
    tags,
    attributes,
    urlSchemes: requireStringArray(
      allowlist.urlSchemes,
      "sanitizer urlSchemes",
    ),
    forbiddenTags: requireStringArray(
      forbidden.tags,
      "sanitizer forbidden.tags",
    ),
    forbiddenAttributePrefixes: requireStringArray(
      forbidden.attributePrefixes,
      "sanitizer forbidden.attributePrefixes",
    ),
    forbiddenURLSchemes: requireStringArray(
      forbidden.urlSchemes,
      "sanitizer forbidden.urlSchemes",
    ),
    externalRel: linkHardening.externalRel,
    payloads,
  };
}

function tsFrozenArray(values, indent = "") {
  const entries = values
    .map((value) => `${indent}  ${JSON.stringify(value)},`)
    .join("\n");
  return `Object.freeze([\n${entries}\n${indent}])`;
}

function generateSanitizerTs(contract, outFile) {
  const attributes = Object.entries(contract.attributes)
    .map(
      ([tag, values]) =>
        `  ${JSON.stringify(tag)}: ${tsFrozenArray(values, "  ")},`,
    )
    .join("\n");
  const payloads = contract.payloads
    .map(
      (entry) => `  Object.freeze({
    id: ${JSON.stringify(entry.id)},
    category: ${JSON.stringify(entry.category)},
    payload: ${JSON.stringify(entry.payload)},
  }),`,
    )
    .join("\n");

  const body = `${generatedHeader(
    "validation/sanitizer-allowlist.v1.json and validation/hostile-corpus.json",
  )}

export const SANITIZER_ALLOWLIST_VERSION = ${contract.version} as const;

export const ALLOWED_TAGS = ${tsFrozenArray(contract.tags)};

export const ALLOWED_ATTRIBUTES: Readonly<
  Record<string, readonly string[]>
> = Object.freeze({
${attributes}
});

export const ALLOWED_URL_SCHEMES = ${tsFrozenArray(contract.urlSchemes)};

export const FORBIDDEN_TAGS = ${tsFrozenArray(contract.forbiddenTags)};

export const FORBIDDEN_ATTRIBUTE_PREFIXES = ${tsFrozenArray(
    contract.forbiddenAttributePrefixes,
  )};

export const FORBIDDEN_URL_SCHEMES = ${tsFrozenArray(
    contract.forbiddenURLSchemes,
  )};

export const EXTERNAL_REL = ${JSON.stringify(contract.externalRel)} as const;

export interface HostilePayload {
  readonly id: string;
  readonly category: string;
  readonly payload: string;
}

export const HOSTILE_CORPUS: readonly Readonly<HostilePayload>[] = Object.freeze([
${payloads}
]);
`;

  writeFileSync(outFile, body);
  execFileSync(prettierBin, ["--write", outFile], { stdio: "ignore" });
}

function goStringSlice(values, indent = "") {
  return values.map((value) => `${indent}${JSON.stringify(value)},`).join("\n");
}

function generateSanitizerGo(contract, outFile) {
  const attributes = Object.entries(contract.attributes)
    .map(
      ([tag, values]) =>
        `\t${JSON.stringify(tag)}: {\n${goStringSlice(values, "\t\t")}\n\t},`,
    )
    .join("\n");
  const payloads = contract.payloads
    .map(
      (entry) => `\t{
\t\tID:       ${JSON.stringify(entry.id)},
\t\tCategory: ${JSON.stringify(entry.category)},
\t\tPayload:  ${JSON.stringify(entry.payload)},
\t},`,
    )
    .join("\n");

  const body = `${generatedHeader(
    "validation/sanitizer-allowlist.v1.json and validation/hostile-corpus.json",
  )}

package schema

const (
\tSanitizerAllowlistVersion = ${contract.version}
\tExternalRel               = ${JSON.stringify(contract.externalRel)}
)

var AllowedTags = []string{
${goStringSlice(contract.tags, "\t")}
}

var AllowedAttributes = map[string][]string{
${attributes}
}

var AllowedURLSchemes = []string{
${goStringSlice(contract.urlSchemes, "\t")}
}

var ForbiddenTags = []string{
${goStringSlice(contract.forbiddenTags, "\t")}
}

var ForbiddenAttributePrefixes = []string{
${goStringSlice(contract.forbiddenAttributePrefixes, "\t")}
}

var ForbiddenURLSchemes = []string{
${goStringSlice(contract.forbiddenURLSchemes, "\t")}
}

type HostilePayload struct {
\tID       string
\tCategory string
\tPayload  string
}

var HostileCorpus = []HostilePayload{
${payloads}
}
`;

  writeFileSync(outFile, body);
  execFileSync("gofmt", ["-w", outFile], { stdio: "inherit" });
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

    const sanitizerContract = readSanitizerSources(schema);
    const templatePresets = readTemplatePresets(schema);
    generateSanitizerGo(sanitizerContract, join(goDir, "sanitizer.go"));
    generateSanitizerTs(sanitizerContract, join(tsDir, "sanitizer.ts"));
    generateTemplatesTs(
      templatePresets,
      schema.$defs.sectionType.enum,
      schema.$defs.customization.properties.layout.properties.surfaceTarget
        .enum,
      join(tsDir, "templates.ts"),
    );
    written.push(
      "gen/go/sanitizer.go",
      "gen/ts/sanitizer.ts",
      "gen/ts/templates.ts",
    );

    // Applications compile against these current outputs from the working
    // resume.schema.json.
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
      const versionShared = buildSharedCodegenSchema(
        JSON.parse(versionBytes.toString("utf8")),
      );
      const versionGoDir = join(packageRoot, entry.goPackage);
      const versionTsFile = join(packageRoot, entry.tsTypes);
      mkdirSync(versionGoDir, { recursive: true });
      mkdirSync(dirname(versionTsFile), { recursive: true });

      const versionTmpDir = mkdtempSync(
        join(tmpdir(), `aboutme-schema-codegen-v${entry.version}-`),
      );
      try {
        generateGo(
          versionShared,
          versionTmpDir,
          join(versionGoDir, "resume.go"),
          {
            packageName: `schemav${entry.version}`,
            sourceName: entry.schema,
            sectionMode: "rawSection",
          },
        );
      } finally {
        rmSync(versionTmpDir, { recursive: true, force: true });
      }
      await generateTs(versionShared, versionTsFile, {
        sourceName: entry.schema,
      });
      generateRawSchema(versionBytes, join(versionGoDir, "rawschema.go"), {
        packageName: `schemav${entry.version}`,
        sourceName: entry.schema,
        verifiedBy: `gen/go/released_test.go asserts this\n// byte-equals ../../../${entry.schema} read directly at test time, and\n// test/gen.test.ts's regenerate-and-byte-compare check covers this file\n// too.`,
      });
      written.push(
        `${entry.goPackage}/resume.go`,
        `${entry.goPackage}/rawschema.go`,
        entry.tsTypes,
      );
    }

    generateReleasedGo(manifest, join(goDir, "released.go"));
    generateReleasedTs(manifest, join(tsDir, "released.ts"));
    written.push("gen/go/released.go", "gen/ts/released.ts");
  } finally {
    rmSync(tmpDir, { recursive: true, force: true });
  }

  console.log(`Generated ${written.join(", ")}`);
}

const isMain =
  process.argv[1] !== undefined &&
  import.meta.url === pathToFileURL(process.argv[1]).href;

if (isMain) {
  await main();
}
