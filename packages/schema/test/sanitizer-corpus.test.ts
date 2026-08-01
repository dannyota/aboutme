import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// validation/sanitizer-allowlist.v1.json and validation/hostile-corpus.json
// (design spec §5 sanitizer contract) are the data both bluemonday
// (apps/server, write path) and DOMPurify (apps/web, render path) will be
// generated/conformance-tested against — that wiring lives outside
// packages/schema, but the data itself, and its shape, lives here. These
// tests guard the shape so a future edit can't silently drop a required
// category or scheme.
const root = new URL("..", import.meta.url).pathname;
const allowlist = JSON.parse(
  readFileSync(join(root, "validation", "sanitizer-allowlist.v1.json"), "utf8"),
);
const corpus = JSON.parse(
  readFileSync(join(root, "validation", "hostile-corpus.json"), "utf8"),
);
const schema = JSON.parse(
  readFileSync(join(root, "resume.schema.json"), "utf8"),
);

describe("sanitizer-allowlist.v1.json", () => {
  it("version matches resume.schema.json's sanitizerAllowlistVersion", () => {
    expect(allowlist.version).toBe(
      schema.$defs.sanitizerAllowlistVersion.const,
    );
  });

  it("permits exactly https, mailto, tel and nothing else", () => {
    expect(allowlist.urlSchemes.sort()).toEqual(["https", "mailto", "tel"]);
  });

  it("forbids javascript:/data: explicitly, and they don't overlap the permitted list", () => {
    expect(allowlist.forbidden.urlSchemes).toEqual(
      expect.arrayContaining(["javascript", "data"]),
    );
    for (const scheme of allowlist.forbidden.urlSchemes) {
      expect(allowlist.urlSchemes).not.toContain(scheme);
    }
  });

  it("declares at least one allowed tag and no forbidden tag is also allowed", () => {
    expect(allowlist.tags.length).toBeGreaterThan(0);
    for (const tag of allowlist.forbidden.tags) {
      expect(allowlist.tags).not.toContain(tag);
    }
  });
});

describe("hostile-corpus.json", () => {
  it("has at least one payload", () => {
    expect(corpus.payloads.length).toBeGreaterThan(0);
  });

  it("every payload has a unique id", () => {
    const ids = corpus.payloads.map((p: { id: string }) => p.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("covers every category the phase-gate review required: javascript:, data:, protocol-relative, mixed-case/whitespace-obfuscated schemes, and event-handler attributes", () => {
    const categories = new Set(
      corpus.payloads.map((p: { category: string }) => p.category),
    );
    for (const required of [
      "javascript-scheme",
      "data-scheme",
      "protocol-relative",
      "obfuscated-scheme",
      "event-handler-attribute",
    ]) {
      expect(
        categories,
        `missing hostile-corpus category "${required}"`,
      ).toContain(required);
    }
  });

  it("every payload is a non-empty string", () => {
    for (const p of corpus.payloads) {
      expect(typeof p.payload).toBe("string");
      expect(p.payload.length).toBeGreaterThan(0);
    }
  });

  // Phase-gate review finding I2: the corpus had zero payloads for two of
  // the allowlist's own forbidden.urlSchemes (vbscript:, file:) — the
  // allowlist and the corpus had silently drifted apart, and this suite's
  // old category check couldn't notice because it only asserted five
  // hardcoded category names, none of which covered the gap. This test is
  // deliberately NOT hardcoded to today's scheme list: it derives the
  // required schemes from allowlist.forbidden.urlSchemes directly, so a
  // future scheme added there without a matching corpus payload fails here
  // instead of silently shipping an ungraded sanitizer conformance corpus
  // (design spec §5 / AC-SEC-003).
  it("every forbidden.urlSchemes scheme in the allowlist has at least one corpus payload exercising it", () => {
    for (const scheme of allowlist.forbidden.urlSchemes) {
      const covering = corpus.payloads.filter((p: { payload: string }) =>
        p.payload.toLowerCase().includes(`${scheme}:`),
      );
      expect(
        covering.length,
        `no hostile-corpus payload contains "${scheme}:" (sanitizer-allowlist.v1.json forbids this scheme)`,
      ).toBeGreaterThan(0);
    }
  });

  // Phase-gate review finding I2: sanitizer-allowlist.v1.json's
  // linkHardening mandates rel="noopener noreferrer" on every emitted <a>,
  // but no corpus payload exercised target="_blank" at all — a sanitizer
  // that dropped rel hardening entirely would still pass the whole corpus.
  it("covers the mandated rel=\"noopener noreferrer\" hardening on target=\"_blank\" anchors", () => {
    const relHardeningPayloads = corpus.payloads.filter((p: { payload: string }) =>
      p.payload.includes('target="_blank"'),
    );
    expect(
      relHardeningPayloads.length,
      'no hostile-corpus payload exercises target="_blank" (sanitizer-allowlist.v1.json linkHardening)',
    ).toBeGreaterThan(0);
    // At least one of those payloads must supply NO rel at all (proving the
    // sanitizer must ADD it, not just correct a wrong one) — this is what
    // "rel-hardening-target-blank-missing-rel" exists for.
    expect(
      relHardeningPayloads.some((p: { payload: string }) => !p.payload.includes("rel=")),
      'expected at least one target="_blank" payload with no rel attribute at all',
    ).toBe(true);
  });

  // Phase-gate re-review finding NEW-M4: the mechanical drift guard above
  // covers forbidden.urlSchemes but not forbidden.tags, one field over in
  // the same allowlist object — 7 of 12 forbidden tags (object, embed,
  // form, input, link, meta, base) had zero corpus coverage. Mirrors the
  // schemes test exactly: derived from allowlist.forbidden.tags directly,
  // not hardcoded, so a future forbidden tag added there without a
  // matching payload fails here.
  it("every forbidden.tags entry in the allowlist has at least one corpus payload exercising it", () => {
    for (const tag of allowlist.forbidden.tags) {
      const covering = corpus.payloads.filter((p: { payload: string }) =>
        p.payload.toLowerCase().includes(`<${tag}`),
      );
      expect(
        covering.length,
        `no hostile-corpus payload contains "<${tag}" (sanitizer-allowlist.v1.json forbids this tag)`,
      ).toBeGreaterThan(0);
    }
  });

  // Phase-gate re-review finding NEW-M4: the two leftovers from I2's own
  // original "concrete fix" list (an entity-encoded scheme and a
  // nested/normalization payload) were still absent after I2 was marked
  // closed on its literally-stated scope (forbidden.urlSchemes coverage).
  it("covers an HTML-entity-encoded scheme (bypasses a literal-string scheme check)", () => {
    const covering = corpus.payloads.filter((p: { payload: string }) =>
      p.payload.includes("&#"),
    );
    expect(
      covering.length,
      "expected at least one payload with an HTML numeric character reference (&#...;) encoding a scheme",
    ).toBeGreaterThan(0);
  });

  it("covers a nested/normalization tag-stripping bypass", () => {
    const covering = corpus.payloads.filter(
      (p: { category: string }) => p.category === "nested-tag-normalization",
    );
    expect(
      covering.length,
      "expected at least one payload demonstrating a single-pass tag-stripping bypass",
    ).toBeGreaterThan(0);
  });
});
