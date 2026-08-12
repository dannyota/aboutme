import { readFileSync } from "node:fs";
import { describe, expect, expectTypeOf, it } from "vitest";

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

interface ResumeSchemaSource {
  $defs: { sanitizerAllowlistVersion: { const: number } };
}

const readJSON = <T>(relativePath: string): T =>
  JSON.parse(readFileSync(new URL(relativePath, import.meta.url), "utf8")) as T;

const sourceAllowlist = readJSON<AllowlistSource>(
  "../validation/sanitizer-allowlist.v1.json",
);
const sourceCorpus = readJSON<CorpusSource>(
  "../validation/hostile-corpus.json",
);
const sourceSchema = readJSON<ResumeSchemaSource>("../resume.schema.json");

const EXPECTED_TAGS = [
  "p",
  "br",
  "strong",
  "em",
  "u",
  "ol",
  "ul",
  "li",
  "a",
] as const;
const EXPECTED_ATTRIBUTES = { a: ["href", "rel", "target"] } as const;
const EXPECTED_URL_SCHEMES = ["https", "mailto", "tel"] as const;
const EXPECTED_FORBIDDEN_TAGS = [
  "script",
  "style",
  "iframe",
  "svg",
  "img",
  "object",
  "embed",
  "form",
  "input",
  "link",
  "meta",
  "base",
] as const;
const EXPECTED_FORBIDDEN_ATTRIBUTE_PREFIXES = ["on"] as const;
const EXPECTED_FORBIDDEN_URL_SCHEMES = [
  "javascript",
  "data",
  "vbscript",
  "file",
] as const;

const EXPECTED_CORPUS = [
  {
    id: "js-scheme-bare",
    category: "javascript-scheme",
    payload: "javascript:alert(1)",
  },
  {
    id: "js-scheme-in-anchor-href",
    category: "javascript-scheme",
    payload: '<a href="javascript:alert(document.cookie)">click me</a>',
  },
  {
    id: "js-scheme-mixed-case",
    category: "obfuscated-scheme",
    payload: '<a href="JavaScript:alert(1)">click me</a>',
  },
  {
    id: "js-scheme-upper-case",
    category: "obfuscated-scheme",
    payload: '<a href="JAVASCRIPT:alert(1)">click me</a>',
  },
  {
    id: "js-scheme-leading-whitespace",
    category: "obfuscated-scheme",
    payload: '<a href="   javascript:alert(1)">click me</a>',
  },
  {
    id: "js-scheme-embedded-tab",
    category: "obfuscated-scheme",
    payload: '<a href="java\tscript:alert(1)">click me</a>',
  },
  {
    id: "js-scheme-embedded-newline",
    category: "obfuscated-scheme",
    payload: '<a href="java\nscript:alert(1)">click me</a>',
  },
  {
    id: "data-scheme-html",
    category: "data-scheme",
    payload: '<a href="data:text/html,<script>alert(1)</script>">click me</a>',
  },
  {
    id: "data-scheme-base64-script",
    category: "data-scheme",
    payload:
      '<a href="data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==">click me</a>',
  },
  {
    id: "vbscript-scheme-in-anchor-href",
    category: "vbscript-scheme",
    payload: '<a href="vbscript:msgbox(1)">click me</a>',
  },
  {
    id: "file-scheme-in-anchor-href",
    category: "file-scheme",
    payload: '<a href="file:///etc/passwd">click me</a>',
  },
  {
    id: "rel-hardening-target-blank-missing-rel",
    category: "rel-hardening",
    payload: '<a href="https://example.com" target="_blank">click me</a>',
  },
  {
    id: "rel-hardening-target-blank-weak-rel",
    category: "rel-hardening",
    payload:
      '<a href="https://example.com" target="_blank" rel="opener">click me</a>',
  },
  {
    id: "protocol-relative",
    category: "protocol-relative",
    payload: '<a href="//evil.example.com/phish">click me</a>',
  },
  {
    id: "protocol-relative-whitespace",
    category: "protocol-relative",
    payload: '<a href=" //evil.example.com/phish">click me</a>',
  },
  {
    id: "event-handler-img-onerror",
    category: "event-handler-attribute",
    payload: '<img src="x" onerror="alert(1)">',
  },
  {
    id: "event-handler-svg-onload",
    category: "event-handler-attribute",
    payload: '<svg onload="alert(1)"></svg>',
  },
  {
    id: "event-handler-anchor-onclick",
    category: "event-handler-attribute",
    payload: '<a href="https://example.com" onclick="alert(1)">click me</a>',
  },
  {
    id: "iframe-js-src",
    category: "forbidden-tag",
    payload: '<iframe src="javascript:alert(1)"></iframe>',
  },
  {
    id: "style-tag-expression",
    category: "forbidden-tag",
    payload: "<style>body{background:url(javascript:alert(1))}</style>",
  },
  {
    id: "object-data-js",
    category: "forbidden-tag",
    payload: '<object data="javascript:alert(1)"></object>',
  },
  {
    id: "embed-src-js",
    category: "forbidden-tag",
    payload: '<embed src="javascript:alert(1)">',
  },
  {
    id: "form-action-js",
    category: "forbidden-tag",
    payload: '<form action="javascript:alert(1)"><button>go</button></form>',
  },
  {
    id: "input-autofocus-onfocus",
    category: "forbidden-tag",
    payload: '<input onfocus="alert(1)" autofocus>',
  },
  {
    id: "link-rel-stylesheet-js",
    category: "forbidden-tag",
    payload: '<link rel="stylesheet" href="javascript:alert(1)">',
  },
  {
    id: "meta-refresh-js",
    category: "forbidden-tag",
    payload: '<meta http-equiv="refresh" content="0;url=javascript:alert(1)">',
  },
  {
    id: "base-href-js",
    category: "forbidden-tag",
    payload: '<base href="javascript:alert(1)/">',
  },
  {
    id: "js-scheme-html-entity-encoded",
    category: "obfuscated-scheme",
    payload: '<a href="&#106;avascript:alert(1)">click me</a>',
  },
  {
    id: "nested-script-tag-stripping-bypass",
    category: "nested-tag-normalization",
    payload: "<scr<script>ipt>alert(1)</scr</script>ipt>",
  },
] as const;

describe("generated sanitizer contract", () => {
  it("pins the source and generated versions to the resume schema", () => {
    expect(sourceSchema.$defs.sanitizerAllowlistVersion.const).toBe(1);
    expect(sourceAllowlist.version).toBe(1);
    expect(sourceCorpus.version).toBe(1);
    expect(SANITIZER_ALLOWLIST_VERSION).toBe(1);
    expect(SANITIZER_ALLOWLIST_VERSION).toBe(
      sourceSchema.$defs.sanitizerAllowlistVersion.const,
    );

    expectTypeOf(SANITIZER_ALLOWLIST_VERSION).toEqualTypeOf<1>();
  });

  it("copies the exact frozen allowlist matrix", () => {
    expect(sourceAllowlist.tags).toEqual(EXPECTED_TAGS);
    expect(sourceAllowlist.attributes).toEqual(EXPECTED_ATTRIBUTES);
    expect(sourceAllowlist.globalAttributes).toEqual([]);
    expect(sourceAllowlist.urlSchemes).toEqual(EXPECTED_URL_SCHEMES);
    expect(sourceAllowlist.forbidden.tags).toEqual(EXPECTED_FORBIDDEN_TAGS);
    expect(sourceAllowlist.forbidden.attributePrefixes).toEqual(
      EXPECTED_FORBIDDEN_ATTRIBUTE_PREFIXES,
    );
    expect(sourceAllowlist.forbidden.urlSchemes).toEqual(
      EXPECTED_FORBIDDEN_URL_SCHEMES,
    );
    expect(sourceAllowlist.linkHardening.externalRel).toBe(
      "noopener noreferrer",
    );

    expect(ALLOWED_TAGS).toEqual(EXPECTED_TAGS);
    expect(ALLOWED_ATTRIBUTES).toEqual(EXPECTED_ATTRIBUTES);
    expect(ALLOWED_URL_SCHEMES).toEqual(EXPECTED_URL_SCHEMES);
    expect(FORBIDDEN_TAGS).toEqual(EXPECTED_FORBIDDEN_TAGS);
    expect(FORBIDDEN_ATTRIBUTE_PREFIXES).toEqual(
      EXPECTED_FORBIDDEN_ATTRIBUTE_PREFIXES,
    );
    expect(FORBIDDEN_URL_SCHEMES).toEqual(EXPECTED_FORBIDDEN_URL_SCHEMES);
    expect(EXTERNAL_REL).toBe("noopener noreferrer");
  });

  it("defines attributes independently for every allowed tag", () => {
    expect(Object.keys(ALLOWED_ATTRIBUTES)).toEqual(["a"]);

    for (const tag of EXPECTED_TAGS) {
      const expected = tag === "a" ? ["href", "rel", "target"] : [];
      expect(ALLOWED_ATTRIBUTES[tag] ?? [], tag).toEqual(expected);
    }
  });

  it("copies every hostile corpus id, category, and payload", () => {
    const sourceTriples = sourceCorpus.payloads.map(
      ({ id, category, payload }) => ({ id, category, payload }),
    );

    expect(sourceTriples).toEqual(EXPECTED_CORPUS);
    expect(HOSTILE_CORPUS).toEqual(EXPECTED_CORPUS);
    expect(new Set(HOSTILE_CORPUS.map(({ id }) => id)).size).toBe(
      EXPECTED_CORPUS.length,
    );
  });

  it("exposes readonly, non-aliased generated collections", () => {
    expectTypeOf(ALLOWED_TAGS).not.toMatchTypeOf<string[]>();
    expectTypeOf(ALLOWED_ATTRIBUTES).toMatchTypeOf<
      Readonly<Record<string, readonly string[]>>
    >();
    expectTypeOf(ALLOWED_ATTRIBUTES.a).not.toMatchTypeOf<string[]>();
    expectTypeOf(ALLOWED_URL_SCHEMES).not.toMatchTypeOf<string[]>();
    expectTypeOf(FORBIDDEN_TAGS).not.toMatchTypeOf<string[]>();
    expectTypeOf(FORBIDDEN_ATTRIBUTE_PREFIXES).not.toMatchTypeOf<string[]>();
    expectTypeOf(FORBIDDEN_URL_SCHEMES).not.toMatchTypeOf<string[]>();
    expectTypeOf(HOSTILE_CORPUS).not.toMatchTypeOf<unknown[]>();
    expectTypeOf(EXTERNAL_REL).toEqualTypeOf<"noopener noreferrer">();

    const collections: readonly (readonly unknown[])[] = [
      ALLOWED_TAGS,
      ALLOWED_ATTRIBUTES.a,
      ALLOWED_URL_SCHEMES,
      FORBIDDEN_TAGS,
      FORBIDDEN_ATTRIBUTE_PREFIXES,
      FORBIDDEN_URL_SCHEMES,
      HOSTILE_CORPUS,
    ];
    for (const [index, collection] of collections.entries()) {
      for (const other of collections.slice(index + 1)) {
        expect(collection).not.toBe(other);
      }
    }
  });
});
