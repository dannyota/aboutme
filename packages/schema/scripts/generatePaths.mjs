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
export const prettierBin = join(packageRoot, "node_modules", ".bin", "prettier");

export const GO_MODULE_PATH = "danny.vn/aboutme/packages/schema/gen/go";

export const generatedHeader = (sourceName) =>
  `// Code generated from ${sourceName}. DO NOT EDIT.`;

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
