// Filesystem locations and the shared "generated" banner every generator in
// this directory writes at the top of its output. Centralized here so every
// sibling module derives the same paths regardless of which file computes
// them (all live in packages/schema/scripts, so the relative depth is the
// same either way).

import { dirname, join } from "node:path";
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
