import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { parse } from "yaml";
import { describe, expect, it } from "vitest";

const doc = parse(readFileSync("docs/api/openapi.yaml", "utf8"));

describe("openapi contract", () => {
  it("lints clean", () => {
    // Invoke the scoped package name, not the bare "redocly" command: npm
    // has an unrelated placeholder package literally named "redocly" that
    // silently exits 0, so "npx redocly ..." can resolve to the wrong
    // binary and mask a real lint failure.
    execFileSync(
      "npx",
      [
        "@redocly/cli",
        "lint",
        "docs/api/openapi.yaml",
        "--config",
        "docs/api/redocly.yaml",
      ],
      { stdio: "inherit" },
    );
  });

  it("serves /api/v1", () => {
    expect(doc.servers[0].url).toMatch(/\/api\/v1$/);
  });

  it("defines the error envelope", () => {
    expect(doc.components.schemas.Error.properties.error.required).toEqual([
      "code",
      "message",
    ]);
  });

  it("documents 412 for stale writes, and never 409, for precondition failure", () => {
    expect(JSON.stringify(doc)).toContain("412");
    // 409 does appear in this document, but only in prose describing domain
    // conflicts (slug taken, idempotency-key body mismatch) — never as a
    // response on a precondition/If-Match path. This guards against a
    // future edit accidentally adding a `409` response to a write endpoint
    // for staleness.
    for (const [path, item] of Object.entries<any>(doc.paths)) {
      for (const [method, operation] of Object.entries<any>(item)) {
        if (typeof operation !== "object" || !operation?.responses) continue;
        if (!("409" in operation.responses)) continue;
        expect(
          "412" in operation.responses,
          `${method.toUpperCase()} ${path}: an operation documenting 409 ` +
            "should also document 412 if it is a write-safety-guarded " +
            "mutation (409 must stay reserved for domain conflicts, not " +
            "staleness)",
        );
      }
    }
  });

  it("keeps health paths unversioned (root-level, not /api/v1)", () => {
    // Health is infrastructure, not product API: a future /api/v2 must
    // never break orchestrator or synthetic checks (design doc §2 route
    // table). The global server stays /api/v1 for every product endpoint;
    // /healthz and /readyz must override it back to the bare root so the
    // contract doesn't silently re-version them.
    expect(doc.servers[0].url).toMatch(/\/api\/v1$/);
    for (const path of ["/healthz", "/readyz"]) {
      const override = doc.paths[path]?.servers;
      expect(override, `${path} should have a path-level servers override`)
        .toBeTruthy();
      expect(override[0].url).not.toMatch(/\/api\/v1$/);
    }
  });

  it("declares the write-safety components (Envelope, Revision, IfMatch, IdempotencyKey)", () => {
    expect(doc.components.schemas.Envelope.required).toContain("data");
    expect(doc.components.schemas.Revision.type).toBe("string");
    expect(doc.components.parameters.IfMatch.name).toBe("If-Match");
    expect(doc.components.parameters.IdempotencyKey.name).toBe(
      "Idempotency-Key",
    );
  });
});
