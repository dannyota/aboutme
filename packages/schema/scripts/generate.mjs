#!/usr/bin/env node
// Generates current and retained Go and TypeScript types from the declared
// JSON Schema files. Run `npm run generate` here or `make schema-gen` at the
// repository root.
//
// json-schema-to-typescript emits the TypeScript discriminated union directly.
// quicktype emits the Go entry structs, while hand-written section.go provides
// the Section dispatch that Go cannot represent as a sum type. In-memory schema
// transforms compensate for generator limits; AJV validates the unchanged
// source schema. Section variants are derived from $defs.section.oneOf, and
// test/conformance.test.ts checks generator fidelity independently. See
// docs/design/repository.md.
//
// This file orchestrates the generation run; the sibling generate*.mjs modules
// hold the per-topic logic (schema transforms, per-language codegen, the raw
// schema embed, the released-version registry, templates, and the sanitizer
// policy).

import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { pathToFileURL } from "node:url";

import { packageRoot, schemaPath } from "./generatePaths.mjs";
import { generateRawSchema } from "./generateRawSchema.mjs";
import {
  generateReleasedGo,
  generateReleasedTs,
  readReleasedManifest,
} from "./generateReleasedRegistry.mjs";
import { buildSharedCodegenSchema } from "./generateSchemaTransform.mjs";
import { generateGo, generateTs } from "./generateLanguageOutputs.mjs";
import {
  generateSanitizerGo,
  generateSanitizerPolicyTs,
  generateSanitizerTs,
  readSanitizerSources,
} from "./generateSanitizer.mjs";
import { generateTemplatesTs, readTemplatePresets } from "./generateTemplates.mjs";

export { generateTemplatesTs } from "./generateTemplates.mjs";

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
    generateSanitizerPolicyTs(
      sanitizerContract,
      join(tsDir, "sanitizer-policy.ts"),
    );
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
      "gen/ts/sanitizer-policy.ts",
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
