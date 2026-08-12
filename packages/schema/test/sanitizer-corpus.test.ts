import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import {
  ALLOWED_ATTRIBUTES,
  ALLOWED_TAGS,
  ALLOWED_URL_SCHEMES,
  EXTERNAL_REL,
  FORBIDDEN_ATTRIBUTE_PREFIXES,
  FORBIDDEN_TAGS,
  FORBIDDEN_URL_SCHEMES,
  HOSTILE_CORPUS,
  SANITIZER_ALLOWLIST_VERSION,
} from "../gen/ts/sanitizer";

interface AllowlistSource {
  version: number;
  tags: string[];
  attributes: Record<string, string[]>;
  globalAttributes: string[];
  urlSchemes: string[];
  forbidden: {
    tags: string[];
    attributePrefixes: string[];
    urlSchemes: string[];
  };
  linkHardening: { externalRel: string };
}

interface CorpusSource {
  version: number;
  payloads: Array<{ id: string; category: string; payload: string }>;
}

interface SchemaSource {
  $defs: { sanitizerAllowlistVersion: { const: number } };
}

const readJSON = <T>(path: string): T =>
  JSON.parse(readFileSync(new URL(path, import.meta.url), "utf8")) as T;

const sourceAllowlist = readJSON<AllowlistSource>(
  "../validation/sanitizer-allowlist.v1.json",
);
const sourceCorpus = readJSON<CorpusSource>(
  "../validation/hostile-corpus.json",
);
const sourceSchema = readJSON<SchemaSource>("../resume.schema.json");

describe("generated sanitizer data", () => {
  it("matches both validation sources and the schema version", () => {
    expect(SANITIZER_ALLOWLIST_VERSION).toBe(sourceAllowlist.version);
    expect(SANITIZER_ALLOWLIST_VERSION).toBe(sourceCorpus.version);
    expect(SANITIZER_ALLOWLIST_VERSION).toBe(
      sourceSchema.$defs.sanitizerAllowlistVersion.const,
    );
    expect(sourceAllowlist.globalAttributes).toEqual([]);
    expect(ALLOWED_TAGS).toEqual(sourceAllowlist.tags);
    expect(ALLOWED_ATTRIBUTES).toEqual(sourceAllowlist.attributes);
    expect(ALLOWED_URL_SCHEMES).toEqual(sourceAllowlist.urlSchemes);
    expect(FORBIDDEN_TAGS).toEqual(sourceAllowlist.forbidden.tags);
    expect(FORBIDDEN_ATTRIBUTE_PREFIXES).toEqual(
      sourceAllowlist.forbidden.attributePrefixes,
    );
    expect(FORBIDDEN_URL_SCHEMES).toEqual(sourceAllowlist.forbidden.urlSchemes);
    expect(EXTERNAL_REL).toBe(sourceAllowlist.linkHardening.externalRel);
    expect(HOSTILE_CORPUS).toEqual(
      sourceCorpus.payloads.map(({ id, category, payload }) => ({
        id,
        category,
        payload,
      })),
    );
  });
});

describe("sanitizer source coverage", () => {
  it("keeps allowed and forbidden URL schemes disjoint", () => {
    expect([...sourceAllowlist.urlSchemes].sort()).toEqual([
      "https",
      "mailto",
      "tel",
    ]);
    for (const scheme of sourceAllowlist.forbidden.urlSchemes) {
      expect(sourceAllowlist.urlSchemes).not.toContain(scheme);
    }
  });

  it("keeps allowed and forbidden tags disjoint", () => {
    expect(sourceAllowlist.tags.length).toBeGreaterThan(0);
    for (const tag of sourceAllowlist.forbidden.tags) {
      expect(sourceAllowlist.tags).not.toContain(tag);
    }
  });

  it("has unique, non-empty payloads for every required attack class", () => {
    const ids = sourceCorpus.payloads.map(({ id }) => id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const { payload } of sourceCorpus.payloads) {
      expect(payload.length).toBeGreaterThan(0);
    }

    const categories = new Set(
      sourceCorpus.payloads.map(({ category }) => category),
    );
    for (const required of [
      "javascript-scheme",
      "data-scheme",
      "protocol-relative",
      "obfuscated-scheme",
      "event-handler-attribute",
    ]) {
      expect(categories).toContain(required);
    }
  });

  it("covers every explicitly forbidden scheme and tag", () => {
    for (const scheme of sourceAllowlist.forbidden.urlSchemes) {
      expect(
        sourceCorpus.payloads.some(({ payload }) =>
          payload.toLowerCase().includes(`${scheme}:`),
        ),
        `missing hostile payload for ${scheme}:`,
      ).toBe(true);
    }
    for (const tag of sourceAllowlist.forbidden.tags) {
      expect(
        sourceCorpus.payloads.some(({ payload }) =>
          payload.toLowerCase().includes(`<${tag}`),
        ),
        `missing hostile payload for <${tag}>`,
      ).toBe(true);
    }
  });

  it("covers link hardening, entity encoding, and nested normalization", () => {
    expect(
      sourceCorpus.payloads.some(
        ({ payload }) =>
          payload.includes('target="_blank"') && !payload.includes("rel="),
      ),
    ).toBe(true);
    expect(
      sourceCorpus.payloads.some(({ payload }) => payload.includes("&#")),
    ).toBe(true);
    expect(
      sourceCorpus.payloads.some(
        ({ category }) => category === "nested-tag-normalization",
      ),
    ).toBe(true);
  });
});
