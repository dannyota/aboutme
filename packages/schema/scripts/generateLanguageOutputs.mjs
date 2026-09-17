// Emits the per-language types (Go via quicktype, TypeScript via
// json-schema-to-typescript) from the shared in-memory schema copy.

import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { compile as compileTypeScript } from "json-schema-to-typescript";

import { generatedHeader, quicktypeBin } from "./generatePaths.mjs";
import {
  buildGoCodegenSchema,
  buildTsCodegenSchema,
  deriveSectionVariants,
  SECTION_TYPE_DEF,
} from "./generateSchemaTransform.mjs";

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
export function generateGo(
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

export async function generateTs(sharedSchema, outFile, { sourceName }) {
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
