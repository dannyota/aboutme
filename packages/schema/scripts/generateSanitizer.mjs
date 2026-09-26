// Reads and validates the sanitizer allowlist and hostile-input corpus, then
// emits the TypeScript and Go sanitizer policy artifacts derived from them.

import { readFileSync } from "node:fs";

import {
  generatedHeader,
  hostileCorpusPath,
  sanitizerAllowlistPath,
  writeGenerated,
} from "./generatePaths.mjs";

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

export function readSanitizerSources(schema) {
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

export function generateSanitizerPolicyTs(contract, outFile) {
  const attributes = Object.entries(contract.attributes)
    .map(
      ([tag, values]) =>
        `  ${JSON.stringify(tag)}: ${tsFrozenArray(values, "  ")},`,
    )
    .join("\n");

  const body = `${generatedHeader(
    "validation/sanitizer-allowlist.v1.json",
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
`;

  writeGenerated(outFile, body, "prettier");
}

export function generateSanitizerTs(contract, outFile) {
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

export * from "./sanitizer-policy";

export interface HostilePayload {
  readonly id: string;
  readonly category: string;
  readonly payload: string;
}

export const HOSTILE_CORPUS: readonly Readonly<HostilePayload>[] = Object.freeze([
${payloads}
]);
`;

  writeGenerated(outFile, body, "prettier");
}

function goStringSlice(values, indent = "") {
  return values.map((value) => `${indent}${JSON.stringify(value)},`).join("\n");
}

export function generateSanitizerGo(contract, outFile) {
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

  writeGenerated(outFile, body, "go");
}
