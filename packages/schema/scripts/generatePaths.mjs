// Filesystem locations, the shared "generated" banner every generator in
// this directory writes at the top of its output, and the one function that
// writes each output. Centralized here so every sibling module derives the
// same paths regardless of which file computes them (all live in
// packages/schema/scripts, so the relative depth is the same either way).

import { execFileSync } from "node:child_process";
import { renameSync, rmSync, writeFileSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export const packageRoot = dirname(dirname(fileURLToPath(import.meta.url)));
export const schemaPath = join(packageRoot, "resume.schema.json");
export const manifestPath = join(packageRoot, "released-versions.json");
export const templateDirectory = process.env.ABOUTME_TEMPLATE_DIR
  ? fileURLToPath(new URL(`file://${process.env.ABOUTME_TEMPLATE_DIR}`))
  : join(packageRoot, "templates");
export const sanitizerAllowlistPath = join(
  packageRoot,
  "validation",
  "sanitizer-allowlist.v1.json",
);
export const hostileCorpusPath = join(
  packageRoot,
  "validation",
  "hostile-corpus.json",
);
export const quicktypeBin = join(packageRoot, "node_modules", ".bin", "quicktype");
// json-schema-to-typescript already installs the formatter it uses for the
// generated resume types. Reuse that pinned binary for the sanitizer artifact.
export const prettierBin = join(packageRoot, "node_modules", ".bin", "prettier");

// The Go import path of the module gen/go/go.mod declares. The released-schema
// registry (gen/go/released.go) imports one retained package per released
// version from underneath it — they are packages inside that same module, not
// modules of their own, so nothing in go.work changes when a version is added.
export const GO_MODULE_PATH = "github.com/dannyota/aboutme/packages/schema/gen/go";

export const generatedHeader = (sourceName) =>
  `// Code generated from ${sourceName}. DO NOT EDIT.`;

// writeGenerated formats content with gofmt or Prettier (format "go" or
// "prettier"; any other value writes it unchanged) and then replaces outFile
// in one rename. `npm test` regenerates these files in place while other test
// files compile and read them, so a reader must only ever see a complete,
// formatted file. The temporary name starts with a dot, which Go builds
// ignore, and carries the process ID, so concurrent generator runs never
// share one.
export function writeGenerated(outFile, content, format) {
  let formatted = content;
  if (format === "go") {
    formatted = execFileSync("gofmt", [], {
      input: content,
      encoding: "utf8",
      stdio: ["pipe", "pipe", "inherit"],
    });
  } else if (format === "prettier") {
    formatted = execFileSync(prettierBin, ["--stdin-filepath", outFile], {
      input: content,
      encoding: "utf8",
      stdio: ["pipe", "pipe", "inherit"],
    });
  }
  const temporary = join(
    dirname(outFile),
    `.${basename(outFile)}.${process.pid}.tmp`,
  );
  try {
    writeFileSync(temporary, formatted);
    renameSync(temporary, outFile);
  } catch (error) {
    rmSync(temporary, { force: true });
    throw error;
  }
}
