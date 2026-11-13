import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { compile as compileTypeScript } from "json-schema-to-typescript";

import {
  generatedHeader,
  quicktypeBin,
  writeGenerated,
} from "./generatePaths.mjs";
import {
  buildGoCodegenSchema,
  buildTsCodegenSchema,
  deriveSectionVariants,
  SECTION_TYPE_DEF,
} from "./generateSchemaTransform.mjs";

function runQuicktype(args) {
  execFileSync(quicktypeBin, args, { stdio: "inherit" });
}

export function generateGo(
  sharedSchema,
  tmpDir,
  outFile,
  { packageName, sourceName, sectionMode },
) {
  const goSchema = buildGoCodegenSchema(sharedSchema);
  const id = goSchema.$id;
  const variants = deriveSectionVariants(sharedSchema);

  // quicktype names positional inputs after their file basenames.
  const schemaFilePath = join(tmpDir, "resume.schema.json");
  writeFileSync(schemaFilePath, JSON.stringify(goSchema, null, 2));

  const entryDefs = variants.map((v) => [v.defKey, v.typeName]);
  const pointerFiles = [];
  for (const [key, name] of [...entryDefs, SECTION_TYPE_DEF]) {
    const pointerPath = join(tmpDir, `${name}.json`);
    writeFileSync(pointerPath, JSON.stringify({ $ref: `${id}#/$defs/${key}` }));
    pointerFiles.push(pointerPath);
  }

  const quicktypeOut = join(tmpDir, "quicktype-output.go");
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
    quicktypeOut,
    schemaFilePath,
    ...pointerFiles,
  ]);

  let raw = readFileSync(quicktypeOut, "utf8");

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
  // quicktype misaligns Go struct fields near inline comments; gofmt fixes it.
  writeGenerated(outFile, body, "go");
}

export async function generateTs(sharedSchema, outFile, { sourceName }) {
  const ts = await compileTypeScript(
    buildTsCodegenSchema(sharedSchema),
    "Resume",
    {
      bannerComment: "",
      style: { semi: true },
      additionalProperties: false,
      // json-schema-to-typescript expands maxItems arrays into tuple unions.
      ignoreMinAndMaxItems: true,
      unknownAny: true,
    },
  );

  writeGenerated(outFile, `${generatedHeader(sourceName)}\n\n${ts}`);
}
